// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package openfga

import (
	"encoding/json"
	"testing"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/fake"
	"github.com/stretchr/testify/require"
)

func TestParseConfig_Legacy(t *testing.T) {
	cfg := openfgaConfig{
		Cluster:     "openfga",
		OpenFGAHost: "openfga:8080",
		StoreID:     "store1",
		User:        valueSource{Header: "x-user-id", Prefix: "user:"},
		Relation:    valueSource{Value: "can_use"},
		Object:      valueSource{Header: "x-ai-model", Prefix: "model:"},
	}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)

	parsed, err := parseConfig(data)
	require.NoError(t, err)
	require.Len(t, parsed.rules, 1)
	require.Nil(t, parsed.rules[0].match)
	require.Equal(t, "x-user-id", parsed.rules[0].user.Header)
	require.Equal(t, "can_use", parsed.rules[0].relation.Value)
	require.Equal(t, "x-ai-model", parsed.rules[0].object.Header)
	require.Equal(t, uint64(5000), parsed.timeoutMs)
	require.Equal(t, 403, parsed.deny.Status)
	require.Equal(t, "Forbidden", parsed.deny.Body)
}

func TestParseConfig_LegacyDefaults(t *testing.T) {
	cfg := openfgaConfig{
		Cluster:     "openfga",
		OpenFGAHost: "openfga:8080",
		StoreID:     "store1",
		User:        valueSource{Header: "x-user-id"},
		Relation:    valueSource{Value: "reader"},
		Object:      valueSource{Header: "x-resource"},
		TimeoutMs:   10000,
		DenyStatus:  401,
		DenyBody:    "Unauthorized",
	}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)

	parsed, err := parseConfig(data)
	require.NoError(t, err)
	require.Equal(t, uint64(10000), parsed.timeoutMs)
	require.Equal(t, 401, parsed.deny.Status)
	require.Equal(t, "Unauthorized", parsed.deny.Body)
}

func TestParseConfig_MultiRule(t *testing.T) {
	cfg := openfgaConfig{
		Cluster:     "openfga",
		OpenFGAHost: "openfga:8080",
		StoreID:     "store1",
		User:        valueSource{Header: "x-user-id", Prefix: "user:"},
		Rules: []checkRule{
			{
				Match:    &ruleMatch{Headers: map[string]string{"x-ai-eg-model": "*"}},
				Relation: valueSource{Value: "can_use"},
				Object:   valueSource{Header: "x-ai-eg-model", Prefix: "model:"},
			},
			{
				Match:    &ruleMatch{Headers: map[string]string{"x-mcp-tool": "*"}},
				Relation: valueSource{Value: "can_invoke"},
				Object:   valueSource{Header: "x-mcp-tool", Prefix: "tool:"},
			},
			{
				Relation: valueSource{Value: "can_access"},
				Object:   valueSource{Header: "x-resource-id", Prefix: "resource:"},
			},
		},
	}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)

	parsed, err := parseConfig(data)
	require.NoError(t, err)
	require.Len(t, parsed.rules, 3)

	require.NotNil(t, parsed.rules[0].match)
	require.Equal(t, "can_use", parsed.rules[0].relation.Value)
	require.Equal(t, "user:", parsed.rules[0].user.Prefix)

	require.NotNil(t, parsed.rules[1].match)
	require.Equal(t, "can_invoke", parsed.rules[1].relation.Value)

	require.Nil(t, parsed.rules[2].match)
	require.Equal(t, "can_access", parsed.rules[2].relation.Value)
}

