// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package coraza

import (
	"crypto/sha256"
	"io/fs"
	"runtime"
	"testing"
	"testing/fstest"
	"time"
	"weak"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func (c *sharedWAFCache) cached(directives string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.entries[sha256.Sum256([]byte(directives))]
	return ok
}

func TestSharedWAF_SameDirectivesShareInstance(t *testing.T) {
	c := newSharedWAFCache()
	root := fstest.MapFS{}

	a, err := c.getOrCreate(`SecRule REQUEST_URI "@rx ^/a$" "id:1,phase:1,deny"`, zap.NewNop(), root)
	require.NoError(t, err)
	again, err := c.getOrCreate(`SecRule REQUEST_URI "@rx ^/a$" "id:1,phase:1,deny"`, zap.NewNop(), root)
	require.NoError(t, err)
	other, err := c.getOrCreate(`SecRule REQUEST_URI "@rx ^/b$" "id:1,phase:1,deny"`, zap.NewNop(), root)
	require.NoError(t, err)

	require.Same(t, a, again)
	require.NotSame(t, a, other)
}

func TestSharedWAF_InvalidDirectives(t *testing.T) {
	c := newSharedWAFCache()
	_, err := c.getOrCreate("SecRuleEngine Invalid", zap.NewNop(), fstest.MapFS{})
	require.ErrorContains(t, err, "failed to create WAF")
	require.False(t, c.cached("SecRuleEngine Invalid"))
}

func TestSharedWAF_RebuiltWhenInputsChange(t *testing.T) {
	const rule = `SecRule REQUEST_URI "@rx ^/included$" "id:1,phase:1,deny"`

	tests := []struct {
		name       string
		directives string
		root       fstest.MapFS
		change     func(fstest.MapFS)
	}{
		{
			name:       "included file edited",
			directives: "Include rules.conf",
			root:       fstest.MapFS{"rules.conf": {Data: []byte(rule)}},
			change: func(root fstest.MapFS) {
				root["rules.conf"] = &fstest.MapFile{Data: []byte(`SecRule REQUEST_URI "@rx ^/edited$" "id:1,phase:1,deny"`)}
			},
		},
		{
			name:       "file added to included glob",
			directives: "Include rules/*.conf",
			root:       fstest.MapFS{"rules/a.conf": {Data: []byte(rule)}},
			change: func(root fstest.MapFS) {
				root["rules/b.conf"] = &fstest.MapFile{Data: []byte(`SecRule REQUEST_URI "@rx ^/b$" "id:2,phase:1,deny"`)}
			},
		},
		{
			// Data files are resolved relative to the directory of the including file.
			name:       "operator data file edited",
			directives: "Include rules/rules.conf",
			root: fstest.MapFS{
				"rules/rules.conf": {Data: []byte(`SecRule REQUEST_URI "@pmFromFile words.txt" "id:1,phase:1,deny"`)},
				"rules/words.txt":  {Data: []byte("one\n")},
			},
			change: func(root fstest.MapFS) {
				root["rules/words.txt"] = &fstest.MapFile{Data: []byte("two\n")}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := newSharedWAFCache()

			first, err := c.getOrCreate(tc.directives, zap.NewNop(), tc.root)
			require.NoError(t, err)
			unchanged, err := c.getOrCreate(tc.directives, zap.NewNop(), tc.root)
			require.NoError(t, err)
			require.Same(t, first, unchanged)

			tc.change(tc.root)
			rebuilt, err := c.getOrCreate(tc.directives, zap.NewNop(), tc.root)
			require.NoError(t, err)
			require.NotSame(t, first, rebuilt)
			// The previous WAF keeps working for the configs and streams still holding it.
			require.NotNil(t, first.NewTransaction())

			again, err := c.getOrCreate(tc.directives, zap.NewNop(), tc.root)
			require.NoError(t, err)
			require.Same(t, rebuilt, again)
		})
	}
}

func TestSharedWAF_ReleasedWhenUnreferenced(t *testing.T) {
	const directives = `SecRule REQUEST_URI "@rx ^/release$" "id:1,phase:1,deny"`
	c := newSharedWAFCache()

	s, err := c.getOrCreate(directives, zap.NewNop(), fstest.MapFS{})
	require.NoError(t, err)
	runtime.GC()
	require.True(t, c.cached(directives), "a referenced WAF must stay cached")
	runtime.KeepAlive(s)

	s = nil //nolint:ineffassign,wastedassign // drop the last reference
	require.Eventually(t, func() bool {
		runtime.GC()
		return !c.cached(directives)
	}, 5*time.Second, 10*time.Millisecond)
}

func TestSharedWAF_ReleaseOfReplacedWAFKeepsNewEntry(t *testing.T) {
	const directives = "Include rules.conf"
	c := newSharedWAFCache()
	root := fstest.MapFS{"rules.conf": {Data: []byte(`SecRule REQUEST_URI "@rx ^/v1$" "id:1,phase:1,deny"`)}}

	old, err := c.getOrCreate(directives, zap.NewNop(), root)
	require.NoError(t, err)
	root["rules.conf"] = &fstest.MapFile{Data: []byte(`SecRule REQUEST_URI "@rx ^/v2$" "id:1,phase:1,deny"`)}
	current, err := c.getOrCreate(directives, zap.NewNop(), root)
	require.NoError(t, err)

	// Wait for the replaced WAF to be collected and its cleanup to run.
	collected := weak.Make(old)
	old = nil //nolint:ineffassign,wastedassign // drop the replaced WAF
	require.Eventually(t, func() bool {
		runtime.GC()
		return collected.Value() == nil
	}, 5*time.Second, 10*time.Millisecond)
	for range 5 {
		runtime.GC()
		time.Sleep(10 * time.Millisecond)
	}

	again, err := c.getOrCreate(directives, zap.NewNop(), root)
	require.NoError(t, err)
	require.Same(t, current, again, "releasing the replaced WAF must not evict the current one")
}

func TestRecordingFS(t *testing.T) {
	t.Run("missing file that appears", func(t *testing.T) {
		root := fstest.MapFS{}
		r := newRecordingFS(root)
		_, err := fs.ReadFile(r, "data.txt")
		require.ErrorIs(t, err, fs.ErrNotExist)
		require.True(t, r.inputs.unchanged(root))

		root["data.txt"] = &fstest.MapFile{Data: []byte("x")}
		require.False(t, r.inputs.unchanged(root))
	})

	t.Run("removed file", func(t *testing.T) {
		root := fstest.MapFS{"data.txt": {Data: []byte("x")}}
		r := newRecordingFS(root)
		_, err := fs.ReadFile(r, "data.txt")
		require.NoError(t, err)
		require.True(t, r.inputs.unchanged(root))

		delete(root, "data.txt")
		require.False(t, r.inputs.unchanged(root))
	})

	t.Run("untracked access is never reused", func(t *testing.T) {
		root := fstest.MapFS{"data.txt": {Data: []byte("x")}}
		r := newRecordingFS(root)
		f, err := r.Open("data.txt")
		require.NoError(t, err)
		require.NoError(t, f.Close())
		require.False(t, r.inputs.unchanged(root))
	})

	t.Run("embedded directives", func(t *testing.T) {
		r := newRecordingFS(combinedDirectivesFS)
		_, err := newWAF("Include @coraza.conf\nInclude @crs-setup.conf\nInclude @owasp_crs/*.conf", zap.NewNop(), r)
		require.NoError(t, err)
		require.False(t, r.inputs.untracked)
		require.NotEmpty(t, r.inputs.files)
		require.NotEmpty(t, r.inputs.globs)
		require.True(t, r.inputs.unchanged(combinedDirectivesFS))
	})
}
