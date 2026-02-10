// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

//go:build !lite

// Package main builds a Go shared library that registers built-in plugins.
package main

/*
#cgo darwin LDFLAGS: -Wl,-undefined,dynamic_lookup
#cgo linux LDFLAGS: -Wl,--unresolved-symbols=ignore-all
*/
import "C"

import (
	_ "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/abi"

	// Example built-in plugin.
	_ "github.com/tetratelabs/built-on-envoy/extensions/composer/example/embedded"
	// Register built-in plugins into the binary. Because only one golang shared library
	// can be loaded into a process, we need to register all built-in plugins here and
	// build them into the same binary.
	_ "github.com/tetratelabs/built-on-envoy/extensions/composer/goplugin"
)

func main() {} // main is required to build as a C shared library.
