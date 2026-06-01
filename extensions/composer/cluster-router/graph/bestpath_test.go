// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package graph

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompute_LocalTerminalsBecomeRoutes(t *testing.T) {
	locals := map[string]LocalCluster{
		"payments":  {Name: "payments", Role: RoleTerminal},
		"peer_b":    {Name: "peer_b", Role: RolePeer, PeerID: "envoyB"},
		"authz_svc": {Name: "authz_svc", Role: RoleIgnore},
	}
	routes := Compute("envoyA", locals, nil)
	require.Contains(t, routes, "payments")
	require.Equal(t, "payments", routes["payments"].NextHopLocalCluster)
	require.Zero(t, routes["payments"].Distance)
	require.NotContains(t, routes, "peer_b")
	require.NotContains(t, routes, "authz_svc")
}

func TestCompute_LearnsFromPeer(t *testing.T) {
	locals := map[string]LocalCluster{
		"peer_b": {Name: "peer_b", Role: RolePeer, PeerID: "envoyB"},
	}
	advs := []PeerAdvertisement{{
		Peer: PeerSpec{ID: "envoyB", LocalCluster: "peer_b", Weight: 10},
		Adv: Advertisement{
			EnvoyID: "envoyB",
			Routes: []Route{
				{TargetCluster: "remote_svc", NextHopLocalCluster: "remote_svc", Distance: 0},
			},
		},
	}}
	routes := Compute("envoyA", locals, advs)
	r := routes["remote_svc"]
	require.Equal(t, "peer_b", r.NextHopLocalCluster)
	require.Equal(t, 10, r.Distance)
	require.Equal(t, []string{"envoyB"}, r.ASPath)
}

func TestCompute_TransitiveTwoHops(t *testing.T) {
	locals := map[string]LocalCluster{
		"peer_b": {Name: "peer_b", Role: RolePeer, PeerID: "envoyB"},
	}
	advs := []PeerAdvertisement{{
		Peer: PeerSpec{ID: "envoyB", LocalCluster: "peer_b", Weight: 10},
		Adv: Advertisement{
			EnvoyID: "envoyB",
			Routes: []Route{
				{TargetCluster: "far_svc", NextHopLocalCluster: "peer_c", Distance: 7, ASPath: []string{"envoyC"}},
			},
		},
	}}
	routes := Compute("envoyA", locals, advs)
	r := routes["far_svc"]
	require.Equal(t, "peer_b", r.NextHopLocalCluster)
	require.Equal(t, 17, r.Distance)
	require.Equal(t, []string{"envoyB", "envoyC"}, r.ASPath)
}

func TestCompute_DropsLoopedAdvertisement(t *testing.T) {
	locals := map[string]LocalCluster{
		"peer_b": {Name: "peer_b", Role: RolePeer, PeerID: "envoyB"},
	}
	advs := []PeerAdvertisement{{
		Peer: PeerSpec{ID: "envoyB", LocalCluster: "peer_b", Weight: 10},
		Adv: Advertisement{
			EnvoyID: "envoyB",
			Routes: []Route{
				{TargetCluster: "svc", NextHopLocalCluster: "peer_a", Distance: 1, ASPath: []string{"envoyA", "envoyZ"}},
			},
		},
	}}
	routes := Compute("envoyA", locals, advs)
	require.NotContains(t, routes, "svc")
}

func TestCompute_LocalTerminalBeatsPeerAdvertisement(t *testing.T) {
	locals := map[string]LocalCluster{
		"payments": {Name: "payments", Role: RoleTerminal},
		"peer_b":   {Name: "peer_b", Role: RolePeer, PeerID: "envoyB"},
	}
	advs := []PeerAdvertisement{{
		Peer: PeerSpec{ID: "envoyB", LocalCluster: "peer_b", Weight: 1},
		Adv: Advertisement{
			EnvoyID: "envoyB",
			Routes:  []Route{{TargetCluster: "payments", NextHopLocalCluster: "payments", Distance: 0}},
		},
	}}
	routes := Compute("envoyA", locals, advs)
	require.Equal(t, "payments", routes["payments"].NextHopLocalCluster)
	require.Empty(t, routes["payments"].ASPath)
}

