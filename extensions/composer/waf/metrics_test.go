// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package waf

import (
	"testing"
	"time"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/mocks"
	"go.uber.org/mock/gomock"
)

// Typical CRS evaluation takes single-digit milliseconds, so the recorded unit
// must resolve below that scale: whole milliseconds truncate most transactions
// to 0 or 1 and lose the signal entirely.
func Test_RecordTx_RecordsMicroseconds(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	configHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	configHandle.EXPECT().DefineCounter("waf_tx_total").Return(shared.MetricID(1), shared.MetricsSuccess)
	configHandle.EXPECT().DefineCounter("waf_tx_blocked", "authority", "phase", "rule_id").Return(shared.MetricID(2), shared.MetricsSuccess)
	configHandle.EXPECT().DefineHistogram("waf_tx_duration").Return(shared.MetricID(3), shared.MetricsSuccess)
	m := newMetrics(configHandle)

	pluginHandle := mocks.NewMockHttpFilterHandle(ctrl)
	pluginHandle.EXPECT().IncrementCounterValue(shared.MetricID(1), uint64(1)).Return(shared.MetricsSuccess).Times(2)

	// A sub-millisecond transaction must not record as 0.
	pluginHandle.EXPECT().RecordHistogramValue(shared.MetricID(3), uint64(750)).Return(shared.MetricsSuccess)
	m.RecordTx(pluginHandle, 750*time.Microsecond)

	// Sub-microsecond remainders truncate; the millisecond boundary does not.
	pluginHandle.EXPECT().RecordHistogramValue(shared.MetricID(3), uint64(1500)).Return(shared.MetricsSuccess)
	m.RecordTx(pluginHandle, 1500*time.Microsecond+400*time.Nanosecond)
}
