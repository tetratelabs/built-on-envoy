// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package extensions

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/mod/semver"
)

func TestLibComposerVersion(t *testing.T) {
	require.Truef(t, semver.IsValid("v"+LibComposerVersion),
		"ComposerVersion %q is not a valid semver", LibComposerVersion)
}

func TestCheckOrBuildLibComposer(t *testing.T) {
	fakeDataHome := t.TempDir()
	err := CheckOrBuildLibComposer(fakeDataHome)
	require.NoError(t, err, "CheckOrBuildLibComposer failed")

	// Ensure the libcomposer.so is created.
	composerPath := filepath.Join(fakeDataHome, "extensions", "dym", "composer",
		LibComposerVersion, "libcomposer.so")
	_, err = os.Stat(composerPath)
	require.NoErrorf(t, err, "libcomposer.so not found at %s", composerPath)
}
