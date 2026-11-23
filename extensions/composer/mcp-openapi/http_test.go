// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package mcpopenapi

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const testUserAgent = "built-on-envoy-mcp-openapi/test"

func TestHttpGet_AddsUserAgent(t *testing.T) {
	actualUserAgent := ""
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actualUserAgent = r.Header.Get(userAgentHeader)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	res, err := httpGet(t.Context(), ts.Client(), ts.URL, testUserAgent)
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()

	require.Equal(t, http.StatusOK, res.StatusCode)
	require.Equal(t, testUserAgent, actualUserAgent)
}

func TestHttpGet_RetryDecisions(t *testing.T) {
	tests := []struct {
		name             string
		ctx              func(*testing.T) context.Context
		dialErr          error
		expectedErr      string
		expectedDials    int
		expectedRequests int
	}{
		{
			name: "retries net error",
			ctx: func(t *testing.T) context.Context {
				t.Helper()
				return t.Context()
			},
			dialErr:          netError{err: errors.New("connection refused")},
			expectedDials:    2,
			expectedRequests: 1,
		},
		{
			name: "does not retry non-net error",
			ctx: func(t *testing.T) context.Context {
				t.Helper()
				return t.Context()
			},
			dialErr:          errors.New("connection refused"),
			expectedErr:      `Get "$URL": connection refused`,
			expectedDials:    1,
			expectedRequests: 0,
		},
		{
			name: "does not dial canceled context",
			ctx: func(t *testing.T) context.Context {
				t.Helper()
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				return ctx
			},
			expectedErr:      `Get "$URL": context canceled`,
			expectedDials:    0,
			expectedRequests: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requests := 0
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests++
				w.WriteHeader(http.StatusOK)
			}))
			defer ts.Close()

			dials := 0
			client := ts.Client()
			transport := client.Transport.(*http.Transport)
			dialContext := transport.DialContext
			transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				dials++
				if dials == 1 && tt.dialErr != nil {
					return nil, tt.dialErr
				}
				return dialContext(ctx, network, addr)
			}

			res, err := httpGet(tt.ctx(t), client, ts.URL, testUserAgent)
			if tt.expectedErr != "" {
				expectedErr := strings.ReplaceAll(tt.expectedErr, "$URL", ts.URL)
				require.EqualError(t, err, expectedErr)
			} else {
				require.NoError(t, err)
				defer func() { _ = res.Body.Close() }()
				require.Equal(t, http.StatusOK, res.StatusCode)
			}

			require.Equal(t, tt.expectedDials, dials)
			require.Equal(t, tt.expectedRequests, requests)
		})
	}
}

var _ net.Error = netError{}

type netError struct {
	err error
}

func (e netError) Error() string { return e.err.Error() }
func (e netError) Unwrap() error { return e.err }
func (netError) Timeout() bool   { return false }
func (netError) Temporary() bool { return false }
