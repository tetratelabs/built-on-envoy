// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package waf

import (
	"strconv"
	"testing"

	ctypes "github.com/corazawaf/coraza/v3/types"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/pkg"
	"github.com/tetratelabs/built-on-envoy/extensions/composer/waf/logger"
)

// metadataTestDirectives log a match on X-Seen (100002), match it again without logging (100004), then block on
// "X-Block-Test: block-me" (100001), all in phase 1: the logged rules come before the blocking one so that they run.
// X-Quiet matches only a nolog rule (100003), and X-Implicit only a rule with no log action of its own (100005):
// phase 1 has no SecDefaultAction here, so that rule is not logged either. X-Block-Quiet denies (100006) with no
// log action, so block_rule and waf_tx_blocked record it and matched_rules does not. X-Log-Phase (100050) matches
// in phase 5: it is included only when the IDs are read after ProcessLogging.
var metadataTestDirectives = []string{
	"SecRuleEngine On",
	`SecRule REQUEST_HEADERS:X-Seen "@rx ." "id:100002,phase:1,pass,log,msg:'seen'"`,
	`SecRule REQUEST_HEADERS:X-Seen "@rx ." "id:100004,phase:1,pass,nolog"`,
	`SecRule REQUEST_HEADERS:X-Quiet "@rx ." "id:100003,phase:1,pass,nolog"`,
	`SecRule REQUEST_HEADERS:X-Implicit "@rx ." "id:100005,phase:1,pass"`,
	`SecRule REQUEST_HEADERS:X-Block-Test "@streq block-me" "id:100001,phase:1,deny,status:403,log,msg:'blocked'"`,
	`SecRule REQUEST_HEADERS:X-Block-Quiet "@streq block-quiet" "id:100006,phase:1,deny,status:403"`,
	`SecRule REQUEST_HEADERS:X-Log-Phase "@streq log-me" "id:100050,phase:5,pass,log,msg:'logged late'"`,
}

