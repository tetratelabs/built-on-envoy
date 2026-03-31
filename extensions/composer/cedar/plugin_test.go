// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package cedar

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	cedarlib "github.com/cedar-policy/cedar-go"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/fake"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/pkg"
)

// Helper to create a temporary policy file for testing.
func createTestPolicyFile(t *testing.T, policy string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "policy-*.cedar")
	require.NoError(t, err)
	_, err = f.WriteString(policy)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return f.Name()
}

// Helper to create a temporary entities file for testing.
func createTestEntitiesFile(t *testing.T, entities string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "entities-*.json")
	require.NoError(t, err)
	_, err = f.WriteString(entities)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return f.Name()
}

// Helper to create a filter for testing.
func createTestFilter(t *testing.T, cfg *cedarConfig) (*cedarHttpFilter, *mocks.MockHttpFilterHandle) {
	t.Helper()

	if cfg.PrincipalType == "" {
		cfg.PrincipalType = "User"
	}
	if cfg.PrincipalIDHeader == "" {
		cfg.PrincipalIDHeader = "x-user-id"
	}

	configJSON, err := json.Marshal(cfg)
	require.NoError(t, err)

	factory := &CedarHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	mockConfigHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockConfigHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockConfigHandle.EXPECT().DefineCounter("cedar_requests_total", "decision").Return(shared.MetricID(1), shared.MetricsSuccess).AnyTimes()

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
	cedarFilter, ok := filter.(*cedarHttpFilter)
	require.True(t, ok)

	return cedarFilter, mockHandle
}

// Tests for CedarHttpFilterConfigFactory.Create

func TestConfigFactory_Create_ValidConfig(t *testing.T) {
	policy := `permit(principal, action, resource);`
	policyFile := createTestPolicyFile(t, policy)

	configJSON, err := json.Marshal(cedarConfig{
		Policy:            pkg.DataSource{File: policyFile},
		PrincipalType:     "User",
		PrincipalIDHeader: "x-user-id",
	})
	require.NoError(t, err)

	factory := &CedarHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockHandle.EXPECT().DefineCounter("cedar_requests_total", "decision").Return(shared.MetricID(1), shared.MetricsSuccess)

	filterFactory, err := factory.Create(mockHandle, configJSON)
	require.NoError(t, err)
	require.NotNil(t, filterFactory)
}

func TestConfigFactory_Create_EmptyConfig(t *testing.T) {
	factory := &CedarHttpFilterConfigFactory{}

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
	factory := &CedarHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	filterFactory, err := factory.Create(mockHandle, []byte("{invalid"))
	require.Error(t, err)
	require.Nil(t, filterFactory)
}

func TestConfigFactory_Create_MissingPolicyFile(t *testing.T) {
	configJSON, err := json.Marshal(cedarConfig{
		PrincipalType:     "User",
		PrincipalIDHeader: "x-user-id",
	})
	require.NoError(t, err)

	factory := &CedarHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	filterFactory, err := factory.Create(mockHandle, configJSON)
	require.Error(t, err)
	require.Nil(t, filterFactory)
	require.ErrorIs(t, err, pkg.ErrDataSourceNeitherSet)
}

func TestConfigFactory_Create_InlinePolicyAndPolicyFile(t *testing.T) {
	configJSON, err := json.Marshal(cedarConfig{
		Policy: pkg.DataSource{
			File:   "some_policy_file.cedar",
			Inline: "permit(principal, action, resource);",
		},
		PrincipalType:     "User",
		PrincipalIDHeader: "x-user-id",
	})
	require.NoError(t, err)

	factory := &CedarHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	filterFactory, err := factory.Create(mockHandle, configJSON)
	require.Error(t, err)
	require.Nil(t, filterFactory)
	require.ErrorIs(t, err, pkg.ErrDataSourceBothSet)
}

func TestConfigFactory_Create_MissingPrincipalType(t *testing.T) {
	policy := `permit(principal, action, resource);`
	policyFile := createTestPolicyFile(t, policy)

	configJSON, err := json.Marshal(cedarConfig{
		Policy:            pkg.DataSource{File: policyFile},
		PrincipalIDHeader: "x-user-id",
	})
	require.NoError(t, err)

	factory := &CedarHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	filterFactory, err := factory.Create(mockHandle, configJSON)
	require.Error(t, err)
	require.Nil(t, filterFactory)
	require.Contains(t, err.Error(), "principal_type is required")
}

func TestConfigFactory_Create_MissingPrincipalIDHeader(t *testing.T) {
	policy := `permit(principal, action, resource);`
	policyFile := createTestPolicyFile(t, policy)

	configJSON, err := json.Marshal(cedarConfig{
		Policy:        pkg.DataSource{File: policyFile},
		PrincipalType: "User",
	})
	require.NoError(t, err)

	factory := &CedarHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	filterFactory, err := factory.Create(mockHandle, configJSON)
	require.Error(t, err)
	require.Nil(t, filterFactory)
	require.Contains(t, err.Error(), "principal_id_header is required")
}

