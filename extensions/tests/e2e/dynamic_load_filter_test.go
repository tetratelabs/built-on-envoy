// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package integration

import (
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	internaltesting "github.com/tetratelabs/built-on-envoy/internal/testing"
)

func TestDistributionDelay(t *testing.T) {
	minimalDelay := 20
	maximalDelay := 300
	config := fmt.Sprintf(`{
	"endpoints": [
		{
			"match": {"prefix": "/delay"},
			"responses": [
				{
					"status": 200,
					"resolution": 1000,
					"distribution": {
						"p0.0": "%dms",
						"p50.0": "50ms",
						"p90.0": "100ms",
						"p99.0": "200ms",
						"p100.0": "%dms"
					}
				}
			]
		}
	]
}`, minimalDelay, maximalDelay)
	fmt.Printf("Using config: %s\n", config)
	proxyPort := startFaultInjectionEnvoy(t, config)

	// Requests to /delay have distribution: p0=20ms, p50=50ms, p90=100ms, p99=200ms, p100=300ms.
	var durations []time.Duration
	const numRequests = 20

	for i := range numRequests {
		req, err := http.NewRequest("GET", fmt.Sprintf("http://localhost:%d/delay/0", proxyPort), nil)
		require.NoError(t, err)

		internaltesting.RequireEventuallyRequestWithTiming(t, req, func(resp *http.Response, elapsed time.Duration) bool {
			defer func() { _ = resp.Body.Close() }()
			_, _ = io.ReadAll(resp.Body)

			require.Equal(t, 200, resp.StatusCode)

			// Verify upstream filter headers are present.
			delayHeader := resp.Header.Get("x-fault-injected-delay")
			require.NotEmpty(t, delayHeader, "x-fault-injected-delay header should be set (target duration)")

			upstreamHeader := resp.Header.Get("x-fault-actual-upstream")
			require.NotEmpty(t, upstreamHeader, "x-fault-actual-upstream header should be set")

			statusHeader := resp.Header.Get("x-fault-status")
			require.Equal(t, "200", statusHeader, "x-fault-status header should be 200")

			// The target delay should be within the distribution range (20-300ms).
			targetDelay, err := time.ParseDuration(delayHeader)
			require.NoError(t, err)
			require.GreaterOrEqual(t, targetDelay.Milliseconds(), int64(minimalDelay),
				"target delay should be at least p0 (20ms)")
			require.LessOrEqual(t, targetDelay.Milliseconds(), int64(maximalDelay),
				"target delay should be at most p100 (300ms)")

			durations = append(durations, elapsed)
			t.Logf("request %d: elapsed=%v, target=%s, upstream=%s, added=%s",
				i, elapsed, delayHeader, upstreamHeader,
				resp.Header.Get("x-fault-added-delay"))
			return resp.StatusCode == 200
		})
	}

	// Verify that total observed time matches the distribution.
	var totalDelay time.Duration
	var actualMinimalDelay time.Duration
	var actualMaximalDelay time.Duration
	for _, d := range durations {
		totalDelay += d
		if d < actualMinimalDelay || actualMinimalDelay == 0 {
			actualMinimalDelay = d
		}
		if d > actualMaximalDelay {
			actualMaximalDelay = d
		}
	}
	avgDelay := totalDelay / time.Duration(numRequests)
	t.Logf("average request time: %v", avgDelay)

	// The actual elapsed delay should also be within the distribution range (20-300ms).
	require.GreaterOrEqual(t, actualMinimalDelay.Milliseconds(), int64(minimalDelay),
		"elapsed delays should be at least p0 (20ms)")
	require.LessOrEqual(t, actualMaximalDelay.Milliseconds(), int64(maximalDelay+100), // the 100ms is rather arbitrary buffer for network jitter, etc.
		"elapsed delay should be at most p100 + some buffer (300ms + 100ms)")

	// Average should be roughly around 50ms (p50 of distribution).
	require.Greater(t, avgDelay.Milliseconds(), int64(20),
		"average delay should be meaningful (got %v)", avgDelay)
}

