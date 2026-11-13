// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

//! The HTTP filter: buffers the request body, resolves virtual models and rewrites the request.
//!
//! Algorithms that decide from the request alone are run in the body callback. Algorithms that
//! call models, such as judges, continue on the module's Tokio runtime: the filter holds the
//! request, makes the calls as HTTP callouts on the worker thread, and resumes the request, or
//! answers it, once the algorithm is done.

use std::collections::HashMap;
use std::sync::{Arc, Mutex};
use std::time::Instant;

use envoy_proxy_dynamic_modules_rust_sdk::*;
use serde_json::Value;
use switchyard_protocol::WireFormat;
use tokio::sync::oneshot;

use crate::config::{Callouts, Config, FailureMode, TraceContext, VirtualModel};
use crate::host::{CallRole, Callout, CalloutResult, Destination, Host, RoutingUsage, Transport};
use crate::judge::HttpReply;
use crate::router::{runtime, Answer, Decision, Outcome, Pending, Start};

/// Dynamic metadata namespace of the routing decision.
pub const METADATA_NAMESPACE: &str = "io.builtonenvoy.switchyard";
/// Request header carrying the virtual model the client requested.
pub const VIRTUAL_MODEL_HEADER: &str = "x-switchyard-virtual-model";
/// Request header carrying the selected model.
pub const SELECTED_MODEL_HEADER: &str = "x-switchyard-selected-model";
/// Request header carrying the reason for the decision.
pub const REASON_HEADER: &str = "x-switchyard-reason";
/// Filter state key of the virtual model the client requested.
pub const VIRTUAL_MODEL_FILTER_STATE: &str = "io.builtonenvoy.switchyard.virtual_model";
/// Filter state key of the selected model.
pub const SELECTED_MODEL_FILTER_STATE: &str = "io.builtonenvoy.switchyard.selected_model";
/// Filter state key of the reason for the decision.
pub const REASON_FILTER_STATE: &str = "io.builtonenvoy.switchyard.reason";
/// Filter state key of the judge evidence.
pub const EVIDENCE_FILTER_STATE: &str = "io.builtonenvoy.switchyard.evidence";
/// Header of the calls made by the filter, with the role of the call.
pub const CALL_HEADER: &str = "x-switchyard-call";
/// Header of the calls made by the filter, with the request ID of the client request.
pub const PARENT_ID_HEADER: &str = "x-switchyard-parent-id";
/// Response headers with the cost of the routing calls.
pub const ROUTING_CALLS_HEADER: &str = "x-switchyard-routing-calls";
pub const ROUTING_INPUT_TOKENS_HEADER: &str = "x-switchyard-routing-input-tokens";
pub const ROUTING_OUTPUT_TOKENS_HEADER: &str = "x-switchyard-routing-output-tokens";
pub const ROUTING_TOTAL_TOKENS_HEADER: &str = "x-switchyard-routing-total-tokens";

/// Outcome label values of the decisions counter.
const OUTCOME_ROUTED: &str = "routed";
const OUTCOME_ANSWERED: &str = "answered";
const OUTCOME_DEFAULT: &str = "default";
const OUTCOME_PASSTHROUGH: &str = "passthrough";
const OUTCOME_REJECTED: &str = "rejected";

/// Scheduled events: callouts are waiting to be sent, and the routing run is done.
const EVENT_CALLOUTS: u64 = 1;
const EVENT_DONE: u64 = 2;

/// The filter configuration shared by all the filter instances.
pub struct FilterConfig {
    pub config: Arc<Config>,
    /// Counter of decisions, by virtual model, selected model and outcome.
    pub decisions: Option<EnvoyCounterVecId>,
    /// Counter of judge and model calls, by judge or model and outcome.
    pub judge_calls: Option<EnvoyCounterVecId>,
}

impl<EHF: EnvoyHttpFilter> HttpFilterConfig<EHF> for FilterConfig {
    fn new_http_filter(&self, _envoy: &mut EHF) -> Box<dyn HttpFilter<EHF>> {
        Box::new(Filter::new(
            Arc::clone(&self.config),
            self.decisions,
            self.judge_calls,
        ))
    }
}

/// The per-request filter.
pub struct Filter {
    config: Arc<Config>,
    decisions: Option<EnvoyCounterVecId>,
    judge_calls: Option<EnvoyCounterVecId>,
    /// The API format of the request body, set when the filter handles the request and cleared
    /// once the request has been processed.
    format: Option<WireFormat>,
    /// The routing run in progress, when the algorithm calls models.
    run: Option<Run>,
    /// Callouts in flight, by callout ID, with the judge or model called.
    callouts: HashMap<u64, (String, oneshot::Sender<CalloutResult>)>,
    /// The cost of the routing calls, once the run is done.
    routing: Option<RoutingUsage>,
}

/// A routing run continuing on the Tokio runtime.
struct Run {
    /// Index of the virtual model in the configuration.
    virtual_model: usize,
    body: Value,
    shared: Arc<Mutex<Shared>>,
    task: tokio::task::AbortHandle,
    usage: Arc<Mutex<RoutingUsage>>,
    started: Instant,
    /// The headers of gateway model calls.
    model_headers: Vec<(String, Vec<u8>)>,
    /// The headers every call carries: the parent request ID and the trace context, by format.
    parent_id: Option<String>,
    trace: Option<(String, String)>,
}

/// State shared between the routing task and the filter.
#[derive(Default)]
struct Shared {
    /// Calls waiting to be sent from the worker thread.
    queue: Vec<Callout>,
    outcome: Option<Result<Outcome, String>>,
}

/// Sends calls through the worker thread of the request.
struct EnvoyTransport {
    shared: Arc<Mutex<Shared>>,
    scheduler: Arc<dyn EnvoyHttpFilterScheduler>,
}

impl Transport for EnvoyTransport {
    fn send(&self, callout: Callout) {
        lock(&self.shared).queue.push(callout);
        self.scheduler.commit(EVENT_CALLOUTS);
    }
}

fn lock<T>(mutex: &Mutex<T>) -> std::sync::MutexGuard<'_, T> {
    mutex.lock().unwrap_or_else(|e| e.into_inner())
}

/// What the filter does after processing the request body.
#[derive(Debug, PartialEq)]
enum Action {
    Continue,
    /// A local reply was sent.
    Stop,
    /// The routing run continues asynchronously.
    Wait,
}

fn header_value<EHF: EnvoyHttpFilter>(envoy: &EHF, name: &str) -> Option<String> {
    envoy
        .get_request_header_value(name)
        .map(|v| String::from_utf8_lossy(v.as_slice()).into_owned())
}

impl Drop for Filter {
    fn drop(&mut self) {
        // Stop the routing run of a request that is gone.
        if let Some(run) = &self.run {
            run.task.abort();
        }
    }
}

impl Filter {
    pub fn new(
        config: Arc<Config>,
        decisions: Option<EnvoyCounterVecId>,
        judge_calls: Option<EnvoyCounterVecId>,
    ) -> Self {
        Self {
            config,
            decisions,
            judge_calls,
            format: None,
            run: None,
            callouts: HashMap::new(),
            routing: None,
        }
    }

    /// Returns the API format of the request body when the filter must handle the request.
    fn handled_format<EHF: EnvoyHttpFilter>(&self, envoy: &EHF) -> Option<WireFormat> {
        if header_value(envoy, ":method").as_deref() != Some("POST") {
            return None;
        }
        let content_type = header_value(envoy, "content-type").unwrap_or_default();
        if !content_type.to_ascii_lowercase().contains("json") {
            return None;
        }
        // Compressed bodies cannot be inspected.
        if header_value(envoy, "content-encoding")
            .is_some_and(|e| !e.eq_ignore_ascii_case("identity"))
        {
            return None;
        }
        self.config
            .endpoint_format(&header_value(envoy, ":path").unwrap_or_default())
    }

    /// Replaces the whole request body.
    fn replace_body<EHF: EnvoyHttpFilter>(envoy: &mut EHF, body: &[u8], in_body_callback: bool) {
        let buffered = envoy.get_buffered_request_body_size();
        envoy.drain_buffered_request_body(buffered);
        if in_body_callback && !envoy.received_buffered_request_body() {
            let received = envoy.get_received_request_body_size();
            envoy.drain_received_request_body(received);
            envoy.append_received_request_body(body);
        } else {
            envoy.append_buffered_request_body(body);
        }
        if envoy.get_request_header_value("content-length").is_some() {
            envoy.set_request_header("content-length", body.len().to_string().as_bytes());
        }
    }

