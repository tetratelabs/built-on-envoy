// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package cmd

import (
	"github.com/alecthomas/kong"

	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
)

// Vars defines Kong variables used across the CLI.
var Vars = kong.Vars{
	"default_registry": extensions.DefaultOCIRegistry,
}
