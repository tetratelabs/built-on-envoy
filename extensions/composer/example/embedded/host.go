// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package host is used when the plugin is built into the host binary. It registers the plugin
// to the host registry so that it can be used without loading a shared library.
package host

import (
	sdk "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"

	impl "github.com/tetratelabs/built-on-envoy/extensions/composer/example"
)

// ExtensionName is the name of the extension that will be used in the
// `run` command to refer to this embedded plugin.
const ExtensionName = "example"

// Register this plugin to the host registry if this is built into the host binary.
func init() {
	sdk.RegisterHttpFilterConfigFactories(map[string]shared.HttpFilterConfigFactory{
		ExtensionName: &impl.PluginConfigFactory{},
	})
}
