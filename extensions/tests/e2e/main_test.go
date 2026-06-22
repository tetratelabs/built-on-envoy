// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package integration

import (
	"fmt"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/mccutchen/go-httpbin/v2/httpbin"

	internaltesting "github.com/tetratelabs/built-on-envoy/internal/testing"
)

var cliBin string

func TestMain(m *testing.M) {
	var err error
	cliBin, err = buildCLIOnDemand()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to build CLI binary: %v\n", err)
		os.Exit(1)
	}

	testUpstream := httptest.NewServer(httpbin.New())
	// This env var is used in the tests RunEnvoy to automatically configure the test upstream cluster.
	_ = os.Setenv("TEST_BOE_UPSTREAM_CLUSTER_INSECURE", testUpstream.Listener.Addr().String())

	code := m.Run()

	testUpstream.Close()

	os.Exit(code)
}

// buildCLIOnDemand builds the CLI binary unless CLI_BIN is set.
// If CLI_BIN environment variable is set, it will use that path instead.
func buildCLIOnDemand() (string, error) {
	return internaltesting.BuildGoBinaryOnDemand("CLI_BIN", "boe", ".")
}
