// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package clusterrouter is a composer plugin that performs BGP-style next-hop
// resolution against an in-process routing table that is maintained in the
// background.
package clusterrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/cluster-router/graph"
)

// ExtensionName is the filter name composer registers this plugin under.
const ExtensionName = "cluster-router"

const dynMetadataNamespace = "boe.cluster_router"

type targetSource string

const (
	targetSourceHeader   targetSource = "header"
	targetSourceMetadata targetSource = "metadata"
)

// Config is the JSON-deserialized plugin configuration.
type Config struct {
	EnvoyID             string           `json:"envoy_id"`
	EnvoyAdminURL       string           `json:"envoy_admin_url"`
	AdvertiseListen     string           `json:"advertise_listen"`
	TargetClusterSource targetSource     `json:"target_cluster_source"`
	TargetClusterHeader string           `json:"target_cluster_header"`
	NextHopHeader       string           `json:"next_hop_header"`
	Peers               []graph.PeerSpec `json:"peers"`
	PollInterval        string           `json:"poll_interval"`
	StaleAfter          string           `json:"stale_after"`

	pollInterval time.Duration
	staleAfter   time.Duration
}

func parseConfig(raw []byte) (*Config, error) {
	var c Config
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty config")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &c, c.validate()
}

func (c *Config) validate() error {
	if c.EnvoyID == "" {
		return fmt.Errorf("envoy_id is required")
	}
	if c.EnvoyAdminURL == "" {
		return fmt.Errorf("envoy_admin_url is required")
	}
	if c.AdvertiseListen == "" {
		return fmt.Errorf("advertise_listen is required")
	}
	if c.TargetClusterSource == "" {
		c.TargetClusterSource = targetSourceHeader
	}
	if c.TargetClusterSource != targetSourceHeader {
		// `metadata` is reserved in the enum but not yet implemented.
		return fmt.Errorf("target_cluster_source must be: header")
	}
	if c.TargetClusterHeader == "" {
		c.TargetClusterHeader = "x-target-cluster"
	}
	if c.NextHopHeader == "" {
		c.NextHopHeader = "x-next-hop"
	}
	if c.PollInterval == "" {
		c.PollInterval = "10s"
	}
	if c.StaleAfter == "" {
		c.StaleAfter = "60s"
	}
	d, err := time.ParseDuration(c.PollInterval)
	if err != nil {
		return fmt.Errorf("poll_interval: %w", err)
	}
	if d < minPollInterval {
		return fmt.Errorf("poll_interval must be >= %s", minPollInterval)
	}
	c.pollInterval = d
	d, err = time.ParseDuration(c.StaleAfter)
	if err != nil {
		return fmt.Errorf("stale_after: %w", err)
	}
	if d < c.pollInterval {
		return fmt.Errorf("stale_after (%s) must be >= poll_interval (%s)", d, c.pollInterval)
	}
	c.staleAfter = d
	seen := map[string]bool{}
	for i, p := range c.Peers {
		if p.ID == "" || p.Endpoint == "" || p.LocalCluster == "" {
			return fmt.Errorf("peers[%d]: id, endpoint, local_cluster required", i)
		}
		if p.ID == c.EnvoyID {
			return fmt.Errorf("peers[%d]: id must not equal envoy_id %q", i, c.EnvoyID)
		}
		if seen[p.ID] {
			return fmt.Errorf("peers[%d]: duplicate id %q", i, p.ID)
		}
		seen[p.ID] = true
		if p.Weight < 0 {
			return fmt.Errorf("peers[%d]: weight must be >= 0", i)
		}
		if p.Weight == 0 {
			c.Peers[i].Weight = 10
		}
	}
	return nil
}

const minPollInterval = 100 * time.Millisecond

// Plugin is the per-stream filter instance.
type Plugin struct {
	shared.EmptyHttpFilter
	cfg    *Config
	table  *graph.AtomicTable
	handle shared.HttpFilterHandle
}

