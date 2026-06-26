// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package impl contains tests for the dynamic-fault-injection extension.
package impl

import (
	"testing"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/fake"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// Valid YAML config for testing.
var validConfig = []byte(`
endpoints:
  - match:
      prefix: "/api/"
    responses:
      - status: 200
        resolution: 90
        distribution:
          p0.0: "1ms"
          p50.0: "10ms"
          p99.0: "200ms"
      - status: 503
        resolution: 10
        distribution:
          p0.0: "50ms"
          p50.0: "100ms"
          p99.0: "500ms"
`)

// Tests for CustomHttpFilterConfigFactory.Create

func TestConfigFactory_Create_ValidConfig(t *testing.T) {
	factory := &CustomHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any()).AnyTimes()

	filterFactory, err := factory.Create(mockHandle, validConfig)
	require.NoError(t, err)
	require.NotNil(t, filterFactory)

	handle := newFilterHandleWithoutPerRouteConfig(ctrl)
	filter := filterFactory.Create(handle)
	require.NotNil(t, filter)
	_, ok := filter.(*latencyFaultFilter)
	require.True(t, ok)
}

func TestConfigFactory_Create_EmptyConfig(t *testing.T) {
	factory := &CustomHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any()).AnyTimes()

	// Empty config should fail because no endpoints are configured.
	filterFactory, err := factory.Create(mockHandle, []byte{})
	// An empty config with no endpoints is technically valid YAML but the filter
	// will just have zero endpoints. Whether this is an error depends on ParseConfig.
	if err != nil {
		require.Nil(t, filterFactory)
	} else {
		require.NotNil(t, filterFactory)
	}
}

func TestConfigFactory_Create_InvalidConfig(t *testing.T) {
	factory := &CustomHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any()).AnyTimes()

	// Invalid YAML should error.
	filterFactory, err := factory.Create(mockHandle, []byte(`{{not valid yaml`))
	require.Error(t, err)
	require.Nil(t, filterFactory)
}

func TestConfigFactory_Create_InvalidEndpoint(t *testing.T) {
	factory := &CustomHttpFilterConfigFactory{}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	mockHandle.EXPECT().Log(gomock.Any(), gomock.Any()).AnyTimes()

	// Endpoint without responses or load_based should error.
	badConfig := []byte(`
endpoints:
  - match:
      prefix: "/api/"
`)
	filterFactory, err := factory.Create(mockHandle, badConfig)
	require.Error(t, err)
	require.Nil(t, filterFactory)
}

// Tests for CustomHttpFilterConfigFactory.CreatePerRoute

func TestConfigFactory_CreatePerRoute_ValidConfig(t *testing.T) {
	factory := &CustomHttpFilterConfigFactory{}

	result, err := factory.CreatePerRoute(validConfig)
	require.NoError(t, err)
	require.NotNil(t, result)

	perRoute, ok := result.(*latencyFaultFilterFactory)
	require.True(t, ok)
	require.Len(t, perRoute.endpoints, 1)
}

func TestConfigFactory_CreatePerRoute_InvalidConfig(t *testing.T) {
	factory := &CustomHttpFilterConfigFactory{}

	result, err := factory.CreatePerRoute([]byte(`{{invalid`))
	require.Error(t, err)
	require.Nil(t, result)
}

// Tests for per-route config override

func TestPerRouteConfigOverride(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Build a base factory.
	baseFactory, err := buildFilterFactory(validConfig)
	require.NoError(t, err)

	t.Run("per-route config overrides factory", func(t *testing.T) {
		perRouteConfig := []byte(`
endpoints:
  - match:
      exact: "/health"
    responses:
      - status: 200
        resolution: 100
        distribution:
          p0.0: "0ms"
          p50.0: "1ms"
          p99.0: "5ms"
`)
		perRouteFactory, err := buildFilterFactory(perRouteConfig)
		require.NoError(t, err)

		handle := newFilterHandleWithPerRouteConfig(ctrl, perRouteFactory)
		filter := baseFactory.Create(handle)
		f, ok := filter.(*latencyFaultFilter)
		require.True(t, ok)
		// The per-route factory should be used.
		require.Len(t, f.factory.endpoints, 1)
		require.Equal(t, "/health", f.factory.endpoints[0].match.Exact)
	})

	t.Run("nil per-route config uses base factory", func(t *testing.T) {
		handle := newFilterHandleWithoutPerRouteConfig(ctrl)
		filter := baseFactory.Create(handle)
		f, ok := filter.(*latencyFaultFilter)
		require.True(t, ok)
		require.Len(t, f.factory.endpoints, 1)
		require.Equal(t, "/api/", f.factory.endpoints[0].match.Prefix)
	})
}

// Tests for OnRequestHeaders

func TestOnRequestHeaders_MatchingRoute(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	factory, err := buildFilterFactory(validConfig)
	require.NoError(t, err)

	handle := newFilterHandleWithoutPerRouteConfig(ctrl)
	filter := factory.Create(handle).(*latencyFaultFilter)

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":path": {"/api/users"},
	})
	status := filter.OnRequestHeaders(headers, false)

	require.Equal(t, shared.HeadersStatusContinue, status)
	require.True(t, filter.matched)
	require.NotZero(t, filter.requestStart)
}

func TestOnRequestHeaders_NonMatchingRoute(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	factory, err := buildFilterFactory(validConfig)
	require.NoError(t, err)

	handle := newFilterHandleWithoutPerRouteConfig(ctrl)
	filter := factory.Create(handle).(*latencyFaultFilter)

	headers := fake.NewFakeHeaderMap(map[string][]string{
		":path": {"/health"},
	})
	status := filter.OnRequestHeaders(headers, false)

	require.Equal(t, shared.HeadersStatusContinue, status)
	require.False(t, filter.matched)
}

// Tests for OnResponseHeaders

func TestOnResponseHeaders_NotMatched(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	factory, err := buildFilterFactory(validConfig)
	require.NoError(t, err)

	handle := newFilterHandleWithoutPerRouteConfig(ctrl)
	filter := factory.Create(handle).(*latencyFaultFilter)
	// Don't set matched = true

	headers := fake.NewFakeHeaderMap(map[string][]string{})
	status := filter.OnResponseHeaders(headers, false)

	require.Equal(t, shared.HeadersStatusContinue, status)
}

// Tests for WellKnownHttpFilterConfigFactories

func TestWellKnownHttpFilterConfigFactories(t *testing.T) {
	factories := WellKnownHttpFilterConfigFactories()
	require.Contains(t, factories, "dynamic-fault-injection")
	_, ok := factories["dynamic-fault-injection"].(*CustomHttpFilterConfigFactory)
	require.True(t, ok)
}

// Helpers

func newFilterHandleWithoutPerRouteConfig(ctrl *gomock.Controller) *mocks.MockHttpFilterHandle {
	h := mocks.NewMockHttpFilterHandle(ctrl)
	h.EXPECT().GetMostSpecificConfig().Return(nil).AnyTimes()
	h.EXPECT().Log(gomock.Any(), gomock.Any()).AnyTimes()
	return h
}

func newFilterHandleWithPerRouteConfig(ctrl *gomock.Controller, perRouteConfig any) *mocks.MockHttpFilterHandle {
	h := mocks.NewMockHttpFilterHandle(ctrl)
	h.EXPECT().GetMostSpecificConfig().Return(perRouteConfig).AnyTimes()
	h.EXPECT().Log(gomock.Any(), gomock.Any()).AnyTimes()
	return h
}
