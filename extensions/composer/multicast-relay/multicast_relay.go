// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package multicastrelay is a composer plugin that brings IPv4 multicast traffic into
// Envoy. It joins the configured groups on sockets it owns and re-emits each datagram as
// unicast UDP to Envoy's UDP listener, where udp_proxy forwards it upstream.
//
// The HTTP filter it registers does nothing on the request path; the filter config
// factory is simply the lifecycle hook for the background relay.
package multicastrelay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/multicast-relay/relay"
)

// ExtensionName is the filter name composer registers this plugin under.
const ExtensionName = "multicast-relay"

func parseConfig(raw []byte) (*relay.Config, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty config")
	}
	var c relay.Config
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &c, c.Validate()
}

// Plugin is the per-stream filter instance, deliberately inert: the work happens in the
// background relay.
type Plugin struct {
	shared.EmptyHttpFilter
}

// pluginInstance is shared across streams because Plugin holds no per-stream state.
var pluginInstance = &Plugin{}

// PluginFactory creates Plugin instances and holds this config's relay reference.
type PluginFactory struct {
	key string
}

// Create returns the shared, stateless filter instance.
func (f *PluginFactory) Create(_ shared.HttpFilterHandle) shared.HttpFilter {
	return pluginInstance
}

// OnDestroy releases this holder's reference, stopping the relay once it was the last.
func (f *PluginFactory) OnDestroy() {
	if f.key != "" {
		relay.Release(f.key)
		f.key = ""
	}
}

// PluginConfigFactory parses the plugin config and starts the relay.
type PluginConfigFactory struct {
	shared.EmptyHttpFilterConfigFactory
}

// Create validates the config and acquires a relay. A bad config or failed group join is
// returned as an error so Envoy rejects the filter config rather than receiving nothing.
func (f *PluginConfigFactory) Create(handle shared.HttpFilterConfigHandle, unparsed []byte) (shared.HttpFilterFactory, error) {
	cfg, err := parseConfig(unparsed)
	if err != nil {
		handle.Log(shared.LogLevelError, "multicast-relay: %v", err)
		return nil, err
	}
	cfg.Logger = handle.Log

	_, key, err := relay.Acquire(context.Background(), cfg)
	if err != nil {
		handle.Log(shared.LogLevelError, "multicast-relay: start: %v", err)
		return nil, err
	}
	return &PluginFactory{key: key}, nil
}

// CreatePerRoute is a no-op; the plugin has no per-route config.
func (f *PluginConfigFactory) CreatePerRoute(_ []byte) (any, error) { return nil, nil }

// WellKnownHttpFilterConfigFactories returns the factories this plugin registers with the composer host.
func WellKnownHttpFilterConfigFactories() map[string]shared.HttpFilterConfigFactory { //nolint:revive
	return map[string]shared.HttpFilterConfigFactory{
		ExtensionName: &PluginConfigFactory{},
	}
}
