// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

//! The Switchyard router: builds Switchyard algorithms and runs routing decisions.

use std::collections::{BTreeMap, BTreeSet, HashMap};
use std::sync::{Arc, Mutex, OnceLock, Weak};

use futures::StreamExt;
use serde_json::Value;
use switchyard_libsy::{Algorithm, RoutingOutcome, RuntimeModels, State, Step, StepStream};
use switchyard_protocol::{
    Category, LlmRequest, LlmResponse, Metadata, ModelId, Request, Response, WireFormat,
};
use switchyard_runner::AlgorithmSpec;
use tokio::runtime::Runtime;

use crate::host::Host;
use crate::judge::{JudgeKind, Judges};

/// Algorithm types the filter cannot run. `prefill_router` runs a PyTorch model in process.
const UNSUPPORTED_ALGORITHMS: &[&str] = &["prefill_router"];

/// Algorithm types that always call a model, even without a judge.
const CALLING_ALGORITHMS: &[&str] = &["advisor", "vgr"];

/// The Tokio runtime that runs the Switchyard algorithms.
///
/// Switchyard is async and needs a Tokio runtime with timers, which stateful algorithms use to
/// expire sessions. Envoy worker threads are not Tokio threads. They block on this runtime until
/// the algorithm takes its first step, which is short as algorithms do no I/O of their own. When
/// that step is a model call, the rest of the run continues on this runtime, and the worker thread
/// is free until the outcome is ready.
///
/// One thread serves the runs of every worker. Runs spend their time waiting on calls, which Envoy
/// makes on the worker threads, so this thread only decodes the call responses, steps the
/// algorithms and encodes the answers. That is short compared to the model calls it waits on.
pub fn runtime() -> &'static Runtime {
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

/// The model groups of an algorithm, by category.
struct ModelGroups {
    parent: HashMap<Category, Vec<ModelId>>,
    subagent: Option<HashMap<Category, Vec<ModelId>>>,
}

/// A Switchyard algorithm and the models it routes to.
pub struct Router {
    algorithm: Arc<dyn Algorithm>,
    models: Arc<RuntimeModels>,
    groups: ModelGroups,
    /// The models a request can be routed to.
    routing_models: BTreeSet<String>,
    /// Judges with their own endpoints that the algorithm calls.
    judges: Judges,
    /// Gateway models that only judge, and never answer.
    gateway_judges: Arc<BTreeSet<String>>,
    /// Whether the algorithm calls models through the gateway, with `model_calls`.
    pub needs_model_calls: bool,
}

/// A routing decision: the request goes on to the selected model.
#[derive(Debug, PartialEq)]
pub struct Decision {
    /// The selected model.
    pub model: String,
    /// The request body to send, when the algorithm changed more than the model. When `None`, only
    /// the `model` field of the original body changes.
    pub body: Option<Value>,
    /// A short, low-cardinality reason for the decision.
    pub reason: String,
    /// The algorithm's evidence for the decision, as compact JSON, such as judge scores.
    pub evidence: Option<String>,
}

/// An answer written by the algorithm, sent to the client as a local response.
#[derive(Debug, PartialEq)]
pub struct Answer {
    /// The model that wrote the answer.
    pub model: String,
    pub reason: String,
    pub evidence: Option<String>,
    pub content_type: &'static str,
    pub body: Vec<u8>,
}

/// The outcome of a routing run.
#[derive(Debug, PartialEq)]
pub enum Outcome {
    Route(Decision),
    Answer(Answer),
}

/// A started routing run.
pub enum Start {
    /// The algorithm finished without calling a model.
    Finished(Result<Outcome, String>),
    /// The algorithm called a model: the rest of the run must be driven by a host.
    Pending(Box<Pending>),
}

/// A routing run waiting on a model call.
pub struct Pending {
    stream: StepStream,
    first: Step,
    finisher: Finisher,
    judges: Judges,
    gateway_judges: Arc<BTreeSet<String>>,
}

impl Pending {
    /// Drives the run to its outcome, serving its calls with the host.
    pub async fn run(self, host: &Host) -> Result<Outcome, String> {
        let outcome = host
            .drive(self.stream, self.first)
            .await
            .map_err(|e| format!("routing failed: {e}"))?;
        self.finisher.finish(outcome).await
    }

    /// Judges with their own endpoints.
    pub fn judges(&self) -> Judges {
        self.judges.clone()
    }

    /// Gateway models that only judge, and never answer.
    pub fn gateway_judges(&self) -> Arc<BTreeSet<String>> {
        Arc::clone(&self.gateway_judges)
    }
}

/// Turns a routing outcome into the filter's outcome.
struct Finisher {
    format: WireFormat,
    input: LlmRequest,
    /// The models the request may be routed to.
    allowed: BTreeSet<String>,
    eligible: Option<BTreeSet<String>>,
    /// Whether the algorithm routes to models. Algorithms that do not, such as `noop`, answer
    /// without one.
    routes: bool,
}

