// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
//
//go:build openfga

// Package main builds a minimal Go shared library with only the OpenFGA plugin.
// Use for the AI Gateway demo: -tags openfga
package main

import (
	_ "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/abi"
	_ "github.com/tetratelabs/built-on-envoy/extensions/composer/openfga/embedded"
)

func main() {} // main is required to build as a C shared library.