func TestConfigFactory_Create_PolicyFileNotFound(t *testing.T) {
	configJSON, err := json.Marshal(cedarConfig{
		Policy:            pkg.DataSource{File: "/nonexistent/policy.cedar"},
		PrincipalType:     "User",
		PrincipalIDHeader: "x-user-id",
	})
	require.NoError(t, err)

	factory := &CedarHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	filterFactory, err := factory.Create(mockHandle, configJSON)
	require.Error(t, err)
	require.Nil(t, filterFactory)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestConfigFactory_Create_InvalidCedarPolicy(t *testing.T) {
	tests := []struct {
		name string
		cfg  cedarConfig
	}{
		{"inline", cedarConfig{
			Policy:            pkg.DataSource{Inline: "this is not valid cedar {{{"},
			PrincipalType:     "User",
			PrincipalIDHeader: "x-user-id",
		}},
		{"file", cedarConfig{
			Policy:            pkg.DataSource{File: createTestPolicyFile(t, "this is not valid cedar {{{")},
			PrincipalType:     "User",
			PrincipalIDHeader: "x-user-id",
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configJSON, err := json.Marshal(tt.cfg)
			require.NoError(t, err)

			factory := &CedarHttpFilterConfigFactory{}

			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
			mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

			filterFactory, err := factory.Create(mockHandle, configJSON)
			require.Error(t, err)
			require.Nil(t, filterFactory)
			require.Contains(t, err.Error(), "failed to parse policy")
		})
	}
}

func TestConfigFactory_Create_PolicyFileReadError(t *testing.T) {
	nonExistent := filepath.Join(t.TempDir(), "does-not-exist.cedar")
	cfg := cedarConfig{
		Policy:            pkg.DataSource{File: nonExistent},
		PrincipalType:     "User",
		PrincipalIDHeader: "x-user-id",
	}
	configJSON, err := json.Marshal(cfg)
	require.NoError(t, err)

	factory := &CedarHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	filterFactory, err := factory.Create(mockHandle, configJSON)
	require.Error(t, err)
	require.Nil(t, filterFactory)
}

func TestConfigFactory_Create_ValidConfigWithEntities(t *testing.T) {
	policy := `permit(principal in Group::"admins", action, resource);`
	policyFile := createTestPolicyFile(t, policy)

	entities := `[
		{"uid": {"type": "User", "id": "alice"}, "parents": [{"type": "Group", "id": "admins"}], "attrs": {}},
		{"uid": {"type": "Group", "id": "admins"}, "parents": [], "attrs": {}}
	]`
	entitiesFile := createTestEntitiesFile(t, entities)

	configJSON, err := json.Marshal(cedarConfig{
		Policy:            pkg.DataSource{File: policyFile},
		EntitiesFile:      entitiesFile,
		PrincipalType:     "User",
		PrincipalIDHeader: "x-user-id",
	})
	require.NoError(t, err)

	factory := &CedarHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockHandle.EXPECT().DefineCounter("cedar_requests_total", "decision").Return(shared.MetricID(1), shared.MetricsSuccess)

	filterFactory, err := factory.Create(mockHandle, configJSON)
	require.NoError(t, err)
	require.NotNil(t, filterFactory)
}

func TestConfigFactory_Create_EntitiesFileNotFound(t *testing.T) {
	policy := `permit(principal, action, resource);`
	policyFile := createTestPolicyFile(t, policy)

	configJSON, err := json.Marshal(cedarConfig{
		Policy:            pkg.DataSource{File: policyFile},
		EntitiesFile:      "/nonexistent/entities.json",
		PrincipalType:     "User",
		PrincipalIDHeader: "x-user-id",
	})
	require.NoError(t, err)

	factory := &CedarHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	filterFactory, err := factory.Create(mockHandle, configJSON)
	require.Error(t, err)
	require.Nil(t, filterFactory)
	require.Contains(t, err.Error(), "failed to read entities file")
}

func TestConfigFactory_Create_InvalidEntitiesJSON(t *testing.T) {
	policy := `permit(principal, action, resource);`
	policyFile := createTestPolicyFile(t, policy)
	entitiesFile := createTestEntitiesFile(t, "not valid json {{{")

	configJSON, err := json.Marshal(cedarConfig{
		Policy:            pkg.DataSource{File: policyFile},
		EntitiesFile:      entitiesFile,
		PrincipalType:     "User",
		PrincipalIDHeader: "x-user-id",
	})
	require.NoError(t, err)

	factory := &CedarHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	filterFactory, err := factory.Create(mockHandle, configJSON)
	require.Error(t, err)
	require.Nil(t, filterFactory)
	require.Contains(t, err.Error(), "failed to parse entities")
}

func TestConfigFactory_Create_DefaultEntityTypes(t *testing.T) {
	policy := `permit(principal, action, resource);`
	policyFile := createTestPolicyFile(t, policy)

	cfg := cedarConfig{
		Policy:            pkg.DataSource{File: policyFile},
		PrincipalType:     "User",
		PrincipalIDHeader: "x-user-id",
	}
	configJSON, err := json.Marshal(cfg)
	require.NoError(t, err)

	factory := &CedarHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockHandle.EXPECT().DefineCounter("cedar_requests_total", "decision").Return(shared.MetricID(1), shared.MetricsSuccess)

	filterFactory, err := factory.Create(mockHandle, configJSON)
	require.NoError(t, err)
	require.NotNil(t, filterFactory)

	cedarFactory, ok := filterFactory.(*cedarHttpFilterFactory)
	require.True(t, ok)
	require.Empty(t, cedarFactory.config.ActionType)
	require.Empty(t, cedarFactory.config.ResourceType)
}

func TestConfigFactory_Create_CustomEntityTypes(t *testing.T) {
	policy := `permit(principal, action, resource);`
	policyFile := createTestPolicyFile(t, policy)

	cfg := cedarConfig{
		Policy:            pkg.DataSource{File: policyFile},
		PrincipalType:     "User",
		PrincipalIDHeader: "x-user-id",
		ActionType:        "HttpMethod",
		ResourceType:      "Endpoint",
	}
	configJSON, err := json.Marshal(cfg)
	require.NoError(t, err)

	factory := &CedarHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockHandle.EXPECT().DefineCounter("cedar_requests_total", "decision").Return(shared.MetricID(1), shared.MetricsSuccess)

	filterFactory, err := factory.Create(mockHandle, configJSON)
	require.NoError(t, err)
	require.NotNil(t, filterFactory)

	cedarFactory, ok := filterFactory.(*cedarHttpFilterFactory)
	require.True(t, ok)
	require.Equal(t, "HttpMethod", cedarFactory.config.ActionType)
	require.Equal(t, "Endpoint", cedarFactory.config.ResourceType)
}

func TestConfigFactory_Create_MultiplePoliciesInFile(t *testing.T) {
	policy := `
permit(principal, action == Action::"GET", resource);
forbid(principal, action == Action::"DELETE", resource);
`
	policyFile := createTestPolicyFile(t, policy)

	configJSON, err := json.Marshal(cedarConfig{
		Policy:            pkg.DataSource{File: policyFile},
		PrincipalType:     "User",
		PrincipalIDHeader: "x-user-id",
	})
	require.NoError(t, err)

	factory := &CedarHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockHandle.EXPECT().DefineCounter("cedar_requests_total", "decision").Return(shared.MetricID(1), shared.MetricsSuccess)

	filterFactory, err := factory.Create(mockHandle, configJSON)
	require.NoError(t, err)
	require.NotNil(t, filterFactory)
}

// Tests for OnRequestHeaders

func TestOnRequestHeaders_Allow(t *testing.T) {
	policy := `permit(principal, action == Action::"GET", resource);`
	filter, _ := createTestFilter(t, &cedarConfig{Policy: pkg.DataSource{Inline: policy}})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/api/resource"},
		":authority": {"example.com"},
		":scheme":    {"http"},
		"x-user-id":  {"alice"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusContinue, status)
}

func TestOnRequestHeaders_Deny(t *testing.T) {
	policy := `forbid(principal, action, resource);`
	filter, mockHandle := createTestFilter(t, &cedarConfig{Policy: pkg.DataSource{Inline: policy}})
	mockHandle.EXPECT().SendLocalResponse(
		uint32(403),
		gomock.Any(),
		[]byte("Forbidden"),
		"cedar_denied",
	)

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/api/resource"},
		":authority": {"example.com"},
		":scheme":    {"http"},
		"x-user-id":  {"alice"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStop, status)
}

func TestOnRequestHeaders_DenyDefaultNoPermit(t *testing.T) {
	// Cedar's default behavior: if no permit policy matches, deny.
	policy := `permit(principal == User::"admin", action, resource);`
	filter, mockHandle := createTestFilter(t, &cedarConfig{Policy: pkg.DataSource{Inline: policy}})
	mockHandle.EXPECT().SendLocalResponse(
		uint32(403),
		gomock.Any(),
		[]byte("Forbidden"),
		"cedar_denied",
	)

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/api/resource"},
		":authority": {"example.com"},
		":scheme":    {"http"},
		"x-user-id":  {"alice"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStop, status)
}

func TestOnRequestHeaders_DenyWithCustomStatus(t *testing.T) {
	policy := `forbid(principal, action, resource);`
	filter, mockHandle := createTestFilter(t, &cedarConfig{
		Policy:     pkg.DataSource{Inline: policy},
		DenyStatus: 401,
		DenyBody:   "Unauthorized",
	})

	var capturedStatus uint32
	var capturedBody []byte
	mockHandle.EXPECT().SendLocalResponse(
		gomock.Any(), gomock.Any(), gomock.Any(), gomock.Eq("cedar_denied"),
	).Do(func(status uint32, _ [][2]string, body []byte, _ string) {
		capturedStatus = status
		capturedBody = body
	})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/api/resource"},
		":authority": {"example.com"},
		":scheme":    {"http"},
		"x-user-id":  {"alice"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStop, status)
	require.Equal(t, uint32(401), capturedStatus)
	require.Equal(t, []byte("Unauthorized"), capturedBody)
}

func TestOnRequestHeaders_DenyWithCustomHeaders(t *testing.T) {
	policy := `forbid(principal, action, resource);`
	filter, mockHandle := createTestFilter(t, &cedarConfig{
		Policy:      pkg.DataSource{File: createTestPolicyFile(t, policy)},
		DenyHeaders: map[string]string{"www-authenticate": "Bearer"},
	})

	var capturedHeaders [][2]string
	mockHandle.EXPECT().SendLocalResponse(
		gomock.Any(), gomock.Any(), gomock.Any(), gomock.Eq("cedar_denied"),
	).Do(func(_ uint32, headers [][2]string, _ []byte, _ string) {
		capturedHeaders = headers
	})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/api/resource"},
		":authority": {"example.com"},
		":scheme":    {"http"},
		"x-user-id":  {"alice"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStop, status)
	require.Len(t, capturedHeaders, 1)
	require.Equal(t, "www-authenticate", capturedHeaders[0][0])
	require.Equal(t, "Bearer", capturedHeaders[0][1])
}

func TestOnRequestHeaders_DryRunAllow(t *testing.T) {
	policy := `permit(principal, action, resource);`
	filter, _ := createTestFilter(t, &cedarConfig{Policy: pkg.DataSource{Inline: policy}, DryRun: true})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/api/resource"},
		":authority": {"example.com"},
		":scheme":    {"http"},
		"x-user-id":  {"alice"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusContinue, status)
}

func TestOnRequestHeaders_DryRunDeny(t *testing.T) {
	policy := `forbid(principal, action, resource);`
	// In dry-run mode, even denied requests should continue.
	filter, _ := createTestFilter(t, &cedarConfig{Policy: pkg.DataSource{Inline: policy}, DryRun: true})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/api/resource"},
		":authority": {"example.com"},
		":scheme":    {"http"},
		"x-user-id":  {"alice"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusContinue, status)
}

func TestOnRequestHeaders_MissingPrincipalHeader_FailOpen(t *testing.T) {
	policy := `permit(principal, action, resource);`
	filter, _ := createTestFilter(t, &cedarConfig{Policy: pkg.DataSource{Inline: policy}, FailOpen: true})

	// No x-user-id header.
	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/api/resource"},
		":authority": {"example.com"},
		":scheme":    {"http"},
	})

	// With fail_open=true, missing principal header should allow the request.
	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusContinue, status)
}

