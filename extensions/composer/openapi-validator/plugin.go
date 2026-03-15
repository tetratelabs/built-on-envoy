// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package openapivalidator implements an OpenAPI request validation HTTP filter plugin.
package openapivalidator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/utility"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/pkg"
)

// openAPIValidatorConfig represents the JSON configuration for this filter.
type openAPIValidatorConfig struct {
	// Spec is the OpenAPI specification, either inline or from a file.
	Spec pkg.DataSource `json:"spec"`
	// MaxBodyBytes is the maximum request body size in bytes to buffer for validation.
	// 0 means no limit. If the body exceeds this limit, the request is rejected with 413.
	MaxBodyBytes uint64 `json:"max_body_bytes"`
	// AllowUnmatchedPaths when true allows requests to paths not defined in the OpenAPI spec.
	AllowUnmatchedPaths bool `json:"allow_unmatched_paths"`
	// DryRun when true logs validation failures but always allows the request.
	DryRun bool `json:"dry_run"`
	// DenyResponse is the local response to return on validation failure.
	// Optional. Default status is 400; default body is the validation error message.
	DenyResponse *pkg.LocalResponse `json:"deny_response,omitempty"`
}

// openAPIValidatorParsedConfig holds the parsed configuration and the compiled router.
type openAPIValidatorParsedConfig struct {
	openAPIValidatorConfig
	router routers.Router
	// denyResponseHeaders is pre-computed from DenyHeaders at config time to avoid
	// allocating on every deny response.
	denyResponseHeaders [][2]string
}

// openAPIValidatorHttpFilter is the per-request HTTP filter instance.
type openAPIValidatorHttpFilter struct { //nolint:revive
	shared.EmptyHttpFilter
	handle shared.HttpFilterHandle
	config *openAPIValidatorParsedConfig

	// Route match result from the OpenAPI router, set in OnRequestHeaders.
	route      *routers.Route
	pathParams map[string]string

	// Accumulated body size across OnRequestBody calls.
	bodySize uint64
	// Whether the request has already been fully validated.
	requestProcessed bool
}

// buildHTTPRequest constructs an http.Request from the Envoy request headers and an optional body.
// It reads headers directly from the handle to avoid storing them across callbacks.
func (o *openAPIValidatorHttpFilter) buildHTTPRequest(body []byte) (*http.Request, error) {
	headers := o.handle.RequestHeaders()

	method := headers.GetOne(":method").ToUnsafeString()
	path := headers.GetOne(":path").ToUnsafeString()
	host := headers.GetOne(":authority").ToUnsafeString()
	scheme := headers.GetOne(":scheme").ToUnsafeString()
	if scheme == "" {
		scheme = "http"
	}

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	httpReq, err := http.NewRequest(method, scheme+"://"+host+path, bodyReader) //nolint:noctx
	if err != nil {
		return nil, err
	}

	// Copy non-pseudo headers.
	for _, h := range headers.GetAll() {
		if !strings.HasPrefix(h[0].ToUnsafeString(), ":") {
			httpReq.Header.Add(h[0].ToUnsafeString(), h[1].ToUnsafeString())
		}
	}
	httpReq.Host = host
	if body != nil {
		httpReq.ContentLength = int64(len(body))
	}

	return httpReq, nil
}

func (o *openAPIValidatorHttpFilter) OnRequestHeaders(headers shared.HeaderMap, endOfStream bool) shared.HeadersStatus {
	method := headers.GetOne(":method").ToUnsafeString()
	path := headers.GetOne(":path").ToUnsafeString()
	host := headers.GetOne(":authority").ToUnsafeString()
	scheme := headers.GetOne(":scheme").ToUnsafeString()
	if scheme == "" {
		scheme = "http"
	}

	// Find matching route in the OpenAPI spec.
	httpReq, err := http.NewRequest(method, scheme+"://"+host+path, nil) //nolint:noctx
	if err != nil {
		o.handle.Log(shared.LogLevelError, "openapi-validator: failed to construct request for route matching: %s", err.Error())
		return o.denyRequest(err)
	}

	route, pathParams, err := o.config.router.FindRoute(httpReq)
	if err != nil {
		if o.config.AllowUnmatchedPaths {
			o.handle.Log(shared.LogLevelInfo, "openapi-validator: allowing unmatched path %s %s", method, path)
			return shared.HeadersStatusContinue
		}
		o.handle.Log(shared.LogLevelInfo, "openapi-validator: no matching operation for %s %s: %s", method, path, err.Error())
		return o.denyRequest(fmt.Errorf("no matching operation for %s %s", method, path))
	}
	o.route = route
	o.pathParams = pathParams

	// Check if the operation defines a request body and the stream is not ended yet.
	if !endOfStream && route.Operation != nil && route.Operation.RequestBody != nil {
		return shared.HeadersStatusStop
	}

	// No body to wait for; validate the request now.
	if o.validateRequest(nil) {
		return shared.HeadersStatusContinue
	}
	return shared.HeadersStatusStop
}

