// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package integration

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	internaltesting "github.com/tetratelabs/built-on-envoy/internal/testing"
)

var dynamicFaultInjectionActiveRequestCountUpperBound = internaltesting.NewEnvVar(
	"TEST_DYNAMIC_FAULT_INJECTION_ACTIVE_REQUEST_COUNT_UPPER_BOUND",
	"Upper bound for the scaled active request count E2E test",
	2000,
)

func TestDynamicFaultInjectionDistributionDelay(t *testing.T) {
	minimalDelay := millis(20)
	maximalDelay := millis(300)
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
}`, minimalDelay.Milliseconds(), maximalDelay.Milliseconds())
	fmt.Printf("Using config: %s\n", config)
	proxyPort := startDynamicFaultInjectionEnvoy(t, config)

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
			require.GreaterOrEqual(t, targetDelay.Milliseconds(), minimalDelay.Milliseconds(),
				"target delay should be at least p0 (20ms)")
			require.LessOrEqual(t, targetDelay.Milliseconds(), maximalDelay.Milliseconds(),
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
	require.GreaterOrEqual(t, actualMinimalDelay, minimalDelay,
		"elapsed delays should be at least p0 (20ms)")
	require.LessOrEqual(t, actualMaximalDelay, maximalDelay+millis(100), // the 100ms is rather arbitrary buffer for network jitter, etc.
		"elapsed delay should be at most p100 + some buffer (300ms + 100ms)")

	// Average should be roughly around 50ms (p50 of distribution).
	require.Greater(t, avgDelay, millis(20),
		"average delay should be meaningful (got %v)", avgDelay)
}

func TestDynamicFaultInjectionDirectBehaviorConfigurationPassThroughForUnconfiguredStatus(t *testing.T) {
	config := `{
  "responses": [
    {
      "status": 200,
      "resolution": 1000,
      "distribution": {"p0.0": "1ms", "p100.0": "5ms"}
    }
  ]
}`
	proxyPort := startDynamicFaultInjectionEnvoy(t, config)
	req, err := http.NewRequest("GET", fmt.Sprintf("http://localhost:%d/direct", proxyPort), nil)
	require.NoError(t, err)

	internaltesting.RequireEventuallyRequestWithTiming(t, req, func(resp *http.Response, _ time.Duration) bool {
		defer func() { _ = resp.Body.Close() }()
		_, _ = io.ReadAll(resp.Body)
		require.Equal(t, http.StatusNotFound, resp.StatusCode)
		require.Equal(t, "404", resp.Header.Get("x-fault-status"))
		require.Equal(t, "0s", resp.Header.Get("x-fault-injected-delay"))
		require.Equal(t, "0s", resp.Header.Get("x-fault-added-delay"))
		require.NotEmpty(t, resp.Header.Get("x-fault-actual-upstream"))
		return true
	})
}

func TestDynamicFaultInjectionGlobalActiveRequestCountAcrossWorkers(t *testing.T) {
	testDynamicFaultInjectionGlobalActiveRequestCount(t, "/status/200", 8, 15*time.Second)
}

func TestDynamicFaultInjectionGlobalActiveRequestCountAtScale(t *testing.T) {
	requestCount := dynamicFaultInjectionActiveRequestCountUpperBound.Get()
	gate := startRequestGate(t, requestCount)
	proxyPort := startDynamicFaultInjectionActiveRequestCountEnvoyWithUpstream(t, "/status/200", gate.upstream)
	requireDynamicFaultInjectionGlobalActiveRequestCounts(t, proxyPort, "/status/200", requestCount, 30*time.Second, gate)
}

func testDynamicFaultInjectionGlobalActiveRequestCount(t *testing.T, path string, upperBound int, timeout time.Duration) {
	t.Helper()
	require.Positive(t, upperBound)
	gate := startRequestGate(t, upperBound)
	proxyPort := startDynamicFaultInjectionActiveRequestCountEnvoyWithUpstream(t, path, gate.upstream)
	requireDynamicFaultInjectionGlobalActiveRequestCounts(t, proxyPort, path, upperBound, timeout, gate)
}

func startDynamicFaultInjectionActiveRequestCountEnvoyWithUpstream(t *testing.T, upstreamPath, upstream string) int {
	t.Helper()
	routes := "- match:\n" +
		"    prefix: " + upstreamPath + "\n" +
		"  typed_per_filter_config:\n" +
		"    dynamic-fault-injection:\n" +
		"      '@type': type.googleapis.com/envoy.extensions.filters.http.dynamic_modules.v3.DynamicModuleFilterPerRoute\n" +
		"      dynamic_module_config: { name: composer }\n" +
		"      filter_name: dynamic-fault-injection\n" +
		"      filter_config:\n" +
		"        '@type': type.googleapis.com/google.protobuf.StringValue\n" +
		"        value: |\n" +
		"          diagnostic: true\n" +
		"          responses:\n" +
		"            - status: 200\n" +
		"              resolution: 1\n" +
		"              distribution:\n" +
		"                p0.0: 3s\n" +
		"                p100.0: 3s\n" +
		"  route:\n" +
		"    cluster: test-upstream\n" +
		"    timeout: 30s"
	if upstream == "" {
		return startDynamicFaultInjectionEnvoyWithRoutes(t, routes)
	}
	return startDynamicFaultInjectionEnvoyWithRoutesAndUpstream(t, routes, upstream)
}

type requestGate struct {
	upstream string
	arrived  <-chan struct{}
	release  func()
}

func startRequestGate(t *testing.T, requestCount int) *requestGate {
	t.Helper()
	arrived := make(chan struct{}, requestCount)
	releaseChannel := make(chan struct{})
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() { close(releaseChannel) })
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case arrived <- struct{}{}:
		case <-r.Context().Done():
			return
		}
		select {
		case <-releaseChannel:
			w.WriteHeader(http.StatusOK)
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() {
		release()
		server.Close()
	})
	return &requestGate{
		upstream: strings.TrimPrefix(server.URL, "http://"),
		arrived:  arrived,
		release:  release,
	}
}

func requireDynamicFaultInjectionGlobalActiveRequestCounts(t *testing.T, proxyPort int, path string, requestCount int, timeout time.Duration, gate *requestGate) {
	t.Helper()
	type requestResult struct {
		count       int
		workerIndex int
	}

	client := &http.Client{
		Transport: &http.Transport{
			DisableKeepAlives:   true,
			MaxConnsPerHost:     requestCount,
			MaxIdleConns:        requestCount,
			MaxIdleConnsPerHost: requestCount,
		},
		Timeout: timeout,
	}
	defer client.CloseIdleConnections()

	results := make(chan requestResult, requestCount)
	errors := make(chan error, requestCount)

	startRequest := func() {
		go func() {
			resp, err := client.Get(fmt.Sprintf("http://localhost:%d%s", proxyPort, path))
			if err != nil {
				errors <- err
				return
			}
			defer func() { _ = resp.Body.Close() }()
			_, _ = io.ReadAll(resp.Body)
			if resp.StatusCode != http.StatusOK {
				errors <- fmt.Errorf("request returned status %d", resp.StatusCode)
				return
			}
			count, err := strconv.Atoi(resp.Header.Get("x-fault-requests-in-flight"))
			if err != nil {
				errors <- fmt.Errorf("invalid active request count: %w", err)
				return
			}
			workerIndex, err := strconv.Atoi(resp.Header.Get("x-fault-worker-index"))
			if err != nil {
				errors <- fmt.Errorf("invalid worker index: %w", err)
				return
			}
			results <- requestResult{count: count, workerIndex: workerIndex}
		}()
	}

	admissionTimer := time.NewTimer(timeout)
	defer admissionTimer.Stop()
	for range requestCount {
		startRequest()
		select {
		case <-gate.arrived:
		case err := <-errors:
			require.NoError(t, err)
		case <-admissionTimer.C:
			t.Fatalf("timed out waiting for request admission")
		}
	}
	gate.release()

	counts := make([]int, 0, requestCount)
	workerIndexes := make(map[int]struct{})
	timeoutTimer := time.NewTimer(timeout)
	defer timeoutTimer.Stop()
	for range requestCount {
		select {
		case err := <-errors:
			require.NoError(t, err)
		case result := <-results:
			counts = append(counts, result.count)
			workerIndexes[result.workerIndex] = struct{}{}
		case <-timeoutTimer.C:
			t.Fatalf("timed out waiting for %d active request count responses", requestCount)
		}
	}

	sort.Ints(counts)
	for expected, count := range counts {
		require.Equal(t, expected, count, "request-entry counts should contain every value from zero to request count minus one")
	}
	require.GreaterOrEqual(t, len(workerIndexes), 2, "requests should be handled by multiple Envoy workers")
}

func TestDynamicFaultInjectionPerRouteConfiguration(t *testing.T) {
	routes := "- match:\n" +
		"    prefix: /anything/\n" +
		"    headers:\n" +
		"      - name: :method\n" +
		"        exact_match: GET\n" +
		"  typed_per_filter_config:\n" +
		"    dynamic-fault-injection:\n" +
		"      '@type': type.googleapis.com/envoy.extensions.filters.http.dynamic_modules.v3.DynamicModuleFilterPerRoute\n" +
		"      dynamic_module_config: { name: composer }\n" +
		"      filter_name: dynamic-fault-injection\n" +
		"      filter_config:\n" +
		"        '@type': type.googleapis.com/google.protobuf.StringValue\n" +
		"        value: |\n" +
		"          responses:\n" +
		"            - status: 200\n" +
		"              resolution: 100\n" +
		"              distribution:\n" +
		"                p0.0: 20ms\n" +
		"                p100.0: 20ms\n" +
		"  route:\n" +
		"    cluster: test-upstream"
	proxyPort := startDynamicFaultInjectionEnvoyWithRoutes(t, routes)
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://localhost:%d/anything/per-route", proxyPort), nil)
	require.NoError(t, err)

	internaltesting.RequireEventuallyRequestWithTiming(t, req, func(resp *http.Response, elapsed time.Duration) bool {
		defer func() { _ = resp.Body.Close() }()
		_, _ = io.ReadAll(resp.Body)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		require.Equal(t, "200", resp.Header.Get("x-fault-status"))
		require.Equal(t, "20ms", resp.Header.Get("x-fault-injected-delay"))
		require.GreaterOrEqual(t, elapsed, 20*time.Millisecond)
		return true
	})
}

func TestDynamicFaultInjectionAbortInjection(t *testing.T) {
	minimalDelay := millis(0)
	maximalDelay := millis(5)
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
}`, minimalDelay.Milliseconds(), maximalDelay.Milliseconds())

	proxyPort := startDynamicFaultInjectionEnvoy(t, config)

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
		require.GreaterOrEqual(t, targetDelay, minimalDelay,
			"target delay should be at least p0")
		require.LessOrEqual(t, targetDelay, maximalDelay,
			"target delay should be within distribution range")

		// The actual elapsed time should be at most the maximal delay plus a buffer for the
		// upstream round-trip and network jitter.
		require.LessOrEqual(t, elapsed, maximalDelay+millis(100),
			"elapsed time should be at most p100 + buffer")

		return true
	})
}

