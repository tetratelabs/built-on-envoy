// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package llmproxy

// customFactory implements LLMFactory for custom OpenAI-compatible APIs.
// It reuses the OpenAI request/response structure.
// TODO(wbpcode): to support custom format in the future, we can add optional fields to
// the factory config to specify how to extract model/stream/usage info.
type customFactory struct{}

func (f *customFactory) ParseRequest(body []byte) (LLMRequest, error) {
	return parseOpenAIRequest(body)
}

func (f *customFactory) ParseResponse(body []byte) (LLMResponse, error) {
	return parseOpenAIResponse(body)
}
func (f *customFactory) NewSSEParser() SSEParser { return newOpenAISSEParser() }
