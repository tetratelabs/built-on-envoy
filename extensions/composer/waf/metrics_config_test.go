// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package waf

import (
	"encoding/json"
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
	"github.com/tetratelabs/built-on-envoy/extensions/composer/waf/logger"
)

// metricsTestDirectives block the requests carrying "X-Block-Test: block-me" in phase 1.
var metricsTestDirectives = []string{
	"SecRuleEngine On",
	`SecRule REQUEST_HEADERS:X-Block-Test "@streq block-me" "id:100001,phase:1,deny,status:403,log,msg:'blocked'"`,
}

// newMetricsTestConfigHandleTags returns a filter config handle mock expecting the metrics that
// every filter config defines, with waf_tx_blocked defined with exactly the given tags. Gauges are
// not under test here and are accepted.
func newMetricsTestConfigHandleTags(ctrl *gomock.Controller, blockedTags ...any) *mocks.MockHttpFilterConfigHandle {
	h := mocks.NewMockHttpFilterConfigHandle(ctrl)
	h.EXPECT().DefineCounter("waf_tx_total").Return(shared.MetricID(1), shared.MetricsSuccess)
	h.EXPECT().DefineCounter("waf_tx_blocked", blockedTags...).Return(shared.MetricID(2), shared.MetricsSuccess)
	h.EXPECT().DefineHistogram("waf_tx_duration").Return(shared.MetricID(3), shared.MetricsSuccess)
	h.EXPECT().DefineGauge(gomock.Any()).Return(shared.MetricID(4), shared.MetricsSuccess).AnyTimes()
	h.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	return h
}

// newMetricsTestFactory creates a filter factory from metricsTestDirectives (unless config sets its
// own) and the given settings.
func newMetricsTestFactory(t *testing.T, configHandle *mocks.MockHttpFilterConfigHandle, config map[string]any) shared.HttpFilterFactory {
	t.Helper()
	full := map[string]any{"directives": metricsTestDirectives}
	maps.Copy(full, config)
	configBytes, err := json.Marshal(full)
	require.NoError(t, err)
	factory, err := (&wafPluginConfigFactory{}).Create(configHandle, configBytes)
	require.NoError(t, err)
	return factory
}