func TestDynamicFaultInjectionFixedDelayAccountsForUpstream(t *testing.T) {
	fixedDelay := millis(100)
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
}`, fixedDelay.Milliseconds(), fixedDelay.Milliseconds())

	proxyPort := startDynamicFaultInjectionEnvoy(t, config)

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
		require.GreaterOrEqual(t, elapsed, fixedDelay-millis(10),
			"total request time should be at least ~100ms (±10m for jitter)")
		require.LessOrEqual(t, elapsed, fixedDelay+millis(400),
			"total request time should not be excessively long")

		// The target header should show the fixed delay.
		delayHeader := resp.Header.Get("x-fault-injected-delay")
		require.Equal(t, fmt.Sprintf("%dms", fixedDelay.Milliseconds()), delayHeader)

		// The actual upstream time should be much less than the fixed delay.
		upstreamHeader := resp.Header.Get("x-fault-actual-upstream")
		require.NotEmpty(t, upstreamHeader)
		upstreamTime, err := time.ParseDuration(upstreamHeader)
		require.NoError(t, err)
		require.Less(t, upstreamTime, fixedDelay-millis(10),
			"upstream time for /status/200 should be well under the fixed delay")

		// The filter should have added the remaining delay (fixedDelay - upstream).
		addedHeader := resp.Header.Get("x-fault-added-delay")
		require.NotEmpty(t, addedHeader, "x-fault-added-delay should be set when target > upstream")
		addedDelay, err := time.ParseDuration(addedHeader)
		require.NoError(t, err)

		// upstream + added should approximate the fixed delay target.
		totalDelay := upstreamTime + addedDelay
		require.GreaterOrEqual(t, totalDelay, fixedDelay-millis(10),
			"upstream + added delay should be at least ~100ms")
		require.LessOrEqual(t, totalDelay, fixedDelay+millis(50),
			"upstream + added delay should not overshoot significantly")

		return true
	})
}

func TestDynamicFaultInjectionCatchallEndpoint(t *testing.T) {
	minimalDelay := millis(5)
	maximalDelay := millis(20)
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
}`, minimalDelay.Milliseconds(), maximalDelay.Milliseconds())

	proxyPort := startDynamicFaultInjectionEnvoy(t, config)

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
		require.GreaterOrEqual(t, targetDelay, minimalDelay,
			"target delay should be at least p0")
		require.LessOrEqual(t, targetDelay, maximalDelay,
			"target delay should be at most p100")

		// The actual elapsed time should be at most the maximal delay plus a buffer for the
		// upstream round-trip and network jitter.
		require.LessOrEqual(t, elapsed, maximalDelay+millis(100),
			"elapsed time should be at most p100 + buffer")

		return true
	})
}

