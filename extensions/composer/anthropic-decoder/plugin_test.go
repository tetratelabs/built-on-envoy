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
	"github.com/tetratelabs/built-on-envoy/extensions/composer/pkg"
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

func (b *testBodyBuffer) GetChunks() []shared.UnsafeEnvoyBuffer {
	return []shared.UnsafeEnvoyBuffer{pkg.UnsafeBufferFromBytes(b.body)}
}
func (b *testBodyBuffer) GetSize() uint64 { return uint64(len(b.body)) }
func (b *testBodyBuffer) Drain(size uint64) {
	if size >= uint64(len(b.body)) {
		b.body = nil
		return
	}
	b.body = b.body[size:]
}
func (b *testBodyBuffer) Append(data []byte) { b.body = append(b.body, data...) }

// defaultCfg returns a config with the default namespace set (simulates what Create does).
func defaultCfg() *anthropicDecoderConfig {
	return &anthropicDecoderConfig{MetadataNamespace: defaultMetadataNamespace}
}

// --- Tests for decoderConfigFactory.Create ---

func TestDecoderConfigFactory_Create_EmptyConfig(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockConfigHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockConfigHandle.EXPECT().Log(shared.LogLevelInfo, gomock.Any(), gomock.Any()).Times(1)

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
	mockConfigHandle.EXPECT().Log(shared.LogLevelInfo, gomock.Any(), gomock.Any()).Times(1)

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
	mockConfigHandle.EXPECT().Log(shared.LogLevelInfo, gomock.Any(), gomock.Any()).Times(1)

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
	require.Contains(t, factories, "anthropic-messages-decoder")

	_, ok := factories["anthropic-messages-decoder"].(*decoderConfigFactory)
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

func TestOnRequestBody_ValidRequest_WithSystem_SetsMetadata(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	body := []byte(`{
		"model": "claude-sonnet-4-20250514",
		"system": "You are a helpful assistant.",
		"messages": [
			{"role": "user", "content": "What is the weather?"}
		]
	}`)
	mockHandle.EXPECT().BufferedRequestBody().Return(newTestBodyBuffer(body)).AnyTimes()
	mockHandle.EXPECT().ReceivedRequestBody().Return(nil).AnyTimes()

	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.model_name", "claude-sonnet-4-20250514").Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.system", "anthropic").Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.input_messages.count", 2).Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.input_messages.0.message.role", "system").Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.input_messages.0.message.content", "You are a helpful assistant.").Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.input_messages.1.message.role", "user").Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.input_messages.1.message.content", "What is the weather?").Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.tools.count", 0).Times(1)

	filter := &decoderFilter{handle: mockHandle, config: defaultCfg()}
	result := filter.OnRequestBody(newTestBodyBuffer(body), true)
	require.Equal(t, shared.BodyStatusContinue, result)
}

func TestOnRequestBody_ValidRequest_NoSystem_SetsMetadata(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	body := []byte(`{
		"model": "claude-sonnet-4-20250514",
		"messages": [
			{"role": "user", "content": "Hello"}
		]
	}`)
	mockHandle.EXPECT().BufferedRequestBody().Return(newTestBodyBuffer(body)).AnyTimes()
	mockHandle.EXPECT().ReceivedRequestBody().Return(nil).AnyTimes()

	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.model_name", "claude-sonnet-4-20250514").Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.system", "anthropic").Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.input_messages.count", 1).Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.input_messages.0.message.role", "user").Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.input_messages.0.message.content", "Hello").Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.tools.count", 0).Times(1)

	filter := &decoderFilter{handle: mockHandle, config: defaultCfg()}
	result := filter.OnRequestBody(newTestBodyBuffer(body), true)
	require.Equal(t, shared.BodyStatusContinue, result)
}

