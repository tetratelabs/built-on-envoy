// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package impl

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/fake"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// --- Tests for joinBody ---

func TestJoinBody_SingleChunk(t *testing.T) {
	chunk := []byte("hello world")
	result := joinBody([][]byte{chunk})
	require.Equal(t, chunk, result)
}

func TestJoinBody_MultipleChunks(t *testing.T) {
	result := joinBody([][]byte{[]byte("foo"), []byte("bar"), []byte("baz")})
	require.Equal(t, []byte("foobarbaz"), result)
}

func TestJoinBody_EmptySlice(t *testing.T) {
	result := joinBody([][]byte{})
	require.Empty(t, result)
}

func TestJoinBody_EmptyChunks(t *testing.T) {
	result := joinBody([][]byte{{}, []byte("data"), {}})
	require.Equal(t, []byte("data"), result)
}

// --- Tests for headerValue ---

func TestHeaderValue_Found(t *testing.T) {
	headers := [][2]string{
		{":method", "POST"},
		{":path", "/api/v1"},
		{":status", "200"},
	}
	require.Equal(t, "POST", headerValue(headers, ":method"))
	require.Equal(t, "/api/v1", headerValue(headers, ":path"))
	require.Equal(t, "200", headerValue(headers, ":status"))
}

func TestHeaderValue_NotFound(t *testing.T) {
	headers := [][2]string{
		{":method", "POST"},
	}
	require.Empty(t, headerValue(headers, ":status"))
}

func TestHeaderValue_EmptyHeaders(t *testing.T) {
	require.Empty(t, headerValue([][2]string{}, ":method"))
}

func TestHeaderValue_ReturnsFirstMatch(t *testing.T) {
	headers := [][2]string{
		{"x-header", "first"},
		{"x-header", "second"},
	}
	require.Equal(t, "first", headerValue(headers, "x-header"))
}

// --- Tests for sendLocalRespError ---

func TestSendLocalRespError_WithBody(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockHandle.EXPECT().SendLocalResponse(uint32(http.StatusBadGateway), gomock.Any(), []byte("error message"), "").Times(1)

	result := sendLocalRespError(mockHandle, shared.LogLevelError, http.StatusBadGateway, "error message", []byte(`{"raw":"body"}`))
	require.Equal(t, shared.HeadersStatusStop, result)
}

func TestSendLocalRespError_WithoutBody(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockHandle.EXPECT().SendLocalResponse(uint32(http.StatusBadRequest), gomock.Any(), []byte("bad request"), "").Times(1)

	result := sendLocalRespError(mockHandle, shared.LogLevelDebug, http.StatusBadRequest, "bad request", nil)
	require.Equal(t, shared.HeadersStatusStop, result)
}

func TestSendLocalRespError_EmptyBody(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockHandle.EXPECT().SendLocalResponse(uint32(http.StatusInternalServerError), gomock.Any(), gomock.Any(), "").Times(1)

	result := sendLocalRespError(mockHandle, shared.LogLevelError, http.StatusInternalServerError, "server error", []byte{})
	require.Equal(t, shared.HeadersStatusStop, result)
}

// --- Tests for getCalloutHeaders ---

func TestGetCalloutHeaders_ValidBody(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"hello world"}],"model":"gpt-4"}`)

	args := &ApplyGuardrailArgs{
		GuardrailIdentifier: "my-guardrail",
		GuardrailVersion:    "DRAFT",
		Body:                body,
		Endpoint:            "bedrock.us-east-1.amazonaws.com",
		APIKey:              "my-secret-key",
	}

	headers, calloutBody, err := getCalloutHeaders(args)
	require.NoError(t, err)
	require.NotNil(t, headers)
	require.NotNil(t, calloutBody)

	// Verify path contains guardrail ID and version
	require.Equal(t, "/guardrail/my-guardrail/version/DRAFT/apply", headerValue(headers, ":path"))
	// Verify required HTTP/2 pseudo-headers
	require.Equal(t, "POST", headerValue(headers, ":method"))
	require.Equal(t, "bedrock.us-east-1.amazonaws.com", headerValue(headers, ":authority"))
	require.Equal(t, "https", headerValue(headers, ":scheme"))
	// Verify auth header
	require.Equal(t, "Bearer my-secret-key", headerValue(headers, "Authorization"))
	// Verify content type
	require.Equal(t, "application/json", headerValue(headers, "Content-type"))

	// Verify body is a valid ApplyGuardrailRequest
	var req ApplyGuardrailRequest
	require.NoError(t, json.Unmarshal(calloutBody, &req))
	require.Equal(t, "INPUT", req.Source)
	require.Len(t, req.Content, 1)
	require.Equal(t, "hello world", req.Content[0].Text.Text)
}

func TestGetCalloutHeaders_MultipleUserMessages(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"first"},{"role":"assistant","content":"answer"},{"role":"user","content":"second"}],"model":"gpt-4"}`)

	args := &ApplyGuardrailArgs{
		GuardrailIdentifier: "g1",
		GuardrailVersion:    "1",
		Body:                body,
	}

	headers, calloutBody, err := getCalloutHeaders(args)
	require.NoError(t, err)
	require.Equal(t, "/guardrail/g1/version/1/apply", headerValue(headers, ":path"))

	var req ApplyGuardrailRequest
	require.NoError(t, json.Unmarshal(calloutBody, &req))
	require.Len(t, req.Content, 2)
	require.Equal(t, "first", req.Content[0].Text.Text)
	require.Equal(t, "second", req.Content[1].Text.Text)
}

