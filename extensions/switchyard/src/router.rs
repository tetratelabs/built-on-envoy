// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

//! The Switchyard router: builds Switchyard algorithms and runs routing decisions.

use std::collections::{BTreeMap, BTreeSet, HashMap};
use std::sync::{Arc, Mutex, OnceLock, Weak};

use futures::StreamExt;
use serde_json::Value;
use switchyard_libsy::{Algorithm, RuntimeModels, State, Step};
use switchyard_protocol::{LlmRequest, Metadata, ModelId, Request, WireFormat};
use switchyard_runner::AlgorithmSpec;
use tokio::runtime::Runtime;

/// The algorithm types that decide from the request alone, without calling a model.
///
/// Algorithms that call a judge model or produce the answer themselves need the host to make model
/// calls, which this filter does not support.
const SUPPORTED_ALGORITHMS: &[&str] = &[
    "auto",
    "passthrough",
    "plan_execute",
    "random",
    "stage_router",
];

/// The Tokio runtime that runs the Switchyard algorithms.
///
/// Switchyard is async and needs a Tokio runtime with timers, which stateful algorithms use to
/// expire sessions. Envoy worker threads are not Tokio threads, so they block on this runtime for
/// the duration of a decision. Decision-only algorithms never wait on I/O, so this is short.
fn runtime() -> &'static Runtime {
    static RUNTIME: OnceLock<Runtime> = OnceLock::new();
    RUNTIME.get_or_init(|| {
        tokio::runtime::Builder::new_multi_thread()
            .worker_threads(1)
            .thread_name("switchyard")
            .enable_all()
            .build()
            .expect("failed to create the Switchyard runtime")
    })
}

/// Routers by configuration, so that Envoy configuration reloads reuse the existing routers and
/// keep their in-memory session state.
fn registry() -> &'static Mutex<HashMap<String, Weak<Router>>> {
    static REGISTRY: OnceLock<Mutex<HashMap<String, Weak<Router>>>> = OnceLock::new();
    REGISTRY.get_or_init(Default::default)
}

/// A Switchyard algorithm and the models it routes to.
pub struct Router {
    algorithm: Arc<dyn Algorithm>,
    models: Arc<RuntimeModels>,
    target_models: BTreeSet<String>,
}

/// A routing decision.
#[derive(Debug, PartialEq)]
pub struct Decision {
    /// The selected model.
    pub model: String,
    /// The request body to send, when the algorithm changed more than the model. When `None`, only
    /// the `model` field of the original body changes.
    pub body: Option<Value>,
    /// A short, low-cardinality reason for the decision.
    pub reason: String,
}

impl Router {
    /// Builds the router for an algorithm configuration, or returns the existing one if a router
    /// with the same configuration is alive.
    ///
    /// `targets` maps the target names used in the algorithm configuration to model names.
    pub fn build(
        name: &str,
        algorithm: &Value,
        targets: &BTreeMap<String, String>,
    ) -> Result<Arc<Self>, String> {
        validate_algorithm(algorithm)?;
        let key = format!(
            "{name}\0{algorithm}\0{}",
            serde_json::to_string(targets).unwrap_or_default()
        );
        let mut registry = registry().lock().unwrap_or_else(|e| e.into_inner());
        if let Some(router) = registry.get(&key).and_then(Weak::upgrade) {
            return Ok(router);
        }

        let spec: AlgorithmSpec = serde_json::from_value(algorithm.clone())
            .map_err(|e| format!("invalid algorithm: {e}"))?;
        let target_ids: BTreeMap<String, ModelId> = targets
            .iter()
            .map(|(name, model)| (name.clone(), ModelId::new(model.clone())))
            .collect();
        // Some algorithms spawn tasks when they are built.
        let _guard = runtime().enter();
        let (algorithm, models) = spec
            .build_with_runtime_models(name, &target_ids)
            .map_err(|e| format!("invalid algorithm: {e}"))?;

        let router = Arc::new(Self {
            algorithm,
            models: Arc::new(models),
            target_models: targets.values().cloned().collect(),
        });
        registry.retain(|_, r| r.strong_count() > 0);
        registry.insert(key, Arc::downgrade(&router));
        Ok(router)
    }

