// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

//! A cache lookup filter that should be used in conjunction with the DNS gateway filter.
//!
//! The filter looks up the destination virtual IP in the shared cache and sets filter state
//! with the matched domain and metadata for downstream filters.

use envoy_proxy_dynamic_modules_rust_sdk::*;
use serde::Deserialize;
use std::net::Ipv4Addr;

use crate::virtual_ip_cache::get_cache;

const DEFAULT_FILTER_STATE_PREFIX: &str = "io.builtonenvoy.dns_gateway";

#[derive(Deserialize, Default)]
struct CacheLookupConfig {
    filter_state_prefix: Option<String>,
    #[serde(default)]
    domain_metadata: Option<DomainMetadata>,
}

/// Optional publication of the matched domain as Envoy **connection dynamic metadata**.
///
/// Filter state (which the cache-lookup filter always writes) is only reachable by other
/// filters that read filter state directly. Some consumers instead read *dynamic metadata* —
/// for example a network `ext_authz` filter with `metadata_context_namespaces` forwards the
/// named namespace to the authorization service. When configured, the resolved domain is
/// published as a string under `namespace`/`key` so those consumers can see it.
#[derive(Deserialize, Clone)]
struct DomainMetadata {
    /// Dynamic-metadata namespace to write under (e.g. `io.builtonenvoy.dns_gateway`).
    namespace: String,
    /// Key within the namespace whose value is the resolved domain (e.g. `domain`).
    key: String,
}

/// The filter configuration that implements
/// [`envoy_proxy_dynamic_modules_rust_sdk::NetworkFilterConfig`].
pub struct CacheLookupFilterConfig {
    filter_state_prefix: String,
    domain_metadata: Option<DomainMetadata>,
}

impl CacheLookupFilterConfig {
    /// Creates a new cache lookup filter configuration.
    pub fn new(config: &[u8]) -> Self {
        let parsed = serde_json::from_slice::<CacheLookupConfig>(config).unwrap_or_default();
        let prefix = parsed
            .filter_state_prefix
            .unwrap_or_else(|| DEFAULT_FILTER_STATE_PREFIX.to_string());
        envoy_log_info!("Filter initialized");
        CacheLookupFilterConfig {
            filter_state_prefix: prefix,
            domain_metadata: parsed.domain_metadata,
        }
    }
}

impl<ENF: EnvoyNetworkFilter> NetworkFilterConfig<ENF> for CacheLookupFilterConfig {
    fn new_network_filter(&self, _envoy: &mut ENF) -> Box<dyn NetworkFilter<ENF>> {
        Box::new(CacheLookupFilter {
            filter_state_prefix: self.filter_state_prefix.clone(),
            domain_metadata: self.domain_metadata.clone(),
        })
    }
}

