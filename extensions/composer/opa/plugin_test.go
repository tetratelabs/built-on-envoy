// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package opa

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/fake"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/mocks"
	"github.com/open-policy-agent/opa/v1/rego"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/pkg"
)

// Helper to create a temporary policy file for testing.
func createTestPolicyFile(t *testing.T, policy string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "policy-*.rego")
	require.NoError(t, err)
	_, err = f.WriteString(policy)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return f.Name()
}

// Tests for OPAHttpFilterConfigFactory.Create

func TestConfigFactory_Create_ValidConfig(t *testing.T) {
	var (
		p1 = createTestPolicyFile(t, `package envoy.authz
default allow := false
`)
		p2 = createTestPolicyFile(t, `package custom.policy
default verdict := false
`)
		p3 = `package another.policy
default decision := false
`
		p4 = `package envoy.authz
default allowed := true
`
	)

	configJSON, err := json.Marshal(opaConfig{
		Policies: []pkg.DataSource{
			{File: p1},
			{File: p2},
			{Inline: p3},
			{Inline: p4},
		},
	})
	require.NoError(t, err)

	factory := &OPAHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockHandle.EXPECT().DefineCounter("opa_requests_total", "decision").Return(shared.MetricID(1), shared.MetricsSuccess)

	filterFactory, err := factory.Create(mockHandle, configJSON)
	require.NoError(t, err)
	require.NotNil(t, filterFactory)
}

func TestConfigFactory_Create_EmptyConfig(t *testing.T) {
	factory := &OPAHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	filterFactory, err := factory.Create(mockHandle, []byte{})
	require.Error(t, err)
	require.Nil(t, filterFactory)
	require.Contains(t, err.Error(), "empty config")
}

func TestConfigFactory_Create_InvalidJSON(t *testing.T) {
	factory := &OPAHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	filterFactory, err := factory.Create(mockHandle, []byte("{invalid"))
	require.Error(t, err)
	require.Nil(t, filterFactory)
}

func TestConfigFactory_Create_MissingPolicyFile(t *testing.T) {
	configJSON, err := json.Marshal(opaConfig{})
	require.NoError(t, err)

	factory := &OPAHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	filterFactory, err := factory.Create(mockHandle, configJSON)
	require.Error(t, err)
	require.Nil(t, filterFactory)
	require.Contains(t, err.Error(), "no policies provided in config")
}

func TestConfigFactory_Create_PolicyFileNotFound(t *testing.T) {
	configJSON, err := json.Marshal(opaConfig{Policies: []pkg.DataSource{{File: "/nonexistent/policy.rego"}}})
	require.NoError(t, err)

	factory := &OPAHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	filterFactory, err := factory.Create(mockHandle, configJSON)
	require.Error(t, err)
	require.Nil(t, filterFactory)
	require.Contains(t, err.Error(), "failed to load policy")
}

func TestConfigFactory_Create_InvalidRego(t *testing.T) {
	policyFile := createTestPolicyFile(t, "this is not valid rego {{{")
	configJSON, err := json.Marshal(opaConfig{Policies: []pkg.DataSource{{File: policyFile}}})
	require.NoError(t, err)

	factory := &OPAHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	filterFactory, err := factory.Create(mockHandle, configJSON)
	require.Error(t, err)
	require.Nil(t, filterFactory)
	require.Contains(t, err.Error(), "failed to compile policy")
}

func TestConfigFactory_Create_DefaultDecisionPath(t *testing.T) {
	policy := `package envoy.authz
default allow := false
`
	policyFile := createTestPolicyFile(t, policy)

	cfg := opaConfig{Policies: []pkg.DataSource{{File: policyFile}}}
	configJSON, err := json.Marshal(cfg)
	require.NoError(t, err)

	factory := &OPAHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockHandle.EXPECT().DefineCounter("opa_requests_total", "decision").Return(shared.MetricID(1), shared.MetricsSuccess)

	filterFactory, err := factory.Create(mockHandle, configJSON)
	require.NoError(t, err)
	require.NotNil(t, filterFactory)

	opaFactory, ok := filterFactory.(*opaHttpFilterFactory)
	require.True(t, ok)
	require.Equal(t, "envoy.authz.allow", opaFactory.config.DecisionPath)
}

func TestConfigFactory_Create_CustomDecisionPath(t *testing.T) {
	policy := `package custom.policy
default verdict := false
`
	policyFile := createTestPolicyFile(t, policy)

	cfg := opaConfig{Policies: []pkg.DataSource{{File: policyFile}}, DecisionPath: "custom.policy.verdict"}
	configJSON, err := json.Marshal(cfg)
	require.NoError(t, err)

	factory := &OPAHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockHandle.EXPECT().DefineCounter("opa_requests_total", "decision").Return(shared.MetricID(1), shared.MetricsSuccess)

	filterFactory, err := factory.Create(mockHandle, configJSON)
	require.NoError(t, err)
	require.NotNil(t, filterFactory)

	opaFactory, ok := filterFactory.(*opaHttpFilterFactory)
	require.True(t, ok)
	require.Equal(t, "custom.policy.verdict", opaFactory.config.DecisionPath)
}

// Helper to create a filter for testing.
func createTestFilter(t *testing.T, cfg opaConfig) (*opaHttpFilter, *mocks.MockHttpFilterHandle) {
	t.Helper()

	configJSON, err := json.Marshal(cfg)
	require.NoError(t, err)

	factory := &OPAHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	mockConfigHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockConfigHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockConfigHandle.EXPECT().DefineCounter("opa_requests_total", "decision").Return(shared.MetricID(1), shared.MetricsSuccess).AnyTimes()

	filterFactory, err := factory.Create(mockConfigHandle, configJSON)
	require.NoError(t, err)

	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDRequestProtocol).Return(pkg.UnsafeBufferFromString("HTTP/1.1"), true).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDSourceAddress).Return(pkg.UnsafeBufferFromString("127.0.0.1:5000"), true).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDDestinationAddress).Return(pkg.UnsafeBufferFromString("127.0.0.1:80"), true).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDConnectionUriSanPeerCertificate).Return(pkg.UnsafeBufferFromString(""), false).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDConnectionDnsSanPeerCertificate).Return(pkg.UnsafeBufferFromString(""), false).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDConnectionSubjectPeerCertificate).Return(pkg.UnsafeBufferFromString(""), false).AnyTimes()
	mockHandle.EXPECT().GetAttributeBool(shared.AttributeIDConnectionMtls).Return(false, false).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDConnectionTlsVersion).Return(pkg.UnsafeBufferFromString(""), false).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDConnectionSha256PeerCertificateDigest).Return(pkg.UnsafeBufferFromString(""), false).AnyTimes()
	mockHandle.EXPECT().IncrementCounterValue(shared.MetricID(1), uint64(1), gomock.Any()).Return(shared.MetricsSuccess).AnyTimes()

	filter := filterFactory.Create(mockHandle)
	opaFilter, ok := filter.(*opaHttpFilter)
	require.True(t, ok)

	return opaFilter, mockHandle
}

