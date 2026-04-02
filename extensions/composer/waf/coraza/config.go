// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package coraza provides WAF configuration and initialization using the Coraza WAF engine.
package coraza

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/corazawaf/coraza/v3"
	ctypes "github.com/corazawaf/coraza/v3/types"
	"go.uber.org/zap"
)

// WAFMode defines the operation mode for the WAF plugin.
type WAFMode int

const (
	// ModeRequestOnly processes only request phase
	ModeRequestOnly WAFMode = iota
	// ModeFull processes both request and response phases
	ModeFull
	// ModeResponseOnly processes only response phase
	ModeResponseOnly
)

// NewWAFConfigFromBytes creates a new WAF from the given raw configuration bytes passed at the
// Envoy filter configuration. configBytes must be a valid json of WAFConfig.
//
// This returns a new WAF instance and a logger. The logger is guaranteed to be non-nil regardless
// of the error state.
func NewWAFConfigFromBytes(configBytes []byte, l *zap.Logger) (coraza.WAF, WAFMode, error) {
	var y struct {
		// List of Coraza directive which will be joined with newlines. Use list here to
		// simplify writing multi-line directives in JSON/YAML.
		Directives []string `json:"directives"`
		ModeString string   `json:"mode"`
	}
	if err := json.Unmarshal(configBytes, &y); err != nil {
		return nil, 0, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Join directives with newlines
	directives := strings.Join(y.Directives, "\n")

	waf, err := NewWAFFromDirectives(directives, l)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create WAF from directives: %w", err)
	}
	var mode WAFMode
	switch y.ModeString {
	case "REQUEST_ONLY":
		mode = ModeRequestOnly
	case "RESPONSE_ONLY":
		mode = ModeResponseOnly
	case "FULL":
		mode = ModeFull
	case "":
		mode = ModeRequestOnly
	default:
		return nil, 0, fmt.Errorf("invalid mode: %s", y.ModeString)
	}

	return waf, mode, nil
}

// NewWAFFromDirectives creates a new WAF from the given directives.
func NewWAFFromDirectives(directives string, l *zap.Logger) (coraza.WAF, error) {
	conf := coraza.NewWAFConfig().
		WithErrorCallback(newSlogError(l)).
		WithRootFS(combinedDirectivesFS)
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
		case "notice", "info":
			l.Info(msg, severityField)
		default:
			l.Debug(msg, severityField)
		}
	}
}
