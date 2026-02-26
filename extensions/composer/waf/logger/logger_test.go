// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package logger

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
)

func TestGetLogger(t *testing.T) {
	t.Run("returns the same logger instance", func(t *testing.T) {
		l1 := GetLogger()
		l2 := GetLogger()
		require.NotNil(t, l1)
		require.Same(t, l1, l2)
	})

	t.Run("production mode by default", func(t *testing.T) {
		l := GetLogger()
		// Production logger does not have debug enabled
		require.False(t, l.Core().Enabled(zapcore.DebugLevel))
	})

	t.Run("development mode", func(t *testing.T) {
		once = sync.Once{} // Reset once to reinitialize logger with new env var
		t.Setenv("LOG_MODE", "development")
		l := GetLogger()
		// Development logger has debug enabled
		require.True(t, l.Core().Enabled(zapcore.DebugLevel))
	})
}
