// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package goplugin

// Config for a Go plugin, which includes the plugin's name, URL, and an optional configuration map.
type Config struct {
	// Name of the dynamic module plugin
	Name string `json:"name"`
	// URL to fetch the plugin if it is a custom plugin
	// Supports file:// URLs for local files and OCI image references for remote registries
	URL string `json:"url"`
	// Config is an optional plugin configuration
	Config map[string]any `json:"config,omitempty"`
	// StrictCheck indicates whether to perform strict compatibility checks between the plugin and the host.
	// If not set, defaults to `true`.
	StrictCheck *bool `json:"strict_check,omitempty"`
}
