// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

use serde::Deserialize;
use std::collections::HashMap;

#[derive(Debug, Deserialize)]
pub struct DnsGateway {
    #[serde(default)]
    pub domains: Vec<DomainMatcher>,
}

#[derive(Debug, Deserialize)]
pub struct DomainMatcher {
    pub domain: String,

    #[serde(default)]
    pub metadata: HashMap<String, String>,

    pub base_ip: String,

    #[serde(default)]
    pub prefix_len: u32,
}
