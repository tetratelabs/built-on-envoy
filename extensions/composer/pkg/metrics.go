// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package pkg

import (
	"cmp"
	"fmt"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
)

// reasons maps metrics results to human-readable strings for logging purposes.
var reasons = map[shared.MetricsResult]string{
	shared.MetricsFrozen:      "frozen",
	shared.MetricsNotFound:    "not_found",
	shared.MetricsInvalidTags: "invalid_tags",
}

// Metric represents a single metric with its ID and whether it was successfully defined.
type Metric struct {
	id      shared.MetricID
	enabled bool
}

// NewMetric is a helper function to define a new metric and log any errors that occur during its definition.
func NewMetric(
	handle shared.HttpFilterConfigHandle,
	defineFunc func(string, ...string) (shared.MetricID, shared.MetricsResult),
	name string,
	tagNames ...string,
) *Metric {
	metric, result := defineFunc(name, tagNames...)
	if result != shared.MetricsSuccess {
		reason := cmp.Or(reasons[result], "unknown_error")
		handle.Log(shared.LogLevelError, fmt.Sprintf("Failed to define metric %q: %s", name, reason))
	}
	return &Metric{
		id:      metric,
		enabled: result == shared.MetricsSuccess,
	}
}

// Record is a helper function to record a metric value taking into account that
// the metric could not be defined successfully. If the metric is not initialized
// (i.e., has an ID of 0), it logs a trace message and skips recording.
func (m *Metric) Record(
	handle shared.HttpFilterHandle,
	recordFunc func(shared.MetricID, uint64, ...string) shared.MetricsResult,
	value uint64,
	tags ...string,
) {
	if !m.enabled { // Metric not initialized, skip recording
		handle.Log(shared.LogLevelTrace, "Metric not initialized, skipping recording")
		return
	}
	if r := recordFunc(m.id, value, tags...); r != shared.MetricsSuccess {
		reason := cmp.Or(reasons[r], "unknown_error")
		handle.Log(shared.LogLevelError, fmt.Sprintf("Failed to record metric: %s", reason))
	}
}