// Tests for OnRequestHeaders

func TestOnRequestHeaders_AllowBooleanMultiplePolicies(t *testing.T) {
	defaultPolicy := `package envoy.authz
default allow := false
`
	rulePolicy := `package envoy.authz
allow if { input.parsed_path[0] == "public" }
`

	filter, _ := createTestFilter(t, opaConfig{Policies: []pkg.DataSource{
		{Inline: defaultPolicy}, {Inline: rulePolicy},
	}})
	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/public/resource"},
		":authority": {"example.com"},
		":scheme":    {"http"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusContinue, status)
}

func TestOnRequestHeaders_DenyBoolean(t *testing.T) {
	policy := `package envoy.authz
default allow := false
allow if { input.parsed_path[0] == "public" }
`
	filter, mockHandle := createTestFilter(t, opaConfig{
		Policies: []pkg.DataSource{{File: createTestPolicyFile(t, policy)}},
	})
	mockHandle.EXPECT().SendLocalResponse(
		uint32(403),
		gomock.Any(),
		[]byte("Forbidden"),
		"opa_denied",
	)

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/private/resource"},
		":authority": {"example.com"},
		":scheme":    {"http"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStop, status)
}

func TestOnRequestHeaders_AllowObjectWithHeaders(t *testing.T) {
	policy := `package envoy.authz
allow := {"allowed": true, "headers": {"x-user": "admin"}}
`
	filter, mockHandle := createTestFilter(t, opaConfig{Policies: []pkg.DataSource{{Inline: policy}}})

	requestHeaders := fake.NewFakeHeaderMap(map[string][]string{})
	mockHandle.EXPECT().RequestHeaders().Return(requestHeaders).AnyTimes()

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/api/resource"},
		":authority": {"example.com"},
		":scheme":    {"http"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusContinue, status)
	require.Equal(t, "admin", requestHeaders.GetOne("x-user").ToUnsafeString())
}

func TestOnRequestHeaders_DenyObjectWithCustomStatus(t *testing.T) {
	policy := `package envoy.authz
default allow := {"allowed": false, "http_status": 401, "body": "Unauthorized", "headers": {"www-authenticate": "Bearer"}}
`
	filter, mockHandle := createTestFilter(t, opaConfig{Policies: []pkg.DataSource{{File: createTestPolicyFile(t, policy)}}})

	var capturedStatus uint32
	var capturedBody []byte
	var capturedHeaders [][2]string
	mockHandle.EXPECT().SendLocalResponse(
		gomock.Any(), gomock.Any(), gomock.Any(), gomock.Eq("opa_denied"),
	).Do(func(status uint32, headers [][2]string, body []byte, _ string) {
		capturedStatus = status
		capturedBody = body
		capturedHeaders = headers
	})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/api/resource"},
		":authority": {"example.com"},
		":scheme":    {"http"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStop, status)
	require.Equal(t, uint32(401), capturedStatus)
	require.Equal(t, []byte("Unauthorized"), capturedBody)
	require.Len(t, capturedHeaders, 1)
	require.Equal(t, "www-authenticate", capturedHeaders[0][0])
	require.Equal(t, "Bearer", capturedHeaders[0][1])
}

func TestOnRequestHeaders_DryRunAllow(t *testing.T) {
	policy := `package envoy.authz
default allow := true
`
	filter, _ := createTestFilter(t, opaConfig{DryRun: true, Policies: []pkg.DataSource{{File: createTestPolicyFile(t, policy)}}})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/api/resource"},
		":authority": {"example.com"},
		":scheme":    {"http"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusContinue, status)
}

func TestOnRequestHeaders_DryRunDeny(t *testing.T) {
	policy := `package envoy.authz
default allow := false
`
	// In dry-run mode, even denied requests should continue.
	filter, _ := createTestFilter(t, opaConfig{DryRun: true, Policies: []pkg.DataSource{{File: createTestPolicyFile(t, policy)}}})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/private/resource"},
		":authority": {"example.com"},
		":scheme":    {"http"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusContinue, status)
}

// Tests for metrics

// createTestFilterWithMetricExpectation creates a filter that expects a specific metric decision tag.
func createTestFilterWithMetricExpectation(t *testing.T, cfg opaConfig, expectedDecision string) (*opaHttpFilter, *mocks.MockHttpFilterHandle) {
	t.Helper()

	configJSON, err := json.Marshal(cfg)
	require.NoError(t, err)

	factory := &OPAHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	mockConfigHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockConfigHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockConfigHandle.EXPECT().DefineCounter("opa_requests_total", "decision").Return(shared.MetricID(1), shared.MetricsSuccess)

	filterFactory, err := factory.Create(mockConfigHandle, configJSON)
	require.NoError(t, err)

	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDRequestProtocol).Return(pkg.UnsafeBufferFromString("HTTP/1.1"), true).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDSourceAddress).Return(pkg.UnsafeBufferFromString("127.0.0.1:5000"), true).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDDestinationAddress).Return(pkg.UnsafeBufferFromString("127.0.0.1:80"), true).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDConnectionUriSanPeerCertificate).Return(pkg.UnsafeBufferFromString(""), false).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDConnectionDnsSanPeerCertificate).Return(pkg.UnsafeBufferFromString(""), false).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDConnectionSubjectPeerCertificate).Return(pkg.UnsafeBufferFromString(""), false).AnyTimes()
	mockHandle.EXPECT().GetAttributeBool(shared.AttributeIDConnectionMtls).Return(false, false).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDConnectionTlsVersion).Return(pkg.UnsafeBufferFromString(""), false).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDConnectionSha256PeerCertificateDigest).Return(pkg.UnsafeBufferFromString(""), false).AnyTimes()
	mockHandle.EXPECT().IncrementCounterValue(shared.MetricID(1), uint64(1), expectedDecision).Return(shared.MetricsSuccess)

	filter := filterFactory.Create(mockHandle)
	opaFilter, ok := filter.(*opaHttpFilter)
	require.True(t, ok)

	return opaFilter, mockHandle
}

func TestOnRequestHeaders_Metrics_Allowed(t *testing.T) {
	policy := `package envoy.authz
default allow := true
`
	filter, _ := createTestFilterWithMetricExpectation(t, opaConfig{Policies: []pkg.DataSource{{Inline: policy}}}, decisionAllowed)

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/api/resource"},
		":authority": {"example.com"},
		":scheme":    {"http"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusContinue, status)
}