    /// Processes the complete request body.
    fn process<EHF: EnvoyHttpFilter>(&mut self, envoy: &mut EHF, in_body_callback: bool) -> Action {
        let Some(format) = self.format.take() else {
            return Action::Continue;
        };
        let raw = utility::read_whole_request_body(envoy);
        // Requests that are not valid JSON, or that have no model, are left to the next filters.
        let Ok(mut body) = serde_json::from_slice::<Value>(&raw) else {
            return Action::Continue;
        };
        let Some(model) = body
            .get("model")
            .and_then(Value::as_str)
            .map(str::to_string)
        else {
            return Action::Continue;
        };
        let config = Arc::clone(&self.config);
        let Some(vm) = config.find_virtual_model(&model, |h| header_value(envoy, h)) else {
            return Action::Continue;
        };

        let metadata = config.session.metadata(|h| header_value(envoy, h));
        let eligible = config.eligible_models(|h| header_value(envoy, h));
        let outcome = match vm.router.start(format, &body, metadata, eligible) {
            Start::Finished(outcome) => outcome,
            Start::Pending(pending) => {
                match self.start_run(envoy, vm, format, body.clone(), pending) {
                    Ok(()) => return Action::Wait,
                    Err(err) => Err(err),
                }
            }
        };
        self.finish(envoy, vm, &mut body, outcome, in_body_callback)
    }

    /// Applies the outcome of a routing run.
    fn finish<EHF: EnvoyHttpFilter>(
        &mut self,
        envoy: &mut EHF,
        vm: &VirtualModel,
        body: &mut Value,
        outcome: Result<Outcome, String>,
        in_body_callback: bool,
    ) -> Action {
        match outcome {
            Ok(Outcome::Route(decision)) => {
                envoy_log_debug!(
                    "switchyard: routed virtual model {} to {} ({})",
                    vm.name,
                    decision.model,
                    decision.reason
                );
                self.apply(envoy, vm, body, decision, OUTCOME_ROUTED, in_body_callback);
                Action::Continue
            }
            Ok(Outcome::Answer(answer)) => {
                envoy_log_debug!(
                    "switchyard: {} answered virtual model {} ({})",
                    answer.model,
                    vm.name,
                    answer.reason
                );
                self.answer(envoy, vm, answer);
                Action::Stop
            }
            Err(err) => self.fail(envoy, vm, body, &err, in_body_callback),
        }
    }

    /// Continues a routing run that calls models on the Tokio runtime.
    fn start_run<EHF: EnvoyHttpFilter>(
        &mut self,
        envoy: &mut EHF,
        vm: &VirtualModel,
        format: WireFormat,
        body: Value,
        pending: Box<Pending>,
    ) -> Result<(), String> {
        let config = Arc::clone(&self.config);
        let client_path = header_value(envoy, ":path").unwrap_or_default();
        // Without model_calls, gateway calls fail when they are sent.
        let (gateway_format, model_headers) = match &config.model_calls {
            Some(model_calls) => {
                let (model_format, model_path) = model_calls.format_and_path(format, &client_path);
                let headers = self.gateway_headers(envoy, &model_calls.callouts, &model_path);
                (model_format, headers)
            }
            None => (format, Vec::new()),
        };
        let parent_id = header_value(envoy, "x-request-id");
        let trace = envoy
            .get_active_span()
            .and_then(|span| Some((span.get_trace_id()?, span.get_span_id()?)));

        let shared = Arc::new(Mutex::new(Shared::default()));
        let scheduler: Arc<dyn EnvoyHttpFilterScheduler> = Arc::new(envoy.new_scheduler());
        let usage = Arc::new(Mutex::new(RoutingUsage::default()));
        let host = Host {
            transport: Arc::new(EnvoyTransport {
                shared: Arc::clone(&shared),
                scheduler: Arc::clone(&scheduler),
            }),
            judges: pending.judges(),
            gateway_format,
            gateway_judges: pending.gateway_judges(),
            usage: Arc::clone(&usage),
        };
        let task_shared = Arc::clone(&shared);
        let task = runtime().spawn(async move {
            let outcome = pending.run(&host).await;
            lock(&task_shared).outcome = Some(outcome);
            scheduler.commit(EVENT_DONE);
        });
        let virtual_model = config
            .virtual_models
            .iter()
            .position(|v| std::ptr::eq(v, vm))
            .unwrap_or_default();
        self.run = Some(Run {
            virtual_model,
            body,
            shared,
            task: task.abort_handle(),
            usage,
            started: Instant::now(),
            model_headers,
            parent_id,
            trace,
        });
        Ok(())
    }

    /// Builds the headers of the gateway calls made for a request: the request line, the forwarded
    /// client headers and the configured headers.
    fn gateway_headers<EHF: EnvoyHttpFilter>(
        &self,
        envoy: &EHF,
        callouts: &Callouts,
        path: &str,
    ) -> Vec<(String, Vec<u8>)> {
        let authority = callouts
            .authority
            .clone()
            .or_else(|| header_value(envoy, ":authority"))
            .unwrap_or_else(|| callouts.cluster.clone());
        let mut headers: Vec<(String, Vec<u8>)> = vec![
            (":method".into(), b"POST".to_vec()),
            (":path".into(), path.as_bytes().to_vec()),
            ("host".into(), authority.into_bytes()),
            ("content-type".into(), b"application/json".to_vec()),
        ];
        for name in &callouts.forward_headers {
            for value in envoy.get_request_header_values(name) {
                headers.push((name.clone(), value.as_slice().to_vec()));
            }
        }
        for (name, value) in &callouts.headers {
            headers.push((name.clone(), value.as_bytes().to_vec()));
        }
        headers
    }

    /// The headers every call carries: its role, the client request ID and the trace context.
    fn call_headers(
        run: &Run,
        role: CallRole,
        trace_context: TraceContext,
    ) -> Vec<(String, Vec<u8>)> {
        let mut headers = vec![(CALL_HEADER.to_string(), role.as_str().as_bytes().to_vec())];
        if let Some(id) = &run.parent_id {
            headers.push((PARENT_ID_HEADER.into(), id.clone().into_bytes()));
        }
        if let Some((trace, span)) = &run.trace {
            match trace_context {
                TraceContext::W3c => headers.push((
                    "traceparent".into(),
                    format!("00-{trace:0>32}-{span:0>16}-01").into_bytes(),
                )),
                TraceContext::B3 => {
                    headers.push(("x-b3-traceid".into(), trace.clone().into_bytes()));
                    headers.push(("x-b3-spanid".into(), span.clone().into_bytes()));
                    headers.push(("x-b3-sampled".into(), b"1".to_vec()));
                }
                TraceContext::None => {}
            }
        }
        headers
    }

    /// Sends the calls the routing run is waiting for.
    fn send_callouts<EHF: EnvoyHttpFilter>(&mut self, envoy: &mut EHF) {
        let Some(run) = &self.run else {
            return;
        };
        let queue = std::mem::take(&mut lock(&run.shared).queue);
        for callout in queue {
            let (cluster, timeout_ms, mut headers) = match &callout.destination {
                Destination::Judge(spec) => {
                    let trace_context = self
                        .config
                        .judge_trace_context(&callout.target)
                        .unwrap_or_default();
                    let mut headers = spec.headers.clone();
                    headers.extend(Self::call_headers(run, callout.role, trace_context));
                    (spec.cluster.clone(), spec.timeout_ms, headers)
                }
                Destination::Gateway => {
                    let Some(model_calls) = &self.config.model_calls else {
                        let _ = callout
                            .reply
                            .send(Err("model_calls is not configured".into()));
                        continue;
                    };
                    let mut headers = run.model_headers.clone();
                    headers.extend(Self::call_headers(
                        run,
                        callout.role,
                        model_calls.callouts.trace_context,
                    ));
                    (
                        model_calls.callouts.cluster.clone(),
                        model_calls.callouts.timeout_ms,
                        headers,
                    )
                }
            };
            headers.retain(|(k, _)| !k.is_empty());
            let refs: Vec<(&str, &[u8])> = headers
                .iter()
                .map(|(k, v)| (k.as_str(), v.as_slice()))
                .collect();
            let (result, id) =
                envoy.send_http_callout(&cluster, &refs, Some(&callout.body), timeout_ms);
            if result == abi::envoy_dynamic_module_type_http_callout_init_result::Success {
                self.callouts.insert(id, (callout.target, callout.reply));
            } else {
                self.count_judge(envoy, &callout.target, "error");
                let _ = callout
                    .reply
                    .send(Err(format!("could not call cluster {cluster}: {result:?}")));
            }
        }
    }

    /// Applies the outcome of the routing run, and resumes the request.
    fn complete<EHF: EnvoyHttpFilter>(&mut self, envoy: &mut EHF) {
        let Some(run) = self.run.take() else {
            return;
        };
        let Some(outcome) = lock(&run.shared).outcome.take() else {
            self.run = Some(run);
            return;
        };
        // Calls still in flight are not needed anymore.
        self.callouts.clear();
        let usage = lock(&run.usage).clone();
        self.record_routing(envoy, &usage, run.started);
        self.routing = Some(usage);

        let config = Arc::clone(&self.config);
        let vm = &config.virtual_models[run.virtual_model];
        let mut body = run.body;
        if self.finish(envoy, vm, &mut body, outcome, false) == Action::Continue {
            envoy.continue_decoding();
        }
    }