func TestParseConfig_MultiRule_UserOverride(t *testing.T) {
	customUser := valueSource{Header: "x-service-account", Prefix: "sa:"}
	cfg := openfgaConfig{
		Cluster:     "openfga",
		OpenFGAHost: "openfga:8080",
		StoreID:     "store1",
		User:        valueSource{Header: "x-user-id", Prefix: "user:"},
		Rules: []checkRule{
			{
				User:     &customUser,
				Relation: valueSource{Value: "can_invoke"},
				Object:   valueSource{Header: "x-mcp-tool", Prefix: "tool:"},
			},
		},
	}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)

	parsed, err := parseConfig(data)
	require.NoError(t, err)
	require.Len(t, parsed.rules, 1)
	require.Equal(t, "x-service-account", parsed.rules[0].user.Header)
	require.Equal(t, "sa:", parsed.rules[0].user.Prefix)
}

func TestParseConfig_CatchAllNotLast(t *testing.T) {
	cfg := openfgaConfig{
		Cluster:     "openfga",
		OpenFGAHost: "openfga:8080",
		StoreID:     "store1",
		User:        valueSource{Header: "x-user-id"},
		Rules: []checkRule{
			{
				Relation: valueSource{Value: "can_access"},
				Object:   valueSource{Header: "x-resource"},
			},
			{
				Match:    &ruleMatch{Headers: map[string]string{"x-ai-model": "*"}},
				Relation: valueSource{Value: "can_use"},
				Object:   valueSource{Header: "x-ai-model"},
			},
		},
	}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)

	_, err = parseConfig(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "catch-all rule (no match) must be last")
}

func TestParseConfig_EmptyConfig(t *testing.T) {
	_, err := parseConfig(nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "configuration is required")
}

func TestParseConfig_MissingCluster(t *testing.T) {
	cfg := openfgaConfig{
		OpenFGAHost: "openfga:8080",
		StoreID:     "store1",
		User:        valueSource{Header: "x-user-id"},
		Relation:    valueSource{Value: "reader"},
		Object:      valueSource{Header: "x-resource"},
	}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)

	_, err = parseConfig(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing required field: cluster")
}

func TestParseConfig_AccumulatesAllErrors(t *testing.T) {
	cfg := openfgaConfig{
		// Missing cluster, openfga_host, store_id — plus invalid consistency.
		Consistency: "BOGUS",
		User:        valueSource{Header: "x-user-id"},
		Relation:    valueSource{Value: "reader"},
		Object:      valueSource{Header: "x-resource"},
	}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)

	_, err = parseConfig(data)
	require.Error(t, err)
	msg := err.Error()
	require.Contains(t, msg, "missing required field: cluster")
	require.Contains(t, msg, "missing required field: openfga_host")
	require.Contains(t, msg, "missing required field: store_id")
	require.Contains(t, msg, "consistency")
}

func TestParseConfig_MissingHost(t *testing.T) {
	cfg := openfgaConfig{
		Cluster:  "openfga",
		StoreID:  "store1",
		User:     valueSource{Header: "x-user-id"},
		Relation: valueSource{Value: "reader"},
		Object:   valueSource{Header: "x-resource"},
	}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)

	_, err = parseConfig(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing required field: openfga_host")
}

func TestParseConfig_MultiRule_MissingTopLevelUser(t *testing.T) {
	cfg := openfgaConfig{
		Cluster:     "openfga",
		OpenFGAHost: "openfga:8080",
		StoreID:     "store1",
		Rules: []checkRule{
			{
				Relation: valueSource{Value: "can_access"},
				Object:   valueSource{Header: "x-resource"},
			},
		},
	}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)

	_, err = parseConfig(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "top-level user is required")
}

func TestParseConfig_MultiRule_InvalidRuleRelation(t *testing.T) {
	cfg := openfgaConfig{
		Cluster:     "openfga",
		OpenFGAHost: "openfga:8080",
		StoreID:     "store1",
		User:        valueSource{Header: "x-user-id"},
		Rules: []checkRule{
			{
				Relation: valueSource{},
				Object:   valueSource{Header: "x-resource"},
			},
		},
	}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)

	_, err = parseConfig(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "rule[0].relation")
}

