// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package llmproxy

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCustomFactory_Basics(t *testing.T) {
	factory := &customFactory{}
	reqBody := []byte(`{"model":"custom-model","stream":true}`)
	req, err := factory.ParseRequest(reqBody)
	require.NoError(t, err)
	require.Equal(t, "custom-model", req.GetModel())
	require.True(t, req.IsStream())

	respBody := []byte(`{"choices":[],"usage":{"prompt_tokens":5,"completion_tokens":10,"total_tokens":15}}`)
	resp, err := factory.ParseResponse(respBody)
	require.NoError(t, err)
	usage := resp.GetUsage()
	require.Equal(t, uint32(5), usage.InputTokens)
	require.Equal(t, uint32(10), usage.OutputTokens)
	require.Equal(t, uint32(15), usage.TotalTokens)

	sseParser := factory.NewSSEParser()
	require.NotNil(t, sseParser)

	// Create a legal SSE event with usage info in the data payload.
	event := []byte(`data: {"choices":[],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}` + "\n\n")
	err = sseParser.Feed(event)
	require.NoError(t, err)
	resp, err = sseParser.Finish()
	require.NoError(t, err)
	usage = resp.GetUsage()
	require.Equal(t, uint32(2), usage.InputTokens)
	require.Equal(t, uint32(3), usage.OutputTokens)
	require.Equal(t, uint32(5), usage.TotalTokens)
}
