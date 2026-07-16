// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

//! A DNS gateway filter that intercepts DNS queries and returns virtual IPs for matched domains.
//!
//! This filter implements:
//! 1. UDP listener filter structure with `UdpListenerFilterConfig` and `UdpListenerFilter` traits.
//! 2. DNS query parsing and response.

use crate::config;
use crate::virtual_ip_cache::get_cache;
use envoy_proxy_dynamic_modules_rust_sdk::*;
use hickory_proto::op::{Message, MessageType, ResponseCode};
use hickory_proto::rr::{Name, RData, Record, RecordType};
use hickory_proto::serialize::binary::{BinDecodable, BinDecoder};
use std::net::IpAddr;
use std::sync::Arc;

/// Matches a domain against a pattern.
/// Supports exact matches, single-level wildcards like "*.aws.com", and a bare "*"
/// catch-all. A "*.aws.com" wildcard matches exactly one subdomain level, e.g. it
/// matches "api.aws.com" but not "sub.api.aws.com".
fn matches_domain(pattern: &str, domain: &str) -> bool {
    // A bare "*" is a catch-all that matches every queried domain, minting a virtual IP
    // for each. Use it as a low-priority final matcher when you want to intercept all DNS
    // and defer routing/authorization to downstream filters (e.g. tcp_proxy route selection
    // on the filter state, or an ext_authz check) rather than enumerating every domain here.
    if pattern == "*" {
        return true;
    }
    let Some(base) = pattern.strip_prefix("*.") else {
        return domain == pattern;
    };

    domain
        .strip_suffix(base)
        .and_then(|prefix| prefix.strip_suffix('.'))
        .is_some_and(|label| !label.is_empty() && !label.contains('.'))
}

/// The filter configuration that implements
/// [`envoy_proxy_dynamic_modules_rust_sdk::UdpListenerFilterConfig`].
///
/// This configuration is shared across all UDP listener filter instances.
pub struct DnsGatewayFilterConfig {
    config: Arc<config::DnsGateway>,
}

impl DnsGatewayFilterConfig {
    /// Creates a new DNS gateway filter configuration from the raw config bytes.
    ///
    /// The config arrives as the JSON object passed via `--config`, e.g.
    /// `{"domains":[...],"fail_open":false}`.
    pub fn new(config: &[u8]) -> Option<Self> {
        let gateway_config: config::DnsGateway = serde_json::from_slice(config)
            .map_err(|e| envoy_log_error!("Invalid DNS gateway config: {e}"))
            .ok()?;

        for d in &gateway_config.domains {
            // A bare "*" is an accepted catch-all (it matches every queried domain at runtime
            // via matches_domain). Skip the DNS-name parse and the bare-wildcard rejection for
            // it — those reject "*" as an invalid host — but still validate its base_ip and
            // prefix_len below like any other matcher.
            if d.domain != "*" {
                let name = Name::from_utf8(&d.domain)
                    .map_err(|e| envoy_log_error!("Invalid domain '{}': {e}", d.domain))
                    .ok()?;
                if name.is_wildcard() && name.num_labels() < 2 {
                    envoy_log_error!(
                        "Bare wildcard '{}' is not allowed, use '*.example.com' or a bare \"*\" catch-all instead",
                        d.domain
                    );
                    return None;
                }
            }

            // Accept both IPv4 (A) and IPv6 (AAAA) base addresses. The address family of
            // base_ip determines which record type the gateway answers for this domain.
            let base = d
                .parse_base_ip()
                .map_err(|e| {
                    envoy_log_error!("Invalid base_ip '{}' for '{}': {e}", d.base_ip, d.domain)
                })
                .ok()?;

            // prefix_len upper bound depends on the family: 32 for IPv4, 128 for IPv6.
            let max = config::DomainMatcher::max_prefix_len(&base);
            if !(1..=max).contains(&d.prefix_len) {
                envoy_log_error!(
                    "Invalid prefix_len {} for '{}' (must be 1..={})",
                    d.prefix_len,
                    d.domain,
                    max
                );
                return None;
            }
        }

        envoy_log_info!("Initialized with {} domains", gateway_config.domains.len());

        Some(DnsGatewayFilterConfig {
            config: Arc::new(gateway_config),
        })
    }
}

