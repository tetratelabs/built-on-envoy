// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package waf

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"go.uber.org/zap"

	waf "github.com/tetratelabs/built-on-envoy/extensions/composer/waf/coraza"
)

// wafMode defines the operation mode for the WAF plugin.
type wafMode int

const (
	// modeRequestOnly processes only the request phase.
	modeRequestOnly wafMode = iota
	// modeFull processes both the request and response phases.
	modeFull
	// modeResponseOnly processes only the response phase.
	//
	// Deprecated: RESPONSE_ONLY is retained for backward compatibility and will be
	// removed in a future release. It skips request-phase initialization, so rule
	// sets whose response-phase rules depend on the request phase (such as OWASP
	// CRS) can misbehave. Prefer FULL or REQUEST_ONLY.
	modeResponseOnly
)

// defaultRequestURIPathOnly is the default for request_uri_path_only.
// It is planned to become true in a future release.
const defaultRequestURIPathOnly = false

// config is the parsed WAF filter configuration: the filter configuration and the per-route
// configurations are both parsed into it, as a whole, by parseConfig.
type config struct {
	// engine is what the WAF engine is built from.
	engine waf.Config
	mode   wafMode
	// requestURIPathOnly sets REQUEST_URI to the path and query instead of the absolute URL.
	requestURIPathOnly bool
	// metrics are the metrics settings. Metrics are defined once per filter configuration, so only
	// the filter configuration's are used: per-route configurations do not override them.
	metrics metricsConfig
}

// parseConfig parses the raw configuration bytes passed at the Envoy filter configuration.
// configBytes must be a valid json of the WAF configuration. Unknown keys in the metrics object are
// rejected, so that a misspelled setting is not silently ignored.
func parseConfig(configBytes []byte, l *zap.Logger) (config, error) {
	var y struct {
		// List of Coraza directive which will be joined with newlines. Use list here to
		// simplify writing multi-line directives in JSON/YAML.
		Directives         []string        `json:"directives"`
		ModeString         string          `json:"mode"`
		RequestURIPathOnly *bool           `json:"request_uri_path_only,omitempty"`
		Metrics            json.RawMessage `json:"metrics"`
	}
	if err := json.Unmarshal(configBytes, &y); err != nil {
		return config{}, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	var mode wafMode
	switch y.ModeString {
	case "REQUEST_ONLY":
		mode = modeRequestOnly
	case "RESPONSE_ONLY":
		// Deprecated: retained for backward compatibility. See modeResponseOnly.
		l.Warn("RESPONSE_ONLY mode is deprecated and will be removed in a future release; use FULL or REQUEST_ONLY instead")
		mode = modeResponseOnly
	case "FULL":
		mode = modeFull
	case "":
		mode = modeRequestOnly
	default:
		return config{}, fmt.Errorf("invalid mode: %s", y.ModeString)
	}

	requestURIPathOnly := defaultRequestURIPathOnly
	if y.RequestURIPathOnly != nil {
		requestURIPathOnly = *y.RequestURIPathOnly
	}

	metrics, err := parseMetrics(y.Metrics)
	if err != nil {
		return config{}, err
	}

	return config{
		engine:             waf.Config{Directives: strings.Join(y.Directives, "\n")},
		mode:               mode,
		requestURIPathOnly: requestURIPathOnly,
		metrics:            metrics,
	}, nil
}

// parseMetrics parses the metrics object of the filter configuration, if there is one.
func parseMetrics(raw json.RawMessage) (metricsConfig, error) {
	var cfg metricsConfig
	if len(raw) == 0 {
		return cfg, nil
	}
	var m struct {
		AuthorityLabel *bool `json:"authority_label"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return metricsConfig{}, fmt.Errorf("invalid metrics: %w", err)
	}

	if m.AuthorityLabel != nil && !*m.AuthorityLabel {
		cfg.authority.disabled = true
	}
	return cfg, nil
}
