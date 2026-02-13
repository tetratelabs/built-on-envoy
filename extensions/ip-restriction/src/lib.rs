// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

use envoy_proxy_dynamic_modules_rust_sdk::*;
use serde::{Deserialize, Serialize};
use std::collections::HashSet;
use std::net::Ipv4Addr;
use std::net::Ipv6Addr;
use std::str::FromStr;
use std::sync::Arc;

// The raw filter config that will be deserialized from the JSON configuration.
// TODO: To support protobuf based API declaration in the future.
// TODO: to support ip range in the future.
#[derive(Serialize, Deserialize, Debug)]
pub struct RawFilterConfig {
    #[serde(default)]
    deny_addresses: HashSet<String>,
    #[serde(default)]
    allow_addresses: HashSet<String>,
}

#[derive(Debug)]
pub struct FilterConfigImpl {
    deny_addresses_exact: HashSet<String>,
    allow_addresses_exact: HashSet<String>,
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
                "Error parsing filter config: one and only one of deny_addresses\
         and allow_addresses should be set"
            );
            return None;
        }

        let mut deny_addresses_exact = HashSet::new();
        let mut allow_addresses_exact = HashSet::new();

        // Validate every ip in the set is a valid IPv4 address or IPv6 address.
        for ip in &filter_config.allow_addresses {
            if Ipv4Addr::from_str(ip).is_err() && Ipv6Addr::from_str(ip).is_err() {
                eprintln!("Error parsing ip in allow_addresses: {ip}");
                return None;
            }
            allow_addresses_exact.insert(ip.clone());
        }
        for ip in &filter_config.deny_addresses {
            if Ipv4Addr::from_str(ip).is_err() && Ipv6Addr::from_str(ip).is_err() {
                eprintln!("Error parsing ip in deny_addresses: {ip}");
                return None;
            }
            deny_addresses_exact.insert(ip.clone());
        }

        Some(FilterConfig {
            config: Arc::new(FilterConfigImpl {
                deny_addresses_exact,
                allow_addresses_exact,
            }),
        })
    }
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

        // Check if the downstream addr is in the allowed list.
        if !self.filter_config.config.allow_addresses_exact.is_empty()
            && !self
                .filter_config
                .config
                .allow_addresses_exact
                .contains(&downstream_addr_str)
        {
            envoy_filter.send_response(403, vec![], Some(b"Request is forbidden."), None);
            return abi::envoy_dynamic_module_type_on_http_filter_request_headers_status::StopIteration;
        }

        // Check if the downstream addr is in the denied list.
        if !self.filter_config.config.deny_addresses_exact.is_empty()
            && self
                .filter_config
                .config
                .deny_addresses_exact
                .contains(&downstream_addr_str)
        {
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
}