func TestOnRequestHeaders_Metrics_Denied(t *testing.T) {
	policy := `package envoy.authz
default allow := false
`
	filter, mockHandle := createTestFilterWithMetricExpectation(t, opaConfig{Policies: []pkg.DataSource{{Inline: policy}}}, decisionDenied)
	mockHandle.EXPECT().SendLocalResponse(uint32(403), gomock.Any(), []byte("Forbidden"), "opa_denied")

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/api/resource"},
		":authority": {"example.com"},
		":scheme":    {"http"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStop, status)
}

func TestOnRequestHeaders_Metrics_FailOpen(t *testing.T) {
	filter, mockHandle := createErrorFilter(t, true)
	mockHandle.EXPECT().IncrementCounterValue(shared.MetricID(1), uint64(1), decisionFailOpen).Return(shared.MetricsSuccess)

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/api/resource"},
		":authority": {"example.com"},
		":scheme":    {"http"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusContinue, status)
}

func TestOnRequestHeaders_Metrics_DryRunAllow(t *testing.T) {
	policy := `package envoy.authz
default allow := false
`
	filter, _ := createTestFilterWithMetricExpectation(t, opaConfig{Policies: []pkg.DataSource{{Inline: policy}}, DryRun: true}, decisionDryAllow)

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/api/resource"},
		":authority": {"example.com"},
		":scheme":    {"http"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusContinue, status)
}

// Tests for parsePath

func TestParsePath_Simple(t *testing.T) {
	segments, query := parsePath("/api/users/123")
	require.Equal(t, []string{"api", "users", "123"}, segments)
	require.Empty(t, query)
}

func TestParsePath_WithQuery(t *testing.T) {
	segments, query := parsePath("/api/search?q=test&page=1")
	require.Equal(t, []string{"api", "search"}, segments)
	require.Equal(t, []string{"test"}, query["q"])
	require.Equal(t, []string{"1"}, query["page"])
}

func TestParsePath_EmptyQuery(t *testing.T) {
	segments, query := parsePath("/api")
	require.Equal(t, []string{"api"}, segments)
	require.Empty(t, query)
}

func TestParsePath_MultiValueQuery(t *testing.T) {
	segments, query := parsePath("/api?tag=a&tag=b")
	require.Equal(t, []string{"api"}, segments)
	require.Equal(t, []string{"a", "b"}, query["tag"])
}

// Tests for interpretResult

func TestInterpretResult_BooleanTrue(t *testing.T) {
	policy := `package test
allow := true
`
	policyFile := createTestPolicyFile(t, policy)
	cfg := opaConfig{Policies: []pkg.DataSource{{File: policyFile}}, DecisionPath: "test.allow"}
	configJSON, err := json.Marshal(cfg)
	require.NoError(t, err)

	factory := &OPAHttpFilterConfigFactory{}
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockHandle.EXPECT().DefineCounter("opa_requests_total", "decision").Return(shared.MetricID(1), shared.MetricsSuccess)

	filterFactory, err := factory.Create(mockHandle, configJSON)
	require.NoError(t, err)

	mockFilterHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockFilterHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockFilterHandle.EXPECT().GetAttributeString(gomock.Any()).Return(pkg.UnsafeBufferFromString(""), false).AnyTimes()
	mockFilterHandle.EXPECT().GetAttributeBool(gomock.Any()).Return(false, false).AnyTimes()
	mockFilterHandle.EXPECT().IncrementCounterValue(shared.MetricID(1), uint64(1), "allowed").Return(shared.MetricsSuccess)

	filter := filterFactory.Create(mockFilterHandle).(*opaHttpFilter)

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method": {"GET"},
		":path":   {"/"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusContinue, status)
}

func TestInterpretResult_BooleanFalse(t *testing.T) {
	policy := `package test
allow := false
`
	policyFile := createTestPolicyFile(t, policy)
	cfg := opaConfig{Policies: []pkg.DataSource{{File: policyFile}}, DecisionPath: "test.allow"}
	configJSON, err := json.Marshal(cfg)
	require.NoError(t, err)

	factory := &OPAHttpFilterConfigFactory{}
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockHandle.EXPECT().DefineCounter("opa_requests_total", "decision").Return(shared.MetricID(1), shared.MetricsSuccess)

	filterFactory, err := factory.Create(mockHandle, configJSON)
	require.NoError(t, err)

	mockFilterHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockFilterHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockFilterHandle.EXPECT().GetAttributeString(gomock.Any()).Return(pkg.UnsafeBufferFromString(""), false).AnyTimes()
	mockFilterHandle.EXPECT().GetAttributeBool(gomock.Any()).Return(false, false).AnyTimes()
	mockFilterHandle.EXPECT().SendLocalResponse(uint32(403), gomock.Any(), []byte("Forbidden"), "opa_denied")
	mockFilterHandle.EXPECT().IncrementCounterValue(shared.MetricID(1), uint64(1), "denied").Return(shared.MetricsSuccess)

	filter := filterFactory.Create(mockFilterHandle).(*opaHttpFilter)

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method": {"GET"},
		":path":   {"/"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStop, status)
}

// Tests for buildInput

func TestBuildInput_BasicRequest(t *testing.T) {
	policy := `package envoy.authz
default allow := true
`
	filter, _ := createTestFilter(t, opaConfig{Policies: []pkg.DataSource{{Inline: policy}}})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":       {"POST"},
		":path":         {"/api/users?role=admin"},
		":authority":    {"example.com"},
		":scheme":       {"https"},
		"authorization": {"Bearer token123"},
		"content-type":  {"application/json"},
	})

	input := filter.buildInput(headers, nil)

	// Check top-level structure.
	attrs, ok := input["attributes"].(map[string]any)
	require.True(t, ok)

	req, ok := attrs["request"].(map[string]any)
	require.True(t, ok)

	http, ok := req["http"].(map[string]any)
	require.True(t, ok)

	require.Equal(t, "POST", http["method"])
	require.Equal(t, "/api/users?role=admin", http["path"])
	require.Equal(t, "example.com", http["host"])
	require.Equal(t, "https", http["scheme"])
	require.Equal(t, "HTTP/1.1", http["protocol"])

	// Check headers exclude pseudo-headers.
	httpHeaders, ok := http["headers"].(map[string]string)
	require.True(t, ok)
	require.Equal(t, "Bearer token123", httpHeaders["authorization"])
	require.Equal(t, "application/json", httpHeaders["content-type"])
	require.NotContains(t, httpHeaders, ":method")
	require.NotContains(t, httpHeaders, ":path")
	require.NotContains(t, httpHeaders, ":authority")
	require.NotContains(t, httpHeaders, ":scheme")

	// Check source and destination.
	source, ok := attrs["source"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "127.0.0.1:5000", source["address"])

	cert, ok := source["certificate"].(map[string]any)
	require.True(t, ok)
	require.Empty(t, cert["uri_san"])
	require.Empty(t, cert["dns_san"])
	require.Empty(t, cert["subject"])
	require.Empty(t, cert["sha256_digest"])

	dest, ok := attrs["destination"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "127.0.0.1:80", dest["address"])

	// Check connection attributes.
	conn, ok := attrs["connection"].(map[string]any)
	require.True(t, ok)
	require.Empty(t, conn["tls_version"])
	require.False(t, conn["mtls"].(bool))

	// Check parsed_path.
	parsedPath, ok := input["parsed_path"].([]string)
	require.True(t, ok)
	require.Equal(t, []string{"api", "users"}, parsedPath)

	// Check parsed_query.
	parsedQuery, ok := input["parsed_query"].(map[string][]string)
	require.True(t, ok)
	require.Equal(t, []string{"admin"}, parsedQuery["role"])
}

