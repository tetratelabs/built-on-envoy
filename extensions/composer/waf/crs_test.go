// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Tests to validate that waf behavior is aligned with CRS expectations.
// These tests include validating that certain coraza build tags are enabled by default in the plugin.
// Details on each build tag can be found at https://github.com/corazawaf/coraza?tab=readme-ov-file#build-tags

//go:build coraza.rule.case_sensitive_args_keys && coraza.rule.no_regex_multiline && coraza.rule.mandatory_rule_id_check && coraza.rule.rx_prefilter

package waf

import (
	"encoding/json"
	"testing"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	fake "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/fake"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/pkg"
)

// newPluginHandle creates a mock HttpFilterHandle. All side-effect calls (metrics,
// metadata, local responses) are caught with AnyTimes wildcards since these tests
// only assert on the filter status returned by OnRequestHeaders/OnRequestBody.
func newPluginHandle(ctrl *gomock.Controller, sourceAddr, protocol string) *mocks.MockHttpFilterHandle {
	h := mocks.NewMockHttpFilterHandle(ctrl)
	h.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	h.EXPECT().GetAttributeString(shared.AttributeIDRequestProtocol).Return(pkg.UnsafeBufferFromString(protocol), true).AnyTimes()
	h.EXPECT().GetAttributeString(shared.AttributeIDSourceAddress).Return(pkg.UnsafeBufferFromString(sourceAddr), true).AnyTimes()
	// waf_tx_total (2 args)
	h.EXPECT().IncrementCounterValue(gomock.Any(), gomock.Any()).Return(shared.MetricsSuccess).AnyTimes()
	// waf_tx_blocked (5 args: id, value, authority, phase, rule_id)
	h.EXPECT().IncrementCounterValue(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(shared.MetricsSuccess).AnyTimes()
	h.EXPECT().SetMetadata(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	h.EXPECT().SendLocalResponse(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	return h
}

// Verify the coraza.rule.case_sensitive_args_keys build tag is used.
// As per RFC 3986, ARGS keys are expected to be case-sensitive.
// A rule targeting ARGS:foo must not fire when the request provides the argument as Foo (different case).
func Test_CaseSensitiveArgsKeys(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	factory := newWAFFactory(t, ctrl, []string{
		"SecRuleEngine On",
		"SecRequestBodyAccess On",
		`SecRule ARGS:foo "@contains xss" "id:1001,phase:2,deny,status:403"`,
	}, "REQUEST_ONLY")

	tests := []struct {
		name           string
		path           string
		wantBodyStatus shared.BodyStatus
	}{
		{
			name:           "lowercase key triggers rule",
			path:           "/?foo=xss",
			wantBodyStatus: shared.BodyStatusStopNoBuffer,
		},
		{
			name:           "uppercase key does not trigger rule",
			path:           "/?Foo=xss",
			wantBodyStatus: shared.BodyStatusContinue,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := factory.Create(newPluginHandle(ctrl, "10.0.0.1:1234", "HTTP/1.1")).(*wafPlugin)
			headers := fake.NewFakeHeaderMap(map[string][]string{
				":authority": {"example.com"}, ":method": {"GET"},
				":path": {tc.path}, "x-request-id": {tc.name},
			})
			require.Equal(t, shared.HeadersStatusStop, p.OnRequestHeaders(headers, false))
			require.Equal(t, tc.wantBodyStatus, p.OnRequestBody(fake.NewFakeBodyBuffer(nil), true))
			p.OnStreamComplete()
		})
	}
}

// Verify the coraza.rule.no_regex_multiline build tag is used.
// By default the @rx operator in coraza should not have the multiline regex modifier active
// anymore.
func Test_NoRegexMultiline(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	factory := newWAFFactory(t, ctrl, []string{
		"SecRuleEngine On",
		`SecRule ARGS:param "@rx ^evil$" "id:1002,phase:1,deny,status:403"`,
	}, "REQUEST_ONLY")

	tests := []struct {
		name              string
		path              string
		wantHeadersStatus shared.HeadersStatus
	}{
		{
			name:              "exact match triggers rule",
			path:              "/?param=evil",
			wantHeadersStatus: shared.HeadersStatusStop,
		},
		{
			name: "evil on a separate line does not trigger rule",
			// ARGS:param decodes to "good\nevil". With no_regex_multiline, ^evil$ requires
			// the whole value to equal "evil", so the multi-line value does not match.
			path:              "/?param=good%0Aevil",
			wantHeadersStatus: shared.HeadersStatusContinue,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := factory.Create(newPluginHandle(ctrl, "10.0.0.1:1234", "HTTP/1.1")).(*wafPlugin)
			headers := fake.NewFakeHeaderMap(map[string][]string{
				":authority": {"example.com"}, ":method": {"GET"},
				":path": {tc.path}, "x-request-id": {tc.name},
			})
			require.Equal(t, tc.wantHeadersStatus, p.OnRequestHeaders(headers, true))
			p.OnStreamComplete()
		})
	}
}

// Verify the coraza.rule.mandatory_rule_id_check build tag.
// WAF creation must fail when a SecRule is missing defining the id.
func Test_MandatoryRuleIDCheck(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	tests := []struct {
		name       string
		directives []string
		wantErr    bool
	}{
		{
			name:       "rule without id returns error",
			directives: []string{`SecRule ARGS "@rx test" "phase:2,deny"`},
			wantErr:    true,
		},
		{
			name:       "rule with id succeeds",
			directives: []string{`SecRule ARGS "@rx test" "id:1,phase:2,deny"`},
			wantErr:    false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			config := map[string]interface{}{"directives": tc.directives, "mode": "REQUEST_ONLY"}
			configBytes, err := json.Marshal(config)
			require.NoError(t, err)

			configHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
			configHandle.EXPECT().DefineCounter(gomock.Any(), gomock.Any()).Return(shared.MetricID(1), shared.MetricsSuccess).AnyTimes()
			configHandle.EXPECT().DefineCounter(gomock.Any()).Return(shared.MetricID(1), shared.MetricsSuccess).AnyTimes()

			_, err = (&wafPluginConfigFactory{}).Create(configHandle, configBytes)
			if tc.wantErr {
				require.ErrorContains(t, err, "rule id is missing")
			} else {
				require.NoError(t, err)
			}
		})
	}
}
