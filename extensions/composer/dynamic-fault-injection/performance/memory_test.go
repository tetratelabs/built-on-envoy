// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package performance

import (
	"fmt"
	"runtime"
	"strconv"
	"time"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/dynamic-fault-injection/internal/fault"
)

func runMemory(mode, endpointCountArg, resolutionArg string) {
	endpointCount, err := strconv.Atoi(endpointCountArg)
	if err != nil || endpointCount < 1 {
		panic("endpoint-count must be a positive integer")
	}
	resolution, err := strconv.Atoi(resolutionArg)
	if err != nil || resolution < 1 {
		panic("resolution must be a positive integer")
	}

	statusDists := []fault.StatusDistribution{
		{Status: 200, Resolution: resolution, Distribution: benchmarkDistribution()},
		{Status: 503, Resolution: benchmarkMax(resolution/10, 1), Distribution: benchmarkDistribution()},
	}
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	start := time.Now()
	dists := make([]*fault.ResponseDistribution, endpointCount)
	for i := range dists {
		dists[i], err = fault.NewResponseDistributionWithMode(statusDists, mode)
		if err != nil {
			panic(err)
		}
	}
	buildDuration := time.Since(start)

	runtime.GC()
	// Keep the distributions live across the measurement. The compiler cannot
	// discard this use, and the retained heap reflects the factory's ownership.
	if dists[len(dists)-1] == nil {
		panic("empty distribution set")
	}
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	fmt.Printf("mode=%s endpoints=%d resolution=%d build=%s heap_alloc_before=%d heap_alloc_after=%d heap_inuse_before=%d heap_inuse_after=%d heap_sys_before=%d heap_sys_after=%d\n",
		mode, endpointCount, resolution, buildDuration,
		before.HeapAlloc, after.HeapAlloc,
		before.HeapInuse, after.HeapInuse,
		before.HeapSys, after.HeapSys)
}

func benchmarkDistribution() map[string]string {
	return map[string]string{
		"p0.0":   "1ms",
		"p50.0":  "10ms",
		"p100.0": "100ms",
	}
}

func benchmarkMax(a, b int) int {
	if a > b {
		return a
	}
	return b
}
