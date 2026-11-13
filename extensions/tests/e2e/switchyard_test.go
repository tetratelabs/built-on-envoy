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
	"strconv"
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

// judgeCall is a request the fake System One judge received.
type judgeCall struct {
	auth    string
	body    string
	headers http.Header
}

// fakeJudge is a System One judge that picks the capable candidate for "refactor" prompts, fails
// with a 500 for "boom" prompts, and picks the efficient candidate otherwise.
type fakeJudge struct {
	mu    sync.Mutex
	calls []judgeCall
}

func (j *fakeJudge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	j.mu.Lock()
	j.calls = append(j.calls, judgeCall{auth: r.Header.Get("Authorization"), body: string(raw), headers: r.Header.Clone()})
	j.mu.Unlock()

	var req struct {
		Questions map[string]struct {
			Criteria map[string]any `json:"criteria"`
		} `json:"questions"`
	}
	if r.URL.Path != "/v1/systemone" || json.Unmarshal(raw, &req) != nil || strings.Contains(string(raw), "boom") {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	capable := strings.Contains(string(raw), "refactor")
	answers := map[string]any{}
	for id, q := range req.Questions {
		// The capability classifier asks for "advantage" (capable wins) vs "no_advantage".
		probs := map[string]float64{}
		for option := range q.Criteria {
			probs[option] = 0.01
		}
		choice := "no_advantage"
		if capable {
			choice = "advantage"
		}
		probs[choice] = 0.99
		answers[id] = map[string]any{"type": "choice", "choice": choice, "probabilities": probs}
	}
	w.Header().Set("content-type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"model": "jev-latest", "answers": answers})
}

func (j *fakeJudge) seen(auth string) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, c := range j.calls {
		if c.auth == auth {
			return true
		}
	}
	return false
}

// switchyardJudgeConfig routes "coding-auto" with an llm_classifier backed by a System One judge
// listening on %[1]s. With fail_open false, a judge failure fails the routing, so the virtual
// model's failure_mode applies.
const switchyardJudgeConfig = `{
  "decision_targets": {
    "jev": {"format": "system_one", "model": "jev-latest", "cluster": "%[1]s", "authority": "%[1]s",
            "api_key": {"inline": "test-key"}}
  },
  "virtual_models": [
    {
      "name": "coding-auto",
      "targets": {"efficient": "small-model", "capable": "large-model"},
      "algorithm": {"type": "llm_classifier", "mode": "capability", "classifier_target": "jev",
                    "strong_target": "capable", "weak_target": "efficient",
                    "classify_trigger": "every_request", "fail_open": false,
                    "decision": {"cutoff": 0.05, "candidates": {"candidate_a": "capable", "candidate_b": "efficient"}, "evidence": {}}},
      "default_model": "small-model",
      "failure_mode": "use_default"
    }
  ],
  "model_header": "x-model"
}`

func TestSwitchyardJudge(t *testing.T) {
	internaltesting.SkipIfEnvoyNotSupportedByExtension(t, "switchyard")

	// Let enough for the extension to be compiled and loaded.
	internaltesting.RunEnvoyTimeout.Set(t, 5*time.Minute)

	judge := &fakeJudge{}
	srv := httptest.NewServer(judge) // Listens on 127.0.0.1.
	t.Cleanup(srv.Close)
	judgeAddr := strings.TrimPrefix(srv.URL, "http://")

	ports := internaltesting.FreePorts(t, 2)
	proxyPort, adminPort := ports[0], ports[1]

	internaltesting.RunEnvoy(t, cliBin, proxyPort, adminPort,
		"--log-level", "dynamic_modules:debug",
		"--local", "../../switchyard",
		"--cluster-insecure", judgeAddr,
		"--config", fmt.Sprintf(switchyardJudgeConfig, judgeAddr))

	url := fmt.Sprintf("http://localhost:%d/anything/v1/chat/completions", proxyPort)
	chat := func(prompt string) string {
		return fmt.Sprintf(`{"model":"coding-auto","messages":[{"role":"user","content":%q}]}`, prompt)
	}

	t.Run("complex prompt routes to the capable model", func(t *testing.T) {
		echoed := postEventually(t, url, chat("refactor the payment module into smaller packages"), http.StatusOK)
		require.Equal(t, "large-model", echoed.JSON["model"])
		require.Equal(t, []string{"large-model"}, echoed.Headers["X-Model"])
		require.True(t, judge.seen("Bearer test-key"), "judge did not receive the API key")
	})

	t.Run("trivial prompt routes to the efficient model", func(t *testing.T) {
		echoed := postEventually(t, url, chat("say hello"), http.StatusOK)
		require.Equal(t, "small-model", echoed.JSON["model"])
		require.Equal(t, []string{"small-model"}, echoed.Headers["X-Model"])
	})

	t.Run("judge failure falls back to the default model", func(t *testing.T) {
		echoed := postEventually(t, url, chat("boom"), http.StatusOK)
		require.Equal(t, "small-model", echoed.JSON["model"])
	})
}

// switchyardAlgorithmsConfig has a virtual model per Switchyard algorithm. Every virtual model
// rejects requests it cannot route, so routing failures fail the tests.
//   - Gateway judges and executors are target keys, called through model_calls on the fake model
//     server (%[1]s).
//   - "mini" is a judge_targets judge on the same server, with its own key.
//   - "jev" is a decision_targets judge on the fake System One server (%[2]s), with its own key.
const switchyardAlgorithmsConfig = `{
  "decision_targets": {
    "jev": {"format": "system_one", "model": "jev-latest", "cluster": "%[2]s", "authority": "%[2]s",
            "api_key": {"inline": "test-key"}}
  },
  "judge_targets": {
    "mini": {"format": "openai_chat", "model": "mini-model", "cluster": "%[1]s", "authority": "%[1]s",
             "api_key": {"inline": "judge-key"}}
  },
  "virtual_models": [
    {"name": "passthrough", "targets": {"capable": "large-model"},
     "algorithm": {"type": "passthrough", "target": "capable"}, "failure_mode": "reject"},
    {"name": "random", "targets": {"efficient": "small-model", "capable": "large-model"},
     "algorithm": {"type": "random", "targets": ["efficient", "capable"], "weights": [0.0, 1.0]}, "failure_mode": "reject"},
    {"name": "auto", "targets": {"efficient": "small-model", "capable": "large-model"},
     "algorithm": {"type": "auto", "capable_target": "capable", "efficient_target": "efficient"}, "failure_mode": "reject"},
    {"name": "split", "targets": {"efficient": "small-model", "capable": "large-model"},
     "algorithm": {"type": "random", "targets": ["efficient", "capable"]}, "failure_mode": "reject"},
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
    {"name": "judge-target", "targets": {"efficient": "small-model", "capable": "large-model"},
     "algorithm": {"type": "llm_classifier", "classifier_target": "mini", "strong_target": "capable",
                   "weak_target": "efficient", "base_threshold": 0.5, "fail_open": false}, "failure_mode": "reject"},
    {"name": "decision-judge", "targets": {"efficient": "small-model", "capable": "large-model"},
     "algorithm": {"type": "llm_classifier", "mode": "capability", "classifier_target": "jev",
                   "strong_target": "capable", "weak_target": "efficient",
                   "classify_trigger": "every_request", "fail_open": false,
                   "decision": {"cutoff": 0.05, "candidates": {"candidate_a": "capable", "candidate_b": "efficient"}, "evidence": {}}},
     "failure_mode": "reject"},
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
  "model_calls": {"cluster": "%[1]s", "forward_headers": ["authorization", "x-tenant-id", "x-verdict"]},
  "eligible_models_header": "x-switchyard-eligible-models",
  "routing_headers": true
}`

// fakeModels is a fake OpenAI chat server for the model and judge calls. It answers:
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
		Model string `json:"model"`
	}
	_ = json.Unmarshal(raw, &req)
	f.mu.Lock()
	f.calls = append(f.calls, fakeCall{Model: req.Model, Headers: r.Header.Clone()})
	f.mu.Unlock()

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
	w.Header().Set("content-type", "application/json")
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
func chatBody(model, prompt string, stream bool) string {
	return fmt.Sprintf(`{"model":%q,"stream":%t,"messages":[{"role":"user","content":%q}]}`, model, stream, prompt)
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

// requireRoutingCalls checks that the response reports at least one routing call.
func requireRoutingCalls(t *testing.T, header http.Header) {
	t.Helper()
	calls, err := strconv.Atoi(header.Get("X-Switchyard-Routing-Calls"))
	require.NoError(t, err, "x-switchyard-routing-calls: %q", header.Get("X-Switchyard-Routing-Calls"))
	require.Positive(t, calls)
}

func TestSwitchyardAlgorithms(t *testing.T) {
	internaltesting.SkipIfEnvoyNotSupportedByExtension(t, "switchyard")

	// Let enough for the extension to be compiled and loaded.
	internaltesting.RunEnvoyTimeout.Set(t, 5*time.Minute)

	models := &fakeModels{}
	modelSrv := httptest.NewServer(models) // Listens on 127.0.0.1.
	t.Cleanup(modelSrv.Close)
	modelAddr := strings.TrimPrefix(modelSrv.URL, "http://")

	judge := &fakeJudge{}
	judgeSrv := httptest.NewServer(judge)
	t.Cleanup(judgeSrv.Close)
	judgeAddr := strings.TrimPrefix(judgeSrv.URL, "http://")

	ports := internaltesting.FreePorts(t, 2)
	proxyPort, adminPort := ports[0], ports[1]

	internaltesting.RunEnvoy(t, cliBin, proxyPort, adminPort,
		"--log-level", "dynamic_modules:debug",
		"--local", "../../switchyard",
		"--cluster-insecure", modelAddr,
		"--cluster-insecure", judgeAddr,
		"--config", fmt.Sprintf(switchyardAlgorithmsConfig, modelAddr, judgeAddr))

	url := fmt.Sprintf("http://localhost:%d/anything/v1/chat/completions", proxyPort)
	const prompt = "explain what the parser does"

	// Algorithms that select a model: the echoed request has the selected model.
	routedCases := []struct {
		name    string
		body    string
		headers []string
		model   string
		reason  string
		judged  bool // The algorithm calls a judge, so the response reports routing calls.
	}{
		{"passthrough", chatBody("passthrough", prompt, false), nil, "large-model", "passthrough", false},
		{"random", chatBody("random", prompt, false), nil, "large-model", "random", false},
		{"auto", chatBody("auto", prompt, false), nil, "small-model", "stage_router", false},
		{"stage_router", chatBody("stage-router", prompt, false), nil, "large-model", "stage_router", false},
		// Only the efficient model is eligible, so the even split always picks it.
		{
			"eligible models limit the pick", chatBody("split", prompt, false),
			[]string{"x-switchyard-eligible-models", "small-model"}, "small-model", "random", false,
		},
		// The tool signals are never confident enough, so the judge decides.
		{
			"stage_router with a classifier", chatBody("stage-router-judge", prompt, false), nil,
			"small-model", "stage_router:llm-classifier", true,
		},
		// The first turn is planned on the capable model.
		{"plan_execute", chatBody("plan-execute", prompt, false), nil, "large-model", "plan_execute", false},
		// Within a turn, the judge hands off to the efficient model.
		{
			"plan_execute with a judge", toolTurnBody("plan-execute-judge"),
			[]string{"x-switchyard-session-id", "plan"}, "small-model", "plan_execute", false,
		},
		{
			"llm_classifier with a gateway judge", chatBody("llm-classifier", prompt, false),
			[]string{"x-tenant-id", "acme"}, "small-model", "llm_task_classifier:llm-classifier", true,
		},
		{
			"llm_classifier with a judge target", chatBody("judge-target", prompt, false),
			[]string{"x-tenant-id", "acme"}, "small-model", "llm_task_classifier:llm-classifier", true,
		},
		// fakeJudge gives the capable model the advantage for "refactor" prompts.
		{
			"llm_classifier with a decision judge", chatBody("decision-judge", "refactor the payment module", false),
			[]string{"x-tenant-id", "acme", "authorization", "Bearer client-token"},
			"large-model", "llm_task_classifier:decision_classifier", true,
		},
		{
			"composite", chatBody("composite", prompt, false),
			[]string{"x-switchyard-session-id", "composite"}, "small-model", "composite:llm-classifier", true,
		},
		// The verifiers reject the efficient answer, so the request escalates.
		{"vgr escalates", chatBody("vgr", prompt, false), []string{"x-verdict", "no"}, "large-model", "vgr", true},
	}
	for _, tc := range routedCases {
		t.Run(tc.name, func(t *testing.T) {
			raw, header := postRawEventually(t, url, tc.body, http.StatusOK, tc.headers...)
			var echoed echoedRequest
			require.NoError(t, json.Unmarshal(raw, &echoed), string(raw))
			require.Equal(t, tc.model, echoed.JSON["model"])
			require.Equal(t, []string{tc.model}, echoed.Headers["X-Switchyard-Selected-Model"])
			require.Len(t, echoed.Headers["X-Switchyard-Reason"], 1)
			require.True(t, strings.HasPrefix(echoed.Headers["X-Switchyard-Reason"][0], tc.reason),
				"reason %q", echoed.Headers["X-Switchyard-Reason"])
			if tc.judged {
				requireRoutingCalls(t, header)
			}
		})
	}

	t.Run("gateway calls carry the call headers and forwarded client headers", func(t *testing.T) {
		// A new prompt, so the judge is called for this request.
		echoed := postEventually(t, url, chatBody("llm-classifier", "list the parser's exported functions", false), http.StatusOK,
			"x-tenant-id", "headers-check", "authorization", "Bearer client-token")
		require.Len(t, echoed.Headers["X-Request-Id"], 1)
		requestID := echoed.Headers["X-Request-Id"][0]
		var found bool
		for _, call := range models.callsFor("judge-model") {
			if call.Headers.Get("X-Switchyard-Parent-Id") != requestID {
				continue
			}
			found = true
			require.Equal(t, "judge", call.Headers.Get("X-Switchyard-Call"))
			require.Equal(t, "headers-check", call.Headers.Get("X-Tenant-Id"))
			require.Equal(t, "Bearer client-token", call.Headers.Get("Authorization"))
		}
		require.True(t, found, "no judge call has x-switchyard-parent-id %q", requestID)
	})

	t.Run("judge targets use their own key and never forward client headers", func(t *testing.T) {
		calls := models.callsFor("mini-model")
		require.NotEmpty(t, calls, "the judge target was not called")
		for _, call := range calls {
			require.Equal(t, "judge", call.Headers.Get("X-Switchyard-Call"))
			require.Equal(t, "Bearer judge-key", call.Headers.Get("Authorization"))
			require.Empty(t, call.Headers.Get("X-Tenant-Id"))
			require.NotEmpty(t, call.Headers.Get("X-Switchyard-Parent-Id"))
		}
	})

	t.Run("decision targets use their own key and never forward client headers", func(t *testing.T) {
		require.True(t, judge.seen("Bearer test-key"), "the decision judge did not receive its API key")
		judge.mu.Lock()
		defer judge.mu.Unlock()
		for _, call := range judge.calls {
			require.Equal(t, "Bearer test-key", call.auth)
			require.Equal(t, "decision", call.headers.Get("X-Switchyard-Call"))
			require.Empty(t, call.headers.Get("X-Tenant-Id"))
			require.NotEmpty(t, call.headers.Get("X-Switchyard-Parent-Id"))
		}
	})

	// Algorithms that answer: the filter sends the answer, and the request never reaches httpbin.
	answeredCases := []struct {
		name    string
		body    string
		headers []string
		model   string
		content string
		judged  bool
	}{
		// The verifiers accept the efficient answer.
		{"vgr answers", chatBody("vgr", prompt, false), nil, "small-model", "answer from small-model", true},
		// The executor answers, and the advisor approves it.
		{
			"advisor", chatBody("advisor", prompt, false), []string{"x-switchyard-session-id", "advisor"},
			"small-model", "answer from small-model", true,
		},
		{"noop", chatBody("noop", prompt, false), nil, "noop", "OK", false},
	}
	for _, tc := range answeredCases {
		t.Run(tc.name, func(t *testing.T) {
			raw, header := postRawEventually(t, url, tc.body, http.StatusOK, tc.headers...)
			require.Equal(t, "application/json", header.Get("Content-Type"))
			var answer chatAnswer
			require.NoError(t, json.Unmarshal(raw, &answer), string(raw))
			require.Equal(t, tc.model, answer.Model)
			require.Len(t, answer.Choices, 1)
			require.Equal(t, tc.content, answer.Choices[0].Message.Content)
			if tc.judged {
				requireRoutingCalls(t, header)
			}
		})
	}

	t.Run("model calls are marked as model calls", func(t *testing.T) {
		var marked bool
		for _, call := range models.callsFor("small-model") {
			if call.Headers.Get("X-Switchyard-Call") == "model" {
				marked = true
				require.NotEmpty(t, call.Headers.Get("X-Switchyard-Parent-Id"))
			}
		}
		require.True(t, marked, "no call to the efficient model has x-switchyard-call: model")
	})

	t.Run("answers are streamed when the client asks", func(t *testing.T) {
		raw, header := postRawEventually(t, url, chatBody("advisor", prompt, true), http.StatusOK,
			"x-switchyard-session-id", "advisor-stream")
		require.Equal(t, "text/event-stream", header.Get("Content-Type"))
		require.Contains(t, string(raw), "answer from small-model")
		require.True(t, strings.HasSuffix(string(raw), "data: [DONE]\n\n"), string(raw))
	})
}

// postEventually posts a JSON body, with the given header name and value pairs, until the response
// has the expected status, and returns the echoed request for successful responses.
func postEventually(t *testing.T, url, body string, status int, headers ...string) echoedRequest {
	t.Helper()
	var echoed echoedRequest
	raw, _ := postRawEventually(t, url, body, status, headers...)
	if status == http.StatusOK {
		require.NoError(t, json.Unmarshal(raw, &echoed), string(raw))
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
