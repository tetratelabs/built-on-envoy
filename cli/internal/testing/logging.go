// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package internaltesting

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

// OutBuffer combines io.Writer with fmt.Stringer for buffer access
type OutBuffer interface {
	io.Writer
	fmt.Stringer
	Reset()
}

// OutBuffers allows you to reset all the buffers easily.
type OutBuffers []OutBuffer

// Reset resets all the buffers.
func (s OutBuffers) Reset() {
	for _, buf := range s {
		buf.Reset()
	}
}

// outBuffer is a thread-safe buffer implementing OutBuffer
type outBuffer struct {
	mu sync.RWMutex
	b  *bytes.Buffer
}

func (s *outBuffer) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.b.Reset()
}

func (s *outBuffer) Write(p []byte) (n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *outBuffer) String() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.b.String()
}

// CaptureOutput creates labeled log capture writers in the same order as labels.
func CaptureOutput(labels ...string) OutBuffers {
	buffers := make([]OutBuffer, len(labels))

	for i := range labels {
		buffers[i] = &outBuffer{b: bytes.NewBuffer(nil)}
	}

	return buffers
}

// DumpLogsOnFail creates labeled log capture writers that dump only on test failure.
// Returns WriterStringers in the same order as labels.
func DumpLogsOnFail(t testing.TB, labels ...string) OutBuffers {
	buffers := CaptureOutput(labels...)

	t.Cleanup(func() {
		if t.Failed() {
			for i, label := range labels {
				out := buffers[i].String()
				if len(out) == 0 {
					continue
				}
				t.Logf("=== %s ===\n%s", label, out)
			}
		}
	})

	return buffers
}

// TLogHandler is a slog.Handler that writes logs to a testing.T instance, allowing log capture in tests.
type TLogHandler struct {
	t     *testing.T
	attrs []slog.Attr
	group string
}

// NewTLogger creates a new slog.Logger that writes to the provided testing.T instance with the specified log level.
func NewTLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(&TLogHandler{t: t})
}

// Enabled always returns true to capture all log levels.
func (h *TLogHandler) Enabled(context.Context, slog.Level) bool { return true }

// Handle formats the log record and writes it to the testing.T instance.
func (h *TLogHandler) Handle(_ context.Context, r slog.Record) error { //nolint:gocritic
	attrs := make([]slog.Attr, 0, r.NumAttrs())

	// Collect record attributes
	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a)
		return true
	})

	var msg strings.Builder
	_, _ = fmt.Fprintf(&msg, "[%s] %s", r.Level.String(), r.Message)
	for _, a := range append(h.attrs, attrs...) {
		_, _ = fmt.Fprintf(&msg, " %s=%v", a.Key, a.Value.Any())
	}

	h.t.Log(msg.String())
	return nil
}

// WithAttrs returns a new TLogHandler with the provided attributes added to the existing ones.
func (h *TLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newAttrs := append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &TLogHandler{t: h.t, attrs: newAttrs, group: h.group}
}

// WithGroup returns a new TLogHandler with the provided group name. Grouping is not implemented in this handler,
// but the method is provided to satisfy the slog.Handler interface.
func (h *TLogHandler) WithGroup(name string) slog.Handler {
	return &TLogHandler{t: h.t, attrs: h.attrs, group: name}
}
