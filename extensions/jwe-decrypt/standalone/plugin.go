// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package main

import (
	shared "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	impl "github.com/tetratelabs/built-on-envoy/extensions/jwe-decrypt"
)

// ExtensionName is the name of the extension taht will be used in the
// `run` command to refer to this embedded plugin.
const ExtensionName = "jwe-decrypt"

func WellKnownHttpFilterConfigFactories() map[string]shared.HttpFilterConfigFactory {
	return map[string]shared.HttpFilterConfigFactory{
		ExtensionName: &impl.JWEDecryptHttpFilterConfigFactory{},
	}
}
