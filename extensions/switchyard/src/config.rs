// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

//! Filter configuration: parsing, validation and the compiled form used by the filter.

use std::collections::BTreeMap;
use std::sync::Arc;

use serde::Deserialize;
use switchyard_protocol::{Metadata, WireFormat};

use crate::router::Router;

/// Default header carrying the session ID.
pub const DEFAULT_SESSION_ID_HEADER: &str = "x-switchyard-session-id";
/// Default fallback session header. This is the header Agent Router already uses for session spans.
pub const DEFAULT_FALLBACK_SESSION_ID_HEADER: &str = "agent-session-id";
/// Default header carrying the agent ID.
pub const DEFAULT_AGENT_ID_HEADER: &str = "x-switchyard-agent-id";
/// Default header flagging sub-agent requests.
pub const DEFAULT_SUBAGENT_HEADER: &str = "x-switchyard-is-subagent";

/// Separator between the scope values and the session ID. It cannot appear in header values, so
/// scoped session IDs cannot collide.
const SESSION_SCOPE_SEPARATOR: char = '\0';

/// The raw filter configuration, deserialized from the JSON filter config.
#[derive(Deserialize, Debug)]
#[serde(deny_unknown_fields)]
pub struct RawFilterConfig {
    /// The virtual models resolved by this filter. The first one that matches a request wins.
    pub virtual_models: Vec<RawVirtualModel>,
    /// How the session identity is read from the request headers.
    #[serde(default)]
    pub session: RawSession,
    /// Request path suffixes handled by the filter, and the API format of their bodies.
    #[serde(default = "default_endpoints")]
    pub endpoints: BTreeMap<String, WireFormat>,
    /// Optional request header set to the selected model.
    #[serde(default)]
    pub model_header: Option<String>,
    /// Whether to clear the route cache after a model is selected, so that Envoy re-evaluates the
    /// route. Not needed when a later filter, such as Agent Router's, clears it.
    #[serde(default)]
    pub clear_route_cache: bool,
    /// Whether to add the `x-switchyard-*` decision headers to the request.
    #[serde(default = "default_true")]
    pub decision_headers: bool,
    /// Whether to rewrite the `model` field of the request body to the selected model. When false,
    /// the body keeps the virtual model and the decision is only carried by the headers, dynamic
    /// metadata and filter state, for a later filter to act on.
    #[serde(default = "default_true")]
    pub rewrite_body: bool,
}

/// A virtual model: a model name that clients send and that the filter resolves to a target model.
#[derive(Deserialize, Debug)]
#[serde(deny_unknown_fields)]
pub struct RawVirtualModel {
    /// The model name clients send in the request body.
    pub name: String,
    /// Switchyard target names, used in the algorithm configuration, mapped to the model names
    /// they route to.
    pub targets: BTreeMap<String, String>,
    /// The Switchyard algorithm configuration, in Switchyard's own route format.
    pub algorithm: serde_json::Value,
    /// Model used when routing fails and the failure mode is `use_default`.
    #[serde(default)]
    pub default_model: Option<String>,
    /// What to do when routing fails.
    #[serde(default)]
    pub failure_mode: Option<FailureMode>,
    /// Additional request headers that must match for this virtual model to apply.
    #[serde(default)]
    pub match_headers: Vec<HeaderMatch>,
}

/// An exact request header match.
#[derive(Deserialize, Debug, Clone, PartialEq, Eq)]
#[serde(deny_unknown_fields)]
pub struct HeaderMatch {
    pub name: String,
    pub value: String,
}

/// What the filter does when the router fails to select a model.
#[derive(Deserialize, Debug, Clone, Copy, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
pub enum FailureMode {
    /// Route to the virtual model's `default_model`.
    UseDefault,
    /// Leave the request unchanged, so that it is routed by the virtual model name.
    Passthrough,
    /// Reject the request with a 503 response.
    Reject,
}

/// How the session identity is read from the request headers.
#[derive(Deserialize, Debug)]
#[serde(deny_unknown_fields)]
pub struct RawSession {
    #[serde(default = "default_session_id_header")]
    pub id_header: String,
    #[serde(default = "default_fallback_id_headers")]
    pub fallback_id_headers: Vec<String>,
    #[serde(default)]
    pub scope_headers: Vec<String>,
    #[serde(default = "default_agent_id_header")]
    pub agent_id_header: String,
    #[serde(default = "default_subagent_header")]
    pub subagent_header: String,
}

