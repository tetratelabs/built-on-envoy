// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package coraza provides WAF configuration and initialization using the Coraza WAF engine.
package coraza

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"

	"github.com/corazawaf/coraza/v3"
	ctypes "github.com/corazawaf/coraza/v3/types"
	"go.uber.org/zap"
)

// WAFMode defines the operation mode for the WAF plugin.
type WAFMode int

const (
	// ModeRequestOnly processes only the request phase.
	ModeRequestOnly WAFMode = iota
	// ModeFull processes both the request and response phases.
	ModeFull
	// ModeResponseOnly processes only the response phase.
	//
	// Deprecated: RESPONSE_ONLY is retained for backward compatibility and will be
	// removed in a future release. It skips request-phase initialization, so rule
	// sets whose response-phase rules depend on the request phase (such as OWASP
	// CRS) can misbehave. Prefer FULL or REQUEST_ONLY.
	ModeResponseOnly
)

// defaultRequestURIPathOnly is the default for request_uri_path_only.
// It is planned to become true in a future release.
const defaultRequestURIPathOnly = false

// parsedConfig is the parsed WAF filter configuration.
type parsedConfig struct {
	// Directives are the Coraza directives, joined with newlines.
	Directives string
	Mode       WAFMode
	// RequestURIPathOnly sets REQUEST_URI to the path and query instead of the absolute URL.
	RequestURIPathOnly bool
}

// parseConfig parses the raw configuration bytes passed at the Envoy filter configuration.
// configBytes must be a valid json of WAFConfig.
func parseConfig(configBytes []byte, l *zap.Logger) (parsedConfig, error) {
	var y struct {
		// List of Coraza directive which will be joined with newlines. Use list here to
		// simplify writing multi-line directives in JSON/YAML.
		Directives         []string `json:"directives"`
		ModeString         string   `json:"mode"`
		RequestURIPathOnly *bool    `json:"request_uri_path_only,omitempty"`
	}
	if err := json.Unmarshal(configBytes, &y); err != nil {
		return parsedConfig{}, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	var mode WAFMode
	switch y.ModeString {
	case "REQUEST_ONLY":
		mode = ModeRequestOnly
	case "RESPONSE_ONLY":
		// Deprecated: retained for backward compatibility. See ModeResponseOnly.
		l.Warn("RESPONSE_ONLY mode is deprecated and will be removed in a future release; use FULL or REQUEST_ONLY instead")
		mode = ModeResponseOnly
	case "FULL":
		mode = ModeFull
	case "":
		mode = ModeRequestOnly
	default:
		return parsedConfig{}, fmt.Errorf("invalid mode: %s", y.ModeString)
	}

	requestURIPathOnly := defaultRequestURIPathOnly
	if y.RequestURIPathOnly != nil {
		requestURIPathOnly = *y.RequestURIPathOnly
	}

	// Join directives with newlines
	return parsedConfig{
		Directives:         strings.Join(y.Directives, "\n"),
		Mode:               mode,
		RequestURIPathOnly: requestURIPathOnly,
	}, nil
}

// GetOrCreateSharedWAFFromConfig parses the raw configuration bytes passed at the Envoy filter
// configuration and returns its WAF, mode and whether REQUEST_URI is the path only. The WAF is
// shared: a live one built from the same directives is reused if the files it was built from are
// unchanged.
func GetOrCreateSharedWAFFromConfig(configBytes []byte, l *zap.Logger) (*SharedWAF, WAFMode, bool, error) {
	cfg, err := parseConfig(configBytes, l)
	if err != nil {
		return nil, 0, false, err
	}
	sharedWAF, err := sharedWAFs.getOrCreate(cfg.Directives, l, combinedDirectivesFS)
	if err != nil {
		return nil, 0, false, err
	}
	return sharedWAF, cfg.Mode, cfg.RequestURIPathOnly, nil
}

// newWAF creates a new WAF, reading included and data files from root.
func newWAF(directives string, l *zap.Logger, root fs.FS) (coraza.WAF, error) {
	conf := coraza.NewWAFConfig().
		WithErrorCallback(newSlogError(l)).
		WithRootFS(root)
	return coraza.NewWAF(conf.WithDirectives(directives))
}

func newSlogError(l *zap.Logger) func(err ctypes.MatchedRule) {
	return func(err ctypes.MatchedRule) {
		msg := err.ErrorLog()
		severity := strings.ToLower(err.Rule().Severity().String())
		severityField := zap.String("severity", severity)

		switch severity {
		case "emergency", "alert", "critical", "error":
			l.Error(msg, severityField)
		case "warning":
			l.Warn(msg, severityField)
		default:
			// Rules without an explicit severity "unknown" and informational severities ("notice", "info") are logged at Info level.
			// The error callback is only invoked for triggered rules with the log action, so suppressing these at Debug level would silently drop intentional log entries.
			l.Info(msg, severityField)
		}
	}
}
