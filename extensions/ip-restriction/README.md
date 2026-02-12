# IP Restriction Dynamic Module

A Rust-based Envoy dynamic module that provides IP-based access control through allowlists or denylists.

## Overview

This extension demonstrates how to build Envoy extensions in Rust using the dynamic module framework. It provides simple yet effective IP-based access control by examining the source IP address of incoming requests.

## Features

- **IP Allowlisting**: Only permit requests from specific IP addresses
- **IP Denylisting**: Block requests from specific IP addresses  
- **IPv4 and IPv6 Support**: Handles both IPv4 and IPv6 addresses
- **High Performance**: Compiled as native code loaded dynamically by Envoy

## Configuration

The filter accepts a JSON configuration with exactly one of the following fields:

### Allow List Mode

```json
{
  "allow_addresses": [
    "127.0.0.1",
    "::1",
    "192.168.1.100"
  ]
}
```

In this mode, only requests from the specified IP addresses are allowed. All other requests receive a 403 Forbidden response.

### Deny List Mode

```json
{
  "deny_addresses": [
    "192.168.1.50",
    "10.0.0.100"
  ]
}
```

In this mode, requests from the specified IP addresses are blocked with a 403 Forbidden response. All other requests are allowed.

## Envoy Configuration

Here's a complete example of using this filter in Envoy:

```yaml
http_filters:
  - name: ip_restriction
    typed_config:
      "@type": type.googleapis.com/envoy.extensions.filters.http.dynamic_modules.v3.DynamicModuleFilter
      dynamic_module_config:
        name: ip_restriction
      filter_name: ip_restriction
      filter_config:
        "@type": "type.googleapis.com/google.protobuf.StringValue"
        value: |
          {
            "deny_addresses": [
              "192.168.22.33"
            ]
          }
  - name: envoy.filters.http.router
    typed_config:
      "@type": type.googleapis.com/envoy.extensions.filters.http.router.v3.Router
```

## Building

See [BUILDING.md](./BUILDING.md) for detailed build instructions.

**Quick start:**

```bash
cd extensions/ip-restriction
cargo build --release
cargo test
```

The compiled library will be at:
- Linux: `target/release/libip_restriction.so`
- macOS: `target/release/libip_restriction.dylib`

## Requirements

- **Rust**: 1.70+ for building  
- **Envoy**: 1.30.0+ with dynamic module support
- **Platform**: Linux or macOS

This is a **standalone Cargo project** - no workspace required!

## Implementation Notes

- Uses `Arc` for efficient filter configuration sharing
- IP validation at configuration time
- Comprehensive error handling
- Port numbers automatically stripped
- Includes unit tests with mocks

## Future Enhancements

- IP range/CIDR support (e.g., `192.168.1.0/24`)
- Rate limiting per IP
- Geographic IP filtering
- Dynamic IP list updates

## License

Apache 2.0 - See [LICENSE](../../LICENSE)

## Contributing

See [CONTRIBUTING.md](../../CONTRIBUTING.md)
