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
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internaltesting "github.com/tetratelabs/built-on-envoy/internal/testing"
)

func TestMCPOpenAPI(t *testing.T) {
	internaltesting.SkipIfEnvoyNotSupportedByExtension(t, "composer/mcp-openapi")

	extensionPath, err := filepath.Abs("../../composer/mcp-openapi")
	require.NoError(t, err)
	specFile, err := filepath.Abs("testdata/mcp-openapi/httpbin.yaml")
	require.NoError(t, err)

	// The extension writes the generated ECDS files to the Envoy working directory,
	// so run Envoy from a temporary directory.
	t.Chdir(t.TempDir())

	ports := internaltesting.FreePorts(t, 2)
	proxyPort, adminPort := ports[0], ports[1]

	internaltesting.RunEnvoy(t, cliBin, proxyPort, adminPort,
		"--log-level", "dynamic_modules:debug",
		"--local", extensionPath,
		"--config", fmt.Sprintf(`{"spec": {"file": %q}}`, specFile))

	mcpURL := fmt.Sprintf("http://localhost:%d/mcp", proxyPort)

	// The bridge and tools/list configs are loaded asynchronously via ECDS, so wait until
	// the generated tools are served before running the rest of the tests.
	t.Run("tools_list", func(t *testing.T) {
		requireEventuallyMCP(t, mcpURL, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, func(c *assert.CollectT, res mcpResponse) {
			var result struct {
				Tools []struct {
					Name        string         `json:"name"`
					Description string         `json:"description"`
					InputSchema map[string]any `json:"inputSchema"`
				} `json:"tools"`
			}
			require.NoError(c, json.Unmarshal(res.Result, &result))
			names := make([]string, 0, len(result.Tools))
			for _, tool := range result.Tools {
				names = append(names, tool.Name)
			}
			require.Equal(c, []string{"delete_anything_items_itemId", "getAnything", "getStatus", "postAnything"}, names)
			assert.Equal(c, "Echo request details for a resource", result.Tools[1].Description)
			assert.Equal(c, "object", result.Tools[1].InputSchema["type"])
			assert.Equal(c, []any{"id"}, result.Tools[1].InputSchema["required"])
		})
	})

	t.Run("tools_list_string_id", func(t *testing.T) {
		requireEventuallyMCP(t, mcpURL, `{"jsonrpc":"2.0","id":"abc","method":"tools/list"}`, func(c *assert.CollectT, res mcpResponse) {
			assert.JSONEq(c, `"abc"`, string(res.ID))
		})
	})

	t.Run("initialize", func(t *testing.T) {
		requireEventuallyMCP(t, mcpURL, `{"jsonrpc":"2.0","id":2,"method":"initialize","params":{
			"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"e2e","version":"1.0"}}}`,
			func(c *assert.CollectT, res mcpResponse) {
				var result struct {
					Capabilities map[string]any `json:"capabilities"`
				}
				require.NoError(c, json.Unmarshal(res.Result, &result))
				assert.Contains(c, result.Capabilities, "tools")
			})
	})

	t.Run("tools_call_get_path_and_query", func(t *testing.T) {
		requireEventuallyMCP(t, mcpURL, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{
			"name":"getAnything","arguments":{"id":"42","q":"hello"}}}`,
			func(c *assert.CollectT, res mcpResponse) {
				echo := toolCallEcho(c, res)
				assert.Equal(c, http.MethodGet, echo.Method)
				assert.True(c, strings.HasSuffix(echo.URL, "/anything/42?q=hello"), "unexpected URL: %s", echo.URL)
			})
	})

	t.Run("tools_call_post_body", func(t *testing.T) {
		requireEventuallyMCP(t, mcpURL, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{
			"name":"postAnything","arguments":{"hello":"world"}}}`,
			func(c *assert.CollectT, res mcpResponse) {
				echo := toolCallEcho(c, res)
				assert.Equal(c, http.MethodPost, echo.Method)
				assert.Equal(c, map[string]any{"hello": "world"}, echo.JSON)
			})
	})

	t.Run("tools_call_typed_arguments", func(t *testing.T) {
		requireEventuallyMCP(t, mcpURL, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{
			"name":"getAnything","arguments":{"id":"7","limit":10,"verbose":true}}}`,
			func(c *assert.CollectT, res mcpResponse) {
				echo := toolCallEcho(c, res)
				assert.Equal(c, map[string][]string{"limit": {"10"}, "verbose": {"true"}}, echo.Args)
			})
		requireEventuallyMCP(t, mcpURL, `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{
			"name":"postAnything","arguments":{"count":3,"ratio":0.5,"enabled":false,"tags":["a","b"],"nested":{"k":"v"}}}}`,
			func(c *assert.CollectT, res mcpResponse) {
				echo := toolCallEcho(c, res)
				assert.Equal(c, map[string]any{
					"count": float64(3), "ratio": 0.5, "enabled": false,
					"tags": []any{"a", "b"}, "nested": map[string]any{"k": "v"},
				}, echo.JSON)
			})
	})

	t.Run("tools_call_without_operation_id", func(t *testing.T) {
		requireEventuallyMCP(t, mcpURL, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{
			"name":"delete_anything_items_itemId","arguments":{"itemId":42}}}`,
			func(c *assert.CollectT, res mcpResponse) {
				echo := toolCallEcho(c, res)
				assert.Equal(c, http.MethodDelete, echo.Method)
				assert.True(c, strings.HasSuffix(echo.URL, "/anything/items/42"), "unexpected URL: %s", echo.URL)
			})
	})

	t.Run("tools_call_trace_context", func(t *testing.T) {
		const traceparent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
		requireEventuallyMCP(t, mcpURL, `{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{
			"name":"getAnything","arguments":{"id":"trace"},"_meta":{"traceparent":"`+traceparent+`"}}}`,
			func(c *assert.CollectT, res mcpResponse) {
				echo := toolCallEcho(c, res)
				assert.Equal(c, []string{traceparent}, echo.Headers["Traceparent"])
			})
	})

	// Use the official MCP Go SDK to check that a spec-compliant client can talk to the endpoint.
	t.Run("mcp_sdk_client", func(t *testing.T) {
		client := mcp.NewClient(&mcp.Implementation{Name: "boe-e2e", Version: "1.0"}, nil)
		session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{
			Endpoint: mcpURL,
			// The extension only serves MCP POST requests, so don't open the server-to-client SSE stream.
			DisableStandaloneSSE: true,
		}, nil)
		require.NoError(t, err)
		t.Cleanup(func() { _ = session.Close() })

		require.NotNil(t, session.InitializeResult().Capabilities.Tools)

		tools, err := session.ListTools(t.Context(), nil)
		require.NoError(t, err)
		descriptions := map[string]string{}
		for _, tool := range tools.Tools {
			descriptions[tool.Name] = tool.Description
		}
		require.Equal(t, map[string]string{
			"delete_anything_items_itemId": "Echo request details for a deleted item",
			"getAnything":                  "Echo request details for a resource",
			"getStatus":                    "Return the given status code with an empty body",
			"postAnything":                 "Echo request details",
		}, descriptions)

		res, err := session.CallTool(t.Context(), &mcp.CallToolParams{
			Name:      "getAnything",
			Arguments: map[string]any{"id": "sdk", "limit": 3},
		})
		require.NoError(t, err)
		require.False(t, res.IsError)
		require.Len(t, res.Content, 1)
		text, ok := res.Content[0].(*mcp.TextContent)
		require.True(t, ok, "unexpected content type %T", res.Content[0])
		var echo httpbinEcho
		require.NoError(t, json.Unmarshal([]byte(text.Text), &echo))
		require.Equal(t, http.MethodGet, echo.Method)
		require.True(t, strings.HasSuffix(echo.URL, "/anything/sdk?limit=3"), "unexpected URL: %s", echo.URL)

		res, err = session.CallTool(t.Context(), &mcp.CallToolParams{
			Name:      "postAnything",
			Arguments: map[string]any{"hello": "sdk"},
		})
		require.NoError(t, err)
		require.False(t, res.IsError)
		text, ok = res.Content[0].(*mcp.TextContent)
		require.True(t, ok, "unexpected content type %T", res.Content[0])
		require.NoError(t, json.Unmarshal([]byte(text.Text), &echo))
		require.Equal(t, map[string]any{"hello": "sdk"}, echo.JSON)
	})

	// Envoy versions before 1.40 don't support upstream responses without a body in the bridge.
	t.Run("tools_call_headers_only_response", func(t *testing.T) {
		internaltesting.SkipIfEnvoyVersionUnsupported(t, "composer/mcp-openapi", internaltesting.EnvoyConstraints{MinEnvoyVersion: "1.40.0"})
		requireEventuallyToolResult(t, mcpURL, 9, http.StatusNoContent, false)
	})

	t.Run("tools_call_headers_only_error_response", func(t *testing.T) {
		internaltesting.SkipIfEnvoyVersionUnsupported(t, "composer/mcp-openapi", internaltesting.EnvoyConstraints{MinEnvoyVersion: "1.40.0"})
		requireEventuallyToolResult(t, mcpURL, 10, http.StatusInternalServerError, true)
	})

	t.Run("non_mcp_request_rejected", func(t *testing.T) {
		internaltesting.RequireEventuallyGet(t, fmt.Sprintf("http://localhost:%d/anything", proxyPort),
			func(r *http.Response) bool { return r.StatusCode >= 400 })
	})
}

