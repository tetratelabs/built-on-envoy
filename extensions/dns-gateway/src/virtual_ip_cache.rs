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
//!
//! # Hardening (fail-closed)
//!
//! Because untrusted DNS queries drive allocation, the cache is bounded and defensive:
//!
//! * **Max-entries cap** — once `max_entries` virtual IPs are live, new domains are refused
//!   (`allocate` returns `None`, so the gateway mints nothing and the caller fails closed via
//!   its `fail_open=false` path). This prevents unbounded memory growth under a flood of unique
//!   FQDNs.
//! * **Idle TTL eviction** — entries unused for `idle_ttl` are reclaimed, both opportunistically
//!   on allocation and on demand via [`VirtualIpCache::evict_idle`]. This frees space for live
//!   traffic without ever handing a still-in-use IP to a different domain.
//! * **Per-allocation rate limit** — at most `max_allocs_per_window` *new* allocations are minted
//!   per `rate_window`; excess requests are refused (fail-closed). Cache hits for already-known
//!   domains are never rate limited.
//! * **Flush hook** — [`VirtualIpCache::flush`] clears every mapping and resets the allocation
//!   counters, so a recycled virtual IP can never resolve to a stale FQDN from a previous
//!   configuration generation.

use dashmap::mapref::entry::Entry;
use dashmap::DashMap;
use envoy_proxy_dynamic_modules_rust_sdk::*;
use ipnet::IpNet;
use std::collections::HashMap;
use std::net::{IpAddr, Ipv4Addr, Ipv6Addr};
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::{Arc, OnceLock};
use std::time::{Duration, Instant};

/// Default ceiling on the number of live virtual-IP mappings. Sized to comfortably cover a busy
/// gateway's working set while bounding worst-case memory under a flood of unique FQDNs.
pub const DEFAULT_MAX_ENTRIES: usize = 65_536;

/// Default idle window after which an unused mapping becomes eligible for eviction.
pub const DEFAULT_IDLE_TTL: Duration = Duration::from_secs(3600);

/// Default maximum number of *new* allocations permitted within [`DEFAULT_RATE_WINDOW`].
pub const DEFAULT_MAX_ALLOCS_PER_WINDOW: u64 = 1_000;

/// Default fixed window for the per-allocation rate limit.
pub const DEFAULT_RATE_WINDOW: Duration = Duration::from_secs(1);

/// Tunable safety limits for [`VirtualIpCache`].
#[derive(Clone, Copy, Debug)]
pub struct CacheLimits {
    /// Maximum number of live virtual-IP mappings. Allocation fails closed once reached
    /// (after attempting idle eviction).
    pub max_entries: usize,
    /// Idle duration after which an unused mapping is eligible for eviction. `Duration::ZERO`
    /// disables idle eviction.
    pub idle_ttl: Duration,
    /// Maximum number of new allocations permitted per [`Self::rate_window`].
    pub max_allocs_per_window: u64,
    /// Length of the fixed window used by the allocation rate limiter.
    pub rate_window: Duration,
}

impl Default for CacheLimits {
    fn default() -> Self {
        Self {
            max_entries: DEFAULT_MAX_ENTRIES,
            idle_ttl: DEFAULT_IDLE_TTL,
            max_allocs_per_window: DEFAULT_MAX_ALLOCS_PER_WINDOW,
            rate_window: DEFAULT_RATE_WINDOW,
        }
    }
}

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

/// Value stored for each allocated virtual IP.
struct CacheEntry {
    domain: String,
    metadata: HashMap<String, String>,
    /// Monotonic-clock millisecond timestamp of the last allocate/lookup touch, used for idle
    /// TTL eviction.
    last_used_ms: AtomicU64,
}

/// Thread-safe cache for virtual IP allocation and lookup.
///
/// Allocates sequential IPs from per-domain CIDR ranges. Works for both IPv4 and IPv6.
/// Deduplicates allocations by domain name. Bounded and rate limited; see the module docs.
pub struct VirtualIpCache {
    // Maps between an allocated virtual IP and its associated domain and metadata.
    ip_to_dest: DashMap<IpAddr, CacheEntry>,

