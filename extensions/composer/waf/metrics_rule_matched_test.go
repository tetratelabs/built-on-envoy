// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package waf

import (
	"maps"
	"strconv"
	"testing"

	ctypes "github.com/corazawaf/coraza/v3/types"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	fake "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/fake"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/pkg"
)

// ruleMatchedDirectives returns rules covering the kinds of matches waf_rule_matched must tell apart,
// for the given SecRuleEngine value:
//   - 100010 matches every transaction, with nolog (like the CRS initialization rules): never counted.
//   - 100020 (phase 1) logs a match without any disruptive action.
//   - 100030 (phase 1) matches without the log action: phase 1 has no SecDefaultAction here, so it is not counted.
//   - 100021 (phase 2) logs a match and passes (like a CRS signature raising the anomaly score).
//   - 100001 (phase 1) denies.
//   - 100006 (phase 1) denies without the log action: named by block_rule and counted in waf_tx_blocked, not here.
//   - 100050 (phase 5) logs a match in the logging phase: counted only if the matches are read after ProcessLogging.
func ruleMatchedDirectives(engine string) []string {
	return []string{
		"SecRuleEngine " + engine,
		`SecAction "id:100010,phase:1,pass,nolog"`,
		`SecRule REQUEST_HEADERS:X-Note "@streq note-me" "id:100020,phase:1,log,msg:'noted'"`,
		`SecRule REQUEST_HEADERS:X-Implicit "@streq quiet" "id:100030,phase:1,pass"`,
		`SecRule REQUEST_HEADERS:X-Score "@streq score-me" "id:100021,phase:2,pass,log,msg:'scored'"`,
		`SecRule REQUEST_HEADERS:X-Block-Test "@streq block-me" "id:100001,phase:1,deny,status:403,log,msg:'blocked'"`,
		`SecRule REQUEST_HEADERS:X-Block-Quiet "@streq block-quiet" "id:100006,phase:1,deny,status:403"`,
		`SecRule REQUEST_HEADERS:X-Log-Phase "@streq log-me" "id:100050,phase:5,pass,log,msg:'logged late'"`,
	}
}

var (
	phase1 = strconv.Itoa(int(ctypes.PhaseRequestHeaders))
	phase2 = strconv.Itoa(int(ctypes.PhaseRequestBody))
	phase5 = strconv.Itoa(int(ctypes.PhaseLogging))
)

// newRuleMatchedConfigHandle returns a filter config handle mock that also expects waf_rule_matched
// to be defined, as metric 5. waf_tx_blocked keeps its default authority tag. waf_rule_matched does
// not: the default authority_label is authority, and that counter has no authority tag.
func newRuleMatchedConfigHandle(ctrl *gomock.Controller) *mocks.MockHttpFilterConfigHandle {
	return newRuleMatchedConfigHandleSplit(ctrl,
		[]any{"authority", "phase", "rule_id"},
		[]any{"phase", "rule_id"},
	)
}

// newRuleMatchedConfigHandleSplit expects waf_tx_blocked and waf_rule_matched with their own tags.
func newRuleMatchedConfigHandleSplit(ctrl *gomock.Controller, blocked, matched []any) *mocks.MockHttpFilterConfigHandle {
	h := newMetricsTestConfigHandleTags(ctrl, blocked...)
	h.EXPECT().DefineCounter("waf_rule_matched", matched...).Return(shared.MetricID(5), shared.MetricsSuccess)
	return h
}

// expectRuleMatched expects exactly one waf_rule_matched increment with the given tags.
func expectRuleMatched(h *mocks.MockHttpFilterHandle, tags ...any) {
	h.EXPECT().IncrementCounterValue(shared.MetricID(5), uint64(1), tags...).Return(shared.MetricsSuccess)
}