// Tests for metrics

// createTestFilterWithMetricExpectation creates a filter that expects a specific metric decision tag.
func createTestFilterWithMetricExpectation(t *testing.T, cfg *cedarConfig, expectedDecision string) (*cedarHttpFilter, *mocks.MockHttpFilterHandle) {
	t.Helper()

	if cfg.PrincipalType == "" {
		cfg.PrincipalType = "User"
	}
	if cfg.PrincipalIDHeader == "" {
		cfg.PrincipalIDHeader = "x-user-id"
	}

	configJSON, err := json.Marshal(cfg)
	require.NoError(t, err)

	factory := &CedarHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	mockConfigHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockConfigHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockConfigHandle.EXPECT().DefineCounter("cedar_requests_total", "decision").Return(shared.MetricID(1), shared.MetricsSuccess)

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
	cedarFilter, ok := filter.(*cedarHttpFilter)
	require.True(t, ok)

	return cedarFilter, mockHandle
}

func TestOnRequestHeaders_Metrics_Allowed(t *testing.T) {
	policy := `permit(principal, action, resource);`
	filter, _ := createTestFilterWithMetricExpectation(t, &cedarConfig{Policy: pkg.DataSource{Inline: policy}}, decisionAllowed)

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/api/resource"},
		":authority": {"example.com"},
		":scheme":    {"http"},
		"x-user-id":  {"alice"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusContinue, status)
}

func TestOnRequestHeaders_Metrics_Denied(t *testing.T) {
	policy := `forbid(principal, action, resource);`
	filter, mockHandle := createTestFilterWithMetricExpectation(t, &cedarConfig{Policy: pkg.DataSource{Inline: policy}}, decisionDenied)
	mockHandle.EXPECT().SendLocalResponse(uint32(403), gomock.Any(), []byte("Forbidden"), "cedar_denied")

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/api/resource"},
		":authority": {"example.com"},
		":scheme":    {"http"},
		"x-user-id":  {"alice"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStop, status)
}

