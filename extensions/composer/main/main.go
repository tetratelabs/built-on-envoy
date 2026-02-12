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

	// All built-in plugins will be registered into the binary.
	_ "github.com/tetratelabs/built-on-envoy/extensions/composer"
)

func main() {} // main is required to build as a C shared library.
