package composer

import (
	// Register built-in plugins into the binary. Because only one golang shared library
	// can be loaded into a process, we need to register all built-in plugins here and
	// build them into the same binary.

	// Go plugin to loader other composer plugins that be compiled into separate shared libraries.
	_ "github.com/tetratelabs/built-on-envoy/extensions/composer/goplugin"
	// Example built-in plugin.
	_ "github.com/tetratelabs/built-on-envoy/extensions/composer/example/embedded"
	// WAF plugin.
	_ "github.com/tetratelabs/built-on-envoy/extensions/composer/waf/embedded"
)
