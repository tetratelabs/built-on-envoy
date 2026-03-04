// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package impl

import (
	"testing"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/fake"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// testBodyBuffer is a test implementation of shared.BodyBuffer.
type testBodyBuffer struct {
	body []byte
}

func newTestBodyBuffer(data []byte) *testBodyBuffer {
	b := make([]byte, len(data))
	copy(b, data)
	return &testBodyBuffer{body: b}
}

func (b *testBodyBuffer) GetChunks() [][]byte { return [][]byte{b.body} }
func (b *testBodyBuffer) GetSize() uint64     { return uint64(len(b.body)) }
func (b *testBodyBuffer) Drain(size uint64) {
	if size >= uint64(len(b.body)) {
		b.body = nil
		return
	}
	b.body = b.body[size:]
}
func (b *testBodyBuffer) Append(data []byte) { b.body = append(b.body, data...) }

// defaultCfg returns a config with the default namespace set (simulates what Create does).
func defaultCfg() *chatCompletionsDecoderConfig {
	return &chatCompletionsDecoderConfig{MetadataNamespace: defaultMetadataNamespace}
}

// --- Tests for decoderConfigFactory.Create ---

func TestDecoderConfigFactory_Create_EmptyConfig(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockConfigHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)

	factory := &decoderConfigFactory{}
	filterFactory, err := factory.Create(mockConfigHandle, []byte{})
	require.NoError(t, err)
	require.NotNil(t, filterFactory)

	ff, ok := filterFactory.(*decoderFilterFactory)
	require.True(t, ok)
	require.Equal(t, defaultMetadataNamespace, ff.config.MetadataNamespace)
}

func TestDecoderConfigFactory_Create_NilConfig(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockConfigHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)

	factory := &decoderConfigFactory{}
	filterFactory, err := factory.Create(mockConfigHandle, nil)
	require.NoError(t, err)
	require.NotNil(t, filterFactory)

	ff, ok := filterFactory.(*decoderFilterFactory)
	require.True(t, ok)
	require.Equal(t, defaultMetadataNamespace, ff.config.MetadataNamespace)
}

func TestDecoderConfigFactory_Create_WithNamespace(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockConfigHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)

	cfg := `{"metadata_namespace": "custom-ns"}`
	factory := &decoderConfigFactory{}
	filterFactory, err := factory.Create(mockConfigHandle, []byte(cfg))
	require.NoError(t, err)

	ff, ok := filterFactory.(*decoderFilterFactory)
	require.True(t, ok)
	require.Equal(t, "custom-ns", ff.config.MetadataNamespace)
}

func TestDecoderConfigFactory_Create_InvalidJSON(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockConfigHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockConfigHandle.EXPECT().Log(shared.LogLevelError, gomock.Any(), gomock.Any()).Times(1)

	factory := &decoderConfigFactory{}
	filterFactory, err := factory.Create(mockConfigHandle, []byte(`{invalid json}`))
	require.Error(t, err)
	require.Nil(t, filterFactory)
}

// --- Tests for decoderFilterFactory.Create ---

func TestDecoderFilterFactory_Create(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	cfg := defaultCfg()
	factory := &decoderFilterFactory{config: cfg}
	filter := factory.Create(mockHandle)

	require.NotNil(t, filter)
	df, ok := filter.(*decoderFilter)
	require.True(t, ok)
	require.Equal(t, mockHandle, df.handle)
	require.Equal(t, cfg, df.config)
}

// --- Tests for WellKnownHttpFilterConfigFactories ---

func TestWellKnownHttpFilterConfigFactories(t *testing.T) {
	factories := WellKnownHttpFilterConfigFactories()
	require.NotNil(t, factories)
	require.Len(t, factories, 1)
	require.Contains(t, factories, "chat-completions-decoder")

	_, ok := factories["chat-completions-decoder"].(*decoderConfigFactory)
	require.True(t, ok)
}

// --- Tests for decoderFilter.OnRequestHeaders ---

func TestOnRequestHeaders_EndOfStream(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	filter := &decoderFilter{handle: mockHandle, config: defaultCfg()}
	result := filter.OnRequestHeaders(fake.NewFakeHeaderMap(map[string][]string{}), true)
	require.Equal(t, shared.HeadersStatusContinue, result)
}

