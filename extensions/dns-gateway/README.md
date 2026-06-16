# DNS Gateway

Transparent egress routing for Envoy. Applications make normal DNS lookups and TCP connections
— this extension intercepts both so Envoy can control which upstream cluster handles each
domain, attach metadata (auth tokens, policy IDs), and enforce routing policy, all without
any application changes.

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

Both filters are provided by the same `dns-gateway` dynamic module; enable each by its filter
type (`udp_listener` and `network`).

1. **UDP listener filter** — Intercepts DNS queries. If the queried domain matches
   a configured pattern, allocates a virtual IP from that domain's dedicated CIDR range and responds
   with an A record. Each domain pattern gets its own IP range, so `*.aws.com` and `*.google.com`
   allocate from separate subnets. Caches the mapping from virtual IP to domain and metadata.
   Non-matching queries pass through.

2. **Network filter** — On new TCP connections, looks up the destination virtual IP
   in the shared cache and sets the resolved domain and metadata as Envoy
   [filter state](https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/advanced/data_sharing_between_filters#primitives)
   for use in routing.

```
 Application
     |  DNS query: "bucket-1.aws.com"
     v
 UDP listener filter
     |  matches "*.aws.com", allocates 10.239.0.0 from *.aws.com's range, responds with A record
     v
 Application
     |  TCP connect to 10.239.0.0:443
     v
 network filter
     |  resolves 10.239.0.0 -> domain="bucket-1.aws.com", metadata.cluster="aws"
     v
 tcp_proxy
     |  routes to upstream cluster using filter state
     v
 External service (bucket-1.aws.com)
```

## Filter state

The network filter sets Envoy filter state keys readable via `%FILTER_STATE(<key>:PLAIN)%`.
The default prefix is `io.builtonenvoy.dns_gateway` and is configurable via `filter_state_prefix`.
See the [extension page](https://builtonenvoy.io/extensions/dns-gateway) for the full key reference.

## Domain matching

- **Exact**: `"example.com"` — matches only `example.com`
- **Wildcard**: `"*.aws.com"` — matches one subdomain level (e.g. `api.aws.com`) but not `aws.com` itself or nested subdomains like `sub.api.aws.com`

## Configuration

### UDP listener filter

| Field                   | Type    | Description                                                        |
| ----------------------- | ------- | ------------------------------------------------------------------ |
| `domains`               | array   | Domain matchers, each with its own CIDR range                      |
| `domains[].domain`      | string  | Exact (`"example.com"`) or wildcard (`"*.example.com"`) pattern    |
| `domains[].base_ip`     | string  | Base IPv4 address for virtual IP allocation (e.g. `"10.239.0.0"`) |
| `domains[].prefix_len`  | integer | CIDR prefix length (1-32). A `/24` gives 256 IPs.                 |
| `domains[].metadata`    | object  | String key-value pairs exposed via filter state                    |
| `fail_open`             | boolean | If `true`, forward queries upstream when a CIDR range is exhausted. Default: `false` (return NODATA) |

### Network filter

| Field                  | Type   | Description                                                                          |
| ---------------------- | ------ | ------------------------------------------------------------------------------------ |
| `filter_state_prefix`  | string | Prefix for filter state keys. Default: `io.builtonenvoy.dns_gateway`                |

## Building

```bash
cargo build --release -p dns-gateway
```

The compiled library will be at `target/release/libdns_gateway.{so,dylib}`.