func TestAbortInjection(t *testing.T) {
	minimalDelay := 0
	maximalDelay := 5
	config := fmt.Sprintf(`{
	"endpoints": [
		{
			"match": {"prefix": "/abort"},
			"responses": [
				{
					"status": 503,
					"resolution": 1000,
					"distribution": {
						"p0.0": "%dms",
						"p100.0": "%dms"
					}
				}
			]
		}
	]
}`, minimalDelay, maximalDelay)

	proxyPort := startFaultInjectionEnvoy(t, config)

	// Requests to /abort: all responses sampled as 503.
	req, err := http.NewRequest("GET", fmt.Sprintf("http://localhost:%d/abort/test", proxyPort), nil)
	require.NoError(t, err)

	internaltesting.RequireEventuallyRequestWithTiming(t, req, func(resp *http.Response, elapsed time.Duration) bool {
		body, _ := io.ReadAll(resp.Body)

		t.Logf("abort response: status=%d elapsed=%v body=%s", resp.StatusCode, elapsed, string(body))
		require.Equal(t, 503, resp.StatusCode)
		require.Contains(t, string(body), "fault filter abort")
		require.Equal(t, "abort", resp.Header.Get("x-fault-injected"))

		// The abort distribution is p0=0ms, p100=5ms — target delay should be within range.
		delayHeader := resp.Header.Get("x-fault-injected-delay")
		require.NotEmpty(t, delayHeader, "x-fault-injected-delay header should be set")
		targetDelay, err := time.ParseDuration(delayHeader)
		require.NoError(t, err)
		require.GreaterOrEqual(t, targetDelay.Milliseconds(), int64(minimalDelay),
			"target delay should be at least p0")
		require.LessOrEqual(t, targetDelay.Milliseconds(), int64(maximalDelay),
			"target delay should be within distribution range")

		// The actual elapsed time should be at most the maximal delay plus a buffer for the
		// upstream round-trip and network jitter.
		require.LessOrEqual(t, elapsed.Milliseconds(), int64(maximalDelay+100),
			"elapsed time should be at most p100 + buffer")

		return true
	})
}

func TestFixedDelayAccountsForUpstream(t *testing.T) {
	fixedDelay := 100
	config := fmt.Sprintf(`{
	"endpoints": [
		{
			"match": {"prefix": "/"},
			"responses": [
				{
					"status": 200,
					"resolution": 1000,
					"distribution": {
						"p0.0": "%dms",
						"p100.0": "%dms"
					}
				}
			]
		}
	]
}`, fixedDelay, fixedDelay)

	proxyPort := startFaultInjectionEnvoy(t, config)

	// Flat 100ms distribution. The filter should only add (100ms - actual_upstream_time)
	// as additional delay.
	req, err := http.NewRequest("GET", fmt.Sprintf("http://localhost:%d/status/200", proxyPort), nil)
	require.NoError(t, err)

	internaltesting.RequireEventuallyRequestWithTiming(t, req, func(resp *http.Response, elapsed time.Duration) bool {
		_, _ = io.ReadAll(resp.Body)

		t.Logf("fixed delay: status=%d elapsed=%v target=%s upstream=%s added=%s",
			resp.StatusCode, elapsed,
			resp.Header.Get("x-fault-injected-delay"),
			resp.Header.Get("x-fault-actual-upstream"),
			resp.Header.Get("x-fault-added-delay"))

		require.Equal(t, 200, resp.StatusCode)

		// Total elapsed time should be ~100ms (target) regardless of upstream speed.
		// Allow a small buffer below for timing slack and a larger buffer above for jitter.
		require.GreaterOrEqual(t, elapsed.Milliseconds(), int64(fixedDelay-10),
			"total request time should be at least ~100ms")
		require.LessOrEqual(t, elapsed.Milliseconds(), int64(fixedDelay+400),
			"total request time should not be excessively long")

		// The target header should show the fixed delay.
		delayHeader := resp.Header.Get("x-fault-injected-delay")
		require.Equal(t, fmt.Sprintf("%dms", fixedDelay), delayHeader)

		// The actual upstream time should be much less than the fixed delay.
		upstreamHeader := resp.Header.Get("x-fault-actual-upstream")
		require.NotEmpty(t, upstreamHeader)
		upstreamTime, err := time.ParseDuration(upstreamHeader)
		require.NoError(t, err)
		require.Less(t, upstreamTime.Milliseconds(), int64(fixedDelay-10),
			"upstream time for /status/200 should be well under the fixed delay")

		// The filter should have added the remaining delay (fixedDelay - upstream).
		addedHeader := resp.Header.Get("x-fault-added-delay")
		require.NotEmpty(t, addedHeader, "x-fault-added-delay should be set when target > upstream")
		addedDelay, err := time.ParseDuration(addedHeader)
		require.NoError(t, err)

		// upstream + added should approximate the fixed delay target.
		totalDelay := upstreamTime + addedDelay
		require.GreaterOrEqual(t, totalDelay.Milliseconds(), int64(fixedDelay-10),
			"upstream + added delay should be at least ~100ms")
		require.LessOrEqual(t, totalDelay.Milliseconds(), int64(fixedDelay+50),
			"upstream + added delay should not overshoot significantly")

		return true
	})
}