/// The cache lookup filter that implements
/// [`envoy_proxy_dynamic_modules_rust_sdk::NetworkFilter`].
///
/// Looks up the destination virtual IP in the shared cache and sets filter state
/// with the matched domain and metadata.
struct CacheLookupFilter {
    filter_state_prefix: String,
    domain_metadata: Option<DomainMetadata>,
}

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

        let domain_key = format!("{}.domain", self.filter_state_prefix);
        envoy_filter.set_filter_state_bytes(domain_key.as_bytes(), domain.as_bytes());
        for (key, value) in &metadata {
            let meta_key = format!("{}.metadata.{}", self.filter_state_prefix, key);
            envoy_filter.set_filter_state_bytes(meta_key.as_bytes(), value.as_bytes());
        }

        // If configured, also publish the resolved domain as connection dynamic metadata so
        // consumers that read dynamic metadata (rather than filter state) can see it.
        if let Some(dm) = &self.domain_metadata {
            envoy_filter.set_dynamic_metadata_string(&dm.namespace, &dm.key, &domain);
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
    fn test_config_default_prefix() {
        let config = CacheLookupFilterConfig::new(b"{}");
        assert_eq!(config.filter_state_prefix, DEFAULT_FILTER_STATE_PREFIX);
    }

    #[test]
    fn test_config_custom_prefix() {
        let config =
            CacheLookupFilterConfig::new(br#"{"filter_state_prefix": "my.custom.prefix"}"#);
        assert_eq!(config.filter_state_prefix, "my.custom.prefix");
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
        mock.expect_set_filter_state_bytes()
            .withf(|key, value| {
                key == b"io.builtonenvoy.dns_gateway.domain"
                    && value == b"cache-hit-test.example.com"
            })
            .times(1)
            .returning(|_, _| true);
        mock.expect_set_filter_state_bytes()
            .withf(|key, value| {
                key == b"io.builtonenvoy.dns_gateway.metadata.cluster" && value == b"test_cluster"
            })
            .times(1)
            .returning(|_, _| true);

        let status = filter.on_new_connection(&mut mock);
        assert_eq!(
            status,
            abi::envoy_dynamic_module_type_on_network_filter_data_status::Continue
        );
    }

    #[test]
    fn test_on_new_connection_custom_prefix() {
        let mut metadata = HashMap::new();
        metadata.insert("env".to_string(), "prod".to_string());
        let ip = get_cache()
            .allocate(
                "custom-prefix-test.example.com".into(),
                metadata,
                0x0C0C_0000, // 12.12.0.0
                24,
            )
            .unwrap();

        let config =
            CacheLookupFilterConfig::new(br#"{"filter_state_prefix": "my.custom.prefix"}"#);
        let mut mock = MockEnvoyNetworkFilter::new();
        let mut filter = config.new_network_filter(&mut mock);

        let mut mock = MockEnvoyNetworkFilter::new();
        mock.expect_get_local_address()
            .returning(move || (ip.to_string(), 8080));
        mock.expect_set_filter_state_bytes()
            .withf(|key, value| {
                key == b"my.custom.prefix.domain" && value == b"custom-prefix-test.example.com"
            })
            .times(1)
            .returning(|_, _| true);
        mock.expect_set_filter_state_bytes()
            .withf(|key, value| key == b"my.custom.prefix.metadata.env" && value == b"prod")
            .times(1)
            .returning(|_, _| true);

        let status = filter.on_new_connection(&mut mock);
        assert_eq!(
            status,
            abi::envoy_dynamic_module_type_on_network_filter_data_status::Continue
        );
    }

    #[test]
    fn test_on_new_connection_publishes_dynamic_metadata() {
        let ip = get_cache()
            .allocate(
                "dyn-meta-test.example.com".into(),
                HashMap::new(),
                0x0D0D_0000, // 13.13.0.0
                24,
            )
            .unwrap();

        let config = CacheLookupFilterConfig::new(
            br#"{"domain_metadata": {"namespace": "com.example.egress", "key": "fqdn"}}"#,
        );
        let mut mock = MockEnvoyNetworkFilter::new();
        let mut filter = config.new_network_filter(&mut mock);

        let mut mock = MockEnvoyNetworkFilter::new();
        mock.expect_get_local_address()
            .returning(move || (ip.to_string(), 8080));
        mock.expect_set_filter_state_bytes().returning(|_, _| true);
        mock.expect_set_dynamic_metadata_string()
            .withf(|ns, key, value| {
                ns == "com.example.egress" && key == "fqdn" && value == "dyn-meta-test.example.com"
            })
            .times(1)
            .returning(|_, _, _| ());

        let status = filter.on_new_connection(&mut mock);
        assert_eq!(
            status,
            abi::envoy_dynamic_module_type_on_network_filter_data_status::Continue
        );
    }

    #[test]
    fn test_on_new_connection_no_dynamic_metadata_by_default() {
        // Without domain_metadata configured, set_dynamic_metadata_string is never called.
        // (mockall fails the test if an unexpected call is made.)
        let ip = get_cache()
            .allocate(
                "no-dyn-meta-test.example.com".into(),
                HashMap::new(),
                0x0E0E_0000, // 14.14.0.0
                24,
            )
            .unwrap();

        let config = CacheLookupFilterConfig::new(b"{}");
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