func (o *openAPIValidatorHttpFilter) OnRequestBody(body shared.BodyBuffer, endOfStream bool) shared.BodyStatus {
	if o.requestProcessed {
		return shared.BodyStatusContinue
	}

	// Track accumulated body size.
	if body != nil {
		o.bodySize += body.GetSize()
	}

	// Check if body exceeds the configured maximum.
	if o.config.MaxBodyBytes > 0 && o.bodySize > o.config.MaxBodyBytes {
		o.requestProcessed = true
		o.handle.Log(shared.LogLevelInfo, "openapi-validator: request body size %d exceeds max_body_bytes %d",
			o.bodySize, o.config.MaxBodyBytes)
		if !o.config.DryRun {
			o.handle.SendLocalResponse(413, nil, []byte("Request body too large"), "openapi_body_too_large")
			return shared.BodyStatusStopNoBuffer
		}
		o.handle.Log(shared.LogLevelInfo, "openapi-validator: dry-run: would reject oversized body")
		return shared.BodyStatusContinue
	}

	if endOfStream {
		if o.validateRequest(utility.ReadWholeRequestBody(o.handle)) {
			return shared.BodyStatusContinue
		}
		return shared.BodyStatusStopNoBuffer
	}

	return shared.BodyStatusStopAndBuffer
}

func (o *openAPIValidatorHttpFilter) OnRequestTrailers(_ shared.HeaderMap) shared.TrailersStatus {
	if o.requestProcessed {
		return shared.TrailersStatusContinue
	}

	// Trailers mean endOfStream was never true in OnRequestBody.
	// Read the full body and validate now.
	if o.validateRequest(utility.ReadWholeRequestBody(o.handle)) {
		return shared.TrailersStatusContinue
	}
	return shared.TrailersStatusStop
}

// validateRequest validates the request against the OpenAPI spec. Returns true if the
// request should be allowed through.
func (o *openAPIValidatorHttpFilter) validateRequest(body []byte) bool {
	o.requestProcessed = true

	httpReq, err := o.buildHTTPRequest(body)
	if err != nil {
		o.handle.Log(shared.LogLevelError, "openapi-validator: failed to construct request: %s", err.Error())
		o.sendDenyResponse(err)
		return false
	}

	o.handle.Log(shared.LogLevelDebug, "openapi-validator: validating request %s %s",
		httpReq.Method, httpReq.URL.Path)

	input := &openapi3filter.RequestValidationInput{
		Request:    httpReq,
		PathParams: o.pathParams,
		Route:      o.route,
	}

	err = openapi3filter.ValidateRequest(context.Background(), input)
	if err != nil {
		o.handle.Log(shared.LogLevelInfo, "openapi-validator: validation failed for %s %s: %s",
			httpReq.Method, httpReq.URL.Path, err.Error())

		if o.config.DryRun {
			o.handle.Log(shared.LogLevelInfo, "openapi-validator: dry-run: would deny request")
			return true
		}

		o.sendDenyResponse(err)
		return false
	}

	return true
}

// denyRequest handles a validation failure from OnRequestHeaders. It respects dry_run mode.
func (o *openAPIValidatorHttpFilter) denyRequest(err error) shared.HeadersStatus {
	if o.config.DryRun {
		o.handle.Log(shared.LogLevelInfo, "openapi-validator: dry-run: would deny request: %s", err.Error())
		return shared.HeadersStatusContinue
	}
	o.sendDenyResponse(err)
	return shared.HeadersStatusStop
}

// sendDenyResponse sends a local response rejecting the request.
func (o *openAPIValidatorHttpFilter) sendDenyResponse(validationErr error) {
	body := o.config.DenyResponse.Body
	if body == "" {
		body = validationErr.Error()
	}
	o.handle.SendLocalResponse(
		uint32(o.config.DenyResponse.Status), //nolint:gosec
		o.config.denyResponseHeaders,
		[]byte(body),
		"openapi_validation_failed",
	)
}

