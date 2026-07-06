// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// TODO: Remove this once the SDK dependency is updated to a version that contains
//       this fix: https://github.com/envoyproxy/envoy/pull/44654
#![allow(unpredictable_function_pointer_comparisons)]

use envoy_proxy_dynamic_modules_rust_sdk::*;

mod cache_lookup;
mod config;
mod dns_gateway;
mod virtual_ip_cache;

declare_all_init_functions!(init,
    network: new_network_filter_config_fn,
    udp_listener: new_udp_listener_filter_config_fn
);

fn init() -> bool {
    true
}

fn new_network_filter_config_fn<EC: EnvoyNetworkFilterConfig, ENF: EnvoyNetworkFilter>(
    _envoy_filter_config: &mut EC,
    filter_name: &str,
    filter_config: &[u8],
) -> Option<Box<dyn NetworkFilterConfig<ENF>>> {
    match filter_name {
        // "dns-gateway" is the legacy combined name kept for backward compatibility;
        // "dns-gateway-lookup" is the split network-filter extension.
        "dns-gateway-lookup" | "dns-gateway" => Some(Box::new(
            cache_lookup::CacheLookupFilterConfig::new(filter_config),
        )),
        _ => panic!("Unknown network filter name: {filter_name}"),
    }
}

fn new_udp_listener_filter_config_fn<
    EC: EnvoyUdpListenerFilterConfig,
    ELF: EnvoyUdpListenerFilter,
>(
    _envoy_filter_config: &mut EC,
    filter_name: &str,
    filter_config: &[u8],
) -> Option<Box<dyn UdpListenerFilterConfig<ELF>>> {
    match filter_name {
        // "dns-gateway" is the legacy combined name kept for backward compatibility;
        // "dns-gateway-resolver" is the split UDP-listener-filter extension.
        "dns-gateway-resolver" | "dns-gateway" => {
            let config = dns_gateway::DnsGatewayFilterConfig::new(filter_config)?;
            Some(Box::new(config))
        }
        _ => panic!("Unknown UDP listener filter name: {filter_name}"),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_new_network_filter_config_dns_gateway_lookup() {
        let mut mock = MockEnvoyNetworkFilterConfig::new();
        let result = new_network_filter_config_fn::<
            MockEnvoyNetworkFilterConfig,
            MockEnvoyNetworkFilter,
        >(&mut mock, "dns-gateway-lookup", b"");
        assert!(result.is_some());
    }

    #[test]
    fn test_new_network_filter_config_legacy_dns_gateway() {
        let mut mock = MockEnvoyNetworkFilterConfig::new();
        let result = new_network_filter_config_fn::<
            MockEnvoyNetworkFilterConfig,
            MockEnvoyNetworkFilter,
        >(&mut mock, "dns-gateway", b"");
        assert!(result.is_some());
    }

    #[test]
    #[should_panic(expected = "Unknown network filter name")]
    fn test_new_network_filter_config_unknown() {
        let mut mock = MockEnvoyNetworkFilterConfig::new();
        new_network_filter_config_fn::<MockEnvoyNetworkFilterConfig, MockEnvoyNetworkFilter>(
            &mut mock, "unknown", b"",
        );
    }

    #[test]
    fn test_new_udp_listener_filter_config_dns_gateway_resolver() {
        let config = r#"{
            "domains": [
                {"domain": "*.librs-test.com", "base_ip": "10.200.0.0", "prefix_len": 24, "metadata": {"cluster": "test"}}
            ]
        }"#;
        let mut mock = MockEnvoyUdpListenerFilterConfig::new();
        let result = new_udp_listener_filter_config_fn::<
            MockEnvoyUdpListenerFilterConfig,
            MockEnvoyUdpListenerFilter,
        >(&mut mock, "dns-gateway-resolver", config.as_bytes());
        assert!(result.is_some());
    }

    #[test]
    fn test_new_udp_listener_filter_config_legacy_dns_gateway() {
        let config = r#"{
            "domains": [
                {"domain": "*.librs-test.com", "base_ip": "10.200.0.0", "prefix_len": 24, "metadata": {"cluster": "test"}}
            ]
        }"#;
        let mut mock = MockEnvoyUdpListenerFilterConfig::new();
        let result = new_udp_listener_filter_config_fn::<
            MockEnvoyUdpListenerFilterConfig,
            MockEnvoyUdpListenerFilter,
        >(&mut mock, "dns-gateway", config.as_bytes());
        assert!(result.is_some());
    }

    #[test]
    fn test_new_udp_listener_filter_config_invalid() {
        let mut mock = MockEnvoyUdpListenerFilterConfig::new();
        let result = new_udp_listener_filter_config_fn::<
            MockEnvoyUdpListenerFilterConfig,
            MockEnvoyUdpListenerFilter,
        >(&mut mock, "dns-gateway-resolver", b"invalid");
        assert!(result.is_none());
    }

    #[test]
    #[should_panic(expected = "Unknown UDP listener filter name")]
    fn test_new_udp_listener_filter_config_unknown() {
        let mut mock = MockEnvoyUdpListenerFilterConfig::new();
        new_udp_listener_filter_config_fn::<
            MockEnvoyUdpListenerFilterConfig,
            MockEnvoyUdpListenerFilter,
        >(&mut mock, "unknown", b"");
    }
}
