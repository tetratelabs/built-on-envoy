// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package graph

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

const dynamicClustersDump = `{
  "configs": [
    {"cluster": {"name": "payments"}},
    {"cluster": {"name": "peer_envoy_b"}},
    {"cluster": {"name": "authz_svc", "metadata": {"filter_metadata": {"boe.cluster_router": {"role": "ignore"}}}}},
    {"cluster": {"name": "explicit_terminal", "metadata": {"filter_metadata": {"boe.cluster_router": {"role": "terminal"}}}}}
  ]
}`

const staticClustersDump = `{
  "configs": [
    {"cluster": {"name": "static_terminal"}},
    {"cluster": {"name": "peer_envoy_b", "metadata": {"filter_metadata": {"boe.cluster_router": {"role": "peer", "peer_id": "envoyB"}}}}}
  ]
}`

func newAdminServer(t *testing.T, byResource map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/config_dump", r.URL.Path)
		res := r.URL.Query().Get("resource")
		body, ok := byResource[res]
		if !ok {
			body = `{"configs":[]}`
		}
		_, _ = w.Write([]byte(body))
	}))
}

func TestDiscoverLocal_MergesStaticAndDynamic(t *testing.T) {
	srv := newAdminServer(t, map[string]string{
		"static_clusters":         staticClustersDump,
		"dynamic_active_clusters": dynamicClustersDump,
	})
	defer srv.Close()

	peers := []PeerSpec{{ID: "envoyB", Endpoint: "http://b", LocalCluster: "peer_envoy_b", Weight: 10}}
	locals, err := DiscoverLocal(context.Background(), srv.URL, peers, http.DefaultClient)
	require.NoError(t, err)

	require.Equal(t, RoleTerminal, locals["payments"].Role)
	require.Equal(t, RoleTerminal, locals["explicit_terminal"].Role)
	require.Equal(t, RoleTerminal, locals["static_terminal"].Role)
	require.Equal(t, RolePeer, locals["peer_envoy_b"].Role)
	require.Equal(t, "envoyB", locals["peer_envoy_b"].PeerID)
	require.NotContains(t, locals, "authz_svc")
}

func TestDiscoverLocal_StaticOnly(t *testing.T) {
	srv := newAdminServer(t, map[string]string{
		"static_clusters": staticClustersDump,
	})
	defer srv.Close()

	locals, err := DiscoverLocal(context.Background(), srv.URL, nil, http.DefaultClient)
	require.NoError(t, err)
	require.Contains(t, locals, "static_terminal")
	require.Contains(t, locals, "peer_envoy_b")
}

func TestDiscoverLocal_DynamicOnly(t *testing.T) {
	srv := newAdminServer(t, map[string]string{
		"dynamic_active_clusters": dynamicClustersDump,
	})
	defer srv.Close()

	locals, err := DiscoverLocal(context.Background(), srv.URL, nil, http.DefaultClient)
	require.NoError(t, err)
	require.Contains(t, locals, "payments")
	require.Contains(t, locals, "explicit_terminal")
}

func TestDiscoverLocal_AdminError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	_, err := DiscoverLocal(context.Background(), srv.URL, nil, http.DefaultClient)
	require.Error(t, err)
}
