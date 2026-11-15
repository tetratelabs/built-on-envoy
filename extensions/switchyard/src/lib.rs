// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

//! Switchyard model routing for Envoy.
//!
//! Clients request a virtual model, such as `coding-auto`. The filter buffers the request body,
//! asks a NVIDIA NeMo Switchyard routing algorithm which target model should serve the request,
//! and rewrites the `model` field of the body. Filters that route by the model in the body, such
//! as Agent Router's, then route the request to the selected model.

use std::sync::Arc;

use envoy_proxy_dynamic_modules_rust_sdk::*;

mod config;
mod filter;
mod host;
mod judge;
mod router;

use config::Config;
use filter::FilterConfig;

/// The name of the filter, as set in `filter_name` of the dynamic module filter configuration.
const FILTER_NAME: &str = "switchyard";

/// Name of the decisions counter.
const DECISIONS_COUNTER: &str = "switchyard_decisions";
/// Name of the judge calls counter.
const JUDGE_CALLS_COUNTER: &str = "switchyard_judge_calls";

fn init() -> bool {
    true
}

fn new_http_filter_config_fn<EC: EnvoyHttpFilterConfig, EHF: EnvoyHttpFilter>(
    envoy_filter_config: &mut EC,
    filter_name: &str,
    filter_config: &[u8],
) -> Option<Box<dyn HttpFilterConfig<EHF>>> {
    let config = new_filter_config(filter_name, filter_config, |name, labels| {
        envoy_filter_config.define_counter_vec(name, labels).ok()
    })?;
    Some(Box::new(config))
}

/// Builds the filter configuration and defines its counters.
fn new_filter_config(
    filter_name: &str,
    filter_config: &[u8],
    mut define_counter: impl FnMut(&str, &[&str]) -> Option<EnvoyCounterVecId>,
) -> Option<FilterConfig> {
    if filter_name != FILTER_NAME {
        envoy_log_error!("switchyard: unknown filter name {filter_name:?}");
        return None;
    }
    match Config::parse(filter_config) {
        Ok(config) => Some(FilterConfig {
            config: Arc::new(config),
            decisions: define_counter(
                DECISIONS_COUNTER,
                &["virtual_model", "selected_model", "outcome"],
            ),
            judge_calls: define_counter(JUDGE_CALLS_COUNTER, &["judge", "outcome"]),
        }),
        Err(err) => {
            envoy_log_error!("switchyard: {err}");
            None
        }
    }
}

declare_init_functions!(init, new_http_filter_config_fn);

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn new_filter_config_validates() {
        assert!(init());
        let config = br#"{"virtual_models": [{"name": "v", "targets": {"a": "m"}, "algorithm": {"type": "passthrough", "target": "a"}}]}"#;
        let built = new_filter_config(FILTER_NAME, config, |name, labels| match name {
            DECISIONS_COUNTER => {
                assert_eq!(labels, &["virtual_model", "selected_model", "outcome"]);
                Some(EnvoyCounterVecId(7))
            }
            JUDGE_CALLS_COUNTER => {
                assert_eq!(labels, &["judge", "outcome"]);
                Some(EnvoyCounterVecId(8))
            }
            _ => panic!("unexpected counter {name}"),
        })
        .unwrap();
        assert_eq!(built.decisions, Some(EnvoyCounterVecId(7)));
        assert_eq!(built.judge_calls, Some(EnvoyCounterVecId(8)));
        assert!(new_filter_config("other", config, |_, _| None).is_none());
        assert!(new_filter_config(FILTER_NAME, b"{}", |_, _| None).is_none());
    }
}
