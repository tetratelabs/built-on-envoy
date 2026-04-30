// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

//! Virtual IP cache for mapping domains to synthetic IPv4 addresses.
//!
//! This module provides a thread-safe cache that allocates sequential virtual IPs from
//! per-domain CIDR subnets. The DNS gateway filter populates this cache, and the cache
//! lookup network filter reads from it.

use dashmap::mapref::entry::Entry;
use dashmap::DashMap;
use envoy_proxy_dynamic_modules_rust_sdk::*;
use ipnet::Ipv4Net;
use std::collections::HashMap;
use std::net::Ipv4Addr;
use std::sync::atomic::{AtomicU32, Ordering};
use std::sync::{Arc, OnceLock};

/// Thread-safe cache for virtual IP allocation and lookup.
///
/// Allocates sequential IPs from per-domain CIDR ranges.
/// Deduplicates allocations by domain name.
pub struct VirtualIpCache {
    // Maps between an allocated virtual IP and its associated domain and metadata.
    ip_to_dest: DashMap<Ipv4Addr, (String, HashMap<String, String>)>,

    // Maps between a domain and its allocated virtual IP. Used to prevent repeat allocations for the same domain.
    domain_to_ip: DashMap<String, Ipv4Addr>,

    // Tracks the next available offset for each CIDR range.
    // Virtual IPs are allocated incrementally, so this number monotonically increases until the range is exhausted.
    offsets: DashMap<Ipv4Net, AtomicU32>,
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
    /// Returns the same IP if the domain was previously allocated.
    /// Returns `None` if the range is exhausted.
    pub fn allocate(
        &self,
        domain: String,
        metadata: HashMap<String, String>,
        base_ip: u32,
        prefix_len: u8,
    ) -> Option<Ipv4Addr> {
        if let Some(ip) = self.domain_to_ip.get(&domain) {
            return Some(*ip);
        }

        match self.domain_to_ip.entry(domain.clone()) {
            Entry::Occupied(entry) => Some(*entry.get()),
            Entry::Vacant(entry) => {
                let capacity = 1u32 << (32 - prefix_len);
                let cidr = Ipv4Net::new(Ipv4Addr::from(base_ip), prefix_len).ok()?;
                let counter = self.offsets.entry(cidr).or_insert(AtomicU32::new(0));
                let offset = counter
                    .fetch_update(Ordering::Relaxed, Ordering::Relaxed, |n| {
                        (n < capacity).then_some(n + 1)
                    })
                    .map_err(|n| {
                        envoy_log_error!(
                            "IP allocation exhausted for {cidr}, allocated {n} of {capacity}"
                        );
                    })
                    .ok()?;

                let ip = Ipv4Addr::from(base_ip + offset);

                envoy_log_info!(
                    "Allocated virtual IP {} for domain {} (range {})",
                    ip,
                    domain,
                    Ipv4Addr::from(base_ip)
                );

                self.ip_to_dest.insert(ip, (domain, metadata));
                entry.insert(ip);

                Some(ip)
            }
        }
    }

    pub fn lookup(&self, ip: Ipv4Addr) -> Option<(String, HashMap<String, String>)> {
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

    const BASE_10_10: u32 = 0x0A0A_0000; // 10.10.0.0
    const BASE_239_0: u32 = 0x0AEF_0000; // 10.239.0.0
    const BASE_239_1: u32 = 0x0AEF_0100; // 10.239.1.0

    #[test]
    fn test_single_range_sequential_allocation() {
        let cache = VirtualIpCache::new();

        let ip1 = cache
            .allocate("api.aws.com".into(), HashMap::new(), BASE_10_10, 24)
            .unwrap();
        let ip2 = cache
            .allocate("s3.aws.com".into(), HashMap::new(), BASE_10_10, 24)
            .unwrap();

        assert_eq!(ip1, Ipv4Addr::new(10, 10, 0, 0));
        assert_eq!(ip2, Ipv4Addr::new(10, 10, 0, 1));
    }

    #[test]
    fn test_multiple_ranges_allocate_independently() {
        let cache = VirtualIpCache::new();

        let ip_a = cache
            .allocate("a.amazon.com".into(), HashMap::new(), BASE_239_0, 24)
            .unwrap();
        let ip_b = cache
            .allocate("a.amazonaws.com".into(), HashMap::new(), BASE_239_1, 24)
            .unwrap();

        assert_eq!(ip_a, Ipv4Addr::new(10, 239, 0, 0));
        assert_eq!(ip_b, Ipv4Addr::new(10, 239, 1, 0));
    }

    #[test]
    fn test_same_domain_returns_same_ip() {
        let cache = VirtualIpCache::new();

        let ip1 = cache
            .allocate("shared.example.com".into(), HashMap::new(), BASE_239_0, 24)
            .unwrap();
        let ip2 = cache
            .allocate("shared.example.com".into(), HashMap::new(), BASE_239_0, 24)
            .unwrap();

        assert_eq!(ip1, ip2);
    }

    #[test]
    fn test_lookup() {
        let cache = VirtualIpCache::new();

        let mut meta = HashMap::new();
        meta.insert("cluster".to_string(), "aws_cluster".to_string());

        let ip = cache
            .allocate("api.aws.com".into(), meta, BASE_239_0, 24)
            .unwrap();
        let (domain, metadata) = cache.lookup(ip).unwrap();
        assert_eq!(domain, "api.aws.com");
        assert_eq!(metadata.get("cluster").unwrap(), "aws_cluster");

        assert!(cache.lookup(Ipv4Addr::new(10, 10, 0, 100)).is_none());
    }

    #[test]
    fn test_range_exhaustion() {
        let cache = VirtualIpCache::new();

        for i in 0..4 {
            assert!(cache
                .allocate(format!("d{}.com", i), HashMap::new(), BASE_10_10, 30)
                .is_some());
        }
        assert!(cache
            .allocate("overflow.com".into(), HashMap::new(), BASE_10_10, 30)
            .is_none());
    }

    #[test]
    fn test_metadata_preserved() {
        let cache = VirtualIpCache::new();

        let mut metadata = HashMap::new();
        metadata.insert("key1".to_string(), "value1".to_string());
        metadata.insert("key2".to_string(), "value2".to_string());

        let ip = cache
            .allocate("test.com".into(), metadata, BASE_10_10, 24)
            .unwrap();
        let (_, metadata) = cache.lookup(ip).unwrap();

        assert_eq!(metadata.len(), 2);
        assert_eq!(metadata.get("key1").unwrap(), "value1");
        assert_eq!(metadata.get("key2").unwrap(), "value2");
    }
}
