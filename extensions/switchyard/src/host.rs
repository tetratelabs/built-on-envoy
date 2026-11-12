// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

//! Serves the model and decision calls of Switchyard algorithms.
//!
//! An algorithm run is a stream of steps. Most algorithms finish in the first step, but judges,
//! verifiers and advisors ask the host to call a model or a decision API first. The host drives the
//! step stream on the module's Tokio runtime and hands each call to a [`Transport`], which makes it
//! as an Envoy HTTP callout on the worker thread of the request. The answer comes back through a
//! oneshot channel, and the host passes it to the algorithm.

use std::collections::BTreeSet;
use std::sync::{Arc, Mutex};

use futures::stream::FuturesUnordered;
use futures::{FutureExt, StreamExt};
use serde_json::Value;
use switchyard_libsy::{
    CallDecision, CallModel, LibsyError, Result, RoutingOutcome, Step, StepStream,
};
use switchyard_protocol::{
    DecisionRequest, DecisionResponse, LlmClientError, LlmResponse, ModelId, Request, Response,
    Usage, WireFormat,
};
use tokio::sync::oneshot;

use crate::decisions::DecisionFormat;

/// Why a call is made, sent to the gateway in the `x-switchyard-call` header.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum CallRole {
    /// A model call to a judge, a verifier or an advisor: a model that decides, and never answers
    /// the client.
    Judge,
    /// A model call to a model that may answer the client, such as the efficient attempt of `vgr`
    /// or the executor of `advisor`.
    Model,
    /// A call to a decision API.
    Decision,
}

impl CallRole {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Judge => "judge",
            Self::Model => "model",
            Self::Decision => "decision",
        }
    }
}

/// The response to a callout.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CalloutResponse {
    pub status: u16,
    pub body: Vec<u8>,
}

/// The result of a callout. Errors are transport failures: resets, timeouts, oversized responses
/// and callouts Envoy could not start.
pub type CalloutResult = std::result::Result<CalloutResponse, String>;

/// A call the transport must make.
pub struct Callout {
    pub role: CallRole,
    /// The JSON request body.
    pub body: Vec<u8>,
    /// Where the transport sends the response.
    pub reply: oneshot::Sender<CalloutResult>,
}

/// Makes calls on behalf of the algorithm.
pub trait Transport: Send + Sync {
    /// Starts a call. The result is sent to the callout's reply channel; dropping the channel
    /// fails the call.
    fn send(&self, callout: Callout);
}

/// The calls made by a routing run and their token usage.
#[derive(Debug, Default, Clone, PartialEq, Eq)]
pub struct RoutingUsage {
    /// Number of model and decision calls, including failed ones.
    pub calls: u64,
    /// Input tokens, cached tokens included.
    pub input_tokens: u64,
    pub output_tokens: u64,
    pub total_tokens: u64,
    /// The models called.
    pub models: BTreeSet<String>,
}

impl RoutingUsage {
    fn record(&mut self, model: &str, usage: Option<&Usage>) {
        self.calls += 1;
        self.models.insert(model.to_string());
        let Some(usage) = usage else {
            return;
        };
        let input = usage.input_tokens.unwrap_or_default()
            + usage.cached_input_tokens().unwrap_or_default()
            + usage.cache_creation_input_tokens().unwrap_or_default();
        let output = usage.output_tokens.unwrap_or_default();
        self.input_tokens += input;
        self.output_tokens += output;
        self.total_tokens += usage.total_tokens.unwrap_or(input + output);
    }
}

/// What the host needs to serve the calls of one run.
#[derive(Clone)]
pub struct Host {
    pub transport: Arc<dyn Transport>,
    /// API format of the model calls.
    pub model_format: WireFormat,
    /// API format of the decision calls, when decision calls are configured.
    pub decision_format: Option<DecisionFormat>,
    /// Models that only judge, whose calls are labelled as judge calls.
    pub judges: Arc<BTreeSet<String>>,
    pub usage: Arc<Mutex<RoutingUsage>>,
}

impl Host {
    /// Drives a run to its outcome, serving its calls concurrently. `first` is the step already
    /// taken from the stream.
    ///
    /// As in `switchyard_libsy::drive`, a call that fails without error recovery aborts the run.
    pub async fn drive(&self, mut stream: StepStream, first: Step) -> Result<RoutingOutcome> {
        let mut in_flight = FuturesUnordered::new();
        let mut next = Some(first);
        loop {
            let step = match next.take() {
                Some(step) => step,
                None => {
                    tokio::select! {
                        Some(result) = in_flight.next() => {
                            result?;
                            continue;
                        }
                        step = stream.next() => match step {
                            Some(step) => step?,
                            None => return Err(LibsyError::MissingFinalResponse),
                        },
                    }
                }
            };
            match step {
                Step::Done(outcome) => return Ok(*outcome),
                Step::CallModel(call) => in_flight.push(self.clone().serve_model(*call).boxed()),
                Step::CallDecision(call) => {
                    in_flight.push(self.clone().serve_decision(*call).boxed())
                }
            }
        }
    }

