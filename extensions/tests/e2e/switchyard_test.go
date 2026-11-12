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
	"net/http/httptest"
	"strings"
	"sync"
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
	internaltesting.SkipIfEnvoyNotSupportedByExtension(t, "switchyard")

	// Let enough for the extension to be compiled and loaded.
	internaltesting.RunEnvoyTimeout.Set(t, 5*time.Minute)

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

// switchyardDecisionOnlyConfig resolves "coding-auto" to "large-model", but keeps the virtual
// model in the body and only carries the decision in the headers.
const switchyardDecisionOnlyConfig = `{
  "virtual_models": [
    {
      "name": "coding-auto",
      "targets": {"efficient": "small-model", "capable": "large-model"},
      "algorithm": {"type": "random", "targets": ["efficient", "capable"], "weights": [0.0, 1.0]}
    }
  ],
  "model_header": "x-model",
  "rewrite_body": false
}`

func TestSwitchyardDecisionOnly(t *testing.T) {
	internaltesting.SkipIfEnvoyNotSupportedByExtension(t, "switchyard")

	// Let enough for the extension to be compiled and loaded.
	internaltesting.RunEnvoyTimeout.Set(t, 5*time.Minute)

	ports := internaltesting.FreePorts(t, 2)
	proxyPort, adminPort := ports[0], ports[1]

	internaltesting.RunEnvoy(t, cliBin, proxyPort, adminPort,
		"--log-level", "dynamic_modules:debug",
		"--local", "../../switchyard",
		"--config", switchyardDecisionOnlyConfig)

	url := fmt.Sprintf("http://localhost:%d/anything/v1/chat/completions", proxyPort)

	body := `{"model":"coding-auto","messages":[{"role":"user","content":"hi"}],"temperature":0.5}`
	echoed := postEventually(t, url, body, http.StatusOK)
	// The body keeps the virtual model.
	require.Equal(t, "coding-auto", echoed.JSON["model"])
	require.InDelta(t, 0.5, echoed.JSON["temperature"], 0)
	// The decision is carried in the headers.
	require.Equal(t, []string{"large-model"}, echoed.Headers["X-Model"])
	require.Equal(t, []string{"coding-auto"}, echoed.Headers["X-Switchyard-Virtual-Model"])
	require.Equal(t, []string{"large-model"}, echoed.Headers["X-Switchyard-Selected-Model"])
	require.NotEmpty(t, echoed.Headers["X-Switchyard-Reason"])
}

