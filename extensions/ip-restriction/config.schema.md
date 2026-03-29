# IP Restriction Configuration Schema

IP-based access control using allowlists or denylists. Exactly one of `allow_addresses` or `deny_addresses` must be provided.

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `allow_addresses` | array of strings | One of the two | - | IP addresses or CIDR ranges to allow. Only matching requests are permitted. |
| `deny_addresses` | array of strings | One of the two | - | IP addresses or CIDR ranges to deny. Matching requests receive 403 Forbidden. |

Each entry must be a valid IPv4/IPv6 address (e.g. `192.168.1.1`, `::1`) or CIDR range (e.g. `10.0.0.0/8`, `2001:db8::/32`). Duplicate entries within a list are ignored.