// openAPIValidatorHttpFilterFactory creates filter instances per-request.
type openAPIValidatorHttpFilterFactory struct { //nolint:revive
	shared.EmptyHttpFilterFactory
	config *openAPIValidatorParsedConfig
}

func (o *openAPIValidatorHttpFilterFactory) Create(handle shared.HttpFilterHandle) shared.HttpFilter {
	return &openAPIValidatorHttpFilter{handle: handle, config: o.config}
}

// OpenAPIValidatorHttpFilterConfigFactory is the configuration factory for the HTTP filter.
type OpenAPIValidatorHttpFilterConfigFactory struct { //nolint:revive
	shared.EmptyHttpFilterConfigFactory
}

// Create parses the JSON configuration, loads the OpenAPI spec, and creates a factory.
func (o *OpenAPIValidatorHttpFilterConfigFactory) Create(handle shared.HttpFilterConfigHandle, config []byte) (shared.HttpFilterFactory, error) {
	if len(config) == 0 {
		handle.Log(shared.LogLevelError, "openapi-validator: empty config")
		return nil, fmt.Errorf("empty config")
	}

	cfg := openAPIValidatorConfig{}
	if err := json.Unmarshal(config, &cfg); err != nil {
		handle.Log(shared.LogLevelError, "openapi-validator: failed to parse config: %s", err.Error())
		return nil, err
	}

	if err := cfg.Spec.Validate(); err != nil {
		handle.Log(shared.LogLevelError, "openapi-validator: invalid spec config: %s", err.Error())
		return nil, fmt.Errorf("invalid spec config: %w", err)
	}

	handle.Log(shared.LogLevelDebug, "openapi-validator: loading spec (max_body_bytes=%d, dry_run=%v)",
		cfg.MaxBodyBytes, cfg.DryRun)

	specData, err := cfg.Spec.Content()
	if err != nil {
		handle.Log(shared.LogLevelError, "openapi-validator: failed to read spec: %s", err.Error())
		return nil, fmt.Errorf("failed to read spec: %w", err)
	}

	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(specData)
	if err != nil {
		handle.Log(shared.LogLevelError, "openapi-validator: failed to parse spec: %s", err.Error())
		return nil, fmt.Errorf("failed to parse spec: %w", err)
	}

	if err = doc.Validate(context.Background()); err != nil {
		handle.Log(shared.LogLevelError, "openapi-validator: invalid OpenAPI spec: %s", err.Error())
		return nil, fmt.Errorf("invalid OpenAPI spec: %w", err)
	}

	router, err := gorillamux.NewRouter(doc)
	if err != nil {
		handle.Log(shared.LogLevelError, "openapi-validator: failed to create router: %s", err.Error())
		return nil, fmt.Errorf("failed to create router: %w", err)
	}

	handle.Log(shared.LogLevelDebug, "openapi-validator: spec loaded and router created successfully")

	if cfg.DenyResponse == nil {
		cfg.DenyResponse = &pkg.LocalResponse{
			Status: 400,
		}
	} else {
		if cfg.DenyResponse.Status == 0 {
			cfg.DenyResponse.Status = 400
		}
		if err := cfg.DenyResponse.Validate(); err != nil {
			handle.Log(shared.LogLevelError, "openapi-validator: invalid deny_response config: %s", err.Error())
			return nil, fmt.Errorf("invalid deny_response config: %w", err)
		}
	}

	// Pre-compute deny response headers to avoid allocating per-request.
	var denyResponseHeaders [][2]string
	for k, v := range cfg.DenyResponse.Headers {
		denyResponseHeaders = append(denyResponseHeaders, [2]string{k, v})
	}

	parsed := &openAPIValidatorParsedConfig{
		openAPIValidatorConfig: cfg,
		router:                 router,
		denyResponseHeaders:    denyResponseHeaders,
	}

	return &openAPIValidatorHttpFilterFactory{config: parsed}, nil
}

// ExtensionName is the name of the extension that will be used in the
// `run` command to refer to this plugin.
const ExtensionName = "openapi-validator"

// WellKnownHttpFilterConfigFactories is used to load the plugin.
func WellKnownHttpFilterConfigFactories() map[string]shared.HttpFilterConfigFactory { //nolint:revive
	return map[string]shared.HttpFilterConfigFactory{
		ExtensionName: &OpenAPIValidatorHttpFilterConfigFactory{},
	}
}