impl<ELF: EnvoyUdpListenerFilter> UdpListenerFilterConfig<ELF> for DnsGatewayFilterConfig {
    fn new_udp_listener_filter(&self, _envoy: &mut ELF) -> Box<dyn UdpListenerFilter<ELF>> {
        Box::new(DnsGatewayFilter {
            config: Arc::clone(&self.config),
        })
    }
}

/// The DNS gateway filter that implements
/// [`envoy_proxy_dynamic_modules_rust_sdk::UdpListenerFilter`].
///
/// Intercepts DNS queries and returns virtual IPs for domains matching configured matchers.
struct DnsGatewayFilter {
    config: Arc<config::DnsGateway>,
}

impl DnsGatewayFilter {
    /// Processes a raw DNS query and returns the response bytes to send back, or `None` to
    /// forward the query upstream unchanged.
    ///
    /// Returns `None` for non-matching domains, non-query messages, and CIDR-exhausted matched
    /// domains when `fail_open` is true. Returns a NODATA response for non-A record types on
    /// matched domains and for CIDR-exhausted matched domains when `fail_open` is false.
    fn process_dns_query(&self, data: &[u8]) -> Option<Vec<u8>> {
        let query = Message::read(&mut BinDecoder::new(data))
            .map_err(|e| envoy_log_warn!("Failed to parse DNS query: {}", e))
            .ok()?;
        if query.message_type() != MessageType::Query {
            return None;
        }

        let question = query.queries().first()?;
        // DNS names are fully qualified with a trailing dot (e.g. "api.aws.com.").
        // Strip it so our wildcard patterns like "*.aws.com" match correctly.
        let domain = question.name().to_utf8().trim_end_matches('.').to_string();

        // Establish precedence first: the first matcher (in config order) whose pattern matches
        // this domain "owns" it — exactly as before dual-stack. A non-matching domain passes
        // through (None) to upstream resolvers.
        let owner = self
            .config
            .domains
            .iter()
            .find(|m| matches_domain(&m.domain, &domain))?;

        // We only mint virtual IPs for A/AAAA. Any other query type on an intercepted domain
        // returns NODATA (never pass-through, so it can't leak to upstream resolvers).
        let want_v6 = match question.query_type() {
            RecordType::A => false,
            RecordType::AAAA => true,
            _ => return build_nodata_response(&query).ok(),
        };

        // Dual-stack pairs families only within the SAME matcher pattern: a domain may be listed
        // twice with an identical pattern, once with an IPv4 base_ip and once with an IPv6 base_ip,
        // so A resolves from the v4 range and AAAA from the v6 range. Restricting the family search
        // to the winning pattern preserves first-match precedence — a later, broader matcher (e.g.
        // a "*" catch-all) is never selected ahead of an earlier, more specific one just because it
        // carries the queried family. If the winning pattern has no matcher of the requested
        // family, return NODATA (fail closed) — never forward, so a client can't obtain a real
        // address of the other family and bypass the winning matcher's routing/authorization.
        let matcher = self.config.domains.iter().find(|m| {
            m.domain == owner.domain
                && m.parse_base_ip()
                    .map(|ip| ip.is_ipv6() == want_v6)
                    .unwrap_or(false)
        });
        let Some(matcher) = matcher else {
            return build_nodata_response(&query).ok();
        };
        let base_ip = matcher.parse_base_ip().ok()?;

        match get_cache().allocate(
            domain,
            matcher.metadata.clone(),
            base_ip,
            matcher.prefix_len as u8,
        ) {
            Some(ip) => build_dns_response(&query, question.name(), ip).ok(),
            None if self.config.fail_open => None,
            None => build_nodata_response(&query).ok(),
        }
    }
}

