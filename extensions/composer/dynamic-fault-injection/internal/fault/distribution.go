// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package fault

import (
	"math/rand"
	"sync"
	"time"
)

// ProbabilityDistribution samples from a distribution using linear interpolation
// between percentile boundaries. Stateless — each sample is independent.
type ProbabilityDistribution struct {
	percentiles []Percentile
	rng         *rand.Rand
}

// NewProbabilityDistribution creates a new stateless probability distribution.
func NewProbabilityDistribution(percentiles []Percentile, rng *rand.Rand) *ProbabilityDistribution {
	return &ProbabilityDistribution{
		percentiles: percentiles,
		rng:         rng,
	}
}

// Sample returns a random duration from the distribution.
func (pd *ProbabilityDistribution) Sample() time.Duration {
	r := pd.rng.Float64()
	return pd.SampleWithValue(r)
}

// SampleWithValue returns a duration for a specific quantile value [0, 1).
func (pd *ProbabilityDistribution) SampleWithValue(r float64) time.Duration {
	if len(pd.percentiles) == 0 {
		return 0
	}

	// Before the first percentile: interpolate from 0 to first value.
	if r <= pd.percentiles[0].Quantile {
		if pd.percentiles[0].Quantile == 0 {
			return pd.percentiles[0].Duration
		}
		fraction := r / pd.percentiles[0].Quantile
		return time.Duration(float64(pd.percentiles[0].Duration) * fraction)
	}

	// Between two percentiles: linear interpolation.
	for i := 1; i < len(pd.percentiles); i++ {
		if r <= pd.percentiles[i].Quantile {
			lower := pd.percentiles[i-1]
			upper := pd.percentiles[i]
			fraction := (r - lower.Quantile) / (upper.Quantile - lower.Quantile)
			dur := lower.Duration + time.Duration(fraction*float64(upper.Duration-lower.Duration))
			return dur
		}
	}

	// Beyond the last percentile: extrapolate.
	last := pd.percentiles[len(pd.percentiles)-1]
	if last.Quantile >= 1.0 {
		return last.Duration
	}
	fraction := (r - last.Quantile) / (1.0 - last.Quantile)
	return last.Duration + time.Duration(fraction*float64(last.Duration))
}

// StatefulProbabilityDistribution pre-computes exactly `resolution` samples
// and cycles through them in shuffled order. Over N samples, this guarantees
// an exact match to the configured percentile distribution.
type StatefulProbabilityDistribution struct {
	values []time.Duration
	index  int
	rng    *rand.Rand
}

// NewStatefulProbabilityDistribution creates a new stateful distribution with
// the given resolution (number of pre-computed samples).
func NewStatefulProbabilityDistribution(percentiles []Percentile, resolution int, rng *rand.Rand) *StatefulProbabilityDistribution {
	values := make([]time.Duration, resolution)
	idx := 0
	prevQuantile := 0.0
	prevDuration := time.Duration(0)

	for _, p := range percentiles {
		count := int(float64(resolution) * (p.Quantile - prevQuantile))
		for i := 0; i < count && idx < resolution; i++ {
			fraction := float64(i) / float64(count)
			dur := prevDuration + time.Duration(fraction*float64(p.Duration-prevDuration))
			values[idx] = dur
			idx++
		}
		prevQuantile = p.Quantile
		prevDuration = p.Duration
	}

	// Fill remaining slots (tail beyond last percentile).
	lastDuration := prevDuration
	remaining := resolution - idx
	for i := 0; i < remaining; i++ {
		fraction := float64(i) / float64(remaining)
		values[idx] = lastDuration + time.Duration(fraction*float64(lastDuration))
		idx++
	}

	rng.Shuffle(len(values), func(i, j int) {
		values[i], values[j] = values[j], values[i]
	})

	return &StatefulProbabilityDistribution{
		values: values,
		index:  0,
		rng:    rng,
	}
}

// Sample returns the next pre-computed duration, reshuffling when the cycle completes.
func (spd *StatefulProbabilityDistribution) Sample() time.Duration {
	if spd.index >= len(spd.values) {
		spd.rng.Shuffle(len(spd.values), func(i, j int) {
			spd.values[i], spd.values[j] = spd.values[j], spd.values[i]
		})
		spd.index = 0
	}
	val := spd.values[spd.index]
	spd.index++
	return val
}

// ResponseSample represents a sampled response: status code + latency.
type ResponseSample struct {
	Status   int
	Duration time.Duration
}

// ResponseDistribution selects a status code based on resolution weights,
// then samples a latency from that status code's distribution.
type ResponseDistribution struct {
	entries     []responseEntry
	totalWeight int
	rng         *rand.Rand
}

type responseEntry struct {
	status       int
	weight       int
	distribution *StatefulProbabilityDistribution
}

// NewResponseDistribution creates a ResponseDistribution from a set of StatusDistributions.
func NewResponseDistribution(statusDists []StatusDistribution, rng *rand.Rand) (*ResponseDistribution, error) {
	entries := make([]responseEntry, 0, len(statusDists))
	totalWeight := 0

	for _, sd := range statusDists {
		percentiles, err := ParsePercentileDistribution(sd.Distribution)
		if err != nil {
			return nil, err
		}
		dist := NewStatefulProbabilityDistribution(percentiles, sd.Resolution, rng)
		entries = append(entries, responseEntry{
			status:       sd.Status,
			weight:       sd.Resolution,
			distribution: dist,
		})
		totalWeight += sd.Resolution
	}

	return &ResponseDistribution{
		entries:     entries,
		totalWeight: totalWeight,
		rng:         rng,
	}, nil
}

