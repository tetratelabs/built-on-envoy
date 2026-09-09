// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package impl

import (
	"fmt"
	"testing"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/fake"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/dynamic-fault-injection/internal/fault"
)

func BenchmarkEndpointMatchedSampling(b *testing.B) {
	for _, mode := range []string{fault.ProbabilityDistributionStateful, fault.ProbabilityDistributionStateless} {
		for _, endpointCount := range []int{1, 20} {
			b.Run(fmt.Sprintf("%s/endpoints=%d", mode, endpointCount), func(b *testing.B) {
				b.StopTimer()
				factory, err := buildFilterFactory(endpointBenchmarkConfig(mode, endpointCount))
				if err != nil {
					b.Fatal(err)
				}
				headers := fake.NewFakeHeaderMap(map[string][]string{
					":path": {endpointBenchmarkPath(endpointCount)},
				})
				path := headers.GetOne(":path").ToUnsafeString()
				adapter := &headerMapAdapter{headers: headers}
				b.ReportAllocs()
				b.StartTimer()
				b.RunParallel(func(pb *testing.PB) {
					for pb.Next() {
						for i := range factory.endpoints {
							ep := &factory.endpoints[i]
							if fault.MatchRoute(ep.match, path, adapter) {
								if ep.distribution.Sample().Status == 0 {
									panic("invalid status")
								}
								break
							}
						}
					}
				})
			})
		}
	}
}

func endpointBenchmarkConfig(mode string, endpointCount int) []byte {
	config := fmt.Sprintf("probability_distribution: %s\nendpoints:\n", mode)
	for i := 0; i < endpointCount; i++ {
		prefix := fmt.Sprintf("/bench/%d/", i)
		if endpointCount == 1 {
			prefix = "/"
		}
		config += fmt.Sprintf(`  - match:
      prefix: "%s"
    responses:
      - status: 200
        resolution: 1000
        distribution:
          p0.0: "1ms"
          p50.0: "10ms"
          p100.0: "100ms"
`, prefix)
	}
	return []byte(config)
}

func endpointBenchmarkPath(endpointCount int) string {
	if endpointCount == 1 {
		return "/resource"
	}
	return fmt.Sprintf("/bench/%d/resource", endpointCount-1)
}