func TestParseConfig_DenyStatusInvalid(t *testing.T) {
	cfg := openfgaConfig{
		Cluster:     "openfga",
		OpenFGAHost: "openfga:8080",
		StoreID:     "store1",
		User:        valueSource{Header: "x-user-id"},
		Relation:    valueSource{Value: "reader"},
		Object:      valueSource{Header: "x-resource"},
		DenyStatus:  99,
	}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)

	_, err = parseConfig(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "deny_status must be between 100 and 599")

	cfg.DenyStatus = 600
	data, _ = json.Marshal(cfg)
	_, err = parseConfig(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "deny_status must be between 100 and 599")
}

func TestParseConfig_CheckPath(t *testing.T) {
	cfg := openfgaConfig{
		Cluster:     "openfga",
		OpenFGAHost: "openfga:8080",
		StoreID:     "STORE_ABC",
		User:        valueSource{Header: "x-user-id"},
		Relation:    valueSource{Value: "reader"},
		Object:      valueSource{Header: "x-resource"},
	}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)

	parsed, err := parseConfig(data)
	require.NoError(t, err)
	require.Equal(t, "/stores/STORE_ABC/check", parsed.checkPath)
	found := false
	for _, h := range parsed.calloutHeaders {
		if h[0] == ":path" {
			require.Equal(t, "/stores/STORE_ABC/check", h[1])
			found = true
		}
	}
	require.True(t, found, "expected :path in callout headers")
}

func TestRuleMatch_Wildcard(t *testing.T) {
	m := &ruleMatch{Headers: map[string]string{"x-model": "*"}}

	headers := fake.NewFakeHeaderMap(map[string][]string{
		"x-model": {"gpt-4"},
	})
	require.True(t, m.matches(headers))

	empty := fake.NewFakeHeaderMap(map[string][]string{})
	require.False(t, m.matches(empty))
}

func TestRuleMatch_ExactValue(t *testing.T) {
	m := &ruleMatch{Headers: map[string]string{"x-route-type": "ai"}}

	match := fake.NewFakeHeaderMap(map[string][]string{
		"x-route-type": {"ai"},
	})
	require.True(t, m.matches(match))

	noMatch := fake.NewFakeHeaderMap(map[string][]string{
		"x-route-type": {"mcp"},
	})
	require.False(t, m.matches(noMatch))
}

func TestRuleMatch_MultipleHeaders(t *testing.T) {
	m := &ruleMatch{Headers: map[string]string{
		"x-model":   "*",
		"x-version": "v2",
	}}

	both := fake.NewFakeHeaderMap(map[string][]string{
		"x-model":   {"gpt-4"},
		"x-version": {"v2"},
	})
	require.True(t, m.matches(both))

	missingOne := fake.NewFakeHeaderMap(map[string][]string{
		"x-model": {"gpt-4"},
	})
	require.False(t, m.matches(missingOne))

	wrongValue := fake.NewFakeHeaderMap(map[string][]string{
		"x-model":   {"gpt-4"},
		"x-version": {"v1"},
	})
	require.False(t, m.matches(wrongValue))
}

func TestValueSource_Resolve(t *testing.T) {
	headers := fake.NewFakeHeaderMap(map[string][]string{
		"x-user-id": {"alice"},
	})

	vs := valueSource{Header: "x-user-id", Prefix: "user:"}
	require.Equal(t, "user:alice", vs.resolve(headers))

	static := valueSource{Value: "can_use"}
	require.Equal(t, "can_use", static.resolve(headers))

	missing := valueSource{Header: "x-missing"}
	require.Empty(t, missing.resolve(headers))
}

