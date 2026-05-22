// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package graph

import "sync/atomic"

// Table is an immutable snapshot of the resolved routing state.
type Table struct {
	EnvoyID       string
	Routes        map[string]Route
	LocalClusters map[string]LocalCluster
}

// Lookup returns the route for the given target cluster, if any.
func (t *Table) Lookup(target string) (Route, bool) {
	if t == nil {
		return Route{}, false
	}
	r, ok := t.Routes[target]
	return r, ok
}

// AtomicTable holds the current Table with atomic swap semantics.
type AtomicTable struct {
	v atomic.Pointer[Table]
}

// NewAtomicTable returns an AtomicTable seeded with an empty Table so Load never returns nil.
func NewAtomicTable(envoyID string) *AtomicTable {
	a := &AtomicTable{}
	a.Store(&Table{
		EnvoyID:       envoyID,
		Routes:        map[string]Route{},
		LocalClusters: map[string]LocalCluster{},
	})
	return a
}

// Load returns the current Table snapshot. Safe for concurrent use.
func (a *AtomicTable) Load() *Table { return a.v.Load() }

// Store publishes a new Table snapshot.
func (a *AtomicTable) Store(t *Table) { a.v.Store(t) }
