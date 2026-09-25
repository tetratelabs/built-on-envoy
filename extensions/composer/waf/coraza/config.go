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

// Config is the parsed WAF filter configuration.
type Config struct {
	// Directives are the Coraza directives, joined with newlines.
	Directives string
	Mode       WAFMode
}

// ParseConfig parses the raw configuration bytes passed at the Envoy filter configuration.
// configBytes must be a valid json of WAFConfig.
func ParseConfig(configBytes []byte, l *zap.Logger) (Config, error) {
	var y struct {
		// List of Coraza directive which will be joined with newlines. Use list here to
		// simplify writing multi-line directives in JSON/YAML.
		Directives []string `json:"directives"`
		ModeString string   `json:"mode"`
	}
	if err := json.Unmarshal(configBytes, &y); err != nil {
		return Config{}, fmt.Errorf("failed to unmarshal config: %w", err)
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
		return Config{}, fmt.Errorf("invalid mode: %s", y.ModeString)
	}

	// Join directives with newlines
	return Config{Directives: strings.Join(y.Directives, "\n"), Mode: mode}, nil
}

// NewWAFConfigFromBytes creates a new WAF from the given raw configuration bytes passed at the
// Envoy filter configuration. configBytes must be a valid json of WAFConfig.
//
// The returned WAF is not shared: use ParseConfig and GetOrCreateSharedWAF for filter configs.
func NewWAFConfigFromBytes(configBytes []byte, l *zap.Logger) (coraza.WAF, WAFMode, error) {
	config, err := ParseConfig(configBytes, l)
	if err != nil {
		return nil, 0, err
	}
	waf, err := NewWAFFromDirectives(config.Directives, l)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create WAF from directives: %w", err)
	}
	return waf, config.Mode, nil
}

// NewWAFFromDirectives creates a new WAF from the given directives.
func NewWAFFromDirectives(directives string, l *zap.Logger) (coraza.WAF, error) {
	return newWAF(directives, l, combinedDirectivesFS)
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
