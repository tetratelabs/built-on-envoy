// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package impl

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/fake"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/dynamic-fault-injection/internal/fault"
)

func BenchmarkActiveRequestTracking(b *testing.B) {
	for _, operation := range []string{"load-only", "entry-exit"} {
		b.Run(operation, func(b *testing.B) {
			activeRequests.Store(0)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				value := activeRequests.Load()
				if value < 0 {
					b.Fatal("negative active request count")
				}
				if operation == "entry-exit" {
					activeRequests.Add(1)
					activeRequests.Add(-1)
				}
			}
		})
	}
}

func BenchmarkActiveRequestTrackingParallel(b *testing.B) {
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			value := activeRequests.Load()
			if value < 0 {
				panic("negative active request count")
			}
			activeRequests.Add(1)
			activeRequests.Add(-1)
		}
	})
}

func BenchmarkResponseHeaderWrites(b *testing.B) {
	for _, diagnostic := range []bool{false, true} {
		b.Run(fmt.Sprintf("diagnostic=%t", diagnostic), func(b *testing.B) {
			headers := fake.NewFakeHeaderMap(map[string][]string{"x-existing": {"value"}})
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				headers.Set("x-fault-injected-delay", "10ms")
				headers.Set("x-fault-actual-upstream", "1ms")
				headers.Set("x-fault-status", "200")
				headers.Set("x-fault-requests-in-flight", strconv.Itoa(i))
				if diagnostic {
					headers.Set("x-fault-worker-index", strconv.Itoa(i&3))
				}
			}
		})
	}
}

func BenchmarkFactoryConstruction(b *testing.B) {
	for _, mode := range []string{fault.ProbabilityDistributionStateful, fault.ProbabilityDistributionStateless} {
		for _, endpointCount := range []int{1, 5, 10, 20} {
			for _, resolution := range []int{10, 100, 1000, 10000, 100000, 1000000} {
				b.Run(fmt.Sprintf("%s/endpoints=%d/resolution=%d", mode, endpointCount, resolution), func(b *testing.B) {
					b.StopTimer()
					config := benchmarkFactoryConfig(mode, endpointCount, resolution)
					b.ReportAllocs()
					b.StartTimer()
					for i := 0; i < b.N; i++ {
						factory, err := buildFilterFactory(config)
						if err != nil {
							b.Fatal(err)
						}
						if len(factory.endpoints) == 0 {
							b.Fatal("empty factory")
						}
					}
				})
			}
		}
	}
}

func BenchmarkDirectSampling(b *testing.B) {
	for _, mode := range []string{fault.ProbabilityDistributionStateful, fault.ProbabilityDistributionStateless} {
		b.Run(mode, func(b *testing.B) {
			b.StopTimer()
			factory, err := buildFilterFactory(benchmarkDirectFactoryConfig(mode))
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.StartTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					if _, ok := factory.sample(0); !ok {
						panic("missing direct distribution")
					}
				}
			})
		})
	}
}

func benchmarkFactoryConfig(mode string, endpointCount, resolution int) []byte {
	config := fmt.Sprintf("probability_distribution: %s\nendpoints:\n", mode)
	for i := 0; i < endpointCount; i++ {
		config += fmt.Sprintf(`  - match:
      prefix: "/bench/%d/"
    responses:
      - status: 200
        resolution: %d
        distribution:
          p0.0: "1ms"
          p50.0: "10ms"
          p100.0: "100ms"
`, i, resolution)
	}
	return []byte(config)
}

func benchmarkDirectFactoryConfig(mode string) []byte {
	return []byte(fmt.Sprintf(`probability_distribution: %s
responses:
  - status: 200
    resolution: 1000
    distribution:
      p0.0: "1ms"
      p50.0: "10ms"
      p100.0: "100ms"
`, mode))
}