func TestDynamicFaultInjectionMixedStatusCodes(t *testing.T) {
	// Distribution bounds for the two sampled statuses.
	minimalDelay200 := millis(30)
	maximalDelay200 := millis(80)
	minimalDelay429 := millis(0)
	maximalDelay429 := millis(5)
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
}`, minimalDelay200.Milliseconds(), maximalDelay200.Milliseconds(), minimalDelay429.Milliseconds(), maximalDelay429.Milliseconds())

	proxyPort := startDynamicFaultInjectionEnvoy(t, config)

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
			require.GreaterOrEqualf(t, targetDelay, minimalDelay429,
				"429 target delay should be at least p0 (%v)", minimalDelay429)
			require.LessOrEqual(t, targetDelay, maximalDelay429,
				"429 target delay should be within distribution range (%v - %v)", minimalDelay429, maximalDelay429)
			// The actual elapsed time should be at most the maximal delay plus a buffer for the
			// upstream round-trip and network jitter.
			require.LessOrEqual(t, elapsed, maximalDelay429+millis(100),
				"429 elapsed time should be at most p100 + buffer")
			got429 = true
		} else {
			if resp.StatusCode == 200 {
				// 200 distribution: p0=30ms, p50=50ms, p100=80ms.
				require.GreaterOrEqualf(t, targetDelay, minimalDelay200,
					"200 target delay should be at least p0 (%v)", minimalDelay200)
				require.LessOrEqualf(t, targetDelay, maximalDelay200,
					"200 target delay should be at most p100 (%v)", maximalDelay200)
				// The actual elapsed time should be at most the maximal delay plus a buffer for the
				// upstream round-trip and network jitter.
				require.LessOrEqual(t, elapsed, maximalDelay200+millis(100),
					"200 elapsed time should be at most p100 + buffer")
				gotUpstream = true
			} else {
				// Unconfigured upstream statuses pass through and expose zero injected delay.
				require.Equal(t, http.StatusNotFound, resp.StatusCode)
				require.Equal(t, time.Duration(0), targetDelay)
				require.Equal(t, "404", resp.Header.Get("x-fault-status"))
				require.Equal(t, "0s", resp.Header.Get("x-fault-added-delay"))
				require.NotEmpty(t, resp.Header.Get("x-fault-actual-upstream"))
				gotUpstream = true
			}
		}
		return gotUpstream && got429 // By requiring that both statuses are seen, we ensure we see both 200 and 429 at one point.
	})
}

func TestDynamicFaultInjectionUpstreamTimeIsSubtracted(t *testing.T) {
	minimalDelay := millis(20)
	maximalDelay := millis(100)
	// httpbin's /delay/0.05 simulates a ~50ms upstream response time.
	upstreamDelay := millis(50)
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
						"p99.0": "50ms",
						"p100.0": "%dms"
					}
				}
			]
		}
	]
}`, minimalDelay.Milliseconds(), maximalDelay.Milliseconds())

	proxyPort := startDynamicFaultInjectionEnvoy(t, config)

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
		require.GreaterOrEqualf(t, targetDelay, minimalDelay,
			"target delay should be at least p0 (%v)", targetDelay)
		require.LessOrEqualf(t, targetDelay, maximalDelay,
			"target delay should be at most p100 (%v)", targetDelay)

		// The upstream time should reflect httpbin's /delay/0.05 (~50ms).
		upstreamHeader := resp.Header.Get("x-fault-actual-upstream")
		require.NotEmpty(t, upstreamHeader, "x-fault-actual-upstream header should be set")
		upstreamTime, err := time.ParseDuration(upstreamHeader)
		require.NoError(t, err)
		require.GreaterOrEqual(t, upstreamTime, upstreamDelay-millis(10),
			"upstream time should reflect httpbin /delay/0.05 (~50ms)")

		// If the target was less than upstream time, no additional delay should be added.
		// If greater, the added delay should be approximately target - upstream.
		if addedHeader := resp.Header.Get("x-fault-added-delay"); addedHeader != "" {
			addedDelay, err := time.ParseDuration(addedHeader)
			require.NoError(t, err)
			// Added delay should never exceed the target.
			require.LessOrEqual(t, addedDelay, targetDelay,
				"added delay should not exceed target delay")
		}

		// The actual elapsed time should be at least the upstream time (the request cannot
		// complete faster than the slow upstream) and at most the maximal target delay plus a
		// buffer for the upstream round-trip and network jitter.

		requireMinimalMaximalAndAverageDurations(t,
			[]time.Duration{elapsed},
			upstreamTime, millis(30),
			minimalDelay,
			maximalDelay,
			millis(100))
		require.GreaterOrEqual(t, elapsed, upstreamDelay-millis(10),
			"elapsed time should be at least the upstream delay (~50ms)")
		require.LessOrEqual(t, elapsed, maximalDelay+millis(100),
			"elapsed time should be at most p100 + buffer")

		return true
	})
}