    /// Records the cost of the routing calls in dynamic metadata.
    fn record_routing<EHF: EnvoyHttpFilter>(
        &self,
        envoy: &mut EHF,
        usage: &RoutingUsage,
        started: Instant,
    ) {
        let numbers = [
            ("routing_calls", usage.calls),
            ("routing_input_tokens", usage.input_tokens),
            ("routing_output_tokens", usage.output_tokens),
            ("routing_total_tokens", usage.total_tokens),
            ("routing_latency_ms", started.elapsed().as_millis() as u64),
        ];
        for (key, value) in numbers {
            envoy.set_dynamic_metadata_number(METADATA_NAMESPACE, key, value as f64);
        }
        let models: Vec<&str> = usage.models.iter().map(String::as_str).collect();
        envoy.set_dynamic_metadata_string(METADATA_NAMESPACE, "routing_models", &models.join(","));
    }

    /// The routing cost response headers, when enabled.
    fn routing_headers(&self) -> Vec<(&'static str, String)> {
        match (&self.routing, self.config.routing_headers) {
            (Some(usage), true) => vec![
                (ROUTING_CALLS_HEADER, usage.calls.to_string()),
                (ROUTING_INPUT_TOKENS_HEADER, usage.input_tokens.to_string()),
                (
                    ROUTING_OUTPUT_TOKENS_HEADER,
                    usage.output_tokens.to_string(),
                ),
                (ROUTING_TOTAL_TOKENS_HEADER, usage.total_tokens.to_string()),
            ],
            _ => Vec::new(),
        }
    }

    /// Sends the answer written by the algorithm to the client.
    fn answer<EHF: EnvoyHttpFilter>(&self, envoy: &mut EHF, vm: &VirtualModel, answer: Answer) {
        self.record(
            envoy,
            vm,
            &answer.model,
            &answer.reason,
            answer.evidence.as_deref(),
        );
        self.count(envoy, &vm.name, &answer.model, OUTCOME_ANSWERED);
        let routing = self.routing_headers();
        let mut headers: Vec<(&str, &[u8])> =
            vec![("content-type", answer.content_type.as_bytes())];
        headers.extend(routing.iter().map(|(k, v)| (*k, v.as_bytes())));
        envoy.send_response(
            200,
            &headers,
            Some(&answer.body),
            Some("switchyard_answered"),
        );
    }

    /// Applies the failure mode of the virtual model after a routing error.
    fn fail<EHF: EnvoyHttpFilter>(
        &mut self,
        envoy: &mut EHF,
        vm: &VirtualModel,
        body: &mut Value,
        err: &str,
        in_body_callback: bool,
    ) -> Action {
        envoy_log_warn!(
            "switchyard: routing virtual model {} failed: {err}",
            vm.name
        );
        match (vm.failure_mode, &vm.default_model) {
            (FailureMode::UseDefault, Some(default)) => {
                let decision = Decision {
                    model: default.clone(),
                    body: None,
                    reason: "fallback:default".to_string(),
                    evidence: None,
                };
                self.apply(envoy, vm, body, decision, OUTCOME_DEFAULT, in_body_callback);
                Action::Continue
            }
            (FailureMode::Reject, _) => {
                self.count(envoy, &vm.name, "", OUTCOME_REJECTED);
                let reply = serde_json::json!({
                    "error": {
                        "type": "model_router_unavailable",
                        "message": format!("could not select a model for {}", vm.name),
                    }
                })
                .to_string();
                envoy.send_response(
                    503,
                    &[("content-type", b"application/json")],
                    Some(reply.as_bytes()),
                    Some("switchyard_routing_failed"),
                );
                Action::Stop
            }
            _ => {
                self.count(envoy, &vm.name, "", OUTCOME_PASSTHROUGH);
                Action::Continue
            }
        }
    }

    /// Applies the decision: rewrites the body, when enabled, and records the decision.
    fn apply<EHF: EnvoyHttpFilter>(
        &self,
        envoy: &mut EHF,
        vm: &VirtualModel,
        body: &mut Value,
        decision: Decision,
        outcome: &str,
        in_body_callback: bool,
    ) {
        if self.config.rewrite_body {
            let bytes = match &decision.body {
                Some(rewritten) => serde_json::to_vec(rewritten),
                None => {
                    body["model"] = Value::from(decision.model.as_str());
                    serde_json::to_vec(body)
                }
            };
            // Leave the request untouched rather than sending a broken body upstream.
            let bytes = match bytes {
                Ok(bytes) => bytes,
                Err(err) => {
                    envoy_log_warn!(
                        "switchyard: could not encode the request body for {}: {err}",
                        vm.name
                    );
                    self.count(envoy, &vm.name, "", OUTCOME_PASSTHROUGH);
                    return;
                }
            };
            Self::replace_body(envoy, &bytes, in_body_callback);
        } else if decision.body.is_some() {
            envoy_log_debug!(
                "switchyard: rewrite_body is disabled, dropping the request changes of the {} algorithm",
                vm.name
            );
        }

        if self.config.decision_headers {
            for (header, value) in [
                (VIRTUAL_MODEL_HEADER, vm.name.as_str()),
                (SELECTED_MODEL_HEADER, decision.model.as_str()),
                (REASON_HEADER, decision.reason.as_str()),
            ] {
                envoy.set_request_header(header, value.as_bytes());
            }
        }
        self.record(
            envoy,
            vm,
            &decision.model,
            &decision.reason,
            decision.evidence.as_deref(),
        );
        if let Some(header) = &self.config.model_header {
            envoy.set_request_header(header, decision.model.as_bytes());
        }
        if self.config.clear_route_cache {
            envoy.clear_route_cache();
        }
        self.count(envoy, &vm.name, &decision.model, outcome);
    }

    /// Records the decision in dynamic metadata and filter state.
    fn record<EHF: EnvoyHttpFilter>(
        &self,
        envoy: &mut EHF,
        vm: &VirtualModel,
        model: &str,
        reason: &str,
        evidence: Option<&str>,
    ) {
        let fields = [
            (
                "virtual_model",
                VIRTUAL_MODEL_FILTER_STATE,
                vm.name.as_str(),
            ),
            ("selected_model", SELECTED_MODEL_FILTER_STATE, model),
            ("reason", REASON_FILTER_STATE, reason),
        ];
        for (key, filter_state, value) in fields {
            envoy.set_dynamic_metadata_string(METADATA_NAMESPACE, key, value);
            if !envoy.set_filter_state_bytes(filter_state.as_bytes(), value.as_bytes()) {
                envoy_log_warn!("switchyard: could not set the {filter_state} filter state");
            }
        }
        if let Some(evidence) = evidence {
            envoy.set_dynamic_metadata_string(METADATA_NAMESPACE, "evidence", evidence);
            if !envoy.set_filter_state_bytes(EVIDENCE_FILTER_STATE.as_bytes(), evidence.as_bytes())
            {
                envoy_log_warn!("switchyard: could not set the evidence filter state");
            }
        }
    }

    fn count<EHF: EnvoyHttpFilter>(
        &self,
        envoy: &mut EHF,
        virtual_model: &str,
        selected: &str,
        outcome: &str,
    ) {
        if let Some(id) = self.decisions {
            let _ = envoy.increment_counter_vec(id, &[virtual_model, selected, outcome], 1);
        }
    }

    fn count_judge<EHF: EnvoyHttpFilter>(&self, envoy: &EHF, judge: &str, outcome: &str) {
        if let Some(id) = self.judge_calls {
            let _ = envoy.increment_counter_vec(id, &[judge, outcome], 1);
        }
    }
}

impl<EHF: EnvoyHttpFilter> HttpFilter<EHF> for Filter {
    fn on_request_headers(
        &mut self,
        envoy: &mut EHF,
        end_of_stream: bool,
    ) -> abi::envoy_dynamic_module_type_on_http_filter_request_headers_status {
        // The decision headers are set only by this filter. Remove the ones sent by the client so
        // they never reach the upstream when the filter does not route the request.
        for header in [VIRTUAL_MODEL_HEADER, SELECTED_MODEL_HEADER, REASON_HEADER] {
            envoy.remove_request_header(header);
        }
        if end_of_stream {
            return abi::envoy_dynamic_module_type_on_http_filter_request_headers_status::Continue;
        }
        self.format = self.handled_format(envoy);
        if self.format.is_none() {
            return abi::envoy_dynamic_module_type_on_http_filter_request_headers_status::Continue;
        }
        // Hold the headers until the body is processed, as the decision may change them.
        abi::envoy_dynamic_module_type_on_http_filter_request_headers_status::StopIteration
    }

    fn on_request_body(
        &mut self,
        envoy: &mut EHF,
        end_of_stream: bool,
    ) -> abi::envoy_dynamic_module_type_on_http_filter_request_body_status {
        if self.run.is_some() {
            // The body is complete, but trailers may still come while routing runs.
            return abi::envoy_dynamic_module_type_on_http_filter_request_body_status::StopIterationAndBuffer;
        }
        if self.format.is_none() {
            return abi::envoy_dynamic_module_type_on_http_filter_request_body_status::Continue;
        }
        if !end_of_stream {
            return abi::envoy_dynamic_module_type_on_http_filter_request_body_status::StopIterationAndBuffer;
        }
        match self.process(envoy, true) {
            Action::Continue => {
                abi::envoy_dynamic_module_type_on_http_filter_request_body_status::Continue
            }
            Action::Stop => {
                abi::envoy_dynamic_module_type_on_http_filter_request_body_status::StopIterationNoBuffer
            }
            // Buffer the last chunk, so the whole body is in the buffer when routing resumes.
            Action::Wait => {
                abi::envoy_dynamic_module_type_on_http_filter_request_body_status::StopIterationAndBuffer
            }
        }
    }