func TestOnRequestHeaders_NotEndOfStream(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	filter := &decoderFilter{handle: mockHandle, config: defaultCfg()}
	result := filter.OnRequestHeaders(fake.NewFakeHeaderMap(map[string][]string{}), false)
	require.Equal(t, shared.HeadersStatusStop, result)
}

// --- Tests for decoderFilter.OnRequestBody ---

func TestOnRequestBody_NotEndOfStream(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	filter := &decoderFilter{handle: mockHandle, config: defaultCfg()}
	result := filter.OnRequestBody(newTestBodyBuffer([]byte("data")), false)
	require.Equal(t, shared.BodyStatusStopAndBuffer, result)
}

func TestOnRequestBody_EmptyBody(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	mockHandle.EXPECT().BufferedRequestBody().Return(newTestBodyBuffer([]byte{})).AnyTimes()
	mockHandle.EXPECT().ReceivedRequestBody().Return(nil).AnyTimes()

	filter := &decoderFilter{handle: mockHandle, config: defaultCfg()}
	result := filter.OnRequestBody(newTestBodyBuffer([]byte{}), true)
	require.Equal(t, shared.BodyStatusContinue, result)
}

func TestOnRequestBody_InvalidJSON(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	body := []byte(`{invalid json}`)
	mockHandle.EXPECT().BufferedRequestBody().Return(newTestBodyBuffer(body)).AnyTimes()
	mockHandle.EXPECT().ReceivedRequestBody().Return(nil).AnyTimes()
	mockHandle.EXPECT().Log(shared.LogLevelDebug, gomock.Any(), gomock.Any()).Times(1)

	filter := &decoderFilter{handle: mockHandle, config: defaultCfg()}
	result := filter.OnRequestBody(newTestBodyBuffer(body), true)
	// Even with invalid JSON, we continue (graceful degradation)
	require.Equal(t, shared.BodyStatusContinue, result)
}

func TestOnRequestBody_ValidRequest_SetsMetadata(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	body := []byte(`{
		"model": "gpt-4o",
		"messages": [
			{"role": "system", "content": "You are a helpful assistant."},
			{"role": "user", "content": "What is the weather?"}
		]
	}`)
	mockHandle.EXPECT().BufferedRequestBody().Return(newTestBodyBuffer(body)).AnyTimes()
	mockHandle.EXPECT().ReceivedRequestBody().Return(nil).AnyTimes()

	mockHandle.EXPECT().SetMetadata("openai", "llm.model_name", "gpt-4o").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.system", "openai").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.input_messages.count", 2).Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.input_messages.0.message.role", "system").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.input_messages.0.message.content", "You are a helpful assistant.").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.input_messages.1.message.role", "user").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.input_messages.1.message.content", "What is the weather?").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.tools.count", 0).Times(1)

	filter := &decoderFilter{handle: mockHandle, config: defaultCfg()}
	result := filter.OnRequestBody(newTestBodyBuffer(body), true)
	require.Equal(t, shared.BodyStatusContinue, result)
}

func TestOnRequestBody_WithTools_SetsMetadata(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	body := []byte(`{
		"model": "gpt-4o",
		"messages": [{"role": "user", "content": "Call a tool"}],
		"tools": [
			{"type": "function", "function": {"name": "my_func", "description": "A function"}}
		]
	}`)
	mockHandle.EXPECT().BufferedRequestBody().Return(newTestBodyBuffer(body)).AnyTimes()
	mockHandle.EXPECT().ReceivedRequestBody().Return(nil).AnyTimes()

	mockHandle.EXPECT().SetMetadata("openai", "llm.model_name", "gpt-4o").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.system", "openai").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.input_messages.count", 1).Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.input_messages.0.message.role", "user").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.input_messages.0.message.content", "Call a tool").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.tools.count", 1).Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.tools.0.tool.json_schema",
		`{"type":"function","function":{"name":"my_func","description":"A function"}}`).Times(1)

	filter := &decoderFilter{handle: mockHandle, config: defaultCfg()}
	result := filter.OnRequestBody(newTestBodyBuffer(body), true)
	require.Equal(t, shared.BodyStatusContinue, result)
}