func TestBuildInput_MTLSAttributes(t *testing.T) {
	policy := `package envoy.authz
default allow := true
`
	policyFile := createTestPolicyFile(t, policy)
	cfg := opaConfig{Policies: []pkg.DataSource{{File: policyFile}}}
	configJSON, err := json.Marshal(cfg)
	require.NoError(t, err)

	factory := &OPAHttpFilterConfigFactory{}
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockConfigHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockConfigHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockConfigHandle.EXPECT().DefineCounter("opa_requests_total", "decision").Return(shared.MetricID(1), shared.MetricsSuccess)

	filterFactory, err := factory.Create(mockConfigHandle, configJSON)
	require.NoError(t, err)

	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDRequestProtocol).Return(pkg.UnsafeBufferFromString("HTTP/2"), true).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDSourceAddress).Return(pkg.UnsafeBufferFromString("10.0.0.1:5000"), true).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDDestinationAddress).Return(pkg.UnsafeBufferFromString("10.0.0.2:443"), true).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDConnectionUriSanPeerCertificate).Return(pkg.UnsafeBufferFromString("spiffe://cluster.local/ns/default/sa/client"), true).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDConnectionDnsSanPeerCertificate).Return(pkg.UnsafeBufferFromString("client.default.svc.cluster.local"), true).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDConnectionSubjectPeerCertificate).Return(pkg.UnsafeBufferFromString("CN=client,O=example"), true).AnyTimes()
	mockHandle.EXPECT().GetAttributeBool(shared.AttributeIDConnectionMtls).Return(false, false).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDConnectionTlsVersion).Return(pkg.UnsafeBufferFromString("TLSv1.3"), true).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDConnectionSha256PeerCertificateDigest).Return(pkg.UnsafeBufferFromString("abc123def456"), true).AnyTimes()

	filter := filterFactory.Create(mockHandle).(*opaHttpFilter)

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/api/resource"},
		":authority": {"example.com"},
		":scheme":    {"https"},
	})

	input := filter.buildInput(headers, nil)

	attrs, ok := input["attributes"].(map[string]any)
	require.True(t, ok)

	// Check source certificate attributes.
	source, ok := attrs["source"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "10.0.0.1:5000", source["address"])

	cert, ok := source["certificate"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "spiffe://cluster.local/ns/default/sa/client", cert["uri_san"])
	require.Equal(t, "client.default.svc.cluster.local", cert["dns_san"])
	require.Equal(t, "CN=client,O=example", cert["subject"])
	require.Equal(t, "abc123def456", cert["sha256_digest"])

	// Check connection attributes.
	conn, ok := attrs["connection"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "TLSv1.3", conn["tls_version"])
}

func TestOnRequestHeaders_PolicyUsesSPIFFE(t *testing.T) {
	policy := `package envoy.authz
default allow := false
allow if {
  input.attributes.source.certificate.uri_san == "spiffe://cluster.local/ns/default/sa/trusted"
}
`
	policyFile := createTestPolicyFile(t, policy)
	cfg := opaConfig{Policies: []pkg.DataSource{{File: policyFile}}}
	configJSON, err := json.Marshal(cfg)
	require.NoError(t, err)

	factory := &OPAHttpFilterConfigFactory{}
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockConfigHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockConfigHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockConfigHandle.EXPECT().DefineCounter("opa_requests_total", "decision").Return(shared.MetricID(1), shared.MetricsSuccess)

	filterFactory, err := factory.Create(mockConfigHandle, configJSON)
	require.NoError(t, err)

	// Test with trusted SPIFFE identity - should allow.
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDRequestProtocol).Return(pkg.UnsafeBufferFromString("HTTP/2"), true).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDSourceAddress).Return(pkg.UnsafeBufferFromString("10.0.0.1:5000"), true).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDDestinationAddress).Return(pkg.UnsafeBufferFromString("10.0.0.2:443"), true).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDConnectionUriSanPeerCertificate).Return(pkg.UnsafeBufferFromString("spiffe://cluster.local/ns/default/sa/trusted"), true).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDConnectionDnsSanPeerCertificate).Return(pkg.UnsafeBufferFromString(""), false).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDConnectionSubjectPeerCertificate).Return(pkg.UnsafeBufferFromString(""), false).AnyTimes()
	mockHandle.EXPECT().GetAttributeBool(shared.AttributeIDConnectionMtls).Return(false, false).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDConnectionTlsVersion).Return(pkg.UnsafeBufferFromString("TLSv1.3"), true).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDConnectionSha256PeerCertificateDigest).Return(pkg.UnsafeBufferFromString(""), false).AnyTimes()
	mockHandle.EXPECT().IncrementCounterValue(shared.MetricID(1), uint64(1), "allowed").Return(shared.MetricsSuccess)

	filter := filterFactory.Create(mockHandle).(*opaHttpFilter)

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/api/resource"},
		":authority": {"example.com"},
		":scheme":    {"https"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusContinue, status)
}