impl<ELF: EnvoyUdpListenerFilter> UdpListenerFilter<ELF> for DnsGatewayFilter {
    fn on_data(
        &mut self,
        envoy_filter: &mut ELF,
    ) -> abi::envoy_dynamic_module_type_on_udp_listener_filter_status {
        use abi::envoy_dynamic_module_type_on_udp_listener_filter_status::{
            Continue, StopIteration,
        };

        let (chunks, _) = envoy_filter.get_datagram_data();
        let data: Vec<u8> = chunks.iter().flat_map(|c| c.as_slice()).copied().collect();
        let peer = envoy_filter.get_peer_address();

        let Some(response) = self.process_dns_query(&data) else {
            return Continue;
        };
        let Some((addr, port)) = peer else {
            envoy_log_error!("No peer address available, cannot send response");
            return StopIteration;
        };

        if !envoy_filter.send_datagram(&response, &addr, port) {
            envoy_log_error!("Failed to send datagram to {}:{}", addr, port);
        }
        StopIteration
    }
}

fn build_dns_response(
    query_message: &Message,
    name: &Name,
    ip: IpAddr,
) -> Result<Vec<u8>, Box<dyn std::error::Error>> {
    let mut response = query_message.clone();

    response.set_message_type(MessageType::Response);
    response.set_response_code(ResponseCode::NoError);
    response.set_recursion_available(true);
    response.set_authoritative(true);

    // Emit an A record for IPv4 virtual IPs and an AAAA record for IPv6 virtual IPs.
    let rdata = match ip {
        IpAddr::V4(v4) => RData::A(v4.into()),
        IpAddr::V6(v6) => RData::AAAA(v6.into()),
    };
    let record = Record::from_rdata(name.clone(), 600, rdata);

    response.add_answer(record);

    let bytes = response.to_vec()?;
    Ok(bytes)
}