func TestValueSource_PathSegment(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		idx    int
		prefix string
		want   string
	}{
		{"segment 0", "/api/documents/planning", 0, "", "api"},
		{"segment 1 with prefix", "/api/documents/planning", 1, "doc:", "doc:documents"},
		{"segment 2 last", "/api/documents/planning", 2, "resource:", "resource:planning"},
		{"negative -1 is last", "/api/documents/planning", -1, "", "planning"},
		{"negative -2", "/api/documents/planning", -2, "", "documents"},
		{"out of range high", "/api/documents/planning", 5, "", ""},
		{"out of range negative", "/api/documents/planning", -10, "", ""},
		{"query string stripped", "/api/docs/budget?v=2", 2, "", "budget"},
		{"root path", "/", 0, "", ""},
		{"single segment", "/health", 0, "", "health"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idx := tt.idx
			vs := valueSource{PathSegment: &idx, Prefix: tt.prefix}
			headers := fake.NewFakeHeaderMap(map[string][]string{":path": {tt.path}})
			require.Equal(t, tt.want, vs.resolve(headers))
		})
	}
}

func TestValueSource_QueryParam(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		param  string
		prefix string
		want   string
	}{
		{"present", "/api?resource=planning", "resource", "doc:", "doc:planning"},
		{"absent", "/api?other=val", "resource", "", ""},
		{"no query string", "/api/resource", "resource", "", ""},
		{"multi-value first wins", "/api?resource=a&resource=b", "resource", "", "a"},
		{"url-encoded value", "/api?resource=my%20doc", "resource", "", "my doc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vs := valueSource{QueryParam: tt.param, Prefix: tt.prefix}
			headers := fake.NewFakeHeaderMap(map[string][]string{":path": {tt.path}})
			require.Equal(t, tt.want, vs.resolve(headers))
		})
	}
}

func TestValueSource_Validate(t *testing.T) {
	idx := 2
	tests := []struct {
		name    string
		vs      valueSource
		wantErr string
	}{
		{"value ok", valueSource{Value: "x"}, ""},
		{"header ok", valueSource{Header: "x-h"}, ""},
		{"path_segment ok", valueSource{PathSegment: &idx}, ""},
		{"path_segment zero ok", valueSource{PathSegment: new(int)}, ""},
		{"query_param ok", valueSource{QueryParam: "q"}, ""},
		{"none set", valueSource{}, "must be set"},
		{"value+header", valueSource{Value: "x", Header: "y"}, "only one"},
		{"value+path_segment", valueSource{Value: "x", PathSegment: &idx}, "only one"},
		{"header+query_param", valueSource{Header: "x-h", QueryParam: "q"}, "only one"},
		{"path_segment+query_param", valueSource{PathSegment: &idx, QueryParam: "q"}, "only one"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateValueSource("field", &tt.vs)
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.wantErr)
			}
		})
	}
}

func TestParseConfig_PathSegmentSource(t *testing.T) {
	idx := 2
	cfg := openfgaConfig{
		Cluster:     "openfga",
		OpenFGAHost: "openfga:8080",
		StoreID:     "store1",
		User:        valueSource{Header: "x-user-id", Prefix: "user:"},
		Relation:    valueSource{Value: "can_access"},
		Object:      valueSource{PathSegment: &idx, Prefix: "document:"},
	}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)

	parsed, err := parseConfig(data)
	require.NoError(t, err)
	require.Len(t, parsed.rules, 1)
	require.NotNil(t, parsed.rules[0].object.PathSegment)
	require.Equal(t, 2, *parsed.rules[0].object.PathSegment)
	require.Equal(t, "document:", parsed.rules[0].object.Prefix)

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":path":     {"/api/documents/budget"},
		"x-user-id": {"alice"},
	})
	require.Equal(t, "document:budget", parsed.rules[0].object.resolve(headers))
}

