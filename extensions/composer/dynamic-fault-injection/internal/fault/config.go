// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package fault

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// FilterConfig is the top-level configuration for the latency/fault filter.
type FilterConfig struct {
	Endpoints []EndpointConfig `yaml:"endpoints"`
}

// EndpointConfig defines fault behavior for a matched endpoint.
type EndpointConfig struct {
	Match     MatchConfig          `yaml:"match"`
	Responses []StatusDistribution `yaml:"responses"`
	LoadBased *LoadBasedConfig     `yaml:"load_based,omitempty"`
}

// StatusDistribution defines a latency distribution for a specific HTTP status code.
// Resolution acts as both the relative weight for selecting this status code
// and the number of pre-computed samples in the stateful distribution.
type StatusDistribution struct {
	Status       int               `yaml:"status"`
	Resolution   int               `yaml:"resolution"`
	Distribution map[string]string `yaml:"distribution"`
}

// LoadBasedConfig enables different response distributions based on current RPS.
type LoadBasedConfig struct {
	Healthy      *LoadTier       `yaml:"healthy"`
	TippingPoint *LoadTier       `yaml:"tipping_point"`
	GreyZone     *GreyZoneConfig `yaml:"grey_zone,omitempty"`
}

// LoadTier defines the response behavior at a specific load level.
type LoadTier struct {
	ThresholdRPS float64              `yaml:"threshold_rps"`
	Responses    []StatusDistribution `yaml:"responses"`
}

// GreyZoneConfig controls behavior in the transition zone between healthy and tipping point.
type GreyZoneConfig struct {
	PenaltyBase            string  `yaml:"penalty_base"`
	SpikeThreshold         float64 `yaml:"spike_threshold"`
	SpikePenaltyDuration   string  `yaml:"spike_penalty_duration"`
	SpikePenaltyMultiplier float64 `yaml:"spike_penalty_multiplier"`
	RecoveryRate           float64 `yaml:"recovery_rate"`
}

// ParseConfig parses a filter configuration into a FilterConfig.
// Accepts both YAML and JSON input. When using google.protobuf.Struct as the
// filter_config type in Envoy, the config is received as JSON (which is valid YAML).
func ParseConfig(data []byte) (*FilterConfig, error) {
	var cfg FilterConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse filter config: %w", err)
	}

	// Validate endpoints.
	for i, ep := range cfg.Endpoints {
		if len(ep.Responses) == 0 && ep.LoadBased == nil {
			return nil, fmt.Errorf("endpoint %d: must have at least 'responses' or 'load_based' configured", i)
		}
		for j, resp := range ep.Responses {
			if err := validateStatusDistribution(resp, fmt.Sprintf("endpoint %d response %d", i, j)); err != nil {
				return nil, err
			}
		}
		if ep.LoadBased != nil {
			if err := validateLoadBased(ep.LoadBased, i); err != nil {
				return nil, err
			}
		}
	}

	return &cfg, nil
}

func validateStatusDistribution(sd StatusDistribution, context string) error {
	if sd.Status < 100 || sd.Status > 599 {
		return fmt.Errorf("%s: invalid HTTP status code %d", context, sd.Status)
	}
	if sd.Resolution <= 0 {
		return fmt.Errorf("%s: resolution must be positive, got %d", context, sd.Resolution)
	}
	if len(sd.Distribution) == 0 {
		return fmt.Errorf("%s: distribution must have at least one entry", context)
	}
	if _, err := ParsePercentileDistribution(sd.Distribution); err != nil {
		return fmt.Errorf("%s: %w", context, err)
	}
	return nil
}