func TestCompute_PicksLowerDistance(t *testing.T) {
	locals := map[string]LocalCluster{
		"peer_b": {Name: "peer_b", Role: RolePeer, PeerID: "envoyB"},
		"peer_c": {Name: "peer_c", Role: RolePeer, PeerID: "envoyC"},
	}
	advs := []PeerAdvertisement{
		{
			Peer: PeerSpec{ID: "envoyB", LocalCluster: "peer_b", Weight: 100},
			Adv:  Advertisement{Routes: []Route{{TargetCluster: "svc", Distance: 0}}},
		},
		{
			Peer: PeerSpec{ID: "envoyC", LocalCluster: "peer_c", Weight: 1},
			Adv:  Advertisement{Routes: []Route{{TargetCluster: "svc", Distance: 0}}},
		},
	}
	routes := Compute("envoyA", locals, advs)
	require.Equal(t, "peer_c", routes["svc"].NextHopLocalCluster)
}

func TestCompute_TieBreakOnPeerID(t *testing.T) {
	locals := map[string]LocalCluster{
		"peer_b": {Name: "peer_b", Role: RolePeer, PeerID: "envoyB"},
		"peer_c": {Name: "peer_c", Role: RolePeer, PeerID: "envoyC"},
	}
	advs := []PeerAdvertisement{
		{
			Peer: PeerSpec{ID: "envoyC", LocalCluster: "peer_c", Weight: 10},
			Adv:  Advertisement{Routes: []Route{{TargetCluster: "svc", Distance: 0}}},
		},
		{
			Peer: PeerSpec{ID: "envoyB", LocalCluster: "peer_b", Weight: 10},
			Adv:  Advertisement{Routes: []Route{{TargetCluster: "svc", Distance: 0}}},
		},
	}
	routes := Compute("envoyA", locals, advs)
	require.Equal(t, "peer_b", routes["svc"].NextHopLocalCluster)
}

func TestCompute_DropsCandidateWhenLocalClusterMissing(t *testing.T) {
	locals := map[string]LocalCluster{}
	advs := []PeerAdvertisement{{
		Peer: PeerSpec{ID: "envoyB", LocalCluster: "peer_b", Weight: 10},
		Adv:  Advertisement{Routes: []Route{{TargetCluster: "svc", Distance: 0}}},
	}}
	require.Empty(t, Compute("envoyA", locals, advs))
}

func TestAdvertiseTo_SplitHorizon(t *testing.T) {
	tbl := &Table{
		Routes: map[string]Route{
			"local_svc":  {TargetCluster: "local_svc", NextHopLocalCluster: "local_svc"},
			"via_b_svc":  {TargetCluster: "via_b_svc", NextHopLocalCluster: "peer_b", ASPath: []string{"envoyB"}},
			"via_bc_svc": {TargetCluster: "via_bc_svc", NextHopLocalCluster: "peer_b", ASPath: []string{"envoyB", "envoyC"}},
		},
	}
	got := AdvertiseTo(tbl, "envoyB")
	require.Len(t, got, 1)
	require.Equal(t, "local_svc", got[0].TargetCluster)
}

func TestAdvertiseTo_NilTable(t *testing.T) {
	require.Nil(t, AdvertiseTo(nil, "x"))
}

func TestCompute_DropsEmptyTargetCluster(t *testing.T) {
	locals := map[string]LocalCluster{
		"peer_b": {Name: "peer_b", Role: RolePeer, PeerID: "envoyB"},
	}
	advs := []PeerAdvertisement{{
		Peer: PeerSpec{ID: "envoyB", LocalCluster: "peer_b", Weight: 10},
		Adv: Advertisement{Routes: []Route{
			{TargetCluster: "", Distance: 0},
			{TargetCluster: "valid", Distance: 0},
		}},
	}}
	routes := Compute("envoyA", locals, advs)
	require.NotContains(t, routes, "")
	require.Contains(t, routes, "valid")
}