    // Maps between a domain and its allocated virtual IP. Used to prevent repeat allocations for the same domain.
    domain_to_ip: DashMap<String, IpAddr>,

    // Tracks the next available offset for each CIDR range.
    // Virtual IPs are allocated incrementally, so this number monotonically increases until the range is exhausted.
    offsets: DashMap<IpNet, AtomicU64>,

    // Safety limits.
    limits: CacheLimits,

    // Allocation rate-limiter state: start-of-window timestamp (monotonic ms) and the count of
    // new allocations minted in the current window.
    rate_window_start_ms: AtomicU64,
    rate_window_count: AtomicU64,
}

/// Process-wide monotonic clock base. `now_ms()` returns milliseconds since this instant.
fn clock_base() -> Instant {
    static BASE: OnceLock<Instant> = OnceLock::new();
    *BASE.get_or_init(Instant::now)
}

/// Current monotonic time in milliseconds since [`clock_base`].
fn now_ms() -> u64 {
    clock_base().elapsed().as_millis() as u64
}

impl VirtualIpCache {
    /// Creates a cache with the default safety limits. Used by tests and as the basis for the
    /// process-wide singleton.
    fn new() -> Self {
        Self::with_limits(CacheLimits::default())
    }

    /// Creates a cache with explicit safety limits.
    pub fn with_limits(limits: CacheLimits) -> Self {
        Self {
            ip_to_dest: DashMap::new(),
            domain_to_ip: DashMap::new(),
            offsets: DashMap::new(),
            limits,
            rate_window_start_ms: AtomicU64::new(0),
            rate_window_count: AtomicU64::new(0),
        }
    }

    // `len`/`is_empty`/`evict_idle`/`flush` are public introspection and operational
    // hooks for embedders (e.g. a periodic eviction sweep or a config-reload flush). The
    // built-in filters don't call them, so `#[allow(dead_code)]` keeps this cdylib build
    // warning-free without hiding them from library consumers.

    /// Number of live virtual-IP mappings.
    #[allow(dead_code)]
    pub fn len(&self) -> usize {
        self.ip_to_dest.len()
    }

    /// Whether the cache holds no mappings.
    #[allow(dead_code)]
    pub fn is_empty(&self) -> bool {
        self.ip_to_dest.is_empty()
    }

    /// Allocates a virtual IP for the given destination within the specified CIDR range.
    ///
    /// `base_ip` may be IPv4 or IPv6; the allocated address inherits the same family.
    /// Returns the same IP if the domain was previously allocated (and refreshes its idle timer).
    /// Returns `None` — minting nothing, so the caller fails closed — if the range is exhausted,
    /// the entry cap is reached, or the allocation rate limit is exceeded.
    pub fn allocate(
        &self,
        domain: String,
        metadata: HashMap<String, String>,
        base_ip: IpAddr,
        prefix_len: u8,
    ) -> Option<IpAddr> {
        self.allocate_at(domain, metadata, base_ip, prefix_len, now_ms())
    }

