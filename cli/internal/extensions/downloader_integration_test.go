// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

//go:build integration

package extensions

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	internaltesting "github.com/tetratelabs/built-on-envoy/cli/internal/testing"
)

var registryAddr string

func TestMain(m *testing.M) {
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

func TestNewOCIRepositoryClient(t *testing.T) {
	t.Run("missing repo", func(t *testing.T) {
		client, err := newOCIRepositoryClient(registryAddr, "", "", true)
		require.Error(t, err)
		require.Nil(t, client)
	})

	t.Run("can connect", func(t *testing.T) {
		client, err := newOCIRepositoryClient(registryAddr+"/repo", "", "", true)
		require.NoError(t, err)
		// We just want to test connectivity here so we expect a failure saying that
		// the repo does not exist, but this is good enough for this test.
		_, err = client.Tags(t.Context())
		require.ErrorContains(t, err, "repository name not known")
	})
}