func TestOnRequestBody_WithToolCalls_SetsMetadata(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	body := []byte(`{
		"model": "gpt-4o",
		"messages": [
			{"role": "user", "content": "What is the weather?"},
			{"role": "assistant", "content": null, "tool_calls": [
				{"id": "call_1", "type": "function", "function": {"name": "get_weather", "arguments": "{}"}}
			]}
		]
	}`)
	mockHandle.EXPECT().BufferedRequestBody().Return(newTestBodyBuffer(body)).AnyTimes()
	mockHandle.EXPECT().ReceivedRequestBody().Return(nil).AnyTimes()

	mockHandle.EXPECT().SetMetadata("openai", "llm.model_name", "gpt-4o").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.system", "openai").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.input_messages.count", 2).Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.input_messages.0.message.role", "user").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.input_messages.0.message.content", "What is the weather?").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.input_messages.1.message.role", "assistant").Times(1)
	// assistant message has null content, so no content key is set
	mockHandle.EXPECT().SetMetadata("openai", "llm.input_messages.1.message.tool_calls.count", 1).Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.input_messages.1.message.tool_calls.0.tool_call.id", "call_1").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.input_messages.1.message.tool_calls.0.tool_call.function.name", "get_weather").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.input_messages.1.message.tool_calls.0.tool_call.function.arguments", "{}").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.tools.count", 0).Times(1)

	filter := &decoderFilter{handle: mockHandle, config: defaultCfg()}
	result := filter.OnRequestBody(newTestBodyBuffer(body), true)
	require.Equal(t, shared.BodyStatusContinue, result)
}

func TestOnRequestBody_CustomNamespace(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	body := []byte(`{
		"model": "gpt-4o",
		"messages": [{"role": "user", "content": "Hello"}]
	}`)
	mockHandle.EXPECT().BufferedRequestBody().Return(newTestBodyBuffer(body)).AnyTimes()
	mockHandle.EXPECT().ReceivedRequestBody().Return(nil).AnyTimes()

	mockHandle.EXPECT().SetMetadata("my-namespace", "llm.model_name", "gpt-4o").Times(1)
	mockHandle.EXPECT().SetMetadata("my-namespace", "llm.system", "openai").Times(1)
	mockHandle.EXPECT().SetMetadata("my-namespace", "llm.input_messages.count", 1).Times(1)
	mockHandle.EXPECT().SetMetadata("my-namespace", "llm.input_messages.0.message.role", "user").Times(1)
	mockHandle.EXPECT().SetMetadata("my-namespace", "llm.input_messages.0.message.content", "Hello").Times(1)
	mockHandle.EXPECT().SetMetadata("my-namespace", "llm.tools.count", 0).Times(1)

	filter := &decoderFilter{
		handle: mockHandle,
		config: &chatCompletionsDecoderConfig{MetadataNamespace: "my-namespace"},
	}
	result := filter.OnRequestBody(newTestBodyBuffer(body), true)
	require.Equal(t, shared.BodyStatusContinue, result)
}

// --- Tests for decoderFilter.OnRequestTrailers ---

func TestOnRequestTrailers_SetsMetadata(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	body := []byte(`{
		"model": "gpt-4o",
		"messages": [{"role": "user", "content": "Hello"}]
	}`)
	mockHandle.EXPECT().BufferedRequestBody().Return(newTestBodyBuffer(body)).AnyTimes()
	mockHandle.EXPECT().ReceivedRequestBody().Return(nil).AnyTimes()

	mockHandle.EXPECT().SetMetadata("openai", "llm.model_name", "gpt-4o").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.system", "openai").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.input_messages.count", 1).Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.input_messages.0.message.role", "user").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.input_messages.0.message.content", "Hello").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.tools.count", 0).Times(1)

	filter := &decoderFilter{handle: mockHandle, config: defaultCfg()}
	result := filter.OnRequestTrailers(fake.NewFakeHeaderMap(map[string][]string{}))
	require.Equal(t, shared.TrailersStatusContinue, result)
}

// --- Tests for decoderFilter.OnResponseHeaders ---

func TestOnResponseHeaders_EndOfStream(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	filter := &decoderFilter{handle: mockHandle, config: defaultCfg()}
	result := filter.OnResponseHeaders(fake.NewFakeHeaderMap(map[string][]string{}), true)
	require.Equal(t, shared.HeadersStatusContinue, result)
}

func TestOnResponseHeaders_NotEndOfStream(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	filter := &decoderFilter{handle: mockHandle, config: defaultCfg()}
	result := filter.OnResponseHeaders(fake.NewFakeHeaderMap(map[string][]string{}), false)
	require.Equal(t, shared.HeadersStatusStop, result)
}

