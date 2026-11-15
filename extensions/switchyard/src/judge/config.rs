// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

//! Judge configuration: the `decision_targets` and `judge_targets` maps of the filter config.

use std::collections::BTreeMap;
use std::sync::Arc;

use serde::Deserialize;
use switchyard_protocol::WireFormat;

use super::{DecisionFormat, Judge, JudgeKind, Judges};

/// A secret read inline or from a file, as in the other Built On Envoy extensions.
#[derive(Deserialize, Debug, Clone, PartialEq, Eq)]
#[serde(deny_unknown_fields)]
pub struct DataSource {
    #[serde(default)]
    pub inline: Option<String>,
    #[serde(default)]
    pub file: Option<String>,
}

/// The trace context headers added to calls, so that they join the client request's trace.
#[derive(Deserialize, Debug, Clone, Copy, PartialEq, Eq, Default)]
#[serde(rename_all = "snake_case")]
pub enum TraceContext {
    /// W3C `traceparent`.
    #[default]
    W3c,
    /// Zipkin B3 headers.
    B3,
    /// No trace context.
    None,
}

/// A decision judge, such as Jev, in the `decision_targets` map.
#[derive(Deserialize, Debug)]
#[serde(deny_unknown_fields)]
pub struct RawDecisionTarget {
    pub format: DecisionFormat,
    pub model: String,
    pub cluster: String,
    pub authority: String,
    #[serde(default)]
    pub path: Option<String>,
    #[serde(default)]
    pub api_key: Option<DataSource>,
    #[serde(default)]
    pub headers: BTreeMap<String, String>,
    #[serde(default)]
    pub timeout_ms: Option<u64>,
    #[serde(default)]
    pub trace_context: TraceContext,
}

/// An LLM judge, called with chat completions, in the `judge_targets` map.
#[derive(Deserialize, Debug)]
#[serde(deny_unknown_fields)]
pub struct RawJudgeTarget {
    pub format: WireFormat,
    pub model: String,
    pub cluster: String,
    pub authority: String,
    #[serde(default)]
    pub path: Option<String>,
    #[serde(default)]
    pub api_key: Option<DataSource>,
    #[serde(default)]
    pub headers: BTreeMap<String, String>,
    #[serde(default)]
    pub timeout_ms: Option<u64>,
    #[serde(default)]
    pub trace_context: TraceContext,
}

const DEFAULT_DECISION_TIMEOUT_MS: u64 = 5000;
const DEFAULT_JUDGE_TIMEOUT_MS: u64 = 30000;
const ANTHROPIC_VERSION: &str = "2023-06-01";

/// The fields the two raw target types share.
struct Common {
    model: String,
    cluster: String,
    authority: String,
    path: Option<String>,
    api_key: Option<DataSource>,
    headers: BTreeMap<String, String>,
    timeout_ms: Option<u64>,
}

/// Validates the judge targets and compiles them into judges.
pub fn compile(
    decision_targets: BTreeMap<String, RawDecisionTarget>,
    judge_targets: BTreeMap<String, RawJudgeTarget>,
) -> Result<Judges, String> {
    let mut judges = Judges::new();
    for (name, t) in decision_targets {
        if judge_targets.contains_key(&name) {
            return Err(format!(
                "judge name {name:?} is configured in both decision_targets and judge_targets"
            ));
        }
        let path = match t.format {
            DecisionFormat::SystemOne => "/v1/systemone",
            DecisionFormat::OpenaiDecisions => "/v1/decisions",
        };
        let common = Common {
            model: t.model,
            cluster: t.cluster,
            authority: t.authority,
            path: t.path,
            api_key: t.api_key,
            headers: t.headers,
            timeout_ms: t.timeout_ms,
        };
        let judge = build(
            &name,
            JudgeKind::Decision(t.format),
            path,
            DEFAULT_DECISION_TIMEOUT_MS,
            false,
            common,
        )
        .map_err(|e| format!("decision target {name:?}: {e}"))?;
        judges.insert(name, Arc::new(judge));
    }
    for (name, t) in judge_targets {
        let path = match t.format {
            WireFormat::OpenAiChat => "/v1/chat/completions",
            WireFormat::OpenAiResponses => "/v1/responses",
            WireFormat::AnthropicMessages => "/v1/messages",
        };
        let common = Common {
            model: t.model,
            cluster: t.cluster,
            authority: t.authority,
            path: t.path,
            api_key: t.api_key,
            headers: t.headers,
            timeout_ms: t.timeout_ms,
        };
        let anthropic = t.format == WireFormat::AnthropicMessages;
        let judge = build(
            &name,
            JudgeKind::Llm(t.format),
            path,
            DEFAULT_JUDGE_TIMEOUT_MS,
            anthropic,
            common,
        )
        .map_err(|e| format!("judge target {name:?}: {e}"))?;
        judges.insert(name, Arc::new(judge));
    }
    Ok(judges)
}