    /// [`Self::allocate`] with an explicit monotonic timestamp, for deterministic tests.
    fn allocate_at(
        &self,
        domain: String,
        metadata: HashMap<String, String>,
        base_ip: IpAddr,
        prefix_len: u8,
        now: u64,
    ) -> Option<IpAddr> {
        // Fast path: known domain. Refresh its idle timer and return the existing IP. Known
        // domains are never rate limited or cap-rejected — they consume no new resources.
        if let Some(ip) = self.domain_to_ip.get(&domain) {
            if let Some(e) = self.ip_to_dest.get(&*ip) {
                e.last_used_ms.store(now, Ordering::Relaxed);
            }
            return Some(*ip);
        }

        // New allocation. Enforce the per-window rate limit first (cheapest rejection).
        if !self.try_consume_rate_token(now) {
            envoy_log_warn!(
                "Virtual IP allocation rate limit exceeded ({} per {:?}); refusing {}",
                self.limits.max_allocs_per_window,
                self.limits.rate_window,
                domain
            );
            return None;
        }

        // Enforce the entry cap. If full, try to reclaim idle space before failing closed.
        if self.ip_to_dest.len() >= self.limits.max_entries {
            self.evict_idle_at(now);
            if self.ip_to_dest.len() >= self.limits.max_entries {
                envoy_log_error!(
                    "Virtual IP cache full ({} entries); refusing allocation for {}",
                    self.limits.max_entries,
                    domain
                );
                return None;
            }
        }

        match self.domain_to_ip.entry(domain.clone()) {
            Entry::Occupied(entry) => {
                // Raced with another allocation for the same domain; reuse it.
                let ip = *entry.get();
                if let Some(e) = self.ip_to_dest.get(&ip) {
                    e.last_used_ms.store(now, Ordering::Relaxed);
                }
                Some(ip)
            }
            Entry::Vacant(entry) => {
                // Allocation offsets are tracked in a u64 counter, which comfortably covers
                // any practical IPv6 subnet (and all of IPv4). Cap capacity at u64::MAX so the
                // counter type never overflows even for very large prefixes.
                let capacity = cidr_capacity(&base_ip, prefix_len).min(u64::MAX as u128) as u64;
                let cidr = IpNet::new(base_ip, prefix_len).ok()?;
                let counter = self.offsets.entry(cidr).or_insert(AtomicU64::new(0));
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

                let ip = ip_add(base_ip, offset as u128)?;

                envoy_log_info!(
                    "Allocated virtual IP {} for domain {} (range {})",
                    ip,
                    domain,
                    base_ip
                );

                self.ip_to_dest.insert(
                    ip,
                    CacheEntry {
                        domain,
                        metadata,
                        last_used_ms: AtomicU64::new(now),
                    },
                );
                entry.insert(ip);

                Some(ip)
            }
        }
    }

    /// Looks up the destination domain and metadata for an allocated virtual IP, refreshing its
    /// idle timer on a hit.
    pub fn lookup(&self, ip: IpAddr) -> Option<(String, HashMap<String, String>)> {
        self.ip_to_dest.get(&ip).map(|e| {
            e.last_used_ms.store(now_ms(), Ordering::Relaxed);
            (e.domain.clone(), e.metadata.clone())
        })
    }

    /// Fixed-window allocation rate limiter. Returns `true` if a new-allocation token was
    /// available (and consumes it), `false` if the current window is exhausted.
    fn try_consume_rate_token(&self, now: u64) -> bool {
        let window_ms = self.limits.rate_window.as_millis() as u64;
        // A zero/limitless window disables rate limiting.
        if window_ms == 0 || self.limits.max_allocs_per_window == 0 {
            return self.limits.max_allocs_per_window != 0;
        }

        loop {
            let start = self.rate_window_start_ms.load(Ordering::Relaxed);
            if now.saturating_sub(start) >= window_ms {
                // Roll the window forward. Whichever thread wins the CAS resets the counter.
                if self
                    .rate_window_start_ms
                    .compare_exchange(start, now, Ordering::Relaxed, Ordering::Relaxed)
                    .is_ok()
                {
                    self.rate_window_count.store(1, Ordering::Relaxed);
                    return true;
                }
                // Lost the race; re-read and retry.
                continue;
            }

            let prev = self.rate_window_count.fetch_add(1, Ordering::Relaxed);
            if prev < self.limits.max_allocs_per_window {
                return true;
            }
            // Over budget for this window. Leave the counter saturated; it is reset on the
            // next window roll. Avoid unbounded growth by not incrementing further.
            self.rate_window_count
                .store(self.limits.max_allocs_per_window, Ordering::Relaxed);
            return false;
        }
    }