func TestGetCalloutHeaders_InvalidBody(t *testing.T) {
	args := &ApplyGuardrailArgs{
		GuardrailIdentifier: "g1",
		GuardrailVersion:    "1",
		Body:                []byte(`{invalid json}`),
	}

	headers, body, err := getCalloutHeaders(args)
	require.Error(t, err)
	require.Nil(t, headers)
	require.Nil(t, body)
}

// --- Tests for applyGuardrailCallback index-based next guardrail logic ---

// nextGuardrailIndex returns the next guardrail index or nil if done.
// This mirrors the logic in OnHttpCalloutDone: nextIndex := a.index + 1.
func TestNextGuardrailIndex_FirstOfTwo(t *testing.T) {
	cfg := &bedrockGuardrailsConfig{
		BedrockGuardrails: []bedrockGuardrail{
			{Identifier: "g1", Version: "1"},
			{Identifier: "g2", Version: "2"},
		},
	}
	index := 0
	nextIndex := index + 1
	require.Less(t, nextIndex, len(cfg.BedrockGuardrails))
	require.Equal(t, "g2", cfg.BedrockGuardrails[nextIndex].Identifier)
}

func TestNextGuardrailIndex_LastOfTwo(t *testing.T) {
	cfg := &bedrockGuardrailsConfig{
		BedrockGuardrails: []bedrockGuardrail{
			{Identifier: "g1", Version: "1"},
			{Identifier: "g2", Version: "2"},
		},
	}
	index := 1
	nextIndex := index + 1
	require.GreaterOrEqual(t, nextIndex, len(cfg.BedrockGuardrails))
}

func TestNextGuardrailIndex_SingleGuardrail(t *testing.T) {
	cfg := &bedrockGuardrailsConfig{
		BedrockGuardrails: []bedrockGuardrail{
			{Identifier: "g1", Version: "1"},
		},
	}
	index := 0
	nextIndex := index + 1
	require.GreaterOrEqual(t, nextIndex, len(cfg.BedrockGuardrails))
}

func TestNextGuardrailIndex_EmptyConfig(t *testing.T) {
	cfg := &bedrockGuardrailsConfig{}
	index := 0
	nextIndex := index + 1
	require.GreaterOrEqual(t, nextIndex, len(cfg.BedrockGuardrails))
}

// --- Helpers for OnHttpCalloutDone tests ---

