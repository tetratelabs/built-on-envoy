// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package fault

import (
	"fmt"
	"testing"
	"time"
)

func benchmarkPercentiles(count int) []Percentile {
	percentiles := make([]Percentile, 0, count)
	for i := 0; i < count; i++ {
		q := float64(i) / float64(count-1)
		percentiles = append(percentiles, Percentile{
			Quantile: q,
			Duration: time.Duration(float64(time.Second) * (0.001 + q*q)),
		})
	}
	return percentiles
}

func benchmarkStatusDistributions(resolution, percentileCount int) []StatusDistribution {
	percentiles := benchmarkPercentiles(percentileCount)
	distribution := make(map[string]string, len(percentiles))
	for _, p := range percentiles {
		distribution[fmt.Sprintf("p%.1f", p.Quantile*100)] = p.Duration.String()
	}
	return []StatusDistribution{
		{Status: 200, Resolution: resolution, Distribution: distribution},
		{Status: 503, Resolution: max(resolution/10, 1), Distribution: distribution},
	}
}

func BenchmarkDurationSample(b *testing.B) {
	for _, mode := range []string{ProbabilityDistributionStateful, ProbabilityDistributionStateless} {
		for _, resolution := range []int{10, 100, 1000, 10000} {
			for _, percentileCount := range []int{2, 5, 10} {
				name := fmt.Sprintf("%s/resolution=%d/percentiles=%d", mode, resolution, percentileCount)
				b.Run(name, func(b *testing.B) {
					dist, err := newDurationDistribution(benchmarkPercentiles(percentileCount), resolution, mode)
					if err != nil {
						b.Fatal(err)
					}
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						dist.Sample()
					}
				})
			}
		}
	}
}

func BenchmarkDurationSampleParallel(b *testing.B) {
	for _, mode := range []string{ProbabilityDistributionStateful, ProbabilityDistributionStateless} {
		b.Run(mode+"/resolution=1000/percentiles=5", func(b *testing.B) {
			b.StopTimer()
			dist, err := newDurationDistribution(benchmarkPercentiles(5), 1000, mode)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.StartTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					dist.Sample()
				}
			})
		})
	}
}

// BenchmarkDurationSampleCycle measures one complete stateful cycle at the
// high resolutions used for construction and retained-memory analysis. The
// warm-up cycle is outside the timer so the timed stateful cycle includes the
// periodic reshuffle that follows it.
func BenchmarkDurationSampleCycle(b *testing.B) {
	for _, mode := range []string{ProbabilityDistributionStateful, ProbabilityDistributionStateless} {
		for _, resolution := range []int{100000, 1000000} {
			b.Run(fmt.Sprintf("%s/resolution=%d", mode, resolution), func(b *testing.B) {
				dist, err := newDurationDistribution(benchmarkPercentiles(5), resolution, mode)
				if err != nil {
					b.Fatal(err)
				}
				for i := 0; i < resolution; i++ {
					dist.Sample()
				}
				b.ReportAllocs()
				b.SetBytes(int64(resolution))
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					for j := 0; j < resolution; j++ {
						dist.Sample()
					}
				}
			})
		}
	}
}

func BenchmarkResponseSampleParallel(b *testing.B) {
	for _, mode := range []string{ProbabilityDistributionStateful, ProbabilityDistributionStateless} {
		for _, resolution := range []int{10, 100, 1000, 10000} {
			b.Run(fmt.Sprintf("%s/resolution=%d/percentiles=5", mode, resolution), func(b *testing.B) {
				b.StopTimer()
				dist, err := NewResponseDistributionWithMode(benchmarkStatusDistributions(resolution, 5), mode)
				if err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.StartTimer()
				b.RunParallel(func(pb *testing.PB) {
					for pb.Next() {
						dist.Sample()
					}
				})
			})
		}
	}
}

func BenchmarkLoadBasedSampleParallel(b *testing.B) {
	for _, mode := range []string{ProbabilityDistributionStateful, ProbabilityDistributionStateless} {
		for _, load := range []struct {
			name    string
			rps     float64
			penalty bool
		}{
			{name: "healthy", rps: 0},
			{name: "grey-zone", rps: 150, penalty: false},
			{name: "grey-zone-penalty", rps: 150, penalty: true},
			{name: "tipping", rps: 250},
		} {
			b.Run(fmt.Sprintf("%s/%s", mode, load.name), func(b *testing.B) {
				b.StopTimer()
				dists := benchmarkStatusDistributions(1000, 5)
				var greyZone *GreyZoneConfig
				if load.penalty {
					greyZone = &GreyZoneConfig{
						PenaltyBase:            "10ms",
						SpikeThreshold:         0.5,
						SpikePenaltyDuration:   "5s",
						SpikePenaltyMultiplier: 2,
						RecoveryRate:           0.5,
					}
				}
				lb, err := NewLoadBasedResponseDistributionWithMode(dists, 100, dists, 200, greyZone, mode)
				if err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.StartTimer()
				b.RunParallel(func(pb *testing.PB) {
					for pb.Next() {
						lb.Sample(load.rps)
					}
				})
			})
		}
	}
}

func BenchmarkDistributionConstruction(b *testing.B) {
	for _, mode := range []string{ProbabilityDistributionStateful, ProbabilityDistributionStateless} {
		for _, resolution := range []int{1, 10, 100, 1000, 10000, 100000, 1000000} {
			b.Run(fmt.Sprintf("%s/resolution=%d", mode, resolution), func(b *testing.B) {
				b.StopTimer()
				dists := benchmarkStatusDistributions(resolution, 5)
				b.ReportAllocs()
				b.StartTimer()
				for i := 0; i < b.N; i++ {
					dist, err := NewResponseDistributionWithMode(dists, mode)
					if err != nil {
						b.Fatal(err)
					}
					if len(dist.entries) == 0 {
						b.Fatal("empty distribution")
					}
				}
			})
		}
	}
}
