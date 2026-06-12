// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package cmd

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tetratelabs/built-on-envoy/cli/internal/extensions"
)

func TestExtensionPositionsSort(t *testing.T) {
	cwd, err := os.Getwd()
	require.NoError(t, err)

	localDir1 := cwd + "/testdata/input_lua_inline"
	localDir2 := "./testdata/invalid_manifest/"

	tests := []struct {
		name      string
		args      []string
		manifests []*extensions.Manifest
		want      []*extensions.Manifest
	}{
		{
			name: "single remote extension",
			args: []string{"boe", "gen-config", "--extension", "ext1"},
			manifests: []*extensions.Manifest{
				{Name: "ext1", Remote: true, RemoteRef: "ext1"},
			},
			want: []*extensions.Manifest{
				{Name: "ext1", Remote: true, RemoteRef: "ext1"},
			},
		},
		{
			name: "single remote extension with version",
			args: []string{"boe", "gen-config", "--extension", "ext1:0.1.0"},
			manifests: []*extensions.Manifest{
				{Name: "ext1", Version: "0.1.0", Remote: true, RemoteRef: "ext1:0.1.0"},
			},
			want: []*extensions.Manifest{
				{Name: "ext1", Version: "0.1.0", Remote: true, RemoteRef: "ext1:0.1.0"},
			},
		},
		{
			name: "single local extension",
			args: []string{"boe", "gen-config", "--local", localDir1},
			manifests: []*extensions.Manifest{
				{Name: "local1", Path: localDir1 + "/manifest.yaml"},
			},
			want: []*extensions.Manifest{
				{Name: "local1", Path: localDir1 + "/manifest.yaml"},
			},
		},
		{
			name: "local before remote",
			args: []string{"boe", "gen-config", "--local", localDir1, "--extension", "ext1"},
			manifests: []*extensions.Manifest{
				{Name: "ext1", Remote: true, RemoteRef: "ext1"},
				{Name: "local1", Path: localDir1 + "/manifest.yaml"},
			},
			want: []*extensions.Manifest{
				{Name: "local1", Path: localDir1 + "/manifest.yaml"},
				{Name: "ext1", Remote: true, RemoteRef: "ext1"},
			},
		},
		{
			name: "remote before local",
			args: []string{"boe", "gen-config", "--extension", "ext1", "--local", localDir1},
			manifests: []*extensions.Manifest{
				{Name: "ext1", Remote: true, RemoteRef: "ext1"},
				{Name: "local1", Path: localDir1 + "/manifest.yaml"},
			},
			want: []*extensions.Manifest{
				{Name: "ext1", Remote: true, RemoteRef: "ext1"},
				{Name: "local1", Path: localDir1 + "/manifest.yaml"},
			},
		},
		{
			name: "interleaved and duplicate remote and local",
			args: []string{
				"boe", "gen-config",
				"--extension", "ext1",
				"--local", localDir1,
				"--extension", "ext2",
				"--local", localDir2,
				"--extension", "ext1:1.0.0",
				"--local", localDir1,
			},
			manifests: []*extensions.Manifest{
				{Name: "ext1", Version: "1.0.0", Remote: true, RemoteRef: "ext1"},
				{Name: "ext2", Remote: true, RemoteRef: "ext2"},
				{Name: "ext1", Version: "1.0.0", Remote: true, RemoteRef: "ext1:1.0.0"},
				{Name: "local1", Path: localDir1 + "/manifest.yaml"},
				{Name: "local2", Path: localDir2 + "/manifest.yaml"},
				{Name: "local1", Path: localDir1 + "/manifest.yaml"},
			},
			want: []*extensions.Manifest{
				{Name: "ext1", Version: "1.0.0", Remote: true, RemoteRef: "ext1"},
				{Name: "local1", Path: localDir1 + "/manifest.yaml"},
				{Name: "ext2", Remote: true, RemoteRef: "ext2"},
				{Name: "local2", Path: localDir2 + "/manifest.yaml"},
				{Name: "ext1", Version: "1.0.0", Remote: true, RemoteRef: "ext1:1.0.0"},
				{Name: "local1", Path: localDir1 + "/manifest.yaml"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			positions, err := saveExtensionPositions(tt.args)
			require.NoError(t, err)

			sorted, err := positions.sort(tt.manifests)
			require.NoError(t, err)
			require.Equal(t, tt.want, sorted)
		})
	}
}