func TestCatchallEndpoint(t *testing.T) {
	minimalDelay := 5
	maximalDelay := 20
	config := fmt.Sprintf(`{
	"endpoints": [
		{
			"match": {"prefix": "/"},
			"responses": [
				{
					"status": 200,
					"resolution": 1000,
					"distribution": {
						"p0.0": "%dms",
						"p50.0": "10ms",
						"p100.0": "%dms"
					}
				}
			]
		}
	]
}`, minimalDelay, maximalDelay)

	proxyPort := startFaultInjectionEnvoy(t, config)

	// Requests to paths that don't match specific prefixes hit the "/" catch-all.
	// Distribution: p0=5ms, p50=10ms, p100=20ms.
	req, err := http.NewRequest("GET", fmt.Sprintf("http://localhost:%d/status/200", proxyPort), nil)
	require.NoError(t, err)

	internaltesting.RequireEventuallyRequestWithTiming(t, req, func(resp *http.Response, elapsed time.Duration) bool {
		_, _ = io.ReadAll(resp.Body)

		t.Logf("catchall: status=%d elapsed=%v target=%s upstream=%s",
			resp.StatusCode, elapsed,
			resp.Header.Get("x-fault-injected-delay"),
			resp.Header.Get("x-fault-actual-upstream"))

		require.Equal(t, 200, resp.StatusCode)
		require.NotEmpty(t, resp.Header.Get("x-fault-injected-delay"),
			"should have target delay header from catchall endpoint")
		require.NotEmpty(t, resp.Header.Get("x-fault-actual-upstream"),
			"should have actual upstream header")

		// The catch-all distribution is p0=5ms, p50=10ms, p100=20ms.
		targetDelay, err := time.ParseDuration(resp.Header.Get("x-fault-injected-delay"))
		require.NoError(t, err)
		require.GreaterOrEqual(t, targetDelay.Milliseconds(), int64(minimalDelay),
			"target delay should be at least p0")
		require.LessOrEqual(t, targetDelay.Milliseconds(), int64(maximalDelay),
			"target delay should be at most p100")

		// The actual elapsed time should be at most the maximal delay plus a buffer for the
		// upstream round-trip and network jitter.
		require.LessOrEqual(t, elapsed.Milliseconds(), int64(maximalDelay+100),
			"elapsed time should be at most p100 + buffer")

		return true
	})
}

