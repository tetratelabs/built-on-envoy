# Envoy Latency and Fault Distribution Simulation

An Envoy [dynamic module](https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/advanced/dynamic_modules) **upstream HTTP filter** written in Go that injects latency and fault responses based on configurable percentile distributions.

This is similar to Envoy's built-in [fault injection filter](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/fault_filter.html) but adds support for **percentile-based latency distributions** with **per-status-code weighting** — allowing you to simulate realistic endpoint behavior including error rates, latency profiles, and load-dependent degradation.

**Key differentiator**: 
By operating as an **upstream filter on the cluster**, this module measures the actual upstream response time and only injects the *remaining* delay needed to reach the target distribution value. 
If the upstream is already slower than the target, no additional delay is added.

## Features

- **Percentile-based latency injection**: Configure latency distributions using flexible percentile notation (`p0.0`, `p50.0`, `p99.9`, `p100.0`)
- **Per-status-code distributions**: Define different latency profiles for different HTTP status codes (e.g., 200s are fast, 503s are slow)
- **Resolution-based weighting**: Use `resolution` as both the sampling accuracy and the relative weight for status code selection
- **Load-based behavior**: Configure different response profiles based on current RPS with smooth grey-zone transitions
- **Grey zone penalties**: Model degradation with spike detection, penalty multipliers, and recovery rates
- **Route matching**: Apply different fault configurations via prefix/exact path matching and header matching
- **First-match routing**: Endpoints are evaluated in order; first match wins
- **Upstream-aware timing**: Measures actual upstream latency and only adds the remaining delay to reach the target — avoids over-delaying when the upstream is naturally slow

## Configuration

The filter is configured as native YAML in the Envoy config using `google.protobuf.Struct` as the `filter_config` type. Envoy parses the YAML natively and serializes it as JSON to the module — no string escaping or `value: |` indirection needed.

### Configuration Schema

```yaml
$schema: https://github.com/spockz/built-on-envoy/blob/cea69ebebebc25a7f172724abc2697655ec08674/extensions/composer/dynamic-fault-injection/config.schema.json
endpoints:
  # Simple endpoint with weighted status codes
  - match:
      prefix: "/api/v1/users"
      headers:
        - name: "x-env"
          exact_match: "staging"
    responses:
      - status: 200
        resolution: 900          # 90% of responses are 200 OK
        distribution:
          p0.0: "1ms"
          p50.0: "10ms"
          p99.0: "200ms"
          p99.9: "500ms"
          p100.0: "1s"
      - status: 503
        resolution: 100          # 10% of responses are 503
        distribution:
          p0.0: "100ms"
          p50.0: "500ms"
          p100.0: "2s"

  # Load-based endpoint with grey zone behavior
  - match:
      prefix: "/api/v1/heavy"
    responses:
      - status: 200
        resolution: 1000
        distribution:
          p0.0: "5ms"
          p100.0: "50ms"
    load_based:
      healthy:
        threshold_rps: 100
        responses:
          - status: 200
            resolution: 950
            distribution:
              p0.0: "1ms"
              p50.0: "5ms"
              p100.0: "20ms"
          - status: 500
            resolution: 50
            distribution:
              p0.0: "10ms"
              p100.0: "50ms"
      tipping_point:
        threshold_rps: 500
        responses:
          - status: 200
            resolution: 500
            distribution:
              p0.0: "50ms"
              p50.0: "500ms"
              p100.0: "5s"
          - status: 503
            resolution: 500
            distribution:
              p0.0: "10ms"
              p100.0: "1s"
      grey_zone:
        penalty_base: "50ms"
        spike_threshold: 0.8
        spike_penalty_duration: "2s"
        spike_penalty_multiplier: 3.0
        recovery_rate: 0.5
```

### Configuration Fields

| Field | Description |
|-------|-------------|
| `endpoints` | Array of endpoint configurations. First match wins. |
| `endpoints[].match.prefix` | Match requests whose path starts with this prefix |
| `endpoints[].match.exact` | Match requests with exactly this path |
| `endpoints[].match.headers` | Array of header match conditions (all must match) |
| `endpoints[].responses` | Array of status-code distributions (weighted by resolution) |
| `endpoints[].responses[].status` | HTTP status code (100-599) |
| `endpoints[].responses[].resolution` | Weight for status selection AND number of pre-computed samples |
| `endpoints[].responses[].distribution` | Percentile-to-duration mapping |
| `endpoints[].load_based` | Load-sensitive behavior configuration |
| `endpoints[].load_based.healthy` | Behavior below the healthy RPS threshold |
| `endpoints[].load_based.tipping_point` | Behavior above the tipping point RPS |
| `endpoints[].load_based.grey_zone` | Transition parameters between healthy and tipping |