    fn on_request_trailers(
        &mut self,
        envoy: &mut EHF,
    ) -> abi::envoy_dynamic_module_type_on_http_filter_request_trailers_status {
        if self.run.is_some() {
            return abi::envoy_dynamic_module_type_on_http_filter_request_trailers_status::StopIteration;
        }
        // With trailers, the body callback never sees the end of the stream, so the buffered body
        // is processed here.
        match self.process(envoy, false) {
            Action::Continue => {
                abi::envoy_dynamic_module_type_on_http_filter_request_trailers_status::Continue
            }
            Action::Stop | Action::Wait => {
                abi::envoy_dynamic_module_type_on_http_filter_request_trailers_status::StopIteration
            }
        }
    }

    fn on_response_headers(
        &mut self,
        envoy: &mut EHF,
        _end_of_stream: bool,
    ) -> abi::envoy_dynamic_module_type_on_http_filter_response_headers_status {
        for (name, value) in self.routing_headers() {
            envoy.set_response_header(name, value.as_bytes());
        }
        abi::envoy_dynamic_module_type_on_http_filter_response_headers_status::Continue
    }

    fn on_scheduled(&mut self, envoy: &mut EHF, event_id: u64) {
        match event_id {
            EVENT_CALLOUTS => self.send_callouts(envoy),
            EVENT_DONE => self.complete(envoy),
            _ => {}
        }
    }

    fn on_http_callout_done(
        &mut self,
        envoy: &mut EHF,
        callout_id: u64,
        result: abi::envoy_dynamic_module_type_http_callout_result,
        response_headers: Option<&[(EnvoyBuffer, EnvoyBuffer)]>,
        response_body: Option<&[EnvoyBuffer]>,
    ) {
        let Some((target, reply)) = self.callouts.remove(&callout_id) else {
            return;
        };
        use abi::envoy_dynamic_module_type_http_callout_result as CalloutResultCode;
        match result {
            CalloutResultCode::Success => {}
            CalloutResultCode::Reset => {
                self.count_judge(envoy, &target, "timeout");
                let _ = reply.send(Err("the call was reset or timed out".into()));
                return;
            }
            CalloutResultCode::ExceedResponseBufferLimit => {
                self.count_judge(envoy, &target, "error");
                let _ = reply.send(Err("the response is too large".into()));
                return;
            }
        }
        let status = response_headers
            .unwrap_or_default()
            .iter()
            .find(|(k, _)| k.as_slice() == b":status")
            .and_then(|(_, v)| std::str::from_utf8(v.as_slice()).ok()?.parse().ok())
            .unwrap_or(0);
        let body = response_body
            .unwrap_or_default()
            .iter()
            .flat_map(|chunk| chunk.as_slice().iter().copied())
            .collect();
        self.count_judge(
            envoy,
            &target,
            if (200..300).contains(&status) {
                "ok"
            } else {
                "error"
            },
        );
        let _ = reply.send(Ok(HttpReply { status, body }));
    }

    fn on_stream_complete(&mut self, _envoy: &mut EHF) {
        if let Some(run) = self.run.take() {
            run.task.abort();
        }
        self.callouts.clear();
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::HashMap;
    use std::sync::Mutex;

    type HeadersStatus = abi::envoy_dynamic_module_type_on_http_filter_request_headers_status;
    type BodyStatus = abi::envoy_dynamic_module_type_on_http_filter_request_body_status;
    type TrailersStatus = abi::envoy_dynamic_module_type_on_http_filter_request_trailers_status;

    /// The observable effects of the filter on the request.
    #[derive(Default)]
    struct Recorded {
        body: Option<Vec<u8>>,
        headers: HashMap<String, String>,
        removed_headers: Vec<String>,
        metadata: HashMap<String, String>,
        filter_state: HashMap<String, String>,
        cleared_route_cache: bool,
        counters: Vec<Vec<String>>,
        local_reply: Option<(u32, String)>,
    }

    struct Request {
        headers: Vec<(&'static str, &'static str)>,
        body: Vec<u8>,
        /// Whether the body arrives in the trailers callback instead of the last body callback.
        trailers: bool,
    }

    impl Request {
        fn chat(body: &str) -> Self {
            Self {
                headers: vec![
                    (":method", "POST"),
                    (":path", "/v1/chat/completions"),
                    ("content-type", "application/json"),
                    ("content-length", "1"),
                ],
                body: body.as_bytes().to_vec(),
                trailers: false,
            }
        }

        fn with_header(mut self, name: &'static str, value: &'static str) -> Self {
            self.headers.retain(|(n, _)| *n != name);
            self.headers.push((name, value));
            self
        }
    }

    const CONFIG: &str = r#"{
        "virtual_models": [
            {
                "name": "coding-auto",
                "match_headers": [{"name": "x-tier", "value": "free"}],
                "targets": {"only": "free-model"},
                "algorithm": {"type": "passthrough", "target": "only"}
            },
            {
                "name": "coding-auto",
                "targets": {"efficient": "small-model", "capable": "large-model"},
                "algorithm": {"type": "random", "targets": ["efficient", "capable"], "weights": [0.0, 1.0]}
            }
        ]
    }"#;

    fn filter(config: &str) -> Filter {
        Filter::new(
            Arc::new(Config::parse(config.as_bytes()).unwrap()),
            None,
            None,
        )
    }

    /// Runs a request through the filter with a mock Envoy and returns the effects.
    fn run(
        filter: &mut Filter,
        req: Request,
    ) -> (
        HeadersStatus,
        Option<BodyStatus>,
        Option<TrailersStatus>,
        Recorded,
    ) {
        let recorded = Arc::new(Mutex::new(Recorded::default()));
        let mut envoy = MockEnvoyHttpFilter::new();

        let headers: HashMap<String, String> = req
            .headers
            .iter()
            .map(|(k, v)| (k.to_string(), v.to_string()))
            .collect();
        envoy
            .expect_get_request_header_value()
            .returning(move |name| {
                headers
                    .get(name)
                    .map(|v| EnvoyBuffer::new(v.clone().leak().as_bytes()))
            });

        // The body is split in two chunks: the first is buffered, the second is received in the
        // last body callback, unless trailers follow, in which case it is all buffered and, as
        // in Envoy, there is no received body in the trailers callback.
        let (first, second) = req.body.split_at(req.body.len() / 2);
        let (buffered, received): (&'static mut [u8], &'static mut [u8]) = if req.trailers {
            (req.body.clone().leak(), Vec::new().leak())
        } else {
            (first.to_vec().leak(), second.to_vec().leak())
        };
        let buffered_ptr = buffered as *mut [u8] as *mut u8 as usize;
        let buffered_len = buffered.len();
        let received_ptr = received as *mut [u8] as *mut u8 as usize;
        let received_len = received.len();
        envoy.expect_get_buffered_request_body().returning(move || {
            Some(vec![unsafe {
                EnvoyMutBuffer::new_from_raw(buffered_ptr as *mut u8, buffered_len)
            }])
        });
        envoy.expect_get_received_request_body().returning(move || {
            Some(vec![unsafe {
                EnvoyMutBuffer::new_from_raw(received_ptr as *mut u8, received_len)
            }])
        });
        envoy
            .expect_received_buffered_request_body()
            .returning(|| false);
        envoy
            .expect_get_buffered_request_body_size()
            .returning(move || buffered_len);
        envoy
            .expect_get_received_request_body_size()
            .returning(move || received_len);
        envoy
            .expect_drain_buffered_request_body()
            .returning(|_| true);
        envoy
            .expect_drain_received_request_body()
            .returning(|_| true);
        let r = Arc::clone(&recorded);
        envoy
            .expect_append_received_request_body()
            .returning(move |b| {
                r.lock().unwrap().body = Some(b.to_vec());
                true
            });
        let r = Arc::clone(&recorded);
        envoy
            .expect_append_buffered_request_body()
            .returning(move |b| {
                r.lock().unwrap().body = Some(b.to_vec());
                true
            });
        let r = Arc::clone(&recorded);
        envoy.expect_remove_request_header().returning(move |k| {
            r.lock().unwrap().removed_headers.push(k.to_string());
            true
        });
        let r = Arc::clone(&recorded);
        envoy.expect_set_request_header().returning(move |k, v| {
            r.lock()
                .unwrap()
                .headers
                .insert(k.to_string(), String::from_utf8_lossy(v).into_owned());
            true
        });
        let r = Arc::clone(&recorded);
        envoy
            .expect_set_dynamic_metadata_string()
            .returning(move |ns, k, v| {
                assert_eq!(ns, METADATA_NAMESPACE);
                r.lock()
                    .unwrap()
                    .metadata
                    .insert(k.to_string(), v.to_string());
            });
        let r = Arc::clone(&recorded);
        envoy
            .expect_set_filter_state_bytes()
            .returning(move |k, v| {
                r.lock().unwrap().filter_state.insert(
                    String::from_utf8_lossy(k).into_owned(),
                    String::from_utf8_lossy(v).into_owned(),
                );
                true
            });
        let r = Arc::clone(&recorded);
        envoy
            .expect_clear_route_cache()
            .returning(move || r.lock().unwrap().cleared_route_cache = true);
        let r = Arc::clone(&recorded);
        envoy
            .expect_increment_counter_vec()
            .returning(move |_, labels, _| {
                r.lock()
                    .unwrap()
                    .counters
                    .push(labels.iter().map(|l| l.to_string()).collect());
                Ok(())
            });
        let r = Arc::clone(&recorded);
        envoy
            .expect_send_response()
            .returning(move |status, _, body, _| {
                r.lock().unwrap().local_reply = Some((
                    status,
                    String::from_utf8_lossy(body.unwrap_or_default()).into_owned(),
                ));
            });

        let headers_status = filter.on_request_headers(&mut envoy, false);
        let mut body_status = None;
        let mut trailers_status = None;
        if headers_status == HeadersStatus::StopIteration {
            assert_eq!(
                filter.on_request_body(&mut envoy, false),
                BodyStatus::StopIterationAndBuffer
            );
            if req.trailers {
                trailers_status = Some(filter.on_request_trailers(&mut envoy));
            } else {
                body_status = Some(filter.on_request_body(&mut envoy, true));
            }
        }
        drop(envoy);
        let recorded = Arc::try_unwrap(recorded)
            .ok()
            .unwrap()
            .into_inner()
            .unwrap();
        (headers_status, body_status, trailers_status, recorded)
    }