func TestParseConfig_QueryParamSource(t *testing.T) {
	cfg := openfgaConfig{
		Cluster:     "openfga",
		OpenFGAHost: "openfga:8080",
		StoreID:     "store1",
		User:        valueSource{Header: "x-user-id", Prefix: "user:"},
		Relation:    valueSource{Value: "can_access"},
		Object:      valueSource{QueryParam: "resource", Prefix: "doc:"},
	}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)

	parsed, err := parseConfig(data)
	require.NoError(t, err)

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":path":     {"/api/search?resource=planning&v=2"},
		"x-user-id": {"alice"},
	})
	require.Equal(t, "doc:planning", parsed.rules[0].object.resolve(headers))
}

func TestExtractPathSegment(t *testing.T) {
	require.Equal(t, "api", extractPathSegment("/api/docs/budget", 0))
	require.Equal(t, "docs", extractPathSegment("/api/docs/budget", 1))
	require.Equal(t, "budget", extractPathSegment("/api/docs/budget", 2))
	require.Equal(t, "budget", extractPathSegment("/api/docs/budget", -1))
	require.Equal(t, "docs", extractPathSegment("/api/docs/budget", -2))
	require.Empty(t, extractPathSegment("/api/docs/budget", 5))
	require.Empty(t, extractPathSegment("/api/docs/budget", -10))
	require.Equal(t, "budget", extractPathSegment("/api/docs/budget?foo=bar", 2))
}

func TestExtractQueryParam(t *testing.T) {
	require.Equal(t, "planning", extractQueryParam("/api?resource=planning", "resource"))
	require.Empty(t, extractQueryParam("/api?other=val", "resource"))
	require.Empty(t, extractQueryParam("/api/resource", "resource"))
	require.Equal(t, "a", extractQueryParam("/api?resource=a&resource=b", "resource"))
	require.Equal(t, "my doc", extractQueryParam("/api?resource=my%20doc", "resource"))
}

func TestParseConfig_CalloutHeaders(t *testing.T) {
	cfg := openfgaConfig{
		Cluster:     "openfga",
		OpenFGAHost: "openfga:8080",
		StoreID:     "01JSGQ95TCAS7GXY6P3VGMKDJ9",
		User:        valueSource{Header: "x-user-id", Prefix: "user:"},
		Relation:    valueSource{Value: "can_use"},
		Object:      valueSource{Header: "x-ai-model", Prefix: "model:"},
		CalloutHeaders: map[string]string{
			"authorization": "Bearer my-token",
			"x-custom":      "value",
		},
	}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)

	parsed, err := parseConfig(data)
	require.NoError(t, err)
	require.Equal(t, "Bearer my-token", headerValue(parsed.calloutHeaders, "authorization"))
	require.Equal(t, "value", headerValue(parsed.calloutHeaders, "x-custom"))
	// Standard headers still present
	require.Equal(t, "POST", headerValue(parsed.calloutHeaders, ":method"))
	require.Equal(t, "application/json", headerValue(parsed.calloutHeaders, "content-type"))
}

func TestParseConfig_ContextualTuples(t *testing.T) {
	cfg := openfgaConfig{
		Cluster:     "openfga",
		OpenFGAHost: "openfga:8080",
		StoreID:     "01JSGQ95TCAS7GXY6P3VGMKDJ9",
		User:        valueSource{Header: "x-user-id", Prefix: "user:"},
		Relation:    valueSource{Value: "can_use"},
		Object:      valueSource{Header: "x-ai-model", Prefix: "model:"},
		ContextualTuples: []contextualTupleCfg{
			{
				User:     valueSource{Header: "x-user-id", Prefix: "user:"},
				Relation: valueSource{Value: "member"},
				Object:   valueSource{Header: "x-org-id", Prefix: "organization:"},
			},
		},
	}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)

	parsed, err := parseConfig(data)
	require.NoError(t, err)
	require.Len(t, parsed.contextualTuples, 1)
	require.Equal(t, "x-user-id", parsed.contextualTuples[0].user.Header)
	require.Equal(t, "member", parsed.contextualTuples[0].relation.resolved)
	require.Equal(t, "x-org-id", parsed.contextualTuples[0].object.Header)
}

