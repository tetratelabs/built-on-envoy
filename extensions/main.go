package main

import (
	_ "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/abi"

	// Register built-in plugins into the binary. Because only one golang shared library
	// can be loaded into a process, we need to register all built-in plugins here and
	// build them into the same binary.
	_ "github.com/tetratelabs/built-on-envoy/extensions/goplugin"

	// Example built-in plugin.
	_ "github.com/tetratelabs/built-on-envoy/extensions/example"
)

func main() {} // main is required to build as a C shared library.
