// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package clusterrouter

import (
	"testing"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/fake"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/cluster-router/graph"
)

func newTestPlugin(t *testing.T, routes map[string]graph.Route) (*Plugin, *mocks.MockHttpFilterHandle) {
	t.Helper()
	ctrl := gomock.NewController(t)
	handle := mocks.NewMockHttpFilterHandle(ctrl)
	handle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	tbl := graph.NewAtomicTable("envoyA")
	tbl.Store(&graph.Table{EnvoyID: "envoyA", Routes: routes})
	cfg := &Config{
		TargetClusterSource: targetSourceHeader,
		TargetClusterHeader: "x-target-cluster",
		NextHopHeader:       "x-next-hop",
	}
	return &Plugin{cfg: cfg, table: tbl, handle: handle}, handle
}

func TestPlugin_LookupHit_WritesHeaderAndMetadata(t *testing.T) {
	p, handle := newTestPlugin(t, map[string]graph.Route{
		"remote_svc": {
			TargetCluster:       "remote_svc",
			NextHopLocalCluster: "peer_envoy_b",
			Distance:            10,
			ASPath:              []string{"envoyB"},
		},
	})

	handle.EXPECT().ClearRouteCache()
	handle.EXPECT().SetMetadata(dynMetadataNamespace, "next_hop_cluster", "peer_envoy_b")
	handle.EXPECT().SetMetadata(dynMetadataNamespace, "target_cluster", "remote_svc")
	handle.EXPECT().SetMetadata(dynMetadataNamespace, "distance", int64(10))
	handle.EXPECT().SetMetadata(dynMetadataNamespace, "as_path", "envoyB")

	hdrs := fake.NewFakeHeaderMap(map[string][]string{
		"x-target-cluster": {"remote_svc"},
	})

	status := p.OnRequestHeaders(hdrs, true)
	require.Equal(t, shared.HeadersStatusContinue, status)
	require.Equal(t, "peer_envoy_b", hdrs.GetOne("x-next-hop").ToUnsafeString())
}

func TestPlugin_LookupMiss_SendsLocalReply503(t *testing.T) {
	p, handle := newTestPlugin(t, map[string]graph.Route{})

	var sentBody []byte
	handle.EXPECT().SendLocalResponse(uint32(503), gomock.Any(), gomock.Any(), gomock.Any()).
		Do(func(_ uint32, _ [][2]string, body []byte, _ string) {
			sentBody = body
		})

	hdrs := fake.NewFakeHeaderMap(map[string][]string{
		"x-target-cluster": {"unknown"},
	})
	status := p.OnRequestHeaders(hdrs, true)
	require.Equal(t, shared.HeadersStatusStop, status)
	require.Contains(t, string(sentBody), `"target":"unknown"`)
}

func TestPlugin_NoTargetHeader_PassesThrough(t *testing.T) {
	p, _ := newTestPlugin(t, map[string]graph.Route{"svc": {TargetCluster: "svc"}})
	hdrs := fake.NewFakeHeaderMap(map[string][]string{"x-other": {"v"}})
	status := p.OnRequestHeaders(hdrs, true)
	require.Equal(t, shared.HeadersStatusContinue, status)
	require.Empty(t, hdrs.GetOne("x-next-hop").ToUnsafeString())
}

func TestPlugin_LocalTerminal_OmitsASPathMetadata(t *testing.T) {
	p, handle := newTestPlugin(t, map[string]graph.Route{
		"payments": {TargetCluster: "payments", NextHopLocalCluster: "payments"},
	})
	handle.EXPECT().ClearRouteCache()
	handle.EXPECT().SetMetadata(dynMetadataNamespace, "next_hop_cluster", "payments")
	handle.EXPECT().SetMetadata(dynMetadataNamespace, "target_cluster", "payments")
	handle.EXPECT().SetMetadata(dynMetadataNamespace, "distance", int64(0))

	hdrs := fake.NewFakeHeaderMap(map[string][]string{"x-target-cluster": {"payments"}})
	status := p.OnRequestHeaders(hdrs, true)
	require.Equal(t, shared.HeadersStatusContinue, status)
}

func TestPlugin_StripsClientSuppliedNextHopHeader_OnLookupMiss(t *testing.T) {
	p, handle := newTestPlugin(t, map[string]graph.Route{})
	handle.EXPECT().SendLocalResponse(uint32(503), gomock.Any(), gomock.Any(), gomock.Any())

	hdrs := fake.NewFakeHeaderMap(map[string][]string{
		"x-target-cluster": {"unknown"},
		"x-next-hop":       {"forged-by-client"},
	})
	_ = p.OnRequestHeaders(hdrs, true)
	require.Empty(t, hdrs.GetOne("x-next-hop").ToUnsafeString())
}