    async fn serve_model(self, call: CallModel) -> Result<()> {
        let request = call.request.clone();
        let models = call.models.clone();
        call.respond(async move { self.call_models(&request, &models).await })
            .await
    }

    async fn serve_decision(self, call: CallDecision) -> Result<()> {
        let request = call.request.clone();
        let model = call.model.clone();
        call.respond(async move {
            self.call_decision(&request)
                .await
                .map_err(|e| LibsyError::client_call(model, e))
        })
        .await
    }

    /// Calls the candidate models in order, until one answers or a failure is not worth trying
    /// another candidate for.
    async fn call_models(&self, request: &Request, models: &[ModelId]) -> Result<Response> {
        let mut error = LibsyError::NoTargets;
        for model in models {
            match self.call_model(request, model).await {
                Ok(response) => return Ok(response),
                Err(e) => {
                    let retry = worth_another_candidate(&e);
                    error = LibsyError::client_call(model.clone(), e);
                    if !retry {
                        break;
                    }
                }
            }
        }
        Err(error)
    }

    async fn call_model(
        &self,
        request: &Request,
        model: &ModelId,
    ) -> std::result::Result<Response, LlmClientError> {
        let mut llm_request = request.llm_request.clone();
        switchyard_translation::prepare_request_for_target(&mut llm_request, model, None);
        // Calls are buffered: the algorithm needs the whole answer before deciding.
        llm_request.stream = false;
        let mut body = switchyard_translation::encode_request(&llm_request, self.model_format)
            .map_err(|e| LlmClientError::RequestEncoding(e.to_string()))?;
        if let Value::Object(object) = &mut body {
            // Exact replays of the client body keep its streaming settings.
            object.insert("stream".into(), Value::Bool(false));
            object.remove("stream_options");
        }
        let role = if self.judges.contains(model.as_str()) {
            CallRole::Judge
        } else {
            CallRole::Model
        };
        let response = self.send(role, &body, model.as_str()).await;
        let agg = response.and_then(|body| {
            let value: Value =
                serde_json::from_slice(&body).map_err(|e| LlmClientError::InvalidResponse {
                    source: Box::new(e),
                })?;
            switchyard_translation::decode_aggregated_response(&value, self.model_format)
                .map_err(|e| LlmClientError::ResponseTranslation(e.to_string()))
        });
        let usage = agg.as_ref().ok().map(|agg| &agg.usage);
        self.lock_usage().record(model.as_str(), usage);
        let mut response = Response {
            llm_response: LlmResponse::Agg(agg?),
            metadata: request.metadata.clone(),
            upstream_headers: http::HeaderMap::new(),
        };
        response.set_served_model(model);
        Ok(response)
    }

    async fn call_decision(
        &self,
        request: &DecisionRequest,
    ) -> std::result::Result<DecisionResponse, LlmClientError> {
        let model = request
            .model
            .as_ref()
            .map(ModelId::to_string)
            .unwrap_or_default();
        let Some(format) = self.decision_format else {
            return Err(LlmClientError::Configuration {
                message: "the algorithm made a decision call, but decision_calls is not configured"
                    .into(),
            });
        };
        let body = format
            .encode(request)
            .map_err(|message| LlmClientError::InvalidRequest { message })?;
        let response = self
            .send(CallRole::Decision, &body, &model)
            .await
            .and_then(|body| {
                format
                    .decode(request, &body)
                    .map_err(LlmClientError::ResponseTranslation)
            });
        self.lock_usage()
            .record(&model, response.as_ref().ok().map(|r| &r.usage));
        response
    }

    /// Sends a call through the transport and returns the body of a successful response.
    async fn send(
        &self,
        role: CallRole,
        body: &Value,
        model: &str,
    ) -> std::result::Result<Vec<u8>, LlmClientError> {
        let (reply, response) = oneshot::channel();
        self.transport.send(Callout {
            role,
            body: serde_json::to_vec(body).unwrap_or_default(),
            reply,
        });
        let transport = |message: String| LlmClientError::Transport {
            source: message.into(),
        };
        let response = response
            .await
            .map_err(|_| transport("the request was cancelled".to_string()))?
            .map_err(transport)?;
        if !(200..300).contains(&response.status) {
            let status = http::StatusCode::from_u16(response.status)
                .unwrap_or(http::StatusCode::BAD_GATEWAY);
            let body = String::from_utf8_lossy(&response.body).into_owned();
            return Err(LlmClientError::UpstreamHttp { status, body });
        }
        if response.body.is_empty() {
            return Err(LlmClientError::InvalidResponse {
                source: format!("empty response from {model}").into(),
            });
        }
        Ok(response.body)
    }

