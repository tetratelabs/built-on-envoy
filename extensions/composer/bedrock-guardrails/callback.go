// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package impl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
)

func getCalloutHeaders(args *ApplyGuardrailArgs) ([][2]string, []byte, error) {
	path := fmt.Sprintf("/guardrail/%s/version/%s/apply", args.GuardrailIdentifier, args.GuardrailVersion)
	// Request body
	content, err := getContent(args.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("getting content: %w", err)
	}
	r := ApplyGuardrailRequest{
		Source:  "INPUT",
		Content: content,
	}
	b, err := json.Marshal(r)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal request: %w", err)
	}

	// Request headers
	return [][2]string{
		{":method", http.MethodPost},
		{":path", path},
		{":authority", args.Endpoint},
		{":scheme", "https"},
		{"Authorization", "Bearer " + args.APIKey},
		{"Content-type", "application/json"},
	}, b, nil
}

type applyGuardrailCallback struct {
	handle shared.HttpFilterHandle
	body   []byte
	cfg    *bedrockGuardrailsConfig

	index int
}

func (a *applyGuardrailCallback) OnHttpCalloutDone(_ uint64, result shared.HttpCalloutResult, headers [][2]string, body [][]byte) { //nolint:revive
	a.handle.Log(shared.LogLevelDebug, "bedrock-guardrails: Started Callout callback")

	fullbody := joinBody(body)

	// Check callout succeeded
	if result != shared.HttpCalloutSuccess {
		sendLocalRespError(a.handle, shared.LogLevelError, http.StatusBadGateway, fmt.Sprintf("failed to apply guardrail, result: %v", result), fullbody)
		return
	}

	// Check the actual HTTP request succeeded
	statusCode := headerValue(headers, ":status")
	if !strings.HasPrefix(statusCode, "2") {
		// Request failed
		sendLocalRespError(a.handle, shared.LogLevelError, http.StatusBadGateway, fmt.Sprintf("failed to apply guardrail, status: %v", statusCode), fullbody)
		return
	}

	var (
		resp *ApplyGuardrailResponse
		err  error
	)

	newBody := a.body
	blocked := false
	var messages []string
	if e := json.Unmarshal(fullbody, &resp); e != nil {
		sendLocalRespError(a.handle, shared.LogLevelError, http.StatusBadGateway, fmt.Sprintf("failed to unmarshal response: %v", e.Error()), fullbody)
		return
	}
	a.handle.Log(shared.LogLevelDebug, "bedrock-guardrails: got response back: %+v", resp)

	// Guardrail took action, figure out if request is blocked and build the list of messages to replace, if any
	if resp.Action == "GUARDRAIL_INTERVENED" {
		if !blocked {
			// Avoid parsing more assesements if we are already blocked
			for i := range resp.Assessments {
				assesment := &resp.Assessments[i]
				for _, f := range assesment.ContentPolicy.Filters {
					if f.Action == "BLOCKED" {
						a.handle.Log(shared.LogLevelDebug, "bedrock-guardrails: request has been blocked")
						blocked = true
						break
					}
				}
				for _, topic := range assesment.TopicPolicy.Topics {
					if topic.Action == "BLOCKED" {
						a.handle.Log(shared.LogLevelDebug, "bedrock-guardrails: request has been blocked")
						blocked = true
						break
					}
				}
				for _, p := range assesment.SensitiveInformationPolicy.PiiEntities {
					if p.Action == "BLOCKED" {
						a.handle.Log(shared.LogLevelDebug, "bedrock-guardrails: request has been blocked")
						blocked = true
						break
					}
				}
			}
		}
	}
	if len(resp.Outputs) > 0 {
		for _, output := range resp.Outputs {
			messages = append(messages, output.Text)
		}
	}
	a.handle.Log(shared.LogLevelDebug, "bedrock-guardrails: got messages: %+v", messages)

	// Request blocked by some guardrail
	if blocked {
		a.handle.Log(shared.LogLevelInfo, "bedrock-guardrails: guardrail blocked the request")
		newBodyBytes, e := json.Marshal(map[string]any{
			"messages": messages,
		})
		if e != nil {
			sendLocalRespError(a.handle, shared.LogLevelDebug, http.StatusBadRequest, fmt.Sprintf("bedrock-guardrails: failed to marshal modified request body: %v", err), newBodyBytes)
			return
		}
		// If one guardrail blocks the request, we are going to return error right away
		sendLocalRespError(a.handle, shared.LogLevelDebug, http.StatusBadRequest, "bedrock-guardrails: request blocked by guardrail", newBodyBytes)
		return
	}

	// Since no guardrail blocked the request, but we have messages, it means some have been masked.
	// We need to put them back into the request, replace the request body so it can continue
	// traversing the proxy.
	if len(messages) > 0 {
		a.handle.Log(shared.LogLevelDebug, "bedrock-guardrails: guardrails triggered, modifying request")
		a.handle.Log(shared.LogLevelError, "bedrock-guardrails: original body: '%s'", string(a.body))
		newBody, err = ReplaceUserPrompts(a.body, messages)
		if err != nil {
			a.handle.Log(shared.LogLevelError, "bedrock-guardrails: failed to marshal modified request body: %v", err)
			a.handle.Log(shared.LogLevelError, "bedrock-guardrails: we will fail the request due to previous error")
			sendLocalRespError(a.handle, shared.LogLevelDebug, http.StatusBadGateway, "bedrock-guardrails: request blocked by guardrail", newBody)
			return
		}
		a.handle.Log(shared.LogLevelError, "bedrock-guardrails: sending new body: '%s'", string(newBody))
	}

	// This guardrail is completed, update its info
	appliedGuardrail := resp.Assessments[0].AppliedGuardrailDetails
	a.handle.Log(shared.LogLevelDebug, "bedrock-guardrails: completed guardrail %s v %s", appliedGuardrail.GuardrailID, appliedGuardrail.GuardrailVersion)

	nextIndex := a.index + 1
	if nextIndex < len(a.cfg.BedrockGuardrails) {
		// Trigger next guardrail, if any
		nextGuardrail := a.cfg.BedrockGuardrails[nextIndex]
		a.handle.Log(shared.LogLevelDebug, "bedrock-guardrails: next guardrail %+v", nextGuardrail)
		args := &ApplyGuardrailArgs{
			GuardrailIdentifier: nextGuardrail.Identifier,
			GuardrailVersion:    nextGuardrail.Version,
			Body:                newBody,
			Handle:              a.handle,
			Endpoint:            a.cfg.BedrockEndpoint,
			APIKey:              a.cfg.BedrockAPIKey,
		}
		a.handle.Log(shared.LogLevelDebug, "bedrock-guardrails: applying guardrail %s version %s", nextGuardrail.Identifier, nextGuardrail.Version)

		calloutHeaders, calloutBody, err := getCalloutHeaders(args)
		if err != nil {
			sendLocalRespError(a.handle, shared.LogLevelDebug, http.StatusBadGateway, "bedrock-guardrails: getting callout headers", []byte(err.Error()))
			return
		}
		a.handle.Log(shared.LogLevelDebug, "bedrock-guardrails: got callout headers: %+v", calloutHeaders)
		newResult, cid := a.handle.HttpCallout(
			a.cfg.Cluster,
			calloutHeaders,
			calloutBody,
			a.cfg.TimeoutMs,
			&applyGuardrailCallback{
				cfg:    a.cfg,
				handle: a.handle,
				body:   newBody,
				index:  nextIndex,
			},
		)
		if newResult != shared.HttpCalloutInitSuccess {
			sendLocalRespError(a.handle, shared.LogLevelDebug, http.StatusBadGateway, fmt.Sprintf("bedrock-guardrails: execute callout status=%v", newResult), []byte{})
			return
		}
		a.handle.Log(shared.LogLevelDebug, "bedrock-guardrails: http callout sent ID: %v", cid)
		return
	}

	// no more processing to do
	a.handle.Log(shared.LogLevelDebug, "bedrock-guardrails: all guardrails completed, continuing request")
	// Clear body, set new one
	a.handle.BufferedRequestBody().Append(newBody)
	a.handle.ContinueRequest()
}

// joinBody returns the body as a single byte slice, avoiding to call bytes.Join
// when there is only one chunk which always returns a copy.
func joinBody(body [][]byte) []byte {
	if len(body) == 1 {
		return body[0]
	}
	return bytes.Join(body, nil)
}

// sendLocalRespError logs the message (with optional raw body), sends
// a local response with the message, and returns HeadersStatusStop.
func sendLocalRespError(handle shared.HttpFilterHandle, level shared.LogLevel, status uint32, msg string, rawBody []byte) shared.HeadersStatus {
	if len(rawBody) > 0 {
		handle.Log(level, "bedrock-guardrails: %s, raw_body=%s", msg, rawBody)
	} else {
		handle.Log(level, "bedrock-guardrails: %s", msg)
	}
	handle.SendLocalResponse(status, [][2]string{{"content-type", "text/plain"}}, []byte(msg), "")
	return shared.HeadersStatusStop
}

// headerValue returns the first value for a key in a [][2]string header list.
func headerValue(headers [][2]string, key string) string {
	for _, h := range headers {
		if h[0] == key {
			return h[1]
		}
	}
	return ""
}