func TestPlugin_StripsClientSuppliedNextHopHeader_OnPassThrough(t *testing.T) {
	p, _ := newTestPlugin(t, map[string]graph.Route{})

	hdrs := fake.NewFakeHeaderMap(map[string][]string{
		"x-next-hop": {"forged-by-client"},
	})
	status := p.OnRequestHeaders(hdrs, true)
	require.Equal(t, shared.HeadersStatusContinue, status)
	require.Empty(t, hdrs.GetOne("x-next-hop").ToUnsafeString())
}

func TestPlugin_ClientSuppliedNextHopOverwrittenOnHit(t *testing.T) {
	p, handle := newTestPlugin(t, map[string]graph.Route{
		"remote_svc": {TargetCluster: "remote_svc", NextHopLocalCluster: "peer_envoy_b"},
	})
	handle.EXPECT().ClearRouteCache()
	handle.EXPECT().SetMetadata(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	hdrs := fake.NewFakeHeaderMap(map[string][]string{
		"x-target-cluster": {"remote_svc"},
		"x-next-hop":       {"forged-by-client"},
	})
	_ = p.OnRequestHeaders(hdrs, true)
	require.Equal(t, "peer_envoy_b", hdrs.GetOne("x-next-hop").ToUnsafeString())
}

func TestParseConfig_Defaults(t *testing.T) {
	c, err := parseConfig([]byte(`{
		"envoy_id":"envoyA",
		"advertise_listen":"0.0.0.0:7000"
	}`))
	require.NoError(t, err)
	require.Equal(t, targetSourceHeader, c.TargetClusterSource)
	require.Equal(t, "x-target-cluster", c.TargetClusterHeader)
	require.Equal(t, "x-next-hop", c.NextHopHeader)
}

func TestParseConfig_RejectsPeerSelfReference(t *testing.T) {
	_, err := parseConfig([]byte(`{
		"envoy_id":"envoyA",
				"advertise_listen":"0.0.0.0:0",
		"peers":[{"id":"envoyA","endpoint":"http://x","local_cluster":"peer_a"}]
	}`))
	require.ErrorContains(t, err, "must not equal envoy_id")
}

func TestParseConfig_RejectsUnknownField(t *testing.T) {
	_, err := parseConfig([]byte(`{"envoy_id":"a","advertise_listen":"0.0.0.0:0","bogus":1}`))
	require.ErrorContains(t, err, "unknown field")
}

func TestParseConfig_RejectsEmpty(t *testing.T) {
	_, err := parseConfig(nil)
	require.ErrorContains(t, err, "empty config")
}

func TestParseConfig_RejectsMetadataSource(t *testing.T) {
	_, err := parseConfig([]byte(`{
		"envoy_id":"a","advertise_listen":"0.0.0.0:0",
		"target_cluster_source":"metadata"
	}`))
	require.ErrorContains(t, err, "target_cluster_source")
}

func TestParseConfig_RejectsNegativeWeight(t *testing.T) {
	_, err := parseConfig([]byte(`{
		"envoy_id":"a","advertise_listen":"0.0.0.0:0",
		"peers":[{"id":"b","endpoint":"http://b","local_cluster":"peer_b","weight":-1}]
	}`))
	require.ErrorContains(t, err, "weight")
}

func TestParseConfig_RejectsSubFloorPollInterval(t *testing.T) {
	_, err := parseConfig([]byte(`{
		"envoy_id":"a","advertise_listen":"0.0.0.0:0",
		"poll_interval":"1ms"
	}`))
	require.ErrorContains(t, err, "poll_interval")
}

func TestParseConfig_RejectsStaleAfterShorterThanPollInterval(t *testing.T) {
	_, err := parseConfig([]byte(`{
		"envoy_id":"a","advertise_listen":"0.0.0.0:0",
		"poll_interval":"10s","stale_after":"1s"
	}`))
	require.ErrorContains(t, err, "stale_after")
}

func TestWellKnownHttpFilterConfigFactories_ExportsName(t *testing.T) {
	got := WellKnownHttpFilterConfigFactories()
	require.Contains(t, got, ExtensionName)
}

func TestParseConfig_RejectsMissingAdvertiseListen(t *testing.T) {
	_, err := parseConfig([]byte(`{"envoy_id":"a"}`))
	require.ErrorContains(t, err, "advertise_listen")
}

func TestParseConfig_RejectsBadPollInterval(t *testing.T) {
	_, err := parseConfig([]byte(`{"envoy_id":"a","advertise_listen":"0.0.0.0:0","poll_interval":"not-a-duration"}`))
	require.ErrorContains(t, err, "poll_interval")
}

func TestParseConfig_RejectsBadStaleAfter(t *testing.T) {
	_, err := parseConfig([]byte(`{"envoy_id":"a","advertise_listen":"0.0.0.0:0","stale_after":"not-a-duration"}`))
	require.ErrorContains(t, err, "stale_after")
}

func TestParseConfig_RejectsPeerMissingFields(t *testing.T) {
	_, err := parseConfig([]byte(`{
		"envoy_id":"a","advertise_listen":"0.0.0.0:0",
		"peers":[{"id":"b","endpoint":"http://b"}]
	}`))
	require.ErrorContains(t, err, "local_cluster")
}

func TestParseConfig_RejectsDuplicatePeerID(t *testing.T) {
	_, err := parseConfig([]byte(`{
		"envoy_id":"a","advertise_listen":"0.0.0.0:0",
		"peers":[
			{"id":"b","endpoint":"http://b","local_cluster":"peer_b"},
			{"id":"b","endpoint":"http://b","local_cluster":"peer_b2"}
		]
	}`))
	require.ErrorContains(t, err, "duplicate")
}

func TestParseConfig_RejectsEmptyTerminal(t *testing.T) {
	_, err := parseConfig([]byte(`{
		"envoy_id":"a","advertise_listen":"0.0.0.0:0",
		"terminals":[""]
	}`))
	require.ErrorContains(t, err, "terminals[0]")
}

func TestParseConfig_RejectsDuplicateTerminal(t *testing.T) {
	_, err := parseConfig([]byte(`{
		"envoy_id":"a","advertise_listen":"0.0.0.0:0",
		"terminals":["x","x"]
	}`))
	require.ErrorContains(t, err, "duplicate")
}

func TestParseConfig_RejectsTerminalCollidingWithPeerLocalCluster(t *testing.T) {
	_, err := parseConfig([]byte(`{
		"envoy_id":"a","advertise_listen":"0.0.0.0:0",
		"peers":[{"id":"b","endpoint":"http://b","local_cluster":"shared"}],
		"terminals":["shared"]
	}`))
	require.ErrorContains(t, err, "peer local_cluster")
}

func TestPluginFactory_CreateReturnsPluginBoundToHandle(t *testing.T) {
	ctrl := gomock.NewController(t)
	handle := mocks.NewMockHttpFilterHandle(ctrl)
	tbl := graph.NewAtomicTable("envoyA")
	d := &graph.Daemon{Table: tbl}

	f := &PluginFactory{cfg: &Config{}, daemon: d}
	filter := f.Create(handle)
	require.NotNil(t, filter)
	p, ok := filter.(*Plugin)
	require.True(t, ok)
	require.Equal(t, handle, p.handle)
	require.Equal(t, tbl, p.table)
}

func TestPluginFactory_OnDestroyNilDaemonIsSafe(t *testing.T) {
	f := &PluginFactory{}
	require.NotPanics(t, f.OnDestroy)
}

func TestPluginConfigFactory_CreatePerRouteIsNoOp(t *testing.T) {
	f := &PluginConfigFactory{}
	v, err := f.CreatePerRoute([]byte(`{"any":"thing"}`))
	require.NoError(t, err)
	require.Nil(t, v)
}

func TestPluginConfigFactory_CreateRejectsBadConfig(t *testing.T) {
	ctrl := gomock.NewController(t)
	handle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	handle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	f := &PluginConfigFactory{}
	got, err := f.Create(handle, []byte(`{"envoy_id":""}`))
	require.Error(t, err)
	require.Nil(t, got)
}

func TestPluginConfigFactory_CreateHappyPath(t *testing.T) {
	ctrl := gomock.NewController(t)
	handle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	handle.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	f := &PluginConfigFactory{}
	got, err := f.Create(handle, []byte(`{
		"envoy_id":"envoyA",
		"advertise_listen":"127.0.0.1:0",
		"poll_interval":"1s",
		"stale_after":"5s"
	}`))
	require.NoError(t, err)
	require.NotNil(t, got)

	pf, ok := got.(*PluginFactory)
	require.True(t, ok)
	require.NotNil(t, pf.daemon)

	// OnDestroy must stop the daemon cleanly.
	pf.OnDestroy()
}
