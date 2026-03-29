// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

use envoy_proxy_dynamic_modules_rust_sdk::*;
use ipnet::IpNet;
use serde::{Deserialize, Serialize};
use std::collections::HashSet;
use std::net::IpAddr;
use std::str::FromStr;
use std::sync::Arc;

// The raw filter config that will be deserialized from the JSON configuration.
// TODO: To support protobuf based API declaration in the future.
#[derive(Serialize, Deserialize, Debug)]
pub struct RawFilterConfig {
    #[serde(default)]
    deny_addresses: HashSet<String>,
    #[serde(default)]
    allow_addresses: HashSet<String>,
}

#[derive(Debug)]
pub struct FilterConfigImpl {
    // Exact IP addresses stored in a HashSet for O(1) lookup.
    deny_addresses_exact: HashSet<IpAddr>,
    allow_addresses_exact: HashSet<IpAddr>,
    // CIDR ranges stored in a Vec for sequential containment checks.
    deny_addresses_cidr: Vec<IpNet>,
    allow_addresses_cidr: Vec<IpNet>,
}

#[derive(Debug, Clone)]
pub struct FilterConfig {
    config: Arc<FilterConfigImpl>,
}

impl FilterConfig {
    pub fn new(filter_config: &str) -> Option<Self> {
        let filter_config: RawFilterConfig = match serde_json::from_str(filter_config) {
            Ok(cfg) => cfg,
            Err(err) => {
                eprintln!("Error parsing filter config: {err}");
                return None;
            }
        };

        // One and only one of deny_addresses and allow_addresses should be set.
        if filter_config.deny_addresses.is_empty() == filter_config.allow_addresses.is_empty() {
            eprintln!(
                "Error parsing filter config: one and only one of deny_addresses \
         and allow_addresses should be set"
            );
            return None;
        }

        let mut deny_addresses_exact = HashSet::new();
        let mut allow_addresses_exact = HashSet::new();
        let mut deny_addresses_cidr = Vec::new();
        let mut allow_addresses_cidr = Vec::new();

        for entry in &filter_config.allow_addresses {
            if let Ok(ip) = IpAddr::from_str(entry) {
                allow_addresses_exact.insert(ip);
            } else if let Ok(net) = IpNet::from_str(entry) {
                allow_addresses_cidr.push(net);
            } else {
                eprintln!("Error parsing IP or CIDR in allow_addresses: {entry}");
                return None;
            }
        }
        for entry in &filter_config.deny_addresses {
            if let Ok(ip) = IpAddr::from_str(entry) {
                deny_addresses_exact.insert(ip);
            } else if let Ok(net) = IpNet::from_str(entry) {
                deny_addresses_cidr.push(net);
            } else {
                eprintln!("Error parsing IP or CIDR in deny_addresses: {entry}");
                return None;
            }
        }

        Some(FilterConfig {
            config: Arc::new(FilterConfigImpl {
                deny_addresses_exact,
                allow_addresses_exact,
                deny_addresses_cidr,
                allow_addresses_cidr,
            }),
        })
    }
}

/// Returns true if `ip` matches any exact address or CIDR range in the given sets.
fn matches(ip: &IpAddr, exact: &HashSet<IpAddr>, cidrs: &[IpNet]) -> bool {
    exact.contains(ip) || cidrs.iter().any(|net| net.contains(ip))
}

impl<EHF: EnvoyHttpFilter> HttpFilterConfig<EHF> for FilterConfig {
    fn new_http_filter(&self, _envoy: &mut EHF) -> Box<dyn HttpFilter<EHF>> {
        Box::new(Filter {
            filter_config: self.clone(),
        })
    }
}

pub struct Filter {
    filter_config: FilterConfig,
}

