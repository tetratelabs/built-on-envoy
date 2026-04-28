// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package pkg

import (
	"testing"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/mocks"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

func TestNewMetric_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	configHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	configHandle.EXPECT().DefineCounter("test_metric", "tag1").Return(shared.MetricID(42), shared.MetricsSuccess)

	m := NewMetric(configHandle, configHandle.DefineCounter, "test_metric", "tag1")

	assert.True(t, m.enabled, "expected metric to be enabled on success")
	assert.Equal(t, shared.MetricID(42), m.id, "expected metric ID to match")
}

func TestNewMetric_Frozen(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	configHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	configHandle.EXPECT().DefineCounter("test_metric").Return(shared.MetricID(0), shared.MetricsFrozen)
	configHandle.EXPECT().Log(shared.LogLevelError, `Failed to define metric "test_metric": frozen`)

	m := NewMetric(configHandle, configHandle.DefineCounter, "test_metric")

	assert.False(t, m.enabled, "expected metric to be disabled on frozen error")
}

func TestNewMetric_NotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	configHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	configHandle.EXPECT().DefineCounter("test_metric").Return(shared.MetricID(0), shared.MetricsNotFound)
	configHandle.EXPECT().Log(shared.LogLevelError, `Failed to define metric "test_metric": not_found`)

	m := NewMetric(configHandle, configHandle.DefineCounter, "test_metric")

	assert.False(t, m.enabled, "expected metric to be disabled on not found error")
}

func TestNewMetric_InvalidTags(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	configHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	configHandle.EXPECT().DefineCounter("test_metric", "t1", "t2").Return(shared.MetricID(0), shared.MetricsInvalidTags)
	configHandle.EXPECT().Log(shared.LogLevelError, `Failed to define metric "test_metric": invalid_tags`)

	m := NewMetric(configHandle, configHandle.DefineCounter, "test_metric", "t1", "t2")

	assert.False(t, m.enabled, "expected metric to be disabled on invalid tags error")
}

func TestNewMetric_UnknownError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	configHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	configHandle.EXPECT().DefineCounter("test_metric").Return(shared.MetricID(0), shared.MetricsResult(99))
	configHandle.EXPECT().Log(shared.LogLevelError, `Failed to define metric "test_metric": unknown_error`)

	m := NewMetric(configHandle, configHandle.DefineCounter, "test_metric")

	assert.False(t, m.enabled, "expected metric to be disabled on unknown error")
}

func TestRecord_DisabledMetricSkipsRecording(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	filterHandle := mocks.NewMockHttpFilterHandle(ctrl)
	filterHandle.EXPECT().Log(shared.LogLevelTrace, "Metric not initialized, skipping recording")

	m := &Metric{id: 0, enabled: false}

	// IncrementCounterValue should NOT be called.
	m.Record(filterHandle, filterHandle.IncrementCounterValue, 1)
}

func TestRecord_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	filterHandle := mocks.NewMockHttpFilterHandle(ctrl)
	filterHandle.EXPECT().IncrementCounterValue(shared.MetricID(5), uint64(1), "v1", "v2").Return(shared.MetricsSuccess)

	m := &Metric{id: shared.MetricID(5), enabled: true}

	m.Record(filterHandle, filterHandle.IncrementCounterValue, 1, "v1", "v2")
}

func TestRecord_SuccessNoTags(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	filterHandle := mocks.NewMockHttpFilterHandle(ctrl)
	filterHandle.EXPECT().IncrementCounterValue(shared.MetricID(3), uint64(10)).Return(shared.MetricsSuccess)

	m := &Metric{id: shared.MetricID(3), enabled: true}

	m.Record(filterHandle, filterHandle.IncrementCounterValue, 10)
}

func TestRecord_FailureLogs(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	filterHandle := mocks.NewMockHttpFilterHandle(ctrl)
	filterHandle.EXPECT().IncrementCounterValue(shared.MetricID(7), uint64(1)).Return(shared.MetricsNotFound)
	filterHandle.EXPECT().Log(shared.LogLevelError, "Failed to record metric: not_found")

	m := &Metric{id: shared.MetricID(7), enabled: true}

	m.Record(filterHandle, filterHandle.IncrementCounterValue, 1)
}

func TestRecord_FailureUnknownErrorLogs(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	filterHandle := mocks.NewMockHttpFilterHandle(ctrl)
	filterHandle.EXPECT().IncrementCounterValue(shared.MetricID(7), uint64(1)).Return(shared.MetricsResult(99))
	filterHandle.EXPECT().Log(shared.LogLevelError, "Failed to record metric: unknown_error")

	m := &Metric{id: shared.MetricID(7), enabled: true}

	m.Record(filterHandle, filterHandle.IncrementCounterValue, 1)
}
