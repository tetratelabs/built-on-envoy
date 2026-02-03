// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package oci

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/registry"
)

type registryClientTest struct {
	name              string
	newRegistryClient func(t *testing.T) RegistryClient
}

var testRepos = []string{"repo1", "repo2"}

var registryClientTests = []registryClientTest{
	{
		name: "in-memory store",
		newRegistryClient: func(*testing.T) RegistryClient {
			return NewRegistryClient(&mockRegistry{
				repos: testRepos,
			})
		},
	},
}

func TestRegistryClient_ListRepositories(t *testing.T) {
	for _, tt := range registryClientTests {
		t.Run(tt.name, func(t *testing.T) {
			client := tt.newRegistryClient(t)
			repos, err := client.ListRepositories(t.Context())
			require.NoError(t, err)
			for _, wantRepo := range testRepos {
				require.Contains(t, repos, wantRepo)
			}
		})
	}
}

func TestNewRemoteRegistry(t *testing.T) {
	reg, err := NewRemoteRegistry("ghcr.io", nil)
	require.NoError(t, err)
	require.NotNil(t, reg)
	assert.Equal(t, "ghcr.io", reg.Reference.Registry)
}

func TestNewRemoteRegistry_WithOptions(t *testing.T) {
	opts := &ClientOptions{
		Credentials: &Credentials{
			Username: "user",
			Password: "pass",
		},
		PlainHTTP: true,
	}

	reg, err := NewRemoteRegistry("ghcr.io", opts)
	require.NoError(t, err)
	require.NotNil(t, reg)
	assert.True(t, reg.PlainHTTP)
	assert.NotNil(t, reg.Client)
}

func TestNewRemoteRegistry_InvalidReference(t *testing.T) {
	_, err := NewRemoteRegistry("invalid/reference/format", nil)
	require.Error(t, err)
}

var _ registry.Registry = (*mockRegistry)(nil)

// mockRegistry is a mock that implements registry.Registry.
type mockRegistry struct {
	repos []string
}

func (m *mockRegistry) Repositories(_ context.Context, _ string, fn func(repos []string) error) error {
	return fn(m.repos)
}

func (m *mockRegistry) Repository(context.Context, string) (registry.Repository, error) {
	return nil, nil
}
