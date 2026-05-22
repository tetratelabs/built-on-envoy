// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package graph

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDaemon_TwoEnvoyConvergence(t *testing.T) {
	// envoyB hosts a "remote_svc" terminal and serves /advertisements.
	tblB := NewAtomicTable("envoyB")
	tblB.Store(&Table{
		EnvoyID:       "envoyB",
		LocalClusters: map[string]LocalCluster{"remote_svc": {Name: "remote_svc", Role: RoleTerminal}},
		Routes: map[string]Route{
			"remote_svc": {TargetCluster: "remote_svc", NextHopLocalCluster: "remote_svc"},
		},
	})
	bServer, err := NewAdvertisementServer("envoyB", "127.0.0.1:0", tblB)
	require.NoError(t, err)
	bServer.Start()
	defer func() { _ = bServer.Stop(context.Background()) }()

	// envoyA's admin lists peer_envoy_b and a local terminal.
	adminA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"configs":[
			{"cluster":{"name":"peer_envoy_b"}},
			{"cluster":{"name":"local_a"}}
		]}`))
	}))
	defer adminA.Close()

	d := NewDaemon(&DaemonConfig{
		EnvoyID:         "envoyA",
		EnvoyAdminURL:   adminA.URL,
		AdvertiseListen: "127.0.0.1:0",
		Peers: []PeerSpec{{
			ID: "envoyB", Endpoint: "http://" + bServer.Addr(), LocalCluster: "peer_envoy_b", Weight: 10,
		}},
		PollInterval: 20 * time.Millisecond,
		StaleAfter:   1 * time.Second,
	})
	require.NoError(t, d.Start(context.Background()))
	defer d.Stop()

	require.Eventually(t, func() bool {
		r, ok := d.Table.Load().Lookup("remote_svc")
		return ok && r.NextHopLocalCluster == "peer_envoy_b" && r.Distance == 10
	}, 2*time.Second, 20*time.Millisecond)

	r, _ := d.Table.Load().Lookup("local_a")
	require.Equal(t, "local_a", r.NextHopLocalCluster)
}

func TestDaemon_StalePeerRoutesExpire(t *testing.T) {
	// Peer that returns success once then 500s forever.
	var calls int
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			_ = json.NewEncoder(w).Encode(Advertisement{
				EnvoyID: "envoyB",
				Routes:  []Route{{TargetCluster: "remote_svc"}},
			})
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer peer.Close()

	adminA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"configs":[{"cluster":{"name":"peer_envoy_b"}}]}`))
	}))
	defer adminA.Close()

	d := NewDaemon(&DaemonConfig{
		EnvoyID:         "envoyA",
		EnvoyAdminURL:   adminA.URL,
		AdvertiseListen: "127.0.0.1:0",
		Peers: []PeerSpec{{
			ID: "envoyB", Endpoint: peer.URL, LocalCluster: "peer_envoy_b", Weight: 10,
		}},
		PollInterval: 20 * time.Millisecond,
		StaleAfter:   100 * time.Millisecond,
	})
	require.NoError(t, d.Start(context.Background()))
	defer d.Stop()

	require.Eventually(t, func() bool {
		_, ok := d.Table.Load().Lookup("remote_svc")
		return ok
	}, 1*time.Second, 20*time.Millisecond)

	require.Eventually(t, func() bool {
		_, ok := d.Table.Load().Lookup("remote_svc")
		return !ok
	}, 2*time.Second, 50*time.Millisecond)
}

func TestDaemon_PollsPeersInParallel(t *testing.T) {
	ready := make(chan string, 2)
	release := make(chan struct{})
	handler := func(id string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			ready <- id
			<-release
			_ = json.NewEncoder(w).Encode(Advertisement{EnvoyID: id, Routes: nil})
		})
	}
	pb := httptest.NewServer(handler("envoyB"))
	pc := httptest.NewServer(handler("envoyC"))
	defer pb.Close()
	defer pc.Close()

	adminA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"configs":[
			{"cluster":{"name":"peer_envoy_b"}},
			{"cluster":{"name":"peer_envoy_c"}}
		]}`))
	}))
	defer adminA.Close()

	d := NewDaemon(&DaemonConfig{
		EnvoyID:         "envoyA",
		EnvoyAdminURL:   adminA.URL,
		AdvertiseListen: "127.0.0.1:0",
		Peers: []PeerSpec{
			{ID: "envoyB", Endpoint: pb.URL, LocalCluster: "peer_envoy_b", Weight: 10},
			{ID: "envoyC", Endpoint: pc.URL, LocalCluster: "peer_envoy_c", Weight: 10},
		},
		PollInterval: 1 * time.Second,
		StaleAfter:   10 * time.Second,
	})
	require.NoError(t, d.Start(context.Background()))
	defer func() {
		close(release)
		d.Stop()
	}()

	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case id := <-ready:
			seen[id] = true
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d of 2 peers were polled concurrently; polling looks serial", len(seen))
		}
	}
	require.True(t, seen["envoyB"] && seen["envoyC"], "both peers must be in-flight together")
}
