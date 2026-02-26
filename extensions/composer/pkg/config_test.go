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

func TestMetadataKey_Validate(t *testing.T) {
	tests := []struct {
		name    string
		key     MetadataKey
		wantErr error
	}{
		{"valid", MetadataKey{Namespace: "ns", Key: "k"}, nil},
		{"missing namespace", MetadataKey{Key: "k"}, ErrMetadataKeyInvalid},
		{"missing key", MetadataKey{Namespace: "ns"}, ErrMetadataKeyInvalid},
		{"both missing", MetadataKey{}, ErrMetadataKeyInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.ErrorIs(t, tt.key.Validate(), tt.wantErr)
		})
	}
}

func TestLocalResponse_Validate(t *testing.T) {
	tests := []struct {
		name    string
		resp    LocalResponse
		wantErr error
	}{
		{"valid 200", LocalResponse{Status: 200}, nil},
		{"valid 100", LocalResponse{Status: 100}, nil},
		{"valid 599", LocalResponse{Status: 599}, nil},
		{"valid with body and headers", LocalResponse{Status: 403, Body: "forbidden", Headers: map[string]string{"X-Reason": "denied"}}, nil},
		{"status too low", LocalResponse{Status: 99}, ErrInvalidHTTPStatus},
		{"status too high", LocalResponse{Status: 600}, ErrInvalidHTTPStatus},
		{"status zero", LocalResponse{}, ErrInvalidHTTPStatus},
		{"status negative", LocalResponse{Status: -1}, ErrInvalidHTTPStatus},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.ErrorIs(t, tt.resp.Validate(), tt.wantErr)
		})
	}
}

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
