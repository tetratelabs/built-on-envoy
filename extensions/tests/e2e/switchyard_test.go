// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internaltesting "github.com/tetratelabs/built-on-envoy/internal/testing"
)

// switchyardConfig resolves "coding-auto" to "large-model" (a 0/1 weighted random split), and
// rejects requests to "strict-auto" that cannot be routed.
const switchyardConfig = `{
  "virtual_models": [
    {
      "name": "coding-auto",
      "targets": {"efficient": "small-model", "capable": "large-model"},
      "algorithm": {"type": "random", "targets": ["efficient", "capable"], "weights": [0.0, 1.0]}
    },
    {
      "name": "strict-auto",
      "targets": {"only": "small-model"},
      "algorithm": {"type": "passthrough", "target": "only"},
      "failure_mode": "reject"
    }
  ],
  "model_header": "x-model",
  "clear_route_cache": true
}`

// echoedRequest is the part of the httpbin /anything response the tests check.
type echoedRequest struct {
	Headers map[string][]string `json:"headers"`
	JSON    map[string]any      `json:"json"`
}

func TestSwitchyard(t *testing.T) {
	ports := internaltesting.FreePorts(t, 2)
	proxyPort, adminPort := ports[0], ports[1]

	internaltesting.RunEnvoy(t, cliBin, proxyPort, adminPort,
		"--log-level", "dynamic_modules:debug",
		"--local", "../../switchyard",
		"--config", switchyardConfig)

	// httpbin echoes the request, and the path ends with an endpoint the filter handles.
	url := fmt.Sprintf("http://localhost:%d/anything/v1/chat/completions", proxyPort)

	t.Run("virtual model is rewritten", func(t *testing.T) {
		body := `{"model":"coding-auto","messages":[{"role":"user","content":"hi"}],"temperature":0.5}`
		echoed := postEventually(t, url, body, http.StatusOK)
		require.Equal(t, "large-model", echoed.JSON["model"])
		require.InDelta(t, 0.5, echoed.JSON["temperature"], 0)
		require.Equal(t, []string{"large-model"}, echoed.Headers["X-Model"])
		require.Equal(t, []string{"coding-auto"}, echoed.Headers["X-Switchyard-Virtual-Model"])
		require.Equal(t, []string{"large-model"}, echoed.Headers["X-Switchyard-Selected-Model"])
		require.NotEmpty(t, echoed.Headers["X-Switchyard-Reason"])
	})

	t.Run("other models are unchanged", func(t *testing.T) {
		body := `{"model":"gpt-5","messages":[{"role":"user","content":"hi"}]}`
		echoed := postEventually(t, url, body, http.StatusOK)
		require.Equal(t, "gpt-5", echoed.JSON["model"])
		require.NotContains(t, echoed.Headers, "X-Switchyard-Selected-Model")
	})

	t.Run("routing failures are rejected", func(t *testing.T) {
		body := `{"model":"strict-auto","messages":"not-a-list"}`
		postEventually(t, url, body, http.StatusServiceUnavailable)
	})
}

// postEventually posts a JSON body until the response has the expected status, and returns the
// echoed request for successful responses.
func postEventually(t *testing.T, url, body string, status int) echoedRequest {
	t.Helper()
	var echoed echoedRequest
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, strings.NewReader(body))
		require.NoError(c, err)
		req.Header.Set("content-type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(c, err)
		defer resp.Body.Close() // nolint:errcheck
		raw, err := io.ReadAll(resp.Body)
		require.NoError(c, err)
		require.Equal(c, status, resp.StatusCode, string(raw))
		if status == http.StatusOK {
			require.NoError(c, json.Unmarshal(raw, &echoed))
		}
	}, time.Minute, 200*time.Millisecond)
	return echoed
}
