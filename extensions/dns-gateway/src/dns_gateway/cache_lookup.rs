// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

//! A cache lookup filter that should be used in conjunction with the DNS gateway filter.
//!
//! The filter looks up the destination virtual IP in the shared cache and sets filter state
//! with the matched domain and metadata for downstream filters.

use envoy_proxy_dynamic_modules_rust_sdk::*;
use std::net::Ipv4Addr;

use super::virtual_ip_cache::get_cache;

/// The filter configuration that implements
/// [`envoy_proxy_dynamic_modules_rust_sdk::NetworkFilterConfig`].
pub struct CacheLookupFilterConfig;

impl CacheLookupFilterConfig {
    /// Creates a new cache lookup filter configuration.
    pub fn new(_config: &[u8]) -> Self {
        envoy_log_info!("Filter initialized");
        CacheLookupFilterConfig
    }
}

impl<ENF: EnvoyNetworkFilter> NetworkFilterConfig<ENF> for CacheLookupFilterConfig {
    fn new_network_filter(&self, _envoy: &mut ENF) -> Box<dyn NetworkFilter<ENF>> {
        Box::new(CacheLookupFilter)
    }
}

/// The cache lookup filter that implements
/// [`envoy_proxy_dynamic_modules_rust_sdk::NetworkFilter`].
///
/// Looks up the destination virtual IP in the shared cache and sets filter state
/// with the matched domain and metadata.
struct CacheLookupFilter;

impl<ENF: EnvoyNetworkFilter> NetworkFilter<ENF> for CacheLookupFilter {
    fn on_new_connection(
        &mut self,
        envoy_filter: &mut ENF,
    ) -> abi::envoy_dynamic_module_type_on_network_filter_data_status {
        let (ip_str, port) = envoy_filter.get_local_address();
        envoy_log_debug!("New connection, local_address={}:{}", ip_str, port);

        let ip: Ipv4Addr = match ip_str.parse() {
            Ok(ip) => ip,
            Err(_) => {
                envoy_log_warn!("Failed to parse destination IP: {}", ip_str);
                return abi::envoy_dynamic_module_type_on_network_filter_data_status::Continue;
            }
        };

        let (domain, metadata) = match get_cache().lookup(ip) {
            Some(d) => d,
            None => {
                envoy_log_warn!("No destination found for virtual IP: {} (cache miss)", ip);
                return abi::envoy_dynamic_module_type_on_network_filter_data_status::Continue;
            }
        };

        envoy_filter.set_filter_state_bytes(b"envoy.dns_gateway.domain", domain.as_bytes());
        for (key, value) in &metadata {
            envoy_filter.set_filter_state_bytes(
                format!("envoy.dns_gateway.metadata.{}", key).as_bytes(),
                value.as_bytes(),
            );
        }

        abi::envoy_dynamic_module_type_on_network_filter_data_status::Continue
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::HashMap;

    #[test]
    fn test_config_creation() {
        let _config = CacheLookupFilterConfig::new(b"");
    }

    #[test]
    fn test_new_network_filter() {
        let config = CacheLookupFilterConfig::new(b"");
        let mut mock = MockEnvoyNetworkFilter::new();
        let _filter = config.new_network_filter(&mut mock);
    }

    #[test]
    fn test_on_new_connection_cache_hit() {
        let mut metadata = HashMap::new();
        metadata.insert("cluster".to_string(), "test_cluster".to_string());
        let ip = get_cache()
            .allocate(
                "cache-hit-test.example.com".into(),
                metadata,
                0x0B0B_0000, // 11.11.0.0
                24,
            )
            .unwrap();

        let config = CacheLookupFilterConfig::new(b"");
        let mut mock = MockEnvoyNetworkFilter::new();
        let mut filter = config.new_network_filter(&mut mock);

        let mut mock = MockEnvoyNetworkFilter::new();
        mock.expect_get_local_address()
            .returning(move || (ip.to_string(), 8080));
        mock.expect_set_filter_state_bytes().returning(|_, _| true);

        let status = filter.on_new_connection(&mut mock);
        assert_eq!(
            status,
            abi::envoy_dynamic_module_type_on_network_filter_data_status::Continue
        );
    }

    #[test]
    fn test_on_new_connection_invalid_ip() {
        let config = CacheLookupFilterConfig::new(b"");
        let mut mock = MockEnvoyNetworkFilter::new();
        let mut filter = config.new_network_filter(&mut mock);

        let mut mock = MockEnvoyNetworkFilter::new();
        mock.expect_get_local_address()
            .returning(|| ("not-an-ip".to_string(), 8080));

        let status = filter.on_new_connection(&mut mock);
        assert_eq!(
            status,
            abi::envoy_dynamic_module_type_on_network_filter_data_status::Continue
        );
    }

    #[test]
    fn test_on_new_connection_cache_miss() {
        let config = CacheLookupFilterConfig::new(b"");
        let mut mock = MockEnvoyNetworkFilter::new();
        let mut filter = config.new_network_filter(&mut mock);

        let mut mock = MockEnvoyNetworkFilter::new();
        mock.expect_get_local_address()
            .returning(|| ("192.168.99.99".to_string(), 8080));

        let status = filter.on_new_connection(&mut mock);
        assert_eq!(
            status,
            abi::envoy_dynamic_module_type_on_network_filter_data_status::Continue
        );
    }
}