func TestOnRequestHeaders_PolicyUsesSPIFFE_Denied(t *testing.T) {
	policy := `package envoy.authz
default allow := false
allow if {
  input.attributes.source.certificate.uri_san == "spiffe://cluster.local/ns/default/sa/trusted"
}
`
	policyFile := createTestPolicyFile(t, policy)
	cfg := opaConfig{Policies: []pkg.DataSource{{File: policyFile}}}
	configJSON, err := json.Marshal(cfg)
	require.NoError(t, err)

	factory := &OPAHttpFilterConfigFactory{}
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockConfigHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockConfigHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockConfigHandle.EXPECT().DefineCounter("opa_requests_total", "decision").Return(shared.MetricID(1), shared.MetricsSuccess)

	filterFactory, err := factory.Create(mockConfigHandle, configJSON)
	require.NoError(t, err)

	// Test with untrusted SPIFFE identity - should deny.
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDRequestProtocol).Return(pkg.UnsafeBufferFromString("HTTP/2"), true).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDSourceAddress).Return(pkg.UnsafeBufferFromString("10.0.0.1:5000"), true).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDDestinationAddress).Return(pkg.UnsafeBufferFromString("10.0.0.2:443"), true).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDConnectionUriSanPeerCertificate).Return(pkg.UnsafeBufferFromString("spiffe://cluster.local/ns/default/sa/untrusted"), true).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDConnectionDnsSanPeerCertificate).Return(pkg.UnsafeBufferFromString(""), false).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDConnectionSubjectPeerCertificate).Return(pkg.UnsafeBufferFromString(""), false).AnyTimes()
	mockHandle.EXPECT().GetAttributeBool(shared.AttributeIDConnectionMtls).Return(false, false).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDConnectionTlsVersion).Return(pkg.UnsafeBufferFromString("TLSv1.3"), true).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDConnectionSha256PeerCertificateDigest).Return(pkg.UnsafeBufferFromString(""), false).AnyTimes()
	mockHandle.EXPECT().SendLocalResponse(uint32(403), gomock.Any(), []byte("Forbidden"), "opa_denied")
	mockHandle.EXPECT().IncrementCounterValue(shared.MetricID(1), uint64(1), "denied").Return(shared.MetricsSuccess)

	filter := filterFactory.Create(mockHandle).(*opaHttpFilter)

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/api/resource"},
		":authority": {"example.com"},
		":scheme":    {"https"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStop, status)
}

// Tests for WellKnownHttpFilterConfigFactories

func TestWellKnownHttpFilterConfigFactories(t *testing.T) {
	factories := WellKnownHttpFilterConfigFactories()

	require.NotNil(t, factories)
	require.Len(t, factories, 1)
	require.Contains(t, factories, "opa")

	factory, ok := factories["opa"].(*OPAHttpFilterConfigFactory)
	require.True(t, ok)
	require.NotNil(t, factory)
}

// Tests for opaHttpFilterFactory.Create

func TestFilterFactory_Create(t *testing.T) {
	policy := `package envoy.authz
default allow := false
`
	policyFile := createTestPolicyFile(t, policy)
	cfg := opaConfig{Policies: []pkg.DataSource{{File: policyFile}}}
	configJSON, err := json.Marshal(cfg)
	require.NoError(t, err)

	factory := &OPAHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockConfigHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockConfigHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockConfigHandle.EXPECT().DefineCounter("opa_requests_total", "decision").Return(shared.MetricID(1), shared.MetricsSuccess)

	filterFactory, err := factory.Create(mockConfigHandle, configJSON)
	require.NoError(t, err)

	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	filter := filterFactory.Create(mockHandle)

	require.NotNil(t, filter)
	opaFilter, ok := filter.(*opaHttpFilter)
	require.True(t, ok)
	require.Equal(t, mockHandle, opaFilter.handle)
}

// Test that policy can access request headers for authorization.
func TestOnRequestHeaders_PolicyUsesHeaders(t *testing.T) {
	policy := `package envoy.authz
default allow := false
allow if {
  token := input.attributes.request.http.headers.authorization
  token == "Bearer valid-token"
}
`
	filter, _ := createTestFilter(t, opaConfig{Policies: []pkg.DataSource{{Inline: policy}}})

	// With valid token - should allow.
	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":       {"GET"},
		":path":         {"/api/resource"},
		":authority":    {"example.com"},
		":scheme":       {"http"},
		"authorization": {"Bearer valid-token"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusContinue, status)
}

func TestOnRequestHeaders_PolicyUsesHeaders_Denied(t *testing.T) {
	policy := `package envoy.authz
default allow := false
allow if {
  token := input.attributes.request.http.headers.authorization
  token == "Bearer valid-token"
}
`
	filter, mockHandle := createTestFilter(t, opaConfig{Policies: []pkg.DataSource{{Inline: policy}}})
	mockHandle.EXPECT().SendLocalResponse(uint32(403), gomock.Any(), []byte("Forbidden"), "opa_denied")

	// With invalid token - should deny.
	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":       {"GET"},
		":path":         {"/api/resource"},
		":authority":    {"example.com"},
		":scheme":       {"http"},
		"authorization": {"Bearer invalid-token"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStop, status)
}

// Test policy with method-based rules.
func TestOnRequestHeaders_PolicyUsesMethod(t *testing.T) {
	policy := `package envoy.authz
default allow := false
allow if { input.attributes.request.http.method == "GET" }
`
	filter, mockHandle := createTestFilter(t, opaConfig{Policies: []pkg.DataSource{{Inline: policy}}})
	mockHandle.EXPECT().SendLocalResponse(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	// GET should be allowed.
	getHeaders := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/api/resource"},
		":authority": {"example.com"},
	})
	require.Equal(t, shared.HeadersStatusContinue, filter.OnRequestHeaders(getHeaders, true))

	// POST should be denied (new filter instance needed since the mock may have been called).
	filter2, mockHandle2 := createTestFilter(t, opaConfig{Policies: []pkg.DataSource{{Inline: policy}}})
	mockHandle2.EXPECT().SendLocalResponse(uint32(403), gomock.Any(), []byte("Forbidden"), "opa_denied")

	postHeaders := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"POST"},
		":path":      {"/api/resource"},
		":authority": {"example.com"},
	})
	require.Equal(t, shared.HeadersStatusStop, filter2.OnRequestHeaders(postHeaders, true))
}

