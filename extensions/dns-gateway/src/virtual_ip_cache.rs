// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

//! Virtual IP cache for mapping domains to synthetic IP addresses.
//!
//! This module provides a thread-safe cache that allocates sequential virtual IPs from
//! per-domain CIDR subnets. Both IPv4 (A) and IPv6 (AAAA) address families are supported:
//! allocation arithmetic is performed on a `u128` offset so a single code path covers both.
//! The DNS gateway filter populates this cache, and the cache lookup network filter reads
//! from it.

use dashmap::mapref::entry::Entry;
use dashmap::DashMap;
use envoy_proxy_dynamic_modules_rust_sdk::*;
use ipnet::IpNet;
use std::collections::HashMap;
use std::net::{IpAddr, Ipv4Addr, Ipv6Addr};
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::{Arc, OnceLock};

/// Adds `offset` to an `IpAddr`, preserving the address family.
///
/// IPv4 arithmetic is done in `u32` space and IPv6 in `u128` space. Returns `None` on
/// overflow (which a caller's capacity check normally prevents).
fn ip_add(base: IpAddr, offset: u128) -> Option<IpAddr> {
    match base {
        IpAddr::V4(v4) => {
            let n = u32::from(v4) as u128 + offset;
            let n: u32 = n.try_into().ok()?;
            Some(IpAddr::V4(Ipv4Addr::from(n)))
        }
        IpAddr::V6(v6) => {
            let n = u128::from(v6).checked_add(offset)?;
            Some(IpAddr::V6(Ipv6Addr::from(n)))
        }
    }
}

/// Total number of addresses in a CIDR of the given prefix length for `base`'s family.
///
/// Saturates at `u128::MAX` for an IPv6 /0 (whose true count, 2^128, does not fit in u128);
/// the per-family bit width is used so IPv4 capacity never exceeds 2^32.
fn cidr_capacity(base: &IpAddr, prefix_len: u8) -> u128 {
    let bits = match base {
        IpAddr::V4(_) => 32u32,
        IpAddr::V6(_) => 128u32,
    };
    let host_bits = bits - prefix_len as u32;
    // 1 << 128 overflows u128; only an IPv6 /0 hits that, so saturate.
    1u128.checked_shl(host_bits).unwrap_or(u128::MAX)
}

/// Thread-safe cache for virtual IP allocation and lookup.
///
/// Allocates sequential IPs from per-domain CIDR ranges. Works for both IPv4 and IPv6.
/// Deduplicates allocations by `(domain, address family)`, so a single domain can hold both
/// an IPv4 (A) and an IPv6 (AAAA) virtual IP simultaneously (dual-stack).
pub struct VirtualIpCache {
    // Maps between an allocated virtual IP and its associated domain and metadata.
    ip_to_dest: DashMap<IpAddr, (String, HashMap<String, String>)>,

    // Maps between a (domain, is_ipv6) pair and its allocated virtual IP. Used to prevent repeat
    // allocations for the same domain within an address family, while still allowing the same
    // domain to hold one IPv4 and one IPv6 virtual IP (dual-stack).
    domain_to_ip: DashMap<(String, bool), IpAddr>,

    // Tracks the next available offset for each CIDR range.
    // Virtual IPs are allocated incrementally, so this number monotonically increases until the range is exhausted.
    offsets: DashMap<IpNet, AtomicU64>,
}

impl VirtualIpCache {
    fn new() -> Self {
        Self {
            ip_to_dest: DashMap::new(),
            domain_to_ip: DashMap::new(),
            offsets: DashMap::new(),
        }
    }