    /// Selects the model for a request body in the given API format.
    pub fn route(
        &self,
        format: WireFormat,
        body: &Value,
        metadata: Metadata,
    ) -> Result<Decision, String> {
        let mut llm_request = switchyard_translation::decode_request(format, body)
            .map_err(|e| format!("failed to decode the request: {e}"))?;
        switchyard_translation::normalize_compaction(&mut llm_request, None);
        let input = llm_request.clone();
        let request = Request {
            llm_request,
            metadata: Some(metadata),
            ..Default::default()
        };

        let step = runtime().block_on(async {
            Arc::clone(&self.algorithm)
                .run_stream(request, Arc::clone(&self.models), State::default())
                .next()
                .await
        });
        let outcome = match step {
            Some(Ok(Step::Done(outcome))) => outcome,
            Some(Ok(Step::CallModel(_) | Step::CallDecision(_))) => {
                return Err("the algorithm requested a model call, which is not supported".into())
            }
            Some(Err(e)) => return Err(format!("routing failed: {e}")),
            None => return Err("routing ended without a decision".into()),
        };
        if outcome.response.is_some() {
            return Err("the algorithm produced a response, which is not supported".into());
        }
        let model = outcome
            .selected_model_ids
            .first()
            .ok_or("the algorithm selected no model")?
            .to_string();
        if !self.target_models.contains(&model) {
            return Err(format!(
                "the algorithm selected {model:?}, which is not a target"
            ));
        }

        // Only send Switchyard's encoding of the request when the algorithm changed it. Otherwise
        // keep the client's body, which may have fields Switchyard does not model.
        let rewritten = encode(&outcome.request.llm_request, &model, format)?;
        let body = (rewritten != encode(&input, &model, format)?).then_some(rewritten);

        Ok(Decision {
            model,
            body,
            reason: reason(outcome.metadata.as_ref()),
        })
    }
}

/// Encodes a request for the given model.
fn encode(request: &LlmRequest, model: &str, format: WireFormat) -> Result<Value, String> {
    let mut request = request.clone();
    switchyard_translation::prepare_request_for_target(&mut request, &ModelId::new(model), None);
    switchyard_translation::encode_request(&request, format)
        .map_err(|e| format!("failed to encode the request: {e}"))
}

/// Builds the decision reason from the outcome metadata: the algorithm, and the evidence source
/// when the algorithm reports one.
fn reason(metadata: Option<&switchyard_libsy::OutcomeMetadata>) -> String {
    let Some(metadata) = metadata else {
        return "unknown".to_string();
    };
    match metadata
        .evidence
        .as_ref()
        .and_then(|e| e.get("source"))
        .and_then(Value::as_str)
    {
        Some(source) => format!("{}:{source}", metadata.algorithm),
        None => metadata.algorithm.clone(),
    }
}

