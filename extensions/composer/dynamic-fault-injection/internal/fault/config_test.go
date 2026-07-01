// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package fault

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ToDo: Add errors.As or errors.Is assertions on actual error messages.

func TestParseConfig_BasicEndpoint(t *testing.T) {
	input := `
endpoints:
  - match:
      prefix: "/api/"
    responses:
      - status: 200
        resolution: 90
        distribution:
          p0.0: "1ms"
          p50.0: "10ms"
          p99.0: "200ms"
      - status: 503
        resolution: 10
        distribution:
          p0.0: "50ms"
          p50.0: "100ms"
          p99.0: "500ms"
`

	cfg, err := ParseConfig([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Endpoints) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(cfg.Endpoints))
	}

	ep := cfg.Endpoints[0]
	if ep.Match.Prefix != "/api/" {
		t.Errorf("expected prefix /api/, got %v", ep.Match.Prefix)
	}
	if len(ep.Responses) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(ep.Responses))
	}
	if ep.Responses[0].Status != 200 {
		t.Errorf("expected status 200, got %d", ep.Responses[0].Status)
	}
	if ep.Responses[0].Resolution != 90 {
		t.Errorf("expected resolution 90, got %d", ep.Responses[0].Resolution)
	}
	if ep.Responses[1].Status != 503 {
		t.Errorf("expected status 503, got %d", ep.Responses[1].Status)
	}
	if ep.Responses[1].Resolution != 10 {
		t.Errorf("expected resolution 10, got %d", ep.Responses[1].Resolution)
	}
}

func TestParseConfig_LoadBased(t *testing.T) {
	input := `
endpoints:
  - match:
      prefix: "/api/"
    load_based:
      healthy:
        threshold_rps: 100
        responses:
          - status: 200
            resolution: 100
            distribution:
              p0.0: "1ms"
              p50.0: "5ms"
              p99.0: "50ms"
      tipping_point:
        threshold_rps: 500
        responses:
          - status: 200
            resolution: 50
            distribution:
              p0.0: "50ms"
              p50.0: "200ms"
              p99.0: "2s"
          - status: 503
            resolution: 50
            distribution:
              p0.0: "10ms"
              p50.0: "50ms"
              p99.0: "100ms"
      grey_zone:
        penalty_base: "10ms"
        spike_threshold: 0.8
        spike_penalty_duration: "5s"
        spike_penalty_multiplier: 2.0
        recovery_rate: 0.1
`

	cfg, err := ParseConfig([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Endpoints) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(cfg.Endpoints))
	}

	ep := cfg.Endpoints[0]
	if ep.LoadBased == nil {
		t.Fatal("expected load_based config")
	}
	if ep.LoadBased.Healthy.ThresholdRPS != 100 {
		t.Errorf("expected healthy threshold 100, got %v", ep.LoadBased.Healthy.ThresholdRPS)
	}
	if ep.LoadBased.TippingPoint.ThresholdRPS != 500 {
		t.Errorf("expected tipping_point threshold 500, got %v", ep.LoadBased.TippingPoint.ThresholdRPS)
	}
	if ep.LoadBased.GreyZone == nil {
		t.Fatal("expected grey_zone config")
	}
	if ep.LoadBased.GreyZone.SpikeThreshold != 0.8 {
		t.Errorf("expected spike_threshold 0.8, got %v", ep.LoadBased.GreyZone.SpikeThreshold)
	}
}