// startDynamicFaultInjectionEnvoy starts an Envoy instance with the dynamic-fault-injection
// extension configured with the given JSON config. It returns the proxy port.
func startDynamicFaultInjectionEnvoy(t *testing.T, config string) (proxyPort int) {
	t.Helper()
	ports := internaltesting.FreePorts(t, 2)
	proxyPort, adminPort := ports[0], ports[1]

	internaltesting.RunEnvoy(t, cliBin, proxyPort, adminPort,
		"--log-level", "dynamic_modules:debug",
		"--local", "../../composer/dynamic-fault-injection",
		"--config", config)

	return proxyPort
}

// startDynamicFaultInjectionEnvoyWithRoutes provides shared bootstrap wiring while
// callers specify only the route configuration under test.
func startDynamicFaultInjectionEnvoyWithRoutes(t *testing.T, routes string) (proxyPort int) {
	upstream := internaltesting.TestUpstreamClusterInsecure.Get()
	require.NotEmpty(t, upstream)
	return startDynamicFaultInjectionEnvoyWithRoutesAndUpstream(t, routes, upstream)
}

func startDynamicFaultInjectionEnvoyWithRoutesAndUpstream(t *testing.T, routes, upstream string) (proxyPort int) {
	t.Helper()
	ports := internaltesting.FreePorts(t, 2)
	proxyPort, adminPort := ports[0], ports[1]
	moduleDir := t.TempDir()
	modulePath := filepath.Join(moduleDir, "libcomposer.so")
	build := exec.Command("go", "build", "-trimpath", "-buildmode=c-shared", "-o", modulePath, "./main") // #nosec G204 -- fixed command and repository-relative package.
	build.Dir = "../../composer"
	output, err := build.CombinedOutput()
	require.NoErrorf(t, err, "build composer: %s", output)

	upstreamHost, upstreamPort, err := net.SplitHostPort(upstream)
	require.NoError(t, err)
	upstreamPortNumber, err := strconv.ParseUint(upstreamPort, 10, 32)
	require.NoError(t, err)
	routeConfig := strings.TrimSpace(routes)
	routeConfig = "                        " + strings.ReplaceAll(routeConfig, "\n", "\n                        ")
	bootstrap := fmt.Sprintf(`admin:
  address:
    socket_address: { address: 127.0.0.1, port_value: %d }
static_resources:
  listeners:
    - name: main
      address:
        socket_address: { address: 127.0.0.1, port_value: %d }
      filter_chains:
        - filters:
            - name: envoy.filters.network.http_connection_manager
              typed_config:
                "@type": type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager
                stat_prefix: ingress_http
                http_filters:
                  - name: dynamic-fault-injection
                    typed_config:
                      "@type": type.googleapis.com/envoy.extensions.filters.http.dynamic_modules.v3.DynamicModuleFilter
                      dynamic_module_config: { name: composer }
                      filter_name: dynamic-fault-injection
                      filter_config:
                        "@type": type.googleapis.com/google.protobuf.StringValue
                        value: "endpoints: []"
                  - name: envoy.filters.http.router
                    typed_config:
                      "@type": type.googleapis.com/envoy.extensions.filters.http.router.v3.Router
                route_config:
                  name: routes
                  virtual_hosts:
                    - name: default
                      domains: ["*"]
                      routes:
%s
  clusters:
    - name: test-upstream
      circuit_breakers:
        thresholds:
          - max_connections: 10000
            max_pending_requests: 10000
            max_requests: 10000
      type: STATIC
      load_assignment:
        cluster_name: test-upstream
        endpoints:
          - lb_endpoints:
              - endpoint:
                  address:
                    socket_address: { address: %s, port_value: %d }
`, adminPort, proxyPort, routeConfig, upstreamHost, upstreamPortNumber)
	internaltesting.RunEnvoyYAML(t, proxyPort, adminPort, bootstrap, map[string]string{
		"ENVOY_DYNAMIC_MODULES_SEARCH_PATH": moduleDir,
		"GODEBUG":                           "cgocheck=0",
	}, "--concurrency", "4")
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

	require.GreaterOrEqualf(t, actualMinimalDelay, expectedMinDelay,
		"elapsed delays should be at least p0 (%dms)", expectedMinDelay)
	require.LessOrEqualf(t, actualMaximalDelay, expectedMaxDelay+tolerance,
		"elapsed delay should be at most p100 + some buffer (%dms + %dms)", expectedMaxDelay, tolerance)

	// Average should be roughly around 50ms (p50 of distribution).
	require.InDelta(t, expectedAvgDelay.Milliseconds(),
		avgDelay.Milliseconds(),
		float64(averageTolerance.Milliseconds()),
		"average delay should be %v ± %v [%v,%v, got %v]", expectedAvgDelay, averageTolerance, expectedAvgDelay-averageTolerance, expectedAvgDelay+averageTolerance, avgDelay)
}

func millis(ms int) time.Duration {
	return time.Duration(ms) * time.Millisecond
}