func TestOnRequestBody_WithTools_SetsMetadata(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	body := []byte(`{
		"model": "claude-sonnet-4-20250514",
		"messages": [{"role": "user", "content": "Call a tool"}],
		"tools": [
			{"name": "my_func", "description": "A function", "input_schema": {"type": "object"}}
		]
	}`)
	mockHandle.EXPECT().BufferedRequestBody().Return(newTestBodyBuffer(body)).AnyTimes()
	mockHandle.EXPECT().ReceivedRequestBody().Return(nil).AnyTimes()

	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.model_name", "claude-sonnet-4-20250514").Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.system", "anthropic").Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.input_messages.count", 1).Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.input_messages.0.message.role", "user").Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.input_messages.0.message.content", "Call a tool").Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.tools.count", 1).Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.tools.0.tool.json_schema",
		`{"name":"my_func","description":"A function","input_schema":{"type":"object"}}`).Times(1)

	filter := &decoderFilter{handle: mockHandle, config: defaultCfg()}
	result := filter.OnRequestBody(newTestBodyBuffer(body), true)
	require.Equal(t, shared.BodyStatusContinue, result)
}

func TestOnRequestBody_WithToolUse_SetsMetadata(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	body := []byte(`{
		"model": "claude-sonnet-4-20250514",
		"messages": [
			{"role": "user", "content": "What is the weather?"},
			{"role": "assistant", "content": [
				{"type": "tool_use", "id": "toolu_1", "name": "get_weather", "input": {}}
			]}
		]
	}`)
	mockHandle.EXPECT().BufferedRequestBody().Return(newTestBodyBuffer(body)).AnyTimes()
	mockHandle.EXPECT().ReceivedRequestBody().Return(nil).AnyTimes()

	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.model_name", "claude-sonnet-4-20250514").Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.system", "anthropic").Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.input_messages.count", 2).Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.input_messages.0.message.role", "user").Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.input_messages.0.message.content", "What is the weather?").Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.input_messages.1.message.role", "assistant").Times(1)
	// assistant message has no text content, so no content key is set
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.input_messages.1.message.tool_calls.count", 1).Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.input_messages.1.message.tool_calls.0.tool_call.id", "toolu_1").Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.input_messages.1.message.tool_calls.0.tool_call.function.name", "get_weather").Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.input_messages.1.message.tool_calls.0.tool_call.function.arguments", "{}").Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.tools.count", 0).Times(1)

	filter := &decoderFilter{handle: mockHandle, config: defaultCfg()}
	result := filter.OnRequestBody(newTestBodyBuffer(body), true)
	require.Equal(t, shared.BodyStatusContinue, result)
}