    fn body_json(recorded: &Recorded) -> Value {
        serde_json::from_slice(recorded.body.as_ref().expect("body was not rewritten")).unwrap()
    }

    #[test]
    fn rewrites_virtual_model() {
        let mut f = filter(CONFIG);
        f.decisions = Some(EnvoyCounterVecId(0));
        let (headers, body, _, rec) = run(
            &mut f,
            Request::chat(
                r#"{"model":"coding-auto","messages":[{"role":"user","content":"hi"}],"custom":1}"#,
            ),
        );
        assert_eq!(headers, HeadersStatus::StopIteration);
        assert_eq!(body, Some(BodyStatus::Continue));
        let rewritten = body_json(&rec);
        assert_eq!(rewritten["model"], "large-model");
        // Fields Switchyard does not model are kept.
        assert_eq!(rewritten["custom"], 1);
        assert_eq!(
            rec.headers["content-length"],
            rec.body.as_ref().unwrap().len().to_string()
        );
        assert_eq!(rec.headers[VIRTUAL_MODEL_HEADER], "coding-auto");
        assert_eq!(rec.headers[SELECTED_MODEL_HEADER], "large-model");
        assert!(
            rec.headers[REASON_HEADER].starts_with("random"),
            "{:?}",
            rec.headers
        );
        assert_eq!(rec.metadata["virtual_model"], "coding-auto");
        assert_eq!(rec.metadata["selected_model"], "large-model");
        assert!(!rec.cleared_route_cache);
        assert_eq!(rec.filter_state[VIRTUAL_MODEL_FILTER_STATE], "coding-auto");
        assert_eq!(rec.filter_state[SELECTED_MODEL_FILTER_STATE], "large-model");
        assert_eq!(
            rec.counters,
            vec![vec!["coding-auto", "large-model", "routed"]]
        );
    }

    #[test]
    fn decision_only_keeps_the_body() {
        let config = r#"{
            "virtual_models": [{"name": "v", "targets": {"a": "m"}, "algorithm": {"type": "passthrough", "target": "a"}}],
            "rewrite_body": false,
            "model_header": "x-model"
        }"#;
        let (headers, body, _, rec) = run(
            &mut filter(config),
            Request::chat(r#"{"model":"v","messages":[{"role":"user","content":"hi"}]}"#),
        );
        assert_eq!(headers, HeadersStatus::StopIteration);
        assert_eq!(body, Some(BodyStatus::Continue));
        assert!(rec.body.is_none());
        assert!(!rec.headers.contains_key("content-length"));
        assert_eq!(rec.headers["x-model"], "m");
        assert_eq!(rec.headers[VIRTUAL_MODEL_HEADER], "v");
        assert_eq!(rec.headers[SELECTED_MODEL_HEADER], "m");
        assert_eq!(rec.metadata["selected_model"], "m");
        assert_eq!(rec.filter_state[VIRTUAL_MODEL_FILTER_STATE], "v");
        assert_eq!(rec.filter_state[SELECTED_MODEL_FILTER_STATE], "m");
        assert!(rec.filter_state[REASON_FILTER_STATE].starts_with("passthrough"));
    }