// newMetricsTestPluginHandle returns a stream handle mock that accepts everything a stream does
// except the tagged counters, which each test expects explicitly: an unexpected waf_tx_blocked
// or waf_rule_matched increment fails the test.
func newMetricsTestPluginHandle(ctrl *gomock.Controller) *mocks.MockHttpFilterHandle {
	h := mocks.NewMockHttpFilterHandle(ctrl)
	h.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	h.EXPECT().GetMostSpecificConfig().Return(nil).AnyTimes()
	h.EXPECT().GetAttributeString(shared.AttributeIDRequestProtocol).Return(pkg.UnsafeBufferFromString("HTTP/1.1"), true).AnyTimes()
	h.EXPECT().GetAttributeString(shared.AttributeIDSourceAddress).Return(pkg.UnsafeBufferFromString("10.0.0.1:12345"), true).AnyTimes()
	h.EXPECT().IncrementCounterValue(shared.MetricID(1), uint64(1)).Return(shared.MetricsSuccess).AnyTimes()
	h.EXPECT().RecordHistogramValue(shared.MetricID(3), gomock.Any()).Return(shared.MetricsSuccess).AnyTimes()
	h.EXPECT().IncrementGaugeValue(gomock.Any(), gomock.Any()).Return(shared.MetricsSuccess).AnyTimes()
	h.EXPECT().DecrementGaugeValue(gomock.Any(), gomock.Any()).Return(shared.MetricsSuccess).AnyTimes()
	h.EXPECT().SetMetadata(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	h.EXPECT().SendLocalResponse(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	return h
}

// driveRequest runs a header-only GET through a new stream of the factory, then ends the stream.
// headers override the default pseudo-headers.
func driveRequest(t *testing.T, factory shared.HttpFilterFactory, h shared.HttpFilterHandle, headers map[string][]string) shared.HeadersStatus {
	t.Helper()
	p, ok := factory.Create(h).(*wafPlugin)
	require.True(t, ok, "failed to cast plugin to wafPlugin")
	m := map[string][]string{":authority": {"example.com:8080"}, ":method": {"GET"}, ":path": {"/"}}
	maps.Copy(m, headers)
	status := p.OnRequestHeaders(fake.NewFakeHeaderMap(m), true)
	p.OnStreamComplete()
	return status
}

func Test_ParseConfigMetrics(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config string
		want   authorityLabel
	}{
		{"unset is the header", `{}`, authorityLabel{}},
		{"null is the header", `{"metrics": null}`, authorityLabel{}},
		{"empty metrics is the header", `{"metrics": {}}`, authorityLabel{}},
		{"authority_label true is the header", `{"metrics": {"authority_label": true}}`, authorityLabel{}},
		{"authority_label false", `{"metrics": {"authority_label": false}}`, authorityLabel{disabled: true}},
		{"other top-level keys are not checked here", `{"mode": "FULL", "unknown": 1}`, authorityLabel{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := parseConfig([]byte(tc.config), logger.GetLogger())
			require.NoError(t, err)
			require.Equal(t, tc.want, cfg.metrics.authority)
		})
	}

	for _, tc := range []struct {
		name    string
		config  string
		wantErr []string
	}{
		{"invalid json", `{invalid`, []string{"failed to unmarshal config"}},
		{"metrics is not an object", `{"metrics": true}`, []string{"invalid metrics"}},
		{"authority_label is not a boolean", `{"metrics": {"authority_label": "false"}}`, []string{"invalid metrics"}},
		{"unknown key", `{"metrics": {"authority_lable": false}}`, []string{"invalid metrics", "authority_lable"}},
		{"authority_literal is not a setting", `{"metrics": {"authority_literal": "shop"}}`, []string{"invalid metrics", "authority_literal"}},
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

func Test_InvalidMetricsConfigRejectsFilterConfig(t *testing.T) {
	for _, metrics := range []map[string]any{
		{"authority_lable": false},
		{"authority_literal": "shop"},
	} {
		ctrl := gomock.NewController(t)
		// No expectations: the filter config must be rejected before any metric is defined.
		configHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
		configBytes, err := json.Marshal(map[string]any{"directives": metricsTestDirectives, "metrics": metrics})
		require.NoError(t, err)
		_, err = (&wafPluginConfigFactory{}).Create(configHandle, configBytes)
		require.ErrorContains(t, err, "invalid metrics")
	}
}

func Test_PerRouteMetricsConfig(t *testing.T) {
	create := func(metrics map[string]any) error {
		configBytes, err := json.Marshal(map[string]any{"directives": metricsTestDirectives, "metrics": metrics})
		require.NoError(t, err)
		_, err = (&wafPluginConfigFactory{}).CreatePerRoute(configBytes)
		return err
	}
	// Valid settings are accepted, although per-route configurations do not override them.
	require.NoError(t, create(map[string]any{"authority_label": false}))
	// Invalid ones are rejected rather than silently ignored.
	require.ErrorContains(t, create(map[string]any{"authority_lable": false}), "invalid metrics")
	require.ErrorContains(t, create(map[string]any{"authority_literal": "shop"}), "invalid metrics")
}

// Test_MetricsAuthorityLabel checks the tags waf_tx_blocked is defined and recorded with, for every
// metrics setting, through a real filter and Coraza transaction.
func Test_MetricsAuthorityLabel(t *testing.T) {
	blockedPhase := strconv.Itoa(int(ctypes.PhaseRequestHeaders))
	withAuthority := []any{"authority", "phase", "rule_id"}
	for _, tc := range []struct {
		name        string
		config      map[string]any
		definedTags []any
		wantTags    []any
	}{
		{"unset is the header without its port", nil, withAuthority, []any{"attacker-1.example", blockedPhase, "100001"}},
		{"empty metrics", map[string]any{"metrics": map[string]any{}}, withAuthority, []any{"attacker-1.example", blockedPhase, "100001"}},
		{"authority_label true", map[string]any{"metrics": map[string]any{"authority_label": true}}, withAuthority, []any{"attacker-1.example", blockedPhase, "100001"}},
		{
			"authority_label false defines the metric without the authority tag",
			map[string]any{"metrics": map[string]any{"authority_label": false}},
			[]any{"phase", "rule_id"},
			[]any{blockedPhase, "100001"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			factory := newMetricsTestFactory(t, newMetricsTestConfigHandleTags(ctrl, tc.definedTags...), tc.config)
			h := newMetricsTestPluginHandle(ctrl)
			h.EXPECT().IncrementCounterValue(shared.MetricID(2), uint64(1), tc.wantTags...).Return(shared.MetricsSuccess)

			status := driveRequest(t, factory, h, map[string][]string{
				":authority":   {"attacker-1.example:8080"},
				"x-block-test": {"block-me"},
			})
			require.Equal(t, shared.HeadersStatusStop, status)
		})
	}

	// The point of the settings: distinct client-chosen authorities share one series.
	for _, tc := range []struct {
		name        string
		metrics     map[string]any
		definedTags []any
		wantTags    []any
	}{
		{"authority_label false", map[string]any{"authority_label": false}, []any{"phase", "rule_id"}, []any{blockedPhase, "100001"}},
	} {
		t.Run(tc.name+" shares one series across authorities", func(t *testing.T) {
			ctrl := gomock.NewController(t)
			factory := newMetricsTestFactory(t, newMetricsTestConfigHandleTags(ctrl, tc.definedTags...), map[string]any{"metrics": tc.metrics})
			authorities := []string{"attacker-1.example", "attacker-2.example:8443", "10.1.2.3"}
			h := newMetricsTestPluginHandle(ctrl)
			h.EXPECT().IncrementCounterValue(shared.MetricID(2), uint64(1), tc.wantTags...).
				Return(shared.MetricsSuccess).Times(len(authorities))
			for _, authority := range authorities {
				status := driveRequest(t, factory, h, map[string][]string{
					":authority":   {authority},
					"x-block-test": {"block-me"},
				})
				require.Equal(t, shared.HeadersStatusStop, status)
			}
		})
	}
}

// Test_MetricsAuthorityLabelInternalBlock checks the tags of the blocks that are not caused by a
// rule (internal errors).
func Test_MetricsAuthorityLabelInternalBlock(t *testing.T) {
	phase := strconv.Itoa(int(ctypes.PhaseRequestBody))
	for _, tc := range []struct {
		name        string
		cfg         metricsConfig
		definedTags []any
		wantTags    []any
	}{
		{"header", metricsConfig{}, []any{"authority", "phase", "rule_id"}, []any{"attacker-1.example", phase, ""}},
		{"authority_label false", metricsConfig{authority: authorityLabel{disabled: true}}, []any{"phase", "rule_id"}, []any{phase, ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			m := newMetrics(newMetricsTestConfigHandleTags(ctrl, tc.definedTags...), tc.cfg)
			h := mocks.NewMockHttpFilterHandle(ctrl)
			h.EXPECT().IncrementCounterValue(shared.MetricID(2), uint64(1), tc.wantTags...).Return(shared.MetricsSuccess)
			m.RecordBlockInternal(h, "attacker-1.example", ctypes.PhaseRequestBody)
		})
	}
}