func TestOnRequestBody_CustomNamespace(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	body := []byte(`{
		"model": "claude-sonnet-4-20250514",
		"messages": [{"role": "user", "content": "Hello"}]
	}`)
	mockHandle.EXPECT().BufferedRequestBody().Return(newTestBodyBuffer(body)).AnyTimes()
	mockHandle.EXPECT().ReceivedRequestBody().Return(nil).AnyTimes()

	mockHandle.EXPECT().SetMetadata("my-namespace", "llm.model_name", "claude-sonnet-4-20250514").Times(1)
	mockHandle.EXPECT().SetMetadata("my-namespace", "llm.system", "anthropic").Times(1)
	mockHandle.EXPECT().SetMetadata("my-namespace", "llm.input_messages.count", 1).Times(1)
	mockHandle.EXPECT().SetMetadata("my-namespace", "llm.input_messages.0.message.role", "user").Times(1)
	mockHandle.EXPECT().SetMetadata("my-namespace", "llm.input_messages.0.message.content", "Hello").Times(1)
	mockHandle.EXPECT().SetMetadata("my-namespace", "llm.tools.count", 0).Times(1)

	filter := &decoderFilter{
		handle: mockHandle,
		config: &anthropicDecoderConfig{MetadataNamespace: "my-namespace"},
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
		"model": "claude-sonnet-4-20250514",
		"messages": [{"role": "user", "content": "Hello"}]
	}`)
	mockHandle.EXPECT().BufferedRequestBody().Return(newTestBodyBuffer(body)).AnyTimes()
	mockHandle.EXPECT().ReceivedRequestBody().Return(nil).AnyTimes()

	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.model_name", "claude-sonnet-4-20250514").Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.system", "anthropic").Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.input_messages.count", 1).Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.input_messages.0.message.role", "user").Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.input_messages.0.message.content", "Hello").Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.tools.count", 0).Times(1)

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
	require.Nil(t, filter.sseAcc)
}

func TestOnResponseHeaders_SSEContentType(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(shared.LogLevelDebug, gomock.Any(), gomock.Any()).Times(1)

	filter := &decoderFilter{handle: mockHandle, config: defaultCfg()}
	result := filter.OnResponseHeaders(fake.NewFakeHeaderMap(map[string][]string{
		"content-type": {"text/event-stream"},
	}), false)
	require.Equal(t, shared.HeadersStatusContinue, result)
	require.NotNil(t, filter.sseAcc)
}

func TestOnResponseHeaders_SSEContentTypeWithCharset(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(shared.LogLevelDebug, gomock.Any(), gomock.Any()).Times(1)

	filter := &decoderFilter{handle: mockHandle, config: defaultCfg()}
	result := filter.OnResponseHeaders(fake.NewFakeHeaderMap(map[string][]string{
		"content-type": {"text/event-stream; charset=utf-8"},
	}), false)
	require.Equal(t, shared.HeadersStatusContinue, result)
	require.NotNil(t, filter.sseAcc)
}

// --- Tests for decoderFilter.OnResponseBody ---

func TestOnResponseBody_NotEndOfStream_NonStreaming(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	filter := &decoderFilter{handle: mockHandle, config: defaultCfg()}
	result := filter.OnResponseBody(newTestBodyBuffer([]byte("data")), false)
	require.Equal(t, shared.BodyStatusStopAndBuffer, result)
}

func TestOnResponseBody_NotEndOfStream_Streaming(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	filter := &decoderFilter{handle: mockHandle, config: defaultCfg()}
	filter.sseAcc = newAnthropicSSEAccumulator(t.Logf)
	result := filter.OnResponseBody(newTestBodyBuffer([]byte("event: ping\ndata: {}\n\n")), false)
	require.Equal(t, shared.BodyStatusContinue, result)
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
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [
			{"type": "text", "text": "The weather is sunny."}
		],
		"usage": {
			"input_tokens": 20,
			"output_tokens": 10
		}
	}`)
	mockHandle.EXPECT().BufferedResponseBody().Return(newTestBodyBuffer(body)).AnyTimes()
	mockHandle.EXPECT().ReceivedResponseBody().Return(nil).AnyTimes()

	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.output_messages.count", 1).Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.output_messages.0.message.role", "assistant").Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.output_messages.0.message.content", "The weather is sunny.").Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.token_count.prompt", 20).Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.token_count.completion", 10).Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.token_count.total", 30).Times(1)

	filter := &decoderFilter{handle: mockHandle, config: defaultCfg()}
	result := filter.OnResponseBody(newTestBodyBuffer(body), true)
	require.Equal(t, shared.BodyStatusContinue, result)
}

func TestOnResponseBody_NoUsage_SetsMetadataWithoutTokenCounts(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	body := []byte(`{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [
			{"type": "text", "text": "Hello!"}
		]
	}`)
	mockHandle.EXPECT().BufferedResponseBody().Return(newTestBodyBuffer(body)).AnyTimes()
	mockHandle.EXPECT().ReceivedResponseBody().Return(nil).AnyTimes()

	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.output_messages.count", 1).Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.output_messages.0.message.role", "assistant").Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.output_messages.0.message.content", "Hello!").Times(1)
	// No token count calls expected when usage is absent

	filter := &decoderFilter{handle: mockHandle, config: defaultCfg()}
	result := filter.OnResponseBody(newTestBodyBuffer(body), true)
	require.Equal(t, shared.BodyStatusContinue, result)
}

func TestOnResponseBody_WithCacheTokens_SetsMetadata(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	body := []byte(`{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [
			{"type": "text", "text": "Done."}
		],
		"usage": {
			"input_tokens": 100,
			"output_tokens": 50,
			"cache_creation_input_tokens": 20,
			"cache_read_input_tokens": 30
		}
	}`)
	mockHandle.EXPECT().BufferedResponseBody().Return(newTestBodyBuffer(body)).AnyTimes()
	mockHandle.EXPECT().ReceivedResponseBody().Return(nil).AnyTimes()

	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.output_messages.count", 1).Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.output_messages.0.message.role", "assistant").Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.output_messages.0.message.content", "Done.").Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.token_count.prompt", 100).Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.token_count.completion", 50).Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.token_count.total", 150).Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.token_count.completion_details.cache_creation_input_tokens", 20).Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.token_count.completion_details.cache_read_input_tokens", 30).Times(1)

	filter := &decoderFilter{handle: mockHandle, config: defaultCfg()}
	result := filter.OnResponseBody(newTestBodyBuffer(body), true)
	require.Equal(t, shared.BodyStatusContinue, result)
}