func TestOnRequestHeaders_Metrics_FailOpen(t *testing.T) {
	policy := `permit(principal, action, resource);`
	filter, _ := createTestFilterWithMetricExpectation(t, &cedarConfig{Policy: pkg.DataSource{Inline: policy}, FailOpen: true}, decisionFailOpen)

	// No x-user-id header triggers an error, which should be allowed via fail_open.
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
	policy := `forbid(principal, action, resource);`
	filter, _ := createTestFilterWithMetricExpectation(t, &cedarConfig{Policy: pkg.DataSource{Inline: policy}, DryRun: true}, decisionDryAllow)

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/api/resource"},
		":authority": {"example.com"},
		":scheme":    {"http"},
		"x-user-id":  {"alice"},
	})

	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusContinue, status)
}

func TestOnRequestHeaders_MissingPrincipalHeader_FailClosed(t *testing.T) {
	policy := `permit(principal, action, resource);`
	filter, mockHandle := createTestFilter(t, &cedarConfig{Policy: pkg.DataSource{Inline: policy}})

	mockHandle.EXPECT().SendLocalResponse(
		uint32(403), gomock.Nil(), []byte("Forbidden"), "cedar_denied",
	)

	// No x-user-id header.
	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/api/resource"},
		":authority": {"example.com"},
		":scheme":    {"http"},
	})

	// With fail_open=false (default), missing principal header should deny with 500.
	status := filter.OnRequestHeaders(headers, true)
	require.Equal(t, shared.HeadersStatusStop, status)
}

func TestOnRequestHeaders_PolicyUsesMethod(t *testing.T) {
	policy := `permit(principal, action == Action::"GET", resource);`

	t.Run("GET is allowed", func(t *testing.T) {
		filter, _ := createTestFilter(t, &cedarConfig{Policy: pkg.DataSource{Inline: policy}})

		headers := fake.NewFakeHeaderMap(map[string][]string{
			":method":    {"GET"},
			":path":      {"/api/resource"},
			":authority": {"example.com"},
			"x-user-id":  {"alice"},
		})
		require.Equal(t, shared.HeadersStatusContinue, filter.OnRequestHeaders(headers, true))
	})

	t.Run("POST is denied", func(t *testing.T) {
		filter, mockHandle := createTestFilter(t, &cedarConfig{Policy: pkg.DataSource{Inline: policy}})
		mockHandle.EXPECT().SendLocalResponse(uint32(403), gomock.Any(), []byte("Forbidden"), "cedar_denied")

		headers := fake.NewFakeHeaderMap(map[string][]string{
			":method":    {"POST"},
			":path":      {"/api/resource"},
			":authority": {"example.com"},
			"x-user-id":  {"alice"},
		})
		require.Equal(t, shared.HeadersStatusStop, filter.OnRequestHeaders(headers, true))
	})
}

func TestOnRequestHeaders_PolicyUsesPath(t *testing.T) {
	policy := `permit(principal, action, resource == Resource::"/public/resource");`

	t.Run("matching path is allowed", func(t *testing.T) {
		filter, _ := createTestFilter(t, &cedarConfig{Policy: pkg.DataSource{Inline: policy}})

		headers := fake.NewFakeHeaderMap(map[string][]string{
			":method":    {"GET"},
			":path":      {"/public/resource"},
			":authority": {"example.com"},
			"x-user-id":  {"alice"},
		})
		require.Equal(t, shared.HeadersStatusContinue, filter.OnRequestHeaders(headers, true))
	})

	t.Run("non-matching path is denied", func(t *testing.T) {
		filter, mockHandle := createTestFilter(t, &cedarConfig{Policy: pkg.DataSource{Inline: policy}})
		mockHandle.EXPECT().SendLocalResponse(uint32(403), gomock.Any(), []byte("Forbidden"), "cedar_denied")

		headers := fake.NewFakeHeaderMap(map[string][]string{
			":method":    {"GET"},
			":path":      {"/private/resource"},
			":authority": {"example.com"},
			"x-user-id":  {"alice"},
		})
		require.Equal(t, shared.HeadersStatusStop, filter.OnRequestHeaders(headers, true))
	})
}

func TestOnRequestHeaders_PolicyUsesPrincipal(t *testing.T) {
	policy := `permit(principal == User::"admin", action, resource);`

	t.Run("matching principal is allowed", func(t *testing.T) {
		filter, _ := createTestFilter(t, &cedarConfig{Policy: pkg.DataSource{Inline: policy}})

		headers := fake.NewFakeHeaderMap(map[string][]string{
			":method":    {"GET"},
			":path":      {"/api/resource"},
			":authority": {"example.com"},
			"x-user-id":  {"admin"},
		})
		require.Equal(t, shared.HeadersStatusContinue, filter.OnRequestHeaders(headers, true))
	})

	t.Run("non-matching principal is denied", func(t *testing.T) {
		filter, mockHandle := createTestFilter(t, &cedarConfig{Policy: pkg.DataSource{Inline: policy}})
		mockHandle.EXPECT().SendLocalResponse(uint32(403), gomock.Any(), []byte("Forbidden"), "cedar_denied")

		headers := fake.NewFakeHeaderMap(map[string][]string{
			":method":    {"GET"},
			":path":      {"/api/resource"},
			":authority": {"example.com"},
			"x-user-id":  {"alice"},
		})
		require.Equal(t, shared.HeadersStatusStop, filter.OnRequestHeaders(headers, true))
	})
}

func TestOnRequestHeaders_PolicyUsesContext(t *testing.T) {
	policy := `permit(principal, action, resource) when { context.request.headers.authorization == "Bearer valid-token" };`

	t.Run("matching header is allowed", func(t *testing.T) {
		filter, _ := createTestFilter(t, &cedarConfig{Policy: pkg.DataSource{Inline: policy}})

		headers := fake.NewFakeHeaderMap(map[string][]string{
			":method":       {"GET"},
			":path":         {"/api/resource"},
			":authority":    {"example.com"},
			"x-user-id":     {"alice"},
			"authorization": {"Bearer valid-token"},
		})
		require.Equal(t, shared.HeadersStatusContinue, filter.OnRequestHeaders(headers, true))
	})

	t.Run("non-matching header is denied", func(t *testing.T) {
		filter, mockHandle := createTestFilter(t, &cedarConfig{Policy: pkg.DataSource{Inline: policy}})
		mockHandle.EXPECT().SendLocalResponse(uint32(403), gomock.Any(), []byte("Forbidden"), "cedar_denied")

		headers := fake.NewFakeHeaderMap(map[string][]string{
			":method":       {"GET"},
			":path":         {"/api/resource"},
			":authority":    {"example.com"},
			"x-user-id":     {"alice"},
			"authorization": {"Bearer invalid-token"},
		})
		require.Equal(t, shared.HeadersStatusStop, filter.OnRequestHeaders(headers, true))
	})
}