impl Default for RawSession {
    fn default() -> Self {
        Self {
            id_header: default_session_id_header(),
            fallback_id_headers: default_fallback_id_headers(),
            scope_headers: Vec::new(),
            agent_id_header: default_agent_id_header(),
            subagent_header: default_subagent_header(),
        }
    }
}

fn default_true() -> bool {
    true
}

fn default_session_id_header() -> String {
    DEFAULT_SESSION_ID_HEADER.to_string()
}

fn default_fallback_id_headers() -> Vec<String> {
    vec![DEFAULT_FALLBACK_SESSION_ID_HEADER.to_string()]
}

fn default_agent_id_header() -> String {
    DEFAULT_AGENT_ID_HEADER.to_string()
}

fn default_subagent_header() -> String {
    DEFAULT_SUBAGENT_HEADER.to_string()
}

fn default_endpoints() -> BTreeMap<String, WireFormat> {
    BTreeMap::from([
        ("/v1/chat/completions".to_string(), WireFormat::OpenAiChat),
        ("/v1/responses".to_string(), WireFormat::OpenAiResponses),
        ("/v1/messages".to_string(), WireFormat::AnthropicMessages),
    ])
}

/// The compiled filter configuration, shared by all the filter instances.
pub struct Config {
    pub virtual_models: Vec<VirtualModel>,
    pub session: SessionConfig,
    /// Path suffixes and their formats, longest suffix first.
    pub endpoints: Vec<(String, WireFormat)>,
    pub model_header: Option<String>,
    pub clear_route_cache: bool,
    pub decision_headers: bool,
    pub rewrite_body: bool,
}

/// A compiled virtual model.
pub struct VirtualModel {
    pub name: String,
    pub match_headers: Vec<HeaderMatch>,
    pub router: Arc<Router>,
    pub default_model: Option<String>,
    pub failure_mode: FailureMode,
}

/// The compiled session configuration. Header names are lowercase.
#[derive(Debug, PartialEq, Eq)]
pub struct SessionConfig {
    pub id_headers: Vec<String>,
    pub scope_headers: Vec<String>,
    pub agent_id_header: String,
    pub subagent_header: String,
}

impl Config {
    /// Parses and validates the JSON filter configuration, and builds the routers.
    pub fn parse(config: &[u8]) -> Result<Self, String> {
        let raw: RawFilterConfig =
            serde_json::from_slice(config).map_err(|e| format!("invalid configuration: {e}"))?;
        Self::new(raw)
    }

    fn new(raw: RawFilterConfig) -> Result<Self, String> {
        if raw.virtual_models.is_empty() {
            return Err("virtual_models must not be empty".to_string());
        }
        if raw.endpoints.is_empty() {
            return Err("endpoints must not be empty".to_string());
        }

        let mut virtual_models = Vec::with_capacity(raw.virtual_models.len());
        for vm in raw.virtual_models {
            virtual_models.push(VirtualModel::new(vm)?);
        }

        let mut endpoints: Vec<(String, WireFormat)> = raw.endpoints.into_iter().collect();
        if let Some((path, _)) = endpoints.iter().find(|(p, _)| !p.starts_with('/')) {
            return Err(format!("endpoint {path:?} must start with '/'"));
        }
        // Longest suffix first, so the most specific endpoint wins.
        endpoints.sort_by_key(|(path, _)| std::cmp::Reverse(path.len()));

        let session = raw.session;
        let mut id_headers = vec![session.id_header];
        id_headers.extend(session.fallback_id_headers);

        Ok(Self {
            virtual_models,
            session: SessionConfig {
                id_headers: lowercase_all(id_headers),
                scope_headers: lowercase_all(session.scope_headers),
                agent_id_header: session.agent_id_header.to_ascii_lowercase(),
                subagent_header: session.subagent_header.to_ascii_lowercase(),
            },
            endpoints,
            model_header: raw.model_header.map(|h| h.to_ascii_lowercase()),
            clear_route_cache: raw.clear_route_cache,
            decision_headers: raw.decision_headers,
            rewrite_body: raw.rewrite_body,
        })
    }

    /// Returns the API format of the request path, if the filter handles it.
    pub fn endpoint_format(&self, path: &str) -> Option<WireFormat> {
        let path = path.split(['?', '#']).next().unwrap_or(path);
        self.endpoints
            .iter()
            .find(|(suffix, _)| path.ends_with(suffix.as_str()))
            .map(|(_, format)| *format)
    }

    /// Returns the first virtual model matching the requested model and the request headers.
    pub fn find_virtual_model<'a>(
        &'a self,
        model: &str,
        header: impl Fn(&str) -> Option<String>,
    ) -> Option<&'a VirtualModel> {
        self.virtual_models.iter().find(|vm| {
            vm.name == model
                && vm
                    .match_headers
                    .iter()
                    .all(|m| header(&m.name).is_some_and(|v| v == m.value))
        })
    }
}

