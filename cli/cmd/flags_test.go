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
				{Name: "ext1", Remote: true},
			},
			want: []*extensions.Manifest{
				{Name: "ext1", Remote: true},
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
				{Name: "ext1", Remote: true},
				{Name: "local1", Path: localDir1 + "/manifest.yaml"},
			},
			want: []*extensions.Manifest{
				{Name: "local1", Path: localDir1 + "/manifest.yaml"},
				{Name: "ext1", Remote: true},
			},
		},
		{
			name: "remote before local",
			args: []string{"boe", "gen-config", "--extension", "ext1", "--local", localDir1},
			manifests: []*extensions.Manifest{
				{Name: "ext1", Remote: true},
				{Name: "local1", Path: localDir1 + "/manifest.yaml"},
			},
			want: []*extensions.Manifest{
				{Name: "ext1", Remote: true},
				{Name: "local1", Path: localDir1 + "/manifest.yaml"},
			},
		},
		{
			name: "interleaved and duplicate remote and local",
			args: []string{
				"boe", "gen-config", "--extension", "ext1",
				"--local", localDir1, "--extension", "ext2", "--local", localDir2, "--extension", "ext1", "--local", localDir1,
			},
			manifests: []*extensions.Manifest{
				{Name: "ext1", Remote: true},
				{Name: "ext2", Remote: true},
				{Name: "ext1", Remote: true},
				{Name: "local1", Path: localDir1 + "/manifest.yaml"},
				{Name: "local2", Path: localDir2 + "/manifest.yaml"},
				{Name: "local1", Path: localDir1 + "/manifest.yaml"},
			},
			want: []*extensions.Manifest{
				{Name: "ext1", Remote: true},
				{Name: "local1", Path: localDir1 + "/manifest.yaml"},
				{Name: "ext2", Remote: true},
				{Name: "local2", Path: localDir2 + "/manifest.yaml"},
				{Name: "ext1", Remote: true},
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
