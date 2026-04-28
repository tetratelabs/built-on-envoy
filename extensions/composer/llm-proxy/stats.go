// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package llmproxy

import (
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
)

// llmProxyStats holds metric definitions for the llm-proxy filter.
// Metrics are defined once at config-factory time and shared across all per-request filter instances.
type llmProxyStats struct {
	// requestTotal counts every successfully-parsed LLM request, tagged by kind and model.
	requestTotal shared.MetricID
	// requestError counts requests whose body could not be parsed, tagged by kind and model.
	requestError shared.MetricID
	// inputTokens accumulates prompt/input token counts from responses, tagged by kind and model.
	inputTokens shared.MetricID
	// outputTokens accumulates completion/output token counts from responses, tagged by kind and model.
	outputTokens shared.MetricID
	// totalTokens accumulates total token counts from responses, tagged by kind and model.
	totalTokens shared.MetricID
	// requestTTFT (Time To First Token) histogram records the milliseconds from request completion to the
	// first SSE body chunk, for streaming responses only, tagged by kind and model.
	requestTTFT shared.MetricID
	// requestTPOT (Time Per Output Token) histogram records the average milliseconds per output token
	// during streaming (stream_duration / output_tokens), tagged by kind and model.
	requestTPOT shared.MetricID
}

// newLLMProxyStats initialises and registers all metrics for the llm-proxy filter.
func newLLMProxyStats(h shared.HttpFilterConfigHandle) *llmProxyStats {
	// The defintions here will never fail.
	requestTotal, _ := h.DefineCounter("llm_proxy_request_total", "kind", "model")
	requestError, _ := h.DefineCounter("llm_proxy_request_error", "kind", "model")
	inputTokens, _ := h.DefineCounter("llm_proxy_input_tokens", "kind", "model")
	outputTokens, _ := h.DefineCounter("llm_proxy_output_tokens", "kind", "model")
	totalTokens, _ := h.DefineCounter("llm_proxy_total_tokens", "kind", "model")
	requestTTFT, _ := h.DefineHistogram("llm_proxy_request_ttft", "kind", "model")
	requestTPOT, _ := h.DefineHistogram("llm_proxy_request_tpot", "kind", "model")

	return &llmProxyStats{
		requestTotal: requestTotal,
		requestError: requestError,
		inputTokens:  inputTokens,
		outputTokens: outputTokens,
		totalTokens:  totalTokens,
		requestTTFT:  requestTTFT,
		requestTPOT:  requestTPOT,
	}
}
