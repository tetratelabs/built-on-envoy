// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// These tests verify fault distributions without relying on random percentile estimates.
package fault

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/dynamic-fault-injection/internal/config"
)

func TestProbabilityDistribution_Sample(t *testing.T) {
	percentiles := []config.Percentile{
		{Quantile: 0.0, Duration: 0},
		{Quantile: 0.50, Duration: 10 * time.Millisecond},
		{Quantile: 0.90, Duration: 50 * time.Millisecond},
		{Quantile: 0.99, Duration: 200 * time.Millisecond},
		{Quantile: 1.0, Duration: 1 * time.Second},
	}

	dist := NewProbabilityDistribution(percentiles)

	const numSamples = 10000
	// Binomial(10000, q) bounds allocate at most 1e-9/6 failure probability to each of six tails.
	checks := []struct {
		name       string
		boundary   time.Duration
		lowerBound int
		upperBound int
		count      int
	}{
		{name: "p50", boundary: 10 * time.Millisecond, lowerBound: 4686, upperBound: 5314},
		{name: "p90", boundary: 50 * time.Millisecond, lowerBound: 8807, upperBound: 9183},
		{name: "p99", boundary: 200 * time.Millisecond, lowerBound: 9832, upperBound: 9956},
	}
	for range numSamples {
		sample := dist.Sample()
		if sample < 0 || sample > time.Second {
			t.Fatalf("sample %s outside [0, %s]", sample, time.Second)
		}
		for i := range checks {
			if sample < checks[i].boundary {
				checks[i].count++
			}
		}
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			t.Logf("%d/%d samples below %s", check.count, numSamples, check.boundary)
			require.GreaterOrEqual(t, check.count, check.lowerBound)
			require.LessOrEqual(t, check.count, check.upperBound)
		})
	}
}

func TestProbabilityDistribution_SampleWithValue(t *testing.T) {
	percentiles := []config.Percentile{
		{Quantile: 0.0, Duration: 0},
		{Quantile: 0.50, Duration: 10 * time.Millisecond},
		{Quantile: 0.90, Duration: 50 * time.Millisecond},
		{Quantile: 0.99, Duration: 200 * time.Millisecond},
		{Quantile: 1.0, Duration: 1 * time.Second},
	}

	dist := NewProbabilityDistribution(percentiles)

	tests := []struct {
		name     string
		value    float64
		expected time.Duration
	}{
		{name: "at zero", value: 0.0, expected: 0},
		{name: "at p50", value: 0.50, expected: 10 * time.Millisecond},
		{name: "at p90", value: 0.90, expected: 50 * time.Millisecond},
		{name: "at p99", value: 0.99, expected: 200 * time.Millisecond},
		{name: "at p100", value: 1.0, expected: time.Second},
		{name: "between p0 and p50", value: 0.25, expected: 5 * time.Millisecond},
		{name: "between p50 and p90", value: 0.70, expected: 30 * time.Millisecond},
		{name: "between p90 and p99", value: 0.945, expected: 125 * time.Millisecond},
		{name: "at the former p99 tolerance limit", value: 0.99075, expected: 260 * time.Millisecond},
		{name: "halfway through the upper tail", value: 0.995, expected: 600 * time.Millisecond},
		{name: "near the maximum", value: 0.999, expected: 920 * time.Millisecond},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := dist.SampleWithValue(tt.value)
			// Floating-point interpolation can truncate the result by one nanosecond.
			require.InDelta(t, int64(tt.expected), int64(result), 1)
		})
	}
}