impl Router {
    /// Builds the router for an algorithm configuration, or returns the existing one if a router
    /// with the same configuration is alive.
    ///
    /// `targets` maps the target names used in the algorithm configuration to model names, and
    /// `judges` holds the judges configured with their own endpoints.
    pub fn build(
        name: &str,
        algorithm: &Value,
        targets: &BTreeMap<String, String>,
        judges: &Judges,
    ) -> Result<Arc<Self>, String> {
        let typ = validate_algorithm(algorithm)?;
        let spec: AlgorithmSpec = serde_json::from_value(algorithm.clone())
            .map_err(|e| format!("invalid algorithm: {e}"))?;
        let judge_names = spec.judge_target_names();
        let mut used = Judges::new();
        for judge in &judge_names {
            if judge.trim().is_empty() {
                return Err("invalid algorithm: missing judge target name (classifier_target or classifier target)".into());
            }
            if let Some(configured) = judges.get(*judge) {
                used.insert(judge.to_string(), Arc::clone(configured));
            } else if !targets.contains_key(*judge) {
                return Err(format!("algorithm references judge {judge:?}, which is not configured in decision_targets, judge_targets or targets"));
            }
        }
        let decision_judge = has_decision_judge(algorithm)
            || (typ == "llm_classifier"
                && algorithm.get("mode").and_then(Value::as_str) == Some("direct"));
        if decision_judge {
            for judge in &judge_names {
                if !used
                    .get(*judge)
                    .is_some_and(|j| matches!(j.kind, JudgeKind::Decision(_)))
                {
                    return Err(format!(
                        "decision judge {judge:?} must be configured in decision_targets"
                    ));
                }
            }
        }
        for judge in used.keys() {
            if targets.contains_key(judge) || targets.values().any(|m| m == judge) {
                return Err(format!("judge {judge:?} has the same name as a target"));
            }
        }
        let key = format!(
            "{name}\0{algorithm}\0{}\0{}",
            serde_json::to_string(targets).unwrap_or_default(),
            judges_fingerprint(&used)
        );
        let mut registry = registry().lock().unwrap_or_else(|e| e.into_inner());
        if let Some(router) = registry.get(&key).and_then(Weak::upgrade) {
            return Ok(router);
        }

        // A configured judge is known to the algorithm by its own name.
        let mut target_ids: BTreeMap<String, ModelId> = targets
            .iter()
            .map(|(name, model)| (name.clone(), ModelId::new(model.clone())))
            .collect();
        target_ids.extend(used.keys().map(|name| (name.clone(), ModelId::new(name))));
        // Some algorithms spawn tasks when they are built.
        let _guard = runtime().enter();
        let (algorithm_impl, models) = spec
            .build_with_runtime_models(name, &target_ids)
            .map_err(|e| format!("invalid algorithm: {e}"))?;

        let resolve = |names: Vec<&str>| -> BTreeSet<String> {
            names
                .into_iter()
                .filter_map(|n| targets.get(n).cloned())
                .collect()
        };
        let routing_models = resolve(spec.routing_target_names());
        let gateway_judges: BTreeSet<String> = resolve(judge_names.clone())
            .difference(&routing_models)
            .cloned()
            .collect();
        let gateway_judge_names = judge_names.iter().any(|n| !used.contains_key(*n));
        let needs_model_calls = CALLING_ALGORITHMS.contains(&typ) || gateway_judge_names;

        let groups = model_groups(&models, algorithm);
        let router = Arc::new(Self {
            algorithm: algorithm_impl,
            models: Arc::new(models),
            groups,
            routing_models,
            judges: used,
            gateway_judges: Arc::new(gateway_judges),
            needs_model_calls,
        });
        registry.retain(|_, r| r.strong_count() > 0);
        registry.insert(key, Arc::downgrade(&router));
        Ok(router)
    }

    /// Starts routing a request body in the given API format.
    ///
    /// When `eligible` is set, the request may only be routed to these models.
    pub fn start(
        &self,
        format: WireFormat,
        body: &Value,
        metadata: Metadata,
        eligible: Option<BTreeSet<String>>,
    ) -> Start {
        let mut llm_request = match switchyard_translation::decode_request(format, body) {
            Ok(request) => request,
            Err(e) => return Start::Finished(Err(format!("failed to decode the request: {e}"))),
        };
        switchyard_translation::normalize_compaction(&mut llm_request, None);
        let input = llm_request.clone();
        let request = Request {
            llm_request,
            metadata: Some(metadata),
            ..Default::default()
        };
        let models = self.runtime_models(eligible.as_ref());
        let allowed = match &eligible {
            Some(eligible) => self
                .routing_models
                .intersection(eligible)
                .cloned()
                .collect(),
            None => self.routing_models.clone(),
        };
        let finisher = Finisher {
            format,
            input,
            allowed,
            eligible,
            routes: !self.routing_models.is_empty(),
        };

        let algorithm = Arc::clone(&self.algorithm);
        let runtime = runtime();
        let (stream, first) = runtime.block_on(async move {
            let mut stream = algorithm.run_stream(request, models, State::default());
            let first = stream.next().await;
            (stream, first)
        });
        match first {
            Some(Ok(Step::Done(outcome))) => {
                Start::Finished(runtime.block_on(finisher.finish(*outcome)))
            }
            Some(Ok(first)) => Start::Pending(Box::new(Pending {
                stream,
                first,
                finisher,
                judges: self.judges.clone(),
                gateway_judges: Arc::clone(&self.gateway_judges),
            })),
            Some(Err(e)) => Start::Finished(Err(format!("routing failed: {e}"))),
            None => Start::Finished(Err("routing ended without a decision".into())),
        }
    }

