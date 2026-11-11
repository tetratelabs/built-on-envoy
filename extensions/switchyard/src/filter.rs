// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

//! The HTTP filter: buffers the request body, resolves virtual models and rewrites the request.

use std::sync::Arc;

use envoy_proxy_dynamic_modules_rust_sdk::*;
use serde_json::Value;
use switchyard_protocol::WireFormat;

use crate::config::{Config, FailureMode, VirtualModel};
use crate::router::Decision;

/// Dynamic metadata namespace of the routing decision.
pub const METADATA_NAMESPACE: &str = "io.builtonenvoy.switchyard";
/// Request header carrying the virtual model the client requested.
pub const VIRTUAL_MODEL_HEADER: &str = "x-switchyard-virtual-model";
/// Request header carrying the selected model.
pub const SELECTED_MODEL_HEADER: &str = "x-switchyard-selected-model";
/// Request header carrying the reason for the decision.
pub const REASON_HEADER: &str = "x-switchyard-reason";

/// Outcome label values of the decisions counter.
const OUTCOME_ROUTED: &str = "routed";
const OUTCOME_DEFAULT: &str = "default";
const OUTCOME_PASSTHROUGH: &str = "passthrough";
const OUTCOME_REJECTED: &str = "rejected";

/// The filter configuration shared by all the filter instances.
pub struct FilterConfig {
    pub config: Arc<Config>,
    /// Counter of decisions, by virtual model, selected model and outcome.
    pub decisions: Option<EnvoyCounterVecId>,
}

impl<EHF: EnvoyHttpFilter> HttpFilterConfig<EHF> for FilterConfig {
    fn new_http_filter(&self, _envoy: &mut EHF) -> Box<dyn HttpFilter<EHF>> {
        Box::new(Filter {
            config: Arc::clone(&self.config),
            decisions: self.decisions,
            format: None,
        })
    }
}

/// The per-request filter.
pub struct Filter {
    config: Arc<Config>,
    decisions: Option<EnvoyCounterVecId>,
    /// The API format of the request body, set when the filter handles the request and cleared
    /// once the request has been processed.
    format: Option<WireFormat>,
}

/// What the filter does after processing the request body.
#[derive(Debug, PartialEq)]
enum Action {
    Continue,
    /// A local reply was sent.
    Stop,
}

fn header_value<EHF: EnvoyHttpFilter>(envoy: &EHF, name: &str) -> Option<String> {
    envoy
        .get_request_header_value(name)
        .map(|v| String::from_utf8_lossy(v.as_slice()).into_owned())
}

impl Filter {
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
        match vm.router.route(format, &body, metadata) {
            Ok(decision) => {
                envoy_log_debug!(
                    "switchyard: routed virtual model {} to {} ({})",
                    vm.name,
                    decision.model,
                    decision.reason
                );
                self.apply(
                    envoy,
                    vm,
                    &mut body,
                    decision,
                    OUTCOME_ROUTED,
                    in_body_callback,
                );
                Action::Continue
            }
            Err(err) => self.fail(envoy, vm, &mut body, &err, in_body_callback),
        }
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

    /// Rewrites the request for the selected model.
    fn apply<EHF: EnvoyHttpFilter>(
        &self,
        envoy: &mut EHF,
        vm: &VirtualModel,
        body: &mut Value,
        decision: Decision,
        outcome: &str,
        in_body_callback: bool,
    ) {
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

        if self.config.decision_headers {
            envoy.set_request_header(VIRTUAL_MODEL_HEADER, vm.name.as_bytes());
            envoy.set_request_header(SELECTED_MODEL_HEADER, decision.model.as_bytes());
            envoy.set_request_header(REASON_HEADER, decision.reason.as_bytes());
        }
        envoy.set_dynamic_metadata_string(METADATA_NAMESPACE, "virtual_model", &vm.name);
        envoy.set_dynamic_metadata_string(METADATA_NAMESPACE, "selected_model", &decision.model);
        envoy.set_dynamic_metadata_string(METADATA_NAMESPACE, "reason", &decision.reason);
        if let Some(header) = &self.config.model_header {
            envoy.set_request_header(header, decision.model.as_bytes());
        }
        if self.config.clear_route_cache {
            envoy.clear_route_cache();
        }
        self.count(envoy, &vm.name, &decision.model, outcome);
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
        }
    }

    fn on_request_trailers(
        &mut self,
        envoy: &mut EHF,
    ) -> abi::envoy_dynamic_module_type_on_http_filter_request_trailers_status {
        // With trailers, the body callback never sees the end of the stream, so the buffered body
        // is processed here.
        match self.process(envoy, false) {
            Action::Continue => {
                abi::envoy_dynamic_module_type_on_http_filter_request_trailers_status::Continue
            }
            Action::Stop => {
                abi::envoy_dynamic_module_type_on_http_filter_request_trailers_status::StopIteration
            }
        }
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
        Filter {
            config: Arc::new(Config::parse(config.as_bytes()).unwrap()),
            decisions: None,
            format: None,
        }
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
        assert_eq!(
            rec.counters,
            vec![vec!["coding-auto", "large-model", "routed"]]
        );
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

    #[test]
    fn new_http_filter() {
        let config = FilterConfig {
            config: Arc::new(Config::parse(CONFIG.as_bytes()).unwrap()),
            decisions: None,
        };
        let mut envoy = MockEnvoyHttpFilter::new();
        let _filter = HttpFilterConfig::<MockEnvoyHttpFilter>::new_http_filter(&config, &mut envoy);
    }
}