func TestOnResponseBody_WithOutputToolCalls_SetsMetadata(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	body := []byte(`{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [
			{"type": "tool_use", "id": "toolu_abc", "name": "get_weather", "input": {"loc":"NYC"}}
		]
	}`)
	mockHandle.EXPECT().BufferedResponseBody().Return(newTestBodyBuffer(body)).AnyTimes()
	mockHandle.EXPECT().ReceivedResponseBody().Return(nil).AnyTimes()

	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.output_messages.count", 1).Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.output_messages.0.message.role", "assistant").Times(1)
	// no text content: no content key
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.output_messages.0.message.tool_calls.count", 1).Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.output_messages.0.message.tool_calls.0.tool_call.id", "toolu_abc").Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.output_messages.0.message.tool_calls.0.tool_call.function.name", "get_weather").Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.output_messages.0.message.tool_calls.0.tool_call.function.arguments", `{"loc":"NYC"}`).Times(1)

	filter := &decoderFilter{handle: mockHandle, config: defaultCfg()}
	result := filter.OnResponseBody(newTestBodyBuffer(body), true)
	require.Equal(t, shared.BodyStatusContinue, result)
}

func TestOnResponseBody_CustomNamespace(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	body := []byte(`{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [
			{"type": "text", "text": "Hi"}
		]
	}`)
	mockHandle.EXPECT().BufferedResponseBody().Return(newTestBodyBuffer(body)).AnyTimes()
	mockHandle.EXPECT().ReceivedResponseBody().Return(nil).AnyTimes()

	mockHandle.EXPECT().SetMetadata("my-ns", "llm.output_messages.count", 1).Times(1)
	mockHandle.EXPECT().SetMetadata("my-ns", "llm.output_messages.0.message.role", "assistant").Times(1)
	mockHandle.EXPECT().SetMetadata("my-ns", "llm.output_messages.0.message.content", "Hi").Times(1)

	filter := &decoderFilter{
		handle: mockHandle,
		config: &anthropicDecoderConfig{MetadataNamespace: "my-ns"},
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
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [
			{"type": "text", "text": "Done."}
		],
		"usage": {"input_tokens": 5, "output_tokens": 3}
	}`)
	mockHandle.EXPECT().BufferedResponseBody().Return(newTestBodyBuffer(body)).AnyTimes()
	mockHandle.EXPECT().ReceivedResponseBody().Return(nil).AnyTimes()

	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.output_messages.count", 1).Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.output_messages.0.message.role", "assistant").Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.output_messages.0.message.content", "Done.").Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.token_count.prompt", 5).Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.token_count.completion", 3).Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.token_count.total", 8).Times(1)

	filter := &decoderFilter{handle: mockHandle, config: defaultCfg()}
	result := filter.OnResponseTrailers(fake.NewFakeHeaderMap(map[string][]string{}))
	require.Equal(t, shared.TrailersStatusContinue, result)
}

// --- Tests for streaming (SSE) response handling ---

// sseHeaders returns a fake header map with content-type: text/event-stream.
func sseHeaders() shared.HeaderMap {
	return fake.NewFakeHeaderMap(map[string][]string{
		"content-type": {"text/event-stream"},
	})
}

func TestOnResponseBody_StreamingResponse_SetsMetadata(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(shared.LogLevelDebug, gomock.Any(), gomock.Any()).Times(1)

	chunk1 := []byte(
		"event: message_start\n" +
			"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_123\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n" +
			"event: content_block_start\n" +
			"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
			"event: content_block_delta\n" +
			"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n",
	)
	chunk2 := []byte(
		"event: content_block_delta\n" +
			"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\" world\"}}\n\n" +
			"event: content_block_stop\n" +
			"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
			"event: message_delta\n" +
			"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":5}}\n\n" +
			"event: message_stop\n" +
			"data: {\"type\":\"message_stop\"}\n\n",
	)

	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.output_messages.count", 1).Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.output_messages.0.message.role", "assistant").Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.output_messages.0.message.content", "Hello world").Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.token_count.prompt", 10).Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.token_count.completion", 5).Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.token_count.total", 15).Times(1)

	filter := &decoderFilter{handle: mockHandle, config: defaultCfg()}
	filter.OnResponseHeaders(sseHeaders(), false)

	result := filter.OnResponseBody(newTestBodyBuffer(chunk1), false)
	require.Equal(t, shared.BodyStatusContinue, result)

	result = filter.OnResponseBody(newTestBodyBuffer(chunk2), true)
	require.Equal(t, shared.BodyStatusContinue, result)
}

func TestOnResponseBody_StreamingResponse_SingleChunk(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(shared.LogLevelDebug, gomock.Any(), gomock.Any()).Times(1)

	body := []byte(
		"event: message_start\n" +
			"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_123\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n" +
			"event: content_block_start\n" +
			"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
			"event: content_block_delta\n" +
			"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello world\"}}\n\n" +
			"event: content_block_stop\n" +
			"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
			"event: message_delta\n" +
			"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":5}}\n\n" +
			"event: message_stop\n" +
			"data: {\"type\":\"message_stop\"}\n\n",
	)

	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.output_messages.count", 1).Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.output_messages.0.message.role", "assistant").Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.output_messages.0.message.content", "Hello world").Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.token_count.prompt", 10).Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.token_count.completion", 5).Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.token_count.total", 15).Times(1)

	filter := &decoderFilter{handle: mockHandle, config: defaultCfg()}
	filter.OnResponseHeaders(sseHeaders(), false)

	result := filter.OnResponseBody(newTestBodyBuffer(body), true)
	require.Equal(t, shared.BodyStatusContinue, result)
}

func TestOnResponseBody_StreamingResponse_WithToolCalls(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(shared.LogLevelDebug, gomock.Any(), gomock.Any()).Times(1)

	body := []byte(
		"event: message_start\n" +
			"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_123\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"usage\":{\"input_tokens\":20,\"output_tokens\":0}}}\n\n" +
			"event: content_block_start\n" +
			"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_1\",\"name\":\"get_weather\",\"input\":{}}}\n\n" +
			"event: content_block_delta\n" +
			"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"loc\\\":\\\"NYC\\\"}\"}}\n\n" +
			"event: content_block_stop\n" +
			"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
			"event: message_delta\n" +
			"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":10}}\n\n" +
			"event: message_stop\n" +
			"data: {\"type\":\"message_stop\"}\n\n",
	)

	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.output_messages.count", 1).Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.output_messages.0.message.role", "assistant").Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.output_messages.0.message.tool_calls.count", 1).Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.output_messages.0.message.tool_calls.0.tool_call.id", "toolu_1").Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.output_messages.0.message.tool_calls.0.tool_call.function.name", "get_weather").Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.output_messages.0.message.tool_calls.0.tool_call.function.arguments", `{"loc":"NYC"}`).Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.token_count.prompt", 20).Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.token_count.completion", 10).Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.token_count.total", 30).Times(1)

	filter := &decoderFilter{handle: mockHandle, config: defaultCfg()}
	filter.OnResponseHeaders(sseHeaders(), false)

	result := filter.OnResponseBody(newTestBodyBuffer(body), true)
	require.Equal(t, shared.BodyStatusContinue, result)
}

func TestOnResponseBody_StreamingResponse_ChunkSplitMidLine(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(shared.LogLevelDebug, gomock.Any(), gomock.Any()).Times(1)

	// Split a data line in the middle to test buffering of partial lines.
	fullStream := "event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_123\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[]}}\n\n" +
		"event: content_block_start\n" +
		"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"OK\"}}\n\n" +
		"event: content_block_stop\n" +
		"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n\n"

	chunk1 := []byte(fullStream[:30])
	chunk2 := []byte(fullStream[30:])

	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.output_messages.count", 1).Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.output_messages.0.message.role", "assistant").Times(1)
	mockHandle.EXPECT().SetMetadata("io.builtonenvoy.anthropic", "llm.output_messages.0.message.content", "OK").Times(1)

	filter := &decoderFilter{handle: mockHandle, config: defaultCfg()}
	filter.OnResponseHeaders(sseHeaders(), false)

	result := filter.OnResponseBody(newTestBodyBuffer(chunk1), false)
	require.Equal(t, shared.BodyStatusContinue, result)

	result = filter.OnResponseBody(newTestBodyBuffer(chunk2), true)
	require.Equal(t, shared.BodyStatusContinue, result)
}