    /// Evicts every mapping idle for at least `idle_ttl`, using the process clock.
    /// Returns the number of entries reclaimed.
    #[allow(dead_code)]
    pub fn evict_idle(&self) -> usize {
        self.evict_idle_at(now_ms())
    }

    /// [`Self::evict_idle`] with an explicit monotonic timestamp, for deterministic tests.
    fn evict_idle_at(&self, now: u64) -> usize {
        let ttl_ms = self.limits.idle_ttl.as_millis() as u64;
        if ttl_ms == 0 {
            return 0;
        }

        // Collect first to avoid holding shard locks across the paired removals.
        let stale: Vec<(IpAddr, String)> = self
            .ip_to_dest
            .iter()
            .filter(|e| {
                now.saturating_sub(e.value().last_used_ms.load(Ordering::Relaxed)) >= ttl_ms
            })
            .map(|e| (*e.key(), e.value().domain.clone()))
            .collect();

        for (ip, domain) in &stale {
            self.ip_to_dest.remove(ip);
            // Only drop the reverse mapping if it still points at this IP (guards against a
            // concurrent re-allocation of the same domain).
            self.domain_to_ip
                .remove_if(domain, |_, mapped| mapped == ip);
        }
        if !stale.is_empty() {
            envoy_log_info!("Evicted {} idle virtual IP mapping(s)", stale.len());
        }
        stale.len()
    }

