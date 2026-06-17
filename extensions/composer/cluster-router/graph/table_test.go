// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package graph

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAtomicTable_InitialNotNil(t *testing.T) {
	a := NewAtomicTable("envoyA")
	tbl := a.Load()
	require.NotNil(t, tbl)
	require.Equal(t, "envoyA", tbl.EnvoyID)
}

func TestAtomicTable_StoreLoad(t *testing.T) {
	a := NewAtomicTable("envoyA")
	a.Store(&Table{
		EnvoyID: "envoyA",
		Routes:  map[string]Route{"svc": {TargetCluster: "svc", NextHopLocalCluster: "peer_b"}},
	})
	r, ok := a.Load().Lookup("svc")
	require.True(t, ok)
	require.Equal(t, "peer_b", r.NextHopLocalCluster)
	_, ok = a.Load().Lookup("missing")
	require.False(t, ok)
}

func TestAtomicTable_ConcurrentSwap(_ *testing.T) {
	a := NewAtomicTable("envoyA")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				_ = a.Load()
			}
		}()
	}
	for i := 0; i < 100; i++ {
		a.Store(&Table{EnvoyID: "envoyA", Routes: map[string]Route{}})
	}
	wg.Wait()
}

func TestTable_LookupNil(t *testing.T) {
	var t1 *Table
	_, ok := t1.Lookup("anything")
	require.False(t, ok)
}