impl<EHF: EnvoyHttpFilter> HttpFilter<EHF> for Filter {
    fn on_request_headers(
        &mut self,
        envoy_filter: &mut EHF,
        _end_stream: bool,
    ) -> abi::envoy_dynamic_module_type_on_http_filter_request_headers_status {
        let downstream_addr = envoy_filter
            .get_attribute_string(abi::envoy_dynamic_module_type_attribute_id::SourceAddress);
        let downstream_port =
            envoy_filter.get_attribute_int(abi::envoy_dynamic_module_type_attribute_id::SourcePort);

        if downstream_addr.is_none() || downstream_port.is_none() {
            envoy_filter.send_response(
                403,
                vec![],
                Some(b"No remote address and request is forbidden."),
                None,
            );
            return abi::envoy_dynamic_module_type_on_http_filter_request_headers_status::StopIteration;
        }

        let mut downstream_addr_str = String::new();

        let address_buffer = downstream_addr.unwrap();
        let downstream_addr_slice = address_buffer.as_slice();

        if let Some(port) = downstream_port {
            // Strip the port from the downstream addr.
            let downstream_addr_slice =
                &downstream_addr_slice[0..downstream_addr_slice.len() - port.to_string().len() - 1];

            unsafe {
                downstream_addr_str
                    .as_mut_vec()
                    .extend_from_slice(downstream_addr_slice);
            }
        } else {
            unsafe {
                downstream_addr_str
                    .as_mut_vec()
                    .extend_from_slice(downstream_addr_slice);
            }
        }

        // Strip IPv6 brackets if present (e.g. "[::1]" → "::1").
        let ip_str = if downstream_addr_str.starts_with('[') && downstream_addr_str.ends_with(']') {
            &downstream_addr_str[1..downstream_addr_str.len() - 1]
        } else {
            downstream_addr_str.as_str()
        };

        // Parse the downstream IP for CIDR matching.
        let downstream_ip = match IpAddr::from_str(ip_str) {
            Ok(ip) => ip,
            Err(_) => {
                envoy_filter.send_response(
                    403,
                    vec![],
                    Some(b"No remote address and request is forbidden."),
                    None,
                );
                return abi::envoy_dynamic_module_type_on_http_filter_request_headers_status::StopIteration;
            }
        };

        let cfg = &self.filter_config.config;

        // Check if the downstream addr is in the allowed list.
        if (!cfg.allow_addresses_exact.is_empty() || !cfg.allow_addresses_cidr.is_empty())
            && !matches(
                &downstream_ip,
                &cfg.allow_addresses_exact,
                &cfg.allow_addresses_cidr,
            )
        {
            envoy_filter.send_response(403, vec![], Some(b"Request is forbidden."), None);
            return abi::envoy_dynamic_module_type_on_http_filter_request_headers_status::StopIteration;
        }

        // Check if the downstream addr is in the denied list.
        if matches(
            &downstream_ip,
            &cfg.deny_addresses_exact,
            &cfg.deny_addresses_cidr,
        ) {
            envoy_filter.send_response(403, vec![], Some(b"Request is forbidden."), None);
            return abi::envoy_dynamic_module_type_on_http_filter_request_headers_status::StopIteration;
        }

        abi::envoy_dynamic_module_type_on_http_filter_request_headers_status::Continue
    }
}

fn init() -> bool {
    true
}

#[allow(dead_code)]
fn new_http_filter_config_fn<EC: EnvoyHttpFilterConfig, EHF: EnvoyHttpFilter>(
    _envoy_filter_config: &mut EC,
    filter_name: &str,
    filter_config: &[u8],
) -> Option<Box<dyn HttpFilterConfig<EHF>>> {
    let filter_config = std::str::from_utf8(filter_config).unwrap();
    match filter_name {
        "ip-restriction" => FilterConfig::new(filter_config)
            .map(|config| Box::new(config) as Box<dyn HttpFilterConfig<EHF>>),
        _ => panic!("Unknown filter name: {filter_name}"),
    }
}

