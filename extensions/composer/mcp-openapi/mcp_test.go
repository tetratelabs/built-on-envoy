// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package mcpopenapi

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormatToolName(t *testing.T) {
	tests := []struct {
		method   string
		path     string
		expected string
	}{
		{"GET", "/v1/forecast", "get_v1_forecast"},
		{"POST", "/pet", "post_pet"},
		{"GET", "/pet/{petId}", "get_pet_petId"},
		{"DELETE", "/store/order/{orderId}", "delete_store_order_orderId"},
		{"PUT", "/users/{userId}/profile", "put_users_userId_profile"},
		{"PATCH", "/api/v2/items.json", "patch_api_v2_items_json"},
		{"GET", "v1/forecast", "get_v1_forecast"},          // no leading slash
		{"POST", "/users//profile/", "post_users_profile"}, // multiple slashes
		{"GET", "/pet/{petId}/uploadImage", "get_pet_petId_uploadImage"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			require.Equal(t, tt.expected, formatToolName(tt.method, tt.path))
		})
	}
}

func TestDescriptionOf(t *testing.T) {
	tests := []struct {
		name       string
		extensions map[string]any
		desc       string
		summary    string
		fallback   string
		expected   string
	}{
		{
			name:       "x-mcp-description wins",
			extensions: map[string]any{"x-mcp-description": "custom desc"},
			desc:       "normal desc",
			summary:    "summary",
			fallback:   "fallback",
			expected:   "custom desc",
		},
		{
			name:     "description wins over summary",
			desc:     "normal desc",
			summary:  "summary",
			fallback: "fallback",
			expected: "normal desc",
		},
		{
			name:     "summary wins over fallback",
			summary:  "summary",
			fallback: "fallback",
			expected: "summary",
		},
		{
			name:     "fallback with Calls prefix",
			fallback: "getPetById",
			expected: "Calls getPetById",
		},
		{
			name:       "empty x-mcp-description falls through",
			extensions: map[string]any{"x-mcp-description": ""},
			desc:       "normal desc",
			expected:   "normal desc",
		},
		{
			name:       "non-string x-mcp-description ignored",
			extensions: map[string]any{"x-mcp-description": 123},
			desc:       "normal desc",
			expected:   "normal desc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ext := tt.extensions
			if ext == nil {
				ext = map[string]any{}
			}
			require.Equal(t, tt.expected, descriptionOf(ext, tt.desc, tt.summary, tt.fallback))
		})
	}
}