func validateLoadBased(lb *LoadBasedConfig, endpointIdx int) error {
	if lb.Healthy == nil {
		return fmt.Errorf("endpoint %d: load_based.healthy is required", endpointIdx)
	}
	if lb.TippingPoint == nil {
		return fmt.Errorf("endpoint %d: load_based.tipping_point is required", endpointIdx)
	}
	if lb.Healthy.ThresholdRPS <= 0 {
		return fmt.Errorf("endpoint %d: load_based.healthy.threshold_rps must be positive", endpointIdx)
	}
	if lb.TippingPoint.ThresholdRPS <= lb.Healthy.ThresholdRPS {
		return fmt.Errorf("endpoint %d: load_based.tipping_point.threshold_rps must be greater than healthy.threshold_rps", endpointIdx)
	}
	for j, resp := range lb.Healthy.Responses {
		if err := validateStatusDistribution(resp, fmt.Sprintf("endpoint %d healthy response %d", endpointIdx, j)); err != nil {
			return err
		}
	}
	for j, resp := range lb.TippingPoint.Responses {
		if err := validateStatusDistribution(resp, fmt.Sprintf("endpoint %d tipping_point response %d", endpointIdx, j)); err != nil {
			return err
		}
	}
	if lb.GreyZone != nil {
		if _, err := time.ParseDuration(lb.GreyZone.PenaltyBase); err != nil {
			return fmt.Errorf("endpoint %d: grey_zone.penalty_base: %w", endpointIdx, err)
		}
		if _, err := time.ParseDuration(lb.GreyZone.SpikePenaltyDuration); err != nil {
			return fmt.Errorf("endpoint %d: grey_zone.spike_penalty_duration: %w", endpointIdx, err)
		}
		if lb.GreyZone.SpikeThreshold <= 0 || lb.GreyZone.SpikeThreshold >= 1 {
			return fmt.Errorf("endpoint %d: grey_zone.spike_threshold must be between 0 and 1 exclusive", endpointIdx)
		}
		if lb.GreyZone.SpikePenaltyMultiplier <= 0 {
			return fmt.Errorf("endpoint %d: grey_zone.spike_penalty_multiplier must be positive", endpointIdx)
		}
		if lb.GreyZone.RecoveryRate <= 0 || lb.GreyZone.RecoveryRate > 1 {
			return fmt.Errorf("endpoint %d: grey_zone.recovery_rate must be between 0 exclusive and 1 inclusive", endpointIdx)
		}
	}
	return nil
}

// Percentile represents a quantile-duration pair in a distribution.
type Percentile struct {
	Quantile float64
	Duration time.Duration
}

// ParsePercentileDistribution parses a map of percentile keys (e.g., "p0.0", "p50.0", "p99.9")
// to duration strings into a sorted slice of Percentiles.
func ParsePercentileDistribution(dist map[string]string) ([]Percentile, error) {
	if len(dist) == 0 {
		return nil, fmt.Errorf("distribution must have at least one entry")
	}

	var result []Percentile
	for key, durStr := range dist {
		quantile, err := parsePercentileKey(key)
		if err != nil {
			return nil, err
		}
		dur, err := time.ParseDuration(durStr)
		if err != nil {
			return nil, fmt.Errorf("invalid duration for %q: %w", key, err)
		}
		if dur < 0 {
			return nil, fmt.Errorf("negative duration for %q: %v", key, dur)
		}
		result = append(result, Percentile{Quantile: quantile, Duration: dur})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Quantile < result[j].Quantile
	})

	// Validate that durations are non-decreasing.
	for i := 1; i < len(result); i++ {
		if result[i].Duration < result[i-1].Duration {
			return nil, fmt.Errorf("distribution values must be non-decreasing: p%.1f (%v) < p%.1f (%v)",
				result[i].Quantile*100, result[i].Duration,
				result[i-1].Quantile*100, result[i-1].Duration)
		}
	}

	return result, nil
}

// parsePercentileKey parses keys like "p0.0", "p50.0", "p99.9", "p100.0"
// into a quantile value between 0.0 and 1.0.
func parsePercentileKey(key string) (float64, error) {
	if !strings.HasPrefix(key, "p") {
		return 0, fmt.Errorf("percentile key must start with 'p', got %q", key)
	}
	numStr := key[1:]
	val, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid percentile key %q: %w", key, err)
	}
	if val < 0 || val > 100 {
		return 0, fmt.Errorf("percentile key %q: value must be between 0 and 100", key)
	}
	return val / 100.0, nil
}

// TotalResolution returns the sum of all resolutions in a slice of StatusDistributions.
func TotalResolution(responses []StatusDistribution) int {
	total := 0
	for _, r := range responses {
		total += r.Resolution
	}
	return total
}