// Test policy that verifies a JWT token using OPA's built-in io.jwt.decode_verify.
func TestOnRequestHeaders_PolicyVerifiesJWT(t *testing.T) {
	// This policy verifies an HS256 JWT, checks the issuer, and extracts the role claim.
	// The secret "test-secret-key" is embedded in the policy for testing purposes.
	policy := `package envoy.authz

import rego.v1

default allow := {"allowed": false, "http_status": 401, "body": "Unauthorized"}

allow := {"allowed": true, "headers": {"x-jwt-role": payload.role}} if {
  auth_header := input.attributes.request.http.headers.authorization
  startswith(auth_header, "Bearer ")
  token := substring(auth_header, 7, -1)
  [valid, _, payload] := io.jwt.decode_verify(token, {
    "secret": "test-secret-key",
    "alg": "HS256",
  })
  valid == true
  payload.iss == "test-issuer"
}
`
	// Valid JWT: {"sub":"user123","role":"admin","iss":"test-issuer","exp":9999999999}, signed with "test-secret-key".
	// nolint:gosec
	validToken := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJ1c2VyMTIzIiwicm9sZSI6ImFkbWluIiwiaXNzIjoidGVzdC1pc3N1ZXIiLCJleHAiOjk5OTk5OTk5OTl9.AgWMvlXsikFYopkQ8xnqsmshOU7BrydgwdGNQBE3rog"

	// Expired JWT: {"sub":"user456","role":"viewer","iss":"test-issuer","exp":1000000000}, signed with "test-secret-key".
	// nolint:gosec
	expiredToken := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJ1c2VyNDU2Iiwicm9sZSI6InZpZXdlciIsImlzcyI6InRlc3QtaXNzdWVyIiwiZXhwIjoxMDAwMDAwMDAwfQ.CqWHLAH26GMiRdAtPaNbU2S0nCQg1k-aG0IfICVNuMU"

	// Wrong-secret JWT: {"sub":"user789","role":"admin","iss":"test-issuer","exp":9999999999}, signed with "wrong-secret".
	// nolint:gosec
	wrongSecretToken := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJ1c2VyNzg5Iiwicm9sZSI6ImFkbWluIiwiaXNzIjoidGVzdC1pc3N1ZXIiLCJleHAiOjk5OTk5OTk5OTl9.jqjuxP5_6wpMg-HrVnLMoIIXLFgWyGTO9BWhGxZOa_M"

	t.Run("valid JWT is allowed and role header is set", func(t *testing.T) {
		filter, mockHandle := createTestFilter(t, opaConfig{Policies: []pkg.DataSource{{Inline: policy}}})
		requestHeaders := fake.NewFakeHeaderMap(map[string][]string{})
		mockHandle.EXPECT().RequestHeaders().Return(requestHeaders).AnyTimes()

		headers := fake.NewFakeHeaderMap(map[string][]string{
			":method":       {"GET"},
			":path":         {"/api/resource"},
			":authority":    {"example.com"},
			":scheme":       {"https"},
			"authorization": {"Bearer " + validToken},
		})

		status := filter.OnRequestHeaders(headers, true)
		require.Equal(t, shared.HeadersStatusContinue, status)
		require.Equal(t, "admin", requestHeaders.GetOne("x-jwt-role").ToUnsafeString())
	})

	t.Run("expired JWT is denied", func(t *testing.T) {
		filter, mockHandle := createTestFilter(t, opaConfig{Policies: []pkg.DataSource{{Inline: policy}}})

		var capturedStatus uint32
		var capturedBody []byte
		mockHandle.EXPECT().SendLocalResponse(
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Eq("opa_denied"),
		).Do(func(status uint32, _ [][2]string, body []byte, _ string) {
			capturedStatus = status
			capturedBody = body
		})

		headers := fake.NewFakeHeaderMap(map[string][]string{
			":method":       {"GET"},
			":path":         {"/api/resource"},
			":authority":    {"example.com"},
			":scheme":       {"https"},
			"authorization": {"Bearer " + expiredToken},
		})

		status := filter.OnRequestHeaders(headers, true)
		require.Equal(t, shared.HeadersStatusStop, status)
		require.Equal(t, uint32(401), capturedStatus)
		require.Equal(t, []byte("Unauthorized"), capturedBody)
	})

	t.Run("wrong secret JWT is denied", func(t *testing.T) {
		filter, mockHandle := createTestFilter(t, opaConfig{Policies: []pkg.DataSource{{File: createTestPolicyFile(t, policy)}}})

		var capturedStatus uint32
		mockHandle.EXPECT().SendLocalResponse(
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Eq("opa_denied"),
		).Do(func(status uint32, _ [][2]string, _ []byte, _ string) {
			capturedStatus = status
		})

		headers := fake.NewFakeHeaderMap(map[string][]string{
			":method":       {"GET"},
			":path":         {"/api/resource"},
			":authority":    {"example.com"},
			":scheme":       {"https"},
			"authorization": {"Bearer " + wrongSecretToken},
		})

		status := filter.OnRequestHeaders(headers, true)
		require.Equal(t, shared.HeadersStatusStop, status)
		require.Equal(t, uint32(401), capturedStatus)
	})

	t.Run("missing authorization header is denied", func(t *testing.T) {
		filter, mockHandle := createTestFilter(t, opaConfig{Policies: []pkg.DataSource{{File: createTestPolicyFile(t, policy)}}})

		mockHandle.EXPECT().SendLocalResponse(
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Eq("opa_denied"),
		)

		headers := fake.NewFakeHeaderMap(map[string][]string{
			":method":    {"GET"},
			":path":      {"/api/resource"},
			":authority": {"example.com"},
			":scheme":    {"https"},
		})

		status := filter.OnRequestHeaders(headers, true)
		require.Equal(t, shared.HeadersStatusStop, status)
	})
}

// Helper to create a filter with a policy that will error at evaluation time.
// It uses rego.StrictBuiltinErrors so that division by zero produces an error
// instead of being silently undefined.
func createErrorFilter(t *testing.T, failOpen bool) (*opaHttpFilter, *mocks.MockHttpFilterHandle) {
	t.Helper()

	// This policy compiles successfully but will fail at evaluation time
	// because it divides by zero with strict builtin errors enabled.
	policy := `package envoy.authz
result := 1 / 0
`
	policyFile := createTestPolicyFile(t, policy)

	r := rego.New(
		rego.Query("result = data.envoy.authz.result"),
		rego.Module(policyFile, policy),
		rego.StrictBuiltinErrors(true),
	)

	pq, err := r.PrepareForEval(context.Background())
	require.NoError(t, err)

	parsed := &opaParsedConfig{
		opaConfig: opaConfig{
			Policies:     []pkg.DataSource{{File: policyFile}},
			DecisionPath: "envoy.authz.result",
			FailOpen:     failOpen,
		},
		preparedQuery: pq,
		metrics: opaMetrics{
			requestsTotal: shared.MetricID(1),
			enabled:       true,
		},
	}

	ctrl := gomock.NewController(t)
	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDRequestProtocol).Return(pkg.UnsafeBufferFromString("HTTP/1.1"), true).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDSourceAddress).Return(pkg.UnsafeBufferFromString("127.0.0.1:5000"), true).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDDestinationAddress).Return(pkg.UnsafeBufferFromString("127.0.0.1:80"), true).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDConnectionUriSanPeerCertificate).Return(pkg.UnsafeBufferFromString(""), false).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDConnectionDnsSanPeerCertificate).Return(pkg.UnsafeBufferFromString(""), false).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDConnectionSubjectPeerCertificate).Return(pkg.UnsafeBufferFromString(""), false).AnyTimes()
	mockHandle.EXPECT().GetAttributeBool(shared.AttributeIDConnectionMtls).Return(false, false).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDConnectionTlsVersion).Return(pkg.UnsafeBufferFromString(""), false).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(shared.AttributeIDConnectionSha256PeerCertificateDigest).Return(pkg.UnsafeBufferFromString(""), false).AnyTimes()

	filter := &opaHttpFilter{handle: mockHandle, config: parsed}
	return filter, mockHandle
}