    #[test]
    fn removes_client_decision_headers() {
        // Even requests the filter does not handle never carry client-set decision headers.
        let (headers, _, _, rec) = run(
            &mut filter(CONFIG),
            Request::chat(r#"{"model":"not-virtual"}"#)
                .with_header(":method", "GET")
                .with_header(SELECTED_MODEL_HEADER, "spoofed"),
        );
        assert_eq!(headers, HeadersStatus::Continue);
        assert_eq!(
            rec.removed_headers,
            vec![VIRTUAL_MODEL_HEADER, SELECTED_MODEL_HEADER, REASON_HEADER]
        );
    }

    #[test]
    fn header_match_selects_the_variant() {
        let (_, _, _, rec) = run(
            &mut filter(CONFIG),
            Request::chat(r#"{"model":"coding-auto","messages":[{"role":"user","content":"hi"}]}"#)
                .with_header("x-tier", "free"),
        );
        assert_eq!(body_json(&rec)["model"], "free-model");
    }

    #[test]
    fn body_with_trailers() {
        let mut req =
            Request::chat(r#"{"model":"coding-auto","messages":[{"role":"user","content":"hi"}]}"#);
        req.trailers = true;
        let (_, body, trailers, rec) = run(&mut filter(CONFIG), req);
        assert_eq!(body, None);
        assert_eq!(trailers, Some(TrailersStatus::Continue));
        assert_eq!(body_json(&rec)["model"], "large-model");
    }

    #[test]
    fn model_header_and_route_cache() {
        let config = r#"{
            "virtual_models": [{"name": "v", "targets": {"a": "m"}, "algorithm": {"type": "passthrough", "target": "a"}}],
            "model_header": "x-model",
            "clear_route_cache": true,
            "decision_headers": false
        }"#;
        let (_, _, _, rec) = run(
            &mut filter(config),
            Request::chat(r#"{"model":"v","messages":[{"role":"user","content":"hi"}]}"#)
                .with_header("content-length", "")
                .with_header(":path", "/v1/chat/completions?x=1"),
        );
        assert_eq!(body_json(&rec)["model"], "m");
        assert_eq!(rec.headers["x-model"], "m");
        assert!(!rec.headers.contains_key(VIRTUAL_MODEL_HEADER));
        assert!(rec.cleared_route_cache);
    }

    #[test]
    fn ignored_requests() {
        let cases = [
            Request::chat("{}").with_header(":method", "GET"),
            Request::chat("{}").with_header(":path", "/v1/embeddings"),
            Request::chat("{}").with_header("content-type", "text/plain"),
            Request::chat("{}").with_header("content-encoding", "gzip"),
        ];
        for req in cases {
            let (headers, body, _, rec) = run(&mut filter(CONFIG), req);
            assert_eq!(headers, HeadersStatus::Continue);
            assert_eq!(body, None);
            assert!(rec.body.is_none());
        }

        // Headers-only requests.
        let mut envoy = MockEnvoyHttpFilter::new();
        envoy.expect_remove_request_header().returning(|_| true);
        assert_eq!(
            filter(CONFIG).on_request_headers(&mut envoy, true),
            HeadersStatus::Continue
        );
        // Body and trailers callbacks of ignored requests.
        let mut f = filter(CONFIG);
        assert_eq!(f.on_request_body(&mut envoy, true), BodyStatus::Continue);
        assert_eq!(f.on_request_trailers(&mut envoy), TrailersStatus::Continue);
    }

    #[test]
    fn unchanged_bodies() {
        for body in [
            "not json",
            r#"{"messages":[]}"#,
            r#"{"model":"gpt-5","messages":[]}"#,
            r#"{"model":42}"#,
        ] {
            let (headers, status, _, rec) = run(&mut filter(CONFIG), Request::chat(body));
            assert_eq!(headers, HeadersStatus::StopIteration, "{body}");
            assert_eq!(status, Some(BodyStatus::Continue), "{body}");
            assert!(rec.body.is_none(), "{body}");
        }
    }

    // A request Switchyard cannot decode makes routing fail.
    const UNDECODABLE: &str = r#"{"model":"v","messages":"not-a-list"}"#;

    fn failing_config(extra: &str) -> String {
        format!(
            r#"{{"virtual_models": [{{"name": "v", "targets": {{"a": "m", "b": "n"}}, "algorithm": {{"type": "passthrough", "target": "a"}}{extra}}}]}}"#
        )
    }

    #[test]
    fn failure_mode_use_default() {
        let mut f = filter(&failing_config(r#", "default_model": "n""#));
        f.decisions = Some(EnvoyCounterVecId(0));
        let (_, status, _, rec) = run(&mut f, Request::chat(UNDECODABLE));
        assert_eq!(status, Some(BodyStatus::Continue));
        assert_eq!(body_json(&rec)["model"], "n");
        assert_eq!(rec.headers[REASON_HEADER], "fallback:default");
        assert_eq!(rec.counters, vec![vec!["v", "n", "default"]]);
    }

    #[test]
    fn failure_mode_passthrough() {
        let mut f = filter(&failing_config(""));
        f.decisions = Some(EnvoyCounterVecId(0));
        let (_, status, _, rec) = run(&mut f, Request::chat(UNDECODABLE));
        assert_eq!(status, Some(BodyStatus::Continue));
        assert!(rec.body.is_none());
        assert_eq!(rec.counters, vec![vec!["v", "", "passthrough"]]);
    }

    #[test]
    fn failure_mode_reject() {
        let (_, status, _, rec) = run(
            &mut filter(&failing_config(r#", "failure_mode": "reject""#)),
            Request::chat(UNDECODABLE),
        );
        assert_eq!(status, Some(BodyStatus::StopIterationNoBuffer));
        assert!(rec.body.is_none());
        let (code, reply) = rec.local_reply.unwrap();
        assert_eq!(code, 503);
        assert!(reply.contains("model_router_unavailable"), "{reply}");

        let mut req = Request::chat(UNDECODABLE);
        req.trailers = true;
        let (_, _, trailers, _) = run(
            &mut filter(&failing_config(r#", "failure_mode": "reject""#)),
            req,
        );
        assert_eq!(trailers, Some(TrailersStatus::StopIteration));
    }

    // ------------------------- Algorithms that call models -------------------------

    struct FakeSpan;

    impl EnvoySpan for FakeSpan {
        fn set_tag(&self, _key: &str, _value: &str) {}
        fn set_operation(&self, _operation: &str) {}
        fn log(&self, _event: &str) {}
        fn set_sampled(&self, _sampled: bool) {}
        fn disable_local_decision(&self) {}
        fn get_baggage(&self, _key: &str) -> Option<String> {
            None
        }
        fn set_baggage(&self, _key: &str, _value: &str) {}
        fn get_trace_id(&self) -> Option<String> {
            Some("abc".into())
        }
        fn get_span_id(&self) -> Option<String> {
            Some("def".into())
        }
        fn spawn_child(&self, _operation_name: &str) -> Option<Box<dyn EnvoyChildSpan + '_>> {
            None
        }
    }

    /// A callout sent by the filter.
    #[derive(Debug, Clone)]
    struct SentCallout {
        cluster: String,
        headers: HashMap<String, String>,
        body: Value,
        timeout: u64,
    }

    /// The effects of an asynchronous routing run.
    #[derive(Default)]
    struct AsyncRecorded {
        callouts: Vec<SentCallout>,
        continued: bool,
        local_reply: Option<(u32, HashMap<String, String>, String)>,
        body: Option<Vec<u8>>,
        response_headers: HashMap<String, String>,
        metadata_numbers: HashMap<String, f64>,
        metadata: HashMap<String, String>,
        filter_state: HashMap<String, String>,
        judge_counters: Vec<Vec<String>>,
        counters: Vec<Vec<String>>,
    }

    const JUDGE_CONFIG: &str = r#"{
        "virtual_models": [{
            "name": "coding-auto",
            "targets": {"efficient": "small-model", "capable": "large-model", "judge": "judge-model"},
            "algorithm": {"type": "llm_classifier", "classifier_target": "judge", "strong_target": "capable",
                          "weak_target": "efficient", "base_threshold": 0.5, "fail_open": false},
            "default_model": "large-model"
        }],
        "model_calls": {
            "cluster": "loopback",
            "authority": "gateway.local",
            "timeout_ms": 1234,
            "forward_headers": ["authorization", "x-tenant"],
            "headers": {"x-extra": "1"}
        },
        "routing_headers": true
    }"#;

    const ADVISOR_CONFIG: &str = r#"{
        "virtual_models": [{
            "name": "coding-auto",
            "targets": {"executor": "small-model", "advisor": "judge-model"},
            "algorithm": {"type": "advisor", "executor_target": "executor", "advisor_target": "advisor"}
        }],
        "model_calls": {"cluster": "loopback", "trace_context": "b3"}
    }"#;

    type CalloutResponder = dyn Fn(
        &SentCallout,
    ) -> (
        abi::envoy_dynamic_module_type_http_callout_result,
        u16,
        String,
    );

    /// Runs a request through a filter whose algorithm calls models. Callouts are answered by
    /// `respond`, and scheduled events are delivered until the run completes.
    fn run_async(
        config: &str,
        req: Request,
        respond: &CalloutResponder,
        init_result: abi::envoy_dynamic_module_type_http_callout_init_result,
    ) -> (BodyStatus, AsyncRecorded) {
        let mut filter = filter(config);
        filter.decisions = Some(EnvoyCounterVecId(0));
        filter.judge_calls = Some(EnvoyCounterVecId(1));
        let recorded = Arc::new(Mutex::new(AsyncRecorded::default()));
        let events = Arc::new(Mutex::new(std::collections::VecDeque::<u64>::new()));
        let (tx, rx) = std::sync::mpsc::channel::<u64>();
        let mut envoy = MockEnvoyHttpFilter::new();

        let headers: HashMap<String, String> = req
            .headers
            .iter()
            .map(|(k, v)| (k.to_string(), v.to_string()))
            .collect();
        let values = headers.clone();
        envoy
            .expect_get_request_header_value()
            .returning(move |name| {
                headers
                    .get(name)
                    .map(|v| EnvoyBuffer::new(v.clone().leak().as_bytes()))
            });
        envoy
            .expect_get_request_header_values()
            .returning(move |name| {
                values
                    .get(name)
                    .map(|v| vec![EnvoyBuffer::new(v.clone().leak().as_bytes())])
                    .unwrap_or_default()
            });
        let body: &'static mut [u8] = req.body.clone().leak();
        let body_ptr = body as *mut [u8] as *mut u8 as usize;
        let body_len = body.len();
        envoy.expect_get_buffered_request_body().returning(move || {
            Some(vec![unsafe {
                EnvoyMutBuffer::new_from_raw(body_ptr as *mut u8, body_len)
            }])
        });
        envoy
            .expect_get_received_request_body()
            .returning(|| Some(vec![]));
        envoy
            .expect_received_buffered_request_body()
            .returning(|| true);
        envoy
            .expect_get_buffered_request_body_size()
            .returning(move || body_len);
        envoy
            .expect_get_received_request_body_size()
            .returning(|| 0);
        envoy
            .expect_drain_buffered_request_body()
            .returning(|_| true);
        envoy
            .expect_drain_received_request_body()
            .returning(|_| true);
        let r = Arc::clone(&recorded);
        envoy
            .expect_append_buffered_request_body()
            .returning(move |b| {
                r.lock().unwrap().body = Some(b.to_vec());
                true
            });
        envoy.expect_remove_request_header().returning(|_| true);
        envoy.expect_set_request_header().returning(|_, _| true);
        let r = Arc::clone(&recorded);
        envoy
            .expect_set_dynamic_metadata_string()
            .returning(move |_, k, v| {
                r.lock()
                    .unwrap()
                    .metadata
                    .insert(k.to_string(), v.to_string());
            });
        let r = Arc::clone(&recorded);
        envoy
            .expect_set_dynamic_metadata_number()
            .returning(move |_, k, v| {
                r.lock().unwrap().metadata_numbers.insert(k.to_string(), v);
            });
        let r = Arc::clone(&recorded);
        envoy
            .expect_set_filter_state_bytes()
            .returning(move |k, v| {
                r.lock().unwrap().filter_state.insert(
                    String::from_utf8_lossy(k).into_owned(),
                    String::from_utf8_lossy(v).into_owned(),
                );
                true
            });
        let r = Arc::clone(&recorded);
        envoy
            .expect_increment_counter_vec()
            .returning(move |id, labels, value| {
                assert_eq!(value, 1);
                let mut r = r.lock().unwrap();
                let counters = if id == EnvoyCounterVecId(1) {
                    &mut r.judge_counters
                } else {
                    &mut r.counters
                };
                counters.push(labels.iter().map(|l| l.to_string()).collect());
                Ok(())
            });
        envoy
            .expect_get_active_span()
            .returning(|| Some(Box::new(FakeSpan)));
        envoy.expect_new_scheduler().returning(move || {
            let tx = Mutex::new(tx.clone());
            let mut scheduler = MockEnvoyHttpFilterScheduler::new();
            scheduler.expect_commit().returning(move |id| {
                let _ = tx.lock().unwrap().send(id);
            });
            Box::new(scheduler)
        });
        let r = Arc::clone(&recorded);
        let pending = Arc::clone(&events);
        let next_id = Arc::new(Mutex::new(0u64));
        envoy
            .expect_send_http_callout()
            .returning(move |cluster, headers, body, timeout| {
                let sent = SentCallout {
                    cluster: cluster.to_string(),
                    headers: headers
                        .iter()
                        .map(|(k, v)| (k.to_string(), String::from_utf8_lossy(v).into_owned()))
                        .collect(),
                    body: serde_json::from_slice(body.unwrap()).unwrap(),
                    timeout,
                };
                r.lock().unwrap().callouts.push(sent);
                let mut id = next_id.lock().unwrap();
                *id += 1;
                // The callout completes after the scheduled event is handled.
                pending.lock().unwrap().push_back(*id);
                (init_result, *id)
            });
        envoy.expect_continue_decoding().returning({
            let r = Arc::clone(&recorded);
            move || r.lock().unwrap().continued = true
        });
        let r = Arc::clone(&recorded);
        envoy
            .expect_send_response()
            .returning(move |status, headers, body, _| {
                r.lock().unwrap().local_reply = Some((
                    status,
                    headers
                        .iter()
                        .map(|(k, v)| (k.to_string(), String::from_utf8_lossy(v).into_owned()))
                        .collect(),
                    String::from_utf8_lossy(body.unwrap_or_default()).into_owned(),
                ));
            });
        let r = Arc::clone(&recorded);
        envoy.expect_set_response_header().returning(move |k, v| {
            r.lock()
                .unwrap()
                .response_headers
                .insert(k.to_string(), String::from_utf8_lossy(v).into_owned());
            true
        });

        assert_eq!(
            filter.on_request_headers(&mut envoy, false),
            HeadersStatus::StopIteration
        );
        let status = filter.on_request_body(&mut envoy, true);
        if status != BodyStatus::StopIterationAndBuffer {
            return (
                status,
                Arc::try_unwrap(recorded)
                    .ok()
                    .unwrap()
                    .into_inner()
                    .unwrap(),
            );
        }
        // Body chunks and trailers arriving while routing runs are held.
        assert_eq!(
            filter.on_request_body(&mut envoy, true),
            BodyStatus::StopIterationAndBuffer
        );
        assert_eq!(
            filter.on_request_trailers(&mut envoy),
            TrailersStatus::StopIteration
        );

        // Deliver the scheduled events, and complete the callouts, until the run is done.
        loop {
            let event = rx
                .recv_timeout(std::time::Duration::from_secs(10))
                .expect("no event");
            filter.on_scheduled(&mut envoy, event);
            let ids: Vec<u64> = events.lock().unwrap().drain(..).collect();
            for id in ids {
                if init_result != abi::envoy_dynamic_module_type_http_callout_init_result::Success {
                    continue;
                }
                let sent = recorded.lock().unwrap().callouts[id as usize - 1].clone();
                let (result, code, body) = respond(&sent);
                let code = code.to_string();
                let headers = [(
                    EnvoyBuffer::new(b":status"),
                    EnvoyBuffer::new(code.as_bytes()),
                )];
                let (first, second) = body.as_bytes().split_at(body.len() / 2);
                let chunks = [EnvoyBuffer::new(first), EnvoyBuffer::new(second)];
                filter.on_http_callout_done(&mut envoy, id, result, Some(&headers), Some(&chunks));
            }
            if event == EVENT_DONE {
                break;
            }
        }
        // A callout completing after the run is ignored.
        filter.on_http_callout_done(
            &mut envoy,
            999,
            abi::envoy_dynamic_module_type_http_callout_result::Reset,
            None,
            None,
        );
        filter.on_response_headers(&mut envoy, false);
        filter.on_stream_complete(&mut envoy);
        drop(envoy);
        (
            status,
            Arc::try_unwrap(recorded)
                .ok()
                .unwrap()
                .into_inner()
                .unwrap(),
        )
    }

    const SUCCESS: abi::envoy_dynamic_module_type_http_callout_init_result =
        abi::envoy_dynamic_module_type_http_callout_init_result::Success;
    const OK: abi::envoy_dynamic_module_type_http_callout_result =
        abi::envoy_dynamic_module_type_http_callout_result::Success;

    fn completion(model: &str, text: &str) -> String {
        String::from_utf8(
            crate::host::testing::chat_completion(model, text)
                .unwrap()
                .body,
        )
        .unwrap()
    }

    fn judge_says(
        p_solve: f64,
    ) -> impl Fn(
        &SentCallout,
    ) -> (
        abi::envoy_dynamic_module_type_http_callout_result,
        u16,
        String,
    ) {
        move |_| {
            let verdict = serde_json::json!({"crux": "c", "primary_rule": "SUP-1",
                "capability_boundary": "supported", "p_solve": p_solve});
            (OK, 200, completion("judge-model", &verdict.to_string()))
        }
    }

    fn judge_request() -> Request {
        Request::chat(r#"{"model":"coding-auto","stream":true,"messages":[{"role":"user","content":"fix it"}]}"#)
            .with_header(":authority", "client.local")
            .with_header("authorization", "Bearer t")
            .with_header("x-tenant", "acme")
            .with_header("x-request-id", "req-1")
    }

    #[test]
    fn judge_calls_go_through_callouts() {
        let (status, rec) = run_async(JUDGE_CONFIG, judge_request(), &judge_says(0.01), SUCCESS);
        assert_eq!(status, BodyStatus::StopIterationAndBuffer);
        assert!(rec.continued);
        assert_eq!(
            serde_json::from_slice::<Value>(rec.body.as_ref().unwrap()).unwrap()["model"],
            "large-model"
        );
        assert_eq!(
            rec.counters,
            vec![vec!["coding-auto", "large-model", "routed"]]
        );

        let call = &rec.callouts[0];
        assert_eq!(call.cluster, "loopback");
        assert_eq!(call.timeout, 1234);
        assert_eq!(call.body["model"], "judge-model");
        assert_eq!(call.body["stream"], false);
        for (name, value) in [
            (":method", "POST"),
            (":path", "/v1/chat/completions"),
            ("host", "gateway.local"),
            ("authorization", "Bearer t"),
            ("x-tenant", "acme"),
            ("x-extra", "1"),
            (CALL_HEADER, "judge"),
            (PARENT_ID_HEADER, "req-1"),
            (
                "traceparent",
                "00-00000000000000000000000000000abc-0000000000000def-01",
            ),
        ] {
            assert_eq!(
                call.headers.get(name).map(String::as_str),
                Some(value),
                "{name}"
            );
        }

        // The filter removes client-sent decision headers, so calls looping back through it
        // never carry them.
        assert!(!call.headers.contains_key(VIRTUAL_MODEL_HEADER));

        assert_eq!(rec.metadata_numbers["routing_calls"], 1.0);
        assert_eq!(rec.metadata_numbers["routing_input_tokens"], 100.0);
        assert_eq!(rec.metadata_numbers["routing_output_tokens"], 10.0);
        assert_eq!(rec.metadata_numbers["routing_total_tokens"], 110.0);
        assert!(rec.metadata_numbers.contains_key("routing_latency_ms"));
        assert_eq!(rec.metadata["routing_models"], "judge-model");
        assert_eq!(rec.response_headers[ROUTING_CALLS_HEADER], "1");
        assert_eq!(rec.response_headers[ROUTING_TOTAL_TOKENS_HEADER], "110");
    }

    #[test]
    fn judge_failures_apply_the_failure_mode() {
        let failing = |_: &SentCallout| {
            (
                abi::envoy_dynamic_module_type_http_callout_result::Reset,
                0,
                String::new(),
            )
        };
        let (_, rec) = run_async(JUDGE_CONFIG, judge_request(), &failing, SUCCESS);
        assert!(rec.continued);
        assert_eq!(
            serde_json::from_slice::<Value>(rec.body.as_ref().unwrap()).unwrap()["model"],
            "large-model"
        );
        assert_eq!(
            rec.counters,
            vec![vec!["coding-auto", "large-model", "default"]]
        );
        assert_eq!(rec.metadata_numbers["routing_calls"], 1.0);

        // Callouts Envoy cannot start fail the call too.
        let (_, rec) = run_async(
            JUDGE_CONFIG,
            judge_request(),
            &|_| unreachable!(),
            abi::envoy_dynamic_module_type_http_callout_init_result::ClusterNotFound,
        );
        assert_eq!(
            rec.counters,
            vec![vec!["coding-auto", "large-model", "default"]]
        );

        let reject = JUDGE_CONFIG.replace(
            r#""default_model": "large-model""#,
            r#""failure_mode": "reject""#,
        );
        let (_, rec) = run_async(&reject, judge_request(), &failing, SUCCESS);
        assert!(!rec.continued);
        assert_eq!(rec.local_reply.unwrap().0, 503);
    }

    #[test]
    fn advisor_answers_with_a_local_response() {
        let respond = |call: &SentCallout| {
            let model = call.body["model"].as_str().unwrap().to_string();
            let text = if model == "judge-model" {
                "APPROVE"
            } else {
                "the answer"
            };
            (OK, 200, completion(&model, text))
        };
        let req = Request::chat(
            r#"{"model":"coding-auto","messages":[{"role":"user","content":"explain"}]}"#,
        )
        .with_header("x-switchyard-session-id", "advisor-filter");
        let (_, rec) = run_async(ADVISOR_CONFIG, req, &respond, SUCCESS);
        assert!(!rec.continued);
        let (status, headers, body) = rec.local_reply.unwrap();
        assert_eq!(status, 200);
        assert_eq!(headers["content-type"], "application/json");
        let body: Value = serde_json::from_str(&body).unwrap();
        assert_eq!(body["choices"][0]["message"]["content"], "the answer");
        assert_eq!(
            rec.counters,
            vec![vec!["coding-auto", "small-model", "answered"]]
        );
        assert_eq!(rec.metadata["selected_model"], "small-model");
        // The executor call is a model call, with B3 trace headers.
        let executor = rec
            .callouts
            .iter()
            .find(|c| c.body["model"] == "small-model")
            .unwrap();
        assert_eq!(executor.headers[CALL_HEADER], "model");
        assert_eq!(executor.headers["x-b3-traceid"], "abc");
        assert_eq!(executor.headers["host"], "loopback");
        assert!(!executor.headers.contains_key("traceparent"));
        // No routing headers unless enabled.
        assert!(rec.response_headers.is_empty());
    }

    #[test]
    fn stream_completion_aborts_the_run() {
        let mut f = filter(JUDGE_CONFIG);
        let mut envoy = MockEnvoyHttpFilter::new();
        envoy.expect_new_scheduler().returning(|| {
            let mut scheduler = MockEnvoyHttpFilterScheduler::new();
            scheduler.expect_commit().returning(|_| ());
            Box::new(scheduler)
        });
        let shared = Arc::new(Mutex::new(Shared::default()));
        let task = runtime().spawn(std::future::pending::<()>());
        f.run = Some(Run {
            virtual_model: 0,
            body: Value::Null,
            shared,
            task: task.abort_handle(),
            usage: Arc::default(),
            started: Instant::now(),
            model_headers: vec![],
            parent_id: None,
            trace: None,
        });
        // Events for a run without an outcome, or of unknown types, do nothing.
        f.on_scheduled(&mut envoy, EVENT_DONE);
        f.on_scheduled(&mut envoy, 42);
        f.on_stream_complete(&mut envoy);
        assert!(f.run.is_none());
        let err = runtime().block_on(task).unwrap_err();
        assert!(err.is_cancelled());
    }

    #[test]
    fn new_http_filter() {
        let config = FilterConfig {
            config: Arc::new(Config::parse(CONFIG.as_bytes()).unwrap()),
            decisions: None,
            judge_calls: None,
        };
        let mut envoy = MockEnvoyHttpFilter::new();
        let _filter = HttpFilterConfig::<MockEnvoyHttpFilter>::new_http_filter(&config, &mut envoy);
    }

    fn configured_judge_config(decision: bool) -> String {
        let mut config: Value = serde_json::from_str(JUDGE_CONFIG).unwrap();
        config["virtual_models"][0]["targets"]
            .as_object_mut()
            .unwrap()
            .remove("judge");
        let target = serde_json::json!({"format":if decision {"system_one"} else {"openai_chat"},"model":"private-model","cluster":"private-judge","authority":"private.example","api_key":{"inline":"private-key"}});
        config[if decision {
            "decision_targets"
        } else {
            "judge_targets"
        }] = serde_json::json!({"judge":target});
        if decision {
            config["virtual_models"][0]["algorithm"]
                .as_object_mut()
                .unwrap()
                .remove("base_threshold");
            config["virtual_models"][0]["algorithm"]["decision"] = serde_json::json!({"cutoff":0.5,"candidates":{"candidate_a":"capable","candidate_b":"efficient"},"evidence":{}});
        }
        config.to_string()
    }

    #[test]
    fn decision_judge_records_evidence_and_own_credentials() {
        let (_, rec) = run_async(
            &configured_judge_config(true),
            judge_request(),
            &|call| {
                assert_eq!(call.body["questions"]["route"]["type"], "choice");
                (OK,200,serde_json::json!({"answers":{"route":{"type":"choice","choice":"advantage","probabilities":{"advantage":0.9,"no_advantage":0.1}}}}).to_string())
            },
            SUCCESS,
        );
        assert!(rec.continued);
        assert_eq!(rec.metadata["selected_model"], "large-model");
        let evidence: Value = serde_json::from_str(&rec.metadata["evidence"]).unwrap();
        assert_eq!(evidence["evidence"]["selected_candidate"], "candidate_a");
        assert_eq!(evidence["judgments"].as_array().unwrap().len(), 1);
        assert_eq!(
            rec.filter_state[EVIDENCE_FILTER_STATE],
            rec.metadata["evidence"]
        );
        assert_eq!(rec.judge_counters, vec![vec!["judge", "ok"]]);
        let call = &rec.callouts[0];
        assert_eq!(call.cluster, "private-judge");
        assert_eq!(call.headers["authorization"], "Bearer private-key");
        assert_eq!(call.headers[CALL_HEADER], "decision");
        assert_eq!(call.headers[PARENT_ID_HEADER], "req-1");
        assert_eq!(
            call.headers["traceparent"],
            "00-00000000000000000000000000000abc-0000000000000def-01"
        );
        assert!(!call.headers.contains_key("x-tenant"));
        assert!(!call.headers.contains_key("x-extra"));
    }

    #[test]
    fn configured_llm_judge_does_not_forward_client_headers() {
        let (_, rec) = run_async(
            &configured_judge_config(false),
            judge_request(),
            &judge_says(0.99),
            SUCCESS,
        );
        assert_eq!(rec.callouts.len(), 1);
        let call = &rec.callouts[0];
        assert_eq!(call.cluster, "private-judge");
        assert_eq!(call.body["model"], "private-model");
        assert_eq!(call.headers["authorization"], "Bearer private-key");
        assert_eq!(call.headers[CALL_HEADER], "judge");
        assert_eq!(call.headers[PARENT_ID_HEADER], "req-1");
        assert!(call.headers.contains_key("traceparent"));
        assert!(!call.headers.contains_key("x-tenant"));
        assert!(!call.headers.contains_key("x-extra"));
        assert_eq!(rec.judge_counters, vec![vec!["judge", "ok"]]);
    }

    #[test]
    fn judge_call_counters_label_success_error_and_timeout() {
        use abi::envoy_dynamic_module_type_http_callout_result as ResultCode;
        for (result,status,body,label) in [
            (OK,200,completion("judge-model",&serde_json::json!({"crux":"c","primary_rule":"SUP-1","capability_boundary":"supported","p_solve":0.9}).to_string()),"ok"),
            (OK,503,"unavailable".into(),"error"),
            (ResultCode::Reset,0,String::new(),"timeout"),
            (ResultCode::ExceedResponseBufferLimit,0,String::new(),"error")
        ] {
            let (_,rec)=run_async(JUDGE_CONFIG,judge_request(),&move |_| (result,status,body.clone()),SUCCESS);
            assert_eq!(rec.judge_counters,vec![vec!["judge-model",label]]);
        }
        let (_, rec) = run_async(
            JUDGE_CONFIG,
            judge_request(),
            &|_| unreachable!(),
            abi::envoy_dynamic_module_type_http_callout_init_result::ClusterNotFound,
        );
        assert_eq!(rec.judge_counters, vec![vec!["judge-model", "error"]]);
    }

    #[test]
    fn eligible_models_header_limits_the_pick() {
        let mut config: Value = serde_json::from_str(CONFIG).unwrap();
        config["eligible_models_header"] = serde_json::json!("x-eligible");
        config["virtual_models"][1]["algorithm"] =
            serde_json::json!({"type":"random","targets":["efficient","capable"]});
        let (_, status, _, rec) = run(
            &mut filter(&config.to_string()),
            Request::chat(r#"{"model":"coding-auto","messages":[{"role":"user","content":"hi"}]}"#)
                .with_header("x-eligible", "small-model"),
        );
        assert_eq!(status, Some(BodyStatus::Continue));
        assert_eq!(body_json(&rec)["model"], "small-model");
    }

    #[test]
    fn advisor_answer_includes_routing_headers_when_enabled() {
        let mut config: Value = serde_json::from_str(ADVISOR_CONFIG).unwrap();
        config["routing_headers"] = serde_json::json!(true);
        let (_, rec) = run_async(
            &config.to_string(),
            Request::chat(r#"{"model":"coding-auto","messages":[{"role":"user","content":"hi"}]}"#)
                .with_header("x-switchyard-session-id", "routing-answer"),
            &|call| {
                (
                    OK,
                    200,
                    completion(call.body["model"].as_str().unwrap(), "APPROVE"),
                )
            },
            SUCCESS,
        );
        let (status, headers, _) = rec.local_reply.unwrap();
        assert_eq!(status, 200);
        assert_eq!(headers[ROUTING_CALLS_HEADER], "2");
        assert_eq!(headers[ROUTING_INPUT_TOKENS_HEADER], "200");
        assert_eq!(headers[ROUTING_OUTPUT_TOKENS_HEADER], "20");
        assert_eq!(headers[ROUTING_TOTAL_TOKENS_HEADER], "220");
        assert_eq!(rec.metadata_numbers["routing_calls"], 2.0);
    }
}
