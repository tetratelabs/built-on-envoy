# DNS Gateway

Transparent egress routing for Envoy. Applications make normal DNS lookups and TCP connections
— this extension set intercepts both so Envoy can control which upstream cluster handles each
domain, attach metadata (auth tokens, policy IDs), and enforce routing policy, all without
any application changes.

This is an **extension set**: the vendor manifest at the repo root
([`manifest.yaml`](manifest.yaml)) declares the shared `dns-gateway` dynamic module, and two
child extensions under [`manifests/`](manifests) expose its two filters independently:

| Extension | Filter type | Role |
| --------- | ----------- | ---- |
| [`resolver`](manifests/resolver) | `udp_listener` | Intercept DNS queries, allocate virtual IPs |
| [`lookup`](manifests/lookup) | `network` | Resolve virtual IPs back to domain + metadata as filter state |

Both filters are compiled into the same `libdns_gateway.so` and share a process-wide virtual-IP
cache, so they must run in the same Envoy instance.

> **Backward compatibility:** the legacy combined `dns-gateway` extension still works —
> `--extension dns-gateway --filter-type udp_listener` and `--extension dns-gateway --filter-type network`
> resolve to the same two filters. Both filter names are registered by the dynamic module.

**Use cases:**
- Route outbound traffic through different upstream proxies based on destination domain
- Attach per-domain credentials or policy metadata to proxied connections
- Enforce egress access control by selectively intercepting DNS for specific domains

![DNS Gateway Flow](diagram.png)

## Prerequisites

Requires iptables/nftables rules to redirect application traffic to Envoy:

- **DNS**: UDP port 53 redirected to Envoy's DNS listener (e.g. port 15053)
- **TCP**: Outbound connections to virtual IP ranges redirected to Envoy's TCP listener (e.g. port 15001)

## How it works

Both filters are provided by the same `dns-gateway` dynamic module; enable each via its own
extension (`resolver` for the `udp_listener` filter, `lookup` for the
`network` filter), referenced as `dns-gateway/resolver` and `dns-gateway/lookup`.

1. **`resolver` (UDP listener filter)** — Intercepts DNS queries. If the queried domain matches
   a configured pattern, allocates a virtual IP from that domain's dedicated CIDR range and responds
   with an A record (for an IPv4 `base_ip`) or an AAAA record (for an IPv6 `base_ip`). The address
   family of the domain's `base_ip` determines which query type it answers; the other type returns
   NODATA. Each domain pattern gets its own IP range, so `*.aws.com` and `*.google.com`
   allocate from separate subnets. Caches the mapping from virtual IP to domain and metadata.
   Non-matching queries pass through.

2. **`lookup` (Network filter)** — On new TCP connections, looks up the destination virtual IP
   in the shared cache and sets the resolved domain and metadata as Envoy
   [filter state](https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/advanced/data_sharing_between_filters#primitives)
   for use in routing.

```
 Application
     |  DNS query: "bucket-1.aws.com"
     v
 resolver (UDP listener filter)
     |  matches "*.aws.com", allocates 10.0.0.0 from *.aws.com's range, responds with A record
     v
 Application
     |  TCP connect to 10.0.0.0:443
     v
 lookup (network filter)
     |  resolves 10.0.0.0 -> domain="bucket-1.aws.com", metadata.cluster="aws"
     v
 tcp_proxy
     |  routes to upstream cluster using filter state
     v
 External service (bucket-1.aws.com)
```

## Filter state

The `lookup` network filter sets Envoy filter state keys readable via `%FILTER_STATE(<key>:PLAIN)%`.
The default prefix is `io.builtonenvoy.dns_gateway` and is configurable via `filter_state_prefix`.
See the [extension page](https://builtonenvoy.io/extensions/lookup) for the full key reference.

## Domain matching

- **Exact**: `"example.com"` — matches only `example.com`
- **Wildcard**: `"*.aws.com"` — matches one subdomain level (e.g. `api.aws.com`) but not `aws.com` itself or nested subdomains like `sub.api.aws.com`

## Configuration

### `resolver` (UDP listener filter)

| Field                   | Type    | Description                                                        |
| ----------------------- | ------- | ------------------------------------------------------------------ |
| `domains`               | array   | Domain matchers, each with its own CIDR range                      |
| `domains[].domain`      | string  | Exact (`"example.com"`) or wildcard (`"*.example.com"`) pattern    |
| `domains[].base_ip`     | string  | Base address for virtual IP allocation. IPv4 (e.g. `"10.0.0.0"`, answered as an A record) or IPv6 (e.g. `"fd00::"`, answered as an AAAA record). |
| `domains[].prefix_len`  | integer | CIDR prefix length. IPv4: 1-32 (a `/24` gives 256 IPs). IPv6: 1-128 (a `/64` gives 2^64 IPs). |
| `domains[].metadata`    | object  | String key-value pairs exposed via filter state                    |
| `fail_open`             | boolean | If `true`, forward queries upstream when a CIDR range is exhausted. Default: `false` (return NODATA) |

### `lookup` (network filter)

| Field                  | Type   | Description                                                                          |
| ---------------------- | ------ | ------------------------------------------------------------------------------------ |
| `filter_state_prefix`  | string | Prefix for filter state keys. Default: `io.builtonenvoy.dns_gateway`                |

## Building

```bash
cargo build --release -p dns-gateway
```

The compiled library will be at `target/release/libdns_gateway.{so,dylib}`.
