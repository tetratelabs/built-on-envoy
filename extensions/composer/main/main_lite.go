// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

//go:build lite && !openfga

// Package main builds a Go shared library that registers built-in plugins.
package main

import (
	_ "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/abi"

	// Register built-in plugins into the binary. Because only one golang shared library
	// can be loaded into a process, we need to register all built-in plugins here and
	// build them into the same binary.
	// Go plugin to loader other composer plugins that be compiled into separate shared libraries.
	_ "github.com/tetratelabs/built-on-envoy/extensions/composer/goplugin-loader"
)

func main() {} // main is required to build as a C shared library.
