// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package mcpopenapi is a config-time-only Envoy dynamic module. It reads an
// OpenAPI spec at init and writes the ECDS config for Envoy's native
// MCP-to-REST bridge filter, which serves all MCP traffic.
package mcpopenapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/pkg"
)

const userAgent = "built-on-envoy-mcp-openapi"

const (
	filterName = "mcp-openapi"
	// defaultBridgeConfigPath must match the path_config_source of the bridge filter in the
	// manifest. Envoy requires a directory in the path, so use ./ for the working directory.
	defaultBridgeConfigPath = "./mcp-openapi-bridge.json"
	// defaultSpecFetchTimeout bounds the spec download, as it blocks Envoy's main thread
	// while the configuration is loaded.
	defaultSpecFetchTimeout = 30 * time.Second
)

var (
	_ shared.HttpFilterConfigFactory = (*configFactory)(nil)
	_ shared.HttpFilterFactory       = (*filterFactory)(nil)
	_ shared.HttpFilter              = (*filter)(nil)
)

// WellKnownHttpFilterConfigFactories registers the mcp-openapi filter with the
// Envoy dynamic module loader.
func WellKnownHttpFilterConfigFactories() map[string]shared.HttpFilterConfigFactory { //nolint:revive
	return map[string]shared.HttpFilterConfigFactory{
		filterName: new(configFactory),
	}
}

type configFactory struct {
	shared.EmptyHttpFilterConfigFactory
}

// Create loads an OpenAPI spec and writes the ECDS config for Envoy's MCP-to-REST bridge.
func (cf *configFactory) Create(handle shared.HttpFilterConfigHandle, raw []byte) (shared.HttpFilterFactory, error) {
	var cfg config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	if err := cfg.Spec.validate(); err != nil {
		return nil, fmt.Errorf("invalid spec config: %w", err)
	}

	bridgeConfigPath := cfg.BridgeConfigPath
	if bridgeConfigPath == "" {
		bridgeConfigPath = defaultBridgeConfigPath
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Spec.timeout())
	defer cancel()
	specData, err := cfg.Spec.content(ctx, http.DefaultClient)
	if err != nil {
		return nil, fmt.Errorf("reading spec %s: %w", cfg.Spec, err)
	}

	doc, err := loadOpenAPISpec(specData)
	if err != nil {
		return nil, fmt.Errorf("loading spec: %w", err)
	}

	tools, err := generateTools(doc, &cfg)
	if err != nil {
		return nil, fmt.Errorf("generating tools: %w", err)
	}

	// The bridge ECDS maps each tool to an HTTP rule so Envoy routes MCP calls to REST, and
	// has the tool definitions so it answers tools/list locally.
	if err := writeECDSConfig(bridgeConfigPath, tools); err != nil {
		return nil, fmt.Errorf("writing bridge ECDS config: %w", err)
	}

	handle.Log(shared.LogLevelInfo, "mcp-openapi: loaded %d tools from %s", len(tools), cfg.Spec)

	return new(filterFactory), nil
}

var (
	errSpecSourceMultipleSet = errors.New("only one of 'inline', 'file' or 'url' can be set")
	errSpecSourceNoneSet     = errors.New("one of 'inline', 'file' or 'url' must be set")
)

// specSource is where the OpenAPI spec is loaded from: inline, a local file or a remote URL.
type specSource struct {
	pkg.DataSource
	// URL is the HTTP(S) URL of the spec.
	URL string `json:"url,omitempty"`
	// Timeout is the maximum time to fetch the spec from the URL, as a Go duration string.
	Timeout string `json:"timeout,omitempty"`

	parsedTimeout time.Duration
}

// timeout returns the maximum time to load the spec.
func (s *specSource) timeout() time.Duration {
	if s.parsedTimeout > 0 {
		return s.parsedTimeout
	}
	return defaultSpecFetchTimeout
}

// validate checks that exactly one of the spec sources is set, and that the timeout is valid.
func (s *specSource) validate() error {
	if s.Timeout != "" {
		d, err := time.ParseDuration(s.Timeout)
		if err != nil || d <= 0 {
			return fmt.Errorf("invalid timeout %q: must be a positive duration", s.Timeout)
		}
		s.parsedTimeout = d
	}

	set := 0
	for _, v := range []string{s.Inline, s.File, s.URL} {
		if v != "" {
			set++
		}
	}
	switch set {
	case 0:
		return errSpecSourceNoneSet
	case 1:
		return nil
	default:
		return errSpecSourceMultipleSet
	}
}

// content returns the raw spec, fetching it with the given client when it is a remote URL.
func (s *specSource) content(ctx context.Context, client *http.Client) ([]byte, error) {
	if s.URL == "" {
		return s.Content()
	}
	resp, err := httpGet(ctx, client, s.URL, userAgent)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck // not actionable in defer
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// String describes the spec source for logs and errors.
func (s specSource) String() string {
	switch {
	case s.URL != "":
		return s.URL
	case s.File != "":
		return s.File
	default:
		return "<inline>"
	}
}

type filterFactory struct {
	shared.EmptyHttpFilterFactory
}

func (ff *filterFactory) Create(_ shared.HttpFilterHandle) shared.HttpFilter {
	return new(filter)
}

type filter struct {
	shared.EmptyHttpFilter
}