// Tests for policies using `when` and `unless` clauses with various context fields.

func TestOnRequestHeaders_WhenMethodAndHost(t *testing.T) {
	// Only allow GET requests to a specific host.
	policy := `permit(principal, action, resource) when {
		context.request.method == "GET" && context.request.host == "api.example.com"
	};`

	t.Run("GET to matching host is allowed", func(t *testing.T) {
		filter, _ := createTestFilter(t, &cedarConfig{Policy: pkg.DataSource{Inline: policy}})

		headers := fake.NewFakeHeaderMap(map[string][]string{
			":method":    {"GET"},
			":path":      {"/api/resource"},
			":authority": {"api.example.com"},
			"x-user-id":  {"alice"},
		})
		require.Equal(t, shared.HeadersStatusContinue, filter.OnRequestHeaders(headers, true))
	})

	t.Run("GET to different host is denied", func(t *testing.T) {
		filter, mockHandle := createTestFilter(t, &cedarConfig{Policy: pkg.DataSource{Inline: policy}})
		mockHandle.EXPECT().SendLocalResponse(uint32(403), gomock.Any(), []byte("Forbidden"), "cedar_denied")

		headers := fake.NewFakeHeaderMap(map[string][]string{
			":method":    {"GET"},
			":path":      {"/api/resource"},
			":authority": {"other.example.com"},
			"x-user-id":  {"alice"},
		})
		require.Equal(t, shared.HeadersStatusStop, filter.OnRequestHeaders(headers, true))
	})

	t.Run("POST to matching host is denied", func(t *testing.T) {
		filter, mockHandle := createTestFilter(t, &cedarConfig{Policy: pkg.DataSource{Inline: policy}})
		mockHandle.EXPECT().SendLocalResponse(uint32(403), gomock.Any(), []byte("Forbidden"), "cedar_denied")

		headers := fake.NewFakeHeaderMap(map[string][]string{
			":method":    {"POST"},
			":path":      {"/api/resource"},
			":authority": {"api.example.com"},
			"x-user-id":  {"alice"},
		})
		require.Equal(t, shared.HeadersStatusStop, filter.OnRequestHeaders(headers, true))
	})
}

func TestOnRequestHeaders_WhenSchemeAndPath(t *testing.T) {
	// Only allow HTTPS requests and check the full path in context.
	policy := `permit(principal, action, resource) when {
		context.request.scheme == "https" && context.request.path == "/api/secure"
	};`

	t.Run("HTTPS to matching path is allowed", func(t *testing.T) {
		filter, _ := createTestFilter(t, &cedarConfig{Policy: pkg.DataSource{Inline: policy}})

		headers := fake.NewFakeHeaderMap(map[string][]string{
			":method":    {"GET"},
			":path":      {"/api/secure"},
			":authority": {"example.com"},
			":scheme":    {"https"},
			"x-user-id":  {"alice"},
		})
		require.Equal(t, shared.HeadersStatusContinue, filter.OnRequestHeaders(headers, true))
	})

	t.Run("HTTP is denied", func(t *testing.T) {
		filter, mockHandle := createTestFilter(t, &cedarConfig{Policy: pkg.DataSource{Inline: policy}})
		mockHandle.EXPECT().SendLocalResponse(uint32(403), gomock.Any(), []byte("Forbidden"), "cedar_denied")

		headers := fake.NewFakeHeaderMap(map[string][]string{
			":method":    {"GET"},
			":path":      {"/api/secure"},
			":authority": {"example.com"},
			":scheme":    {"http"},
			"x-user-id":  {"alice"},
		})
		require.Equal(t, shared.HeadersStatusStop, filter.OnRequestHeaders(headers, true))
	})
}

func TestOnRequestHeaders_Unless(t *testing.T) {
	// Allow all requests unless the method is DELETE.
	policy := `permit(principal, action, resource) unless { context.request.method == "DELETE" };`

	t.Run("GET is allowed", func(t *testing.T) {
		filter, _ := createTestFilter(t, &cedarConfig{Policy: pkg.DataSource{Inline: policy}})

		headers := fake.NewFakeHeaderMap(map[string][]string{
			":method":    {"GET"},
			":path":      {"/api/resource"},
			":authority": {"example.com"},
			"x-user-id":  {"alice"},
		})
		require.Equal(t, shared.HeadersStatusContinue, filter.OnRequestHeaders(headers, true))
	})

	t.Run("POST is allowed", func(t *testing.T) {
		filter, _ := createTestFilter(t, &cedarConfig{Policy: pkg.DataSource{Inline: policy}})

		headers := fake.NewFakeHeaderMap(map[string][]string{
			":method":    {"POST"},
			":path":      {"/api/resource"},
			":authority": {"example.com"},
			"x-user-id":  {"alice"},
		})
		require.Equal(t, shared.HeadersStatusContinue, filter.OnRequestHeaders(headers, true))
	})

	t.Run("DELETE is denied", func(t *testing.T) {
		filter, mockHandle := createTestFilter(t, &cedarConfig{Policy: pkg.DataSource{Inline: policy}})
		mockHandle.EXPECT().SendLocalResponse(uint32(403), gomock.Any(), []byte("Forbidden"), "cedar_denied")

		headers := fake.NewFakeHeaderMap(map[string][]string{
			":method":    {"DELETE"},
			":path":      {"/api/resource"},
			":authority": {"example.com"},
			"x-user-id":  {"alice"},
		})
		require.Equal(t, shared.HeadersStatusStop, filter.OnRequestHeaders(headers, true))
	})
}

