// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package extensions

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/mod/semver"
)

func TestLibComposerVersion(t *testing.T) {
	require.Truef(t, semver.IsValid("v"+LibComposerVersion),
		"ComposerVersion %q is not a valid semver", LibComposerVersion)
}
