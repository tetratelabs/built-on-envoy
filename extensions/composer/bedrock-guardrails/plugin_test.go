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

// testBodyBuffer is a correct implementation of shared.BodyBuffer for tests,
// working around a missing return in fake.FakeBodyBuffer.Drain.
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

// --- Tests for getContent ---

func TestGetContent_SingleUserMessage(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"hello world"}],"model":"gpt-4"}`)

	content, err := getContent(body)
	require.NoError(t, err)
	require.Len(t, content, 1)
	require.Equal(t, "hello world", content[0].Text.Text)
}

func TestGetContent_MultipleUserMessages(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role": "system", "content": "You are helpful"},
			{"role": "user", "content": "First question"},
			{"role": "assistant", "content": "First answer"},
			{"role": "user", "content": "Second question"}
		],
		"model": "gpt-4"
	}`)

	content, err := getContent(body)
	require.NoError(t, err)
	require.Len(t, content, 2)
	require.Equal(t, "First question", content[0].Text.Text)
	require.Equal(t, "Second question", content[1].Text.Text)
}

func TestGetContent_ArrayContentUserMessage(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role": "user", "content": [{"type": "text", "text": "Part 1"}, {"type": "text", "text": "Part 2"}]}
		],
		"model": "gpt-4"
	}`)

	content, err := getContent(body)
	require.NoError(t, err)
	require.Len(t, content, 1)
	require.Equal(t, "Part 1\nPart 2", content[0].Text.Text)
}

func TestGetContent_NoUserMessages(t *testing.T) {
	body := []byte(`{"messages":[{"role":"system","content":"You are helpful"}],"model":"gpt-4"}`)

	content, err := getContent(body)
	require.NoError(t, err)
	require.Empty(t, content)
}

func TestGetContent_InvalidJSON(t *testing.T) {
	body := []byte(`{invalid json}`)

	content, err := getContent(body)
	require.Error(t, err)
	require.Contains(t, err.Error(), "parsing chat request")
	require.Nil(t, content)
}

// --- Tests for CustomHttpFilterConfigFactory.Create ---

func TestCustomHTTPFilterConfigFactory_Create_ValidConfig(t *testing.T) {
	cfg := bedrockGuardrailsConfig{
		BedrockEndpoint: "bedrock.us-east-1.amazonaws.com",
		Cluster:         "bedrock-cluster",
		BedrockAPIKey:   "my-api-key",
		BedrockGuardrails: []bedrockGuardrail{
			{Identifier: "g1", Version: "1"},
		},
	}
	cfgJSON, err := json.Marshal(cfg)
	require.NoError(t, err)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockConfigHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockConfigHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	factory := &CustomHttpFilterConfigFactory{}
	filterFactory, err := factory.Create(mockConfigHandle, cfgJSON)

	require.NoError(t, err)
	require.NotNil(t, filterFactory)

	customFactory, ok := filterFactory.(*customHTTPFilterFactory)
	require.True(t, ok)
	require.Equal(t, "bedrock-cluster", customFactory.config.Cluster)
	require.Equal(t, "my-api-key", customFactory.config.BedrockAPIKey)
	require.Len(t, customFactory.config.BedrockGuardrails, 1)
	require.Equal(t, "g1", customFactory.config.BedrockGuardrails[0].Identifier)
}

func TestCustomHTTPFilterConfigFactory_Create_EmptyConfig(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockConfigHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockConfigHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	factory := &CustomHttpFilterConfigFactory{}
	filterFactory, err := factory.Create(mockConfigHandle, []byte{})

	// Empty config is valid — defaults are applied
	require.NoError(t, err)
	require.NotNil(t, filterFactory)

	customFactory, ok := filterFactory.(*customHTTPFilterFactory)
	require.True(t, ok)
	// Default TimeoutMs should be set
	require.Equal(t, uint64(1000*10), customFactory.config.TimeoutMs)
}

func TestCustomHTTPFilterConfigFactory_Create_NilConfig(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockConfigHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockConfigHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	factory := &CustomHttpFilterConfigFactory{}
	filterFactory, err := factory.Create(mockConfigHandle, nil)

	require.NoError(t, err)
	require.NotNil(t, filterFactory)
}

func TestCustomHTTPFilterConfigFactory_Create_InvalidJSON(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockConfigHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockConfigHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	factory := &CustomHttpFilterConfigFactory{}
	filterFactory, err := factory.Create(mockConfigHandle, []byte(`{invalid json}`))

	require.Error(t, err)
	require.Nil(t, filterFactory)
}

func TestCustomHTTPFilterConfigFactory_Create_DefaultTimeout(t *testing.T) {
	cfgJSON := []byte(`{"bedrock_cluster": "my-cluster"}`)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockConfigHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockConfigHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	factory := &CustomHttpFilterConfigFactory{}
	filterFactory, err := factory.Create(mockConfigHandle, cfgJSON)

	require.NoError(t, err)
	customFactory := filterFactory.(*customHTTPFilterFactory)
	// TimeoutMs defaults to 10s
	require.Equal(t, uint64(10000), customFactory.config.TimeoutMs)
}

// --- Tests for customHTTPFilterFactory.Create ---

func TestCustomHTTPFilterFactory_Create(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	cfg := &bedrockGuardrailsConfig{
		Cluster: "my-cluster",
	}
	factory := &customHTTPFilterFactory{config: cfg}

	filter := factory.Create(mockHandle)

	require.NotNil(t, filter)
	bgFilter, ok := filter.(*bedrockGuardrailsHTTPFilter)
	require.True(t, ok)
	require.Equal(t, mockHandle, bgFilter.handle)
	require.Equal(t, cfg, bgFilter.config)
}

// --- Tests for WellKnownHttpFilterConfigFactories ---

func TestWellKnownHttpFilterConfigFactories_BedrockGuardrails(t *testing.T) {
	factories := WellKnownHttpFilterConfigFactories()

	require.NotNil(t, factories)
	require.Len(t, factories, 1)
	require.Contains(t, factories, "bedrock-guardrails")

	_, ok := factories["bedrock-guardrails"].(*CustomHttpFilterConfigFactory)
	require.True(t, ok)
}

// --- Tests for bedrockGuardrailsHTTPFilter.OnRequestHeaders ---

func TestOnRequestHeaders_AlwaysStops(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	filter := &bedrockGuardrailsHTTPFilter{
		handle: mockHandle,
		config: &bedrockGuardrailsConfig{},
	}

	result := filter.OnRequestHeaders(fake.NewFakeHeaderMap(map[string][]string{}), false)
	require.Equal(t, shared.HeadersStatusStop, result)
}

func TestOnRequestHeaders_HeadersOnly(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)

	filter := &bedrockGuardrailsHTTPFilter{
		handle: mockHandle,
		config: &bedrockGuardrailsConfig{},
	}
	mockHandle.EXPECT().Log(shared.LogLevelDebug, gomock.Any()).Times(1)

	result := filter.OnRequestHeaders(fake.NewFakeHeaderMap(map[string][]string{}), true)
	require.Equal(t, shared.HeadersStatusContinue, result)
}

// --- Tests for dedupGuardrails ---

func TestDedupGuardrails_NoDuplicates(t *testing.T) {
	input := []bedrockGuardrail{
		{Identifier: "g1", Version: "1"},
		{Identifier: "g2", Version: "2"},
	}
	result := dedupGuardrails(input)
	require.Len(t, result, 2)
}

func TestDedupGuardrails_WithDuplicates(t *testing.T) {
	input := []bedrockGuardrail{
		{Identifier: "g1", Version: "1"},
		{Identifier: "g1", Version: "1"},
		{Identifier: "g2", Version: "2"},
	}
	result := dedupGuardrails(input)
	require.Len(t, result, 2)
	require.Equal(t, "g1", result[0].Identifier)
	require.Equal(t, "g2", result[1].Identifier)
}

func TestDedupGuardrails_SameIdentifierDifferentVersion(t *testing.T) {
	input := []bedrockGuardrail{
		{Identifier: "g1", Version: "1"},
		{Identifier: "g1", Version: "2"},
	}
	result := dedupGuardrails(input)
	require.Len(t, result, 2)
}

func TestDedupGuardrails_Empty(t *testing.T) {
	result := dedupGuardrails([]bedrockGuardrail{})
	require.Empty(t, result)
}

func TestDedupGuardrails_AllDuplicates(t *testing.T) {
	input := []bedrockGuardrail{
		{Identifier: "g1", Version: "1"},
		{Identifier: "g1", Version: "1"},
		{Identifier: "g1", Version: "1"},
	}
	result := dedupGuardrails(input)
	require.Len(t, result, 1)
	require.Equal(t, "g1", result[0].Identifier)
}

func TestCustomHTTPFilterConfigFactory_Create_DeduplicatesGuardrails(t *testing.T) {
	cfg := bedrockGuardrailsConfig{
		Cluster: "bedrock-cluster",
		BedrockGuardrails: []bedrockGuardrail{
			{Identifier: "g1", Version: "1"},
			{Identifier: "g1", Version: "1"},
			{Identifier: "g2", Version: "2"},
		},
	}
	cfgJSON, err := json.Marshal(cfg)
	require.NoError(t, err)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockConfigHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockConfigHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	factory := &CustomHttpFilterConfigFactory{}
	filterFactory, err := factory.Create(mockConfigHandle, cfgJSON)

	require.NoError(t, err)
	customFactory := filterFactory.(*customHTTPFilterFactory)
	require.Len(t, customFactory.config.BedrockGuardrails, 2)
	require.Equal(t, "g1", customFactory.config.BedrockGuardrails[0].Identifier)
	require.Equal(t, "g2", customFactory.config.BedrockGuardrails[1].Identifier)
}

// --- Tests for bedrockGuardrailsHTTPFilter.OnRequestBody ---

func TestOnRequestBody_NotEndStream(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	filter := &bedrockGuardrailsHTTPFilter{
		handle: mockHandle,
		config: &bedrockGuardrailsConfig{},
	}

	// endStream=false should return StopAndBuffer immediately
	result := filter.OnRequestBody(newTestBodyBuffer([]byte("some data")), false)
	require.Equal(t, shared.BodyStatusStopAndBuffer, result)
}

func TestOnRequestBody_EmptyBody(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	// ReadWholeRequestBody calls BufferedRequestBody and ReceivedRequestBody
	emptyBuffer := newTestBodyBuffer([]byte{})
	mockHandle.EXPECT().BufferedRequestBody().Return(emptyBuffer).AnyTimes()
	mockHandle.EXPECT().ReceivedRequestBody().Return(nil).AnyTimes()

	filter := &bedrockGuardrailsHTTPFilter{
		handle: mockHandle,
		config: &bedrockGuardrailsConfig{
			BedrockGuardrails: []bedrockGuardrail{{Identifier: "g1", Version: "1"}},
		},
	}

	result := filter.OnRequestBody(newTestBodyBuffer([]byte{}), true)
	require.Equal(t, shared.BodyStatusContinue, result)
}

func TestOnRequestBody_NoGuardrailsConfigured(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	bodyBytes := []byte(`{"messages":[{"role":"user","content":"hello"}],"model":"gpt-4"}`)
	fakeBuffer := newTestBodyBuffer(bodyBytes)
	mockHandle.EXPECT().BufferedRequestBody().Return(fakeBuffer).AnyTimes()
	mockHandle.EXPECT().ReceivedRequestBody().Return(nil).AnyTimes()

	filter := &bedrockGuardrailsHTTPFilter{
		handle: mockHandle,
		config: &bedrockGuardrailsConfig{
			BedrockGuardrails: []bedrockGuardrail{}, // empty
		},
	}

	result := filter.OnRequestBody(newTestBodyBuffer(bodyBytes), true)
	require.Equal(t, shared.BodyStatusContinue, result)
}

func TestOnRequestBody_ValidBody_CalloutSuccess(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	bodyBytes := []byte(`{"messages":[{"role":"user","content":"hello"}],"model":"gpt-4"}`)
	fakeBuffer := newTestBodyBuffer(bodyBytes)
	mockHandle.EXPECT().BufferedRequestBody().Return(fakeBuffer).AnyTimes()
	mockHandle.EXPECT().ReceivedRequestBody().Return(fakeBuffer).AnyTimes()

	// RequestHeaders().Remove is called to remove content-length
	fakeHeaders := fake.NewFakeHeaderMap(map[string][]string{"content-length": {"42"}})
	mockHandle.EXPECT().RequestHeaders().Return(fakeHeaders).AnyTimes()

	mockHandle.EXPECT().HttpCallout(
		"bedrock-cluster",
		gomock.Any(), // headers
		gomock.Any(), // body
		uint64(1000*20),
		gomock.Any(), // callback
	).Return(shared.HttpCalloutInitSuccess, uint64(1)).Times(1)

	cfg := &bedrockGuardrailsConfig{
		Cluster:         "bedrock-cluster",
		BedrockEndpoint: "bedrock.us-east-1.amazonaws.com",
		BedrockAPIKey:   "my-api-key",
		BedrockGuardrails: []bedrockGuardrail{
			{Identifier: "g1", Version: "1"},
		},
	}

	filter := &bedrockGuardrailsHTTPFilter{
		handle: mockHandle,
		config: cfg,
	}

	result := filter.OnRequestBody(newTestBodyBuffer(bodyBytes), true)
	require.Equal(t, shared.BodyStatusStopAndBuffer, result)

	// content-length header should have been removed
	require.Empty(t, fakeHeaders.Headers["content-length"])
}

func TestOnRequestBody_InvalidBody_GetCalloutHeadersError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	// Body is invalid JSON — getCalloutHeaders will fail
	invalidBody := []byte(`{invalid json}`)
	fakeBuffer := newTestBodyBuffer(invalidBody)
	mockHandle.EXPECT().BufferedRequestBody().Return(fakeBuffer).AnyTimes()
	mockHandle.EXPECT().ReceivedRequestBody().Return(fakeBuffer).AnyTimes()

	fakeHeaders := fake.NewFakeHeaderMap(map[string][]string{})
	mockHandle.EXPECT().RequestHeaders().Return(fakeHeaders).AnyTimes()

	mockHandle.EXPECT().SendLocalResponse(uint32(502), gomock.Any(), gomock.Any(), "").Times(1)

	cfg := &bedrockGuardrailsConfig{
		Cluster:         "bedrock-cluster",
		BedrockEndpoint: "bedrock.us-east-1.amazonaws.com",
		BedrockAPIKey:   "my-api-key",
		BedrockGuardrails: []bedrockGuardrail{
			{Identifier: "g1", Version: "1"},
		},
	}

	filter := &bedrockGuardrailsHTTPFilter{
		handle: mockHandle,
		config: cfg,
	}

	result := filter.OnRequestBody(newTestBodyBuffer(invalidBody), true)
	require.Equal(t, shared.BodyStatusStopAndBuffer, result)
}

func TestOnRequestBody_CalloutInitFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	bodyBytes := []byte(`{"messages":[{"role":"user","content":"hello"}],"model":"gpt-4"}`)
	fakeBuffer := newTestBodyBuffer(bodyBytes)
	mockHandle.EXPECT().BufferedRequestBody().Return(fakeBuffer).AnyTimes()
	mockHandle.EXPECT().ReceivedRequestBody().Return(fakeBuffer).AnyTimes()

	fakeHeaders := fake.NewFakeHeaderMap(map[string][]string{})
	mockHandle.EXPECT().RequestHeaders().Return(fakeHeaders).AnyTimes()

	// HttpCallout fails to initialize
	mockHandle.EXPECT().HttpCallout(
		gomock.Any(),
		gomock.Any(),
		gomock.Any(),
		gomock.Any(),
		gomock.Any(),
	).Return(shared.HttpCalloutInitClusterNotFound, uint64(0)).Times(1)

	mockHandle.EXPECT().SendLocalResponse(uint32(502), gomock.Any(), gomock.Any(), "").Times(1)

	cfg := &bedrockGuardrailsConfig{
		Cluster:         "bedrock-cluster",
		BedrockEndpoint: "bedrock.us-east-1.amazonaws.com",
		BedrockAPIKey:   "my-api-key",
		BedrockGuardrails: []bedrockGuardrail{
			{Identifier: "g1", Version: "1"},
		},
	}

	filter := &bedrockGuardrailsHTTPFilter{
		handle: mockHandle,
		config: cfg,
	}

	result := filter.OnRequestBody(newTestBodyBuffer(bodyBytes), true)
	require.Equal(t, shared.BodyStatusStopAndBuffer, result)
}

func TestOnRequestBody_MultipleGuardrails_FirstTriggered(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	bodyBytes := []byte(`{"messages":[{"role":"user","content":"hello"}],"model":"gpt-4"}`)
	fakeBuffer := newTestBodyBuffer(bodyBytes)
	mockHandle.EXPECT().BufferedRequestBody().Return(fakeBuffer).AnyTimes()
	mockHandle.EXPECT().ReceivedRequestBody().Return(fakeBuffer).AnyTimes()

	fakeHeaders := fake.NewFakeHeaderMap(map[string][]string{})
	mockHandle.EXPECT().RequestHeaders().Return(fakeHeaders).AnyTimes()

	var capturedHeaders [][2]string
	mockHandle.EXPECT().HttpCallout(
		"bedrock-cluster",
		gomock.Any(),
		gomock.Any(),
		gomock.Any(),
		gomock.Any(),
	).DoAndReturn(func(_ string, headers [][2]string, _ []byte, _ uint64, _ shared.HttpCalloutCallback) (shared.HttpCalloutInitResult, uint64) {
		capturedHeaders = headers
		return shared.HttpCalloutInitSuccess, uint64(1)
	}).Times(1)

	cfg := &bedrockGuardrailsConfig{
		Cluster:         "bedrock-cluster",
		BedrockEndpoint: "bedrock.us-east-1.amazonaws.com",
		BedrockAPIKey:   "my-api-key",
		BedrockGuardrails: []bedrockGuardrail{
			{Identifier: "first-guardrail", Version: "1"},
			{Identifier: "second-guardrail", Version: "2"},
		},
	}

	filter := &bedrockGuardrailsHTTPFilter{
		handle: mockHandle,
		config: cfg,
	}

	result := filter.OnRequestBody(newTestBodyBuffer(bodyBytes), true)
	require.Equal(t, shared.BodyStatusStopAndBuffer, result)

	// Only the first guardrail should have been called
	require.Equal(t, "/guardrail/first-guardrail/version/1/apply", headerValue(capturedHeaders, ":path"))
}