fn build(
    name: &str,
    kind: JudgeKind,
    default_path: &str,
    default_timeout_ms: u64,
    anthropic: bool,
    c: Common,
) -> Result<Judge, String> {
    if name.is_empty() {
        return Err("the name must not be empty".into());
    }
    for (field, value) in [
        ("model", &c.model),
        ("cluster", &c.cluster),
        ("authority", &c.authority),
    ] {
        if value.trim().is_empty() {
            return Err(format!("{field} must not be empty"));
        }
    }
    let path = c.path.unwrap_or_else(|| default_path.to_string());
    if !path.starts_with('/') {
        return Err(format!("path {path:?} must start with '/'"));
    }
    let timeout_ms = c.timeout_ms.unwrap_or(default_timeout_ms);
    if timeout_ms < 1 {
        return Err("timeout_ms must be at least 1".into());
    }

    let mut headers: Vec<(String, String)> = Vec::new();
    if let Some(source) = &c.api_key {
        let key = read_key(source)?;
        if !valid_value(&key) {
            return Err("api_key contains characters that are not allowed in a header".into());
        }
        if anthropic {
            headers.push(("x-api-key".into(), key));
        } else {
            headers.push(("authorization".into(), format!("Bearer {key}")));
        }
    }
    let mut extra: Vec<(String, String)> = Vec::new();
    for (k, v) in c.headers {
        let k = k.to_ascii_lowercase();
        if k.is_empty() || !k.bytes().all(|b| b.is_ascii_graphic() && b != b':') {
            return Err(format!("header name {k:?} is not valid"));
        }
        if !valid_value(&v) {
            return Err(format!("the value of header {k:?} is not valid"));
        }
        if extra.iter().any(|(e, _)| *e == k) {
            return Err(format!("header {k:?} is set more than once"));
        }
        extra.push((k, v));
    }
    if anthropic && !extra.iter().any(|(k, _)| k == "anthropic-version") {
        headers.push(("anthropic-version".into(), ANTHROPIC_VERSION.into()));
    }
    headers.extend(extra);

    Ok(Judge {
        name: name.to_string(),
        kind,
        cluster: c.cluster,
        authority: c.authority,
        path,
        model: c.model,
        headers,
        timeout_ms,
    })
}

/// Reads a key from its data source. Errors name the file, never the content.
fn read_key(source: &DataSource) -> Result<String, String> {
    let key = match (&source.inline, &source.file) {
        (Some(inline), None) => inline.trim().to_string(),
        (None, Some(path)) => std::fs::read_to_string(path)
            .map_err(|e| format!("could not read api_key file {path:?}: {}", e.kind()))?
            .trim()
            .to_string(),
        _ => return Err("api_key must set exactly one of inline or file".into()),
    };
    if key.is_empty() {
        return Err("api_key must not be empty".into());
    }
    Ok(key)
}