// Tests for fail_open behavior on evaluation errors.

func TestOnRequestHeaders_FailOpen_AllowsOnError(t *testing.T) {
	filter, mockHandle := createErrorFilter(t, true)
	mockHandle.EXPECT().IncrementCounterValue(shared.MetricID(1), uint64(1), "failopen").Return(shared.MetricsSuccess)

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/api/resource"},
		":authority": {"example.com"},
		":scheme":    {"http"},
	})

	// With fail_open=true, evaluation errors should allow the request.
	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusContinue, status)
}

func TestOnRequestHeaders_FailClosed_DeniesOnError(t *testing.T) {
	filter, mockHandle := createErrorFilter(t, false)

	mockHandle.EXPECT().SendLocalResponse(
		uint32(500), gomock.Nil(), []byte("Internal Server Error"), "opa_eval_error",
	)
	mockHandle.EXPECT().IncrementCounterValue(shared.MetricID(1), uint64(1), "denied").Return(shared.MetricsSuccess)

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/api/resource"},
		":authority": {"example.com"},
		":scheme":    {"http"},
	})

	// With fail_open=false (default), evaluation errors should deny with 500.
	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStop, status)
}

// Test with an absolute path to a non-existent rego file to trigger read error in ConfigFactory.
func TestConfigFactory_Create_PolicyFileReadError(t *testing.T) {
	nonExistent := filepath.Join(t.TempDir(), "does-not-exist.rego")
	cfg := opaConfig{Policies: []pkg.DataSource{{File: nonExistent}}}
	configJSON, err := json.Marshal(cfg)
	require.NoError(t, err)

	factory := &OPAHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	filterFactory, err := factory.Create(mockHandle, configJSON)
	require.Error(t, err)
	require.Nil(t, filterFactory)
}

// Tests for with_body

func TestOnRequestHeaders_WithBody_StopsForBody(t *testing.T) {
	policy := `package envoy.authz
default allow := false
allow if { input.body.user == "admin" }
`
	filter, _ := createTestFilter(t, opaConfig{
		WithBody: true,
		Policies: []pkg.DataSource{{Inline: policy}},
	})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":      {"POST"},
		":path":        {"/api/resource"},
		":authority":   {"example.com"},
		":scheme":      {"http"},
		"content-type": {"application/json"},
	})

	// With body enabled and endOfStream=false, headers processing must stop to wait for the body.
	status := filter.OnRequestHeaders(headers, false)
	require.Equal(t, shared.HeadersStatusStop, status)
}

func TestOnRequestHeaders_WithBody_EvaluatesImmediatelyOnEndOfStream(t *testing.T) {
	policy := `package envoy.authz
default allow := true
`
	filter, _ := createTestFilter(t, opaConfig{
		WithBody: true,
		Policies: []pkg.DataSource{{Inline: policy}},
	})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/api/resource"},
		":authority": {"example.com"},
		":scheme":    {"http"},
	})

	// When endOfStream=true (no body), evaluate immediately without body.
	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusContinue, status)
}

func TestOnRequestBody_AllowedByBodyPolicy(t *testing.T) {
	policy := `package envoy.authz
default allow := false
allow if { input.body.user == "admin" }
`
	filter, mockHandle := createTestFilter(t, opaConfig{
		WithBody: true,
		Policies: []pkg.DataSource{{Inline: policy}},
	})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":      {"POST"},
		":path":        {"/api/resource"},
		":authority":   {"example.com"},
		":scheme":      {"http"},
		"content-type": {"application/json"},
	})
	status := filter.OnRequestHeaders(headers, false)
	require.Equal(t, shared.HeadersStatusStop, status)

	body := []byte(`{"user": "admin"}`)
	fakeBody := fake.NewFakeBodyBuffer(body)
	mockHandle.EXPECT().RequestHeaders().Return(headers).AnyTimes()
	mockHandle.EXPECT().BufferedRequestBody().Return(fakeBody)
	mockHandle.EXPECT().ReceivedRequestBody().Return(fakeBody)

	bodyStatus := filter.OnRequestBody(fakeBody, true)
	require.Equal(t, shared.BodyStatusContinue, bodyStatus)
}

func TestOnRequestBody_DeniedByBodyPolicy(t *testing.T) {
	policy := `package envoy.authz
default allow := false
allow if { input.body.user == "admin" }
`
	filter, mockHandle := createTestFilter(t, opaConfig{
		WithBody: true,
		Policies: []pkg.DataSource{{Inline: policy}},
	})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":      {"POST"},
		":path":        {"/api/resource"},
		":authority":   {"example.com"},
		":scheme":      {"http"},
		"content-type": {"application/json"},
	})
	status := filter.OnRequestHeaders(headers, false)
	require.Equal(t, shared.HeadersStatusStop, status)

	body := []byte(`{"user": "guest"}`)
	fakeBody := fake.NewFakeBodyBuffer(body)
	mockHandle.EXPECT().RequestHeaders().Return(headers).AnyTimes()
	mockHandle.EXPECT().BufferedRequestBody().Return(fakeBody)
	mockHandle.EXPECT().ReceivedRequestBody().Return(fakeBody)
	mockHandle.EXPECT().SendLocalResponse(uint32(403), gomock.Any(), []byte("Forbidden"), "opa_denied")

	bodyStatus := filter.OnRequestBody(fakeBody, true)
	require.Equal(t, shared.BodyStatusStopNoBuffer, bodyStatus)
}

func TestOnRequestBody_NonJsonBodyEvaluatesWithNilParsedBody(t *testing.T) {
	policy := `package envoy.authz
default allow := false
allow if { not input.body }
`
	filter, mockHandle := createTestFilter(t, opaConfig{
		WithBody: true,
		Policies: []pkg.DataSource{{Inline: policy}},
	})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":      {"POST"},
		":path":        {"/api/resource"},
		":authority":   {"example.com"},
		":scheme":      {"http"},
		"content-type": {"text/plain"},
	})
	status := filter.OnRequestHeaders(headers, false)
	require.Equal(t, shared.HeadersStatusStop, status)

	// Non-JSON content-type: body is not read; input.body is absent from the policy input.
	fakeBody := fake.NewFakeBodyBuffer([]byte(`not json at all`))
	mockHandle.EXPECT().RequestHeaders().Return(headers).AnyTimes()

	bodyStatus := filter.OnRequestBody(fakeBody, true)
	require.Equal(t, shared.BodyStatusContinue, bodyStatus)
}