    /// The models of the algorithm, limited to the eligible models when set. Judges are never
    /// limited, as they do not answer requests.
    fn runtime_models(&self, eligible: Option<&BTreeSet<String>>) -> Arc<RuntimeModels> {
        let Some(eligible) = eligible else {
            return Arc::clone(&self.models);
        };
        let limit = |groups: &HashMap<Category, Vec<ModelId>>| -> HashMap<Category, Vec<ModelId>> {
            groups
                .iter()
                .map(|(category, models)| {
                    let models = models
                        .iter()
                        .filter(|m| *category == Category::Judge || eligible.contains(m.as_str()))
                        .cloned()
                        .collect();
                    (category.clone(), models)
                })
                .collect()
        };
        let mut models = RuntimeModels::new(limit(&self.groups.parent));
        if let Some(subagent) = &self.groups.subagent {
            models = models.with_subagent(limit(subagent));
        }
        Arc::new(models)
    }
}

impl Finisher {
    async fn finish(self, outcome: RoutingOutcome) -> Result<Outcome, String> {
        let reason = reason(outcome.metadata.as_ref());
        let evidence = evidence(outcome.metadata.as_ref());
        let model = outcome
            .selected_model_ids
            .first()
            .ok_or("the algorithm selected no model")?
            .to_string();

        if let Some(response) = outcome.response {
            let model = response
                .served_model()
                .map(ModelId::to_string)
                .unwrap_or(model);
            if self.routes && self.eligible.as_ref().is_some_and(|e| !e.contains(&model)) {
                return Err(format!(
                    "the answer is from {model:?}, which is not eligible"
                ));
            }
            let (content_type, body) =
                encode_answer(response, self.format, self.input.stream, &model).await?;
            return Ok(Outcome::Answer(Answer {
                model,
                reason,
                evidence,
                content_type,
                body,
            }));
        }

        if !self.allowed.contains(&model) {
            return Err(format!(
                "the algorithm selected {model:?}, which is not an eligible target"
            ));
        }
        // Only send Switchyard's encoding of the request when the algorithm changed it. Otherwise
        // keep the client's body, which may have fields Switchyard does not model.
        let rewritten = encode(&outcome.request.llm_request, &model, self.format)?;
        let body = (rewritten != encode(&self.input, &model, self.format)?).then_some(rewritten);
        Ok(Outcome::Route(Decision {
            model,
            body,
            reason,
            evidence,
        }))
    }
}

/// Identifies the judge definitions, credentials included, so that a configuration with a rotated
/// key builds a new router. Hashed, so the registry never holds the credentials in plain text.
fn judges_fingerprint(judges: &Judges) -> u64 {
    use std::hash::{Hash, Hasher};
    let mut hasher = std::collections::hash_map::DefaultHasher::new();
    for (name, judge) in judges {
        (
            name,
            &judge.cluster,
            &judge.authority,
            &judge.path,
            &judge.model,
        )
            .hash(&mut hasher);
        (
            &judge.headers,
            judge.timeout_ms,
            format!("{:?}", judge.kind),
        )
            .hash(&mut hasher);
    }
    hasher.finish()
}

/// Encodes a request for the given model.
fn encode(request: &LlmRequest, model: &str, format: WireFormat) -> Result<Value, String> {
    let mut request = request.clone();
    switchyard_translation::prepare_request_for_target(&mut request, &ModelId::new(model), None);
    switchyard_translation::encode_request(&request, format)
        .map_err(|e| format!("failed to encode the request: {e}"))
}

