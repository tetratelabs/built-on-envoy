// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package graph

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdvertisementServer_RoundTrip(t *testing.T) {
	tbl := NewAtomicTable("envoyB")
	tbl.Store(&Table{
		EnvoyID: "envoyB",
		Routes: map[string]Route{
			"svc_local": {TargetCluster: "svc_local", NextHopLocalCluster: "svc_local"},
			"svc_loop":  {TargetCluster: "svc_loop", NextHopLocalCluster: "peer_a", ASPath: []string{"envoyA"}},
		},
	})

	s, err := NewAdvertisementServer("envoyB", "127.0.0.1:0", tbl)
	require.NoError(t, err)
	s.Start()
	defer func() {
		_ = s.Stop(context.Background())
	}()

	adv, err := FetchAdvertisement(context.Background(), http.DefaultClient, "http://"+s.Addr(), "envoyA")
	require.NoError(t, err)
	require.Equal(t, "envoyB", adv.EnvoyID)
	require.Len(t, adv.Routes, 1)
	require.Equal(t, "svc_local", adv.Routes[0].TargetCluster)
}

func TestFetchAdvertisement_PeerError(t *testing.T) {
	_, err := FetchAdvertisement(context.Background(), http.DefaultClient, "http://127.0.0.1:1", "envoyA")
	require.Error(t, err)
}

func TestFetchAdvertisement_MalformedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{not json`))
	}))
	defer srv.Close()
	_, err := FetchAdvertisement(context.Background(), http.DefaultClient, srv.URL, "envoyA")
	require.Error(t, err)
}

func TestFetchAdvertisement_OversizedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Write a JSON document larger than maxResponseBytes by stuffing a
		// huge string into the envoy_id field. LimitReader truncates and
		// json.Decode fails on the resulting unexpected EOF.
		oversized := strings.Repeat("x", maxResponseBytes+1)
		_, _ = w.Write([]byte(`{"envoy_id":"` + oversized + `","routes":[]}`))
	}))
	defer srv.Close()
	_, err := FetchAdvertisement(context.Background(), http.DefaultClient, srv.URL, "envoyA")
	require.Error(t, err)
}

func TestAdvertisementServer_RejectsNonGET(t *testing.T) {
	tbl := NewAtomicTable("envoyA")
	s, err := NewAdvertisementServer("envoyA", "127.0.0.1:0", tbl)
	require.NoError(t, err)
	s.Start()
	defer func() { _ = s.Stop(context.Background()) }()

	resp, err := http.Post("http://"+s.Addr()+"/advertisements", "application/json", strings.NewReader("{}"))
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

func TestAdvertisementServer_StopHaltsServing(t *testing.T) {
	tbl := NewAtomicTable("envoyA")
	s, err := NewAdvertisementServer("envoyA", "127.0.0.1:0", tbl)
	require.NoError(t, err)
	s.Start()
	addr := s.Addr()

	// Sanity: server responds before Stop.
	_, err = FetchAdvertisement(context.Background(), http.DefaultClient, "http://"+addr, "tester")
	require.NoError(t, err)

	require.NoError(t, s.Stop(context.Background()))

	// After Stop, requests must fail.
	_, err = FetchAdvertisement(context.Background(), http.DefaultClient, "http://"+addr, "tester")
	require.Error(t, err)
}
