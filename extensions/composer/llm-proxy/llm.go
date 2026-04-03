// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package llmproxy implements an HTTP filter that identifies LLM API requests
// and extracts model, stream, and token-usage information into filter metadata.
package llmproxy

import (
	"fmt"

	anthropicpkg "github.com/tetratelabs/built-on-envoy/extensions/composer/llm-proxy/anthropic"
	llm "github.com/tetratelabs/built-on-envoy/extensions/composer/llm-proxy/llm"
	openaipkg "github.com/tetratelabs/built-on-envoy/extensions/composer/llm-proxy/openai"
)

// Type aliases so plugin.go and stats.go need no changes.
type (
	LLMRequest       = llm.LLMRequest
	LLMResponse      = llm.LLMResponse
	LLMResponseChunk = llm.LLMResponseChunk
	SSEParser        = llm.SSEParser
	LLMFactory       = llm.LLMFactory
	LLMUsage         = llm.LLMUsage
	LLMMessage       = llm.LLMMessage
	LLMContentPart   = llm.LLMContentPart
	LLMTool          = llm.LLMTool
	LLMToolCall      = llm.LLMToolCall
	LLMToolChoice    = llm.LLMToolChoice
	LLMContentAudio  = llm.LLMContentAudio
	LLMContentImage  = llm.LLMContentImage
	LLMContentFile   = llm.LLMContentFile
)

const (
	// KindOpenAI matches the OpenAI Chat Completions API.
	KindOpenAI = llm.KindOpenAI
	// KindAnthropic matches the Anthropic Messages API.
	KindAnthropic = llm.KindAnthropic
	// KindCustom matches a custom OpenAI-compatible API.
	KindCustom = llm.KindCustom
)

// Message role constants.
const (
	RoleSystem    = llm.RoleSystem
	RoleUser      = llm.RoleUser
	RoleAssistant = llm.RoleAssistant
	RoleTool      = llm.RoleTool
)

// Tool type constant.
const ToolTypeFunction = llm.ToolTypeFunction

// Tool choice type constants.
const (
	ToolChoiceAuto     = llm.ToolChoiceAuto
	ToolChoiceNone     = llm.ToolChoiceNone
	ToolChoiceRequired = llm.ToolChoiceRequired
	ToolChoiceFunction = llm.ToolChoiceFunction
)

// Canonical finish reason constants (OpenAI-compatible values used throughout).
const (
	FinishReasonStop      = llm.FinishReasonStop
	FinishReasonLength    = llm.FinishReasonLength
	FinishReasonToolCalls = llm.FinishReasonToolCalls
)

// Content part type constants.
const (
	ContentPartTypeText    = llm.ContentPartTypeText
	ContentPartTypeRefusal = llm.ContentPartTypeRefusal
	ContentPartTypeImage   = llm.ContentPartTypeImage
	ContentPartTypeAudio   = llm.ContentPartTypeAudio
	ContentPartTypeFile    = llm.ContentPartTypeFile
)

// FactoryForKind returns the LLMFactory for the given kind string.
// Supported kinds: KindOpenAI, KindAnthropic, KindCustom.
// KindCustom reuses the OpenAI factory since custom providers are OpenAI-compatible.
func FactoryForKind(kind string) (LLMFactory, error) {
	switch kind {
	case KindOpenAI, KindCustom:
		return openaipkg.NewFactory(), nil
	case KindAnthropic:
		return anthropicpkg.NewFactory(), nil
	default:
		return nil, fmt.Errorf("llm-proxy: unsupported target kind %q", kind)
	}
}

// TransformLLMRequestTo converts any LLMRequest into a new LLMRequest backed by
// the concrete implementation for the given API kind.
func TransformLLMRequestTo(req LLMRequest, kind string) (LLMRequest, error) {
	f, err := FactoryForKind(kind)
	if err != nil {
		return nil, err
	}
	return f.TransformRequest(req)
}

// TransformLLMResponseTo converts any LLMResponse into a new LLMResponse backed by
// the concrete implementation for the given API kind.
func TransformLLMResponseTo(resp LLMResponse, kind string) (LLMResponse, error) {
	f, err := FactoryForKind(kind)
	if err != nil {
		return nil, err
	}
	return f.TransformResponse(resp)
}

// TransformLLMResponseChunkTo converts any LLMResponseChunk into a new
// LLMResponseChunk backed by the concrete implementation for the given API kind.
func TransformLLMResponseChunkTo(chunk LLMResponseChunk, kind string) (LLMResponseChunk, error) {
	f, err := FactoryForKind(kind)
	if err != nil {
		return nil, err
	}
	return f.TransformChunk(chunk)
}