### Percentile Keys

Percentile keys use the format `p<value>` where value is between 0 and 100:

| Key | Quantile |
|-----|----------|
| `p0.0` | 0th percentile (minimum) |
| `p25.0` | 25th percentile |
| `p50.0` | 50th percentile (median) |
| `p75.0` | 75th percentile |
| `p90.0` | 90th percentile |
| `p95.0` | 95th percentile |
| `p99.0` | 99th percentile |
| `p99.9` | 99.9th percentile |
| `p99.99` | 99.99th percentile |
| `p100.0` | 100th percentile (maximum) |

Distribution values must be non-decreasing (a higher percentile cannot have a shorter duration).

### Grey Zone Configuration

| Field | Description |
|-------|-------------|
| `penalty_base` | Base latency penalty at full grey zone position (e.g., "50ms") |
| `spike_threshold` | Grey zone position (0-1) above which spike behavior activates |
| `spike_penalty_duration` | How long a spike penalty persists (e.g., "2s") |
| `spike_penalty_multiplier` | Multiplier applied to base penalty during spikes |
| `recovery_rate` | Rate at which spike penalty decays (0-1) |

### Envoy Configuration Example

The filter is configured as an **upstream HTTP filter** on the cluster, not on the listener.

Note: `filter_config` uses `google.protobuf.Struct` — Envoy parses the YAML natively and passes it as JSON to the module. No string-wrapping required.

```yaml
static_resources:
  listeners:
    - address:
        socket_address: { address: 0.0.0.0, port_value: 10000 }
      filter_chains:
        - filters:
            - name: envoy.filters.network.http_connection_manager
              typed_config:
                "@type": type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager
                stat_prefix: ingress_http
                route_config:
                  virtual_hosts:
                    - name: local_route
                      domains: ["*"]
                      routes:
                        - match: { prefix: "/" }
                          route: { cluster: simulated_backend }
                http_filters:
                  - name: envoy.filters.http.router
                    typed_config:
                      "@type": type.googleapis.com/envoy.extensions.filters.http.router.v3.Router
  clusters:
    - name: simulated_backend
      connect_timeout: 5s
      type: strict_dns
      load_assignment:
        cluster_name: simulated_backend
        endpoints:
          - lb_endpoints:
              - endpoint:
                  address:
                    socket_address: { address: 127.0.0.1, port_value: 8080 }
      typed_extension_protocol_options:
        envoy.extensions.upstreams.http.v3.HttpProtocolOptions:
          "@type": type.googleapis.com/envoy.extensions.upstreams.http.v3.HttpProtocolOptions
          explicit_http_config:
            http_protocol_options: {}
          upstream_http_filters:
            - name: dynamic_modules/latency_fault
              typed_config:
                "@type": type.googleapis.com/envoy.extensions.filters.http.dynamic_modules.v3.DynamicModuleFilter
                dynamic_module_config:
                  name: latency_fault_module
                  do_not_close: true
                filter_name: latency_fault
                filter_config:
                  "@type": type.googleapis.com/google.protobuf.Struct
                  endpoints:
                    - match:
                        prefix: "/api/v1/slow-endpoint"
                      responses:
                        - status: 200
                          resolution: 900
                          distribution:
                            p0.0: "5ms"
                            p50.0: "10ms"
                            p90.0: "50ms"
                            p99.0: "200ms"
                            p100.0: "1s"
                        - status: 503
                          resolution: 100
                          distribution:
                            p0.0: "100ms"
                            p100.0: "500ms"
            - name: envoy.filters.http.upstream_codec
              typed_config:
                "@type": type.googleapis.com/envoy.extensions.filters.http.upstream_codec.v3.UpstreamCodec
```

> **Note**: The `envoy.filters.http.upstream_codec` filter must always be the last filter in the upstream HTTP filter chain.

## How It Works

### Upstream Filter Architecture