fn build_nodata_response(query_message: &Message) -> Result<Vec<u8>, Box<dyn std::error::Error>> {
    let mut response = query_message.clone();

    response.set_message_type(MessageType::Response);
    response.set_response_code(ResponseCode::NoError);
    response.set_recursion_available(true);
    response.set_authoritative(true);

    let bytes = response.to_vec()?;
    Ok(bytes)
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::HashMap;

    #[test]
    fn test_domain_matcher_wildcard() {
        assert!(matches_domain("*.aws.com", "api.aws.com"));
        assert!(matches_domain("*.aws.com", "s3.aws.com"));
        assert!(matches_domain("*.aws.com", "lambda.aws.com"));

        assert!(!matches_domain("*.aws.com", "sub.api.aws.com"));
        assert!(!matches_domain("*.aws.com", "deep.sub.api.aws.com"));
        assert!(!matches_domain("*.aws.com", "aws.com"));
        assert!(!matches_domain("*.aws.com", "xaws.com"));
        assert!(!matches_domain("*.aws.com", "aws.com.evil.com"));
        assert!(!matches_domain("*.aws.com", "api.aws.org"));
    }

    #[test]
    fn test_domain_matcher_catchall() {
        // A bare "*" matches every queried domain, at any label depth.
        assert!(matches_domain("*", "aws.com"));
        assert!(matches_domain("*", "api.aws.com"));
        assert!(matches_domain("*", "deep.sub.api.aws.com"));
        assert!(matches_domain("*", "example.org"));
        assert!(matches_domain("*", "localhost"));
    }

    #[test]
    fn test_domain_matcher_exact() {
        assert!(matches_domain("api.example.com", "api.example.com"));

        assert!(!matches_domain("api.example.com", "www.api.example.com"));
        assert!(!matches_domain("api.example.com", "example.com"));
        assert!(!matches_domain("api.example.com", "api.example.org"));
    }

    #[test]
    fn test_config_parsing_valid() {
        let config = r#"{
            "domains": [
                {
                    "domain": "*.aws.com",
                    "base_ip": "10.0.0.0",
                    "prefix_len": 24,
                    "metadata": {
                        "cluster": "aws_cluster",
                        "region": "us-east-1"
                    }
                }
            ]
        }"#;

        let config = DnsGatewayFilterConfig::new(config.as_bytes()).unwrap();
        assert_eq!(config.config.domains.len(), 1);
        assert_eq!(config.config.domains[0].domain, "*.aws.com");
        assert_eq!(
            config.config.domains[0].metadata.get("cluster").unwrap(),
            "aws_cluster"
        );
        assert_eq!(
            config.config.domains[0].metadata.get("region").unwrap(),
            "us-east-1"
        );
    }

    #[test]
    fn test_config_parsing_multiple_domains() {
        let config = r#"{
            "domains": [
                {"domain": "*.aws.com", "base_ip": "10.0.0.0", "prefix_len": 24, "metadata": {"cluster": "aws"}},
                {"domain": "*.google.com", "base_ip": "10.0.1.0", "prefix_len": 24, "metadata": {"cluster": "google"}},
                {"domain": "exact.example.com", "base_ip": "10.0.2.0", "prefix_len": 24, "metadata": {"cluster": "exact"}}
            ]
        }"#;

        let config = DnsGatewayFilterConfig::new(config.as_bytes()).unwrap();
        assert_eq!(config.config.domains.len(), 3);
    }

    #[test]
    fn test_config_parsing_missing_base_ip() {
        let config = r#"{
            "domains": [
                {"domain": "*.aws.com", "prefix_len": 24, "metadata": {}}
            ]
        }"#;

        assert!(DnsGatewayFilterConfig::new(config.as_bytes()).is_none());
    }

    #[test]
    fn test_config_parsing_missing_prefix_len() {
        // serde defaults missing uint32 to 0, which fails the 1..=32 range check.
        let config = r#"{
            "domains": [
                {"domain": "*.aws.com", "base_ip": "10.10.0.0", "metadata": {}}
            ]
        }"#;

        assert!(DnsGatewayFilterConfig::new(config.as_bytes()).is_none());
    }

    #[test]
    fn test_config_parsing_invalid_prefix_len() {
        let config = r#"{
            "domains": [
                {"domain": "*.aws.com", "base_ip": "10.10.0.0", "prefix_len": 33, "metadata": {}}
            ]
        }"#;

        assert!(DnsGatewayFilterConfig::new(config.as_bytes()).is_none());
    }

    #[test]
    fn test_config_parsing_invalid_json() {
        assert!(DnsGatewayFilterConfig::new(b"invalid json {").is_none());
    }

    #[test]
    fn test_config_parsing_non_string_metadata_value() {
        let config = r#"{
            "domains": [
                {"domain": "*.aws.com", "base_ip": "10.10.0.0", "prefix_len": 24, "metadata": {"count": 42}}
            ]
        }"#;

        assert!(DnsGatewayFilterConfig::new(config.as_bytes()).is_none());
    }

    #[test]
    fn test_domain_stripping_trailing_dot() {
        let domain_raw = "api.aws.com.";
        let domain = domain_raw.strip_suffix('.').unwrap_or(domain_raw);
        assert_eq!(domain, "api.aws.com");
    }

    #[test]
    fn test_domain_without_trailing_dot() {
        let domain_raw = "api.aws.com";
        let domain = domain_raw.strip_suffix('.').unwrap_or(domain_raw);
        assert_eq!(domain, "api.aws.com");
    }

    #[test]
    fn test_dns_response_building() {
        let mut query = Message::new();
        query.set_id(12345);
        query.set_message_type(MessageType::Query);
        query.set_recursion_desired(true);

        let name = Name::from_utf8("test.example.com").unwrap();
        let ip = IpAddr::V4(std::net::Ipv4Addr::new(10, 10, 0, 1));

        let result = build_dns_response(&query, &name, ip);
        assert!(result.is_ok());

        let response_bytes = result.unwrap();
        assert!(!response_bytes.is_empty());

        let mut decoder = BinDecoder::new(&response_bytes);
        let response = Message::read(&mut decoder).unwrap();

        assert_eq!(response.id(), 12345);
        assert_eq!(response.message_type(), MessageType::Response);
        assert_eq!(response.response_code(), ResponseCode::NoError);
        assert!(response.recursion_available());
        assert_eq!(response.answers().len(), 1);
        assert!(matches!(response.answers()[0].data(), RData::A(_)));
    }

    #[test]
    fn test_dns_response_building_v6_emits_aaaa() {
        let mut query = Message::new();
        query.set_id(2222);
        query.set_message_type(MessageType::Query);
        query.set_recursion_desired(true);

        let name = Name::from_utf8("test.example.com").unwrap();
        let ip = IpAddr::V6("fd00::1".parse::<std::net::Ipv6Addr>().unwrap());

        let response_bytes = build_dns_response(&query, &name, ip).unwrap();
        let response = Message::read(&mut BinDecoder::new(&response_bytes)).unwrap();

        assert_eq!(response.message_type(), MessageType::Response);
        assert_eq!(response.answers().len(), 1);
        match response.answers()[0].data() {
            RData::AAAA(a) => assert_eq!(a.0, "fd00::1".parse::<std::net::Ipv6Addr>().unwrap()),
            other => panic!("expected AAAA, got {:?}", other),
        }
    }

    #[test]
    fn test_nodata_response_building() {
        let mut query = Message::new();
        query.set_id(54321);
        query.set_message_type(MessageType::Query);
        query.set_recursion_desired(false);

        let result = build_nodata_response(&query);
        assert!(result.is_ok());

        let response_bytes = result.unwrap();
        assert!(!response_bytes.is_empty());

        let mut decoder = BinDecoder::new(&response_bytes);
        let response = Message::read(&mut decoder).unwrap();

        assert_eq!(response.id(), 54321);
        assert_eq!(response.message_type(), MessageType::Response);
        assert_eq!(response.response_code(), ResponseCode::NoError);
        assert!(response.recursion_available());
        assert_eq!(response.answers().len(), 0);
    }

    #[test]
    fn test_config_parsing_accepts_catchall_wildcard() {
        // A bare "*" is an accepted catch-all; its base_ip/prefix_len are still validated.
        let config = r#"{
            "domains": [
                {"domain": "*", "base_ip": "10.0.0.0", "prefix_len": 24, "metadata": {}}
            ]
        }"#;

        assert!(DnsGatewayFilterConfig::new(config.as_bytes()).is_some());
    }

    #[test]
    fn test_config_parsing_catchall_still_validates_base_ip() {
        // The name-parse is skipped for "*", but base_ip is still validated.
        let config = r#"{
            "domains": [
                {"domain": "*", "base_ip": "not-an-ip", "prefix_len": 24, "metadata": {}}
            ]
        }"#;

        assert!(DnsGatewayFilterConfig::new(config.as_bytes()).is_none());
    }

    #[test]
    fn test_process_dns_query_catchall_matches_any_domain() {
        let filter = DnsGatewayFilter {
            config: Arc::new(config::DnsGateway {
                domains: vec![config::DomainMatcher {
                    domain: "*".to_string(),
                    base_ip: "10.123.0.0".to_string(),
                    prefix_len: 24,
                    metadata: HashMap::new(),
                }],
                fail_open: false,
            }),
        };

        // An arbitrary domain not enumerated anywhere still resolves via the catch-all.
        let query = make_dns_query("anything.example.net", RecordType::A);
        let msg = parse_response(&filter.process_dns_query(&query).unwrap());
        assert_eq!(msg.answers().len(), 1);
        match msg.answers()[0].data() {
            RData::A(ip) => assert_eq!(&ip.0.octets()[..3], &[10, 123, 0]),
            other => panic!("expected A record from catch-all, got {:?}", other),
        }
    }

    // ── process_dns_query tests ──────────────────────────────────────────────

    fn make_dns_query(domain: &str, record_type: RecordType) -> Vec<u8> {
        use hickory_proto::op::{OpCode, Query as DnsQuery};
        use hickory_proto::rr::DNSClass;

        let name = Name::from_utf8(domain).unwrap();
        let mut q = DnsQuery::new();
        q.set_name(name);
        q.set_query_type(record_type);
        q.set_query_class(DNSClass::IN);

        let mut msg = Message::new();
        msg.set_id(1);
        msg.set_message_type(MessageType::Query);
        msg.set_op_code(OpCode::Query);
        msg.set_recursion_desired(true);
        msg.add_query(q);
        msg.to_vec().unwrap()
    }

    fn parse_response(bytes: &[u8]) -> Message {
        Message::read(&mut BinDecoder::new(bytes)).unwrap()
    }

    #[test]
    fn test_process_dns_query_a_record_matched() {
        let filter = DnsGatewayFilter {
            config: Arc::new(config::DnsGateway {
                domains: vec![config::DomainMatcher {
                    domain: "*.on-data-test.com".to_string(),
                    base_ip: "10.100.0.0".to_string(),
                    prefix_len: 24,
                    metadata: HashMap::new(),
                }],
                fail_open: false,
            }),
        };

        let query = make_dns_query("api.on-data-test.com", RecordType::A);
        let response = filter.process_dns_query(&query);
        assert!(
            response.is_some(),
            "expected a DNS response for matched A query"
        );

        let msg = parse_response(&response.unwrap());
        assert_eq!(msg.message_type(), MessageType::Response);
        assert_eq!(msg.response_code(), ResponseCode::NoError);
        assert_eq!(msg.answers().len(), 1);
        // The allocated virtual IP must fall within 10.100.0.0/24
        match msg.answers()[0].data() {
            RData::A(ip) => assert_eq!(&ip.0.octets()[..3], &[10, 100, 0]),
            other => panic!("expected A record in response, got {:?}", other),
        }
    }

    #[test]
    fn test_process_dns_query_aaaa_record_matched_v6_matcher() {
        // An IPv6 base_ip answers AAAA queries with an AAAA record from its range.
        let filter = DnsGatewayFilter {
            config: Arc::new(config::DnsGateway {
                domains: vec![config::DomainMatcher {
                    domain: "*.v6-data-test.com".to_string(),
                    base_ip: "fd00:100::".to_string(),
                    prefix_len: 64,
                    metadata: HashMap::new(),
                }],
                fail_open: false,
            }),
        };

        let query = make_dns_query("api.v6-data-test.com", RecordType::AAAA);
        let response = filter.process_dns_query(&query);
        assert!(
            response.is_some(),
            "expected an AAAA response for matched AAAA query on a v6 matcher"
        );

        let msg = parse_response(&response.unwrap());
        assert_eq!(msg.answers().len(), 1);
        match msg.answers()[0].data() {
            RData::AAAA(ip) => {
                assert_eq!(&ip.0.octets()[..6], &[0xfd, 0x00, 0x01, 0x00, 0x00, 0x00])
            }
            other => panic!("expected AAAA record in response, got {:?}", other),
        }
    }

    #[test]
    fn test_process_dns_query_a_on_v6_matcher_returns_nodata() {
        // An IPv6 base_ip answers only AAAA; an A query returns NODATA.
        let filter = DnsGatewayFilter {
            config: Arc::new(config::DnsGateway {
                domains: vec![config::DomainMatcher {
                    domain: "*.v6-data-test.com".to_string(),
                    base_ip: "fd00:200::".to_string(),
                    prefix_len: 64,
                    metadata: HashMap::new(),
                }],
                fail_open: false,
            }),
        };

        let query = make_dns_query("api.v6-data-test.com", RecordType::A);
        let msg = parse_response(&filter.process_dns_query(&query).unwrap());
        assert_eq!(
            msg.answers().len(),
            0,
            "A query against a v6 matcher must return NODATA"
        );
    }

    #[test]
    fn test_process_dns_query_dual_stack_answers_both_families() {
        // A domain configured with both an IPv4 and an IPv6 matcher answers A from the v4
        // range and AAAA from the v6 range (dual-stack).
        let filter = DnsGatewayFilter {
            config: Arc::new(config::DnsGateway {
                domains: vec![
                    config::DomainMatcher {
                        domain: "*.dual-test.com".to_string(),
                        base_ip: "10.140.0.0".to_string(),
                        prefix_len: 24,
                        metadata: HashMap::new(),
                    },
                    config::DomainMatcher {
                        domain: "*.dual-test.com".to_string(),
                        base_ip: "fd00:d00a::".to_string(),
                        prefix_len: 64,
                        metadata: HashMap::new(),
                    },
                ],
                fail_open: false,
            }),
        };

        let a = parse_response(
            &filter
                .process_dns_query(&make_dns_query("api.dual-test.com", RecordType::A))
                .unwrap(),
        );
        assert_eq!(a.answers().len(), 1);
        match a.answers()[0].data() {
            RData::A(ip) => assert_eq!(&ip.0.octets()[..3], &[10, 140, 0]),
            other => panic!("expected A record from the v4 matcher, got {:?}", other),
        }

        let aaaa = parse_response(
            &filter
                .process_dns_query(&make_dns_query("api.dual-test.com", RecordType::AAAA))
                .unwrap(),
        );
        assert_eq!(aaaa.answers().len(), 1);
        match aaaa.answers()[0].data() {
            RData::AAAA(ip) => {
                assert_eq!(&ip.0.octets()[..6], &[0xfd, 0x00, 0xd0, 0x0a, 0x00, 0x00])
            }
            other => panic!("expected AAAA record from the v6 matcher, got {:?}", other),
        }
    }

    #[test]
    fn test_process_dns_query_family_selection_preserves_matcher_precedence() {
        // An earlier, more specific IPv4 matcher must not be bypassed by a later IPv6 catch-all.
        // Before dual-stack, an AAAA query for the specific domain returned NODATA; it must still
        // do so (the specific v4 rule owns the domain) rather than answering from the "*" rule.
        let filter = DnsGatewayFilter {
            config: Arc::new(config::DnsGateway {
                domains: vec![
                    config::DomainMatcher {
                        domain: "api.aws.com".to_string(),
                        base_ip: "10.150.0.0".to_string(),
                        prefix_len: 24,
                        metadata: HashMap::new(),
                    },
                    config::DomainMatcher {
                        domain: "*".to_string(),
                        base_ip: "fd00:cafe::".to_string(),
                        prefix_len: 64,
                        metadata: HashMap::new(),
                    },
                ],
                fail_open: false,
            }),
        };

        // AAAA for the specific domain: the v4 exact rule owns it and has no v6 pair -> NODATA;
        // must NOT fall through to the later v6 catch-all.
        let aaaa = parse_response(
            &filter
                .process_dns_query(&make_dns_query("api.aws.com", RecordType::AAAA))
                .unwrap(),
        );
        assert_eq!(
            aaaa.answers().len(),
            0,
            "AAAA on a v4-only specific rule must NODATA, not use the later catch-all"
        );

        // A for the specific domain still answers from its own v4 range.
        let a = parse_response(
            &filter
                .process_dns_query(&make_dns_query("api.aws.com", RecordType::A))
                .unwrap(),
        );
        assert_eq!(a.answers().len(), 1);
        match a.answers()[0].data() {
            RData::A(ip) => assert_eq!(&ip.0.octets()[..3], &[10, 150, 0]),
            other => panic!("expected A from the specific v4 rule, got {:?}", other),
        }

        // A domain only matched by "*" is served by the catch-all (AAAA), and its A query
        // NODATAs (the "*" pattern only has a v6 entry).
        let other_aaaa = parse_response(
            &filter
                .process_dns_query(&make_dns_query("other.example.net", RecordType::AAAA))
                .unwrap(),
        );
        assert_eq!(other_aaaa.answers().len(), 1);
        assert!(matches!(other_aaaa.answers()[0].data(), RData::AAAA(_)));

        let other_a = parse_response(
            &filter
                .process_dns_query(&make_dns_query("other.example.net", RecordType::A))
                .unwrap(),
        );
        assert_eq!(
            other_a.answers().len(),
            0,
            "the catch-all pattern only has a v6 entry, so A must NODATA"
        );
    }

    #[test]
    fn test_process_dns_query_single_family_other_family_nodata_not_passthrough() {
        // A domain with only a v4 matcher must return NODATA (not pass-through) for AAAA, so a
        // client can't resolve a real v6 address and bypass the gateway.
        let filter = DnsGatewayFilter {
            config: Arc::new(config::DnsGateway {
                domains: vec![config::DomainMatcher {
                    domain: "*.single-test.com".to_string(),
                    base_ip: "10.141.0.0".to_string(),
                    prefix_len: 24,
                    metadata: HashMap::new(),
                }],
                fail_open: false,
            }),
        };

        let resp =
            filter.process_dns_query(&make_dns_query("api.single-test.com", RecordType::AAAA));
        assert!(
            resp.is_some(),
            "intercepted domain must return NODATA for the unconfigured family, not pass through"
        );
        assert_eq!(parse_response(&resp.unwrap()).answers().len(), 0);
    }

    #[test]
    fn test_process_dns_query_non_matched_domain() {
        let filter = DnsGatewayFilter {
            config: Arc::new(config::DnsGateway {
                domains: vec![config::DomainMatcher {
                    domain: "*.on-data-test.com".to_string(),
                    base_ip: "10.100.1.0".to_string(),
                    prefix_len: 24,
                    metadata: HashMap::new(),
                }],
                fail_open: false,
            }),
        };

        let query = make_dns_query("api.other-domain.com", RecordType::A);
        assert!(
            filter.process_dns_query(&query).is_none(),
            "expected None for non-matched domain (pass through)"
        );
    }

    #[test]
    fn test_process_dns_query_aaaa_matched_returns_nodata() {
        let filter = DnsGatewayFilter {
            config: Arc::new(config::DnsGateway {
                domains: vec![config::DomainMatcher {
                    domain: "*.on-data-test.com".to_string(),
                    base_ip: "10.100.2.0".to_string(),
                    prefix_len: 24,
                    metadata: HashMap::new(),
                }],
                fail_open: false,
            }),
        };

        let query = make_dns_query("api.on-data-test.com", RecordType::AAAA);
        let response = filter.process_dns_query(&query);
        assert!(
            response.is_some(),
            "expected NODATA response for AAAA on matched domain"
        );

        let msg = parse_response(&response.unwrap());
        assert_eq!(msg.message_type(), MessageType::Response);
        assert_eq!(msg.response_code(), ResponseCode::NoError);
        assert_eq!(
            msg.answers().len(),
            0,
            "NODATA response must have no answers"
        );
    }

    #[test]
    fn test_process_dns_query_exhausted_fail_open_false_returns_nodata() {
        // Use a /30 CIDR (4 IPs) to exhaust quickly
        let base_ip = "10.200.0.0";
        let base_addr: IpAddr = base_ip.parse().unwrap();

        for i in 0..4 {
            get_cache()
                .allocate(
                    format!("exhaust-{}.fail-closed-test.com", i),
                    HashMap::new(),
                    base_addr,
                    30,
                )
                .unwrap_or_else(|| panic!("failed to pre-allocate IP {}", i));
        }

        let filter = DnsGatewayFilter {
            config: Arc::new(config::DnsGateway {
                domains: vec![config::DomainMatcher {
                    domain: "*.fail-closed-test.com".to_string(),
                    base_ip: base_ip.to_string(),
                    prefix_len: 30,
                    metadata: HashMap::new(),
                }],
                fail_open: false,
            }),
        };

        let query = make_dns_query("new.fail-closed-test.com", RecordType::A);
        let response = filter.process_dns_query(&query);
        assert!(
            response.is_some(),
            "expected NODATA response when CIDR exhausted and fail_open=false"
        );
        assert_eq!(
            parse_response(&response.unwrap()).answers().len(),
            0,
            "exhausted CIDR with fail_open=false must return NODATA"
        );
    }

    #[test]
    fn test_process_dns_query_exhausted_fail_open_true_returns_none() {
        // Use a /30 CIDR (4 IPs) to exhaust quickly
        let base_ip = "10.201.0.0";
        let base_addr: IpAddr = base_ip.parse().unwrap();

        for i in 0..4 {
            get_cache()
                .allocate(
                    format!("exhaust-{}.fail-open-test.com", i),
                    HashMap::new(),
                    base_addr,
                    30,
                )
                .unwrap_or_else(|| panic!("failed to pre-allocate IP {}", i));
        }

        let filter = DnsGatewayFilter {
            config: Arc::new(config::DnsGateway {
                domains: vec![config::DomainMatcher {
                    domain: "*.fail-open-test.com".to_string(),
                    base_ip: base_ip.to_string(),
                    prefix_len: 30,
                    metadata: HashMap::new(),
                }],
                fail_open: true,
            }),
        };

        let query = make_dns_query("new.fail-open-test.com", RecordType::A);
        assert!(
            filter.process_dns_query(&query).is_none(),
            "expected None (pass through) when CIDR exhausted and fail_open=true"
        );
    }
}