// requireEventuallyToolResult calls the getStatus tool with the given status code and checks that
// the result has an empty text content and the expected isError value.
func requireEventuallyToolResult(t *testing.T, url string, id, code int, isError bool) {
	t.Helper()
	body := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{
		"name":"getStatus","arguments":{"code":%d}}}`, id, code)
	requireEventuallyMCP(t, url, body, func(c *assert.CollectT, res mcpResponse) {
		var result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		require.NoError(c, json.Unmarshal(res.Result, &result))
		assert.Equal(c, isError, result.IsError, "result: %s", res.Result)
		require.Len(c, result.Content, 1, "result: %s", res.Result)
		assert.Empty(c, result.Content[0].Text)
	})
}

// mcpResponse is a JSON-RPC response from the MCP endpoint.
type mcpResponse struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

// httpbinEcho is the subset of the httpbin /anything response checked by the tests.
type httpbinEcho struct {
	Method  string              `json:"method"`
	URL     string              `json:"url"`
	Args    map[string][]string `json:"args"`
	Headers map[string][]string `json:"headers"`
	JSON    any                 `json:"json"`
}

// requireEventuallyMCP sends the given JSON-RPC request to the MCP endpoint until the
// response is successful and satisfies the given condition.
func requireEventuallyMCP(t *testing.T, url, body string, condition func(c *assert.CollectT, res mcpResponse)) {
	t.Helper()
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, strings.NewReader(body))
		require.NoError(c, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")

		resp, err := (&http.Client{Timeout: internaltesting.TestRequestTimeout.Get()}).Do(req)
		require.NoError(c, err)
		defer func() { _ = resp.Body.Close() }()
		raw, err := io.ReadAll(resp.Body)
		require.NoError(c, err)
		require.Equal(c, http.StatusOK, resp.StatusCode, "body: %s", raw)

		var res mcpResponse
		require.NoError(c, json.Unmarshal(raw, &res), "body: %s", raw)
		require.Empty(c, res.Error, "body: %s", raw)
		condition(c, res)
	}, time.Minute, 200*time.Millisecond)
}

// toolCallEcho extracts the httpbin echo response from the text content of a tools/call result.
func toolCallEcho(c *assert.CollectT, res mcpResponse) httpbinEcho {
	var result struct {
		IsError bool `json:"isError"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	require.NoError(c, json.Unmarshal(res.Result, &result))
	require.False(c, result.IsError, "result: %s", res.Result)
	require.Len(c, result.Content, 1, "result: %s", res.Result)
	require.Equal(c, "text", result.Content[0].Type)

	var echo httpbinEcho
	require.NoError(c, json.Unmarshal([]byte(result.Content[0].Text), &echo), "text: %s", result.Content[0].Text)
	return echo
}
