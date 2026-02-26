// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package impl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/pkg"
)

const (
	defaultAPIVersion              = "2024-09-01"
	defaultTaskAdherenceAPIVersion = "2025-09-15-preview"
	defaultThreshold               = 2
)

// azureContentSafetyConfig holds the configuration for the Azure Content Safety client.
type azureContentSafetyConfig struct {
	Endpoint                string         `json:"endpoint"`
	APIKey                  pkg.DataSource `json:"api_key"`
	Mode                    string         `json:"mode"`
	FailOpen                bool           `json:"fail_open"`
	APIVersion              string         `json:"api_version"`
	HateThreshold           *int           `json:"hate_threshold"`
	SelfHarmThreshold       *int           `json:"self_harm_threshold"`
	SexualThreshold         *int           `json:"sexual_threshold"`
	ViolenceThreshold       *int           `json:"violence_threshold"`
	Categories              []string       `json:"categories"`
	EnableProtectedMaterial bool           `json:"enable_protected_material"`
	EnableTaskAdherence     bool           `json:"enable_task_adherence"`
	TaskAdherenceAPIVersion string         `json:"task_adherence_api_version"`
}

func (c *azureContentSafetyConfig) apiVersion() string {
	if c.APIVersion != "" {
		return c.APIVersion
	}
	return defaultAPIVersion
}

func (c *azureContentSafetyConfig) taskAdherenceAPIVersion() string {
	if c.TaskAdherenceAPIVersion != "" {
		return c.TaskAdherenceAPIVersion
	}
	return defaultTaskAdherenceAPIVersion
}

func (c *azureContentSafetyConfig) threshold(category string) int {
	switch strings.ToLower(category) {
	case "hate":
		if c.HateThreshold != nil {
			return *c.HateThreshold
		}
	case "selfharm":
		if c.SelfHarmThreshold != nil {
			return *c.SelfHarmThreshold
		}
	case "sexual":
		if c.SexualThreshold != nil {
			return *c.SexualThreshold
		}
	case "violence":
		if c.ViolenceThreshold != nil {
			return *c.ViolenceThreshold
		}
	}
	return defaultThreshold
}

func (c *azureContentSafetyConfig) categories() []string {
	if len(c.Categories) > 0 {
		return c.Categories
	}
	return []string{"Hate", "SelfHarm", "Sexual", "Violence"}
}

func (c *azureContentSafetyConfig) isBlockMode() bool {
	return c.Mode != "monitor"
}

// promptShieldRequest is the request body for the Prompt Shield API.
type promptShieldRequest struct {
	UserPrompt string   `json:"userPrompt"`
	Documents  []string `json:"documents"`
}

// promptShieldResponse is the response from the Prompt Shield API.
type promptShieldResponse struct {
	UserPromptAnalysis *promptAnalysis  `json:"userPromptAnalysis"`
	DocumentsAnalysis  []promptAnalysis `json:"documentsAnalysis"`
}

// promptAnalysis contains the analysis result for a single prompt or document.
type promptAnalysis struct {
	AttackDetected bool `json:"attackDetected"`
}

// textAnalyzeRequest is the request body for the Text Analysis API.
type textAnalyzeRequest struct {
	Text       string   `json:"text"`
	Categories []string `json:"categories"`
	OutputType string   `json:"outputType"`
}

// textAnalyzeResponse is the response from the Text Analysis API.
type textAnalyzeResponse struct {
	CategoriesAnalysis []categoryAnalysis `json:"categoriesAnalysis"`
}

// categoryAnalysis contains the analysis result for a single category.
type categoryAnalysis struct {
	Category string `json:"category"`
	Severity int    `json:"severity"`
}

// protectedMaterialRequest is the request body for the Protected Material Detection API.
type protectedMaterialRequest struct {
	Text string `json:"text"`
}

// protectedMaterialResponse is the response from the Protected Material Detection API.
type protectedMaterialResponse struct {
	ProtectedMaterialAnalysis *protectedMaterialAnalysis `json:"protectedMaterialAnalysis"`
}

// protectedMaterialAnalysis contains the analysis result for protected material detection.
type protectedMaterialAnalysis struct {
	Detected bool `json:"detected"`
}

// taskAdherenceRequest is the request body for the Task Adherence API.
type taskAdherenceRequest struct {
	Tools    []taskAdherenceTool    `json:"tools"`
	Messages []taskAdherenceMessage `json:"messages"`
}

// taskAdherenceTool represents a tool definition in the Task Adherence API format.
type taskAdherenceTool struct {
	Type     string                    `json:"type"`
	Function taskAdherenceToolFunction `json:"function"`
}

