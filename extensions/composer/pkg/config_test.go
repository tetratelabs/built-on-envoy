// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package pkg

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDataSource_Validate(t *testing.T) {
	tests := []struct {
		name    string
		ds      DataSource
		wantErr error
	}{
		{"inline only", DataSource{Inline: "some data"}, nil},
		{"file only", DataSource{File: "/some/path"}, nil},
		{"both set", DataSource{Inline: "data", File: "/path"}, ErrDataSourceBothSet},
		{"neither set", DataSource{}, ErrDataSourceNeitherSet},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.ErrorIs(t, tt.ds.Validate(), tt.wantErr)
		})
	}
}

func TestDataSource_Content(t *testing.T) {
	t.Run("inline content", func(t *testing.T) {
		ds := DataSource{Inline: "inline data"}
		content, err := ds.Content()
		require.NoError(t, err)
		require.Equal(t, []byte("inline data"), content)
	})

	t.Run("file content", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "data.txt")
		require.NoError(t, os.WriteFile(path, []byte("file data"), 0o600))

		ds := DataSource{File: path}
		content, err := ds.Content()
		require.NoError(t, err)
		require.Equal(t, []byte("file data"), content)
	})

	t.Run("file not found", func(t *testing.T) {
		ds := DataSource{File: "/nonexistent/path/data.txt"}
		_, err := ds.Content()
		require.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("neither set", func(t *testing.T) {
		ds := DataSource{}
		_, err := ds.Content()
		require.ErrorIs(t, err, ErrDataSourceNeitherSet)
	})

	t.Run("inline takes precedence when both set", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "data.txt")
		require.NoError(t, os.WriteFile(path, []byte("file data"), 0o600))

		ds := DataSource{Inline: "inline data", File: path}
		content, err := ds.Content()
		require.NoError(t, err)
		require.Equal(t, []byte("inline data"), content)
	})
}