// newMetadataTestPluginHandle is newMetricsTestPluginHandle without accepting every SetMetadata call: each test
// expects its metadata explicitly, so an unexpected matched_rules key fails the test.
func newMetadataTestPluginHandle(ctrl *gomock.Controller) *mocks.MockHttpFilterHandle {
	h := mocks.NewMockHttpFilterHandle(ctrl)
	h.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	h.EXPECT().GetMostSpecificConfig().Return(nil).AnyTimes()
	h.EXPECT().GetAttributeString(shared.AttributeIDRequestProtocol).Return(pkg.UnsafeBufferFromString("HTTP/1.1"), true).AnyTimes()
	h.EXPECT().GetAttributeString(shared.AttributeIDSourceAddress).Return(pkg.UnsafeBufferFromString("10.0.0.1:12345"), true).AnyTimes()
	h.EXPECT().IncrementCounterValue(shared.MetricID(1), uint64(1)).Return(shared.MetricsSuccess).AnyTimes()
	h.EXPECT().RecordHistogramValue(shared.MetricID(3), gomock.Any()).Return(shared.MetricsSuccess).AnyTimes()
	h.EXPECT().IncrementGaugeValue(gomock.Any(), gomock.Any()).Return(shared.MetricsSuccess).AnyTimes()
	h.EXPECT().DecrementGaugeValue(gomock.Any(), gomock.Any()).Return(shared.MetricsSuccess).AnyTimes()
	h.EXPECT().SendLocalResponse(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	return h
}

// expectBlock expects the counter and metadata of a phase 1 block by the given rule.
func expectBlock(h *mocks.MockHttpFilterHandle, ruleID int) {
	h.EXPECT().IncrementCounterValue(shared.MetricID(2), uint64(1),
		"example.com", strconv.Itoa(int(ctypes.PhaseRequestHeaders)), strconv.Itoa(ruleID)).Return(shared.MetricsSuccess)
	h.EXPECT().SetMetadata(defaultMetadataNamespace, metadataKeyBlockRule, ruleID)
	h.EXPECT().SetMetadata(defaultMetadataNamespace, metadataKeyBlockPhase, int(ctypes.PhaseRequestHeaders))
}

func Test_ParseConfigMetadata(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config string
		want   bool
	}{
		{"unset is off", `{}`, false},
		{"null is off", `{"metadata": null}`, false},
		{"empty metadata is off", `{"metadata": {}}`, false},
		{"matched_rules false", `{"metadata": {"matched_rules": false}}`, false},
		{"matched_rules true", `{"metadata": {"matched_rules": true}}`, true},
		{"other top-level keys are not checked here", `{"mode": "FULL", "unknown": 1}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := parseConfig([]byte(tc.config), logger.GetLogger())
			require.NoError(t, err)
			require.Equal(t, tc.want, cfg.metadata.matchedRules)
		})
	}

	for _, tc := range []struct {
		name    string
		config  string
		wantErr []string
	}{
		{"invalid json", `{invalid`, []string{"failed to unmarshal config"}},
		{"metadata is not an object", `{"metadata": true}`, []string{"invalid metadata"}},
		{"matched_rules is not a boolean", `{"metadata": {"matched_rules": "true"}}`, []string{"invalid metadata"}},
		{"metadata is a list", `{"metadata": []}`, []string{"invalid metadata"}},
		{"matched_rules is a number", `{"metadata": {"matched_rules": 1}}`, []string{"invalid metadata"}},
		{"unknown key", `{"metadata": {"matched_rule": true}}`, []string{"invalid metadata", "matched_rule"}},
		{
			"unknown key after a valid one",
			`{"metadata": {"matched_rules": true, "matched_rule": true}}`,
			[]string{"invalid metadata", "matched_rule"},
		},
		{
			"invalid metadata after valid metrics",
			`{"metrics": {"authority_label": "none"}, "metadata": {"matched_rule": true}}`,
			[]string{"invalid metadata", "matched_rule"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseConfig([]byte(tc.config), logger.GetLogger())
			require.Error(t, err)
			for _, want := range tc.wantErr {
				require.ErrorContains(t, err, want)
			}
		})
	}
}

func Test_InvalidMetadataConfigRejectsFilterConfig(t *testing.T) {
	for _, metadata := range []string{
		`{"matched_rule": true}`,
		`{"matched_rules": "true"}`,
		`true`,
	} {
		t.Run(metadata, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			config := []byte(`{"directives": ["SecRuleEngine On"], "metadata": ` + metadata + `}`)

			// No expectations: the filter config must be rejected before any metric is defined.
			_, err := (&wafPluginConfigFactory{}).Create(mocks.NewMockHttpFilterConfigHandle(ctrl), config)
			require.ErrorContains(t, err, "invalid metadata")

			_, err = (&wafPluginConfigFactory{}).CreatePerRoute(config)
			require.ErrorContains(t, err, "invalid metadata")
		})
	}

	// A valid metadata object is accepted per route, although it does not override the filter configuration's.
	_, err := (&wafPluginConfigFactory{}).CreatePerRoute([]byte(`{"directives": ["SecRuleEngine On"], "metadata": {"matched_rules": true}}`))
	require.NoError(t, err)
}

func Test_MatchedRulesMetadata(t *testing.T) {
	enabled := map[string]any{"directives": metadataTestDirectives, "metadata": map[string]any{"matched_rules": true}}
	for _, tc := range []struct {
		name      string
		config    map[string]any
		headers   map[string][]string
		block     bool
		blockRule int    // the blocking rule when block is set; 0 means 100001
		want      string // the expected matched_rules value; "" expects no matched_rules key
	}{
		{
			name:    "off by default",
			config:  map[string]any{"directives": metadataTestDirectives},
			headers: map[string][]string{"x-seen": {"a"}, "x-block-test": {"block-me"}},
			block:   true,
		},
		{
			name:    "a logged match that does not block",
			config:  enabled,
			headers: map[string][]string{"x-seen": {"a"}},
			want:    "100002",
		},
		{
			name:    "the logged matches behind a block, in order, without the nolog rule",
			config:  enabled,
			headers: map[string][]string{"x-seen": {"a"}, "x-block-test": {"block-me"}},
			block:   true,
			want:    "100002,100001",
		},
		{
			name:    "no match sets no key",
			config:  enabled,
			headers: map[string][]string{"x-other": {"a"}},
		},
		{
			name:    "matched_rules false sets no key",
			config:  map[string]any{"directives": metadataTestDirectives, "metadata": map[string]any{"matched_rules": false}},
			headers: map[string][]string{"x-seen": {"a"}},
		},
		{
			name:    "a nolog match alone sets no key",
			config:  enabled,
			headers: map[string][]string{"x-quiet": {"a"}},
		},
		{
			name:    "a match without the log action sets no key",
			config:  enabled,
			headers: map[string][]string{"x-implicit": {"a"}},
		},
		{
			name:      "a blocking rule without the log action is not listed",
			config:    enabled,
			headers:   map[string][]string{"x-block-quiet": {"block-quiet"}},
			block:     true,
			blockRule: 100006,
		},
		{
			name:    "a match in the logging phase is included",
			config:  enabled,
			headers: map[string][]string{"x-log-phase": {"log-me"}},
			want:    "100050",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			factory := newMetricsTestFactory(t, newMetricsTestConfigHandle(ctrl), tc.config)
			h := newMetadataTestPluginHandle(ctrl)
			if tc.block {
				ruleID := tc.blockRule
				if ruleID == 0 {
					ruleID = 100001
				}
				expectBlock(h, ruleID)
			}
			if tc.want != "" {
				h.EXPECT().SetMetadata(defaultMetadataNamespace, metadataKeyMatchedRules, tc.want)
			}
			status := driveRequest(t, factory, h, tc.headers)
			if tc.block {
				require.Equal(t, shared.HeadersStatusStop, status)
			} else {
				require.Equal(t, shared.HeadersStatusContinue, status)
			}
		})
	}
}

// idOnlyRule and idOnlyMatch implement only what matchedRuleIDs reads.
type idOnlyRule struct {
	ctypes.RuleMetadata
	id int
}

func (r idOnlyRule) ID() int { return r.id }

type idOnlyMatch struct {
	ctypes.MatchedRule
	id  int
	log bool
}

func (m idOnlyMatch) Rule() ctypes.RuleMetadata { return idOnlyRule{id: m.id} }
func (m idOnlyMatch) Log() bool                 { return m.log }

func Test_MatchedRuleIDs(t *testing.T) {
	require.Empty(t, matchedRuleIDs(nil))
	require.Empty(t, matchedRuleIDs([]ctypes.MatchedRule{idOnlyMatch{id: 901100, log: false}}))
	// In match order, not sorted; each rule once; nolog rules left out.
	require.Equal(t, "942100,941100,949110", matchedRuleIDs([]ctypes.MatchedRule{
		idOnlyMatch{id: 901100, log: false},
		idOnlyMatch{id: 942100, log: true},
		idOnlyMatch{id: 942100, log: true},
		idOnlyMatch{id: 941100, log: true},
		idOnlyMatch{id: 949110, log: true},
	}))
}