func TestOnRequestBody_BufferingUntilEndOfStream(t *testing.T) {
	policy := `package envoy.authz
default allow := true
`
	filter, _ := createTestFilter(t, opaConfig{
		WithBody: true,
		Policies: []pkg.DataSource{{Inline: policy}},
	})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":      {"POST"},
		":path":        {"/api/resource"},
		":authority":   {"example.com"},
		":scheme":      {"http"},
		"content-type": {"application/json"},
	})
	status := filter.OnRequestHeaders(headers, false)
	require.Equal(t, shared.HeadersStatusStop, status)

	// First chunk, not end of stream: should buffer.
	chunk := fake.NewFakeBodyBuffer([]byte(`{"user"`))
	bodyStatus := filter.OnRequestBody(chunk, false)
	require.Equal(t, shared.BodyStatusStopAndBuffer, bodyStatus)
}

func TestOnRequestBody_WithTrailers(t *testing.T) {
	policy := `package envoy.authz
default allow := false
allow if { input.body.user == "admin" }
`
	filter, mockHandle := createTestFilter(t, opaConfig{
		WithBody: true,
		Policies: []pkg.DataSource{{Inline: policy}},
	})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":      {"POST"},
		":path":        {"/api/resource"},
		":authority":   {"example.com"},
		":scheme":      {"http"},
		"content-type": {"application/json"},
	})
	status := filter.OnRequestHeaders(headers, false)
	require.Equal(t, shared.HeadersStatusStop, status)

	// Body chunk, not end of stream.
	chunk := fake.NewFakeBodyBuffer([]byte(`{"user"`))
	bodyStatus := filter.OnRequestBody(chunk, false)
	require.Equal(t, shared.BodyStatusStopAndBuffer, bodyStatus)

	// Full body available when trailers arrive.
	fullBody := fake.NewFakeBodyBuffer([]byte(`{"user": "admin"}`))
	mockHandle.EXPECT().RequestHeaders().Return(headers).AnyTimes()
	mockHandle.EXPECT().BufferedRequestBody().Return(fullBody)
	mockHandle.EXPECT().ReceivedRequestBody().Return(fullBody)

	trailers := fake.NewFakeHeaderMap(map[string][]string{})
	trailersStatus := filter.OnRequestTrailers(trailers)
	require.Equal(t, shared.TrailersStatusContinue, trailersStatus)
}

func TestOnRequestBody_AlreadyProcessed(t *testing.T) {
	policy := `package envoy.authz
default allow := true
`
	filter, _ := createTestFilter(t, opaConfig{
		WithBody: true,
		Policies: []pkg.DataSource{{Inline: policy}},
	})
	filter.requestProcessed = true

	bodyStatus := filter.OnRequestBody(nil, true)
	require.Equal(t, shared.BodyStatusContinue, bodyStatus)
}

func TestOnRequestTrailers_AlreadyProcessed(t *testing.T) {
	policy := `package envoy.authz
default allow := true
`
	filter, _ := createTestFilter(t, opaConfig{
		WithBody: true,
		Policies: []pkg.DataSource{{Inline: policy}},
	})
	filter.requestProcessed = true

	trailers := fake.NewFakeHeaderMap(map[string][]string{})
	trailersStatus := filter.OnRequestTrailers(trailers)
	require.Equal(t, shared.TrailersStatusContinue, trailersStatus)
}

// Tests for dynamicMetadataMap / buildInput dynamic metadata

func TestBuildInput_DynamicMetadata_MultipleNamespacesAndKeys(t *testing.T) {
	policy := `package envoy.authz
default allow := true
`
	policyFile := createTestPolicyFile(t, policy)
	cfg := opaConfig{
		Policies:           []pkg.DataSource{{File: policyFile}},
		MetadataNamespaces: []string{"ns.auth", "ns.ratelimit"},
	}
	configJSON, err := json.Marshal(cfg)
	require.NoError(t, err)

	factory := &OPAHttpFilterConfigFactory{}
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockConfigHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockConfigHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockConfigHandle.EXPECT().DefineCounter("opa_requests_total", "decision").Return(shared.MetricID(1), shared.MetricsSuccess)

	filterFactory, err := factory.Create(mockConfigHandle, configJSON)
	require.NoError(t, err)

	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockHandle.EXPECT().GetAttributeString(gomock.Any()).Return(pkg.UnsafeBufferFromString(""), false).AnyTimes()
	mockHandle.EXPECT().GetAttributeBool(gomock.Any()).Return(false, false).AnyTimes()

	// Namespace "ns.auth" has two string keys.
	mockHandle.EXPECT().GetMetadataKeys(shared.MetadataSourceTypeDynamic, "ns.auth").Return([]shared.UnsafeEnvoyBuffer{
		pkg.UnsafeBufferFromString("identity"),
		pkg.UnsafeBufferFromString("role"),
	})
	mockHandle.EXPECT().GetMetadataString(shared.MetadataSourceTypeDynamic, "ns.auth", "identity").Return(pkg.UnsafeBufferFromString("user-123"), true)
	mockHandle.EXPECT().GetMetadataString(shared.MetadataSourceTypeDynamic, "ns.auth", "role").Return(pkg.UnsafeBufferFromString("admin"), true)

	// Namespace "ns.ratelimit" has a string key
	mockHandle.EXPECT().GetMetadataKeys(shared.MetadataSourceTypeDynamic, "ns.ratelimit").Return([]shared.UnsafeEnvoyBuffer{
		pkg.UnsafeBufferFromString("bucket"),
	})
	mockHandle.EXPECT().GetMetadataString(shared.MetadataSourceTypeDynamic, "ns.ratelimit", "bucket").Return(pkg.UnsafeBufferFromString("default"), true)

	filter := filterFactory.Create(mockHandle).(*opaHttpFilter)

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/api/resource"},
		":authority": {"example.com"},
	})

	input := filter.buildInput(headers, nil)

	// Verify dynamic_metadata is present and structured by namespace.
	dm, ok := input["dynamic_metadata"].(map[string]any)
	require.True(t, ok)
	require.Len(t, dm, 2, "expected two namespaces in dynamic_metadata")

	// Verify ns.auth namespace.
	authNs, ok := dm["ns.auth"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "user-123", authNs["identity"])
	require.Equal(t, "admin", authNs["role"])

	// Verify ns.ratelimit namespace.
	rlNs, ok := dm["ns.ratelimit"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "default", rlNs["bucket"])
}

func TestBuildInput_DynamicMetadata_Empty(t *testing.T) {
	policy := `package envoy.authz
default allow := true
`
	filter, _ := createTestFilter(t, opaConfig{Policies: []pkg.DataSource{{Inline: policy}}})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/"},
		":authority": {"example.com"},
	})

	input := filter.buildInput(headers, nil)

	// When no dynamic metadata exists, the map should be empty.
	dm, ok := input["dynamic_metadata"].(map[string]any)
	require.True(t, ok)
	require.Empty(t, dm)
}
