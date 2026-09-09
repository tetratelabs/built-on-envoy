// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package main builds a c-shared library that can be loaded directly by Envoy as a dynamic module.
// Use this entry point when building the extension as an independent shared library.
// For building as a Go plugin loaded by composer, use the standalone/ directory instead.
package main

import (
	// Importthe Dynamic module SDK ABI package to ensure the ABI implementations are linked in
	// correctly and the Envoy host could load and use this module.
	_ "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/abi"

	// Import the extension which will register itself to the SDK in its init() function.
	_ "github.com/tetratelabs/built-on-envoy/extensions/composer/dynamic-fault-injection/embedded"
)

func main() {} // required for c-shared build
