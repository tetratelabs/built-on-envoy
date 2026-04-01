// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// mockExecutor implements executorRunner for tests.
type mockExecutor struct {
	runCalled bool
	stopErr   error
}

func (m *mockExecutor) RunStreaming(_ context.Context, _ []ExtensionConfig, _ http.ResponseWriter, _ http.Flusher) {
	m.runCalled = true
}

func (m *mockExecutor) Stop() error {
	return m.stopErr
}

// newTestServer creates a Server with a mock executor for testing.
func newTestServer(exec executorRunner) *Server {
	s := &Server{
		mux:      http.NewServeMux(),
		logger:   nil,
		executor: exec,
	}
	s.routes()
	return s
}

func TestHandleGetExtensions(t *testing.T) {
	s := newTestServer(&mockExecutor{})
	req := httptest.NewRequest(http.MethodGet, "/api/extensions", nil)
	w := httptest.NewRecorder()

	s.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var result interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
}

func TestHandleGetSchema_NotFound(t *testing.T) {
	s := newTestServer(&mockExecutor{})
	req := httptest.NewRequest(http.MethodGet, "/api/extensions/nonexistent-xyz/schema", nil)
	w := httptest.NewRecorder()

	s.ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleRun_InvalidBody(t *testing.T) {
	s := newTestServer(&mockExecutor{})
	req := httptest.NewRequest(http.MethodPost, "/api/run", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleRun_EmptyExtensions(t *testing.T) {
	s := newTestServer(&mockExecutor{})
	body := `{"extensions":[]}`
	req := httptest.NewRequest(http.MethodPost, "/api/run", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

// flushingRecorder wraps httptest.ResponseRecorder and implements http.Flusher.
type flushingRecorder struct {
	*httptest.ResponseRecorder
}

func (f *flushingRecorder) Flush() {}

func TestHandleRun_OK(t *testing.T) {
	mock := &mockExecutor{}
	s := newTestServer(mock)
	body := `{"extensions":[{"name":"opa","config":""}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/run", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := &flushingRecorder{httptest.NewRecorder()}

	s.ServeHTTP(w, req)

	require.True(t, mock.runCalled, "RunStreaming should have been called")
	require.Equal(t, "text/event-stream", w.Header().Get("Content-Type"))
}

func TestHandleStop_NoProcess(t *testing.T) {
	mock := &mockExecutor{stopErr: fmt.Errorf("no running process")}
	s := newTestServer(mock)
	req := httptest.NewRequest(http.MethodPost, "/api/stop", nil)
	w := httptest.NewRecorder()

	s.ServeHTTP(w, req)

	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleStop_OK(t *testing.T) {
	mock := &mockExecutor{stopErr: nil}
	s := newTestServer(mock)
	req := httptest.NewRequest(http.MethodPost, "/api/stop", nil)
	w := httptest.NewRecorder()

	s.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var result map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	require.Equal(t, "stopped", result["status"])
}

func TestNewServer(t *testing.T) {
	s := NewServer(slog.Default(), RunParams{})
	require.NotNil(t, s)
	require.NotNil(t, s.mux)
	require.NotNil(t, s.executor)
}

func TestHandleGetSchema_Found(t *testing.T) {
	s := newTestServer(&mockExecutor{})
	req := httptest.NewRequest(http.MethodGet, "/api/extensions/cedar/schema", nil)
	w := httptest.NewRecorder()

	s.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "application/json", w.Header().Get("Content-Type"))
	var v interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&v))
}

// nonFlushingWriter implements http.ResponseWriter but not http.Flusher.
type nonFlushingWriter struct {
	headers http.Header
	code    int
	body    bytes.Buffer
}

func newNonFlushingWriter() *nonFlushingWriter {
	return &nonFlushingWriter{headers: make(http.Header)}
}

func (n *nonFlushingWriter) Header() http.Header         { return n.headers }
func (n *nonFlushingWriter) WriteHeader(code int)        { n.code = code }
func (n *nonFlushingWriter) Write(b []byte) (int, error) { return n.body.Write(b) }

func TestHandleRun_StreamingNotSupported(t *testing.T) {
	s := newTestServer(&mockExecutor{})
	body := `{"extensions":[{"name":"opa","config":""}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/run", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := newNonFlushingWriter()

	s.mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusInternalServerError, w.code)
}