// OnRequestHeaders resolves the target cluster into a next-hop header.
func (p *Plugin) OnRequestHeaders(headers shared.HeaderMap, _ bool) shared.HeadersStatus {
	if headers != nil {
		headers.Remove(p.cfg.NextHopHeader)
	}

	target := p.readTarget(headers)
	if target == "" {
		return shared.HeadersStatusContinue
	}

	route, ok := p.table.Load().Lookup(target)
	if !ok {
		body, _ := json.Marshal(map[string]string{"error": "no_route", "target": target})
		p.handle.SendLocalResponse(503,
			[][2]string{{"content-type", "application/json"}},
			body,
			"cluster-router-no-route",
		)
		return shared.HeadersStatusStop
	}

	headers.Set(p.cfg.NextHopHeader, route.NextHopLocalCluster)
	p.handle.ClearRouteCache()

	p.handle.SetMetadata(dynMetadataNamespace, "next_hop_cluster", route.NextHopLocalCluster)
	p.handle.SetMetadata(dynMetadataNamespace, "target_cluster", route.TargetCluster)
	p.handle.SetMetadata(dynMetadataNamespace, "distance", int64(route.Distance))
	if len(route.ASPath) > 0 {
		p.handle.SetMetadata(dynMetadataNamespace, "as_path", strings.Join(route.ASPath, ","))
	}
	return shared.HeadersStatusContinue
}

func (p *Plugin) readTarget(headers shared.HeaderMap) string {
	if p.cfg.TargetClusterSource != targetSourceHeader || headers == nil {
		return ""
	}
	return headers.GetOne(p.cfg.TargetClusterHeader).ToUnsafeString()
}

// PluginFactory creates Plugin instances and owns the background daemon.
type PluginFactory struct {
	cfg    *Config
	daemon *graph.Daemon
}

// Create returns a new Plugin bound to this stream's handle.
func (f *PluginFactory) Create(handle shared.HttpFilterHandle) shared.HttpFilter {
	return &Plugin{cfg: f.cfg, table: f.daemon.Table, handle: handle}
}

// OnDestroy stops the daemon so the listener and goroutines are released on
// config reload.
func (f *PluginFactory) OnDestroy() {
	if f.daemon != nil {
		f.daemon.Stop()
	}
}

// PluginConfigFactory parses the plugin config and starts the routing daemon.
type PluginConfigFactory struct {
	shared.EmptyHttpFilterConfigFactory
}

// Create parses the config, starts the graph daemon, and returns a PluginFactory.
func (f *PluginConfigFactory) Create(handle shared.HttpFilterConfigHandle, unparsed []byte) (shared.HttpFilterFactory, error) {
	cfg, err := parseConfig(unparsed)
	if err != nil {
		handle.Log(shared.LogLevelError, "cluster-router: %v", err)
		return nil, err
	}

	d := graph.NewDaemon(&graph.DaemonConfig{
		EnvoyID:         cfg.EnvoyID,
		EnvoyAdminURL:   cfg.EnvoyAdminURL,
		AdvertiseListen: cfg.AdvertiseListen,
		Peers:           cfg.Peers,
		PollInterval:    cfg.pollInterval,
		StaleAfter:      cfg.staleAfter,
	})
	if err := d.Start(context.Background()); err != nil {
		handle.Log(shared.LogLevelError, "cluster-router: daemon start: %v", err)
		return nil, err
	}
	handle.Log(shared.LogLevelInfo, "cluster-router: daemon started for envoy_id=%s", cfg.EnvoyID)

	return &PluginFactory{cfg: cfg, daemon: d}, nil
}

// CreatePerRoute is a no-op; the plugin has no per-route config.
func (f *PluginConfigFactory) CreatePerRoute(_ []byte) (any, error) { return nil, nil }

// WellKnownHttpFilterConfigFactories returns the factories this plugin registers with the composer host.
func WellKnownHttpFilterConfigFactories() map[string]shared.HttpFilterConfigFactory { //nolint:revive
	return map[string]shared.HttpFilterConfigFactory{
		ExtensionName: &PluginConfigFactory{},
	}
}
