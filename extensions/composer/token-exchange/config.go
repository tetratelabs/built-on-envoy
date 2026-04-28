// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package oauth2te

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

const (
	defaultSubjectTokenType        = "urn:ietf:params:oauth:token-type:access_token"   // #nosec G101 -- not a credential.
	grantTypeTokenExchange         = "urn:ietf:params:oauth:grant-type:token-exchange" // #nosec G101 -- not a credential.
	defaultTimeoutMs        uint64 = 5000
)

// tokenExchangeConfig holds the configuration for the OAuth2 Token Exchange
// filter following the RFC 8693 (https://datatracker.ietf.org/doc/html/rfc8693).
type tokenExchangeConfig struct {
	// Envoy cluster name that routes to the token exchange endpoint.
	Cluster string `json:"cluster"`
	// URL of the token exchange endpoint, e.g. "sts.example.com/oauth2/token".
	// The URL scheme can be included but will be ignored in favor of the cluster's transport socket configuration.
	TokenExchangeURL string `json:"token_exchange_url"`
	// Client identifier used for HTTP Basic authentication with the authorization server.
	ClientID string `json:"client_id"`
	// Client secret used for HTTP Basic authentication with the authorization server.
	ClientSecret string `json:"client_secret"`
	// Type of the presented security token (subject_token parameter in the request).
	// Optional. Defaults to "urn:ietf:params:oauth:token-type:access_token".
	SubjectTokenType string `json:"subject_token_type"`
	// A URI that indicates the target service or resource where the client intends
	// to use the requested security token.
	// It must be an absolute URI without a fragment component. Optional.
	Resource string `json:"resource"`
	// The logical name of the target service where the client intends to use
	// the requested security token. Optional.
	Audience string `json:"audience"`
	// List of space-delimited, case-sensitive strings that allow the client
	// to specify the desired scope of the requested security token in the
	// context of the service or resource where the token will be used. Optional.
	Scope string `json:"scope"`
	// Type of the requested security token. If unspecified, the issued token
	// type is at the discretion of the authorization server. Optional.
	RequestedTokenType string `json:"requested_token_type"`
	// A security token that represents the identity of the acting party.
	// Used for delegation. Optional.
	ActorToken string `json:"actor_token"`
	// Type of the actor token. Required when actor_token is present.
	ActorTokenType string `json:"actor_token_type"`

	// HTTP callout timeout in milliseconds. Optional. Defaults to 5000.
	TimeoutMs uint64 `json:"timeout_ms"`

	// tokenExchangeHost is extracted from TokenExchangeURL at config time.
	tokenExchangeHost string
	// tokenExchangePath is extracted from TokenExchangeURL at config time.
	tokenExchangePath string
	// calloutHeaders is the precomputed set of headers for the request to the STS.
	calloutHeaders [][2]string
	// stsPostBodyPrefix is the URL-encoded precomputed body with all static fields,
	// ending with "&subject_token=" so the per-request token value can be appended.
	stsPostBodyPrefix string
}

// stsPostBody returns the full URL-encoded form body for the token exchange request,
// appending the given subject token to the precomputed stsPostBodyPrefix.
func (c *tokenExchangeConfig) stsPostBody(subjectToken string) []byte {
	return []byte(c.stsPostBodyPrefix + url.QueryEscape(subjectToken))
}

// parseTokenExchangeURL extracts the host and path from the token exchange URL.
// It accepts both scheme-less ("host/path") and absolute ("http://host/path").
// The scheme is ignored in favor of the cluster's transport socket configuration.
func parseTokenExchangeURL(inputURL string) (host, path string, err error) {
	if inputURL == "" {
		return "", "", fmt.Errorf("missing required field: token_exchange_url")
	}
	// Prepend a scheme if missing, so url.Parse treats it as an absolute URL
	parsedURL := inputURL
	if !strings.Contains(inputURL, "://") {
		parsedURL = "scheme://" + parsedURL
	}
	u, parseErr := url.Parse(parsedURL)
	if parseErr != nil || u.Host == "" || u.Path == "" {
		return "", "", fmt.Errorf("token_exchange_url must contain a host and path, got %q", inputURL)
	}
	return u.Host, u.RequestURI(), nil
}

// parseConfig parses and validates the JSON configuration.
func parseConfig(data []byte) (*tokenExchangeConfig, error) {
	if len(data) == 0 {
		return nil, nil
	}

	cfg := &tokenExchangeConfig{}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("token-exchange: failed to parse config: %w", err)
	}

	var (
		errs []error
		err  error
	)
	if cfg.Cluster == "" {
		errs = append(errs, fmt.Errorf("missing required field: cluster"))
	}
	cfg.tokenExchangeHost, cfg.tokenExchangePath, err = parseTokenExchangeURL(cfg.TokenExchangeURL)
	if err != nil {
		errs = append(errs, err)
	}
	if cfg.ClientID == "" {
		errs = append(errs, fmt.Errorf("missing required field: client_id"))
	}
	if cfg.ClientSecret == "" {
		errs = append(errs, fmt.Errorf("missing required field: client_secret"))
	}
	if cfg.Resource != "" {
		// As per RFC: the value of the resource parameter MUST be an absolute URI, [..] that MAY include
		// a query component and MUST NOT include a fragment component.
		u, err := url.Parse(cfg.Resource)
		if err != nil || !u.IsAbs() || u.Fragment != "" {
			errs = append(errs, fmt.Errorf("resource must be an absolute URI without fragment, got %q", cfg.Resource))
		}
	}
	if cfg.ActorToken != "" && cfg.ActorTokenType == "" {
		errs = append(errs, fmt.Errorf("actor_token_type is required when actor_token is present"))
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("token-exchange: %w", errors.Join(errs...))
	}

	// Apply defaults.
	if cfg.SubjectTokenType == "" {
		cfg.SubjectTokenType = defaultSubjectTokenType
	}
	if cfg.TimeoutMs == 0 {
		cfg.TimeoutMs = defaultTimeoutMs
	}

	// Precompute fixed values derived from the config
	creds := base64.StdEncoding.EncodeToString([]byte(cfg.ClientID + ":" + cfg.ClientSecret))
	cfg.calloutHeaders = [][2]string{
		{":method", "POST"},
		{":path", cfg.tokenExchangePath},
		{"host", cfg.tokenExchangeHost},
		{"content-type", "application/x-www-form-urlencoded"},
		{"authorization", "Basic " + creds},
	}

	// Precompute request body of the token exchange: every field is static
	// except for the subject_token, which will be appended at every request
	static := url.Values{}
	static.Set("grant_type", grantTypeTokenExchange)
	static.Set("subject_token_type", cfg.SubjectTokenType)
	if cfg.RequestedTokenType != "" {
		static.Set("requested_token_type", cfg.RequestedTokenType)
	}
	if cfg.Audience != "" {
		static.Set("audience", cfg.Audience)
	}
	if cfg.Resource != "" {
		static.Set("resource", cfg.Resource)
	}
	if cfg.Scope != "" {
		static.Set("scope", cfg.Scope)
	}
	if cfg.ActorToken != "" {
		static.Set("actor_token", cfg.ActorToken)
		static.Set("actor_token_type", cfg.ActorTokenType)
	}
	// at request time we will append the URL-encoded subject token value.
	cfg.stsPostBodyPrefix = static.Encode() + "&subject_token="
	return cfg, nil
}
