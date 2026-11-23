// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package waf

import (
	"strconv"
	"time"

	ctypes "github.com/corazawaf/coraza/v3/types"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/pkg"
)

// metrics contains the metrics definitions for the WAF extension. These metrics are
type metrics struct {
	// authority decides the authority tag of the metrics tagged with it.
	authority authorityLabel
	// ruleMatched counts the rules matched by each transaction. It is nil unless enabled.
	ruleMatched *pkg.Metric

	txTotal    *pkg.Metric
	txBlocked  *pkg.Metric
	txDuration *pkg.Metric
	txActive   *pkg.Metric
}

// newMetrics initializes and registers the metrics for the WAF extension.
func newMetrics(h shared.HttpFilterConfigHandle, cfg metricsConfig) *metrics {
	a := cfg.authority
	return &metrics{
		authority:   a,
		ruleMatched: newRuleMatchedMetric(h, cfg),

		txTotal:    pkg.NewMetric(h, h.DefineCounter, "waf_tx_total"),
		txBlocked:  pkg.NewMetric(h, h.DefineCounter, "waf_tx_blocked", a.tagNames("phase", "rule_id")...),
		txDuration: pkg.NewMetric(h, h.DefineHistogram, "waf_tx_duration"),
		txActive:   pkg.NewMetric(h, h.DefineGauge, "waf_tx_active"),
	}
}

// RecordTx increments the total transaction counter and records the accumulated
// WAF processing duration, in microseconds. Typical CRS evaluation takes a few
// milliseconds at most, so whole milliseconds would truncate most transactions
// to 0 or 1; microseconds resolve that range within Envoy's default histogram
// buckets.
// This should be called once for every transaction processed by the WAF.
func (m *metrics) RecordTx(h shared.HttpFilterHandle, elapsed time.Duration) {
	m.txTotal.Record(h, h.IncrementCounterValue, 1)
	m.txDuration.Record(h, h.RecordHistogramValue, uint64(elapsed.Microseconds())) //nolint:gosec // G115: elapsed is a non-negative duration
}

// RecordTxCreated increments the gauge of transactions currently held by the WAF.
// This should be called once for every transaction created.
func (m *metrics) RecordTxCreated(h shared.HttpFilterHandle) {
	m.txActive.Record(h, h.IncrementGaugeValue, 1)
}

// RecordTxReleased decrements the gauge of transactions currently held by the WAF.
// This should be called once for every transaction closed and returned to the pool.
func (m *metrics) RecordTxReleased(h shared.HttpFilterHandle) {
	m.txActive.Record(h, h.DecrementGaugeValue, 1)
}

// RecordBlockedByRule increments the blocked transaction counter with the appropriate labels.
// This should be called for every transaction that is blocked by the WAF, and it includes
// labels for the phase and rule ID that caused the block.
func (m *metrics) RecordBlockedByRule(h shared.HttpFilterHandle, authority string, phase ctypes.RulePhase, ruleID int) {
	m.txBlocked.Record(h, h.IncrementCounterValue, 1,
		m.authority.tags(h, authority,
			strconv.Itoa(int(phase)),
			strconv.Itoa(ruleID),
		)...,
	)
}

// RecordBlockInternal increments the blocked transaction counter with the appropriate labels.
// This should be called for every transaction that is blocked by the WAF but not for a concrete
// rule execution.
func (m *metrics) RecordBlockInternal(h shared.HttpFilterHandle, authority string, phase ctypes.RulePhase) {
	m.txBlocked.Record(h, h.IncrementCounterValue, 1,
		m.authority.tags(h, authority,
			strconv.Itoa(int(phase)),
			"",
		)...,
	)
}

// newRuleMatchedMetric defines the waf_rule_matched counter if metrics.rule_matches is enabled,
// and returns nil otherwise. Its authority tag is ruleMatchedAuthority, not waf_tx_blocked's.
func newRuleMatchedMetric(h shared.HttpFilterConfigHandle, cfg metricsConfig) *pkg.Metric {
	if !cfg.ruleMatches {
		return nil
	}
	auth := cfg.authority.ruleMatchedAuthority()
	return pkg.NewMetric(h, h.DefineCounter, "waf_rule_matched", auth.tagNames("phase", "rule_id")...)
}

// RecordMatchedRules increments the matched rule counter once for every rule a transaction
// matched, whether or not it interrupted the transaction: with OWASP CRS, these are the signature
// rules that raised the anomaly score, besides the anomaly blocking rule that waf_tx_blocked counts.
// Rules without the log action (set on the rule or by SecDefaultAction), such as the CRS
// initialization rules that match every transaction, are skipped, as Coraza does for its error log
// callback.
// It does nothing unless metrics.rule_matches is enabled. It should be called once for every
// transaction, when it ends.
func (m *metrics) RecordMatchedRules(h shared.HttpFilterHandle, authority string, matched []ctypes.MatchedRule) {
	if m.ruleMatched == nil {
		return
	}
	labels := m.authority.ruleMatchedAuthority()
	for i, mr := range matched {
		if !isLogged(mr) || matchedBefore(matched[:i], mr.Rule().ID()) {
			continue
		}
		m.ruleMatched.Record(h, h.IncrementCounterValue, 1,
			labels.tags(h, authority,
				strconv.Itoa(int(mr.Rule().Phase())),
				strconv.Itoa(mr.Rule().ID()),
			)...,
		)
	}
}