func TestMixedStatusCodes(t *testing.T) {
	// Distribution bounds for the two sampled statuses.
	minimalDelay200 := 30
	maximalDelay200 := 80
	minimalDelay429 := 0
	maximalDelay429 := 5
	config := fmt.Sprintf(`{
	"endpoints": [
		{
			"match": {"prefix": "/mixed"},
			"responses": [
				{
					"status": 200,
					"resolution": 500,
					"distribution": {
						"p0.0": "%dms",
						"p50.0": "50ms",
						"p100.0": "%dms"
					}
				},
				{
					"status": 429,
					"resolution": 500,
					"distribution": {
						"p0.0": "%dms",
						"p100.0": "%dms"
					}
				}
			]
		}
	]
}`, minimalDelay200, maximalDelay200, minimalDelay429, maximalDelay429)

	proxyPort := startFaultInjectionEnvoy(t, config)

	// Requests to /mixed: 50% -> 200 (30-80ms delay, upstream response passes through),
	// 50% -> 429 abort (overrides upstream response).
	gotUpstream := false
	got429 := false

	req, err := http.NewRequest("GET", fmt.Sprintf("http://localhost:%d/mixed/test", proxyPort), nil)
	require.NoError(t, err)

	internaltesting.RequireEventuallyRequestWithTiming(t, req, func(resp *http.Response, elapsed time.Duration) bool {
		_, _ = io.ReadAll(resp.Body)

		t.Logf("mixed: status=%d elapsed=%v fault-status=%s delay=%s",
			resp.StatusCode, elapsed, resp.Header.Get("x-fault-status"),
			resp.Header.Get("x-fault-injected-delay"))

		// Assert timing based on which status was sampled.
		delayHeader := resp.Header.Get("x-fault-injected-delay")
		require.NotEmpty(t, delayHeader, "x-fault-injected-delay header should be set")
		targetDelay, err := time.ParseDuration(delayHeader)
		require.NoError(t, err)

		if resp.StatusCode == 429 {
			// 429 distribution: p0=0ms, p100=5ms.
			require.GreaterOrEqualf(t, targetDelay.Milliseconds(), int64(minimalDelay429),
				"429 target delay should be at least p0 (%v)", minimalDelay429)
			require.LessOrEqual(t, targetDelay.Milliseconds(), int64(maximalDelay429),
				"429 target delay should be within distribution range (%v - %v)", minimalDelay429, maximalDelay429)
			// The actual elapsed time should be at most the maximal delay plus a buffer for the
			// upstream round-trip and network jitter.
			require.LessOrEqual(t, elapsed.Milliseconds(), int64(maximalDelay429+100),
				"429 elapsed time should be at most p100 + buffer")
			got429 = true
		} else {
			// 200 distribution: p0=30ms, p50=50ms, p100=80ms.
			require.GreaterOrEqualf(t, targetDelay.Milliseconds(), int64(minimalDelay200),
				"200 target delay should be at least p0 (%v)", minimalDelay200)
			require.LessOrEqualf(t, targetDelay.Milliseconds(), int64(maximalDelay200),
				"200 target delay should be at most p100 (%v)", maximalDelay200)
			// The actual elapsed time should be at most the maximal delay plus a buffer for the
			// upstream round-trip and network jitter.
			require.LessOrEqual(t, elapsed.Milliseconds(), int64(maximalDelay200+100),
				"200 elapsed time should be at most p100 + buffer")
			// When the sampled status is 200, upstream response passes through.
			gotUpstream = true
		}
		return gotUpstream && got429 // By requiring that both statuses are seen, we ensure we see both 200 and 429 at one point.
	})
}

