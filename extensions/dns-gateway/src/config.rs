// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

use serde::Deserialize;
use std::collections::HashMap;
use std::net::IpAddr;

#[derive(Debug, Deserialize)]
pub struct DnsGateway {
    #[serde(default)]
    pub domains: Vec<DomainMatcher>,
    #[serde(default)]
    pub fail_open: bool,
}

#[derive(Debug, Deserialize)]
pub struct DomainMatcher {
    pub domain: String,

    #[serde(default)]
    pub metadata: HashMap<String, String>,

    /// Base address for virtual IP allocation. May be an IPv4 address
    /// (e.g. "10.0.0.0", answered as an A record) or an IPv6 address
    /// (e.g. "fd00::", answered as an AAAA record).
    pub base_ip: String,

    #[serde(default)]
    pub prefix_len: u32,
}

impl DomainMatcher {
    /// Parses `base_ip` into an [`IpAddr`], accepting both IPv4 and IPv6.
    pub fn parse_base_ip(&self) -> Result<IpAddr, std::net::AddrParseError> {
        self.base_ip.parse::<IpAddr>()
    }

    /// Returns the maximum valid prefix length for this matcher's address family:
    /// 32 for IPv4, 128 for IPv6.
    pub fn max_prefix_len(addr: &IpAddr) -> u32 {
        match addr {
            IpAddr::V4(_) => 32,
            IpAddr::V6(_) => 128,
        }
    }
}
