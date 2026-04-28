// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package impl contains the implementation of the bedrock-guardrails extension.
package impl

import "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"

// bedrockGuardrail represents a single guardrail configuration.
type bedrockGuardrail struct {
	// Identifier is the unique identifier for the guardrail.
	Identifier string `json:"identifier"`
	// Version is the version of the guardrail to apply.
	Version string `json:"version"`
}

// bedrockGuardrailsConfig represents the JSON configuration for this filter.
type bedrockGuardrailsConfig struct { //nolint:revive
	// BedrockEndpoint is the AWS Bedrock API endpoint to use
	BedrockEndpoint string `json:"bedrock_endpoint"`
	// Cluster is the Envoy cluster name that represents the Bedrock endpoint.
	Cluster string `json:"bedrock_cluster"`
	// BedrockAPIKey is the API key for authenticating with Bedrock Guardrails.
	BedrockAPIKey string `json:"bedrock_api_key"`
	// BedrockGuardrails is the list of guardrails to apply, each with an identifier and version.
	BedrockGuardrails []bedrockGuardrail `json:"bedrock_guardrails"`
	// TimeoutMs is the Bedrock API request timeout
	TimeoutMs uint64 `json:"bedrock_timeoutms"`
}

// Text represents the text content in the guardrail request.
type Text struct {
	Text string `json:"text"`
}

// Content represents a single content item in the guardrail request, which currently only supports text.
type Content struct {
	Text Text `json:"text"`
}

// ApplyGuardrailRequest represents the JSON body of the request to apply a guardrail.
type ApplyGuardrailRequest struct {
	Source  string    `json:"source"`
	Content []Content `json:"content"`
}

// ApplyGuardrailArgs represents the arguments needed to apply a guardrail, including the guardrail identifier, version, and the request body.
type ApplyGuardrailArgs struct {
	GuardrailIdentifier string
	GuardrailVersion    string
	Body                []byte
	Handle              shared.HttpFilterHandle
	Endpoint            string
	APIKey              string
}

// ApplyGuardrailResponse represents the JSON response from the Bedrock Guardrails API after applying a guardrail, including the action to take and any outputs.
type ApplyGuardrailResponse struct {
	Action  string `json:"action"`
	Outputs []struct {
		Text string `json:"text"`
	} `json:"outputs"`
	Assessments []struct {
		AppliedGuardrailDetails struct {
			GuardrailID      string `json:"guardrailId"`
			GuardrailVersion string `json:"guardrailVersion"`
		} `json:"appliedGuardrailDetails"`
		ContentPolicy struct {
			Filters []struct {
				Action         string `json:"action"`
				Confidence     string `json:"confidence"`
				Detected       bool   `json:"detected"`
				FilterStrength string `json:"filterStrength"`
				Type           string `json:"type"`
			} `json:"filters"`
		} `json:"contentPolicy"`
		TopicPolicy struct {
			Topics []struct {
				Name   string `json:"name"`
				Type   string `json:"type"`
				Action string `json:"action"`
			} `json:"topics"`
		} `json:"topicPolicy"`
		SensitiveInformationPolicy struct {
			PiiEntities []struct {
				Type   string `json:"type"`
				Match  string `json:"match"`
				Action string `json:"action"`
			} `json:"piiEntities"`
			Regexes []any `json:"regexes"`
		} `json:"sensitiveInformationPolicy"`
	} `json:"assessments"`
}