    fn lock_usage(&self) -> std::sync::MutexGuard<'_, RoutingUsage> {
        self.usage.lock().unwrap_or_else(|e| e.into_inner())
    }
}

/// Whether a failed model call should be retried on the next candidate model: when the model was
/// unreachable, overloaded or could not take the request, as Switchyard's own client does.
fn worth_another_candidate(error: &LlmClientError) -> bool {
    match error {
        LlmClientError::Transport { .. }
        | LlmClientError::TemporarilyUnavailable
        | LlmClientError::ContextWindowExceeded { .. } => true,
        LlmClientError::UpstreamHttp { status, .. } => {
            status.is_server_error()
                || matches!(
                    *status,
                    http::StatusCode::FORBIDDEN
                        | http::StatusCode::REQUEST_TIMEOUT
                        | http::StatusCode::TOO_MANY_REQUESTS
                )
        }
        _ => false,
    }
}

#[cfg(test)]
pub mod testing {
    //! A transport that answers calls from a function, for tests.

    use super::*;

    type Responder = dyn Fn(CallRole, &Value) -> CalloutResult + Send + Sync;

    /// Answers every call with a function of its role and body, and records the calls.
    pub struct FnTransport {
        respond: Box<Responder>,
        pub calls: Mutex<Vec<(CallRole, Value)>>,
    }

    impl FnTransport {
        pub fn new(
            respond: impl Fn(CallRole, &Value) -> CalloutResult + Send + Sync + 'static,
        ) -> Arc<Self> {
            Arc::new(Self {
                respond: Box::new(respond),
                calls: Mutex::default(),
            })
        }
    }

    impl Transport for FnTransport {
        fn send(&self, callout: Callout) {
            let body: Value = serde_json::from_slice(&callout.body).unwrap();
            let result = (self.respond)(callout.role, &body);
            self.calls.lock().unwrap().push((callout.role, body));
            let _ = callout.reply.send(result);
        }
    }

    /// A successful OpenAI chat completion with the given text.
    pub fn chat_completion(model: &str, text: &str) -> CalloutResult {
        Ok(CalloutResponse {
            status: 200,
            body: serde_json::json!({
                "id": "c1", "object": "chat.completion", "model": model,
                "choices": [{"index": 0, "message": {"role": "assistant", "content": text}, "finish_reason": "stop"}],
                "usage": {"prompt_tokens": 100, "completion_tokens": 10, "total_tokens": 110}
            })
            .to_string()
            .into_bytes(),
        })
    }
}

#[cfg(test)]
mod tests {
    use super::testing::*;
    use super::*;
    use serde_json::json;
    use switchyard_protocol::{ChoiceOption, DecisionKind, DecisionQuestion};

    fn host(transport: Arc<dyn Transport>, decision_format: Option<DecisionFormat>) -> Host {
        Host {
            transport,
            model_format: WireFormat::OpenAiChat,
            decision_format,
            judges: Arc::new(BTreeSet::from(["judge-model".to_string()])),
            usage: Arc::default(),
        }
    }

    fn request(stream: bool) -> Request {
        let body = json!({"model": "v", "stream": stream, "stream_options": {"include_usage": true},
            "messages": [{"role": "user", "content": "hi"}]});
        let mut llm_request =
            switchyard_translation::decode_request(WireFormat::OpenAiChat, &body).unwrap();
        llm_request.stream = stream;
        Request {
            llm_request,
            ..Default::default()
        }
    }

    #[tokio::test]
    async fn model_calls_are_buffered_and_accounted() {
        let transport =
            FnTransport::new(|_, body| chat_completion(body["model"].as_str().unwrap(), "ok"));
        let host = host(transport.clone(), None);
        let response = host
            .call_models(&request(true), &[ModelId::from("judge-model")])
            .await
            .unwrap();
        assert_eq!(
            response.served_model().map(ModelId::as_str),
            Some("judge-model")
        );
        let agg = response.llm_response.as_agg().unwrap();
        assert_eq!(agg.usage.output_tokens, Some(10));

        let calls = transport.calls.lock().unwrap();
        let (role, body) = &calls[0];
        assert_eq!(*role, CallRole::Judge);
        assert_eq!(body["model"], "judge-model");
        assert_eq!(body["stream"], false);
        assert!(body.get("stream_options").is_none());
        let usage = host.usage.lock().unwrap().clone();
        assert_eq!(usage.calls, 1);
        assert_eq!(usage.input_tokens, 100);
        assert_eq!(usage.output_tokens, 10);
        assert_eq!(usage.total_tokens, 110);
        assert_eq!(usage.models, BTreeSet::from(["judge-model".to_string()]));
    }

