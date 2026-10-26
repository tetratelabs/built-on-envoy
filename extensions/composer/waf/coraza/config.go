// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package coraza provides WAF configuration and initialization using the Coraza WAF engine.
package coraza

import (
	"io/fs"
	"strings"

	"github.com/corazawaf/coraza/v3"
	ctypes "github.com/corazawaf/coraza/v3/types"
	"go.uber.org/zap"
)

// Config is what a WAF is built from. It is parsed, together with the rest of the filter
// configuration, by the caller.
type Config struct {
	// Directives are the Coraza directives, joined with newlines.
	Directives string
}

// GetOrCreateSharedWAFFromConfig returns the WAF built from cfg. The WAF is shared: a live one
// built from the same directives is reused if the files it was built from are unchanged.
func GetOrCreateSharedWAFFromConfig(cfg Config, l *zap.Logger) (*SharedWAF, error) {
	return sharedWAFs.getOrCreate(cfg.Directives, l, combinedDirectivesFS)
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
