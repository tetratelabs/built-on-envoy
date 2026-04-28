// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package waf

import (
	"strconv"

	ctypes "github.com/corazawaf/coraza/v3/types"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/pkg"
)

// metrics contains the metrics definitions for the WAF extension. These metrics are
type metrics struct {
	txTotal   *pkg.Metric
	txBlocked *pkg.Metric
}

// newMetrics initializes and registers the metrics for the WAF extension.
func newMetrics(h shared.HttpFilterConfigHandle) *metrics {
	return &metrics{
		txTotal:   pkg.NewMetric(h, h.DefineCounter, "waf_tx_total"),
		txBlocked: pkg.NewMetric(h, h.DefineCounter, "waf_tx_blocked", "authority", "phase", "rule_id"),
	}
}

// RecordTx increments the total transaction counter.
// This should be called for every transaction processed by the WAF.
func (m *metrics) RecordTx(h shared.HttpFilterHandle) {
	m.txTotal.Record(h, h.IncrementCounterValue, 1)
}

// RecordBlockedByRule increments the blocked transaction counter with the appropriate labels.
// This should be called for every transaction that is blocked by the WAF, and it includes
// labels for the phase and rule ID that caused the block.
func (m *metrics) RecordBlockedByRule(h shared.HttpFilterHandle, authority string, phase ctypes.RulePhase, ruleID int) {
	m.txBlocked.Record(h, h.IncrementCounterValue, 1,
		authority,
		strconv.Itoa(int(phase)),
		strconv.Itoa(ruleID),
	)
}

// RecordBlockInternal increments the blocked transaction counter with the appropriate labels.
// This should be called for every transaction that is blocked by the WAF but not for a concrete
// rule execution.
func (m *metrics) RecordBlockInternal(h shared.HttpFilterHandle, authority string, phase ctypes.RulePhase) {
	m.txBlocked.Record(h, h.IncrementCounterValue, 1,
		authority,
		strconv.Itoa(int(phase)),
		"",
	)
}