    /// Allocates a virtual IP for the given destination within the specified CIDR range.
    ///
    /// The address family of `base_ip` selects the family of the allocated virtual IP.
    /// Returns the same IP if the domain was previously allocated *in this address family*
    /// (so a domain can hold one IPv4 and one IPv6 virtual IP).
    /// Returns `None` if the range is exhausted.
    pub fn allocate(
        &self,
        domain: String,
        metadata: HashMap<String, String>,
        base_ip: IpAddr,
        prefix_len: u8,
    ) -> Option<IpAddr> {
        // Dedup per (domain, address family): the same FQDN may hold both an A and an AAAA
        // virtual IP, but repeat queries within one family reuse the existing allocation.
        let key = (domain.clone(), base_ip.is_ipv6());
        if let Some(ip) = self.domain_to_ip.get(&key) {
            return Some(*ip);
        }

        match self.domain_to_ip.entry(key) {
            Entry::Occupied(entry) => Some(*entry.get()),
            Entry::Vacant(entry) => {
                let capacity = cidr_capacity(&base_ip, prefix_len);
                let cidr = IpNet::new(base_ip, prefix_len).ok()?;
                let counter = self.offsets.entry(cidr).or_insert(AtomicU64::new(0));
                let offset = counter
                    .fetch_update(Ordering::Relaxed, Ordering::Relaxed, |n| {
                        ((n as u128) < capacity).then_some(n + 1)
                    })
                    .map_err(|n| {
                        envoy_log_error!(
                            "IP allocation exhausted for {cidr}, allocated {n} of {capacity}"
                        );
                    })
                    .ok()?;

                let ip = ip_add(base_ip, offset as u128)?;

                envoy_log_info!(
                    "Allocated virtual IP {} for domain {} (range {})",
                    ip,
                    domain,
                    base_ip
                );

                self.ip_to_dest.insert(ip, (domain, metadata));
                entry.insert(ip);

                Some(ip)
            }
        }
    }

    pub fn lookup(&self, ip: IpAddr) -> Option<(String, HashMap<String, String>)> {
        self.ip_to_dest.get(&ip).map(|r| r.value().clone())
    }
}

static VIRTUAL_IP_CACHE: OnceLock<Arc<VirtualIpCache>> = OnceLock::new();

pub fn get_cache() -> &'static Arc<VirtualIpCache> {
    VIRTUAL_IP_CACHE.get_or_init(|| Arc::new(VirtualIpCache::new()))
}

#[cfg(test)]
mod tests {
    use super::*;

    fn v4(s: &str) -> IpAddr {
        s.parse::<Ipv4Addr>().unwrap().into()
    }

    fn v6(s: &str) -> IpAddr {
        s.parse::<Ipv6Addr>().unwrap().into()
    }

    #[test]
    fn test_single_range_sequential_allocation() {
        let cache = VirtualIpCache::new();

        let ip1 = cache
            .allocate("api.aws.com".into(), HashMap::new(), v4("10.10.0.0"), 24)
            .unwrap();
        let ip2 = cache
            .allocate("s3.aws.com".into(), HashMap::new(), v4("10.10.0.0"), 24)
            .unwrap();

        assert_eq!(ip1, v4("10.10.0.0"));
        assert_eq!(ip2, v4("10.10.0.1"));
    }

    #[test]
    fn test_multiple_ranges_allocate_independently() {
        let cache = VirtualIpCache::new();

        let ip_a = cache
            .allocate("a.amazon.com".into(), HashMap::new(), v4("10.0.0.0"), 24)
            .unwrap();
        let ip_b = cache
            .allocate("a.amazonaws.com".into(), HashMap::new(), v4("10.0.1.0"), 24)
            .unwrap();

        assert_eq!(ip_a, v4("10.0.0.0"));
        assert_eq!(ip_b, v4("10.0.1.0"));
    }

    #[test]
    fn test_same_domain_returns_same_ip() {
        let cache = VirtualIpCache::new();

        let ip1 = cache
            .allocate(
                "shared.example.com".into(),
                HashMap::new(),
                v4("10.0.0.0"),
                24,
            )
            .unwrap();
        let ip2 = cache
            .allocate(
                "shared.example.com".into(),
                HashMap::new(),
                v4("10.0.0.0"),
                24,
            )
            .unwrap();

        assert_eq!(ip1, ip2);
    }