func TestOnRequestHeaders_WhenParsedPathContains(t *testing.T) {
	// Allow requests whose parsed path contains the "api" segment.
	policy := `permit(principal, action, resource) when { context.parsed_path.contains("api") };`

	t.Run("path with api segment is allowed", func(t *testing.T) {
		filter, _ := createTestFilter(t, &cedarConfig{Policy: pkg.DataSource{Inline: policy}})

		headers := fake.NewFakeHeaderMap(map[string][]string{
			":method":    {"GET"},
			":path":      {"/api/users"},
			":authority": {"example.com"},
			"x-user-id":  {"alice"},
		})
		require.Equal(t, shared.HeadersStatusContinue, filter.OnRequestHeaders(headers, true))
	})

	t.Run("path without api segment is denied", func(t *testing.T) {
		filter, mockHandle := createTestFilter(t, &cedarConfig{Policy: pkg.DataSource{Inline: policy}})
		mockHandle.EXPECT().SendLocalResponse(uint32(403), gomock.Any(), []byte("Forbidden"), "cedar_denied")

		headers := fake.NewFakeHeaderMap(map[string][]string{
			":method":    {"GET"},
			":path":      {"/public/resource"},
			":authority": {"example.com"},
			"x-user-id":  {"alice"},
		})
		require.Equal(t, shared.HeadersStatusStop, filter.OnRequestHeaders(headers, true))
	})
}

func TestOnRequestHeaders_WhenCombinedPrincipalAndContext(t *testing.T) {
	// Only admin users can write; anyone can read.
	// Note: Cedar identifiers cannot contain hyphens, so header names with
	// hyphens must use the bracket syntax: context.request.headers["x-admin-token"].
	policy := `
permit(principal, action == Action::"GET", resource);
permit(principal == User::"admin", action, resource) when {
	context.request.headers has "x-admin-token" && context.request.headers["x-admin-token"] == "secret"
};
`
	t.Run("GET by any user is allowed", func(t *testing.T) {
		filter, _ := createTestFilter(t, &cedarConfig{Policy: pkg.DataSource{Inline: policy}})

		headers := fake.NewFakeHeaderMap(map[string][]string{
			":method":    {"GET"},
			":path":      {"/api/resource"},
			":authority": {"example.com"},
			"x-user-id":  {"bob"},
		})
		require.Equal(t, shared.HeadersStatusContinue, filter.OnRequestHeaders(headers, true))
	})

	t.Run("POST by admin with valid token is allowed", func(t *testing.T) {
		filter, _ := createTestFilter(t, &cedarConfig{Policy: pkg.DataSource{Inline: policy}})

		headers := fake.NewFakeHeaderMap(map[string][]string{
			":method":       {"POST"},
			":path":         {"/api/resource"},
			":authority":    {"example.com"},
			"x-user-id":     {"admin"},
			"x-admin-token": {"secret"},
		})
		require.Equal(t, shared.HeadersStatusContinue, filter.OnRequestHeaders(headers, true))
	})

	t.Run("POST by admin without token is denied", func(t *testing.T) {
		filter, mockHandle := createTestFilter(t, &cedarConfig{Policy: pkg.DataSource{Inline: policy}})
		mockHandle.EXPECT().SendLocalResponse(uint32(403), gomock.Any(), []byte("Forbidden"), "cedar_denied")

		headers := fake.NewFakeHeaderMap(map[string][]string{
			":method":    {"POST"},
			":path":      {"/api/resource"},
			":authority": {"example.com"},
			"x-user-id":  {"admin"},
		})
		require.Equal(t, shared.HeadersStatusStop, filter.OnRequestHeaders(headers, true))
	})

	t.Run("POST by non-admin is denied", func(t *testing.T) {
		filter, mockHandle := createTestFilter(t, &cedarConfig{Policy: pkg.DataSource{Inline: policy}})
		mockHandle.EXPECT().SendLocalResponse(uint32(403), gomock.Any(), []byte("Forbidden"), "cedar_denied")

		headers := fake.NewFakeHeaderMap(map[string][]string{
			":method":       {"POST"},
			":path":         {"/api/resource"},
			":authority":    {"example.com"},
			"x-user-id":     {"bob"},
			"x-admin-token": {"secret"},
		})
		require.Equal(t, shared.HeadersStatusStop, filter.OnRequestHeaders(headers, true))
	})
}

func TestOnRequestHeaders_PolicyWithEntities(t *testing.T) {
	policy := `permit(principal in Group::"admins", action, resource);`
	policyFile := createTestPolicyFile(t, policy)

	entities := `[
		{"uid": {"type": "User", "id": "alice"}, "parents": [{"type": "Group", "id": "admins"}], "attrs": {}},
		{"uid": {"type": "Group", "id": "admins"}, "parents": [], "attrs": {}}
	]`
	entitiesFile := createTestEntitiesFile(t, entities)

	t.Run("member of group is allowed", func(t *testing.T) {
		filter, _ := createTestFilter(t, &cedarConfig{
			Policy:       pkg.DataSource{File: policyFile},
			EntitiesFile: entitiesFile,
		})

		headers := fake.NewFakeHeaderMap(map[string][]string{
			":method":    {"GET"},
			":path":      {"/api/resource"},
			":authority": {"example.com"},
			"x-user-id":  {"alice"},
		})
		require.Equal(t, shared.HeadersStatusContinue, filter.OnRequestHeaders(headers, true))
	})

	t.Run("non-member is denied", func(t *testing.T) {
		filter, mockHandle := createTestFilter(t, &cedarConfig{
			Policy:       pkg.DataSource{File: policyFile},
			EntitiesFile: entitiesFile,
		})
		mockHandle.EXPECT().SendLocalResponse(uint32(403), gomock.Any(), []byte("Forbidden"), "cedar_denied")

		headers := fake.NewFakeHeaderMap(map[string][]string{
			":method":    {"GET"},
			":path":      {"/api/resource"},
			":authority": {"example.com"},
			"x-user-id":  {"bob"},
		})
		require.Equal(t, shared.HeadersStatusStop, filter.OnRequestHeaders(headers, true))
	})
}