func TestParseConfig_ContextualTuples_Invalid(t *testing.T) {
	cfg := openfgaConfig{
		Cluster:     "openfga",
		OpenFGAHost: "openfga:8080",
		StoreID:     "01JSGQ95TCAS7GXY6P3VGMKDJ9",
		User:        valueSource{Header: "x-user-id"},
		Relation:    valueSource{Value: "reader"},
		Object:      valueSource{Header: "x-resource"},
		ContextualTuples: []contextualTupleCfg{
			{
				User:     valueSource{}, // invalid: no source set
				Relation: valueSource{Value: "member"},
				Object:   valueSource{Header: "x-org-id"},
			},
		},
	}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)

	_, err = parseConfig(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "contextual_tuples[0].user")
}

func TestParseConfig_Context(t *testing.T) {
	cfg := openfgaConfig{
		Cluster:     "openfga",
		OpenFGAHost: "openfga:8080",
		StoreID:     "01JSGQ95TCAS7GXY6P3VGMKDJ9",
		User:        valueSource{Header: "x-user-id", Prefix: "user:"},
		Relation:    valueSource{Value: "can_use"},
		Object:      valueSource{Header: "x-ai-model", Prefix: "model:"},
		Context: map[string]valueSource{
			"ip_address": {Header: "x-forwarded-for"},
			"region":     {Value: "us-east-1"},
		},
	}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)

	parsed, err := parseConfig(data)
	require.NoError(t, err)
	require.Len(t, parsed.context, 2)
	require.Equal(t, "x-forwarded-for", parsed.context["ip_address"].Header)
	require.Equal(t, "us-east-1", parsed.context["region"].resolved)
}

func TestParseConfig_Context_Invalid(t *testing.T) {
	cfg := openfgaConfig{
		Cluster:     "openfga",
		OpenFGAHost: "openfga:8080",
		StoreID:     "01JSGQ95TCAS7GXY6P3VGMKDJ9",
		User:        valueSource{Header: "x-user-id"},
		Relation:    valueSource{Value: "reader"},
		Object:      valueSource{Header: "x-resource"},
		Context: map[string]valueSource{
			"bad_field": {}, // invalid: no source set
		},
	}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)

	_, err = parseConfig(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "context[bad_field]")
}

func TestWarnStoreIDFormat(t *testing.T) {
	require.Empty(t, warnStoreIDFormat("01JSGQ95TCAS7GXY6P3VGMKDJ9")) // valid 26-char ULID
	require.Contains(t, warnStoreIDFormat("store1"), "does not look like a ULID")
	require.Contains(t, warnStoreIDFormat("01JSGQ95TCAS7GXY6P3VGMKDJ9X"), "does not look like a ULID")
}

func TestParseConfig_InvalidConsistency(t *testing.T) {
	cfg := openfgaConfig{
		Cluster:     "openfga",
		OpenFGAHost: "openfga:8080",
		StoreID:     "store1",
		Consistency: "LOW_LATENCY",
		User:        valueSource{Header: "x-user-id", Prefix: "user:"},
		Relation:    valueSource{Value: "can_access"},
		Object:      valueSource{Header: "x-resource", Prefix: "document:"},
	}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	_, err = parseConfig(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid consistency")
}

func TestBuildCheckBody(t *testing.T) {
	body, err := buildCheckBody("user:alice", "can_use", "model:gpt-4", "", "", nil, nil)
	require.NoError(t, err)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(body, &parsed))
	tk := parsed["tuple_key"].(map[string]any)
	require.Equal(t, "user:alice", tk["user"])
	require.Equal(t, "can_use", tk["relation"])
	require.Equal(t, "model:gpt-4", tk["object"])
	_, hasModelID := parsed["authorization_model_id"]
	require.False(t, hasModelID)
	_, hasConsistency := parsed["consistency"]
	require.False(t, hasConsistency)
	_, hasCtxTuples := parsed["contextual_tuples"]
	require.False(t, hasCtxTuples)
	_, hasContext := parsed["context"]
	require.False(t, hasContext)
}

