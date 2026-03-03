// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package impl

import (
	"encoding/json"
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

// --- Tests for chatCompletionsDecoderConfig ---

func TestNamespace_Default(t *testing.T) {
	cfg := &chatCompletionsDecoderConfig{}
	require.Equal(t, "openai", cfg.namespace())
}

func TestNamespace_Custom(t *testing.T) {
	cfg := &chatCompletionsDecoderConfig{MetadataNamespace: "my-ns"}
	require.Equal(t, "my-ns", cfg.namespace())
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
}

func TestDecoderConfigFactory_Create_NilConfig(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockConfigHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)

	factory := &decoderConfigFactory{}
	filterFactory, err := factory.Create(mockConfigHandle, nil)
	require.NoError(t, err)
	require.NotNil(t, filterFactory)
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

	cfg := &chatCompletionsDecoderConfig{}
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

	filter := &decoderFilter{handle: mockHandle, config: &chatCompletionsDecoderConfig{}}
	result := filter.OnRequestHeaders(fake.NewFakeHeaderMap(map[string][]string{}), true)
	require.Equal(t, shared.HeadersStatusContinue, result)
}

func TestOnRequestHeaders_NotEndOfStream(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	filter := &decoderFilter{handle: mockHandle, config: &chatCompletionsDecoderConfig{}}
	result := filter.OnRequestHeaders(fake.NewFakeHeaderMap(map[string][]string{}), false)
	require.Equal(t, shared.HeadersStatusStop, result)
}

// --- Tests for decoderFilter.OnRequestBody ---

func TestOnRequestBody_NotEndOfStream(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	filter := &decoderFilter{handle: mockHandle, config: &chatCompletionsDecoderConfig{}}
	result := filter.OnRequestBody(newTestBodyBuffer([]byte("data")), false)
	require.Equal(t, shared.BodyStatusStopAndBuffer, result)
}

func TestOnRequestBody_EmptyBody(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	mockHandle.EXPECT().BufferedRequestBody().Return(newTestBodyBuffer([]byte{})).AnyTimes()
	mockHandle.EXPECT().ReceivedRequestBody().Return(nil).AnyTimes()

	filter := &decoderFilter{handle: mockHandle, config: &chatCompletionsDecoderConfig{}}
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

	filter := &decoderFilter{handle: mockHandle, config: &chatCompletionsDecoderConfig{}}
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

	mockHandle.EXPECT().SetMetadata("openai", "model", "gpt-4o").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "system_prompt", "You are a helpful assistant.").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "user_prompt", "What is the weather?").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "message_count", int64(2)).Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "has_tools", "false").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "has_tool_calls", "false").Times(1)

	filter := &decoderFilter{handle: mockHandle, config: &chatCompletionsDecoderConfig{}}
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

	toolNamesJSON, _ := json.Marshal([]string{"my_func"})
	mockHandle.EXPECT().SetMetadata("openai", "model", "gpt-4o").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "system_prompt", "").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "user_prompt", "Call a tool").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "message_count", int64(1)).Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "has_tools", "true").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "has_tool_calls", "false").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "tool_names", string(toolNamesJSON)).Times(1)

	filter := &decoderFilter{handle: mockHandle, config: &chatCompletionsDecoderConfig{}}
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

	mockHandle.EXPECT().SetMetadata("my-namespace", "model", "gpt-4o").Times(1)
	mockHandle.EXPECT().SetMetadata("my-namespace", "system_prompt", "").Times(1)
	mockHandle.EXPECT().SetMetadata("my-namespace", "user_prompt", "Hello").Times(1)
	mockHandle.EXPECT().SetMetadata("my-namespace", "message_count", int64(1)).Times(1)
	mockHandle.EXPECT().SetMetadata("my-namespace", "has_tools", "false").Times(1)
	mockHandle.EXPECT().SetMetadata("my-namespace", "has_tool_calls", "false").Times(1)

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

	mockHandle.EXPECT().SetMetadata("openai", "model", "gpt-4o").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "system_prompt", "").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "user_prompt", "Hello").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "message_count", int64(1)).Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "has_tools", "false").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "has_tool_calls", "false").Times(1)

	filter := &decoderFilter{handle: mockHandle, config: &chatCompletionsDecoderConfig{}}
	result := filter.OnRequestTrailers(fake.NewFakeHeaderMap(map[string][]string{}))
	require.Equal(t, shared.TrailersStatusContinue, result)
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

	mockHandle.EXPECT().SetMetadata("openai", "model", "gpt-4o").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "system_prompt", "").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "user_prompt", "What is the weather?").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "message_count", int64(2)).Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "has_tools", "false").Times(1)
	mockHandle.EXPECT().SetMetadata("openai", "has_tool_calls", "true").Times(1)

	filter := &decoderFilter{handle: mockHandle, config: &chatCompletionsDecoderConfig{}}
	result := filter.OnRequestBody(newTestBodyBuffer(body), true)
	require.Equal(t, shared.BodyStatusContinue, result)
}