// switchyardAlgorithmsConfig has a virtual model per Switchyard algorithm. The algorithms that call
// models call the fake model server, registered as an additional cluster named after its address
// (%[1]s). Every virtual model rejects requests it cannot route, so routing failures fail the tests.
const switchyardAlgorithmsConfig = `{
  "virtual_models": [
    {"name": "passthrough", "targets": {"capable": "large-model"},
     "algorithm": {"type": "passthrough", "target": "capable"}, "failure_mode": "reject"},
    {"name": "random", "targets": {"efficient": "small-model", "capable": "large-model"},
     "algorithm": {"type": "random", "targets": ["efficient", "capable"], "weights": [0.0, 1.0]}, "failure_mode": "reject"},
    {"name": "auto", "targets": {"efficient": "small-model", "capable": "large-model"},
     "algorithm": {"type": "auto", "capable_target": "capable", "efficient_target": "efficient"}, "failure_mode": "reject"},
    {"name": "stage-router", "targets": {"efficient": "small-model", "capable": "large-model"},
     "algorithm": {"type": "stage_router", "capable_target": "capable", "efficient_target": "efficient",
                   "picker": "capable_first", "confidence_threshold": 0.5}, "failure_mode": "reject"},
    {"name": "stage-router-judge", "targets": {"efficient": "small-model", "capable": "large-model", "judge": "judge-model"},
     "algorithm": {"type": "stage_router", "capable_target": "capable", "efficient_target": "efficient",
                   "picker": "capable_first", "confidence_threshold": 1.0,
                   "classifier": {"target": "judge", "base_threshold": 0.5}}, "failure_mode": "reject"},
    {"name": "plan-execute", "targets": {"efficient": "small-model", "capable": "large-model"},
     "algorithm": {"type": "plan_execute", "capable_target": "capable", "efficient_target": "efficient"}, "failure_mode": "reject"},
    {"name": "plan-execute-judge", "targets": {"efficient": "small-model", "capable": "large-model", "judge": "judge-model"},
     "algorithm": {"type": "plan_execute", "capable_target": "capable", "efficient_target": "efficient",
                   "execution_trigger": "judge", "judge": {"target": "judge", "base_threshold": 0.5}}, "failure_mode": "reject"},
    {"name": "llm-classifier", "targets": {"efficient": "small-model", "capable": "large-model", "judge": "judge-model"},
     "algorithm": {"type": "llm_classifier", "classifier_target": "judge", "strong_target": "capable",
                   "weak_target": "efficient", "base_threshold": 0.5, "fail_open": false}, "failure_mode": "reject"},
    {"name": "decision-judge", "targets": {"efficient": "small-model", "capable": "large-model", "judge": "jev"},
     "algorithm": {"type": "llm_classifier", "classifier_target": "judge", "strong_target": "capable",
                   "weak_target": "efficient", "fail_open": false,
                   "decision": {"cutoff": 0.5, "candidates": {"capable_first": "capable", "efficient_first": "efficient"},
                                "evidence": {"description": "Compare the two models"}}}, "failure_mode": "reject"},
    {"name": "composite", "targets": {"efficient": "small-model", "capable": "large-model", "judge": "judge-model"},
     "algorithm": {"type": "composite", "classifier": {"target": "judge", "base_threshold": 0.5, "classify_trigger": "user_turn"},
                   "stage": {"capable_target": "capable", "efficient_target": "efficient"}}, "failure_mode": "reject"},
    {"name": "vgr", "targets": {"efficient": "small-model", "capable": "large-model", "judge": "judge-model"},
     "algorithm": {"type": "vgr", "efficient_target": "efficient", "capable_target": "capable",
                   "judge_target": "judge", "capable_judge_target": "judge",
                   "mode": "active", "active_approval": "prospective-validation-and-canary-approved"}, "failure_mode": "reject"},
    {"name": "advisor", "targets": {"executor": "small-model", "advisor": "judge-model"},
     "algorithm": {"type": "advisor", "executor_target": "executor", "advisor_target": "advisor"}, "failure_mode": "reject"},
    {"name": "noop", "targets": {"any": "small-model"}, "algorithm": {"type": "noop"}, "failure_mode": "reject"}
  ],
  "model_calls": {"cluster": "%[1]s", "forward_headers": ["x-tenant-id", "x-verdict"]},
  "decision_calls": {"cluster": "%[1]s", "path": "/v1/systemone", "format": "system_one"},
  "routing_headers": true
}`

// fakeModels is a fake model server for the calls made by the algorithms. It answers:
//   - System One decision calls: the capable model has the advantage.
//   - Capability judges: the efficient model can solve the task.
//   - vgr's task typing: the task wants an answer.
//   - vgr's verifiers: yes, unless the client sent "x-verdict: no".
//   - The advisor's review: approve.
//   - Other chat completions: "answer from <model>".
type fakeModels struct {
	mu    sync.Mutex
	calls []fakeCall
}

type fakeCall struct {
	Model   string
	Headers http.Header
}

func (f *fakeModels) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var req struct {
		Model     string                     `json:"model"`
		Questions map[string]json.RawMessage `json:"questions"`
	}
	_ = json.Unmarshal(raw, &req)
	f.mu.Lock()
	f.calls = append(f.calls, fakeCall{Model: req.Model, Headers: r.Header.Clone()})
	f.mu.Unlock()
	w.Header().Set("content-type", "application/json")

	if r.URL.Path == "/v1/systemone" {
		answers := map[string]any{}
		for id := range req.Questions {
			answers[id] = map[string]any{
				"type": "choice", "choice": "advantage",
				"probabilities": map[string]float64{"advantage": 0.9, "no_advantage": 0.1},
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": req.Model, "answers": answers,
			"usage": map[string]any{"input_tokens": 50, "output_tokens": 1},
		})
		return
	}

	body := string(raw)
	content := "answer from " + req.Model
	switch {
	case strings.Contains(body, "CapabilityClassifierDecision"):
		content = `{"crux":"c","primary_rule":"SUP-1","capability_boundary":"supported","p_solve":0.99}`
	case strings.Contains(body, "You classify a user request"):
		content = "answer"
	case strings.Contains(body, "strict answer verifier"), strings.Contains(body, "strict verifier"):
		content = "yes"
		if r.Header.Get("X-Verdict") == "no" {
			content = "no"
		}
	case strings.Contains(body, "senior reviewer"):
		content = "APPROVE"
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id": "c1", "object": "chat.completion", "model": req.Model,
		"choices": []any{map[string]any{
			"index": 0, "finish_reason": "stop",
			"message": map[string]any{"role": "assistant", "content": content},
		}},
		"usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 5, "total_tokens": 105},
	})
}