declare_init_functions!(init, new_http_filter_config_fn);

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_new_filter_config_both_set() {
        let filter_config = FilterConfig::new(
            r#"{
        "allow_addresses": [
          "127.0.0.1",
          "::1"
        ],
        "deny_addresses": [
          "192.168.1.1"
        ]
      }"#,
        );
        assert!(filter_config.is_none());
    }

    #[test]
    fn test_new_filter_config_allowed_set() {
        let filter_config = FilterConfig::new(
            r#"{
        "allow_addresses": [
          "127.0.0.1",
          "::1"
        ]
      }"#,
        );
        assert!(filter_config.is_some());
    }

    #[test]
    fn test_new_filter_config_denied_set() {
        let filter_config = FilterConfig::new(
            r#"{
        "deny_addresses": [
          "192.168.1.1"
        ]
      }"#,
        );
        assert!(filter_config.is_some());
    }

    #[test]
    fn test_new_filter_config_invalid_ip() {
        let filter_config = FilterConfig::new(
            r#"{
        "allow_addresses": [
          "127.0.0.1",
          "invalid_ip"
        ]
      }"#,
        );
        assert!(filter_config.is_none());
    }

    #[test]
    fn test_new_filter_config_with_cidr() {
        let filter_config = FilterConfig::new(
            r#"{
        "allow_addresses": [
          "127.0.0.1",
          "192.168.0.0/24",
          "2001:db8::/32"
        ]
      }"#,
        );
        assert!(filter_config.is_some());
    }

    #[test]
    fn test_new_filter_config_invalid_cidr() {
        let filter_config = FilterConfig::new(
            r#"{
        "allow_addresses": [
          "127.0.0.1",
          "192.168.0.0/99"
        ]
      }"#,
        );
        assert!(filter_config.is_none());
    }

    #[test]
    fn test_filter_denied_because_no_address() {
        let filter_config = FilterConfig::new(
            r#"{
        "deny_addresses": [
          "192.168.1.1"
        ]
      }"#,
        );
        assert!(filter_config.is_some());

        let mut filter = Filter {
            filter_config: filter_config.unwrap(),
        };

        let mut mock_envoy_filter =
            envoy_proxy_dynamic_modules_rust_sdk::MockEnvoyHttpFilter::new();

        mock_envoy_filter
            .expect_get_attribute_string()
            .times(1)
            .returning(|_| None);
        mock_envoy_filter
            .expect_get_attribute_int()
            .times(1)
            .returning(|_| None);
        mock_envoy_filter
            .expect_send_response()
            .times(1)
            .returning(|code, _, _, _| assert!(code == 403));

        assert_eq!(
            filter.on_request_headers(&mut mock_envoy_filter, true),
            abi::envoy_dynamic_module_type_on_http_filter_request_headers_status::StopIteration
        );
    }

    #[test]
    fn test_filter_with_allowed_list() {
        let filter_config = FilterConfig::new(
            r#"{
        "allow_addresses": [
          "127.0.0.1",
          "::1"
        ]
      }"#,
        );
        assert!(filter_config.is_some());

        let mut filter = Filter {
            filter_config: filter_config.unwrap(),
        };

        let mut mock_envoy_filter =
            envoy_proxy_dynamic_modules_rust_sdk::MockEnvoyHttpFilter::new();

        mock_envoy_filter
            .expect_get_attribute_string()
            .times(1)
            .returning(|_| Some(EnvoyBuffer::new("127.0.0.1:80")));
        mock_envoy_filter
            .expect_get_attribute_int()
            .times(1)
            .returning(|_| Some(80));

        assert_eq!(
            filter.on_request_headers(&mut mock_envoy_filter, true),
            abi::envoy_dynamic_module_type_on_http_filter_request_headers_status::Continue
        );

        mock_envoy_filter
            .expect_get_attribute_string()
            .times(1)
            .returning(|_| Some(EnvoyBuffer::new("192.168.1.1:80")));
        mock_envoy_filter
            .expect_get_attribute_int()
            .times(1)
            .returning(|_| Some(80));
        mock_envoy_filter
            .expect_send_response()
            .times(1)
            .returning(|code, _, _, _| assert!(code == 403));

        assert_eq!(
            filter.on_request_headers(&mut mock_envoy_filter, true),
            abi::envoy_dynamic_module_type_on_http_filter_request_headers_status::StopIteration
        );
    }

    #[test]
    fn test_filter_with_denied_list() {
        let filter_config = FilterConfig::new(
            r#"{
        "deny_addresses": [
          "192.168.1.1"
        ]
      }"#,
        );
        assert!(filter_config.is_some());

        let mut filter = Filter {
            filter_config: filter_config.unwrap(),
        };

        let mut mock_envoy_filter =
            envoy_proxy_dynamic_modules_rust_sdk::MockEnvoyHttpFilter::new();

        mock_envoy_filter
            .expect_get_attribute_string()
            .times(1)
            .returning(|_| Some(EnvoyBuffer::new("192.168.1.1:80")));
        mock_envoy_filter
            .expect_get_attribute_int()
            .times(1)
            .returning(|_| Some(80));
        mock_envoy_filter
            .expect_send_response()
            .times(1)
            .returning(|code, _, _, _| assert!(code == 403));

        assert_eq!(
            filter.on_request_headers(&mut mock_envoy_filter, true),
            abi::envoy_dynamic_module_type_on_http_filter_request_headers_status::StopIteration
        );

        mock_envoy_filter
            .expect_get_attribute_string()
            .times(1)
            .returning(|_| Some(EnvoyBuffer::new("127.0.0.1:80")));
        mock_envoy_filter
            .expect_get_attribute_int()
            .times(1)
            .returning(|_| Some(80));

        assert_eq!(
            filter.on_request_headers(&mut mock_envoy_filter, true),
            abi::envoy_dynamic_module_type_on_http_filter_request_headers_status::Continue
        );
    }

    #[test]
    fn test_filter_with_allowed_cidr() {
        let filter_config = FilterConfig::new(
            r#"{
        "allow_addresses": [
          "192.168.1.0/24"
        ]
      }"#,
        );
        assert!(filter_config.is_some());

        let mut filter = Filter {
            filter_config: filter_config.unwrap(),
        };

        // IP within the CIDR range should be allowed.
        let mut mock_envoy_filter =
            envoy_proxy_dynamic_modules_rust_sdk::MockEnvoyHttpFilter::new();
        mock_envoy_filter
            .expect_get_attribute_string()
            .times(1)
            .returning(|_| Some(EnvoyBuffer::new("192.168.1.100:8080")));
        mock_envoy_filter
            .expect_get_attribute_int()
            .times(1)
            .returning(|_| Some(8080));
        assert_eq!(
            filter.on_request_headers(&mut mock_envoy_filter, true),
            abi::envoy_dynamic_module_type_on_http_filter_request_headers_status::Continue
        );

        // IP outside the CIDR range should be blocked.
        mock_envoy_filter
            .expect_get_attribute_string()
            .times(1)
            .returning(|_| Some(EnvoyBuffer::new("192.168.2.1:8080")));
        mock_envoy_filter
            .expect_get_attribute_int()
            .times(1)
            .returning(|_| Some(8080));
        mock_envoy_filter
            .expect_send_response()
            .times(1)
            .returning(|code, _, _, _| assert!(code == 403));
        assert_eq!(
            filter.on_request_headers(&mut mock_envoy_filter, true),
            abi::envoy_dynamic_module_type_on_http_filter_request_headers_status::StopIteration
        );
    }

    #[test]
    fn test_filter_with_denied_cidr() {
        let filter_config = FilterConfig::new(
            r#"{
        "deny_addresses": [
          "10.0.0.0/8"
        ]
      }"#,
        );
        assert!(filter_config.is_some());

        let mut filter = Filter {
            filter_config: filter_config.unwrap(),
        };

        // IP within the denied CIDR range should be blocked.
        let mut mock_envoy_filter =
            envoy_proxy_dynamic_modules_rust_sdk::MockEnvoyHttpFilter::new();
        mock_envoy_filter
            .expect_get_attribute_string()
            .times(1)
            .returning(|_| Some(EnvoyBuffer::new("10.42.1.5:12345")));
        mock_envoy_filter
            .expect_get_attribute_int()
            .times(1)
            .returning(|_| Some(12345));
        mock_envoy_filter
            .expect_send_response()
            .times(1)
            .returning(|code, _, _, _| assert!(code == 403));
        assert_eq!(
            filter.on_request_headers(&mut mock_envoy_filter, true),
            abi::envoy_dynamic_module_type_on_http_filter_request_headers_status::StopIteration
        );

        // IP outside the denied CIDR range should be allowed.
        mock_envoy_filter
            .expect_get_attribute_string()
            .times(1)
            .returning(|_| Some(EnvoyBuffer::new("192.168.1.1:12345")));
        mock_envoy_filter
            .expect_get_attribute_int()
            .times(1)
            .returning(|_| Some(12345));
        assert_eq!(
            filter.on_request_headers(&mut mock_envoy_filter, true),
            abi::envoy_dynamic_module_type_on_http_filter_request_headers_status::Continue
        );
    }

    #[test]
    fn test_filter_with_mixed_exact_and_cidr() {
        let filter_config = FilterConfig::new(
            r#"{
        "allow_addresses": [
          "127.0.0.1",
          "10.0.0.0/8"
        ]
      }"#,
        );
        assert!(filter_config.is_some());

        let mut filter = Filter {
            filter_config: filter_config.unwrap(),
        };

        // Exact IP match.
        let mut mock_envoy_filter =
            envoy_proxy_dynamic_modules_rust_sdk::MockEnvoyHttpFilter::new();
        mock_envoy_filter
            .expect_get_attribute_string()
            .times(1)
            .returning(|_| Some(EnvoyBuffer::new("127.0.0.1:9000")));
        mock_envoy_filter
            .expect_get_attribute_int()
            .times(1)
            .returning(|_| Some(9000));
        assert_eq!(
            filter.on_request_headers(&mut mock_envoy_filter, true),
            abi::envoy_dynamic_module_type_on_http_filter_request_headers_status::Continue
        );

        // IP within CIDR range.
        mock_envoy_filter
            .expect_get_attribute_string()
            .times(1)
            .returning(|_| Some(EnvoyBuffer::new("10.100.200.1:9000")));
        mock_envoy_filter
            .expect_get_attribute_int()
            .times(1)
            .returning(|_| Some(9000));
        assert_eq!(
            filter.on_request_headers(&mut mock_envoy_filter, true),
            abi::envoy_dynamic_module_type_on_http_filter_request_headers_status::Continue
        );

        // IP not in exact or CIDR.
        mock_envoy_filter
            .expect_get_attribute_string()
            .times(1)
            .returning(|_| Some(EnvoyBuffer::new("192.168.1.1:9000")));
        mock_envoy_filter
            .expect_get_attribute_int()
            .times(1)
            .returning(|_| Some(9000));
        mock_envoy_filter
            .expect_send_response()
            .times(1)
            .returning(|code, _, _, _| assert!(code == 403));
        assert_eq!(
            filter.on_request_headers(&mut mock_envoy_filter, true),
            abi::envoy_dynamic_module_type_on_http_filter_request_headers_status::StopIteration
        );
    }

    #[test]
    fn test_filter_with_ipv6_cidr() {
        let filter_config = FilterConfig::new(
            r#"{
        "allow_addresses": [
          "2001:db8::/32"
        ]
      }"#,
        );
        assert!(filter_config.is_some());

        let mut filter = Filter {
            filter_config: filter_config.unwrap(),
        };

        // IPv6 address within the CIDR range should be allowed.
        let mut mock_envoy_filter =
            envoy_proxy_dynamic_modules_rust_sdk::MockEnvoyHttpFilter::new();
        mock_envoy_filter
            .expect_get_attribute_string()
            .times(1)
            .returning(|_| Some(EnvoyBuffer::new("[2001:db8::1]:80")));
        mock_envoy_filter
            .expect_get_attribute_int()
            .times(1)
            .returning(|_| Some(80));
        assert_eq!(
            filter.on_request_headers(&mut mock_envoy_filter, true),
            abi::envoy_dynamic_module_type_on_http_filter_request_headers_status::Continue
        );

        // IPv6 address outside the CIDR range should be blocked.
        mock_envoy_filter
            .expect_get_attribute_string()
            .times(1)
            .returning(|_| Some(EnvoyBuffer::new("[2001:db9::1]:80")));
        mock_envoy_filter
            .expect_get_attribute_int()
            .times(1)
            .returning(|_| Some(80));
        mock_envoy_filter
            .expect_send_response()
            .times(1)
            .returning(|code, _, _, _| assert!(code == 403));
        assert_eq!(
            filter.on_request_headers(&mut mock_envoy_filter, true),
            abi::envoy_dynamic_module_type_on_http_filter_request_headers_status::StopIteration
        );
    }

    #[test]
    fn test_config_schema_valid_full() {
        let schema_str = include_str!("../config.schema.json");
        let schema: serde_json::Value = serde_json::from_str(schema_str).unwrap();
        let compiled = jsonschema::validator_for(&schema).unwrap();

        let valid_allow: serde_json::Value = serde_json::from_str(
            r#"{
                "allow_addresses": ["127.0.0.1", "::1", "10.0.0.0/8", "2001:db8::/32"]
            }"#,
        )
        .unwrap();
        assert!(compiled.is_valid(&valid_allow));

        let valid_deny: serde_json::Value = serde_json::from_str(
            r#"{
                "deny_addresses": ["192.168.1.50", "10.0.0.100"]
            }"#,
        )
        .unwrap();
        assert!(compiled.is_valid(&valid_deny));
    }

    #[test]
    fn test_config_schema_empty() {
        let schema_str = include_str!("../config.schema.json");
        let schema: serde_json::Value = serde_json::from_str(schema_str).unwrap();
        let compiled = jsonschema::validator_for(&schema).unwrap();

        let empty: serde_json::Value = serde_json::from_str(r#"{}"#).unwrap();
        assert!(!compiled.is_valid(&empty));
    }

    #[test]
    fn test_config_schema_invalid() {
        let schema_str = include_str!("../config.schema.json");
        let schema: serde_json::Value = serde_json::from_str(schema_str).unwrap();
        let compiled = jsonschema::validator_for(&schema).unwrap();

        // Both set is invalid.
        let both: serde_json::Value = serde_json::from_str(
            r#"{
                "allow_addresses": ["127.0.0.1"],
                "deny_addresses": ["192.168.1.1"]
            }"#,
        )
        .unwrap();
        assert!(!compiled.is_valid(&both));

        // Unknown field is invalid.
        let unknown: serde_json::Value = serde_json::from_str(
            r#"{
                "allow_addresses": ["127.0.0.1"],
                "unknown": true
            }"#,
        )
        .unwrap();
        assert!(!compiled.is_valid(&unknown));
    }
}
