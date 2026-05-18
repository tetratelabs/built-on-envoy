// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package internaltesting

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

// ExtensionsFS returns an fs.FS rooted at the source extensions directory.
func ExtensionsFS(t *testing.T) fs.FS {
	t.Helper()
	_, f, _, _ := runtime.Caller(0)
	dir := filepath.Join(filepath.Dir(f), "..", "..", "..", "extensions")
	require.DirExists(t, dir)
	return os.DirFS(dir)
}
