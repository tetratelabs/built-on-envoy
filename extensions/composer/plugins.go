// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package composer provides built-in plugins for the composer binary.
package composer

import (
	// Example built-in plugin.
	_ "github.com/tetratelabs/built-on-envoy/extensions/composer/example/embedded"
	// Go plugin to loader other composer plugins that be compiled into separate shared libraries.
	_ "github.com/tetratelabs/built-on-envoy/extensions/composer/goplugin"
	// WAF plugin.
	_ "github.com/tetratelabs/built-on-envoy/extensions/composer/waf/embedded"
)