func TestParseConfig_ExactMatch(t *testing.T) {
	input := `
endpoints:
  - match:
      exact: "/health"
    responses:
      - status: 200
        resolution: 100
        distribution:
          p0.0: "0ms"
          p50.0: "1ms"
          p99.0: "5ms"
`

	cfg, err := ParseConfig([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Endpoints[0].Match.Exact != "/health" {
		t.Errorf("expected exact /health, got %v", cfg.Endpoints[0].Match.Exact)
	}
}

func TestParseConfig_HeaderMatch(t *testing.T) {
	input := `
endpoints:
  - match:
      prefix: "/api/"
      headers:
        - name: "x-env"
          exact_match: "staging"
    responses:
      - status: 200
        resolution: 100
        distribution:
          p0.0: "1ms"
          p50.0: "10ms"
          p99.0: "100ms"
`

	cfg, err := ParseConfig([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ep := cfg.Endpoints[0]
	if len(ep.Match.Headers) != 1 {
		t.Fatalf("expected 1 header matcher, got %d", len(ep.Match.Headers))
	}
	if ep.Match.Headers[0].Name != "x-env" {
		t.Errorf("expected header name x-env, got %v", ep.Match.Headers[0].Name)
	}
	if ep.Match.Headers[0].ExactMatch != "staging" {
		t.Errorf("expected exact_match staging, got %v", ep.Match.Headers[0].ExactMatch)
	}
}

func TestParseConfig_InvalidYAML(t *testing.T) {
	_, err := ParseConfig([]byte("{{not yaml"))
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestParseConfig_NoResponsesOrLoadBased(t *testing.T) {
	input := `
endpoints:
  - match:
      prefix: "/api/"
`

	_, err := ParseConfig([]byte(input))
	if err == nil {
		t.Fatal("expected error when neither responses nor load_based is configured")
	}
}

func TestParseConfig_InvalidStatusCode(t *testing.T) {
	input := `
endpoints:
  - match:
      prefix: "/api/"
    responses:
      - status: 999
        resolution: 100
        distribution:
          p0.0: "1ms"
          p50.0: "10ms"
`

	_, err := ParseConfig([]byte(input))
	if err == nil {
		t.Fatal("expected error for invalid status code")
	}
}

func TestParseConfig_MultipleValidationErrors(t *testing.T) {
	input := `
endpoints:
  - match:
      prefix: "/api/"
    responses:
      - status: 999
        resolution: 100
        distribution:
          p0.0: "1ms"
          p50.0: "10ms"
  - match:
      prefix: "/otherAPI/"
    responses:
     - status: 999
       resolution: 0
       distribution:
         p0.0: "1ms"
         p50.0: "10ms"
`

	// We should see 3 errors.
	// - Invalid status code for the /api/ prefix match
	// - Invalid status code for the /otherAPI/ prefix match
	// - Invalid resolution (0) for the /otherAPI/ prefix match
	_, err := ParseConfig([]byte(input))

	totalSumOfErrors := countErrorsRecursively(err)

	require.Equal(t, 3, totalSumOfErrors)
	if err == nil {
		t.Fatal("expected error for invalid status code")
	}
}

func countErrorsRecursively(err error) int {
	for {
		switch x := err.(type) {
		case interface{ Unwrap() error }:
			err = x.Unwrap()
			if err == nil {
				return 1
			}
		case interface{ Unwrap() []error }:
			errorCount := 0
			for _, err := range err.(interface{ Unwrap() []error }).Unwrap() {
				errorCount += countErrorsRecursively(err)
			}
			return errorCount
		default:
			return 1
		}
	}
}

func TestParseConfig_ZeroResolution(t *testing.T) {
	input := `
endpoints:
  - match:
      prefix: "/api/"
    responses:
      - status: 200
        resolution: 0
        distribution:
          p0.0: "1ms"
          p50.0: "10ms"
`

	_, err := ParseConfig([]byte(input))
	if err == nil {
		t.Fatal("expected error for zero resolution")
	}
}

func TestParseConfig_LoadBased_MissingHealthy(t *testing.T) {
	input := `
endpoints:
  - match:
      prefix: "/api/"
    load_based:
      tipping_point:
        threshold_rps: 500
        responses:
          - status: 200
            resolution: 100
            distribution:
              p0.0: "1ms"
              p50.0: "10ms"
              p99.0: "100ms"
`

	_, err := ParseConfig([]byte(input))
	if err == nil {
		t.Fatal("expected error when load_based.healthy is missing")
	}
}

func TestParseConfig_LoadBased_InvalidThresholds(t *testing.T) {
	input := `
endpoints:
  - match:
      prefix: "/api/"
    load_based:
      healthy:
        threshold_rps: 500
        responses:
          - status: 200
            resolution: 100
            distribution:
              p0.0: "1ms"
              p50.0: "10ms"
              p99.0: "100ms"
      tipping_point:
        threshold_rps: 100
        responses:
          - status: 200
            resolution: 100
            distribution:
              p0.0: "1ms"
              p50.0: "10ms"
              p99.0: "100ms"
`

	_, err := ParseConfig([]byte(input))
	if err == nil {
		t.Fatal("expected error when tipping_point threshold is less than healthy threshold")
	}
}

func TestParsePercentileDistribution(t *testing.T) {
	dist := map[string]string{
		"p0.0":  "1ms",
		"p50.0": "10ms",
		"p90.0": "50ms",
		"p99.0": "200ms",
	}

	percentiles, err := ParsePercentileDistribution(dist)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(percentiles) != 4 {
		t.Fatalf("expected 4 percentiles, got %d", len(percentiles))
	}

	// Verify they're sorted by quantile.
	for i := 1; i < len(percentiles); i++ {
		if percentiles[i].Quantile <= percentiles[i-1].Quantile {
			t.Errorf("percentiles not sorted: %v", percentiles)
		}
	}

	// Verify values.
	expected := []struct {
		quantile float64
		duration time.Duration
	}{
		{0.00, 1 * time.Millisecond},
		{0.50, 10 * time.Millisecond},
		{0.90, 50 * time.Millisecond},
		{0.99, 200 * time.Millisecond},
	}

	for i, e := range expected {
		if percentiles[i].Quantile != e.quantile {
			t.Errorf("percentile %d: expected quantile %v, got %v", i, e.quantile, percentiles[i].Quantile)
		}
		if percentiles[i].Duration != e.duration {
			t.Errorf("percentile %d: expected duration %v, got %v", i, e.duration, percentiles[i].Duration)
		}
	}
}

func TestParsePercentileDistribution_InvalidKey(t *testing.T) {
	dist := map[string]string{
		"p50.0":   "10ms",
		"invalid": "100ms",
	}

	_, err := ParsePercentileDistribution(dist)
	if err == nil {
		t.Fatal("expected error for invalid percentile key")
	}
}

func TestParsePercentileDistribution_InvalidDuration(t *testing.T) {
	dist := map[string]string{
		"p50.0": "not-a-duration",
	}

	_, err := ParsePercentileDistribution(dist)
	if err == nil {
		t.Fatal("expected error for invalid duration")
	}
}

func TestParsePercentileDistribution_Empty(t *testing.T) {
	_, err := ParsePercentileDistribution(map[string]string{})
	if err == nil {
		t.Fatal("expected error for empty distribution")
	}
}

func TestParsePercentileDistribution_NegativeDuration(t *testing.T) {
	dist := map[string]string{
		"p50.0": "-10ms",
	}

	_, err := ParsePercentileDistribution(dist)
	if err == nil {
		t.Fatal("expected error for negative duration")
	}
}

func TestParsePercentileDistribution_NonDecreasing(t *testing.T) {
	dist := map[string]string{
		"p0.0":  "100ms",
		"p50.0": "50ms",
		"p99.0": "200ms",
	}

	_, err := ParsePercentileDistribution(dist)
	if err == nil {
		t.Fatal("expected error for non-monotonic distribution")
	}
}

func TestTotalResolution(t *testing.T) {
	responses := []StatusDistribution{
		{Status: 200, Resolution: 90},
		{Status: 503, Resolution: 10},
	}

	total := TotalResolution(responses)
	if total != 100 {
		t.Errorf("expected total resolution 100, got %d", total)
	}
}

func TestParseConfig_JSONFromStruct(t *testing.T) {
	// When using google.protobuf.Struct, Envoy serializes the config as JSON
	// before passing it to the module. Verify that our YAML parser handles
	// this correctly.
	jsonInput := `{"endpoints":[{"match":{"prefix":"/api/"},"responses":[{"status":200,"resolution":900,"distribution":{"p0.0":"1ms","p50.0":"10ms","p100.0":"100ms"}},{"status":503,"resolution":100,"distribution":{"p0.0":"50ms","p100.0":"500ms"}}]}]}`

	cfg, err := ParseConfig([]byte(jsonInput))
	if err != nil {
		t.Fatalf("unexpected error parsing JSON input: %v", err)
	}

	if len(cfg.Endpoints) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(cfg.Endpoints))
	}

	ep := cfg.Endpoints[0]
	if ep.Match.Prefix != "/api/" {
		t.Errorf("expected prefix /api/, got %q", ep.Match.Prefix)
	}
	if len(ep.Responses) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(ep.Responses))
	}
	if ep.Responses[0].Status != 200 {
		t.Errorf("expected status 200, got %d", ep.Responses[0].Status)
	}
	if ep.Responses[0].Resolution != 900 {
		t.Errorf("expected resolution 900, got %d", ep.Responses[0].Resolution)
	}
	if ep.Responses[0].Distribution["p50.0"] != "10ms" {
		t.Errorf("expected p50.0=10ms, got %q", ep.Responses[0].Distribution["p50.0"])
	}
	if ep.Responses[1].Status != 503 {
		t.Errorf("expected status 503, got %d", ep.Responses[1].Status)
	}
}
