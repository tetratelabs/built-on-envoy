// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package fault

import (
	"math"
	"testing"
	"time"
)

func TestProbabilityDistribution_Sample(t *testing.T) {
	percentiles := []Percentile{
		{Quantile: 0.0, Duration: 0},
		{Quantile: 0.50, Duration: 10 * time.Millisecond},
		{Quantile: 0.90, Duration: 50 * time.Millisecond},
		{Quantile: 0.99, Duration: 200 * time.Millisecond},
		{Quantile: 1.0, Duration: 1 * time.Second},
	}

	dist := NewProbabilityDistribution(percentiles)

	const numSamples = 100000
	var samples []time.Duration
	for i := 0; i < numSamples; i++ {
		samples = append(samples, dist.Sample())
	}

	sortDurations(samples)

	p50 := samples[int(float64(numSamples)*0.50)]
	p90 := samples[int(float64(numSamples)*0.90)]
	p99 := samples[int(float64(numSamples)*0.99)]

	t.Logf("Actual p50: %v (expected ~10ms)", p50)
	t.Logf("Actual p90: %v (expected ~50ms)", p90)
	t.Logf("Actual p99: %v (expected ~200ms)", p99)

	assertWithinTolerance(t, "p50", p50, 10*time.Millisecond, 0.3)
	assertWithinTolerance(t, "p90", p90, 50*time.Millisecond, 0.3)
	assertWithinTolerance(t, "p99", p99, 200*time.Millisecond, 0.3)
}

func TestProbabilityDistribution_SampleWithValue(t *testing.T) {
	percentiles := []Percentile{
		{Quantile: 0.0, Duration: 0},
		{Quantile: 0.50, Duration: 10 * time.Millisecond},
		{Quantile: 1.0, Duration: 200 * time.Millisecond},
	}

	dist := NewProbabilityDistribution(percentiles)

	tests := []struct {
		name      string
		value     float64
		expected  time.Duration
		tolerance float64
	}{
		{"at zero", 0.0, 0, 0.01},
		{"at p50", 0.50, 10 * time.Millisecond, 0.01},
		{"at p100", 1.0, 200 * time.Millisecond, 0.01},
		{"between p0 and p50 (at 0.25)", 0.25, 5 * time.Millisecond, 0.05},
		{"between p50 and p100 (at 0.75)", 0.75, 105 * time.Millisecond, 0.05},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := dist.SampleWithValue(tt.value)
			if tt.expected == 0 {
				if result != 0 {
					t.Errorf("expected 0, got %v", result)
				}
				return
			}
			diff := math.Abs(float64(result-tt.expected)) / float64(tt.expected)
			if diff > tt.tolerance {
				t.Errorf("expected ~%v, got %v (diff: %.2f%%)", tt.expected, result, diff*100)
			}
		})
	}
}

func TestStatefulProbabilityDistribution_Sample(t *testing.T) {
	percentiles := []Percentile{
		{Quantile: 0.0, Duration: 0},
		{Quantile: 0.50, Duration: 10 * time.Millisecond},
		{Quantile: 0.90, Duration: 50 * time.Millisecond},
		{Quantile: 0.99, Duration: 200 * time.Millisecond},
		{Quantile: 1.0, Duration: 1 * time.Second},
	}

	const resolution = 10000
	dist := NewStatefulProbabilityDistribution(percentiles, resolution)

	var samples []time.Duration
	for i := 0; i < resolution; i++ {
		samples = append(samples, dist.Sample())
	}

	sortDurations(samples)

	p50 := samples[int(float64(resolution)*0.50)]
	p90 := samples[int(float64(resolution)*0.90)]
	p99 := samples[int(float64(resolution)*0.99)]

	t.Logf("Stateful p50: %v (expected ~10ms)", p50)
	t.Logf("Stateful p90: %v (expected ~50ms)", p90)
	t.Logf("Stateful p99: %v (expected ~200ms)", p99)

	assertWithinTolerance(t, "p50", p50, 10*time.Millisecond, 0.15)
	assertWithinTolerance(t, "p90", p90, 50*time.Millisecond, 0.15)
	assertWithinTolerance(t, "p99", p99, 200*time.Millisecond, 0.15)
}

func TestStatefulProbabilityDistribution_Reshuffle(t *testing.T) {
	percentiles := []Percentile{
		{Quantile: 0.0, Duration: 0},
		{Quantile: 0.50, Duration: 10 * time.Millisecond},
		{Quantile: 1.0, Duration: 100 * time.Millisecond},
	}

	const resolution = 100
	dist := NewStatefulProbabilityDistribution(percentiles, resolution)

	// Sample more than resolution to trigger reshuffle.
	for i := 0; i < resolution+50; i++ {
		s := dist.Sample()
		if s < 0 {
			t.Fatal("negative duration sampled")
		}
	}
}