// --- Tests for decoderFilter.OnResponseBody ---

func TestOnResponseBody_NotEndOfStream(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	filter := &decoderFilter{handle: mockHandle, config: defaultCfg()}
	result := filter.OnResponseBody(newTestBodyBuffer([]byte("data")), false)
	require.Equal(t, shared.BodyStatusStopAndBuffer, result)
}

func TestOnResponseBody_EmptyBody(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	mockHandle.EXPECT().BufferedResponseBody().Return(newTestBodyBuffer([]byte{})).AnyTimes()
	mockHandle.EXPECT().ReceivedResponseBody().Return(nil).AnyTimes()

	filter := &decoderFilter{handle: mockHandle, config: defaultCfg()}
	result := filter.OnResponseBody(newTestBodyBuffer([]byte{}), true)
	require.Equal(t, shared.BodyStatusContinue, result)
}

func TestOnResponseBody_InvalidJSON(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	body := []byte(`{invalid json}`)
	mockHandle.EXPECT().BufferedResponseBody().Return(newTestBodyBuffer(body)).AnyTimes()
	mockHandle.EXPECT().ReceivedResponseBody().Return(nil).AnyTimes()
	mockHandle.EXPECT().Log(shared.LogLevelDebug, gomock.Any(), gomock.Any()).Times(1)

	filter := &decoderFilter{handle: mockHandle, config: defaultCfg()}
	result := filter.OnResponseBody(newTestBodyBuffer(body), true)
	require.Equal(t, shared.BodyStatusContinue, result)
}

func TestOnResponseBody_ValidResponse_SetsMetadata(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	body := []byte(`{
		"choices": [
			{
				"index": 0,
				"message": {"role": "assistant", "content": "The weather is sunny."},
				"finish_reason": "stop"
			}
		],
		"usage": {
			"prompt_tokens": 20,
			"completion_tokens": 10,
			"total_tokens": 30
		}
	}`)
	mockHandle.EXPECT().BufferedResponseBody().Return(newTestBodyBuffer(body)).AnyTimes()
	mockHandle.EXPECT().ReceivedResponseBody().Return(nil).AnyTimes()

	mockHandle.EXPECT().SetMetadata("openai", "llm.output_messages.count", 1).Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.output_messages.0.message.role", "assistant").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.output_messages.0.message.content", "The weather is sunny.").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.token_count.prompt", 20).Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.token_count.completion", 10).Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.token_count.total", 30).Times(1)

	filter := &decoderFilter{handle: mockHandle, config: defaultCfg()}
	result := filter.OnResponseBody(newTestBodyBuffer(body), true)
	require.Equal(t, shared.BodyStatusContinue, result)
}

func TestOnResponseBody_NoUsage_SetsMetadataWithoutTokenCounts(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	body := []byte(`{
		"choices": [
			{"index": 0, "message": {"role": "assistant", "content": "Hello!"}, "finish_reason": "stop"}
		]
	}`)
	mockHandle.EXPECT().BufferedResponseBody().Return(newTestBodyBuffer(body)).AnyTimes()
	mockHandle.EXPECT().ReceivedResponseBody().Return(nil).AnyTimes()

	mockHandle.EXPECT().SetMetadata("openai", "llm.output_messages.count", 1).Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.output_messages.0.message.role", "assistant").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.output_messages.0.message.content", "Hello!").Times(1)
	// No token count calls expected when usage is absent

	filter := &decoderFilter{handle: mockHandle, config: defaultCfg()}
	result := filter.OnResponseBody(newTestBodyBuffer(body), true)
	require.Equal(t, shared.BodyStatusContinue, result)
}

func TestOnResponseBody_WithCompletionTokensDetails(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	body := []byte(`{
		"choices": [
			{"index": 0, "message": {"role": "assistant", "content": "Done."}, "finish_reason": "stop"}
		],
		"usage": {
			"prompt_tokens": 10,
			"completion_tokens": 50,
			"total_tokens": 60,
			"completion_tokens_details": {"reasoning_tokens": 40, "audio_tokens": 10}
		}
	}`)
	mockHandle.EXPECT().BufferedResponseBody().Return(newTestBodyBuffer(body)).AnyTimes()
	mockHandle.EXPECT().ReceivedResponseBody().Return(nil).AnyTimes()

	mockHandle.EXPECT().SetMetadata("openai", "llm.output_messages.count", 1).Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.output_messages.0.message.role", "assistant").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.output_messages.0.message.content", "Done.").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.token_count.prompt", 10).Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.token_count.completion", 50).Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.token_count.total", 60).Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.token_count.completion_details.reasoning", 40).Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.token_count.completion_details.audio", 10).Times(1)

	filter := &decoderFilter{handle: mockHandle, config: defaultCfg()}
	result := filter.OnResponseBody(newTestBodyBuffer(body), true)
	require.Equal(t, shared.BodyStatusContinue, result)
}

