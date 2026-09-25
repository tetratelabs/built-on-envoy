// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package waf

import (
	"encoding/json"
	"fmt"
	"runtime"
	"runtime/debug"
	"testing"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	waf "github.com/tetratelabs/built-on-envoy/extensions/composer/waf/coraza"
)

func reloadTestConfig(t *testing.T, mode string, directives ...string) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{"directives": directives, "mode": mode})
	require.NoError(t, err)
	return b
}

func newReloadTestConfigHandle(ctrl *gomock.Controller) *mocks.MockHttpFilterConfigHandle {
	h := mocks.NewMockHttpFilterConfigHandle(ctrl)
	h.EXPECT().DefineCounter(gomock.Any(), gomock.Any()).Return(shared.MetricID(1), shared.MetricsSuccess).AnyTimes()
	h.EXPECT().DefineCounter(gomock.Any()).Return(shared.MetricID(1), shared.MetricsSuccess).AnyTimes()
	h.EXPECT().DefineHistogram(gomock.Any()).Return(shared.MetricID(2), shared.MetricsSuccess).AnyTimes()
	h.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	return h
}

func Test_ConfigsShareWAF(t *testing.T) {
	ctrl := gomock.NewController(t)
	rule := `SecRule REQUEST_URI "@rx ^/configs-share$" "id:1,phase:1,deny"`

	cf := &wafPluginConfigFactory{}
	factory, err := cf.Create(newReloadTestConfigHandle(ctrl), reloadTestConfig(t, "FULL", "SecRuleEngine On", rule))
	require.NoError(t, err)
	perRoute, err := cf.CreatePerRoute(reloadTestConfig(t, "REQUEST_ONLY", "SecRuleEngine On", rule))
	require.NoError(t, err)
	// A later config generation (e.g. after an RDS update) with the same rules.
	nextGen, err := (&wafPluginConfigFactory{}).CreatePerRoute(reloadTestConfig(t, "FULL", "SecRuleEngine On", rule))
	require.NoError(t, err)
	otherRules, err := cf.CreatePerRoute(reloadTestConfig(t, "FULL", "SecRuleEngine DetectionOnly", rule))
	require.NoError(t, err)

	main := factory.(*wafPluginFactory)
	require.Equal(t, waf.ModeFull, main.mode)
	require.Same(t, main.config, perRoute.(*perRouteWafPluginConfig).config, "mode is not part of the WAF")
	require.Equal(t, waf.ModeRequestOnly, perRoute.(*perRouteWafPluginConfig).mode)
	require.Same(t, main.config, nextGen.(*perRouteWafPluginConfig).config)
	require.NotSame(t, main.config, otherRules.(*perRouteWafPluginConfig).config)
}

func Test_ConfigInvalidMode(t *testing.T) {
	_, err := (&wafPluginConfigFactory{}).CreatePerRoute(reloadTestConfig(t, "BOGUS", "SecRuleEngine On"))
	require.ErrorContains(t, err, "invalid mode")
}

func heapAllocAfterGC() uint64 {
	runtime.GC()
	runtime.GC()
	debug.FreeOSMemory()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

// Simulates Envoy config reloads while long-lived streams (e.g. WebSockets) opened
// in every config generation stay open, each holding the config it started with.
func Test_ConfigReloadsDoNotGrowHeap(t *testing.T) {
	const reloads = 30
	ctrl := gomock.NewController(t)

	directives := []string{"SecRuleEngine On"}
	for i := 1; i <= 200; i++ {
		directives = append(directives, fmt.Sprintf(`SecRule REQUEST_URI "@rx ^/reload-heap/%d$" "id:%d,phase:1,pass,nolog"`, i, i))
	}
	cfg := reloadTestConfig(t, "FULL", directives...)

	reload := func() shared.HttpFilter {
		cf := &wafPluginConfigFactory{}
		f, err := cf.Create(newReloadTestConfigHandle(ctrl), cfg)
		require.NoError(t, err)
		perRoute, err := cf.CreatePerRoute(cfg)
		require.NoError(t, err)
		return f.Create(newPluginHandleWithPerRouteConfig(ctrl, perRoute))
	}

	streams := []shared.HttpFilter{reload()}
	baseline := heapAllocAfterGC()
	for range reloads {
		streams = append(streams, reload())
	}
	growthKB := (max(heapAllocAfterGC(), baseline) - baseline) / 1024
	runtime.KeepAlive(streams)

	require.LessOrEqual(t, growthKB, uint64(512), "heap grew %d KB after %d reloads", growthKB, reloads)
}
