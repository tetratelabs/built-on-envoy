// Copyright Envoy Ecosystem
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Envoy Ecosystem CLI (ee) - Work with Envoy extensions.
package main

import (
	"os"

	"github.com/alecthomas/kong"

	"github.com/tetratelabs/envoy-ecosystem/cli/cmd/run"
)

var CLI struct {
	Run run.Cmd `cmd:"" help:"Run Envoy with extensions"`
}

func main() {
	ctx := kong.Parse(&CLI,
		kong.Name("ee"),
		kong.Description("Envoy Ecosystem CLI - Work with Envoy extensions"),
		kong.UsageOnError(),
	)

	err := ctx.Run()
	ctx.FatalIfErrorf(err)
	os.Exit(0)
}
