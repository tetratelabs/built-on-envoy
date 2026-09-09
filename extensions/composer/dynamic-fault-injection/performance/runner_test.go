// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package performance

import (
	"fmt"
	"os"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/dynamic-fault-injection/internal/fault"
)

// TestMain makes the process-level runner test-only while still allowing it to
// be executed as a compiled Go test binary for CPU and memory measurements.
// With no DFI_PERFORMANCE_MODE, this package behaves like a normal test
// package and runs no tests.
func TestMain(m *testing.M) {
	mode := os.Getenv("DFI_PERFORMANCE_MODE")
	if mode == "" {
		os.Exit(m.Run())
	}

	if mode == "memory" {
		runMemory(
			os.Getenv("DFI_PERFORMANCE_DISTRIBUTION"),
			os.Getenv("DFI_PERFORMANCE_ENDPOINTS"),
			os.Getenv("DFI_PERFORMANCE_RESOLUTION"),
		)
		return
	}
	runSampling(mode)
}

func runSampling(mode string) {
	dist, err := fault.NewResponseDistributionWithMode([]fault.StatusDistribution{{
		Status:       200,
		Resolution:   1000,
		Distribution: map[string]string{"p0.0": "1ms", "p50.0": "10ms", "p100.0": "100ms"},
	}, {
		Status:       503,
		Resolution:   100,
		Distribution: map[string]string{"p0.0": "1ms", "p50.0": "10ms", "p100.0": "100ms"},
	}}, mode)
	if err != nil {
		panic(err)
	}
	runtime.GOMAXPROCS(16)
	const workers = 16
	const duration = 3 * time.Second
	var ready sync.WaitGroup
	ready.Add(workers)
	start := make(chan struct{})
	counts := make([]uint64, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for worker := range workers {
		go func(worker int) {
			defer wg.Done()
			ready.Done()
			<-start
			deadline := time.Now().Add(duration)
			var count uint64
			for {
				for i := 0; i < 1024; i++ {
					dist.Sample()
					count++
				}
				if time.Now().After(deadline) {
					break
				}
			}
			counts[worker] = count
		}(worker)
	}
	ready.Wait()
	started := time.Now()
	close(start)
	wg.Wait()
	elapsed := time.Since(started)
	var samples uint64
	for _, count := range counts {
		samples += count
	}
	fmt.Printf("mode=%s samples=%d elapsed=%s samples_per_second=%.0f\n", mode, samples, elapsed, float64(samples)/elapsed.Seconds())
}