// noInterventionResponse returns a guardrail response JSON with no intervention.
func noInterventionResponse(guardrailID, guardrailVersion string) []byte {
	resp := map[string]any{
		"action":  "NONE",
		"outputs": []any{},
		"assessments": []any{
			map[string]any{
				"appliedGuardrailDetails": map[string]any{
					"guardrailId":      guardrailID,
					"guardrailVersion": guardrailVersion,
				},
				"contentPolicy":              map[string]any{"filters": []any{}},
				"topicPolicy":                map[string]any{"topics": []any{}},
				"sensitiveInformationPolicy": map[string]any{"piiEntities": []any{}},
			},
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

// blockedContentPolicyResponse returns a guardrail response blocked by content policy.
func blockedContentPolicyResponse(guardrailID, guardrailVersion string) []byte {
	resp := map[string]any{
		"action": "GUARDRAIL_INTERVENED",
		"outputs": []any{
			map[string]any{"text": "blocked content"},
		},
		"assessments": []any{
			map[string]any{
				"appliedGuardrailDetails": map[string]any{
					"guardrailId":      guardrailID,
					"guardrailVersion": guardrailVersion,
				},
				"contentPolicy": map[string]any{
					"filters": []any{
						map[string]any{
							"action":         "BLOCKED",
							"confidence":     "HIGH",
							"detected":       true,
							"filterStrength": "HIGH",
							"type":           "VIOLENCE",
						},
					},
				},
				"topicPolicy":                map[string]any{"topics": []any{}},
				"sensitiveInformationPolicy": map[string]any{"piiEntities": []any{}},
			},
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

// blockedTopicPolicyResponse returns a guardrail response blocked by topic policy.
func blockedTopicPolicyResponse(guardrailID, guardrailVersion string) []byte {
	resp := map[string]any{
		"action":  "GUARDRAIL_INTERVENED",
		"outputs": []any{},
		"assessments": []any{
			map[string]any{
				"appliedGuardrailDetails": map[string]any{
					"guardrailId":      guardrailID,
					"guardrailVersion": guardrailVersion,
				},
				"contentPolicy": map[string]any{"filters": []any{}},
				"topicPolicy": map[string]any{
					"topics": []any{
						map[string]any{
							"name":   "off-topic",
							"type":   "DENY",
							"action": "BLOCKED",
						},
					},
				},
				"sensitiveInformationPolicy": map[string]any{"piiEntities": []any{}},
			},
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

// blockedPIIResponse returns a guardrail response blocked by PII policy.
func blockedPIIResponse(guardrailID, guardrailVersion string) []byte {
	resp := map[string]any{
		"action":  "GUARDRAIL_INTERVENED",
		"outputs": []any{},
		"assessments": []any{
			map[string]any{
				"appliedGuardrailDetails": map[string]any{
					"guardrailId":      guardrailID,
					"guardrailVersion": guardrailVersion,
				},
				"contentPolicy": map[string]any{"filters": []any{}},
				"topicPolicy":   map[string]any{"topics": []any{}},
				"sensitiveInformationPolicy": map[string]any{
					"piiEntities": []any{
						map[string]any{
							"type":   "EMAIL",
							"match":  "user@example.com",
							"action": "BLOCKED",
						},
					},
				},
			},
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

// maskedOutputResponse returns a guardrail response that masked content (not blocked).
func maskedOutputResponse(guardrailID, guardrailVersion, maskedText string) []byte {
	resp := map[string]any{
		"action": "GUARDRAIL_INTERVENED",
		"outputs": []any{
			map[string]any{"text": maskedText},
		},
		"assessments": []any{
			map[string]any{
				"appliedGuardrailDetails": map[string]any{
					"guardrailId":      guardrailID,
					"guardrailVersion": guardrailVersion,
				},
				"contentPolicy":              map[string]any{"filters": []any{}},
				"topicPolicy":                map[string]any{"topics": []any{}},
				"sensitiveInformationPolicy": map[string]any{"piiEntities": []any{}},
			},
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

// --- Tests for applyGuardrailCallback.OnHttpCalloutDone ---

func TestOnHttpCalloutDone_CalloutFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockHandle.EXPECT().SendLocalResponse(uint32(http.StatusBadGateway), gomock.Any(), gomock.Any(), "").Times(1)

	a := &applyGuardrailCallback{
		handle: mockHandle,
		body:   []byte(`{"messages":[{"role":"user","content":"hi"}],"model":"gpt-4"}`),
		cfg: &bedrockGuardrailsConfig{
			BedrockGuardrails: []bedrockGuardrail{{Identifier: "g1", Version: "1"}},
		},
	}

	a.OnHttpCalloutDone(1, shared.HttpCalloutReset, [][2]string{}, [][]byte{})
}

func TestOnHttpCalloutDone_NonSuccessHTTPStatus(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockHandle.EXPECT().SendLocalResponse(uint32(http.StatusBadGateway), gomock.Any(), gomock.Any(), "").Times(1)

	a := &applyGuardrailCallback{
		handle: mockHandle,
		body:   []byte(`{"messages":[{"role":"user","content":"hi"}],"model":"gpt-4"}`),
		cfg: &bedrockGuardrailsConfig{
			BedrockGuardrails: []bedrockGuardrail{{Identifier: "g1", Version: "1"}},
		},
	}

	a.OnHttpCalloutDone(1, shared.HttpCalloutSuccess,
		[][2]string{{":status", "503"}},
		[][]byte{[]byte(`{"error":"service unavailable"}`)},
	)
}

func TestOnHttpCalloutDone_NoInterventionLastGuardrail(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	originalBody := []byte(`{"messages":[{"role":"user","content":"hello"}],"model":"gpt-4"}`)
	respBody := noInterventionResponse("g1", "1")

	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	fakeBuffer := fake.NewFakeBodyBuffer([]byte{})
	mockHandle.EXPECT().BufferedRequestBody().Return(fakeBuffer).Times(1)
	mockHandle.EXPECT().ContinueRequest().Times(1)

	a := &applyGuardrailCallback{
		handle: mockHandle,
		body:   originalBody,
		cfg: &bedrockGuardrailsConfig{
			Cluster:           "bedrock-cluster",
			BedrockGuardrails: []bedrockGuardrail{{Identifier: "g1", Version: "1"}},
		},
	}

	a.OnHttpCalloutDone(1, shared.HttpCalloutSuccess,
		[][2]string{{":status", "200"}},
		[][]byte{respBody},
	)

	// The original body should have been appended to the buffered body
	require.Equal(t, originalBody, fakeBuffer.Body)
}

func TestOnHttpCalloutDone_BlockedByContentPolicy(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockHandle.EXPECT().SendLocalResponse(uint32(http.StatusBadRequest), gomock.Any(), gomock.Any(), "").Times(1)

	a := &applyGuardrailCallback{
		handle: mockHandle,
		body:   []byte(`{"messages":[{"role":"user","content":"violent content"}],"model":"gpt-4"}`),
		cfg: &bedrockGuardrailsConfig{
			BedrockGuardrails: []bedrockGuardrail{{Identifier: "g1", Version: "1"}},
		},
	}

	a.OnHttpCalloutDone(1, shared.HttpCalloutSuccess,
		[][2]string{{":status", "200"}},
		[][]byte{blockedContentPolicyResponse("g1", "1")},
	)
}

func TestOnHttpCalloutDone_BlockedByTopicPolicy(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockHandle.EXPECT().SendLocalResponse(uint32(http.StatusBadRequest), gomock.Any(), gomock.Any(), "").Times(1)

	a := &applyGuardrailCallback{
		handle: mockHandle,
		body:   []byte(`{"messages":[{"role":"user","content":"off-topic question"}],"model":"gpt-4"}`),
		cfg: &bedrockGuardrailsConfig{
			BedrockGuardrails: []bedrockGuardrail{{Identifier: "g1", Version: "1"}},
		},
	}

	a.OnHttpCalloutDone(1, shared.HttpCalloutSuccess,
		[][2]string{{":status", "200"}},
		[][]byte{blockedTopicPolicyResponse("g1", "1")},
	)
}

func TestOnHttpCalloutDone_BlockedByPIIPolicy(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockHandle.EXPECT().SendLocalResponse(uint32(http.StatusBadRequest), gomock.Any(), gomock.Any(), "").Times(1)

	a := &applyGuardrailCallback{
		handle: mockHandle,
		body:   []byte(`{"messages":[{"role":"user","content":"my email is user@example.com"}],"model":"gpt-4"}`),
		cfg: &bedrockGuardrailsConfig{
			BedrockGuardrails: []bedrockGuardrail{{Identifier: "g1", Version: "1"}},
		},
	}

	a.OnHttpCalloutDone(1, shared.HttpCalloutSuccess,
		[][2]string{{":status", "200"}},
		[][]byte{blockedPIIResponse("g1", "1")},
	)
}

func TestOnHttpCalloutDone_MaskedLastGuardrail(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	originalBody := []byte(`{"messages":[{"role":"user","content":"my email is user@example.com"}],"model":"gpt-4"}`)
	maskedText := "my email is [EMAIL REDACTED]"
	respBody := maskedOutputResponse("g1", "1", maskedText)

	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	fakeBuffer := fake.NewFakeBodyBuffer([]byte{})
	mockHandle.EXPECT().BufferedRequestBody().Return(fakeBuffer).Times(1)
	mockHandle.EXPECT().ContinueRequest().Times(1)

	a := &applyGuardrailCallback{
		handle: mockHandle,
		body:   originalBody,
		cfg: &bedrockGuardrailsConfig{
			Cluster:           "bedrock-cluster",
			BedrockGuardrails: []bedrockGuardrail{{Identifier: "g1", Version: "1"}},
		},
	}

	a.OnHttpCalloutDone(1, shared.HttpCalloutSuccess,
		[][2]string{{":status", "200"}},
		[][]byte{respBody},
	)

	// The masked body should have been appended (not the original)
	require.NotEmpty(t, fakeBuffer.Body)
	var req CreateChatCompletionRequest
	require.NoError(t, json.Unmarshal(fakeBuffer.Body, &req))
	require.Len(t, req.Messages, 1)
	var content string
	require.NoError(t, json.Unmarshal(req.Messages[0].Content, &content))
	require.Equal(t, maskedText, content)
}

func TestOnHttpCalloutDone_NoInterventionTriggersNextGuardrail(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	originalBody := []byte(`{"messages":[{"role":"user","content":"hello"}],"model":"gpt-4"}`)
	respBody := noInterventionResponse("g1", "1")

	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockHandle.EXPECT().HttpCallout(
		"bedrock-cluster",
		gomock.Any(),
		gomock.Any(),
		gomock.Any(),
		gomock.Any(),
	).Return(shared.HttpCalloutInitSuccess, uint64(2)).Times(1)

	cfg := &bedrockGuardrailsConfig{
		Cluster: "bedrock-cluster",
		BedrockGuardrails: []bedrockGuardrail{
			{Identifier: "g1", Version: "1"},
			{Identifier: "g2", Version: "2"},
		},
		TimeoutMs: 10000,
	}

	a := &applyGuardrailCallback{
		handle: mockHandle,
		body:   originalBody,
		cfg:    cfg,
	}

	a.OnHttpCalloutDone(1, shared.HttpCalloutSuccess,
		[][2]string{{":status", "200"}},
		[][]byte{respBody},
	)
}

func TestOnHttpCalloutDone_MaskedTriggersNextGuardrail(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	originalBody := []byte(`{"messages":[{"role":"user","content":"my email is user@example.com"}],"model":"gpt-4"}`)
	maskedText := "my email is [REDACTED]"
	respBody := maskedOutputResponse("g1", "1", maskedText)

	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockHandle.EXPECT().HttpCallout(
		"bedrock-cluster",
		gomock.Any(),
		gomock.Any(),
		gomock.Any(),
		gomock.Any(),
	).Return(shared.HttpCalloutInitSuccess, uint64(2)).Times(1)

	cfg := &bedrockGuardrailsConfig{
		Cluster: "bedrock-cluster",
		BedrockGuardrails: []bedrockGuardrail{
			{Identifier: "g1", Version: "1"},
			{Identifier: "g2", Version: "2"},
		},
		TimeoutMs: 10000,
	}

	a := &applyGuardrailCallback{
		handle: mockHandle,
		body:   originalBody,
		cfg:    cfg,
	}

	a.OnHttpCalloutDone(1, shared.HttpCalloutSuccess,
		[][2]string{{":status", "200"}},
		[][]byte{respBody},
	)
}

func TestOnHttpCalloutDone_NextGuardrailCalloutFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	originalBody := []byte(`{"messages":[{"role":"user","content":"hello"}],"model":"gpt-4"}`)
	respBody := noInterventionResponse("g1", "1")

	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	// Next callout fails to initialize
	mockHandle.EXPECT().HttpCallout(
		gomock.Any(),
		gomock.Any(),
		gomock.Any(),
		gomock.Any(),
		gomock.Any(),
	).Return(shared.HttpCalloutInitMissingRequiredHeaders, uint64(0)).Times(1)
	// Should send local error response
	mockHandle.EXPECT().SendLocalResponse(uint32(http.StatusBadGateway), gomock.Any(), gomock.Any(), "").Times(1)

	cfg := &bedrockGuardrailsConfig{
		Cluster: "bedrock-cluster",
		BedrockGuardrails: []bedrockGuardrail{
			{Identifier: "g1", Version: "1"},
			{Identifier: "g2", Version: "2"},
		},
		TimeoutMs: 10000,
	}

	a := &applyGuardrailCallback{
		handle: mockHandle,
		body:   originalBody,
		cfg:    cfg,
	}

	a.OnHttpCalloutDone(1, shared.HttpCalloutSuccess,
		[][2]string{{":status", "200"}},
		[][]byte{respBody},
	)
}

func TestOnHttpCalloutDone_BodyMultipleChunks(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	originalBody := []byte(`{"messages":[{"role":"user","content":"hello"}],"model":"gpt-4"}`)
	// Split response across two chunks
	respFull := noInterventionResponse("g1", "1")
	half := len(respFull) / 2
	chunk1 := respFull[:half]
	chunk2 := respFull[half:]

	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	fakeBuffer := fake.NewFakeBodyBuffer([]byte{})
	mockHandle.EXPECT().BufferedRequestBody().Return(fakeBuffer).Times(1)
	mockHandle.EXPECT().ContinueRequest().Times(1)

	a := &applyGuardrailCallback{
		handle: mockHandle,
		body:   originalBody,
		cfg: &bedrockGuardrailsConfig{
			BedrockGuardrails: []bedrockGuardrail{{Identifier: "g1", Version: "1"}},
		},
	}

	a.OnHttpCalloutDone(1, shared.HttpCalloutSuccess,
		[][2]string{{":status", "200"}},
		[][]byte{chunk1, chunk2},
	)

	require.Equal(t, originalBody, fakeBuffer.Body)
}