/// Encodes an answer in the client's API format, as server-sent events when the client asked for
/// a stream. Returns the content type and the body.
async fn encode_answer(
    response: Response,
    format: WireFormat,
    stream: bool,
    model: &str,
) -> Result<(&'static str, Vec<u8>), String> {
    let failed = |e: &dyn std::fmt::Display| format!("failed to encode the answer: {e}");
    if !stream {
        let agg = response
            .llm_response
            .into_agg()
            .await
            .map_err(|e| failed(&e))?;
        let body = switchyard_translation::encode_aggregated_response(&agg, format, Some(model))
            .map_err(|e| failed(&e))?;
        return Ok(("application/json", body.to_string().into_bytes()));
    }

    let chunks = match response.llm_response {
        LlmResponse::Stream(chunks) => chunks,
        LlmResponse::Agg(agg) => agg.into_stream(),
    };
    let mut events = switchyard_translation::encode_stream(chunks, format, Some(model.into()))
        .map_err(|e| failed(&e))?;
    let mut body = Vec::new();
    while let Some(event) = events.next().await {
        let event = event.map_err(|e| failed(&e))?;
        match format {
            WireFormat::OpenAiChat => {
                body.extend_from_slice(format!("data: {event}\n\n").as_bytes())
            }
            _ => {
                let typ = event
                    .get("type")
                    .and_then(Value::as_str)
                    .unwrap_or("message");
                body.extend_from_slice(format!("event: {typ}\ndata: {event}\n\n").as_bytes());
            }
        }
    }
    if format == WireFormat::OpenAiChat {
        body.extend_from_slice(b"data: [DONE]\n\n");
    }
    Ok(("text/event-stream", body))
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

/// The algorithm's evidence and judgments as compact JSON, when it reports any.
fn evidence(metadata: Option<&switchyard_libsy::OutcomeMetadata>) -> Option<String> {
    let metadata = metadata?;
    (metadata.evidence.is_some() || !metadata.judgments.is_empty()).then(|| {
        serde_json::json!({"evidence": metadata.evidence, "judgments": metadata.judgments})
            .to_string()
    })
}

/// Checks the algorithm type and returns it.
fn validate_algorithm(algorithm: &Value) -> Result<&str, String> {
    let typ = algorithm
        .get("type")
        .and_then(Value::as_str)
        .ok_or("algorithm must be an object with a string \"type\" field")?;
    if UNSUPPORTED_ALGORITHMS.contains(&typ) {
        return Err(format!("algorithm type {typ:?} is not supported"));
    }
    Ok(typ)
}

/// Whether the algorithm configuration has a decision judge, a `decision` block, anywhere.
fn has_decision_judge(value: &Value) -> bool {
    match value {
        Value::Object(object) => object.iter().any(|(key, value)| {
            (key == "decision" && value.is_object()) || has_decision_judge(value)
        }),
        Value::Array(values) => values.iter().any(has_decision_judge),
        _ => false,
    }
}

/// Reads the model groups of the algorithm, so they can be limited per request.
///
/// `RuntimeModels` does not list its categories. The built-in ones are probed, as are the custom
/// categories named in the `models` tables of the configuration.
fn model_groups(models: &RuntimeModels, algorithm: &Value) -> ModelGroups {
    fn named(value: &Value, names: &mut BTreeSet<String>) {
        match value {
            Value::Object(object) => {
                for (key, value) in object {
                    if key == "models" {
                        if let Some(models) = value.as_object() {
                            names.extend(models.keys().cloned());
                        }
                    }
                    named(value, names);
                }
            }
            Value::Array(values) => values.iter().for_each(|v| named(v, names)),
            _ => {}
        }
    }
    let mut names = BTreeSet::new();
    named(algorithm, &mut names);
    let categories: Vec<Category> = [
        Category::Any,
        Category::Capable,
        Category::Efficient,
        Category::Judge,
    ]
    .into_iter()
    .chain(names.iter().filter_map(|n| n.parse().ok()))
    .collect();

    let collect = |get: &dyn Fn(&Category) -> Vec<ModelId>| -> HashMap<Category, Vec<ModelId>> {
        categories
            .iter()
            .map(|c| (c.clone(), get(c)))
            .filter(|(_, models)| !models.is_empty())
            .collect()
    };
    let parent = collect(&|c| models.models_for(c).to_vec());
    let subagent = collect(&|c| models.subagent_models_for(c).to_vec());
    let has_subagents = algorithm.get("subagents").is_some_and(|v| !v.is_null());
    ModelGroups {
        parent,
        subagent: (has_subagents || !subagent.is_empty()).then_some(subagent),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::config::SessionConfig;
    use crate::host::testing::{chat_completion, FnTransport};
    use crate::host::{CallRole, RoutingUsage};
    use crate::judge::HttpReply;
    use serde_json::json;

    fn targets() -> BTreeMap<String, String> {
        BTreeMap::from([
            ("efficient".to_string(), "small-model".to_string()),
            ("capable".to_string(), "large-model".to_string()),
            ("judge".to_string(), "judge-model".to_string()),
        ])
    }

    fn chat(content: &str) -> Value {
        json!({"model": "coding-auto", "messages": [{"role": "user", "content": content}]})
    }

    /// Routes a request, driving the run with the given transport when it calls models.
    fn route_with(
        router: &Router,
        format: WireFormat,
        body: &Value,
        metadata: Metadata,
        eligible: Option<BTreeSet<String>>,
        transport: Arc<FnTransport>,
    ) -> Result<Outcome, String> {
        match router.start(format, body, metadata, eligible) {
            Start::Finished(outcome) => outcome,
            Start::Pending(pending) => {
                let host = Host {
                    transport,
                    gateway_format: WireFormat::OpenAiChat,
                    judges: pending.judges(),
                    gateway_judges: pending.gateway_judges(),
                    usage: Arc::new(Mutex::new(RoutingUsage::default())),
                };
                runtime().block_on(async move { pending.run(&host).await })
            }
        }
    }

    fn no_calls() -> Arc<FnTransport> {
        FnTransport::new(|_, _, body| panic!("unexpected call: {body}"))
    }

    fn route(
        router: &Router,
        format: WireFormat,
        body: &Value,
        metadata: Metadata,
    ) -> Result<Decision, String> {
        match route_with(router, format, body, metadata, None, no_calls())? {
            Outcome::Route(decision) => Ok(decision),
            Outcome::Answer(answer) => Err(format!("unexpected answer: {answer:?}")),
        }
    }

    #[test]
    fn validate_algorithms() {
        for ok in [
            json!({"type": "auto"}),
            json!({"type": "llm_classifier"}),
            json!({"type": "vgr"}),
        ] {
            assert!(validate_algorithm(&ok).is_ok(), "{ok}");
        }
        for (bad, want) in [
            (json!("auto"), "string \"type\" field"),
            (json!({}), "string \"type\" field"),
            (json!({"type": "prefill_router"}), "not supported"),
        ] {
            let err = validate_algorithm(&bad).unwrap_err();
            assert!(err.contains(want), "{bad}: {err}");
        }
    }

    #[test]
    fn decision_judges() {
        assert!(has_decision_judge(
            &json!({"classifier": {"decision": {"cutoff": 0.5}}})
        ));
        assert!(has_decision_judge(&json!([{"decision": {}}])));
        assert!(!has_decision_judge(
            &json!({"decision": null, "x": [1, "decision"]})
        ));
    }

    #[test]
    fn build_errors() {
        let err = Router::build(
            "bad-target",
            &json!({"type": "passthrough", "target": "missing"}),
            &targets(),
            &Judges::new(),
        )
        .err()
        .unwrap();
        assert!(err.contains("invalid algorithm"), "{err}");

        let err = Router::build(
            "bad-field",
            &json!({"type": "passthrough", "target": "efficient", "unknown": 1}),
            &targets(),
            &Judges::new(),
        )
        .err()
        .unwrap();
        assert!(err.contains("invalid algorithm"), "{err}");
    }

    #[test]
    fn build_flags() {
        let passthrough = Router::build(
            "flags-p",
            &json!({"type": "passthrough", "target": "efficient"}),
            &targets(),
            &Judges::new(),
        )
        .unwrap();
        assert!(!passthrough.needs_model_calls);

        assert!(passthrough.judges.is_empty());

        let classifier = Router::build(
            "flags-c",
            &capability_classifier(),
            &targets(),
            &Judges::new(),
        )
        .unwrap();
        assert!(classifier.needs_model_calls);

        assert_eq!(
            *classifier.gateway_judges,
            BTreeSet::from(["judge-model".to_string()])
        );

        let mut decision = capability_classifier();
        decision.as_object_mut().unwrap().remove("base_threshold");
        decision["decision"] = json!({
            "cutoff": 0.5,
            "candidates": {"capable_first": "capable", "efficient_first": "efficient"},
            "evidence": {}
        });
        let decision = Router::build("flags-d", &decision, &decision_targets(), &judges()).unwrap();
        assert_eq!(decision.judges.len(), 1);
        assert!(!decision.needs_model_calls);

        let advisor = Router::build(
            "flags-a",
            &json!({"type": "advisor", "executor_target": "efficient", "advisor_target": "capable"}),
            &targets(),
            &Judges::new(),
        )
        .unwrap();
        assert!(advisor.needs_model_calls);
    }

    #[test]
    fn build_reuses_live_routers() {
        let algorithm = json!({"type": "passthrough", "target": "efficient"});
        let a = Router::build("reuse", &algorithm, &targets(), &Judges::new()).unwrap();
        let b = Router::build("reuse", &algorithm, &targets(), &Judges::new()).unwrap();
        assert!(Arc::ptr_eq(&a, &b));
        let other = Router::build("reuse-other", &algorithm, &targets(), &Judges::new()).unwrap();
        assert!(!Arc::ptr_eq(&a, &other));

        let weak = Arc::downgrade(&a);
        drop(a);
        drop(b);
        assert!(weak.upgrade().is_none());
        // A new router is built once the previous one is gone.
        let c = Router::build("reuse", &algorithm, &targets(), &Judges::new()).unwrap();
        assert!(Arc::ptr_eq(
            &c,
            &Router::build("reuse", &algorithm, &targets(), &Judges::new()).unwrap()
        ));
    }

    #[test]
    fn route_passthrough_keeps_the_client_body() {
        let router = Router::build(
            "passthrough",
            &json!({"type": "passthrough", "target": "capable"}),
            &targets(),
            &Judges::new(),
        )
        .unwrap();
        let decision = route(
            &router,
            WireFormat::OpenAiChat,
            &chat("hello"),
            Metadata::default(),
        )
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
            &Judges::new(),
        )
        .unwrap();
        let session = SessionConfig {
            id_headers: vec!["x-session".into()],
            scope_headers: vec![],
            agent_id_header: "x-agent".into(),
            subagent_header: "x-sub".into(),
        };
        let route_headers = |headers: &[(&str, &str)]| {
            let metadata = session.metadata(|name| {
                headers
                    .iter()
                    .find(|(n, _)| *n == name)
                    .map(|(_, v)| v.to_string())
            });
            route(&router, WireFormat::OpenAiChat, &chat("hello"), metadata)
                .unwrap()
                .model
        };
        assert_eq!(route_headers(&[("x-session", "s1")]), "large-model");
        assert_eq!(
            route_headers(&[("x-session", "s1"), ("x-agent", "a1"), ("x-sub", "true")]),
            "small-model"
        );
    }

    #[test]
    fn route_random_weighted() {
        let router = Router::build(
            "random",
            &json!({"type": "random", "targets": ["efficient", "capable"], "weights": [0.0, 1.0]}),
            &targets(),
            &Judges::new(),
        )
        .unwrap();
        for _ in 0..10 {
            let decision = route(
                &router,
                WireFormat::OpenAiChat,
                &chat("hello"),
                Metadata::default(),
            )
            .unwrap();
            assert_eq!(decision.model, "large-model");
        }
    }

    #[test]
    fn route_eligible_models() {
        let router = Router::build(
            "eligible",
            &json!({"type": "random", "targets": ["efficient", "capable"]}),
            &targets(),
            &Judges::new(),
        )
        .unwrap();
        let eligible = |models: &[&str]| Some(models.iter().map(|m| m.to_string()).collect());
        for _ in 0..10 {
            let outcome = route_with(
                &router,
                WireFormat::OpenAiChat,
                &chat("hello"),
                Metadata::default(),
                eligible(&["small-model", "unknown-model"]),
                no_calls(),
            )
            .unwrap();
            // The capable model is not eligible, so the algorithm only has the efficient one.
            assert!(
                matches!(outcome, Outcome::Route(Decision { ref model, .. }) if model == "small-model"),
                "{outcome:?}"
            );
        }

        let err = route_with(
            &router,
            WireFormat::OpenAiChat,
            &chat("hello"),
            Metadata::default(),
            eligible(&["other-model"]),
            no_calls(),
        )
        .unwrap_err();
        assert!(
            err.contains("routing failed") || err.contains("eligible"),
            "{err}"
        );
    }

    #[test]
    fn route_all_formats() {
        let router = Router::build(
            "auto-formats",
            &json!({"type": "auto", "capable_target": "capable", "efficient_target": "efficient"}),
            &targets(),
            &Judges::new(),
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
            let decision = route(&router, format, &body, metadata).unwrap();
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
            &Judges::new(),
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
        let decision = route(&router, WireFormat::OpenAiChat, &body, Metadata::default()).unwrap();
        assert_eq!(decision.model, "large-model", "{decision:?}");
    }

    #[test]
    fn route_decode_error() {
        let router = Router::build(
            "decode-error",
            &json!({"type": "passthrough", "target": "capable"}),
            &targets(),
            &Judges::new(),
        )
        .unwrap();
        let err = route(
            &router,
            WireFormat::OpenAiChat,
            &json!({"model": "coding-auto", "messages": "not-a-list"}),
            Metadata::default(),
        )
        .unwrap_err();
        assert!(err.contains("failed to decode"), "{err}");
    }

    fn capability_classifier() -> Value {
        json!({
            "type": "llm_classifier",
            "classifier_target": "judge",
            "strong_target": "capable",
            "weak_target": "efficient",
            "base_threshold": 0.5,
            "fail_open": false
        })
    }

    fn verdict(p_solve: f64) -> String {
        json!({"crux": "c", "primary_rule": "SUP-1", "capability_boundary": "supported", "p_solve": p_solve}).to_string()
    }

    #[test]
    fn route_llm_classifier() {
        let router = Router::build(
            "classifier",
            &capability_classifier(),
            &targets(),
            &Judges::new(),
        )
        .unwrap();
        for (p_solve, want) in [(0.99, "small-model"), (0.01, "large-model")] {
            let transport = FnTransport::new(move |role, _, body| {
                assert_eq!(role, CallRole::Judge);
                assert_eq!(body["model"], "judge-model");
                chat_completion("judge-model", &verdict(p_solve))
            });
            let outcome = route_with(
                &router,
                WireFormat::OpenAiChat,
                &chat("write a function"),
                Metadata::default(),
                None,
                transport.clone(),
            )
            .unwrap();
            let Outcome::Route(decision) = outcome else {
                panic!("{outcome:?}")
            };
            assert_eq!(decision.model, want);
            assert_eq!(transport.calls.lock().unwrap().len(), 1);
        }
    }

    #[test]
    fn route_llm_classifier_judge_failure() {
        let router = Router::build(
            "classifier-failure",
            &capability_classifier(),
            &targets(),
            &Judges::new(),
        )
        .unwrap();
        let transport = FnTransport::new(|_, _, _| {
            Ok(HttpReply {
                status: 401,
                body: b"denied".to_vec(),
            })
        });
        let err = route_with(
            &router,
            WireFormat::OpenAiChat,
            &chat("write a function"),
            Metadata::default(),
            None,
            transport,
        )
        .unwrap_err();
        assert!(err.contains("routing failed"), "{err}");
    }

    #[test]
    fn route_decision_judge() {
        let mut algorithm = capability_classifier();
        algorithm.as_object_mut().unwrap().remove("base_threshold");
        algorithm["decision"] = json!({
            "cutoff": 0.5,
            "candidates": {"capable_first": "capable", "efficient_first": "efficient"},
            "evidence": {"description": "compare"}
        });
        let router =
            Router::build("decision-judge", &algorithm, &decision_targets(), &judges()).unwrap();
        let transport = FnTransport::new(|role, _, body| {
            assert_eq!(role, CallRole::Decision);
            let question = body["questions"]
                .as_object()
                .unwrap()
                .keys()
                .next()
                .unwrap()
                .clone();
            Ok(HttpReply {
                status: 200,
                body: json!({"answers": {question: {"type": "choice", "choice": "advantage",
                    "probabilities": {"advantage": 0.9, "no_advantage": 0.1}}}})
                .to_string()
                .into_bytes(),
            })
        });
        let outcome = route_with(
            &router,
            WireFormat::OpenAiChat,
            &chat("write a function"),
            Metadata::default(),
            None,
            transport.clone(),
        )
        .unwrap();
        assert!(
            matches!(outcome, Outcome::Route(Decision { ref model, .. }) if model == "large-model"),
            "{outcome:?}"
        );
        assert_eq!(transport.calls.lock().unwrap().len(), 1);
    }

    fn decision_targets() -> BTreeMap<String, String> {
        let mut t = targets();
        t.remove("judge");
        t
    }

    fn judges() -> Judges {
        crate::judge::compile(serde_json::from_value(json!({"judge": {"format":"system_one","model":"jev","cluster":"judge-cluster","authority":"judge.example"}})).unwrap(), BTreeMap::new()).unwrap()
    }

    fn advisor() -> Value {
        json!({"type": "advisor", "executor_target": "efficient", "advisor_target": "judge"})
    }

    #[test]
    fn route_advisor_answers() {
        let router = Router::build("advisor", &advisor(), &targets(), &Judges::new()).unwrap();
        let transport = FnTransport::new(|_, _, body| match body["model"].as_str().unwrap() {
            "small-model" => chat_completion("small-model", "the answer"),
            "judge-model" => chat_completion("judge-model", "APPROVE"),
            other => panic!("unexpected model {other}"),
        });
        let outcome = route_with(
            &router,
            WireFormat::OpenAiChat,
            &chat("explain the code"),
            Metadata::default(),
            None,
            transport,
        )
        .unwrap();
        let Outcome::Answer(answer) = outcome else {
            panic!("{outcome:?}")
        };
        assert_eq!(answer.model, "small-model");
        assert_eq!(answer.content_type, "application/json");
        let body: Value = serde_json::from_slice(&answer.body).unwrap();
        assert_eq!(body["model"], "small-model");
        assert_eq!(body["choices"][0]["message"]["content"], "the answer");
    }

    #[test]
    fn route_advisor_streams_answers() {
        let router =
            Router::build("advisor-stream", &advisor(), &targets(), &Judges::new()).unwrap();
        let transport = FnTransport::new(|_, _, body| {
            assert_eq!(body["stream"], false);
            chat_completion(body["model"].as_str().unwrap(), "APPROVE")
        });
        for (format, body, marker) in [
            (
                WireFormat::OpenAiChat,
                json!({"model": "v", "stream": true, "messages": [{"role": "user", "content": "hi"}]}),
                "data: [DONE]",
            ),
            (
                WireFormat::AnthropicMessages,
                json!({"model": "v", "stream": true, "max_tokens": 64, "messages": [{"role": "user", "content": "hi"}]}),
                "event: message_stop",
            ),
            (
                WireFormat::OpenAiResponses,
                json!({"model": "v", "stream": true, "input": "hi"}),
                "event: response.completed",
            ),
        ] {
            // The advisor reviews one turn per session.
            let metadata = Metadata {
                session_id: Some(format!("stream-{format:?}")),
                ..Default::default()
            };
            let outcome =
                route_with(&router, format, &body, metadata, None, transport.clone()).unwrap();
            let Outcome::Answer(answer) = outcome else {
                panic!("{outcome:?}")
            };
            assert_eq!(answer.content_type, "text/event-stream");
            let text = String::from_utf8(answer.body).unwrap();
            assert!(text.contains(marker), "{format:?}: {text}");
        }
    }

    #[test]
    fn route_noop_answers_without_calls() {
        let router =
            Router::build("noop", &json!({"type": "noop"}), &targets(), &Judges::new()).unwrap();
        let outcome = route_with(
            &router,
            WireFormat::OpenAiChat,
            &chat("hi"),
            Metadata::default(),
            None,
            no_calls(),
        )
        .unwrap();
        let Outcome::Answer(answer) = outcome else {
            panic!("{outcome:?}")
        };
        let body: Value = serde_json::from_slice(&answer.body).unwrap();
        assert_eq!(body["choices"][0]["message"]["content"], "OK");
        assert!(!router.needs_model_calls);

        // noop routes to no model, so eligible models do not apply to its answer.
        let outcome = route_with(
            &router,
            WireFormat::OpenAiChat,
            &chat("hi"),
            Metadata::default(),
            Some(BTreeSet::from(["other-model".to_string()])),
            no_calls(),
        )
        .unwrap();
        assert!(matches!(outcome, Outcome::Answer(_)), "{outcome:?}");
    }

    #[test]
    fn route_advisor_rejects_ineligible_answers() {
        let router =
            Router::build("advisor-eligible", &advisor(), &targets(), &Judges::new()).unwrap();
        let transport = FnTransport::new(|_, _, body| {
            let model = body["model"].as_str().unwrap();
            chat_completion(
                model,
                if model == "judge-model" {
                    "APPROVE"
                } else {
                    "the answer"
                },
            )
        });
        // No target is eligible: the answer must not reach the client.
        let result = route_with(
            &router,
            WireFormat::OpenAiChat,
            &chat("explain the code"),
            Metadata::default(),
            Some(BTreeSet::from(["other-model".to_string()])),
            transport,
        );
        assert!(!matches!(result, Ok(Outcome::Answer(_))), "{result:?}");
    }

    #[test]
    fn model_groups_probe_named_categories() {
        let models = RuntimeModels::new(HashMap::from([
            (Category::Named("fast".into()), vec![ModelId::from("a")]),
            (Category::Capable, vec![ModelId::from("b")]),
        ]));
        let groups = model_groups(&models, &json!({"models": {"fast": ["x"]}}));
        assert_eq!(groups.parent.len(), 2);
        assert!(groups.subagent.is_none());
    }

    #[test]
    fn reason_formats() {
        assert_eq!(reason(None), "unknown");
    }

    #[test]
    fn start_decision_judge_selects_capable_with_evidence() {
        let mut algorithm = capability_classifier();
        algorithm.as_object_mut().unwrap().remove("base_threshold");
        algorithm["decision"] = json!({"cutoff":0.05,"candidates":{"candidate_a":"capable","candidate_b":"efficient"},"evidence":{}});
        let router = Router::build("evidence", &algorithm, &decision_targets(), &judges()).unwrap();
        let transport = FnTransport::new(|role, target, body| {
            assert_eq!(role, CallRole::Decision);
            assert_eq!(target, "judge");
            assert_eq!(body["state"]["comparison"]["capable"], "candidate_a");
            Ok(HttpReply { status:200, body:json!({"answers":{"route":{"type":"choice","choice":"advantage","probabilities":{"advantage":0.9,"no_advantage":0.1}}}}).to_string().into_bytes() })
        });
        let result = route_with(
            &router,
            WireFormat::OpenAiChat,
            &chat("prove the theorem"),
            Metadata::default(),
            None,
            transport,
        )
        .unwrap();
        let Outcome::Route(decision) = result else {
            panic!("expected route")
        };
        assert_eq!(decision.model, "large-model");
        assert_eq!(decision.body, None);
        assert_eq!(decision.reason, "llm_task_classifier:decision_classifier");
        let evidence: Value = serde_json::from_str(&decision.evidence.unwrap()).unwrap();
        assert_eq!(evidence["evidence"]["selected_candidate"], "candidate_a");
        assert_eq!(evidence["judgments"].as_array().unwrap().len(), 1);
    }

    #[test]
    fn start_judge_error_follows_fail_closed_policy() {
        let mut algorithm = capability_classifier();
        algorithm.as_object_mut().unwrap().remove("base_threshold");
        algorithm["decision"] =
            json!({"cutoff":0.5,"candidates":{"a":"capable","b":"efficient"},"evidence":{}});
        let router = Router::build(
            "fail-closed-decision",
            &algorithm,
            &decision_targets(),
            &judges(),
        )
        .unwrap();
        let error = route_with(
            &router,
            WireFormat::OpenAiChat,
            &chat("hello"),
            Metadata::default(),
            None,
            FnTransport::new(|_, _, _| Err("judge unavailable".into())),
        )
        .unwrap_err();
        assert!(error.contains("judge unavailable"), "{error}");
    }

    #[test]
    fn build_requires_configured_judge() {
        let error = Router::build(
            "missing",
            &capability_classifier(),
            &decision_targets(),
            &Judges::new(),
        )
        .err()
        .unwrap();
        assert_eq!(error, "algorithm references judge \"judge\", which is not configured in decision_targets, judge_targets or targets");
        let error = Router::build(
            "empty",
            &json!({"type":"llm_classifier"}),
            &targets(),
            &Judges::new(),
        )
        .err()
        .unwrap();
        assert!(
            error.starts_with("invalid algorithm: missing judge target name"),
            "{error}"
        );
    }

    #[test]
    fn build_judge_definitions_separate_registry_entries() {
        let original = crate::judge::compile(BTreeMap::new(), serde_json::from_value(json!({"judge":{"format":"openai_chat","model":"j","cluster":"c","authority":"j.example","api_key":{"inline":"old-key"}}})).unwrap()).unwrap();
        let a = Router::build(
            "key-rotation",
            &capability_classifier(),
            &decision_targets(),
            &original,
        )
        .unwrap();
        let changed = crate::judge::compile(BTreeMap::new(), serde_json::from_value(json!({"judge":{"format":"openai_chat","model":"j","cluster":"c","authority":"j.example","api_key":{"inline":"new-key"}}})).unwrap()).unwrap();
        let b = Router::build(
            "key-rotation",
            &capability_classifier(),
            &decision_targets(),
            &changed,
        )
        .unwrap();
        assert!(!Arc::ptr_eq(&a, &b));
    }

    #[test]
    fn drop_run_cancels_pending_judge() {
        use crate::host::{Callout, Transport};
        struct Holding(std::sync::mpsc::Sender<Callout>);
        impl Transport for Holding {
            fn send(&self, call: Callout) {
                self.0.send(call).unwrap();
            }
        }
        let router = Router::build(
            "cancel",
            &capability_classifier(),
            &targets(),
            &Judges::new(),
        )
        .unwrap();
        let Start::Pending(pending) = router.start(
            WireFormat::OpenAiChat,
            &chat("hi"),
            Metadata::default(),
            None,
        ) else {
            panic!("expected pending")
        };
        let (tx, rx) = std::sync::mpsc::channel();
        let host = Host {
            transport: Arc::new(Holding(tx)),
            judges: pending.judges(),
            gateway_judges: pending.gateway_judges(),
            gateway_format: WireFormat::OpenAiChat,
            usage: Arc::default(),
        };
        let task = runtime().spawn(async move { pending.run(&host).await });
        let mut call = rx.recv_timeout(std::time::Duration::from_secs(5)).unwrap();
        task.abort();
        assert!(runtime().block_on(task).unwrap_err().is_cancelled());
        runtime().block_on(call.reply.closed());
        assert!(call.reply.is_closed());
    }
}