// taskAdherenceToolFunction represents a function definition within a tool.
type taskAdherenceToolFunction struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// taskAdherenceMessage represents a message in the Task Adherence API format.
type taskAdherenceMessage struct {
	Source     string                  `json:"source"`
	Role       string                  `json:"role"`
	Contents   string                  `json:"contents"`
	ToolCalls  []taskAdherenceToolCall `json:"toolCalls,omitempty"`
	ToolCallID string                  `json:"toolCallId,omitempty"`
}

// taskAdherenceToolCall represents a tool call in the Task Adherence API format.
type taskAdherenceToolCall struct {
	ID       string                        `json:"id"`
	Type     string                        `json:"type"`
	Function taskAdherenceToolCallFunction `json:"function"`
}

// taskAdherenceToolCallFunction represents the function details within a tool call.
type taskAdherenceToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// taskAdherenceResponse is the response from the Task Adherence API.
type taskAdherenceResponse struct {
	TaskRiskDetected bool   `json:"taskRiskDetected"`
	Details          string `json:"details"`
}

// logFunc is a callback for emitting debug log messages.
type logFunc func(format string, args ...any)

// azureContentSafetyClient communicates with the Azure Content Safety APIs.
type azureContentSafetyClient struct {
	httpClient *http.Client
	endpoint   string
	apiKey     string
	apiVersion string
	logFunc    logFunc
}

// newAzureContentSafetyClient creates a new client for the Azure Content Safety APIs.
func newAzureContentSafetyClient(endpoint, apiKey, apiVersion string, logFunc logFunc) *azureContentSafetyClient {
	return &azureContentSafetyClient{
		httpClient: &http.Client{Timeout: 5 * time.Second},
		endpoint:   strings.TrimRight(endpoint, "/"),
		apiKey:     apiKey,
		apiVersion: apiVersion,
		logFunc:    logFunc,
	}
}

// ShieldPrompt calls the Prompt Shield API to detect prompt injection attacks.
func (c *azureContentSafetyClient) ShieldPrompt(userPrompt string, documents []string) (*promptShieldResponse, error) {
	reqBody := promptShieldRequest{
		UserPrompt: userPrompt,
		Documents:  documents,
	}

	url := fmt.Sprintf("%s/contentsafety/text:shieldPrompt?api-version=%s", c.endpoint, c.apiVersion)

	var result promptShieldResponse
	if err := c.doRequest(url, reqBody, &result); err != nil {
		return nil, fmt.Errorf("prompt shield request failed: %w", err)
	}
	return &result, nil
}

// AnalyzeText calls the Text Analysis API to detect harmful content.
func (c *azureContentSafetyClient) AnalyzeText(text string, categories []string) (*textAnalyzeResponse, error) {
	reqBody := textAnalyzeRequest{
		Text:       text,
		Categories: categories,
		OutputType: "FourSeverityLevels",
	}

	url := fmt.Sprintf("%s/contentsafety/text:analyze?api-version=%s", c.endpoint, c.apiVersion)

	var result textAnalyzeResponse
	if err := c.doRequest(url, reqBody, &result); err != nil {
		return nil, fmt.Errorf("text analysis request failed: %w", err)
	}
	return &result, nil
}

// DetectProtectedMaterial calls the Protected Material Detection API to detect copyrighted text.
func (c *azureContentSafetyClient) DetectProtectedMaterial(text string) (*protectedMaterialResponse, error) {
	reqBody := protectedMaterialRequest{
		Text: text,
	}

	url := fmt.Sprintf("%s/contentsafety/text:detectProtectedMaterial?api-version=%s", c.endpoint, c.apiVersion)

	var result protectedMaterialResponse
	if err := c.doRequest(url, reqBody, &result); err != nil {
		return nil, fmt.Errorf("protected material detection request failed: %w", err)
	}
	return &result, nil
}

// AnalyzeTaskAdherence calls the Task Adherence API to detect misaligned tool invocations.
func (c *azureContentSafetyClient) AnalyzeTaskAdherence(req *taskAdherenceRequest, apiVersion string) (*taskAdherenceResponse, error) {
	url := fmt.Sprintf("%s/contentsafety/agent:analyzeTaskAdherence?api-version=%s", c.endpoint, apiVersion)

	var result taskAdherenceResponse
	if err := c.doRequest(url, req, &result); err != nil {
		return nil, fmt.Errorf("task adherence analysis request failed: %w", err)
	}
	return &result, nil
}

func (c *azureContentSafetyClient) doRequest(url string, reqBody any, result any) error {
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	if c.logFunc != nil {
		c.logFunc("azure-content-safety: API request: POST %s, body: %s", url, string(bodyBytes))
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Ocp-Apim-Subscription-Key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	if c.logFunc != nil {
		c.logFunc("azure-content-safety: API response: status=%d, body: %s", resp.StatusCode, string(respBody))
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	if err := json.Unmarshal(respBody, result); err != nil {
		return fmt.Errorf("failed to unmarshal response: %w", err)
	}
	return nil
}