func TestResponseDistribution_StatusWeighting(t *testing.T) {
	statusDists := []StatusDistribution{
		{
			Status:     200,
			Resolution: 900,
			Distribution: map[string]string{
				"p0.0":   "1ms",
				"p100.0": "10ms",
			},
		},
		{
			Status:     503,
			Resolution: 100,
			Distribution: map[string]string{
				"p0.0":   "100ms",
				"p100.0": "200ms",
			},
		},
	}

	rd, err := NewResponseDistribution(statusDists)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	const numSamples = 10000
	statusCounts := make(map[int]int)
	for i := 0; i < numSamples; i++ {
		sample := rd.Sample()
		statusCounts[sample.Status]++
	}

	// Expect ~90% status 200, ~10% status 503.
	ratio200 := float64(statusCounts[200]) / float64(numSamples)
	ratio503 := float64(statusCounts[503]) / float64(numSamples)

	t.Logf("Status 200: %.1f%% (expected ~90%%)", ratio200*100)
	t.Logf("Status 503: %.1f%% (expected ~10%%)", ratio503*100)

	if math.Abs(ratio200-0.9) > 0.05 {
		t.Errorf("expected ~90%% status 200, got %.1f%%", ratio200*100)
	}
	if math.Abs(ratio503-0.1) > 0.05 {
		t.Errorf("expected ~10%% status 503, got %.1f%%", ratio503*100)
	}
}

func TestResponseDistribution_DurationRanges(t *testing.T) {
	statusDists := []StatusDistribution{
		{
			Status:     200,
			Resolution: 1000,
			Distribution: map[string]string{
				"p0.0":   "5ms",
				"p50.0":  "10ms",
				"p100.0": "50ms",
			},
		},
	}

	rd, err := NewResponseDistribution(statusDists)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	const numSamples = 1000
	for i := 0; i < numSamples; i++ {
		sample := rd.Sample()
		if sample.Status != 200 {
			t.Errorf("expected status 200, got %d", sample.Status)
		}
		if sample.Duration < 0 {
			t.Errorf("negative duration: %v", sample.Duration)
		}
	}
}

func TestResponseDistribution_FlatDistribution(t *testing.T) {
	// A flat distribution where all percentiles have the same value.
	statusDists := []StatusDistribution{
		{
			Status:     200,
			Resolution: 100,
			Distribution: map[string]string{
				"p0.0":   "50ms",
				"p100.0": "50ms",
			},
		},
	}

	rd, err := NewResponseDistribution(statusDists)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	const numSamples = 100
	for i := 0; i < numSamples; i++ {
		sample := rd.Sample()
		if sample.Duration != 50*time.Millisecond {
			t.Errorf("flat distribution should always return 50ms, got %v", sample.Duration)
		}
	}
}

