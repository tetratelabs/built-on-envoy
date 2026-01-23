// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package e2e

import (
	"fmt"
	"os"
	"testing"

	internaltesting "github.com/tetratelabs/built-on-envoy/cli/internal/testing"
)

var cliBin string

const proxyPort = 10000

func TestMain(m *testing.M) {
	var err error
	cliBin, err = buildCLIOnDemand()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to build CLI binary: %v\n", err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

// buildCLIOnDemand builds the CLI binary unless CLI_BIN is set.
// If CLI_BIN environment variable is set, it will use that path instead.
func buildCLIOnDemand() (string, error) {
	return internaltesting.BuildGoBinaryOnDemand("CLI_BIN", "boe", ".")
}