func TestOnRequestHeaders_MultiplePolicies(t *testing.T) {
	// Permit GET, explicitly forbid DELETE, implicit deny for everything else.
	policy := `
permit(principal, action == Action::"GET", resource);
forbid(principal, action == Action::"DELETE", resource);
`

	t.Run("GET is allowed", func(t *testing.T) {
		filter, _ := createTestFilter(t, &cedarConfig{Policy: pkg.DataSource{Inline: policy}})

		headers := fake.NewFakeHeaderMap(map[string][]string{
			":method":    {"GET"},
			":path":      {"/api/resource"},
			":authority": {"example.com"},
			"x-user-id":  {"alice"},
		})
		require.Equal(t, shared.HeadersStatusContinue, filter.OnRequestHeaders(headers, true))
	})

	t.Run("DELETE is denied", func(t *testing.T) {
		filter, mockHandle := createTestFilter(t, &cedarConfig{Policy: pkg.DataSource{Inline: policy}})
		mockHandle.EXPECT().SendLocalResponse(uint32(403), gomock.Any(), []byte("Forbidden"), "cedar_denied")

		headers := fake.NewFakeHeaderMap(map[string][]string{
			":method":    {"DELETE"},
			":path":      {"/api/resource"},
			":authority": {"example.com"},
			"x-user-id":  {"alice"},
		})
		require.Equal(t, shared.HeadersStatusStop, filter.OnRequestHeaders(headers, true))
	})

	t.Run("POST is denied (no matching permit)", func(t *testing.T) {
		filter, mockHandle := createTestFilter(t, &cedarConfig{Policy: pkg.DataSource{Inline: policy}})
		mockHandle.EXPECT().SendLocalResponse(uint32(403), gomock.Any(), []byte("Forbidden"), "cedar_denied")

		headers := fake.NewFakeHeaderMap(map[string][]string{
			":method":    {"POST"},
			":path":      {"/api/resource"},
			":authority": {"example.com"},
			"x-user-id":  {"alice"},
		})
		require.Equal(t, shared.HeadersStatusStop, filter.OnRequestHeaders(headers, true))
	})
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

// Tests for buildContext

func TestBuildContext_BasicRequest(t *testing.T) {
	policy := `permit(principal, action, resource);`
	filter, _ := createTestFilter(t, &cedarConfig{Policy: pkg.DataSource{Inline: policy}})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":       {"POST"},
		":path":         {"/api/users?role=admin"},
		":authority":    {"example.com"},
		":scheme":       {"https"},
		"x-user-id":     {"alice"},
		"authorization": {"Bearer token123"},
		"content-type":  {"application/json"},
	})

	req, err := filter.buildRequest(headers)
	require.NoError(t, err)

	// The context should be a record - verify it's non-empty by checking the request was built.
	require.NotEmpty(t, req.Context)
}

func TestBuildContext_MTLSAttributes(t *testing.T) {
	policy := `permit(principal, action, resource);`
	policyFile := createTestPolicyFile(t, policy)
	cfg := cedarConfig{
		Policy:            pkg.DataSource{File: policyFile},
		PrincipalType:     "User",
		PrincipalIDHeader: "x-user-id",
	}
	configJSON, err := json.Marshal(cfg)
	require.NoError(t, err)

	factory := &CedarHttpFilterConfigFactory{}
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockConfigHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockConfigHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockConfigHandle.EXPECT().DefineCounter("cedar_requests_total", "decision").Return(shared.MetricID(1), shared.MetricsSuccess)

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

	filter := filterFactory.Create(mockHandle).(*cedarHttpFilter)

	// Use a policy that checks the SPIFFE URI SAN in the context.
	// This verifies that mTLS attributes are properly passed through.
	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/api/resource"},
		":authority": {"example.com"},
		":scheme":    {"https"},
		"x-user-id":  {"alice"},
	})

	req, err := filter.buildRequest(headers)
	require.NoError(t, err)
	require.NotEmpty(t, req.Context)
}

func TestOnRequestHeaders_PolicyUsesSPIFFE(t *testing.T) {
	policy := `permit(principal, action, resource) when { context.source.certificate.uri_san == "spiffe://cluster.local/ns/default/sa/trusted" };`
	policyFile := createTestPolicyFile(t, policy)
	cfg := cedarConfig{
		Policy:            pkg.DataSource{File: policyFile},
		PrincipalType:     "User",
		PrincipalIDHeader: "x-user-id",
	}
	configJSON, err := json.Marshal(cfg)
	require.NoError(t, err)

	factory := &CedarHttpFilterConfigFactory{}
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockConfigHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockConfigHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockConfigHandle.EXPECT().DefineCounter("cedar_requests_total", "decision").Return(shared.MetricID(1), shared.MetricsSuccess)

	filterFactory, err := factory.Create(mockConfigHandle, configJSON)
	require.NoError(t, err)

	t.Run("trusted SPIFFE identity is allowed", func(t *testing.T) {
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

		filter := filterFactory.Create(mockHandle).(*cedarHttpFilter)

		headers := fake.NewFakeHeaderMap(map[string][]string{
			":method":    {"GET"},
			":path":      {"/api/resource"},
			":authority": {"example.com"},
			":scheme":    {"https"},
			"x-user-id":  {"alice"},
		})

		status := filter.OnRequestHeaders(headers, true)
		require.Equal(t, shared.HeadersStatusContinue, status)
	})

	t.Run("untrusted SPIFFE identity is denied", func(t *testing.T) {
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
		mockHandle.EXPECT().SendLocalResponse(uint32(403), gomock.Any(), []byte("Forbidden"), "cedar_denied")
		mockHandle.EXPECT().IncrementCounterValue(shared.MetricID(1), uint64(1), "denied").Return(shared.MetricsSuccess)

		filter := filterFactory.Create(mockHandle).(*cedarHttpFilter)

		headers := fake.NewFakeHeaderMap(map[string][]string{
			":method":    {"GET"},
			":path":      {"/api/resource"},
			":authority": {"example.com"},
			":scheme":    {"https"},
			"x-user-id":  {"alice"},
		})

		status := filter.OnRequestHeaders(headers, true)
		require.Equal(t, shared.HeadersStatusStop, status)
	})
}

// Tests for buildRequest

func TestBuildRequest_BasicRequest(t *testing.T) {
	policy := `permit(principal, action, resource);`
	filter, _ := createTestFilter(t, &cedarConfig{Policy: pkg.DataSource{Inline: policy}})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/api/users"},
		":authority": {"example.com"},
		":scheme":    {"https"},
		"x-user-id":  {"alice"},
	})

	req, err := filter.buildRequest(headers)
	require.NoError(t, err)

	require.Equal(t, "User", string(req.Principal.Type))
	require.Equal(t, "alice", string(req.Principal.ID))
	require.Equal(t, "Action", string(req.Action.Type))
	require.Equal(t, "GET", string(req.Action.ID))
	require.Equal(t, "Resource", string(req.Resource.Type))
	require.Equal(t, "/api/users", string(req.Resource.ID))
}

func TestBuildRequest_MissingPrincipalHeader(t *testing.T) {
	policy := `permit(principal, action, resource);`
	filter, _ := createTestFilter(t, &cedarConfig{Policy: pkg.DataSource{Inline: policy}})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/api/users"},
		":authority": {"example.com"},
	})

	_, err := filter.buildRequest(headers)
	require.Error(t, err)
	require.Contains(t, err.Error(), "principal header")
}