func TestCompute_DropsNegativeDistance(t *testing.T) {
	locals := map[string]LocalCluster{
		"peer_b": {Name: "peer_b", Role: RolePeer, PeerID: "envoyB"},
	}
	advs := []PeerAdvertisement{{
		Peer: PeerSpec{ID: "envoyB", LocalCluster: "peer_b", Weight: 10},
		Adv:  Advertisement{Routes: []Route{{TargetCluster: "svc", Distance: -1}}},
	}}
	require.Empty(t, Compute("envoyA", locals, advs))
}

func TestCompute_DropsOversizeASPath(t *testing.T) {
	locals := map[string]LocalCluster{
		"peer_b": {Name: "peer_b", Role: RolePeer, PeerID: "envoyB"},
	}
	overlong := make([]string, MaxASPath+1)
	for i := range overlong {
		overlong[i] = "x"
	}
	advs := []PeerAdvertisement{{
		Peer: PeerSpec{ID: "envoyB", LocalCluster: "peer_b", Weight: 10},
		Adv:  Advertisement{Routes: []Route{{TargetCluster: "svc", Distance: 0, ASPath: overlong}}},
	}}
	require.Empty(t, Compute("envoyA", locals, advs))
}

func TestCompute_DedupesWithinAdvertisement(t *testing.T) {
	locals := map[string]LocalCluster{
		"peer_b": {Name: "peer_b", Role: RolePeer, PeerID: "envoyB"},
	}
	advs := []PeerAdvertisement{{
		Peer: PeerSpec{ID: "envoyB", LocalCluster: "peer_b", Weight: 10},
		Adv: Advertisement{Routes: []Route{
			{TargetCluster: "svc", Distance: 0},
			{TargetCluster: "svc", Distance: 999, ASPath: []string{"evil"}},
		}},
	}}
	routes := Compute("envoyA", locals, advs)
	require.Equal(t, 10, routes["svc"].Distance)
}

func TestCompute_TieBreakOnIdenticalDistanceAndPath(t *testing.T) {
	locals := map[string]LocalCluster{
		"peer_b": {Name: "peer_b", Role: RolePeer, PeerID: "envoyB"},
		"peer_c": {Name: "peer_c", Role: RolePeer, PeerID: "envoyC"},
	}
	// Same weight, both report distance 0 with empty ASPath: candidates have
	// identical distance (10) and AS-PATH length (1). Tie-break must be
	// deterministic on the first AS-PATH element (peer id).
	advs := []PeerAdvertisement{
		{
			Peer: PeerSpec{ID: "envoyC", LocalCluster: "peer_c", Weight: 10},
			Adv:  Advertisement{Routes: []Route{{TargetCluster: "svc", Distance: 0}}},
		},
		{
			Peer: PeerSpec{ID: "envoyB", LocalCluster: "peer_b", Weight: 10},
			Adv:  Advertisement{Routes: []Route{{TargetCluster: "svc", Distance: 0}}},
		},
	}
	routes := Compute("envoyA", locals, advs)
	require.Equal(t, "peer_b", routes["svc"].NextHopLocalCluster)
}

func TestCompute_DropsRouteWhenDirectPeerIDInASPath(t *testing.T) {
	locals := map[string]LocalCluster{
		"peer_b": {Name: "peer_b", Role: RolePeer, PeerID: "envoyB"},
	}
	// envoyB advertising a route whose AS-PATH already contains envoyB is
	// malformed; drop it instead of double-counting.
	advs := []PeerAdvertisement{{
		Peer: PeerSpec{ID: "envoyB", LocalCluster: "peer_b", Weight: 10},
		Adv:  Advertisement{Routes: []Route{{TargetCluster: "svc", ASPath: []string{"envoyB", "envoyZ"}}}},
	}}
	require.Empty(t, Compute("envoyA", locals, advs))
}