func TestUpstreamTimeIsSubtracted(t *testing.T) {
	minimalDelay := 20
	maximalDelay := 300
	// httpbin's /delay/0.05 simulates a ~50ms upstream response time.
	upstreamDelayMs := 50
	config := fmt.Sprintf(`{
	"endpoints": [
		{
			"match": {"prefix": "/delay"},
			"responses": [
				{
					"status": 200,
					"resolution": 1000,
					"distribution": {
						"p0.0": "%dms",
						"p50.0": "50ms",
						"p90.0": "100ms",
						"p99.0": "200ms",
						"p100.0": "%dms"
					}
				}
			]
		}
	]
}`, minimalDelay, maximalDelay)

	proxyPort := startFaultInjectionEnvoy(t, config)

	// Use httpbin's /delay endpoint to simulate a slow upstream.
	// With /delay/0.05 (50ms upstream) and our distribution (p0=20ms..p100=300ms),
	// the filter should see actual upstream time and subtract it from the target.
	req, err := http.NewRequest("GET", fmt.Sprintf("http://localhost:%d/delay/0.05", proxyPort), nil)
	require.NoError(t, err)

	internaltesting.RequireEventuallyRequestWithTiming(t, req, func(resp *http.Response, elapsed time.Duration) bool {
		_, _ = io.ReadAll(resp.Body)

		t.Logf("upstream_subtraction: status=%d elapsed=%v target=%s upstream=%s added=%s",
			resp.StatusCode, elapsed,
			resp.Header.Get("x-fault-injected-delay"),
			resp.Header.Get("x-fault-actual-upstream"),
			resp.Header.Get("x-fault-added-delay"))

		require.Equal(t, 200, resp.StatusCode)

		// The target delay should be within the distribution range (20-300ms).
		delayHeader := resp.Header.Get("x-fault-injected-delay")
		require.NotEmpty(t, delayHeader, "x-fault-injected-delay header should be set")
		targetDelay, err := time.ParseDuration(delayHeader)
		require.NoError(t, err)
		require.GreaterOrEqualf(t, targetDelay.Milliseconds(), int64(minimalDelay),
			"target delay should be at least p0 (%v)", targetDelay)
		require.LessOrEqualf(t, targetDelay.Milliseconds(), int64(maximalDelay),
			"target delay should be at most p100 (%v)", targetDelay)

		// The upstream time should reflect httpbin's /delay/0.05 (~50ms).
		upstreamHeader := resp.Header.Get("x-fault-actual-upstream")
		require.NotEmpty(t, upstreamHeader, "x-fault-actual-upstream header should be set")
		upstreamTime, err := time.ParseDuration(upstreamHeader)
		require.NoError(t, err)
		require.GreaterOrEqual(t, upstreamTime.Milliseconds(), int64(upstreamDelayMs-10),
			"upstream time should reflect httpbin /delay/0.05 (~50ms)")

		// If the target was less than upstream time, no additional delay should be added.
		// If greater, the added delay should be approximately target - upstream.
		if addedHeader := resp.Header.Get("x-fault-added-delay"); addedHeader != "" {
			addedDelay, err := time.ParseDuration(addedHeader)
			require.NoError(t, err)
			// Added delay should never exceed the target.
			require.LessOrEqual(t, addedDelay.Milliseconds(), targetDelay.Milliseconds(),
				"added delay should not exceed target delay")
		}

		// The actual elapsed time should be at least the upstream time (the request cannot
		// complete faster than the slow upstream) and at most the maximal target delay plus a
		// buffer for the upstream round-trip and network jitter.

		requireMinimalMaximalAndAverageDurations(t,
			[]time.Duration{elapsed},
			upstreamTime, 20*time.Millisecond,
			time.Duration(minimalDelay)*time.Millisecond,
			time.Duration(maximalDelay)*time.Millisecond,
			100*time.Millisecond)
		require.GreaterOrEqual(t, elapsed.Milliseconds(), int64(upstreamDelayMs-10),
			"elapsed time should be at least the upstream delay (~50ms)")
		require.LessOrEqual(t, elapsed.Milliseconds(), int64(maximalDelay+100),
			"elapsed time should be at most p100 + buffer")

		return true
	})
}

// startFaultInjectionEnvoy starts an Envoy instance with the dynamic-fault-injection
// extension configured with the given JSON config. It returns the proxy port.
func startFaultInjectionEnvoy(t *testing.T, config string) (proxyPort int) {
	t.Helper()
	ports := internaltesting.FreePorts(t, 2)
	proxyPort, adminPort := ports[0], ports[1]

	internaltesting.RunEnvoy(t, cliBin, proxyPort, adminPort,
		"--log-level", "dynamic_modules:debug",
		"--local", "../../composer/dynamic-fault-injection",
		"--config", config)

	return proxyPort
}

func requireMinimalMaximalAndAverageDurations(t *testing.T, durations []time.Duration, expectedAvgDelay, averageTolerance, expectedMinDelay, expectedMaxDelay, tolerance time.Duration) {
	var totalDelay time.Duration
	var actualMinimalDelay time.Duration
	var actualMaximalDelay time.Duration
	for _, d := range durations {
		totalDelay += d
		if d < actualMinimalDelay || actualMinimalDelay == 0 {
			actualMinimalDelay = d
		}
		if d > actualMaximalDelay {
			actualMaximalDelay = d
		}
	}
	numRequests := len(durations)
	avgDelay := totalDelay / time.Duration(numRequests)
	t.Logf("average request time: %v", avgDelay)

	require.GreaterOrEqual(t, actualMinimalDelay.Milliseconds(), int64(expectedMinDelay),
		fmt.Sprintf("elapsed delays should be at least p0 (%dms)", expectedMinDelay))
	require.LessOrEqual(t, actualMaximalDelay.Milliseconds(), (expectedMaxDelay + tolerance).Milliseconds(),
		fmt.Sprintf("elapsed delay should be at most p100 + some buffer (%dms + %dms)", expectedMaxDelay, tolerance))

	// Average should be roughly around 50ms (p50 of distribution).
	require.InDelta(t, expectedAvgDelay,
		avgDelay,
		float64(averageTolerance.Milliseconds()),
		"average delay should be %v ± %v [%v,%v, got %v]", expectedAvgDelay, averageTolerance, expectedAvgDelay-averageTolerance, expectedAvgDelay+averageTolerance, avgDelay)

}