func Test_RuleMatched(t *testing.T) {
	enabled := func(engine string, extra map[string]any) map[string]any {
		metrics := map[string]any{"rule_matches": true}
		maps.Copy(metrics, extra)
		return map[string]any{"directives": ruleMatchedDirectives(engine), "metrics": metrics}
	}

	t.Run("clean request adds nothing, although a nolog rule matched", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		factory := newMetricsTestFactory(t, newRuleMatchedConfigHandle(ctrl), enabled("On", nil))
		h := newMetricsTestPluginHandle(ctrl) // no waf_rule_matched increment expected

		p, ok := factory.Create(h).(*wafPlugin)
		require.True(t, ok)
		status := p.OnRequestHeaders(fake.NewFakeHeaderMap(map[string][]string{
			":authority": {"example.com"}, ":method": {"GET"}, ":path": {"/"},
		}), true)
		require.Equal(t, shared.HeadersStatusContinue, status)
		// The nolog rule did match: skipping it is what keeps clean requests out of the counter.
		var ids []int
		for _, mr := range p.txContext.MatchedRules() {
			ids = append(ids, mr.Rule().ID())
		}
		require.Equal(t, []int{100010}, ids)
		p.OnStreamComplete()
	})

	t.Run("matches below the blocking rule are counted for a request that is not blocked", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		factory := newMetricsTestFactory(t, newRuleMatchedConfigHandle(ctrl), enabled("On", nil))
		h := newMetricsTestPluginHandle(ctrl)
		expectRuleMatched(h, phase1, "100020")
		expectRuleMatched(h, phase2, "100021")

		status := driveRequest(t, factory, h, map[string][]string{"x-note": {"note-me"}, "x-score": {"score-me"}})
		require.Equal(t, shared.HeadersStatusContinue, status)
	})

	t.Run("a match without the log action is not counted", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		factory := newMetricsTestFactory(t, newRuleMatchedConfigHandle(ctrl), enabled("On", nil))
		h := newMetricsTestPluginHandle(ctrl) // no waf_rule_matched increment expected

		status := driveRequest(t, factory, h, map[string][]string{"x-implicit": {"quiet"}})
		require.Equal(t, shared.HeadersStatusContinue, status)
	})

	t.Run("a blocking rule without the log action is not counted", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		factory := newMetricsTestFactory(t, newRuleMatchedConfigHandle(ctrl), enabled("On", nil))
		h := newMetricsTestPluginHandle(ctrl) // no waf_rule_matched increment expected
		h.EXPECT().IncrementCounterValue(shared.MetricID(2), uint64(1), "example.com", phase1, "100006").Return(shared.MetricsSuccess)

		status := driveRequest(t, factory, h, map[string][]string{"x-block-quiet": {"block-quiet"}})
		require.Equal(t, shared.HeadersStatusStop, status)
	})

	t.Run("a match in the logging phase is counted", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		factory := newMetricsTestFactory(t, newRuleMatchedConfigHandle(ctrl), enabled("On", nil))
		h := newMetricsTestPluginHandle(ctrl)
		expectRuleMatched(h, phase5, "100050")

		status := driveRequest(t, factory, h, map[string][]string{"x-log-phase": {"log-me"}})
		require.Equal(t, shared.HeadersStatusContinue, status)
	})

	t.Run("blocked request counts every matched rule, the blocking one included", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		factory := newMetricsTestFactory(t, newRuleMatchedConfigHandle(ctrl), enabled("On", nil))
		h := newMetricsTestPluginHandle(ctrl)
		h.EXPECT().IncrementCounterValue(shared.MetricID(2), uint64(1), "example.com", phase1, "100001").Return(shared.MetricsSuccess)
		expectRuleMatched(h, phase1, "100020")
		expectRuleMatched(h, phase1, "100001")

		status := driveRequest(t, factory, h, map[string][]string{"x-note": {"note-me"}, "x-block-test": {"block-me"}})
		require.Equal(t, shared.HeadersStatusStop, status)
	})

	t.Run("DetectionOnly counts the rules that would have blocked", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		factory := newMetricsTestFactory(t, newRuleMatchedConfigHandle(ctrl), enabled("DetectionOnly", nil))
		h := newMetricsTestPluginHandle(ctrl) // no waf_tx_blocked increment expected
		expectRuleMatched(h, phase1, "100001")

		status := driveRequest(t, factory, h, map[string][]string{"x-block-test": {"block-me"}})
		require.Equal(t, shared.HeadersStatusContinue, status)
	})

	t.Run("each request counts its own matches", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		factory := newMetricsTestFactory(t, newRuleMatchedConfigHandle(ctrl), enabled("On", nil))
		h := newMetricsTestPluginHandle(ctrl)
		h.EXPECT().IncrementCounterValue(shared.MetricID(5), uint64(1), phase1, "100020").
			Return(shared.MetricsSuccess).Times(3)
		for range 3 {
			require.Equal(t, shared.HeadersStatusContinue, driveRequest(t, factory, h, map[string][]string{"x-note": {"note-me"}}))
		}
	})

	t.Run("authority tag on waf_rule_matched", func(t *testing.T) {
		withAuthority := []any{"authority", "phase", "rule_id"}
		phaseRule := []any{"phase", "rule_id"}
		attribute := func(id shared.AttributeID, v string, ok bool) func(*mocks.MockHttpFilterHandle) {
			return func(h *mocks.MockHttpFilterHandle) {
				h.EXPECT().GetAttributeString(id).Return(pkg.UnsafeBufferFromString(v), ok).AnyTimes()
			}
		}
		for _, tc := range []struct {
			name        string
			extra       map[string]any
			lookup      func(*mocks.MockHttpFilterHandle)
			blockedTags []any
			matchedTags []any
			wantTags    []any
		}{
			{"default", nil, nil, withAuthority, phaseRule, []any{phase1, "100020"}},
			{"authority", map[string]any{"authority_label": "authority"}, nil, withAuthority, phaseRule, []any{phase1, "100020"}},
			{"none", map[string]any{"authority_label": "none"}, nil, phaseRule, phaseRule, []any{phase1, "100020"}},
			{
				"route_name",
				map[string]any{"authority_label": "route_name"},
				attribute(shared.AttributeIDXdsRouteName, "rt-a", true),
				withAuthority, withAuthority,
				[]any{"rt-a", phase1, "100020"},
			},
			{
				"virtual_host_name",
				map[string]any{"authority_label": "virtual_host_name"},
				attribute(shared.AttributeIDXdsVirtualHostName, "vh-a", true),
				withAuthority, withAuthority,
				[]any{"vh-a", phase1, "100020"},
			},
			{
				"route without a name",
				map[string]any{"authority_label": "route_name"},
				attribute(shared.AttributeIDXdsRouteName, "", true),
				withAuthority, withAuthority,
				[]any{unresolvedLabel, phase1, "100020"},
			},
			{
				"Envoy without the virtual host attribute",
				map[string]any{"authority_label": "virtual_host_name"},
				attribute(shared.AttributeIDXdsVirtualHostName, "", false),
				withAuthority, withAuthority,
				[]any{unresolvedLabel, phase1, "100020"},
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				ctrl := gomock.NewController(t)
				factory := newMetricsTestFactory(t,
					newRuleMatchedConfigHandleSplit(ctrl, tc.blockedTags, tc.matchedTags),
					enabled("On", tc.extra))
				h := newMetricsTestPluginHandle(ctrl)
				if tc.lookup != nil {
					tc.lookup(h)
				}
				expectRuleMatched(h, tc.wantTags...)

				status := driveRequest(t, factory, h, map[string][]string{":authority": {"attacker-1.example"}, "x-note": {"note-me"}})
				require.Equal(t, shared.HeadersStatusContinue, status)
			})
		}
	})
}