/// A header value is valid when it holds no control characters, such as CR or LF.
fn valid_value(value: &str) -> bool {
    !value.chars().any(|c| c.is_control() && c != '\t')
}
#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    fn decisions(v: serde_json::Value) -> BTreeMap<String, RawDecisionTarget> {
        serde_json::from_value(v).unwrap()
    }

    fn llms(v: serde_json::Value) -> BTreeMap<String, RawJudgeTarget> {
        serde_json::from_value(v).unwrap()
    }

    fn decision(extra: serde_json::Value) -> Result<Judges, String> {
        let mut t = json!({"format": "system_one", "model": "m", "cluster": "c", "authority": "a"});
        t.as_object_mut()
            .unwrap()
            .extend(extra.as_object().unwrap().clone());
        compile(decisions(json!({"jev": t})), BTreeMap::new())
    }

    fn llm(format: &str, extra: serde_json::Value) -> Result<Judges, String> {
        let mut t = json!({"format": format, "model": "m", "cluster": "c", "authority": "a"});
        t.as_object_mut()
            .unwrap()
            .extend(extra.as_object().unwrap().clone());
        compile(BTreeMap::new(), llms(json!({"mini": t})))
    }

    fn temp_file(tag: &str, content: &str) -> std::path::PathBuf {
        let path = std::env::temp_dir().join(format!(
            "switchyard-judge-{tag}-{}-{:?}",
            std::process::id(),
            std::thread::current().id()
        ));
        std::fs::write(&path, content).unwrap();
        path
    }

    #[test]
    fn decision_defaults() {
        let judges = decision(json!({})).unwrap();
        let j = &judges["jev"];
        assert_eq!(j.path, "/v1/systemone");
        assert_eq!(j.timeout_ms, 5000);
        assert_eq!(j.kind, JudgeKind::Decision(DecisionFormat::SystemOne));
        assert!(j.headers.is_empty());
        let t =
            json!({"format": "openai_decisions", "model": "m", "cluster": "c", "authority": "a"});
        let judges = compile(decisions(json!({"d": t})), BTreeMap::new()).unwrap();
        assert_eq!(judges["d"].path, "/v1/decisions");
    }

    #[test]
    fn llm_path_defaults() {
        for (format, path) in [
            ("openai_chat", "/v1/chat/completions"),
            ("openai_responses", "/v1/responses"),
            ("anthropic_messages", "/v1/messages"),
        ] {
            let judges = llm(format, json!({})).unwrap();
            assert_eq!(judges["mini"].path, path);
            assert_eq!(judges["mini"].timeout_ms, 30000);
        }
    }

    #[test]
    fn explicit_path_and_timeout() {
        let judges = decision(json!({"path": "/x", "timeout_ms": 7})).unwrap();
        assert_eq!(judges["jev"].path, "/x");
        assert_eq!(judges["jev"].timeout_ms, 7);
    }

    #[test]
    fn validation_errors() {
        let cases = [
            (
                decision(json!({"timeout_ms": 0})),
                "decision target \"jev\": timeout_ms",
            ),
            (
                decision(json!({"model": ""})),
                "decision target \"jev\": model",
            ),
            (
                decision(json!({"cluster": " "})),
                "decision target \"jev\": cluster",
            ),
            (
                decision(json!({"authority": ""})),
                "decision target \"jev\": authority",
            ),
            (
                decision(json!({"path": "v1"})),
                "decision target \"jev\": path",
            ),
            (
                llm("openai_chat", json!({"timeout_ms": 0})),
                "judge target \"mini\": timeout_ms",
            ),
            (
                llm("openai_chat", json!({"path": "x"})),
                "judge target \"mini\": path",
            ),
            (
                llm("openai_chat", json!({"model": ""})),
                "judge target \"mini\": model",
            ),
        ];
        for (result, expected) in cases {
            let err = result.unwrap_err();
            assert!(err.starts_with(expected), "{err}");
        }
    }

    #[test]
    fn name_in_both_maps_errors() {
        let d = decisions(
            json!({"x": {"format": "system_one", "model": "m", "cluster": "c", "authority": "a"}}),
        );
        let l = llms(
            json!({"x": {"format": "openai_chat", "model": "m", "cluster": "c", "authority": "a"}}),
        );
        let err = compile(d, l).unwrap_err();
        assert!(err.contains("\"x\"") && err.contains("both"), "{err}");
    }

    #[test]
    fn data_source_needs_exactly_one() {
        let err = decision(json!({"api_key": {}})).unwrap_err();
        assert!(err.starts_with("decision target \"jev\": api_key"), "{err}");
        let err = decision(json!({"api_key": {"inline": "k", "file": "/nope"}})).unwrap_err();
        assert!(err.contains("exactly one"), "{err}");
    }

    #[test]
    fn empty_key_errors() {
        let err = decision(json!({"api_key": {"inline": "  \n"}})).unwrap_err();
        assert!(err.contains("must not be empty"), "{err}");
        let path = temp_file("empty", " \n");
        let err = decision(json!({"api_key": {"file": path}})).unwrap_err();
        std::fs::remove_file(&path).unwrap();
        assert!(err.contains("must not be empty"), "{err}");
    }

    #[test]
    fn file_key_is_read_and_trimmed() {
        let path = temp_file("key", "  sk-file-secret\n");
        let judges = decision(json!({"api_key": {"file": path}})).unwrap();
        std::fs::remove_file(&path).unwrap();
        assert_eq!(
            judges["jev"].headers,
            vec![(
                "authorization".to_string(),
                "Bearer sk-file-secret".to_string()
            )]
        );
    }

    #[test]
    fn missing_file_names_path() {
        let path =
            std::env::temp_dir().join(format!("switchyard-judge-missing-{}", std::process::id()));
        let err = decision(json!({"api_key": {"file": path}})).unwrap_err();
        assert!(err.contains(path.to_str().unwrap()), "{err}");
    }

    #[test]
    fn bearer_header_and_lowercased_extras() {
        let judges = llm(
            "openai_chat",
            json!({"api_key": {"inline": "sk-1"}, "headers": {"X-Org": "acme"}}),
        )
        .unwrap();
        assert_eq!(
            judges["mini"].headers,
            vec![
                ("authorization".to_string(), "Bearer sk-1".to_string()),
                ("x-org".to_string(), "acme".to_string())
            ]
        );
    }

    #[test]
    fn anthropic_headers() {
        let judges = llm("anthropic_messages", json!({"api_key": {"inline": "k"}})).unwrap();
        assert_eq!(
            judges["mini"].headers,
            vec![
                ("x-api-key".to_string(), "k".to_string()),
                ("anthropic-version".to_string(), "2023-06-01".to_string())
            ]
        );
        let judges = llm(
            "anthropic_messages",
            json!({"api_key": {"inline": "k"}, "headers": {"Anthropic-Version": "2024-01-01"}}),
        )
        .unwrap();
        let versions: Vec<_> = judges["mini"]
            .headers
            .iter()
            .filter(|(k, _)| k == "anthropic-version")
            .collect();
        assert_eq!(
            versions,
            vec![&("anthropic-version".to_string(), "2024-01-01".to_string())]
        );
    }

    #[test]
    fn invalid_header_values_error() {
        let err = decision(json!({"headers": {"x-a": "b\r\nx-evil: 1"}})).unwrap_err();
        assert!(err.starts_with("decision target \"jev\": "), "{err}");
        let err = llm("openai_chat", json!({"api_key": {"inline": "k\nz"}})).unwrap_err();
        assert!(err.starts_with("judge target \"mini\": "), "{err}");
        assert!(!err.contains("k\nz"));
        assert!(decision(json!({"headers": {"bad name": "v"}})).is_err());
    }

    #[test]
    fn debug_redacts_credentials() {
        let judges = llm(
            "anthropic_messages",
            json!({"api_key": {"inline": "sk-super-secret"}}),
        )
        .unwrap();
        let text = format!("{:?}", judges["mini"]);
        assert!(!text.contains("sk-super-secret"), "{text}");
        assert!(text.contains("redacted"));
        let judges = decision(json!({"api_key": {"inline": "sk-other-secret"}})).unwrap();
        assert!(!format!("{:?}", judges["jev"]).contains("sk-other-secret"));
    }
}