    #[tokio::test]
    async fn model_calls_fall_back_to_the_next_candidate() {
        let transport = FnTransport::new(|role, body| {
            assert_eq!(role, CallRole::Model);
            match body["model"].as_str().unwrap() {
                "down" => Err("connection reset".to_string()),
                "busy" => Ok(CalloutResponse {
                    status: 503,
                    body: b"busy".to_vec(),
                }),
                model => chat_completion(model, "ok"),
            }
        });
        let host = host(transport.clone(), None);
        let models = [
            ModelId::from("down"),
            ModelId::from("busy"),
            ModelId::from("up"),
        ];
        let response = host.call_models(&request(false), &models).await.unwrap();
        assert_eq!(response.served_model().map(ModelId::as_str), Some("up"));
        assert_eq!(host.usage.lock().unwrap().calls, 3);
    }

    #[tokio::test]
    async fn model_call_errors() {
        let cases: Vec<(CalloutResult, &str)> = vec![
            (
                Ok(CalloutResponse {
                    status: 400,
                    body: b"bad".to_vec(),
                }),
                "HTTP 400",
            ),
            (
                Ok(CalloutResponse {
                    status: 200,
                    body: vec![],
                }),
                "empty response",
            ),
            (
                Ok(CalloutResponse {
                    status: 200,
                    body: b"not json".to_vec(),
                }),
                "invalid upstream response",
            ),
            (
                Ok(CalloutResponse {
                    status: 200,
                    body: b"[1]".to_vec(),
                }),
                "response translation",
            ),
        ];
        for (result, want) in cases {
            let transport = FnTransport::new(move |_, _| result.clone());
            // A 400 is not worth another candidate, so the second model is never called.
            let models = [ModelId::from("a"), ModelId::from("b")];
            let result = host(transport.clone(), None)
                .call_models(&request(false), &models)
                .await;
            let err = result
                .err()
                .unwrap_or_else(|| panic!("{want}: no error"))
                .to_string();
            assert!(err.contains(want), "{err}");
            assert_eq!(transport.calls.lock().unwrap().len(), 1);
        }
        let err = host(FnTransport::new(|_, _| unreachable!()), None)
            .call_models(&request(false), &[])
            .await
            .err()
            .unwrap();
        assert!(matches!(err, LibsyError::NoTargets));
    }

    #[tokio::test]
    async fn dropped_replies_fail_the_call() {
        struct Dropping;
        impl Transport for Dropping {
            fn send(&self, _callout: Callout) {}
        }
        let err = host(Arc::new(Dropping), None)
            .call_models(&request(false), &[ModelId::from("a")])
            .await
            .err()
            .unwrap()
            .to_string();
        assert!(err.contains("cancelled"), "{err}");
    }

    fn decision_request() -> DecisionRequest {
        DecisionRequest {
            model: Some(ModelId::from("jev")),
            context: json!("context"),
            questions: [(
                "route".to_string(),
                DecisionQuestion {
                    instructions: json!("pick"),
                    kind: DecisionKind::Choice {
                        options: vec![
                            ChoiceOption {
                                id: "a".into(),
                                description: None,
                            },
                            ChoiceOption {
                                id: "b".into(),
                                description: None,
                            },
                        ],
                    },
                },
            )]
            .into(),
        }
    }

    #[tokio::test]
    async fn decision_calls() {
        let transport = FnTransport::new(|role, body| {
            assert_eq!(role, CallRole::Decision);
            assert_eq!(body["model"], "jev");
            Ok(CalloutResponse {
                status: 200,
                body: json!({"answers": {"route": {"type": "choice", "choice": "b"}},
                             "usage": {"input_tokens": 5, "output_tokens": 1}})
                .to_string()
                .into_bytes(),
            })
        });
        let host = host(transport, Some(DecisionFormat::SystemOne));
        let response = host.call_decision(&decision_request()).await.unwrap();
        assert_eq!(response.answers.len(), 1);
        let usage = host.usage.lock().unwrap().clone();
        assert_eq!(
            (usage.calls, usage.input_tokens, usage.total_tokens),
            (1, 5, 6)
        );

        let err = host_without_decisions()
            .call_decision(&decision_request())
            .await
            .unwrap_err()
            .to_string();
        assert!(err.contains("decision_calls is not configured"), "{err}");
    }

    fn host_without_decisions() -> Host {
        host(FnTransport::new(|_, _| unreachable!()), None)
    }
}