impl VirtualModel {
    fn new(raw: RawVirtualModel) -> Result<Self, String> {
        let name = raw.name;
        if name.is_empty() {
            return Err("virtual model name must not be empty".to_string());
        }
        if raw.targets.is_empty() {
            return Err(format!("virtual model {name:?}: targets must not be empty"));
        }
        if let Some(default) = &raw.default_model {
            if !raw.targets.values().any(|m| m == default) {
                return Err(format!(
                    "virtual model {name:?}: default_model {default:?} is not one of the target models"
                ));
            }
        }
        let failure_mode = match (raw.failure_mode, &raw.default_model) {
            (Some(FailureMode::UseDefault), None) => {
                return Err(format!(
                    "virtual model {name:?}: failure_mode use_default requires default_model"
                ));
            }
            (Some(mode), _) => mode,
            (None, Some(_)) => FailureMode::UseDefault,
            (None, None) => FailureMode::Passthrough,
        };
        let router = Router::build(&name, &raw.algorithm, &raw.targets)
            .map_err(|e| format!("virtual model {name:?}: {e}"))?;
        let match_headers = raw
            .match_headers
            .into_iter()
            .map(|m| HeaderMatch {
                name: m.name.to_ascii_lowercase(),
                value: m.value,
            })
            .collect();
        Ok(Self {
            name,
            match_headers,
            router,
            default_model: raw.default_model,
            failure_mode,
        })
    }
}

impl SessionConfig {
    /// Builds the Switchyard request metadata from the request headers.
    ///
    /// The session ID is the first non-empty session header. When scope headers are configured,
    /// their values are prefixed to it, so that sessions of different tenants never share state.
    pub fn metadata(&self, header: impl Fn(&str) -> Option<String>) -> Metadata {
        let session_id = self
            .id_headers
            .iter()
            .find_map(|h| header(h).filter(|v| !v.is_empty()))
            .map(|id| {
                if self.scope_headers.is_empty() {
                    return id;
                }
                let mut scoped = String::new();
                for h in &self.scope_headers {
                    scoped.push_str(&header(h).unwrap_or_default());
                    scoped.push(SESSION_SCOPE_SEPARATOR);
                }
                scoped.push_str(&id);
                scoped
            });
        let agent_id = header(&self.agent_id_header).filter(|v| !v.is_empty());
        let is_subagent = header(&self.subagent_header)
            .is_some_and(|v| v.eq_ignore_ascii_case("true") || v == "1");
        Metadata {
            session_id,
            agent_id,
            is_subagent,
            // Switchyard routes sub-agent requests to the `subagents` algorithm only when they
            // are flagged as delegated work, as it does for an explicit sub-agent header.
            is_delegated_work: is_subagent,
            ..Default::default()
        }
    }
}

