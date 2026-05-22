// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package graph

import "sort"

// MaxASPath caps the AS-PATH length we accept from a peer.
const MaxASPath = 32

// MaxDistance caps the advertised cost we accept from a peer. Pinned to
// MaxASPath: a sane mesh shouldn't need distances larger than the hop bound.
const MaxDistance = MaxASPath

// PeerAdvertisement pairs an inbound advertisement with the local PeerSpec it arrived through.
type PeerAdvertisement struct {
	Peer PeerSpec
	Adv  Advertisement
}

// Compute produces a fresh route map from local clusters and peer
// advertisements. Best-path order: lower distance, then shorter AS-PATH,
// then lexicographically smaller next-hop peer id. Local terminals always
// beat peer-learned candidates for the same target.
func Compute(localEnvoyID string, locals map[string]LocalCluster, advs []PeerAdvertisement) map[string]Route {
	out := map[string]Route{}

	for name, c := range locals {
		if c.Role == RoleTerminal {
			out[name] = Route{TargetCluster: name, NextHopLocalCluster: name}
		}
	}

	sorted := make([]PeerAdvertisement, len(advs))
	copy(sorted, advs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Peer.ID < sorted[j].Peer.ID })

	for _, pa := range sorted {
		if _, ok := locals[pa.Peer.LocalCluster]; !ok {
			continue
		}
		seenInAdv := map[string]bool{}
		for _, r := range pa.Adv.Routes {
			if !validAdvertisedRoute(r) {
				continue
			}
			if seenInAdv[r.TargetCluster] {
				continue
			}
			seenInAdv[r.TargetCluster] = true
			if hasInPath(r.ASPath, localEnvoyID) || hasInPath(r.ASPath, pa.Peer.ID) {
				continue
			}
			candidate := Route{
				TargetCluster:       r.TargetCluster,
				NextHopLocalCluster: pa.Peer.LocalCluster,
				Distance:            pa.Peer.Weight + r.Distance,
				ASPath:              prepend(pa.Peer.ID, r.ASPath),
			}
			existing, ok := out[r.TargetCluster]
			if !ok {
				out[r.TargetCluster] = candidate
				continue
			}
			if len(existing.ASPath) == 0 {
				continue
			}
			if better(candidate, existing) {
				out[r.TargetCluster] = candidate
			}
		}
	}

	return out
}

func validAdvertisedRoute(r Route) bool {
	if r.TargetCluster == "" {
		return false
	}
	if r.Distance < 0 || r.Distance > MaxDistance {
		return false
	}
	if len(r.ASPath) > MaxASPath {
		return false
	}
	return true
}

// AdvertiseTo applies split horizon: routes whose AS-PATH contains peerID
// are withheld from that peer.
func AdvertiseTo(t *Table, peerID string) []Route {
	if t == nil {
		return nil
	}
	out := make([]Route, 0, len(t.Routes))
	for _, r := range t.Routes {
		if hasInPath(r.ASPath, peerID) {
			continue
		}
		r.ASPath = append([]string(nil), r.ASPath...)
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TargetCluster < out[j].TargetCluster })
	return out
}

func better(a, b Route) bool {
	if a.Distance != b.Distance {
		return a.Distance < b.Distance
	}
	if len(a.ASPath) != len(b.ASPath) {
		return len(a.ASPath) < len(b.ASPath)
	}
	var ah, bh string
	if len(a.ASPath) > 0 {
		ah = a.ASPath[0]
	}
	if len(b.ASPath) > 0 {
		bh = b.ASPath[0]
	}
	return ah < bh
}

func hasInPath(path []string, id string) bool {
	for _, p := range path {
		if p == id {
			return true
		}
	}
	return false
}

func prepend(id string, path []string) []string {
	out := make([]string, 0, len(path)+1)
	out = append(out, id)
	out = append(out, path...)
	return out
}