func TestBuildCheckBody_WithModelID(t *testing.T) {
	body, err := buildCheckBody("user:alice", "can_use", "model:gpt-4", "model-123", "", nil, nil)
	require.NoError(t, err)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(body, &parsed))
	require.Equal(t, "model-123", parsed["authorization_model_id"])
}

func TestBuildCheckBody_WithConsistency(t *testing.T) {
	body, err := buildCheckBody("user:alice", "can_use", "model:gpt-4", "model-123", "HIGHER_CONSISTENCY", nil, nil)
	require.NoError(t, err)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(body, &parsed))
	require.Equal(t, "HIGHER_CONSISTENCY", parsed["consistency"])
	require.Equal(t, "model-123", parsed["authorization_model_id"])
}

func TestBuildCheckBody_WithContextualTuples(t *testing.T) {
	tuples := []resolvedTuple{
		{User: "user:alice", Relation: "member", Object: "organization:acme"},
		{User: "user:alice", Relation: "viewer", Object: "team:engineering"},
	}
	body, err := buildCheckBody("user:alice", "can_use", "model:gpt-4", "", "", tuples, nil)
	require.NoError(t, err)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(body, &parsed))
	ctxTuples := parsed["contextual_tuples"].(map[string]any)
	tupleKeys := ctxTuples["tuple_keys"].([]any)
	require.Len(t, tupleKeys, 2)
	first := tupleKeys[0].(map[string]any)
	require.Equal(t, "user:alice", first["user"])
	require.Equal(t, "member", first["relation"])
	require.Equal(t, "organization:acme", first["object"])
}

func TestBuildCheckBody_WithContext(t *testing.T) {
	ctx := map[string]string{
		"current_time": "2024-01-01T00:00:00Z",
		"ip_address":   "10.0.0.1",
	}
	body, err := buildCheckBody("user:alice", "can_use", "model:gpt-4", "", "", nil, ctx)
	require.NoError(t, err)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(body, &parsed))
	context := parsed["context"].(map[string]any)
	require.Equal(t, "2024-01-01T00:00:00Z", context["current_time"])
	require.Equal(t, "10.0.0.1", context["ip_address"])
}

func TestBuildCheckBody_JSONSafety(t *testing.T) {
	// Values with special characters must produce valid JSON without injection.
	body, err := buildCheckBody(`user:alice"evil`, `can_use`, `model:gpt-4\nother`, "", "", nil, nil)
	require.NoError(t, err)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(body, &parsed), "body must be valid JSON even with special characters")
	tk := parsed["tuple_key"].(map[string]any)
	require.Equal(t, `user:alice"evil`, tk["user"])
	require.Equal(t, `model:gpt-4\nother`, tk["object"])
	_, hasModelID := parsed["authorization_model_id"]
	require.False(t, hasModelID)
}

func TestParseConfig_AccumulatesErrors(t *testing.T) {
	cfg := openfgaConfig{
		Cluster:     "openfga",
		OpenFGAHost: "openfga:8080",
		StoreID:     "store1",
		Consistency: "INVALID_CONSISTENCY",
		DenyStatus:  99,
		Context: map[string]valueSource{
			"bad_field": {}, // invalid: no source set
		},
	}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)

	_, err = parseConfig(data)
	require.Error(t, err)
	errStr := err.Error()
	// All three independent errors must be reported in a single error.
	require.Contains(t, errStr, "invalid consistency")
	require.Contains(t, errStr, "deny_status must be between")
	require.Contains(t, errStr, "context[bad_field]")
}

func BenchmarkBuildCheckBody(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, _ = buildCheckBody("user:alice", "can_use", "model:gpt-4", "model-123", "HIGHER_CONSISTENCY", nil, nil)
	}
}
