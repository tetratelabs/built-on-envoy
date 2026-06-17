// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package graph builds and maintains the cluster routing table.
package graph

// PeerSpec describes an outbound peer this Envoy pulls advertisements from.
type PeerSpec struct {
	ID           string `json:"id"`
	Endpoint     string `json:"endpoint"`
	LocalCluster string `json:"local_cluster"`
}

// ClusterRole tags a local Envoy cluster for the routing algorithm.
type ClusterRole string

// Recognized cluster roles.
const (
	RoleTerminal ClusterRole = "terminal"
	RolePeer     ClusterRole = "peer"
	RoleIgnore   ClusterRole = "ignore"
)

// LocalCluster is one cluster from the local Envoy's admin output.
type LocalCluster struct {
	Name   string      `json:"name"`
	Role   ClusterRole `json:"role"`
	PeerID string      `json:"peer_id,omitempty"`
}

// Advertisement is the body of GET /advertisements.
type Advertisement struct {
	EnvoyID string  `json:"envoy_id"`
	Routes  []Route `json:"routes"`
}

// Route is one row of the routing table as it flows on the wire and as the
// handler sees it. On the wire NextHopLocalCluster is the advertiser's local
// cluster; the receiver rewrites it to its own local cluster pointing at the
// advertising peer before storing.
type Route struct {
	TargetCluster       string   `json:"target_cluster"`
	NextHopLocalCluster string   `json:"next_hop_local_cluster"`
	Distance            int      `json:"distance"`
	ASPath              []string `json:"as_path"`
}
