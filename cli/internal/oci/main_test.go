// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

//go:build integration

package oci

import (
	"context"
	"fmt"
	"os"
	"testing"

	internaltesting "github.com/tetratelabs/built-on-envoy/internal/testing"
)

var (
	testRegistry *internaltesting.TestRegistry
	builder      string
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	testRegistry, err = internaltesting.StartOCIRegistry(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to start local OCI registry: %v\n", err)
		os.Exit(1)
	}

	var cleanup func()
	builder, cleanup, err = internaltesting.CreateBuildxBuilder(ctx)
	if err != nil {
		_ = testRegistry.Container.Terminate(ctx)
		fmt.Fprintf(os.Stderr, "failed to create buildx builder: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()

	_ = testRegistry.Container.Terminate(ctx)
	cleanup()

	os.Exit(code)
}