/// Rejects the algorithms, and the algorithm options, that need model calls.
fn validate_algorithm(algorithm: &Value) -> Result<(), String> {
    let typ = algorithm
        .get("type")
        .and_then(Value::as_str)
        .ok_or("algorithm must be an object with a string \"type\" field")?;
    if !SUPPORTED_ALGORITHMS.contains(&typ) {
        return Err(format!(
            "algorithm type {typ:?} is not supported; supported types are: {}",
            SUPPORTED_ALGORITHMS.join(", ")
        ));
    }
    let is_set = |field: &str| algorithm.get(field).is_some_and(|v| !v.is_null());
    if typ == "stage_router" && is_set("classifier") {
        return Err("stage_router with a classifier is not supported".into());
    }
    if typ == "plan_execute"
        && (is_set("judge") || algorithm.get("execution_trigger") == Some(&Value::from("judge")))
    {
        return Err("plan_execute with a judge is not supported".into());
    }
    if let Some(subagents) = algorithm.get("subagents").filter(|v| !v.is_null()) {
        if subagents.get("type").and_then(Value::as_str) != Some("passthrough") {
            return Err("only passthrough subagents are supported".into());
        }
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::config::SessionConfig;
    use serde_json::json;

    fn targets() -> BTreeMap<String, String> {
        BTreeMap::from([
            ("efficient".to_string(), "small-model".to_string()),
            ("capable".to_string(), "large-model".to_string()),
        ])
    }

    fn chat(content: &str) -> Value {
        json!({"model": "coding-auto", "messages": [{"role": "user", "content": content}]})
    }

    #[test]
    fn validate_algorithms() {
        for ok in [
            json!({"type": "auto"}),
            json!({"type": "passthrough", "subagents": {"type": "passthrough", "target": "a"}}),
            json!({"type": "passthrough", "subagents": null}),
            json!({"type": "random"}),
            json!({"type": "stage_router", "classifier": null}),
            json!({"type": "plan_execute", "execution_trigger": "edit"}),
        ] {
            assert_eq!(validate_algorithm(&ok), Ok(()), "{ok}");
        }
        for (bad, want) in [
            (json!("auto"), "string \"type\" field"),
            (json!({}), "string \"type\" field"),
            (
                json!({"type": "composite"}),
                "not supported; supported types are",
            ),
            (
                json!({"type": "stage_router", "classifier": {}}),
                "classifier",
            ),
            (json!({"type": "plan_execute", "judge": {}}), "judge"),
            (
                json!({"type": "plan_execute", "execution_trigger": "judge"}),
                "judge",
            ),
            (
                json!({"type": "stage_router", "subagents": {"type": "llm_classifier"}}),
                "subagents",
            ),
        ] {
            let err = validate_algorithm(&bad).unwrap_err();
            assert!(err.contains(want), "{bad}: {err}");
        }
    }

    #[test]
    fn build_errors() {
        let err = Router::build(
            "bad-target",
            &json!({"type": "passthrough", "target": "missing"}),
            &targets(),
        )
        .err()
        .unwrap();
        assert!(err.contains("invalid algorithm"), "{err}");

        let err = Router::build(
            "bad-field",
            &json!({"type": "passthrough", "target": "efficient", "unknown": 1}),
            &targets(),
        )
        .err()
        .unwrap();
        assert!(err.contains("invalid algorithm"), "{err}");
    }

    #[test]
    fn build_reuses_live_routers() {
        let algorithm = json!({"type": "passthrough", "target": "efficient"});
        let a = Router::build("reuse", &algorithm, &targets()).unwrap();
        let b = Router::build("reuse", &algorithm, &targets()).unwrap();
        assert!(Arc::ptr_eq(&a, &b));
        let other = Router::build("reuse-other", &algorithm, &targets()).unwrap();
        assert!(!Arc::ptr_eq(&a, &other));

        let weak = Arc::downgrade(&a);
        drop(a);
        drop(b);
        assert!(weak.upgrade().is_none());
        // A new router is built once the previous one is gone.
        let c = Router::build("reuse", &algorithm, &targets()).unwrap();
        assert!(Arc::ptr_eq(
            &c,
            &Router::build("reuse", &algorithm, &targets()).unwrap()
        ));
    }

    #[test]
    fn route_passthrough_keeps_the_client_body() {
        let router = Router::build(
            "passthrough",
            &json!({"type": "passthrough", "target": "capable"}),
            &targets(),
        )
        .unwrap();
        let decision = router
            .route(WireFormat::OpenAiChat, &chat("hello"), Metadata::default())
            .unwrap();
        assert_eq!(decision.model, "large-model");
        assert_eq!(decision.body, None);
        assert!(
            decision.reason.starts_with("passthrough"),
            "{}",
            decision.reason
        );
    }

    #[test]
    fn route_subagents() {
        let router = Router::build(
            "subagents",
            &json!({
                "type": "passthrough",
                "target": "capable",
                "subagents": {"type": "passthrough", "target": "efficient"}
            }),
            &targets(),
        )
        .unwrap();
        let session = SessionConfig {
            id_headers: vec!["x-session".into()],
            scope_headers: vec![],
            agent_id_header: "x-agent".into(),
            subagent_header: "x-sub".into(),
        };
        let route = |headers: &[(&str, &str)]| {
            let metadata = session.metadata(|name| {
                headers
                    .iter()
                    .find(|(n, _)| *n == name)
                    .map(|(_, v)| v.to_string())
            });
            router
                .route(WireFormat::OpenAiChat, &chat("hello"), metadata)
                .unwrap()
                .model
        };
        assert_eq!(route(&[("x-session", "s1")]), "large-model");
        assert_eq!(
            route(&[("x-session", "s1"), ("x-agent", "a1"), ("x-sub", "true")]),
            "small-model"
        );
    }

    #[test]
    fn route_random_weighted() {
        let router = Router::build(
            "random",
            &json!({"type": "random", "targets": ["efficient", "capable"], "weights": [0.0, 1.0]}),
            &targets(),
        )
        .unwrap();
        for _ in 0..10 {
            let decision = router
                .route(WireFormat::OpenAiChat, &chat("hello"), Metadata::default())
                .unwrap();
            assert_eq!(decision.model, "large-model");
        }
    }

    #[test]
    fn route_all_formats() {
        let router = Router::build(
            "auto-formats",
            &json!({"type": "auto", "capable_target": "capable", "efficient_target": "efficient"}),
            &targets(),
        )
        .unwrap();
        let bodies = [
            (WireFormat::OpenAiChat, chat("write a function")),
            (
                WireFormat::OpenAiResponses,
                json!({"model": "coding-auto", "input": "write a function"}),
            ),
            (
                WireFormat::AnthropicMessages,
                json!({"model": "coding-auto", "max_tokens": 128, "messages": [{"role": "user", "content": "write a function"}]}),
            ),
        ];
        for (format, body) in bodies {
            let metadata = Metadata {
                session_id: Some(format!("session-{format:?}")),
                ..Default::default()
            };
            let decision = router.route(format, &body, metadata).unwrap();
            // Without tool activity, auto starts on the efficient tier.
            assert_eq!(decision.model, "small-model", "{format:?}: {decision:?}");
            assert!(
                decision.reason.starts_with("stage"),
                "{format:?}: {decision:?}"
            );
        }
    }

    #[test]
    fn route_auto_escalates_critical_tool_errors() {
        let router = Router::build(
            "auto-escalation",
            &json!({"type": "auto", "capable_target": "capable", "efficient_target": "efficient"}),
            &targets(),
        )
        .unwrap();
        let body = json!({
            "model": "coding-auto",
            "messages": [
                {"role": "user", "content": "run the tests"},
                {"role": "assistant", "tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "bash", "arguments": "{\"command\":\"make test\"}"}}]},
                {"role": "tool", "tool_call_id": "call_1", "content": "{\"output\":\"out of memory\",\"exit_code\":1}"}
            ]
        });
        let decision = router
            .route(WireFormat::OpenAiChat, &body, Metadata::default())
            .unwrap();
        assert_eq!(decision.model, "large-model", "{decision:?}");
    }

    #[test]
    fn route_decode_error() {
        let router = Router::build(
            "decode-error",
            &json!({"type": "passthrough", "target": "capable"}),
            &targets(),
        )
        .unwrap();
        let err = router
            .route(
                WireFormat::OpenAiChat,
                &json!({"model": "coding-auto", "messages": "not-a-list"}),
                Metadata::default(),
            )
            .unwrap_err();
        assert!(err.contains("failed to decode"), "{err}");
    }

    #[test]
    fn reason_formats() {
        assert_eq!(reason(None), "unknown");
    }
}