func TestOnResponseBody_WithOutputToolCalls_SetsMetadata(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	body := []byte(`{
		"choices": [
			{
				"index": 0,
				"message": {
					"role": "assistant",
					"content": null,
					"tool_calls": [
						{"id": "call_abc", "type": "function", "function": {"name": "get_weather", "arguments": "{\"loc\":\"NYC\"}"}}
					]
				},
				"finish_reason": "tool_calls"
			}
		]
	}`)
	mockHandle.EXPECT().BufferedResponseBody().Return(newTestBodyBuffer(body)).AnyTimes()
	mockHandle.EXPECT().ReceivedResponseBody().Return(nil).AnyTimes()

	mockHandle.EXPECT().SetMetadata("openai", "llm.output_messages.count", 1).Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.output_messages.0.message.role", "assistant").Times(1)
	// null content: no content key
	mockHandle.EXPECT().SetMetadata("openai", "llm.output_messages.0.message.tool_calls.count", 1).Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.output_messages.0.message.tool_calls.0.tool_call.id", "call_abc").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.output_messages.0.message.tool_calls.0.tool_call.function.name", "get_weather").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.output_messages.0.message.tool_calls.0.tool_call.function.arguments", `{"loc":"NYC"}`).Times(1)

	filter := &decoderFilter{handle: mockHandle, config: defaultCfg()}
	result := filter.OnResponseBody(newTestBodyBuffer(body), true)
	require.Equal(t, shared.BodyStatusContinue, result)
}

func TestOnResponseBody_CustomNamespace(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	body := []byte(`{
		"choices": [
			{"index": 0, "message": {"role": "assistant", "content": "Hi"}, "finish_reason": "stop"}
		]
	}`)
	mockHandle.EXPECT().BufferedResponseBody().Return(newTestBodyBuffer(body)).AnyTimes()
	mockHandle.EXPECT().ReceivedResponseBody().Return(nil).AnyTimes()

	mockHandle.EXPECT().SetMetadata("my-ns", "llm.output_messages.count", 1).Times(1)
	mockHandle.EXPECT().SetMetadata("my-ns", "llm.output_messages.0.message.role", "assistant").Times(1)
	mockHandle.EXPECT().SetMetadata("my-ns", "llm.output_messages.0.message.content", "Hi").Times(1)

	filter := &decoderFilter{
		handle: mockHandle,
		config: &chatCompletionsDecoderConfig{MetadataNamespace: "my-ns"},
	}
	result := filter.OnResponseBody(newTestBodyBuffer(body), true)
	require.Equal(t, shared.BodyStatusContinue, result)
}

// --- Tests for decoderFilter.OnResponseTrailers ---

func TestOnResponseTrailers_SetsMetadata(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	body := []byte(`{
		"choices": [
			{"index": 0, "message": {"role": "assistant", "content": "Done."}, "finish_reason": "stop"}
		],
		"usage": {"prompt_tokens": 5, "completion_tokens": 3, "total_tokens": 8}
	}`)
	mockHandle.EXPECT().BufferedResponseBody().Return(newTestBodyBuffer(body)).AnyTimes()
	mockHandle.EXPECT().ReceivedResponseBody().Return(nil).AnyTimes()

	mockHandle.EXPECT().SetMetadata("openai", "llm.output_messages.count", 1).Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.output_messages.0.message.role", "assistant").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.output_messages.0.message.content", "Done.").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.token_count.prompt", 5).Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.token_count.completion", 3).Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "llm.token_count.total", 8).Times(1)

	filter := &decoderFilter{handle: mockHandle, config: defaultCfg()}
	result := filter.OnResponseTrailers(fake.NewFakeHeaderMap(map[string][]string{}))
	require.Equal(t, shared.TrailersStatusContinue, result)
}