Unlike a traditional downstream HTTP filter that injects delay *before* the request reaches the upstream, this filter operates in the **upstream position**:

1. **On request** (`OnRequestHeaders`): Samples from the distribution, records the start time, and lets the request proceed to the upstream unmodified.
2. **On response** (`OnResponseHeaders`): Measures how long the upstream actually took, calculates `remaining = target - elapsed`, and:
    - If `remaining > 0`: delays the response by that amount before forwarding to the client
    - If `remaining <= 0`: the upstream was already slow enough — no additional delay
    - If sampled status is 4xx/5xx: overrides the response with a local error response

This means the client observes a total latency that matches the configured distribution, regardless of how fast or slow the actual upstream is.

### Status Code Selection

Each endpoint has one or more response entries with a `resolution` that serves as both:
1. **Weight**: The probability of selecting that status code (proportional to total resolution)
2. **Accuracy**: The number of pre-computed latency samples for that status code's distribution

For example, with `resolution: 900` for status 200 and `resolution: 100` for status 503:
- 90% of requests will get a 200 response with latency from the 200 distribution
- 10% of requests will get a 503 abort with latency from the 503 distribution

### Latency Distribution

The stateful probability distribution is inspired by [distribution-calculator](https://github.com/spockz/distribution-calculator). Given a set of percentiles, it:

1. Pre-computes exactly `resolution` samples by interpolating between percentile boundaries
2. Shuffles and serves them in random order
3. Over a full cycle of `resolution` requests, the actual percentile distribution exactly matches the configured one

### Load-Based Behavior

When `load_based` is configured:
- Below `healthy.threshold_rps`: Uses the healthy response distribution
- Above `tipping_point.threshold_rps`: Uses the tipping point distribution
- Between the two (**grey zone**): Probabilistically mixes between healthy and tipping based on position, with optional penalty

### Grey Zone Transitions

In the grey zone, the filter:
1. Calculates position as `(currentRPS - healthyRPS) / (tippingRPS - healthyRPS)` (0.0 to 1.0)
2. Selects healthy or tipping distribution proportionally to position
3. Adds a base latency penalty scaled by position
4. If position exceeds `spike_threshold`, applies the spike multiplier for `spike_penalty_duration`
5. Decays the spike penalty at `recovery_rate` when position drops below threshold

## Response Headers

The filter adds response headers to indicate what was injected:

| Header | Description |
|--------|-------------|
| `x-fault-injected-delay` | Target duration from the distribution (e.g., "52.3ms") |
| `x-fault-actual-upstream` | Actual time the upstream took to respond |
| `x-fault-added-delay` | Additional delay injected (target - upstream, only if > 0) |
| `x-fault-injected` | Set to "abort" when a non-2xx status was injected |
| `x-fault-status` | The status code selected by the distribution |

## Development

### Prerequisites

- Go 1.23+
- Envoy v1.37.0 (or compatible version)

### Build

```bash
cd "built-on-envoy/extensions"
EXTENSION_PATH="composer/dynamic-fault-injection" make build-go
```

This produces `liblatency_fault_module.so` which can be loaded by Envoy.

### Unit Tests

```bash
EXTENSION_PATH="composer/dynamic-fault-injection" make test-go
```

### Integration Tests

```bash
cd "built-on-envoy/extensions/tests/e2e"
GO_TEST_ARGS="-run TestDistributionDelay" make test
```

This builds the module, starts an Envoy instance with the filter configured, and runs tests against it using the boe built-in test harness.

## Comparison with Envoy's Built-in Fault Filter

| Feature | Built-in Fault Filter | This Module |
|---------|----------------------|-------------|
| Fixed delay | ✅ | ✅ (flat distribution) |
| Percentile distributions | ❌ | ✅ |
| Per-status-code distributions | ❌ | ✅ |
| Load-based degradation | ❌ | ✅ |
| Grey zone transitions | ❌ | ✅ |
| HTTP abort | ✅ | ✅ |
| gRPC abort | ✅ | 🚧 (planned) |
| Header-controlled faults | ✅ | ❌ (route-based instead) |
| Response rate limiting | ✅ | ❌ |
| Per-route configuration | Via per-route config | Built-in route matching |
| Runtime configuration | ✅ | ❌ |
| Exact distribution over N requests | ❌ | ✅ (stateful distribution) |