    #[test]
    fn test_lookup() {
        let cache = VirtualIpCache::new();

        let mut meta = HashMap::new();
        meta.insert("cluster".to_string(), "aws_cluster".to_string());

        let ip = cache
            .allocate("api.aws.com".into(), meta, v4("10.0.0.0"), 24)
            .unwrap();
        let (domain, metadata) = cache.lookup(ip).unwrap();
        assert_eq!(domain, "api.aws.com");
        assert_eq!(metadata.get("cluster").unwrap(), "aws_cluster");

        assert!(cache.lookup(v4("10.10.0.100")).is_none());
    }

    #[test]
    fn test_range_exhaustion() {
        let cache = VirtualIpCache::new();

        for i in 0..4 {
            assert!(cache
                .allocate(format!("d{}.com", i), HashMap::new(), v4("10.10.0.0"), 30)
                .is_some());
        }
        assert!(cache
            .allocate("overflow.com".into(), HashMap::new(), v4("10.10.0.0"), 30)
            .is_none());
    }

    #[test]
    fn test_metadata_preserved() {
        let cache = VirtualIpCache::new();

        let mut metadata = HashMap::new();
        metadata.insert("key1".to_string(), "value1".to_string());
        metadata.insert("key2".to_string(), "value2".to_string());

        let ip = cache
            .allocate("test.com".into(), metadata, v4("10.10.0.0"), 24)
            .unwrap();
        let (_, metadata) = cache.lookup(ip).unwrap();

        assert_eq!(metadata.len(), 2);
        assert_eq!(metadata.get("key1").unwrap(), "value1");
        assert_eq!(metadata.get("key2").unwrap(), "value2");
    }

    #[test]
    fn test_v6_sequential_allocation() {
        let cache = VirtualIpCache::new();

        let ip1 = cache
            .allocate("a.example.com".into(), HashMap::new(), v6("fd00::"), 64)
            .unwrap();
        let ip2 = cache
            .allocate("b.example.com".into(), HashMap::new(), v6("fd00::"), 64)
            .unwrap();

        assert!(ip1.is_ipv6());
        assert_eq!(ip1, v6("fd00::"));
        assert_eq!(ip2, v6("fd00::1"));
    }

    #[test]
    fn test_v6_lookup_and_family_isolation() {
        let cache = VirtualIpCache::new();

        let v6ip = cache
            .allocate(
                "v6.example.com".into(),
                HashMap::new(),
                v6("fd00:cafe::"),
                64,
            )
            .unwrap();
        let v4ip = cache
            .allocate("v4.example.com".into(), HashMap::new(), v4("10.0.0.0"), 24)
            .unwrap();

        assert!(v6ip.is_ipv6());
        assert!(v4ip.is_ipv4());
        assert_eq!(cache.lookup(v6ip).unwrap().0, "v6.example.com");
        assert_eq!(cache.lookup(v4ip).unwrap().0, "v4.example.com");
    }

    #[test]
    fn test_dual_stack_same_domain_holds_both_families() {
        let cache = VirtualIpCache::new();

        // The same FQDN allocates independently in each address family.
        let v4ip = cache
            .allocate(
                "dual.example.com".into(),
                HashMap::new(),
                v4("10.0.0.0"),
                24,
            )
            .unwrap();
        let v6ip = cache
            .allocate("dual.example.com".into(), HashMap::new(), v6("fd00::"), 64)
            .unwrap();

        assert!(v4ip.is_ipv4());
        assert!(v6ip.is_ipv6());
        assert_ne!(v4ip, v6ip);

        // Both virtual IPs reverse-map to the same domain.
        assert_eq!(cache.lookup(v4ip).unwrap().0, "dual.example.com");
        assert_eq!(cache.lookup(v6ip).unwrap().0, "dual.example.com");

        // Repeat queries within a family are deduped to the existing IP (not a new allocation).
        assert_eq!(
            cache
                .allocate(
                    "dual.example.com".into(),
                    HashMap::new(),
                    v4("10.0.0.0"),
                    24
                )
                .unwrap(),
            v4ip
        );
        assert_eq!(
            cache
                .allocate("dual.example.com".into(), HashMap::new(), v6("fd00::"), 64)
                .unwrap(),
            v6ip
        );
    }
}
