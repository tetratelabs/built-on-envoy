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
	boeBin       string
	registryAddr string
)

func TestMain(m *testing.M) {
	var err error
	boeBin, err = buildCLIOnDemand()
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

	code := m.Run()
	_ = registryContainer.Terminate(ctx)
	os.Exit(code)
}

// buildCLIOnDemand builds the CLI binary unless BOE_BIN is set.
// If BOE_BIN environment variable is set, it will use that path instead.
func buildCLIOnDemand() (string, error) {
	return internaltesting.BuildGoBinaryOnDemand("BOE_BIN", "boe", ".")
}
