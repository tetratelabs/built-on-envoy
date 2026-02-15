// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package e2e

import (
	"context"
	"fmt"
	"os"
	"testing"

	internaltesting "github.com/tetratelabs/built-on-envoy/cli/internal/testing"
)

var (
	cliBin       string
	registryAddr string
	builder      string
)

func TestMain(m *testing.M) {
	var err error
	cliBin, err = buildCLIOnDemand()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to build CLI binary: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	registryContainer, addr, err := internaltesting.StartOCIRegistry(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to start local OCI registry: %v\n", err)
		os.Exit(1)
	}
	registryAddr = addr

	var cleanup func()
	builder, cleanup, err = internaltesting.CreateBuildxBuilder(ctx)
	if err != nil {
		_ = registryContainer.Terminate(ctx)
		fmt.Fprintf(os.Stderr, "failed to create buildx builder: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()

	_ = registryContainer.Terminate(ctx)
	cleanup()

	os.Exit(code)
}

// buildCLIOnDemand builds the CLI binary unless CLI_BIN is set.
// If CLI_BIN environment variable is set, it will use that path instead.
func buildCLIOnDemand() (string, error) {
	return internaltesting.BuildGoBinaryOnDemand("CLI_BIN", "boe", ".")
}
