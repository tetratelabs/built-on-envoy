// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

//! A DNS gateway filter that intercepts DNS queries and returns virtual IPs for matched domains.
//!
//! This filter implements:
//! 1. UDP listener filter structure with `UdpListenerFilterConfig` and `UdpListenerFilter` traits.
//! 2. DNS query parsing and response.

pub mod cache_lookup;
mod config;
mod virtual_ip_cache;

use envoy_proxy_dynamic_modules_rust_sdk::*;
use hickory_proto::op::{Message, MessageType, ResponseCode};
use hickory_proto::rr::{Name, RData, Record, RecordType};
use hickory_proto::serialize::binary::{BinDecodable, BinDecoder};
use std::net::Ipv4Addr;
use std::sync::Arc;
use virtual_ip_cache::get_cache;

/// Matches a domain against a pattern.
/// Supports exact matches and wildcard patterns like "*.aws.com".
/// Wildcard patterns match exactly one subdomain level, e.g. "*.aws.com"
/// matches "api.aws.com" but not "sub.api.aws.com".
fn matches_domain(pattern: &str, domain: &str) -> bool {
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
    /// The config arrives as a JSON-serialized google.protobuf.Struct
    /// wrapped in an Any: `{"@type":"...Struct", "value":{"domains":[...]}}`.
    pub fn new(config: &[u8]) -> Option<Self> {
        let gateway_config: config::DnsGateway = std::str::from_utf8(config)
            .map_err(|e| eprintln!("Invalid UTF-8: {e}"))
            .and_then(|s| {
                serde_json::from_str::<serde_json::Value>(s)
                    .map_err(|e| eprintln!("Invalid JSON: {e}"))
            })
            .and_then(|v| {
                serde_json::from_value(v["value"].clone())
                    .map_err(|e| eprintln!("Invalid config: {e}"))
            })
            .ok()?;

        for d in &gateway_config.domains {
            let name = Name::from_utf8(&d.domain)
                .map_err(|e| eprintln!("Invalid domain '{}': {e}", d.domain))
                .ok()?;
            if name.is_wildcard() && name.num_labels() < 2 {
                eprintln!(
                    "Bare wildcard '{}' is not allowed, use '*.example.com' instead",
                    d.domain
                );
                return None;
            }

            d.base_ip
                .parse::<Ipv4Addr>()
                .map_err(|e| eprintln!("Invalid base_ip '{}' for '{}': {e}", d.base_ip, d.domain))
                .ok()?;

            if !(1..=32).contains(&d.prefix_len) {
                eprintln!("Invalid prefix_len {} for '{}'", d.prefix_len, d.domain);
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

        let response = (|| -> Option<Vec<u8>> {
            let query = Message::read(&mut BinDecoder::new(&data))
                .map_err(|e| envoy_log_warn!("Failed to parse DNS query: {}", e))
                .ok()?;
            if query.message_type() != MessageType::Query {
                return None;
            }

            let question = query.queries().first()?;
            // DNS names are fully qualified with a trailing dot (e.g. "api.aws.com.").
            // Strip it so our wildcard patterns like "*.aws.com" match correctly.
            let domain = question.name().to_utf8().trim_end_matches('.').to_string();

            let matcher = self
                .config
                .domains
                .iter()
                .find(|m| matches_domain(&m.domain, &domain))?;

            match question.query_type() {
                RecordType::A => {
                    let base_ip: u32 = matcher.base_ip.parse::<Ipv4Addr>().ok()?.into();
                    let ip = get_cache().allocate(
                        domain,
                        matcher.metadata.clone(),
                        base_ip,
                        matcher.prefix_len as u8,
                    )?;
                    build_dns_response(&query, question.name(), ip).ok()
                }
                _ => build_nodata_response(&query).ok(),
            }
        })();

        let Some(response) = response else {
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
    ip: Ipv4Addr,
) -> Result<Vec<u8>, Box<dyn std::error::Error>> {
    let mut response = query_message.clone();

    response.set_message_type(MessageType::Response);
    response.set_response_code(ResponseCode::NoError);
    response.set_recursion_available(true);
    response.set_authoritative(true);

    let record = Record::from_rdata(name.clone(), 600, RData::A(ip.into()));

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
    fn test_domain_matcher_exact() {
        assert!(matches_domain("api.example.com", "api.example.com"));

        assert!(!matches_domain("api.example.com", "www.api.example.com"));
        assert!(!matches_domain("api.example.com", "example.com"));
        assert!(!matches_domain("api.example.com", "api.example.org"));
    }

    #[test]
    fn test_config_parsing_valid_struct() {
        let config = r#"{
            "@type": "type.googleapis.com/google.protobuf.Struct",
            "value": {
                "domains": [
                    {
                        "domain": "*.aws.com",
                        "base_ip": "10.239.0.0",
                        "prefix_len": 24,
                        "metadata": {
                            "cluster": "aws_cluster",
                            "region": "us-east-1"
                        }
                    }
                ]
            }
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
            "@type": "type.googleapis.com/google.protobuf.Struct",
            "value": {
                "domains": [
                    {"domain": "*.aws.com", "base_ip": "10.239.0.0", "prefix_len": 24, "metadata": {"cluster": "aws"}},
                    {"domain": "*.google.com", "base_ip": "10.239.1.0", "prefix_len": 24, "metadata": {"cluster": "google"}},
                    {"domain": "exact.example.com", "base_ip": "10.239.2.0", "prefix_len": 24, "metadata": {"cluster": "exact"}}
                ]
            }
        }"#;

        let config = DnsGatewayFilterConfig::new(config.as_bytes()).unwrap();
        assert_eq!(config.config.domains.len(), 3);
    }

    #[test]
    fn test_config_parsing_missing_base_ip() {
        let config = r#"{
            "value": {
                "domains": [
                    {"domain": "*.aws.com", "prefix_len": 24, "metadata": {}}
                ]
            }
        }"#;

        assert!(DnsGatewayFilterConfig::new(config.as_bytes()).is_none());
    }

    #[test]
    fn test_config_parsing_missing_prefix_len() {
        // serde defaults missing uint32 to 0, which fails the 1..=32 range check.
        let config = r#"{
            "value": {
                "domains": [
                    {"domain": "*.aws.com", "base_ip": "10.10.0.0", "metadata": {}}
                ]
            }
        }"#;

        assert!(DnsGatewayFilterConfig::new(config.as_bytes()).is_none());
    }

    #[test]
    fn test_config_parsing_invalid_prefix_len() {
        let config = r#"{
            "value": {
                "domains": [
                    {"domain": "*.aws.com", "base_ip": "10.10.0.0", "prefix_len": 33, "metadata": {}}
                ]
            }
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
            "value": {
                "domains": [
                    {"domain": "*.aws.com", "base_ip": "10.10.0.0", "prefix_len": 24, "metadata": {"count": 42}}
                ]
            }
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
        let ip = Ipv4Addr::new(10, 10, 0, 1);

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
    fn test_config_parsing_rejects_bare_wildcard() {
        let config = r#"{
            "value": {
                "domains": [
                    {"domain": "*", "base_ip": "10.239.0.0", "prefix_len": 24, "metadata": {}}
                ]
            }
        }"#;

        assert!(DnsGatewayFilterConfig::new(config.as_bytes()).is_none());
    }
}