func (f *fakeModels) callsFor(model string) []fakeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var calls []fakeCall
	for _, c := range f.calls {
		if c.Model == model {
			calls = append(calls, c)
		}
	}
	return calls
}

// chatBody is a chat completions request for a virtual model.
func chatBody(model string, stream bool) string {
	return fmt.Sprintf(`{"model":%q,"stream":%t,"messages":[{"role":"user","content":"explain what the parser does"}]}`, model, stream)
}

// toolTurnBody is a chat completions request in the middle of an agent task: the agent read a file.
func toolTurnBody(model string) string {
	return fmt.Sprintf(`{"model":%q,"messages":[
		{"role":"user","content":"fix the parser"},
		{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"parser.go\"}"}}]},
		{"role":"tool","tool_call_id":"call_1","content":"package parser"}]}`, model)
}

// chatAnswer is the part of a chat completions response the tests check.
type chatAnswer struct {
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func TestSwitchyardAlgorithms(t *testing.T) {
	internaltesting.SkipIfEnvoyNotSupportedByExtension(t, "switchyard")

	// Let enough for the extension to be compiled and loaded.
	internaltesting.RunEnvoyTimeout.Set(t, 5*time.Minute)

	models := &fakeModels{}
	server := httptest.NewServer(models)
	t.Cleanup(server.Close)
	cluster := server.Listener.Addr().String()

	ports := internaltesting.FreePorts(t, 2)
	proxyPort, adminPort := ports[0], ports[1]

	internaltesting.RunEnvoy(t, cliBin, proxyPort, adminPort,
		"--log-level", "dynamic_modules:debug",
		"--cluster-insecure", cluster,
		"--local", "../../switchyard",
		"--config", fmt.Sprintf(switchyardAlgorithmsConfig, cluster))

	url := fmt.Sprintf("http://localhost:%d/anything/v1/chat/completions", proxyPort)

	// Algorithms that select a model: the echoed request has the selected model.
	routed := []struct {
		name    string
		body    string
		headers []string
		model   string
		reason  string
	}{
		{"passthrough", chatBody("passthrough", false), nil, "large-model", "passthrough"},
		{"random", chatBody("random", false), nil, "large-model", "random"},
		{"auto", chatBody("auto", false), nil, "small-model", "stage_router"},
		{"stage_router", chatBody("stage-router", false), nil, "large-model", "stage_router"},
		// The tool signals are never confident enough, so the judge decides.
		{"stage_router with a classifier", chatBody("stage-router-judge", false), nil, "small-model", "stage_router:llm-classifier"},
		// The first turn is planned on the capable model.
		{"plan_execute", chatBody("plan-execute", false), nil, "large-model", "plan_execute"},
		// Within a turn, the judge hands off to the efficient model.
		{"plan_execute with a judge", toolTurnBody("plan-execute-judge"), []string{"x-switchyard-session-id", "plan"}, "small-model", "plan_execute"},
		{"llm_classifier", chatBody("llm-classifier", false), []string{"x-tenant-id", "acme"}, "small-model", "llm_task_classifier:llm-classifier"},
		{"llm_classifier with a decision judge", chatBody("decision-judge", false), nil, "large-model", "llm_task_classifier:decision_classifier"},
		{"composite", chatBody("composite", false), []string{"x-switchyard-session-id", "composite"}, "small-model", "composite:llm-classifier"},
		// The verifiers reject the efficient answer, so the request escalates.
		{"vgr escalates", chatBody("vgr", false), []string{"x-verdict", "no"}, "large-model", "vgr"},
	}
	for _, tc := range routed {
		t.Run(tc.name, func(t *testing.T) {
			echoed := postEventually(t, url, tc.body, http.StatusOK, tc.headers...)
			require.Equal(t, tc.model, echoed.JSON["model"])
			require.Equal(t, []string{tc.model}, echoed.Headers["X-Switchyard-Selected-Model"])
			require.Len(t, echoed.Headers["X-Switchyard-Reason"], 1)
			require.True(t, strings.HasPrefix(echoed.Headers["X-Switchyard-Reason"][0], tc.reason),
				"reason %q", echoed.Headers["X-Switchyard-Reason"])
		})
	}

	t.Run("judge calls carry the client headers", func(t *testing.T) {
		var judged bool
		for _, call := range models.callsFor("judge-model") {
			if call.Headers.Get("X-Tenant-Id") == "acme" {
				judged = true
				require.Equal(t, "judge", call.Headers.Get("X-Switchyard-Call"))
				require.Empty(t, call.Headers.Get("X-Switchyard-Virtual-Model"))
			}
		}
		require.True(t, judged, "the judge was not called with the client headers")
		require.NotEmpty(t, models.callsFor("jev"), "the decision judge was not called")
	})

	// Algorithms that answer: the filter sends the answer, and the request never reaches httpbin.
	answered := []struct {
		name    string
		body    string
		headers []string
		model   string
		content string
		calls   string
	}{
		// The verifiers accept the efficient answer: typing, and two verifier calls.
		{"vgr answers", chatBody("vgr", false), nil, "small-model", "answer from small-model", "4"},
		// The executor answers, and the advisor approves it.
		{"advisor", chatBody("advisor", false), []string{"x-switchyard-session-id", "advisor"}, "small-model", "answer from small-model", "2"},
		{"noop", chatBody("noop", false), nil, "noop", "OK", ""},
	}
	for _, tc := range answered {
		t.Run(tc.name, func(t *testing.T) {
			raw, header := postRawEventually(t, url, tc.body, http.StatusOK, tc.headers...)
			var answer chatAnswer
			require.NoError(t, json.Unmarshal(raw, &answer), string(raw))
			require.Equal(t, tc.model, answer.Model)
			require.Len(t, answer.Choices, 1)
			require.Equal(t, tc.content, answer.Choices[0].Message.Content)
			require.Equal(t, tc.calls, header.Get("X-Switchyard-Routing-Calls"))
		})
	}

	t.Run("answers are streamed when the client asks", func(t *testing.T) {
		raw, header := postRawEventually(t, url, chatBody("advisor", true), http.StatusOK,
			"x-switchyard-session-id", "advisor-stream")
		require.Equal(t, "text/event-stream", header.Get("Content-Type"))
		require.Contains(t, string(raw), "answer from small-model")
		require.True(t, strings.HasSuffix(string(raw), "data: [DONE]\n\n"), string(raw))
	})
}

// postEventually posts a JSON body until the response has the expected status, and returns the
// echoed request for successful responses.
func postEventually(t *testing.T, url, body string, status int, headers ...string) echoedRequest {
	t.Helper()
	var echoed echoedRequest
	raw, _ := postRawEventually(t, url, body, status, headers...)
	if status == http.StatusOK {
		require.NoError(t, json.Unmarshal(raw, &echoed))
	}
	return echoed
}

// postRawEventually posts a JSON body, with the given header name and value pairs, until the
// response has the expected status, and returns the response body and headers.
func postRawEventually(t *testing.T, url, body string, status int, headers ...string) ([]byte, http.Header) {
	t.Helper()
	var (
		raw    []byte
		header http.Header
	)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, strings.NewReader(body))
		require.NoError(c, err)
		req.Header.Set("content-type", "application/json")
		for i := 0; i+1 < len(headers); i += 2 {
			req.Header.Set(headers[i], headers[i+1])
		}
		resp, err := http.DefaultClient.Do(req)
		require.NoError(c, err)
		defer resp.Body.Close() // nolint:errcheck
		raw, err = io.ReadAll(resp.Body)
		require.NoError(c, err)
		require.Equal(c, status, resp.StatusCode, string(raw))
		header = resp.Header
	}, time.Minute, 200*time.Millisecond)
	return raw, header
}