func TestBuildRequest_PathWithQueryString(t *testing.T) {
	policy := `permit(principal, action, resource);`
	filter, _ := createTestFilter(t, &cedarConfig{Policy: pkg.DataSource{Inline: policy}})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/api/users?role=admin"},
		":authority": {"example.com"},
		"x-user-id":  {"alice"},
	})

	req, err := filter.buildRequest(headers)
	require.NoError(t, err)

	// Resource should be the path without query string.
	require.Equal(t, "/api/users", string(req.Resource.ID))
}

func TestBuildRequest_CustomEntityTypes(t *testing.T) {
	policy := `permit(principal, action, resource);`
	filter, _ := createTestFilter(t, &cedarConfig{
		Policy:       pkg.DataSource{File: createTestPolicyFile(t, policy)},
		ActionType:   "HttpMethod",
		ResourceType: "Endpoint",
	})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"POST"},
		":path":      {"/api/users"},
		":authority": {"example.com"},
		"x-user-id":  {"alice"},
	})

	req, err := filter.buildRequest(headers)
	require.NoError(t, err)

	require.Equal(t, "HttpMethod", string(req.Action.Type))
	require.Equal(t, "Endpoint", string(req.Resource.Type))
}

// Tests for WellKnownHttpFilterConfigFactories

func TestWellKnownHttpFilterConfigFactories(t *testing.T) {
	factories := WellKnownHttpFilterConfigFactories()

	require.NotNil(t, factories)
	require.Len(t, factories, 1)
	require.Contains(t, factories, "cedar-auth")

	factory, ok := factories["cedar-auth"].(*CedarHttpFilterConfigFactory)
	require.True(t, ok)
	require.NotNil(t, factory)
}

// Tests for cedarHttpFilterFactory.Create

func TestFilterFactory_Create(t *testing.T) {
	policy := `permit(principal, action, resource);`
	policyFile := createTestPolicyFile(t, policy)
	cfg := cedarConfig{
		Policy:            pkg.DataSource{File: policyFile},
		PrincipalType:     "User",
		PrincipalIDHeader: "x-user-id",
	}
	configJSON, err := json.Marshal(cfg)
	require.NoError(t, err)

	factory := &CedarHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockConfigHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockConfigHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockConfigHandle.EXPECT().DefineCounter("cedar_requests_total", "decision").Return(shared.MetricID(1), shared.MetricsSuccess)

	filterFactory, err := factory.Create(mockConfigHandle, configJSON)
	require.NoError(t, err)

	mockHandle := mocks.NewMockHttpFilterHandle(ctrl)
	filter := filterFactory.Create(mockHandle)

	require.NotNil(t, filter)
	cedarFilter, ok := filter.(*cedarHttpFilter)
	require.True(t, ok)
	require.Equal(t, mockHandle, cedarFilter.handle)
}

// Tests for dynamicMetadataMap / buildContext dynamic metadata

func TestBuildContext_DynamicMetadata_MultipleNamespacesAndKeys(t *testing.T) {
	policy := `permit(principal, action, resource);`
	policyFile := createTestPolicyFile(t, policy)
	cfg := &cedarConfig{
		Policy:             pkg.DataSource{File: policyFile},
		PrincipalType:      "User",
		PrincipalIDHeader:  "x-user-id",
		MetadataNamespaces: []string{"ns.auth", "ns.ratelimit"},
	}
	configJSON, err := json.Marshal(cfg)
	require.NoError(t, err)

	factory := &CedarHttpFilterConfigFactory{}
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockConfigHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockConfigHandle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockConfigHandle.EXPECT().DefineCounter("cedar_requests_total", "decision").Return(shared.MetricID(1), shared.MetricsSuccess)

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

	// Namespace "ns.ratelimit" has a string key.
	mockHandle.EXPECT().GetMetadataKeys(shared.MetadataSourceTypeDynamic, "ns.ratelimit").Return([]shared.UnsafeEnvoyBuffer{
		pkg.UnsafeBufferFromString("bucket"),
	})
	mockHandle.EXPECT().GetMetadataString(shared.MetadataSourceTypeDynamic, "ns.ratelimit", "bucket").Return(pkg.UnsafeBufferFromString("default"), true)

	filter := filterFactory.Create(mockHandle).(*cedarHttpFilter)

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/api/resource"},
		":authority": {"example.com"},
	})

	ctx := filter.buildContext(headers)

	// Extract the dynamic_metadata record from context.
	dmVal, ok := ctx.Get("dynamic_metadata")
	require.True(t, ok, "dynamic_metadata should be present in context")

	// Verify ns.auth namespace.
	dmRecord, ok := dmVal.(cedarlib.Record)
	require.True(t, ok)
	authNs, ok := dmRecord.Get("ns.auth")
	require.True(t, ok, "ns.auth namespace should be present")
	authRecord, ok := authNs.(cedarlib.Record)
	require.True(t, ok)
	identity, ok := authRecord.Get("identity")
	require.True(t, ok)
	require.Equal(t, cedarlib.String("user-123"), identity)
	role, ok := authRecord.Get("role")
	require.True(t, ok)
	require.Equal(t, cedarlib.String("admin"), role)

	// Verify ns.ratelimit namespace.
	rlNs, ok := dmRecord.Get("ns.ratelimit")
	require.True(t, ok, "ns.ratelimit namespace should be present")
	rlRecord, ok := rlNs.(cedarlib.Record)
	require.True(t, ok)
	bucket, ok := rlRecord.Get("bucket")
	require.True(t, ok)
	require.Equal(t, cedarlib.String("default"), bucket)
}

func TestBuildContext_DynamicMetadata_Empty(t *testing.T) {
	policy := `permit(principal, action, resource);`
	filter, _ := createTestFilter(t, &cedarConfig{Policy: pkg.DataSource{Inline: policy}})

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":method":    {"GET"},
		":path":      {"/"},
		":authority": {"example.com"},
	})

	ctx := filter.buildContext(headers)

	// When no metadata namespaces are configured, dynamic_metadata should be an empty record.
	dmVal, ok := ctx.Get("dynamic_metadata")
	require.True(t, ok, "dynamic_metadata should be present in context")
	_, ok = dmVal.(cedarlib.Record)
	require.True(t, ok, "dynamic_metadata should be a Record")
}
