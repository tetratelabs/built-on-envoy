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
	const config = `{
	"endpoints": [
		{
			"match": {"prefix": "/delay"},
			"responses": [
				{
					"status": 200,
					"resolution": 1000,
					"distribution": {
						"p0.0": "20ms",
						"p50.0": "50ms",
						"p90.0": "100ms",
						"p99.0": "200ms",
						"p100.0": "300ms"
					}
				}
			]
		}
	]
}`

	proxyPort := startFaultInjectionEnvoy(t, config)

	// Requests to /delay have distribution: p0=20ms, p50=50ms, p90=100ms, p99=200ms, p100=300ms.
	var durations []time.Duration
	const numRequests = 20

	for i := range numRequests {
		start := time.Now()
		req, err := http.NewRequest("GET", fmt.Sprintf("http://localhost:%d/delay/0", proxyPort), nil)
		require.NoError(t, err)

		internaltesting.RequireEventuallyRequest(t, req, func(resp *http.Response) bool {
			elapsed := time.Since(start)
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
			require.GreaterOrEqual(t, targetDelay.Milliseconds(), int64(20),
				"target delay should be at least p0 (20ms)")
			require.LessOrEqual(t, targetDelay.Milliseconds(), int64(300),
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
	for _, d := range durations {
		totalDelay += d
	}
	avgDelay := totalDelay / time.Duration(numRequests)
	t.Logf("average request time: %v", avgDelay)

	// Average should be roughly around 50ms (p50 of distribution).
	require.Greater(t, avgDelay.Milliseconds(), int64(20),
		"average delay should be meaningful (got %v)", avgDelay)
}

func TestAbortInjection(t *testing.T) {
	const config = `{
	"endpoints": [
		{
			"match": {"prefix": "/abort"},
			"responses": [
				{
					"status": 503,
					"resolution": 1000,
					"distribution": {
						"p0.0": "0ms",
						"p100.0": "5ms"
					}
				}
			]
		}
	]
}`

	proxyPort := startFaultInjectionEnvoy(t, config)

	// Requests to /abort: all responses sampled as 503.
	req, err := http.NewRequest("GET", fmt.Sprintf("http://localhost:%d/abort/test", proxyPort), nil)
	require.NoError(t, err)

	internaltesting.RequireEventuallyRequest(t, req, func(resp *http.Response) bool {
		body, _ := io.ReadAll(resp.Body)

		t.Logf("abort response: status=%d body=%s", resp.StatusCode, string(body))
		require.Equal(t, 503, resp.StatusCode)
		require.Contains(t, string(body), "fault filter abort")
		require.Equal(t, "abort", resp.Header.Get("x-fault-injected"))

		// The abort distribution is p0=0ms, p100=5ms — target delay should be within range.
		delayHeader := resp.Header.Get("x-fault-injected-delay")
		require.NotEmpty(t, delayHeader, "x-fault-injected-delay header should be set")
		targetDelay, err := time.ParseDuration(delayHeader)
		require.NoError(t, err)
		require.LessOrEqual(t, targetDelay.Milliseconds(), int64(5),
			"target delay should be within distribution range (0-5ms)")

		return true
	})
}

func TestFixedDelayAccountsForUpstream(t *testing.T) {
	const config = `{
	"endpoints": [
		{
			"match": {"prefix": "/"},
			"responses": [
				{
					"status": 200,
					"resolution": 1000,
					"distribution": {
						"p0.0": "100ms",
						"p100.0": "100ms"
					}
				}
			]
		}
	]
}`

	proxyPort := startFaultInjectionEnvoy(t, config)

	// Flat 100ms distribution. The filter should only add (100ms - actual_upstream_time)
	// as additional delay.
	req, err := http.NewRequest("GET", fmt.Sprintf("http://localhost:%d/status/200", proxyPort), nil)
	require.NoError(t, err)

	internaltesting.RequireEventuallyRequest(t, req, func(resp *http.Response) bool {
		_, _ = io.ReadAll(resp.Body)

		t.Logf("fixed delay: status=%d target=%s upstream=%s added=%s",
			resp.StatusCode,
			resp.Header.Get("x-fault-injected-delay"),
			resp.Header.Get("x-fault-actual-upstream"),
			resp.Header.Get("x-fault-added-delay"))

		require.Equal(t, 200, resp.StatusCode)

		// The target header should show 100ms.
		delayHeader := resp.Header.Get("x-fault-injected-delay")
		require.Equal(t, "100ms", delayHeader)

		// The actual upstream time should be much less than 100ms.
		upstreamHeader := resp.Header.Get("x-fault-actual-upstream")
		require.NotEmpty(t, upstreamHeader)
		upstreamTime, err := time.ParseDuration(upstreamHeader)
		require.NoError(t, err)
		require.Less(t, upstreamTime.Milliseconds(), int64(90),
			"upstream time for /status/200 should be well under 100ms")

		// The filter should have added the remaining delay (100ms - upstream).
		addedHeader := resp.Header.Get("x-fault-added-delay")
		require.NotEmpty(t, addedHeader, "x-fault-added-delay should be set when target > upstream")
		addedDelay, err := time.ParseDuration(addedHeader)
		require.NoError(t, err)

		// upstream + added should approximate the 100ms target.
		totalDelay := upstreamTime + addedDelay
		require.Greater(t, totalDelay.Milliseconds(), int64(90),
			"upstream + added delay should be at least ~100ms")
		require.Less(t, totalDelay.Milliseconds(), int64(150),
			"upstream + added delay should not overshoot significantly")

		return true
	})
}

func TestCatchallEndpoint(t *testing.T) {
	const config = `{
	"endpoints": [
		{
			"match": {"prefix": "/"},
			"responses": [
				{
					"status": 200,
					"resolution": 1000,
					"distribution": {
						"p0.0": "5ms",
						"p50.0": "10ms",
						"p100.0": "20ms"
					}
				}
			]
		}
	]
}`

	proxyPort := startFaultInjectionEnvoy(t, config)

	// Requests to paths that don't match specific prefixes hit the "/" catch-all.
	// Distribution: p0=5ms, p50=10ms, p100=20ms.
	req, err := http.NewRequest("GET", fmt.Sprintf("http://localhost:%d/status/200", proxyPort), nil)
	require.NoError(t, err)

	internaltesting.RequireEventuallyRequest(t, req, func(resp *http.Response) bool {
		_, _ = io.ReadAll(resp.Body)

		t.Logf("catchall: status=%d target=%s upstream=%s",
			resp.StatusCode,
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
		require.GreaterOrEqual(t, targetDelay.Milliseconds(), int64(5),
			"target delay should be at least p0 (5ms)")
		require.LessOrEqual(t, targetDelay.Milliseconds(), int64(20),
			"target delay should be at most p100 (20ms)")

		return true
	})
}

func TestMixedStatusCodes(t *testing.T) {
	const config = `{
	"endpoints": [
		{
			"match": {"prefix": "/mixed"},
			"responses": [
				{
					"status": 200,
					"resolution": 500,
					"distribution": {
						"p0.0": "30ms",
						"p50.0": "50ms",
						"p100.0": "80ms"
					}
				},
				{
					"status": 429,
					"resolution": 500,
					"distribution": {
						"p0.0": "0ms",
						"p100.0": "5ms"
					}
				}
			]
		}
	]
}`

	proxyPort := startFaultInjectionEnvoy(t, config)

	// Requests to /mixed: 50% -> 200 (30-80ms delay, upstream response passes through),
	// 50% -> 429 abort (overrides upstream response).
	gotUpstream := false
	got429 := false

	req, err := http.NewRequest("GET", fmt.Sprintf("http://localhost:%d/mixed/test", proxyPort), nil)
	require.NoError(t, err)

	internaltesting.RequireEventuallyRequest(t, req, func(resp *http.Response) bool {
		_, _ = io.ReadAll(resp.Body)

		t.Logf("mixed: status=%d fault-status=%s delay=%s",
			resp.StatusCode, resp.Header.Get("x-fault-status"),
			resp.Header.Get("x-fault-injected-delay"))

		// Assert timing based on which status was sampled.
		delayHeader := resp.Header.Get("x-fault-injected-delay")
		require.NotEmpty(t, delayHeader, "x-fault-injected-delay header should be set")
		targetDelay, err := time.ParseDuration(delayHeader)
		require.NoError(t, err)

		if resp.StatusCode == 429 {
			// 429 distribution: p0=0ms, p100=5ms.
			require.LessOrEqual(t, targetDelay.Milliseconds(), int64(5),
				"429 target delay should be within distribution range (0-5ms)")
			got429 = true
		} else {
			// 200 distribution: p0=30ms, p50=50ms, p100=80ms.
			require.GreaterOrEqual(t, targetDelay.Milliseconds(), int64(30),
				"200 target delay should be at least p0 (30ms)")
			require.LessOrEqual(t, targetDelay.Milliseconds(), int64(80),
				"200 target delay should be at most p100 (80ms)")
			// When the sampled status is 200, upstream response passes through.
			gotUpstream = true
		}
		return gotUpstream && got429
	})
}

func TestUpstreamTimeIsSubtracted(t *testing.T) {
	const config = `{
	"endpoints": [
		{
			"match": {"prefix": "/delay"},
			"responses": [
				{
					"status": 200,
					"resolution": 1000,
					"distribution": {
						"p0.0": "20ms",
						"p50.0": "50ms",
						"p90.0": "100ms",
						"p99.0": "200ms",
						"p100.0": "300ms"
					}
				}
			]
		}
	]
}`

	proxyPort := startFaultInjectionEnvoy(t, config)

	// Use httpbin's /delay endpoint to simulate a slow upstream.
	// With /delay/0.05 (50ms upstream) and our distribution (p0=20ms..p100=300ms),
	// the filter should see actual upstream time and subtract it from the target.
	req, err := http.NewRequest("GET", fmt.Sprintf("http://localhost:%d/delay/0.05", proxyPort), nil)
	require.NoError(t, err)

	internaltesting.RequireEventuallyRequest(t, req, func(resp *http.Response) bool {
		_, _ = io.ReadAll(resp.Body)

		t.Logf("upstream_subtraction: status=%d target=%s upstream=%s added=%s",
			resp.StatusCode,
			resp.Header.Get("x-fault-injected-delay"),
			resp.Header.Get("x-fault-actual-upstream"),
			resp.Header.Get("x-fault-added-delay"))

		require.Equal(t, 200, resp.StatusCode)

		// The target delay should be within the distribution range (20-300ms).
		delayHeader := resp.Header.Get("x-fault-injected-delay")
		require.NotEmpty(t, delayHeader, "x-fault-injected-delay header should be set")
		targetDelay, err := time.ParseDuration(delayHeader)
		require.NoError(t, err)
		require.GreaterOrEqual(t, targetDelay.Milliseconds(), int64(20),
			"target delay should be at least p0 (20ms)")
		require.LessOrEqual(t, targetDelay.Milliseconds(), int64(300),
			"target delay should be at most p100 (300ms)")

		// The upstream time should reflect httpbin's /delay/0.05 (~50ms).
		upstreamHeader := resp.Header.Get("x-fault-actual-upstream")
		require.NotEmpty(t, upstreamHeader, "x-fault-actual-upstream header should be set")
		upstreamTime, err := time.ParseDuration(upstreamHeader)
		require.NoError(t, err)
		require.GreaterOrEqual(t, upstreamTime.Milliseconds(), int64(40),
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