func Test_RuleMatchedOffByDefault(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config map[string]any
	}{
		{"unset", map[string]any{"directives": ruleMatchedDirectives("On")}},
		{"empty metrics", map[string]any{"directives": ruleMatchedDirectives("On"), "metrics": map[string]any{}}},
		{"false", map[string]any{"directives": ruleMatchedDirectives("On"), "metrics": map[string]any{"rule_matches": false}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			// Strict: defining waf_rule_matched is an unexpected call.
			factory := newMetricsTestFactory(t, newMetricsTestConfigHandle(ctrl), tc.config)
			wafFactory, ok := factory.(*wafPluginFactory)
			require.True(t, ok)
			require.Nil(t, wafFactory.metrics.ruleMatched)

			h := newMetricsTestPluginHandle(ctrl) // only waf_tx_blocked is expected
			h.EXPECT().IncrementCounterValue(shared.MetricID(2), uint64(1), "example.com", phase1, "100001").Return(shared.MetricsSuccess)
			status := driveRequest(t, factory, h, map[string][]string{"x-note": {"note-me"}, "x-block-test": {"block-me"}})
			require.Equal(t, shared.HeadersStatusStop, status)
		})
	}
}

// fakeRuleMetadata and the fake matched rules below implement only what RecordMatchedRules reads.
type fakeRuleMetadata struct {
	ctypes.RuleMetadata
	id    int
	phase ctypes.RulePhase
}

