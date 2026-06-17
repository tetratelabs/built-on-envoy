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
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/cluster-router/graph"
)

// ExtensionName is the filter name composer registers this plugin under.
const ExtensionName = "cluster-router"

const defaultMetadataNamespace = "io.builtonenvoy.cluster_router"

const minPollInterval = 100 * time.Millisecond

type targetSource string

const (
	targetSourceHeader   targetSource = "header"
	targetSourceMetadata targetSource = "metadata"
)

// Config is the JSON-deserialized plugin configuration.
type Config struct {
	EnvoyID             string           `json:"envoy_id"`
	AdvertiseListen     string           `json:"advertise_listen"`
	TargetClusterSource targetSource     `json:"target_cluster_source"`
	TargetClusterHeader string           `json:"target_cluster_header"`
	NextHopHeader       string           `json:"next_hop_header"`
	Peers               []graph.PeerSpec `json:"peers"`
	Terminals           []string         `json:"terminals"`
	PollInterval        string           `json:"poll_interval"`
	StaleAfter          string           `json:"stale_after"`
	MetadataNamespace   string           `json:"metadata_namespace"`

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
	if c.TargetClusterSource == "" {
		c.TargetClusterSource = targetSourceHeader
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
	if c.MetadataNamespace == "" {
		c.MetadataNamespace = defaultMetadataNamespace
	}

	// Accumulate all problems so one rejected reload reports them together.
	var errs []error

	if c.EnvoyID == "" {
		errs = append(errs, errors.New("envoy_id is required"))
	}
	if c.AdvertiseListen == "" {
		errs = append(errs, errors.New("advertise_listen is required"))
	}
	if c.TargetClusterSource != targetSourceHeader {
		// `metadata` is reserved in the enum but not yet implemented.
		errs = append(errs, errors.New("target_cluster_source must be: header"))
	}

	if d, err := time.ParseDuration(c.PollInterval); err != nil {
		errs = append(errs, fmt.Errorf("poll_interval: %w", err))
	} else if d < minPollInterval {
		errs = append(errs, fmt.Errorf("poll_interval must be >= %s", minPollInterval))
	} else {
		c.pollInterval = d
	}
	if d, err := time.ParseDuration(c.StaleAfter); err != nil {
		errs = append(errs, fmt.Errorf("stale_after: %w", err))
	} else {
		c.staleAfter = d
	}
	// Only comparable once both durations parsed.
	if c.pollInterval > 0 && c.staleAfter > 0 && c.staleAfter < c.pollInterval {
		errs = append(errs, fmt.Errorf("stale_after (%s) must be >= poll_interval (%s)", c.staleAfter, c.pollInterval))
	}

	seen := map[string]bool{}
	peerLocals := map[string]bool{}
	for i, p := range c.Peers {
		if p.ID == "" || p.Endpoint == "" || p.LocalCluster == "" {
			errs = append(errs, fmt.Errorf("peers[%d]: id, endpoint, local_cluster required", i))
		}
		if p.Endpoint != "" {
			if u, err := url.Parse(p.Endpoint); err != nil || u.Scheme == "" || u.Host == "" {
				errs = append(errs, fmt.Errorf("peers[%d]: endpoint %q must be an absolute URL (e.g. http://host:port)", i, p.Endpoint))
			}
		}
		if p.ID == c.EnvoyID {
			errs = append(errs, fmt.Errorf("peers[%d]: id must not equal envoy_id %q", i, c.EnvoyID))
		}
		if seen[p.ID] {
			errs = append(errs, fmt.Errorf("peers[%d]: duplicate id %q", i, p.ID))
		}
		seen[p.ID] = true
		peerLocals[p.LocalCluster] = true
	}

	seenTerm := map[string]bool{}
	for i, name := range c.Terminals {
		if name == "" {
			errs = append(errs, fmt.Errorf("terminals[%d]: name required", i))
		}
		if seenTerm[name] {
			errs = append(errs, fmt.Errorf("terminals[%d]: duplicate name %q", i, name))
		}
		if peerLocals[name] {
			errs = append(errs, fmt.Errorf("terminals[%d]: %q is also a peer local_cluster", i, name))
		}
		seenTerm[name] = true
	}

	return errors.Join(errs...)
}

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
		// Unknown target cluster, so 404 rather than 503.
		body, _ := json.Marshal(map[string]string{"error": "no_route", "target": target})
		p.handle.SendLocalResponse(404,
			[][2]string{{"content-type", "application/json"}},
			body,
			"cluster-router-no-route",
		)
		return shared.HeadersStatusStop
	}

	headers.Set(p.cfg.NextHopHeader, route.NextHopLocalCluster)
	p.handle.ClearRouteCache()

	ns := p.cfg.MetadataNamespace
	p.handle.SetMetadata(ns, "next_hop_cluster", route.NextHopLocalCluster)
	p.handle.SetMetadata(ns, "target_cluster", route.TargetCluster)
	p.handle.SetMetadata(ns, "distance", int64(route.Distance))
	if len(route.ASPath) > 0 {
		p.handle.SetMetadata(ns, "as_path", strings.Join(route.ASPath, ","))
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
		AdvertiseListen: cfg.AdvertiseListen,
		Peers:           cfg.Peers,
		Terminals:       cfg.Terminals,
		PollInterval:    cfg.pollInterval,
		StaleAfter:      cfg.staleAfter,
		Logger:          handle.Log,
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
