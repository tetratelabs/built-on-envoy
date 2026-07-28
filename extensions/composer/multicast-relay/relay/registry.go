// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package relay

import (
	"context"
	"sync"
)

// The composer host may build a filter config factory more than once for the same
// configuration, and two relays on one group would double every datagram. Relays are
// therefore interned by canonical config and reference counted.
var (
	registryMu sync.Mutex
	registry   = map[string]*entry{}
)

type entry struct {
	relay *Relay
	refs  int
}

// Acquire returns a running relay for cfg, starting one if this is the first
// holder. The returned key must be passed to Release when the holder is destroyed.
func Acquire(ctx context.Context, cfg *Config) (*Relay, string, error) {
	key := cfg.key()

	registryMu.Lock()
	defer registryMu.Unlock()

	if e, ok := registry[key]; ok {
		e.refs++
		return e.relay, key, nil
	}

	r := New(cfg)
	if err := r.Start(ctx); err != nil {
		return nil, "", err
	}
	registry[key] = &entry{relay: r, refs: 1}
	return r, key, nil
}

// Release drops one reference and stops the relay once none remain.
func Release(key string) {
	registryMu.Lock()
	e, ok := registry[key]
	if !ok {
		registryMu.Unlock()
		return
	}
	e.refs--
	last := e.refs <= 0
	if last {
		delete(registry, key)
	}
	registryMu.Unlock()

	// Outside the lock: Stop waits on the forwarding goroutines.
	if last {
		e.relay.Stop()
	}
}

// refs reports the current reference count for a key, for tests.
func refs(key string) int {
	registryMu.Lock()
	defer registryMu.Unlock()
	if e, ok := registry[key]; ok {
		return e.refs
	}
	return 0
}