func (r fakeRuleMetadata) ID() int                 { return r.id }
func (r fakeRuleMetadata) Phase() ctypes.RulePhase { return r.phase }

// fakeLoggedMatch is a matched rule exposing Log, as Coraza's implementation does.
type fakeLoggedMatch struct {
	ctypes.MatchedRule
	rule fakeRuleMetadata
	log  bool
}

func (m fakeLoggedMatch) Rule() ctypes.RuleMetadata { return m.rule }
func (m fakeLoggedMatch) Log() bool                 { return m.log }

// fakeMatch is a matched rule that does not expose Log.
type fakeMatch struct {
	ctypes.MatchedRule
	rule fakeRuleMetadata
}

func (m fakeMatch) Rule() ctypes.RuleMetadata { return m.rule }

func Test_RecordMatchedRules(t *testing.T) {
	ctrl := gomock.NewController(t)
	m := newMetrics(newRuleMatchedConfigHandle(ctrl), metricsConfig{ruleMatches: true})
	h := mocks.NewMockHttpFilterHandle(ctrl)
	expectRuleMatched(h, phase2, "942100")
	expectRuleMatched(h, phase2, "949110")
	expectRuleMatched(h, phase1, "200001")

	m.RecordMatchedRules(h, "example.com", []ctypes.MatchedRule{
		fakeLoggedMatch{rule: fakeRuleMetadata{id: 901100, phase: ctypes.PhaseRequestHeaders}, log: false},
		fakeLoggedMatch{rule: fakeRuleMetadata{id: 942100, phase: ctypes.PhaseRequestBody}, log: true},
		// The same rule twice in a transaction is counted once.
		fakeLoggedMatch{rule: fakeRuleMetadata{id: 942100, phase: ctypes.PhaseRequestBody}, log: true},
		fakeLoggedMatch{rule: fakeRuleMetadata{id: 949110, phase: ctypes.PhaseRequestBody}, log: true},
		// Without Log, the rule is counted.
		fakeMatch{rule: fakeRuleMetadata{id: 200001, phase: ctypes.PhaseRequestHeaders}},
	})

	// Disabled: nothing is recorded, whatever matched.
	off := newMetrics(newMetricsTestConfigHandle(ctrl), metricsConfig{})
	off.RecordMatchedRules(mocks.NewMockHttpFilterHandle(ctrl), "example.com", []ctypes.MatchedRule{
		fakeLoggedMatch{rule: fakeRuleMetadata{id: 942100, phase: ctypes.PhaseRequestBody}, log: true},
	})
}
