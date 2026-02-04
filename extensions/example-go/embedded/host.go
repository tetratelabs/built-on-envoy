// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package example

import (
	sdk "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	impl "github.com/tetratelabs/built-on-envoy/extensions/example-go"
)

// ExtensionName is the name of the extension taht will be used in the
// `run` command to refer to this embedded plugin.
const ExtensionName = "example-go-embedded"

// Register this plugin to the host registry if this is built into the host binary.
func init() {
	sdk.RegisterHttpFilterConfigFactories(map[string]shared.HttpFilterConfigFactory{
		ExtensionName: &impl.ExamplePluginConfigFactory{},
	})
}
