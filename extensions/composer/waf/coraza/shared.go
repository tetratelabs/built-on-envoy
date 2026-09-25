// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package coraza

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"runtime"
	"sync"
	"weak"

	"github.com/corazawaf/coraza/v3"
	"github.com/corazawaf/coraza/v3/experimental"
	"go.uber.org/zap"
)

// SharedWAF is a WAF shared by all filter configs, across config updates, built from the same
// directives and unchanged included/data files.
//
// Filter configs and streams must hold the *SharedWAF, not the embedded coraza.WAF: the WAF is
// closed once the SharedWAF is unreachable.
type SharedWAF struct {
	coraza.WAF
	inputs *wafInputs
}

// GetOrCreateSharedWAF returns the WAF for the given directives, reusing a live one built from
// the same directives if the files it was built from are unchanged.
func GetOrCreateSharedWAF(directives string, l *zap.Logger) (*SharedWAF, error) {
	return sharedWAFs.getOrCreate(directives, l, combinedDirectivesFS)
}

var sharedWAFs = newSharedWAFCache()

type sharedWAFCache struct {
	mu      sync.Mutex
	entries map[[sha256.Size]byte]weak.Pointer[SharedWAF]
}

func newSharedWAFCache() *sharedWAFCache {
	return &sharedWAFCache{entries: map[[sha256.Size]byte]weak.Pointer[SharedWAF]{}}
}

func (c *sharedWAFCache) getOrCreate(directives string, l *zap.Logger, root fs.FS) (*SharedWAF, error) {
	key := sha256.Sum256([]byte(directives))

	c.mu.Lock()
	defer c.mu.Unlock()

	if s := c.entries[key].Value(); s != nil && s.inputs.unchanged(root) {
		return s, nil
	}

	recorder := newRecordingFS(root)
	w, err := newWAF(directives, l, recorder)
	if err != nil {
		return nil, fmt.Errorf("failed to create WAF from directives: %w", err)
	}
	s := &SharedWAF{WAF: w, inputs: recorder.inputs}
	c.entries[key] = weak.Make(s)
	runtime.AddCleanup(s, func(w coraza.WAF) { c.release(key, w) }, w)
	return s, nil
}

// release runs once a SharedWAF is unreachable: it drops its cache entry, unless the entry
// already holds a newer WAF, and closes the WAF.
func (c *sharedWAFCache) release(key [sha256.Size]byte, w coraza.WAF) {
	c.mu.Lock()
	if wp, ok := c.entries[key]; ok && wp.Value() == nil {
		delete(c.entries, key)
	}
	c.mu.Unlock()

	if closer, ok := w.(experimental.WAFCloser); ok {
		_ = closer.Close()
	}
}
