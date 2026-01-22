// Copyright Envoy Ecosystem
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package extensions

import (
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAllManifestsAreValid(t *testing.T) {
	for _, manifest := range Manifests {
		assert.NoErrorf(t, validateManifest(manifest), "manifest: %s", manifest.Name)
	}
}

func TestAllMAnifestsAreLoaded(t *testing.T) {
	count := 0
	err := fs.WalkDir(manifestFS, "manifests", func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			count++
		}
		return nil
	})

	require.NoError(t, err)
	require.Len(t, Manifests, count)
}
