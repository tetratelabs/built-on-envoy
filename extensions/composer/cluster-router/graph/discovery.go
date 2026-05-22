// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
)

// maxResponseBytes caps a single admin or peer JSON response.
const maxResponseBytes = 8 << 20 // 8 MiB

type clusterEntry struct {
	Cluster struct {
		Name     string `json:"name"`
		Metadata struct {
			FilterMetadata map[string]map[string]any `json:"filter_metadata"`
		} `json:"metadata"`
	} `json:"cluster"`
}

type configDump struct {
	Configs []clusterEntry `json:"configs"`
}

// DiscoverLocal classifies clusters from the Envoy admin /config_dump.
// Static and dynamic clusters live under different resources, so both are polled and the
// union is returned.
func DiscoverLocal(ctx context.Context, adminURL string, peers []PeerSpec, httpClient *http.Client) (map[string]LocalCluster, error) {
	peerByLocal := map[string]string{}
	for _, p := range peers {
		peerByLocal[p.LocalCluster] = p.ID
	}

	out := map[string]LocalCluster{}
	resources := []string{"static_clusters", "dynamic_active_clusters"}
	errs := make([]error, len(resources))
	successes := 0
	for i, resource := range resources {
		entries, err := fetchClusters(ctx, httpClient, adminURL, resource)
		if err != nil {
			errs[i] = err
			log.Printf("cluster-router: admin %s fetch failed: %v", resource, err)
			continue
		}
		successes++
		for _, c := range entries {
			name := c.Cluster.Name
			if name == "" {
				continue
			}
			role := roleFromMetadata(c.Cluster.Metadata.FilterMetadata)
			peerID := ""
			switch role {
			case RoleIgnore:
				continue
			case "":
				if pid, ok := peerByLocal[name]; ok {
					role = RolePeer
					peerID = pid
				} else {
					role = RoleTerminal
				}
			case RolePeer:
				peerID = peerByLocal[name]
			}
			out[name] = LocalCluster{Name: name, Role: role, PeerID: peerID}
		}
	}
	if successes == 0 {
		return nil, fmt.Errorf("all admin fetches failed: %v", errs)
	}
	return out, nil
}

func fetchClusters(ctx context.Context, client *http.Client, adminURL, resource string) ([]clusterEntry, error) {
	u, err := url.Parse(adminURL)
	if err != nil {
		return nil, fmt.Errorf("parse admin url: %w", err)
	}
	u.Path = "/config_dump"
	q := u.Query()
	q.Set("resource", resource)
	q.Set("include_eds", "false")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("admin request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("admin status %d", resp.StatusCode)
	}
	var dump configDump
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&dump); err != nil {
		return nil, fmt.Errorf("decode admin: %w", err)
	}
	return dump.Configs, nil
}

func roleFromMetadata(fm map[string]map[string]any) ClusterRole {
	ns, ok := fm["boe.cluster_router"]
	if !ok {
		return ""
	}
	v, ok := ns["role"].(string)
	if !ok {
		return ""
	}
	switch ClusterRole(v) {
	case RoleTerminal, RolePeer, RoleIgnore:
		return ClusterRole(v)
	}
	return ""
}
