// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package impl contains the implementation of the token-usage-metrics filter.
package impl

import (
	"encoding/json"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
)

const defaultMetadataNamespace = "openai"

// pluginConfig holds the configuration for the token-usage-metrics filter.
type pluginConfig struct {
	// MetadataNamespace is the filter metadata namespace to read token counts from.
	// Should match the namespace used by the chat-completions-decoder filter.
	// Defaults to "openai".
	MetadataNamespace string `json:"metadata_namespace"`
}

// pluginConfigFactory implements shared.HttpFilterConfigFactory.
type pluginConfigFactory struct {
	shared.EmptyHttpFilterConfigFactory
}

func (f *pluginConfigFactory) Create(handle shared.HttpFilterConfigHandle, unparsedConfig []byte) (shared.HttpFilterFactory, error) {
	var cfg pluginConfig
	if len(unparsedConfig) > 0 {
		if err := json.Unmarshal(unparsedConfig, &cfg); err != nil {
			handle.Log(shared.LogLevelError, "token-usage-metrics: failed to parse config: %s", err.Error())
			return nil, err
		}
	}
	if cfg.MetadataNamespace == "" {
		cfg.MetadataNamespace = defaultMetadataNamespace
	}

	stats := &metrics{}

	promptCounter, status := handle.DefineCounter("llm_token_count_prompt", "model")
	if status == shared.MetricsSuccess {
		stats.promptTokens = promptCounter
		stats.hasPromptTokens = true
	}

	completionCounter, status := handle.DefineCounter("llm_token_count_completion", "model")
	if status == shared.MetricsSuccess {
		stats.completionTokens = completionCounter
		stats.hasCompletionTokens = true
	}

	totalCounter, status := handle.DefineCounter("llm_token_count_total", "model")
	if status == shared.MetricsSuccess {
		stats.totalTokens = totalCounter
		stats.hasTotalTokens = true
	}

	return &pluginFactory{config: &cfg, metrics: stats}, nil
}

// metrics holds the registered metric IDs for token counting.
type metrics struct {
	promptTokens        shared.MetricID
	hasPromptTokens     bool
	completionTokens    shared.MetricID
	hasCompletionTokens bool
	totalTokens         shared.MetricID
	hasTotalTokens      bool
}

// pluginFactory implements shared.HttpFilterFactory.
type pluginFactory struct {
	shared.EmptyHttpFilterFactory
	config  *pluginConfig
	metrics *metrics
}

func (f *pluginFactory) Create(handle shared.HttpFilterHandle) shared.HttpFilter {
	return &plugin{handle: handle, config: f.config, metrics: f.metrics}
}

// plugin implements shared.HttpFilter.
type plugin struct {
	shared.EmptyHttpFilter
	handle  shared.HttpFilterHandle
	config  *pluginConfig
	metrics *metrics
}

// OnResponseBody records token usage metrics when the response body stream ends.
// The token counts are read from filter metadata populated by the chat-completions-decoder filter.
// This filter must be placed after chat-completions-decoder in the filter chain.
func (p *plugin) OnResponseBody(_ shared.BodyBuffer, endOfStream bool) shared.BodyStatus {
	if endOfStream {
		p.recordTokenMetrics()
	}
	return shared.BodyStatusContinue
}

// OnResponseTrailers records token usage metrics when response trailers arrive.
// This handles the case where the response ends via trailers rather than a final body chunk.
func (p *plugin) OnResponseTrailers(_ shared.HeaderMap) shared.TrailersStatus {
	p.recordTokenMetrics()
	return shared.TrailersStatusContinue
}

// recordTokenMetrics reads token counts from filter metadata and increments the counters.
func (p *plugin) recordTokenMetrics() {
	ns := p.config.MetadataNamespace
	modelName, _ := p.handle.GetMetadataString(shared.MetadataSourceTypeDynamic, ns, "llm.model_name")

	if p.metrics.hasPromptTokens {
		if v, ok := p.handle.GetMetadataNumber(shared.MetadataSourceTypeDynamic, ns, "llm.token_count.prompt"); ok && v > 0 {
			p.handle.IncrementCounterValue(p.metrics.promptTokens, uint64(v), modelName)
		}
	}
	if p.metrics.hasCompletionTokens {
		if v, ok := p.handle.GetMetadataNumber(shared.MetadataSourceTypeDynamic, ns, "llm.token_count.completion"); ok && v > 0 {
			p.handle.IncrementCounterValue(p.metrics.completionTokens, uint64(v), modelName)
		}
	}
	if p.metrics.hasTotalTokens {
		if v, ok := p.handle.GetMetadataNumber(shared.MetadataSourceTypeDynamic, ns, "llm.token_count.total"); ok && v > 0 {
			p.handle.IncrementCounterValue(p.metrics.totalTokens, uint64(v), modelName)
		}
	}
}

// ExtensionName is the name used to refer to this plugin.
const ExtensionName = "token-usage-metrics"

var wellKnownHTTPFilterConfigFactories = map[string]shared.HttpFilterConfigFactory{
	ExtensionName: &pluginConfigFactory{},
}

// WellKnownHttpFilterConfigFactories returns the map of well-known HTTP filter config factories.
func WellKnownHttpFilterConfigFactories() map[string]shared.HttpFilterConfigFactory { //nolint:revive
	return wellKnownHTTPFilterConfigFactories
}