func TestLoadBasedResponseDistribution_Healthy(t *testing.T) {
	healthyDists := []StatusDistribution{
		{
			Status:     200,
			Resolution: 1000,
			Distribution: map[string]string{
				"p0.0":   "1ms",
				"p100.0": "10ms",
			},
		},
	}
	tippingDists := []StatusDistribution{
		{
			Status:     503,
			Resolution: 1000,
			Distribution: map[string]string{
				"p0.0":   "500ms",
				"p100.0": "5s",
			},
		},
	}

	lb, err := NewLoadBasedResponseDistribution(healthyDists, 100, tippingDists, 500, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Sample at healthy RPS — should get all 200s.
	const numSamples = 100
	for i := 0; i < numSamples; i++ {
		sample := lb.Sample(50) // below healthy threshold
		if sample.Status != 200 {
			t.Errorf("at healthy RPS, expected status 200, got %d", sample.Status)
		}
		if sample.Duration > 10*time.Millisecond {
			t.Errorf("at healthy RPS, expected duration <= 10ms, got %v", sample.Duration)
		}
	}
}

func TestLoadBasedResponseDistribution_TippingPoint(t *testing.T) {
	healthyDists := []StatusDistribution{
		{
			Status:     200,
			Resolution: 1000,
			Distribution: map[string]string{
				"p0.0":   "1ms",
				"p100.0": "10ms",
			},
		},
	}
	tippingDists := []StatusDistribution{
		{
			Status:     503,
			Resolution: 1000,
			Distribution: map[string]string{
				"p0.0":   "100ms",
				"p100.0": "500ms",
			},
		},
	}

	lb, err := NewLoadBasedResponseDistribution(healthyDists, 100, tippingDists, 500, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Sample at tipping point RPS — should get all 503s.
	const numSamples = 100
	for i := 0; i < numSamples; i++ {
		sample := lb.Sample(600) // above tipping point
		if sample.Status != 503 {
			t.Errorf("at tipping RPS, expected status 503, got %d", sample.Status)
		}
	}
}

func TestLoadBasedResponseDistribution_GreyZone(t *testing.T) {
	healthyDists := []StatusDistribution{
		{
			Status:     200,
			Resolution: 1000,
			Distribution: map[string]string{
				"p0.0":   "1ms",
				"p100.0": "10ms",
			},
		},
	}
	tippingDists := []StatusDistribution{
		{
			Status:     503,
			Resolution: 1000,
			Distribution: map[string]string{
				"p0.0":   "100ms",
				"p100.0": "500ms",
			},
		},
	}

	lb, err := NewLoadBasedResponseDistribution(healthyDists, 100, tippingDists, 500, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Sample in the grey zone — should get a mix of 200 and 503.
	const numSamples = 1000
	got200 := 0
	got503 := 0
	for i := 0; i < numSamples; i++ {
		sample := lb.Sample(300) // middle of grey zone (50% position)
		switch sample.Status {
		case 200:
			got200++
		case 503:
			got503++
		default:
			t.Errorf("unexpected status %d", sample.Status)
		}
	}

	// At 50% grey position, expect roughly 50/50 split.
	ratio200 := float64(got200) / float64(numSamples)
	t.Logf("Grey zone (50%%): 200=%d (%.1f%%), 503=%d (%.1f%%)", got200, ratio200*100, got503, (1-ratio200)*100)

	if ratio200 < 0.3 || ratio200 > 0.7 {
		t.Errorf("expected roughly 50/50 split in grey zone, got %.1f%% 200s", ratio200*100)
	}
}

func TestLoadBasedResponseDistribution_GreyZoneWithPenalty(t *testing.T) {
	healthyDists := []StatusDistribution{
		{
			Status:     200,
			Resolution: 1000,
			Distribution: map[string]string{
				"p0.0":   "1ms",
				"p100.0": "5ms",
			},
		},
	}
	tippingDists := []StatusDistribution{
		{
			Status:     503,
			Resolution: 1000,
			Distribution: map[string]string{
				"p0.0":   "50ms",
				"p100.0": "100ms",
			},
		},
	}

	gz := &GreyZoneConfig{
		PenaltyBase:            "20ms",
		SpikeThreshold:         0.8,
		SpikePenaltyDuration:   "2s",
		SpikePenaltyMultiplier: 3.0,
		RecoveryRate:           0.5,
	}

	lb, err := NewLoadBasedResponseDistribution(healthyDists, 100, tippingDists, 500, gz)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Sample in grey zone with penalty — durations should be increased.
	const numSamples = 100
	var totalDuration time.Duration
	for i := 0; i < numSamples; i++ {
		sample := lb.Sample(300) // 50% grey zone position
		totalDuration += sample.Duration
	}

	avgDuration := totalDuration / time.Duration(numSamples)
	t.Logf("Average duration with grey zone penalty: %v", avgDuration)

	// With a 50% grey position and 20ms penalty base, expect penalty ~10ms added.
	// Base durations are 1-5ms (healthy) or 50-100ms (tipping).
	// Average without penalty would be roughly in the range of these.
	// With penalty it should be noticeably above the minimum.
	if avgDuration < 5*time.Millisecond {
		t.Errorf("expected grey zone penalty to increase average duration, got %v", avgDuration)
	}
}

func TestProbabilityDistribution_AllSamplesPositive(t *testing.T) {
	percentiles := []Percentile{
		{Quantile: 0.0, Duration: 0},
		{Quantile: 0.50, Duration: 5 * time.Millisecond},
		{Quantile: 1.0, Duration: 500 * time.Millisecond},
	}

	dist := NewProbabilityDistribution(percentiles)

	for i := 0; i < 10000; i++ {
		s := dist.Sample()
		if s < 0 {
			t.Fatalf("negative sample at iteration %d: %v", i, s)
		}
	}
}

func sortDurations(d []time.Duration) {
	for i := 1; i < len(d); i++ {
		for j := i; j > 0 && d[j] < d[j-1]; j-- {
			d[j], d[j-1] = d[j-1], d[j]
		}
	}
}

func assertWithinTolerance(t *testing.T, name string, actual, expected time.Duration, tolerance float64) {
	t.Helper()
	if expected == 0 {
		if actual != 0 {
			t.Errorf("%s: expected 0, got %v", name, actual)
		}
		return
	}
	diff := math.Abs(float64(actual-expected)) / float64(expected)
	if diff > tolerance {
		t.Errorf("%s: expected ~%v, got %v (diff: %.2f%%, tolerance: %.2f%%)", name, expected, actual, diff*100, tolerance*100)
	}
}