fn lowercase_all(headers: Vec<String>) -> Vec<String> {
    headers
        .into_iter()
        .map(|h| h.to_ascii_lowercase())
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::HashMap;

    fn headers(pairs: &[(&str, &str)]) -> impl Fn(&str) -> Option<String> {
        let map: HashMap<String, String> = pairs
            .iter()
            .map(|(k, v)| (k.to_string(), v.to_string()))
            .collect();
        move |name| map.get(name).cloned()
    }

    const VALID: &str = r#"{
        "virtual_models": [
            {
                "name": "coding-auto",
                "match_headers": [{"name": "X-Tier", "value": "free"}],
                "targets": {"only": "small-model"},
                "algorithm": {"type": "passthrough", "target": "only"}
            },
            {
                "name": "coding-auto",
                "targets": {"efficient": "small-model", "capable": "large-model"},
                "algorithm": {"type": "auto", "capable_target": "capable", "efficient_target": "efficient"},
                "default_model": "small-model"
            }
        ]
    }"#;

    #[test]
    fn parse_valid_config_with_defaults() {
        let config = Config::parse(VALID.as_bytes()).unwrap();
        assert_eq!(config.virtual_models.len(), 2);
        assert_eq!(
            config.virtual_models[0].failure_mode,
            FailureMode::Passthrough
        );
        assert_eq!(
            config.virtual_models[1].failure_mode,
            FailureMode::UseDefault
        );
        assert_eq!(config.virtual_models[0].match_headers[0].name, "x-tier");
        assert!(config.decision_headers);
        assert!(config.rewrite_body);
        assert!(!config.clear_route_cache);
        assert_eq!(config.model_header, None);
        assert_eq!(
            config.session,
            SessionConfig {
                id_headers: vec![
                    "x-switchyard-session-id".to_string(),
                    "agent-session-id".to_string()
                ],
                scope_headers: vec![],
                agent_id_header: "x-switchyard-agent-id".to_string(),
                subagent_header: "x-switchyard-is-subagent".to_string(),
            }
        );
    }

    #[test]
    fn endpoint_format_matches_suffix() {
        let config = Config::parse(VALID.as_bytes()).unwrap();
        assert_eq!(
            config.endpoint_format("/v1/chat/completions"),
            Some(WireFormat::OpenAiChat)
        );
        assert_eq!(
            config.endpoint_format("/my-prefix/v1/chat/completions?foo=bar"),
            Some(WireFormat::OpenAiChat)
        );
        assert_eq!(
            config.endpoint_format("/anthropic/v1/messages"),
            Some(WireFormat::AnthropicMessages)
        );
        assert_eq!(
            config.endpoint_format("/v1/responses"),
            Some(WireFormat::OpenAiResponses)
        );
        assert_eq!(config.endpoint_format("/v1/embeddings"), None);
    }

    #[test]
    fn find_virtual_model_honours_header_matches() {
        let config = Config::parse(VALID.as_bytes()).unwrap();
        let free = config
            .find_virtual_model("coding-auto", headers(&[("x-tier", "free")]))
            .unwrap();
        assert_eq!(free.failure_mode, FailureMode::Passthrough);
        let other = config
            .find_virtual_model("coding-auto", headers(&[("x-tier", "pro")]))
            .unwrap();
        assert_eq!(other.failure_mode, FailureMode::UseDefault);
        assert!(config.find_virtual_model("gpt-5", headers(&[])).is_none());
    }

    #[test]
    fn parse_errors() {
        let cases = [
            ("not json", "invalid configuration"),
            (
                r#"{"virtual_models": []}"#,
                "virtual_models must not be empty",
            ),
            (r#"{"virtual_models": [], "unknown": 1}"#, "unknown field"),
            (
                r#"{"virtual_models": [{"name": "", "targets": {"a": "m"}, "algorithm": {"type": "passthrough", "target": "a"}}]}"#,
                "name must not be empty",
            ),
            (
                r#"{"virtual_models": [{"name": "v", "targets": {}, "algorithm": {"type": "passthrough", "target": "a"}}]}"#,
                "targets must not be empty",
            ),
            (
                r#"{"virtual_models": [{"name": "v", "targets": {"a": "m"}, "algorithm": {"type": "passthrough", "target": "a"}, "default_model": "x"}]}"#,
                "is not one of the target models",
            ),
            (
                r#"{"virtual_models": [{"name": "v", "targets": {"a": "m"}, "algorithm": {"type": "passthrough", "target": "a"}, "failure_mode": "use_default"}]}"#,
                "requires default_model",
            ),
            (
                r#"{"virtual_models": [{"name": "v", "targets": {"a": "m"}, "algorithm": {"type": "passthrough", "target": "a"}}], "endpoints": {}}"#,
                "endpoints must not be empty",
            ),
            (
                r#"{"virtual_models": [{"name": "v", "targets": {"a": "m"}, "algorithm": {"type": "passthrough", "target": "a"}}], "endpoints": {"v1/chat": "openai_chat"}}"#,
                "must start with '/'",
            ),
            (
                r#"{"virtual_models": [{"name": "v", "targets": {"a": "m"}, "algorithm": {"type": "llm_classifier"}}]}"#,
                "virtual model \"v\": algorithm type \"llm_classifier\" is not supported",
            ),
        ];
        for (input, want) in cases {
            let err = Config::parse(input.as_bytes()).err().unwrap();
            assert!(err.contains(want), "{input}: got {err:?}, want {want:?}");
        }
    }

    #[test]
    fn explicit_settings() {
        let config = Config::parse(
            br#"{
                "virtual_models": [{"name": "v", "targets": {"a": "m"}, "algorithm": {"type": "passthrough", "target": "a"}, "failure_mode": "reject"}],
                "session": {"id_header": "X-Session", "fallback_id_headers": [], "scope_headers": ["X-Tenant"], "agent_id_header": "X-Agent", "subagent_header": "X-Sub"},
                "endpoints": {"/chat": "openai_chat", "/v2/chat": "anthropic_messages"},
                "model_header": "X-Model",
                "clear_route_cache": true,
                "decision_headers": false,
                "rewrite_body": false
            }"#,
        )
        .unwrap();
        assert_eq!(config.virtual_models[0].failure_mode, FailureMode::Reject);
        assert!(!config.rewrite_body);
        assert_eq!(config.model_header.as_deref(), Some("x-model"));
        assert!(config.clear_route_cache);
        assert!(!config.decision_headers);
        assert_eq!(config.session.id_headers, vec!["x-session".to_string()]);
        assert_eq!(config.session.scope_headers, vec!["x-tenant".to_string()]);
        // The longest suffix wins.
        assert_eq!(
            config.endpoint_format("/v2/chat"),
            Some(WireFormat::AnthropicMessages)
        );
        assert_eq!(
            config.endpoint_format("/v1/chat"),
            Some(WireFormat::OpenAiChat)
        );
    }

    #[test]
    fn session_metadata() {
        let session = SessionConfig {
            id_headers: vec!["x-switchyard-session-id".into(), "agent-session-id".into()],
            scope_headers: vec![],
            agent_id_header: "x-switchyard-agent-id".into(),
            subagent_header: "x-switchyard-is-subagent".into(),
        };

        let md = session.metadata(headers(&[]));
        assert_eq!(md.session_id, None);
        assert_eq!(md.agent_id, None);
        assert!(!md.is_subagent);
        assert!(!md.is_subagent_work());

        let md = session.metadata(headers(&[
            ("x-switchyard-session-id", ""),
            ("agent-session-id", "s1"),
            ("x-switchyard-agent-id", "a1"),
            ("x-switchyard-is-subagent", "TRUE"),
        ]));
        assert_eq!(md.session_id.as_deref(), Some("s1"));
        assert_eq!(md.agent_id.as_deref(), Some("a1"));
        assert!(md.is_subagent);
        assert!(md.is_subagent_work());

        let md = session.metadata(headers(&[
            ("x-switchyard-session-id", "s0"),
            ("agent-session-id", "s1"),
            ("x-switchyard-is-subagent", "1"),
        ]));
        assert_eq!(md.session_id.as_deref(), Some("s0"));
        assert!(md.is_subagent);
    }

    #[test]
    fn session_metadata_scoped() {
        let session = SessionConfig {
            id_headers: vec!["x-session".into()],
            scope_headers: vec!["x-tenant".into(), "x-org".into()],
            agent_id_header: "x-agent".into(),
            subagent_header: "x-sub".into(),
        };
        let md = session.metadata(headers(&[("x-session", "s"), ("x-tenant", "t")]));
        assert_eq!(md.session_id.as_deref(), Some("t\0\0s"));
    }

    #[test]
    fn config_schema() {
        let schema: serde_json::Value =
            serde_json::from_str(include_str!("../config.schema.json")).unwrap();
        let compiled = jsonschema::validator_for(&schema).unwrap();

        let valid: serde_json::Value = serde_json::from_str(VALID).unwrap();
        assert!(compiled.is_valid(&valid));

        let full: serde_json::Value = serde_json::from_str(
            r#"{
                "virtual_models": [{"name": "v", "targets": {"a": "m"}, "algorithm": {"type": "passthrough", "target": "a"}, "failure_mode": "reject", "default_model": "m", "match_headers": [{"name": "x", "value": "y"}]}],
                "session": {"id_header": "x-session", "fallback_id_headers": [], "scope_headers": ["x-tenant"], "agent_id_header": "x-agent", "subagent_header": "x-sub"},
                "endpoints": {"/chat": "openai_chat"},
                "model_header": "x-model",
                "clear_route_cache": true,
                "decision_headers": false,
                "rewrite_body": false
            }"#,
        )
        .unwrap();
        assert!(compiled.is_valid(&full));

        for invalid in [
            r#"{}"#,
            r#"{"virtual_models": []}"#,
            r#"{"virtual_models": [{"name": "v", "targets": {"a": "m"}}]}"#,
            r#"{"virtual_models": [{"name": "v", "targets": {"a": "m"}, "algorithm": {"type": "auto"}, "failure_mode": "retry"}]}"#,
            r#"{"virtual_models": [{"name": "v", "targets": {"a": "m"}, "algorithm": {"type": "auto"}}], "endpoints": {"/x": "grpc"}}"#,
            r#"{"virtual_models": [{"name": "v", "targets": {"a": "m"}, "algorithm": {"type": "auto"}}], "unknown": true}"#,
        ] {
            let value: serde_json::Value = serde_json::from_str(invalid).unwrap();
            assert!(!compiled.is_valid(&value), "{invalid} should be invalid");
        }
    }
}