    /// Clears every virtual-IP mapping, the per-CIDR offset counters, and the rate-limiter state.
    ///
    /// Intended for a configuration generation bump: after a flush, a virtual IP that is minted
    /// again can never resolve to a domain from a previous generation, eliminating stale
    /// IP reuse. Concurrent in-flight allocations after the flush simply repopulate the cache.
    #[allow(dead_code)]
    pub fn flush(&self) {
        self.ip_to_dest.clear();
        self.domain_to_ip.clear();
        self.offsets.clear();
        self.rate_window_start_ms.store(0, Ordering::Relaxed);
        self.rate_window_count.store(0, Ordering::Relaxed);
        envoy_log_info!("Flushed virtual IP cache (generation reset)");
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
        IpAddr::V4(s.parse::<Ipv4Addr>().unwrap())
    }
    fn v6(s: &str) -> IpAddr {
        IpAddr::V6(s.parse::<Ipv6Addr>().unwrap())
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

    // ── IPv6 / AAAA allocation tests ─────────────────────────────────────────

    #[test]
    fn test_v6_single_range_sequential_allocation() {
        let cache = VirtualIpCache::new();

        let ip1 = cache
            .allocate("api.aws.com".into(), HashMap::new(), v6("fd00:1::"), 64)
            .unwrap();
        let ip2 = cache
            .allocate("s3.aws.com".into(), HashMap::new(), v6("fd00:1::"), 64)
            .unwrap();

        assert_eq!(ip1, v6("fd00:1::"));
        assert_eq!(ip2, v6("fd00:1::1"));
    }

    #[test]
    fn test_v6_ranges_independent_of_v4() {
        let cache = VirtualIpCache::new();

        let v4ip = cache
            .allocate("v4.example.com".into(), HashMap::new(), v4("10.0.0.0"), 24)
            .unwrap();
        let v6ip = cache
            .allocate("v6.example.com".into(), HashMap::new(), v6("fd00:2::"), 64)
            .unwrap();

        assert_eq!(v4ip, v4("10.0.0.0"));
        assert_eq!(v6ip, v6("fd00:2::"));
    }

    #[test]
    fn test_v6_lookup_roundtrip() {
        let cache = VirtualIpCache::new();

        let mut meta = HashMap::new();
        meta.insert("cluster".to_string(), "v6_cluster".to_string());

        let ip = cache
            .allocate("api.v6.com".into(), meta, v6("fd00:3::"), 64)
            .unwrap();
        assert!(matches!(ip, IpAddr::V6(_)));

        let (domain, metadata) = cache.lookup(ip).unwrap();
        assert_eq!(domain, "api.v6.com");
        assert_eq!(metadata.get("cluster").unwrap(), "v6_cluster");
        assert!(cache.lookup(v6("fd00:3::dead")).is_none());
    }

    #[test]
    fn test_v6_range_exhaustion() {
        let cache = VirtualIpCache::new();

        // /126 over IPv6 = 4 addresses.
        for i in 0..4 {
            assert!(cache
                .allocate(format!("v6d{}.com", i), HashMap::new(), v6("fd00:4::"), 126)
                .is_some());
        }
        assert!(cache
            .allocate("v6overflow.com".into(), HashMap::new(), v6("fd00:4::"), 126)
            .is_none());
    }

    #[test]
    fn test_cidr_capacity() {
        assert_eq!(cidr_capacity(&v4("10.0.0.0"), 24), 256);
        assert_eq!(cidr_capacity(&v4("10.0.0.0"), 32), 1);
        assert_eq!(cidr_capacity(&v4("10.0.0.0"), 30), 4);
        assert_eq!(cidr_capacity(&v6("fd00::"), 126), 4);
        assert_eq!(cidr_capacity(&v6("fd00::"), 128), 1);
        assert_eq!(cidr_capacity(&v6("fd00::"), 64), 1u128 << 64);
    }

    // ── Hardening tests ──────────────────────────────────────────────────────

    fn limits(max_entries: usize) -> CacheLimits {
        CacheLimits {
            max_entries,
            // Disable TTL and rate limiting unless a test opts in, so cap tests are isolated.
            idle_ttl: Duration::ZERO,
            max_allocs_per_window: u64::MAX,
            rate_window: Duration::from_secs(1),
        }
    }

    #[test]
    fn test_max_entries_cap_refuses_new_when_full() {
        // /24 has plenty of address space, so the *entry cap* (not exhaustion) must be what bites.
        let cache = VirtualIpCache::with_limits(limits(3));

        for i in 0..3 {
            assert!(
                cache
                    .allocate(format!("d{i}.com"), HashMap::new(), v4("10.0.0.0"), 24)
                    .is_some(),
                "allocation {i} within cap should succeed"
            );
        }
        assert_eq!(cache.len(), 3);

        // Fourth distinct domain exceeds the cap: must mint nothing (fail closed).
        assert!(
            cache
                .allocate("overflow.com".into(), HashMap::new(), v4("10.0.0.0"), 24)
                .is_none(),
            "allocation beyond cap must return None"
        );
        assert_eq!(cache.len(), 3, "cap must not be exceeded");

        // A domain already in the cache is still served even when full.
        assert!(
            cache
                .allocate("d0.com".into(), HashMap::new(), v4("10.0.0.0"), 24)
                .is_some(),
            "known domain must still resolve when cache is full"
        );
    }

    #[test]
    fn test_cap_reclaims_idle_space_then_admits() {
        let mut l = limits(2);
        l.idle_ttl = Duration::from_millis(100);
        let cache = VirtualIpCache::with_limits(l);

        // Fill to cap at t=0.
        cache
            .allocate_at("a.com".into(), HashMap::new(), v4("10.0.0.0"), 24, 0)
            .unwrap();
        cache
            .allocate_at("b.com".into(), HashMap::new(), v4("10.0.0.0"), 24, 0)
            .unwrap();
        assert_eq!(cache.len(), 2);

        // At t=200ms both entries are idle past the 100ms TTL. A new allocation triggers
        // opportunistic eviction and then admits the newcomer.
        let ip = cache.allocate_at("c.com".into(), HashMap::new(), v4("10.0.0.0"), 24, 200);
        assert!(ip.is_some(), "should admit after reclaiming idle space");
        assert!(cache.len() <= 2, "cap still respected after reclaim");
    }

    #[test]
    fn test_idle_ttl_eviction() {
        let mut l = limits(100);
        l.idle_ttl = Duration::from_millis(500);
        let cache = VirtualIpCache::with_limits(l);

        let ip = cache
            .allocate_at("idle.com".into(), HashMap::new(), v4("10.0.0.0"), 24, 0)
            .unwrap();
        assert!(cache.lookup(ip).is_some());

        // Not yet idle.
        assert_eq!(cache.evict_idle_at(400), 0);
        assert_eq!(cache.len(), 1);

        // Past TTL: evicted, and the reverse mapping is gone too (so the IP can be reused fresh).
        assert_eq!(cache.evict_idle_at(600), 1);
        assert_eq!(cache.len(), 0);
        assert!(cache.lookup(ip).is_none());
    }

    #[test]
    fn test_rate_limit_refuses_burst() {
        let l = CacheLimits {
            max_entries: 1000,
            idle_ttl: Duration::ZERO,
            max_allocs_per_window: 2,
            rate_window: Duration::from_millis(1000),
        };
        let cache = VirtualIpCache::with_limits(l);

        // Two new allocations inside one window succeed.
        assert!(cache
            .allocate_at("r0.com".into(), HashMap::new(), v4("10.0.0.0"), 24, 10)
            .is_some());
        assert!(cache
            .allocate_at("r1.com".into(), HashMap::new(), v4("10.0.0.0"), 24, 20)
            .is_some());
        // Third within the same window is refused (fail closed).
        assert!(cache
            .allocate_at("r2.com".into(), HashMap::new(), v4("10.0.0.0"), 24, 30)
            .is_none());

        // Known domain is not rate limited even while over budget.
        assert!(cache
            .allocate_at("r0.com".into(), HashMap::new(), v4("10.0.0.0"), 24, 40)
            .is_some());

        // Next window resets the budget.
        assert!(cache
            .allocate_at("r3.com".into(), HashMap::new(), v4("10.0.0.0"), 24, 1100)
            .is_some());
    }

    #[test]
    fn test_flush_clears_mappings_and_enables_ip_reuse() {
        let cache = VirtualIpCache::new();

        let ip1 = cache
            .allocate(
                "gen1.example.com".into(),
                HashMap::new(),
                v4("10.50.0.0"),
                24,
            )
            .unwrap();
        assert_eq!(ip1, v4("10.50.0.0"));
        assert_eq!(
            cache.lookup(ip1).unwrap().0,
            "gen1.example.com",
            "pre-flush the IP maps to gen1"
        );

        // Generation bump.
        cache.flush();
        assert!(cache.is_empty());
        assert!(
            cache.lookup(ip1).is_none(),
            "after flush the old virtual IP resolves to nothing (no stale FQDN)"
        );

        // A fresh allocation reuses the same first IP, now bound to the new generation's domain —
        // the old domain can never be reached via the recycled IP.
        let ip2 = cache
            .allocate(
                "gen2.example.com".into(),
                HashMap::new(),
                v4("10.50.0.0"),
                24,
            )
            .unwrap();
        assert_eq!(
            ip2,
            v4("10.50.0.0"),
            "offsets reset, so the first IP is reused"
        );
        assert_eq!(cache.lookup(ip2).unwrap().0, "gen2.example.com");
    }

    #[test]
    fn test_flush_resets_rate_limit() {
        let l = CacheLimits {
            max_entries: 1000,
            idle_ttl: Duration::ZERO,
            max_allocs_per_window: 1,
            rate_window: Duration::from_millis(1000),
        };
        let cache = VirtualIpCache::with_limits(l);

        assert!(cache
            .allocate_at("a.com".into(), HashMap::new(), v4("10.0.0.0"), 24, 10)
            .is_some());
        assert!(
            cache
                .allocate_at("b.com".into(), HashMap::new(), v4("10.0.0.0"), 24, 20)
                .is_none(),
            "second allocation in window is rate limited"
        );

        cache.flush();

        // After flush the rate window is reset, so an allocation is permitted again.
        assert!(cache
            .allocate_at("c.com".into(), HashMap::new(), v4("10.0.0.0"), 24, 30)
            .is_some());
    }
}
