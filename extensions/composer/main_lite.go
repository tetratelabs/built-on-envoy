// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

//go:build lite

// Package main builds a Go shared library that registers built-in plugins.
package main

import (
	_ "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/abi"

	// Register built-in plugins into the binary. Because only one golang shared library
	// can be loaded into a process, we need to register all built-in plugins here and
	// build them into the same binary.
	_ "github.com/tetratelabs/built-on-envoy/extensions/composer/goplugin"
)

func main() {} // main is required to build as a C shared library.