func TestStatefulProbabilityDistribution_Sample(t *testing.T) {
	percentiles := []config.Percentile{
		{Quantile: 0.0, Duration: 0},
		{Quantile: 0.50, Duration: 10 * time.Millisecond},
		{Quantile: 0.90, Duration: 50 * time.Millisecond},
		{Quantile: 0.99, Duration: 200 * time.Millisecond},
		{Quantile: 1.0, Duration: 1 * time.Second},
	}

	const resolution = 10000
	dist := NewStatefulProbabilityDistribution(percentiles, resolution)

	var samples []time.Duration
	for range resolution {
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
	percentiles := []config.Percentile{
		{Quantile: 0.0, Duration: 0},
		{Quantile: 0.50, Duration: 10 * time.Millisecond},
		{Quantile: 1.0, Duration: 100 * time.Millisecond},
	}

	const resolution = 100
	dist := NewStatefulProbabilityDistribution(percentiles, resolution)

	// Sample more than resolution to trigger reshuffle.
	for range resolution + 50 {
		s := dist.Sample()
		if s < 0 {
			t.Fatal("negative duration sampled")
		}
	}
}

func TestResponseDistribution_StatusWeighting(t *testing.T) {
	statusDists := []config.StatusDistribution{
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

	rd, err := NewResponseDistributionWithMode(statusDists, config.ProbabilityDistributionStateful)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	const numSamples = 10000
	statusCounts := make(map[int]int)
	for range numSamples {
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

func TestResponseDistribution_DuplicateStatusesKeepSelectedLocalResponse(t *testing.T) {
	for _, mode := range []string{config.ProbabilityDistributionStateful, config.ProbabilityDistributionStateless} {
		t.Run(mode, func(t *testing.T) {
			firstBody, secondBody := "first", "second"
			distributions := []config.StatusDistribution{
				{Status: 503, Resolution: 100, Distribution: map[string]string{"p0.0": "1ms", "p100.0": "2ms"}, LocalResponse: &config.LocalResponseConfig{Body: &firstBody}},
				{Status: 503, Resolution: 100, Distribution: map[string]string{"p0.0": "100ms", "p100.0": "200ms"}, LocalResponse: &config.LocalResponseConfig{Body: &secondBody}},
			}
			distribution, err := NewResponseDistributionWithMode(distributions, mode)
			require.NoError(t, err)
			seen := map[string]bool{}
			for range 1000 {
				sample := distribution.Sample()
				require.Equal(t, 503, sample.Status)
				require.NotNil(t, sample.LocalResponse)
				require.NotNil(t, sample.LocalResponse.Body)
				body := *sample.LocalResponse.Body
				seen[body] = true
				if sample.Duration < 10*time.Millisecond {
					require.Equal(t, "first", body)
				} else {
					require.Equal(t, "second", body)
				}
			}
			require.True(t, seen["first"])
			require.True(t, seen["second"])
		})
	}
}

func TestResponseDistribution_DurationRanges(t *testing.T) {
	statusDists := []config.StatusDistribution{
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

	rd, err := NewResponseDistributionWithMode(statusDists, config.ProbabilityDistributionStateful)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	const numSamples = 1000
	for range numSamples {
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
	statusDists := []config.StatusDistribution{
		{
			Status:     200,
			Resolution: 100,
			Distribution: map[string]string{
				"p0.0":   "50ms",
				"p100.0": "50ms",
			},
		},
	}

	rd, err := NewResponseDistributionWithMode(statusDists, config.ProbabilityDistributionStateful)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	const numSamples = 100
	for range numSamples {
		sample := rd.Sample()
		if sample.Duration != 50*time.Millisecond {
			t.Errorf("flat distribution should always return 50ms, got %v", sample.Duration)
		}
	}
}

func TestResponseDistribution_StatelessMode(t *testing.T) {
	statusDists := []config.StatusDistribution{
		{
			Status:     200,
			Resolution: 100,
			Distribution: map[string]string{
				"p0.0":   "5ms",
				"p100.0": "5ms",
			},
		},
	}

	rd, err := NewResponseDistributionWithMode(statusDists, config.ProbabilityDistributionStateless)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for range 100 {
		sample := rd.Sample()
		if sample.Duration != 5*time.Millisecond {
			t.Fatalf("expected fixed 5ms sample, got %v", sample.Duration)
		}
	}
}

func TestResponseDistribution_InvalidMode(t *testing.T) {
	statusDists := []config.StatusDistribution{
		{
			Status:     200,
			Resolution: 100,
			Distribution: map[string]string{
				"p0.0":   "1ms",
				"p100.0": "10ms",
			},
		},
	}

	_, err := NewResponseDistributionWithMode(statusDists, "invalid")
	if err == nil {
		t.Fatal("expected error for invalid probability distribution mode")
	}
}

func TestLoadBasedResponseDistribution_Healthy(t *testing.T) {
	healthyDists := []config.StatusDistribution{
		{
			Status:     200,
			Resolution: 1000,
			Distribution: map[string]string{
				"p0.0":   "1ms",
				"p100.0": "10ms",
			},
		},
	}
	tippingDists := []config.StatusDistribution{
		{
			Status:     503,
			Resolution: 1000,
			Distribution: map[string]string{
				"p0.0":   "500ms",
				"p100.0": "5s",
			},
		},
	}

	lb, err := NewLoadBasedResponseDistributionWithMode(healthyDists, 100, tippingDists, 500, nil, config.ProbabilityDistributionStateful)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Sample at healthy RPS — should get all 200s.
	const numSamples = 100
	for range numSamples {
		sample := lb.Sample(50) // below healthy threshold
		if sample.Status != 200 {
			t.Errorf("at healthy RPS, expected status 200, got %d", sample.Status)
		}
		if sample.Duration > 10*time.Millisecond {
			t.Errorf("at healthy RPS, expected duration <= 10ms, got %v", sample.Duration)
		}
	}
}

func TestLoadBasedResponseDistribution_CarriesTierLocalResponse(t *testing.T) {
	healthyBody, tippingBody := "healthy", "tipping"
	healthy := []config.StatusDistribution{{Status: 200, Resolution: 1, Distribution: map[string]string{"p0.0": "1ms"}, LocalResponse: &config.LocalResponseConfig{Body: &healthyBody}}}
	tipping := []config.StatusDistribution{{Status: 503, Resolution: 1, Distribution: map[string]string{"p0.0": "10ms"}, LocalResponse: &config.LocalResponseConfig{Body: &tippingBody}}}
	distribution, err := NewLoadBasedResponseDistributionWithMode(healthy, 10, tipping, 20, nil, config.ProbabilityDistributionStateful)
	require.NoError(t, err)
	require.Equal(t, "healthy", *distribution.Sample(5).LocalResponse.Body)
	require.Equal(t, "tipping", *distribution.Sample(25).LocalResponse.Body)
}

func TestLoadBasedResponseDistribution_TippingPoint(t *testing.T) {
	healthyDists := []config.StatusDistribution{
		{
			Status:     200,
			Resolution: 1000,
			Distribution: map[string]string{
				"p0.0":   "1ms",
				"p100.0": "10ms",
			},
		},
	}
	tippingDists := []config.StatusDistribution{
		{
			Status:     503,
			Resolution: 1000,
			Distribution: map[string]string{
				"p0.0":   "100ms",
				"p100.0": "500ms",
			},
		},
	}

	lb, err := NewLoadBasedResponseDistributionWithMode(healthyDists, 100, tippingDists, 500, nil, config.ProbabilityDistributionStateful)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Sample at tipping point RPS — should get all 503s.
	const numSamples = 100
	for range numSamples {
		sample := lb.Sample(600) // above tipping point
		if sample.Status != 503 {
			t.Errorf("at tipping RPS, expected status 503, got %d", sample.Status)
		}
	}
}

func TestLoadBasedResponseDistribution_GreyZone(t *testing.T) {
	healthyDists := []config.StatusDistribution{
		{
			Status:     200,
			Resolution: 1000,
			Distribution: map[string]string{
				"p0.0":   "1ms",
				"p100.0": "10ms",
			},
		},
	}
	tippingDists := []config.StatusDistribution{
		{
			Status:     503,
			Resolution: 1000,
			Distribution: map[string]string{
				"p0.0":   "100ms",
				"p100.0": "500ms",
			},
		},
	}

	lb, err := NewLoadBasedResponseDistributionWithMode(healthyDists, 100, tippingDists, 500, nil, config.ProbabilityDistributionStateful)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Sample in the grey zone — should get a mix of 200 and 503.
	const numSamples = 1000
	got200 := 0
	got503 := 0
	for range numSamples {
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
	healthyDists := []config.StatusDistribution{
		{
			Status:     200,
			Resolution: 1000,
			Distribution: map[string]string{
				"p0.0":   "1ms",
				"p100.0": "5ms",
			},
		},
	}
	tippingDists := []config.StatusDistribution{
		{
			Status:     503,
			Resolution: 1000,
			Distribution: map[string]string{
				"p0.0":   "50ms",
				"p100.0": "100ms",
			},
		},
	}

	gz := &config.GreyZoneConfig{
		PenaltyBase:            "20ms",
		SpikeThreshold:         0.8,
		SpikePenaltyDuration:   "2s",
		SpikePenaltyMultiplier: 3.0,
		RecoveryRate:           0.5,
	}

	lb, err := NewLoadBasedResponseDistributionWithMode(healthyDists, 100, tippingDists, 500, gz, config.ProbabilityDistributionStateful)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Sample in grey zone with penalty — durations should be increased.
	const numSamples = 100
	var totalDuration time.Duration
	for range numSamples {
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
	percentiles := []config.Percentile{
		{Quantile: 0.0, Duration: 0},
		{Quantile: 0.50, Duration: 5 * time.Millisecond},
		{Quantile: 1.0, Duration: 500 * time.Millisecond},
	}

	dist := NewProbabilityDistribution(percentiles)

	for i := range 10000 {
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

func TestResponseDistribution_RejectsEmptyStatuses(t *testing.T) {
	for _, statuses := range [][]config.StatusDistribution{nil, {}} {
		for _, mode := range []string{config.ProbabilityDistributionStateful, config.ProbabilityDistributionStateless} {
			distribution, err := NewResponseDistributionWithMode(statuses, mode)
			require.ErrorContains(t, err, "at least one status entry")
			require.Nil(t, distribution)
		}
		distribution, err := NewResponseDistributionWithMode(statuses, config.ProbabilityDistributionStateful)
		require.Error(t, err)
		require.Nil(t, distribution)
	}
}

func TestLoadBasedResponseDistribution_RejectsEmptyTier(t *testing.T) {
	valid := []config.StatusDistribution{{Status: 200, Resolution: 1, Distribution: map[string]string{"p0.0": "1ms"}}}
	for _, healthyEmpty := range []bool{true, false} {
		healthy, tipping := valid, valid
		if healthyEmpty {
			healthy = nil
		} else {
			tipping = nil
		}
		distribution, err := NewLoadBasedResponseDistributionWithMode(healthy, 10, tipping, 100, nil, config.ProbabilityDistributionStateful)
		require.ErrorContains(t, err, "at least one status entry")
		require.Nil(t, distribution)
		for _, mode := range []string{config.ProbabilityDistributionStateful, config.ProbabilityDistributionStateless} {
			distribution, err = NewLoadBasedResponseDistributionWithMode(healthy, 10, tipping, 100, nil, mode)
			require.ErrorContains(t, err, "at least one status entry")
			require.Nil(t, distribution)
		}
	}
}

func TestLoadBasedResponseDistribution_SustainedSpikeAndRecovery(t *testing.T) {
	type step struct {
		elapsed time.Duration
		load    float64
		penalty time.Duration
	}
	for _, mode := range []string{config.ProbabilityDistributionStateful, config.ProbabilityDistributionStateless} {
		for _, scenario := range []struct {
			name  string
			steps []step
		}{
			{name: "sustained spike then recovery", steps: []step{
				{0, 180, 240 * time.Millisecond},
				{2 * time.Second, 180, 240 * time.Millisecond},
				{10 * time.Second, 180, 240 * time.Millisecond},
				{10 * time.Second, 150, 150 * time.Millisecond},
				{11 * time.Second, 150, 112500 * time.Microsecond},
				{12 * time.Second, 150, 50 * time.Millisecond},
			}},
			{name: "spike resumes during recovery", steps: []step{
				{0, 180, 240 * time.Millisecond},
				{time.Second, 150, 150 * time.Millisecond},
				{2 * time.Second, 150, 112500 * time.Microsecond},
				{2 * time.Second, 180, 240 * time.Millisecond},
				{3 * time.Second, 150, 150 * time.Millisecond},
				{4 * time.Second, 150, 112500 * time.Microsecond},
				{5 * time.Second, 150, 50 * time.Millisecond},
			}},
			{name: "tipping tier starts spike", steps: []step{
				{0, 250, 0},
				{10 * time.Second, 150, 150 * time.Millisecond},
				{11 * time.Second, 150, 112500 * time.Microsecond},
				{12 * time.Second, 150, 50 * time.Millisecond},
			}},
			{name: "healthy tier starts recovery", steps: []step{
				{0, 180, 240 * time.Millisecond},
				{10 * time.Second, 250, 0},
				{11 * time.Second, 50, 0},
				{12 * time.Second, 150, 112500 * time.Microsecond},
				{13 * time.Second, 150, 50 * time.Millisecond},
			}},
		} {
			t.Run(mode+"/"+scenario.name, func(t *testing.T) {
				responses := []config.StatusDistribution{{Status: 200, Resolution: 1, Distribution: map[string]string{"p0.0": "0s", "p100.0": "0s"}}}
				distribution, err := NewLoadBasedResponseDistributionWithMode(responses, 100, responses, 200, &config.GreyZoneConfig{
					PenaltyBase: "100ms", SpikeThreshold: 0.8, SpikePenaltyDuration: "2s", SpikePenaltyMultiplier: 3, RecoveryRate: 0.5,
				}, mode)
				require.NoError(t, err)
				now := time.Unix(1000, 0)
				distribution.now = func() time.Time { return now }
				for _, step := range scenario.steps {
					now = time.Unix(1000, 0).Add(step.elapsed)
					sample := distribution.Sample(step.load)
					require.Equal(t, step.penalty, sample.Duration, "elapsed=%s load=%v", step.elapsed, step.load)
				}
			})
		}
	}
}
