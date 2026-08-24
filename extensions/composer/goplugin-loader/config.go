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
	URL string `json:"url"`
	// Config is an optional plugin configuration
	Config map[string]any `json:"config,omitempty"`
	// StrictCheck indicates whether to perform strict compatibility checks between the plugin and the host.
	// If not set, defaults to `true`.
	StrictCheck *bool `json:"strict_check,omitempty"`
	// VersionedURLSuffix indicates whether the composer version should be appended to the tag of
	// the `URL` before the plugin image is fetched, so that an `oci://repo/plugin:1.0.0` URL is
	// fetched as `oci://repo/plugin:1.0.0-<composer version>`. This keeps the configuration
	// independent of the composer version: upgrading the proxy (and and also the libcomposer.so)
	// makes the loader fetch the plugin build of the same, compatible composer version.
	// It only applies to `oci://` URLs and defaults to `false`.
	VersionedURLSuffix bool `json:"versioned_url_suffix,omitempty"`
}