// Sample selects a status code by weight and returns a sampled response.
func (rd *ResponseDistribution) Sample() ResponseSample {
	// Select status by weighted random choice.
	r := rd.rng.Intn(rd.totalWeight)
	cumulative := 0
	for i := range rd.entries {
		cumulative += rd.entries[i].weight
		if r < cumulative {
			return ResponseSample{
				Status:   rd.entries[i].status,
				Duration: rd.entries[i].distribution.Sample(),
			}
		}
	}
	// Fallback (should not happen).
	last := &rd.entries[len(rd.entries)-1]
	return ResponseSample{
		Status:   last.status,
		Duration: last.distribution.Sample(),
	}
}

// LoadBasedResponseDistribution switches between healthy and tipping-point
// distributions based on observed RPS, with grey zone transition behavior.
type LoadBasedResponseDistribution struct {
	healthy      *ResponseDistribution
	tippingPoint *ResponseDistribution
	healthyRPS   float64
	tippingRPS   float64
	greyZone     *greyZoneState
	rng          *rand.Rand
	mu           sync.Mutex
}

type greyZoneState struct {
	penaltyBase            time.Duration
	spikePenaltyDuration   time.Duration
	spikeThreshold         float64
	spikePenaltyMultiplier float64
	recoveryRate           float64
	lastSpikeTime          time.Time
	inSpike                bool
}

// NewLoadBasedResponseDistribution creates a load-based distribution.
func NewLoadBasedResponseDistribution(
	healthyDists []StatusDistribution,
	healthyRPS float64,
	tippingDists []StatusDistribution,
	tippingRPS float64,
	gz *GreyZoneConfig,
	rng *rand.Rand,
) (*LoadBasedResponseDistribution, error) {
	healthy, err := NewResponseDistribution(healthyDists, rng)
	if err != nil {
		return nil, err
	}
	tipping, err := NewResponseDistribution(tippingDists, rng)
	if err != nil {
		return nil, err
	}

	lb := &LoadBasedResponseDistribution{
		healthy:      healthy,
		tippingPoint: tipping,
		healthyRPS:   healthyRPS,
		tippingRPS:   tippingRPS,
		rng:          rng,
	}

	if gz != nil {
		penaltyBase, _ := time.ParseDuration(gz.PenaltyBase)
		spikeDur, _ := time.ParseDuration(gz.SpikePenaltyDuration)
		lb.greyZone = &greyZoneState{
			penaltyBase:            penaltyBase,
			spikePenaltyDuration:   spikeDur,
			spikeThreshold:         gz.SpikeThreshold,
			spikePenaltyMultiplier: gz.SpikePenaltyMultiplier,
			recoveryRate:           gz.RecoveryRate,
		}
	}

	return lb, nil
}

// Sample returns a response based on the current RPS.
// In the grey zone (between healthyRPS and tippingRPS), it interpolates
// between healthy and tipping behavior with optional spike penalties.
func (lb *LoadBasedResponseDistribution) Sample(currentRPS float64) ResponseSample {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	if currentRPS <= lb.healthyRPS {
		return lb.healthy.Sample()
	}
	if currentRPS >= lb.tippingRPS {
		return lb.tippingPoint.Sample()
	}

	// Grey zone: interpolate between healthy and tipping.
	greyPosition := (currentRPS - lb.healthyRPS) / (lb.tippingRPS - lb.healthyRPS)

	// Decide whether to use healthy or tipping distribution based on position.
	var sample ResponseSample
	if lb.rng.Float64() > greyPosition {
		sample = lb.healthy.Sample()
	} else {
		sample = lb.tippingPoint.Sample()
	}

	// Apply grey zone penalty if configured.
	if lb.greyZone != nil {
		penalty := lb.calculateGreyZonePenalty(greyPosition)
		sample.Duration += penalty
	}

	return sample
}

// calculateGreyZonePenalty computes additional latency penalty in the grey zone.
func (lb *LoadBasedResponseDistribution) calculateGreyZonePenalty(greyPosition float64) time.Duration {
	gz := lb.greyZone
	basePenalty := time.Duration(float64(gz.penaltyBase) * greyPosition)

	// Check for spike behavior.
	now := time.Now()
	if greyPosition >= gz.spikeThreshold {
		if !gz.inSpike {
			gz.inSpike = true
			gz.lastSpikeTime = now
		}
		// During a spike, apply the multiplier.
		if now.Sub(gz.lastSpikeTime) < gz.spikePenaltyDuration {
			return time.Duration(float64(basePenalty) * gz.spikePenaltyMultiplier)
		}
		// Spike duration expired, start recovery.
		gz.inSpike = false
	} else if gz.inSpike {
		// Below spike threshold but was in spike — recover.
		elapsed := now.Sub(gz.lastSpikeTime)
		if elapsed > gz.spikePenaltyDuration {
			gz.inSpike = false
		} else {
			// Decay the penalty.
			remaining := 1.0 - (float64(elapsed) / float64(gz.spikePenaltyDuration) * gz.recoveryRate)
			if remaining < 0 {
				remaining = 0
				gz.inSpike = false
			}
			return time.Duration(float64(basePenalty) * gz.spikePenaltyMultiplier * remaining)
		}
	}

	return basePenalty
}
