// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

//! Judge models: the decision and LLM judges that routing algorithms consult.
//!
//! The filter calls judges with Envoy HTTP callouts, so worker threads never block on the network.
//! This module only builds the callout requests and decodes their responses.

mod config;
mod llm;
mod openai_decisions;
mod system_one;

use std::collections::BTreeMap;
use std::sync::Arc;

use serde::Deserialize;
use serde_json::Value;
use switchyard_protocol::{DecisionRequest, DecisionResponse, Request, Response, WireFormat};

pub use config::{compile, RawDecisionTarget, RawJudgeTarget, TraceContext};

/// Configured judges, by name. Algorithms refer to judges by these names.
pub type Judges = BTreeMap<String, Arc<Judge>>;

/// Longest judge response body excerpt included in error messages.
const ERROR_BODY_EXCERPT: usize = 256;

/// A compiled judge.
#[derive(PartialEq)]
pub struct Judge {
    /// The name algorithms use to refer to the judge.
    pub name: String,
    pub kind: JudgeKind,
    /// The Envoy cluster the callout is sent to.
    pub cluster: String,
    /// The `:authority` of the callout request.
    pub authority: String,
    /// The `:path` of the callout request.
    pub path: String,
    /// The model the judge provider serves, sent in the request body.
    pub model: String,
    /// Extra request headers, including the credentials. Names are lowercase.
    pub headers: Vec<(String, String)>,
    /// Timeout of each callout.
    pub timeout_ms: u64,
}

/// Header names whose values are credentials, redacted from `Debug` output.
const SECRET_HEADERS: [&str; 2] = ["authorization", "x-api-key"];

impl std::fmt::Debug for Judge {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        let headers: Vec<(&str, &str)> = self
            .headers
            .iter()
            .map(|(k, v)| {
                let secret = SECRET_HEADERS.contains(&k.as_str());
                (k.as_str(), if secret { "<redacted>" } else { v.as_str() })
            })
            .collect();
        f.debug_struct("Judge")
            .field("name", &self.name)
            .field("kind", &self.kind)
            .field("cluster", &self.cluster)
            .field("authority", &self.authority)
            .field("path", &self.path)
            .field("model", &self.model)
            .field("headers", &headers)
            .field("timeout_ms", &self.timeout_ms)
            .finish()
    }
}

/// What a judge answers, and its API format.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum JudgeKind {
    /// Typed decisions, such as Choice or Score answers.
    Decision(DecisionFormat),
    /// Chat completions, in an LLM API format.
    Llm(WireFormat),
}

/// The API format of a decision judge.
#[derive(Deserialize, Debug, Clone, Copy, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
pub enum DecisionFormat {
    /// TypeSafe System One, as served by Jev.
    SystemOne,
    /// OpenAI-style decisions.
    OpenaiDecisions,
}

/// An HTTP callout to send to a judge.
#[derive(Debug, Clone, PartialEq)]
pub struct HttpRequestSpec {
    pub cluster: String,
    /// All the request headers, pseudo-headers included.
    pub headers: Vec<(String, Vec<u8>)>,
    pub body: Vec<u8>,
    pub timeout_ms: u64,
}

/// The response of a judge callout.
#[derive(Debug, Clone, PartialEq)]
pub struct HttpReply {
    pub status: u32,
    pub body: Vec<u8>,
}

impl Judge {
    /// Builds the callout for a decision request.
    pub fn decision_request(&self, request: &DecisionRequest) -> Result<HttpRequestSpec, String> {
        let body = match self.kind {
            JudgeKind::Decision(DecisionFormat::SystemOne) => {
                system_one::encode(&self.model, request)?
            }
            JudgeKind::Decision(DecisionFormat::OpenaiDecisions) => {
                openai_decisions::encode(&self.model, request)?
            }
            JudgeKind::Llm(_) => {
                return Err(format!(
                    "judge {:?} is an LLM judge, but the algorithm asked it for a decision",
                    self.name
                ))
            }
        };
        self.http_request(&body)
    }

    /// Decodes the response of a decision callout.
    pub fn decode_decision(
        &self,
        request: &DecisionRequest,
        reply: &HttpReply,
    ) -> Result<DecisionResponse, String> {
        self.check_status(reply)?;
        let decoded = match self.kind {
            JudgeKind::Decision(DecisionFormat::SystemOne) => {
                system_one::decode(request, &reply.body)
            }
            JudgeKind::Decision(DecisionFormat::OpenaiDecisions) => {
                openai_decisions::decode(request, &reply.body)
            }
            JudgeKind::Llm(_) => Err("not a decision judge".to_string()),
        };
        decoded.map_err(|e| format!("judge {:?}: {e}", self.name))
    }

    /// Builds the callout for a model (chat completion) request.
    pub fn model_request(&self, request: &Request) -> Result<HttpRequestSpec, String> {
        let JudgeKind::Llm(format) = self.kind else {
            return Err(format!(
                "judge {:?} is a decision judge, but the algorithm asked it for a completion",
                self.name
            ));
        };
        self.http_request(&llm::encode(format, &self.model, request)?)
    }

    /// Decodes the response of a model callout.
    pub fn decode_model(&self, reply: &HttpReply) -> Result<Response, String> {
        self.check_status(reply)?;
        let JudgeKind::Llm(format) = self.kind else {
            return Err(format!("judge {:?} is not an LLM judge", self.name));
        };
        llm::decode(format, &reply.body).map_err(|e| format!("judge {:?}: {e}", self.name))
    }

    fn http_request(&self, body: &Value) -> Result<HttpRequestSpec, String> {
        let body = serde_json::to_vec(body)
            .map_err(|e| format!("judge {:?}: could not encode the request: {e}", self.name))?;
        let mut headers: Vec<(String, Vec<u8>)> = vec![
            (":method".into(), b"POST".to_vec()),
            (":path".into(), self.path.clone().into_bytes()),
            // Envoy maps `host` to `:authority`, so only one of them is set: both would send two
            // Host headers, which some servers reject.
            ("host".into(), self.authority.clone().into_bytes()),
            ("content-type".into(), b"application/json".to_vec()),
            ("accept".into(), b"application/json".to_vec()),
        ];
        headers.extend(
            self.headers
                .iter()
                .map(|(k, v)| (k.clone(), v.clone().into_bytes())),
        );
        Ok(HttpRequestSpec {
            cluster: self.cluster.clone(),
            headers,
            body,
            timeout_ms: self.timeout_ms,
        })
    }

    fn check_status(&self, reply: &HttpReply) -> Result<(), String> {
        if (200..300).contains(&reply.status) {
            return Ok(());
        }
        let body = String::from_utf8_lossy(&reply.body);
        let excerpt: String = body.chars().take(ERROR_BODY_EXCERPT).collect();
        Err(format!(
            "judge {:?} returned HTTP {}: {excerpt}",
            self.name, reply.status
        ))
    }
}
#[cfg(test)]
mod tests {
    use super::*;

    fn judge(kind: JudgeKind) -> Judge {
        Judge {
            name: "j".into(),
            kind,
            cluster: "cl".into(),
            authority: "auth.example".into(),
            path: "/p".into(),
            model: "m".into(),
            headers: vec![("authorization".into(), "Bearer sk-secret".into())],
            timeout_ms: 1234,
        }
    }

    fn decision_request() -> DecisionRequest {
        DecisionRequest {
            model: None,
            context: Value::Null,
            questions: BTreeMap::new(),
        }
    }

    #[test]
    fn http_request_has_pseudo_headers_and_credentials() {
        let j = judge(JudgeKind::Llm(WireFormat::OpenAiChat));
        let spec = j.http_request(&serde_json::json!({"a": 1})).unwrap();
        assert_eq!(spec.cluster, "cl");
        assert_eq!(spec.timeout_ms, 1234);
        assert_eq!(spec.body, br#"{"a":1}"#.to_vec());
        let get = |name: &str| {
            spec.headers
                .iter()
                .find(|(k, _)| k == name)
                .map(|(_, v)| v.clone())
        };
        assert_eq!(get(":method"), Some(b"POST".to_vec()));
        assert_eq!(get(":path"), Some(b"/p".to_vec()));
        assert_eq!(get(":authority"), None);
        assert_eq!(get("host"), Some(b"auth.example".to_vec()));
        assert_eq!(get("content-type"), Some(b"application/json".to_vec()));
        assert_eq!(get("authorization"), Some(b"Bearer sk-secret".to_vec()));
    }

    #[test]
    fn decision_request_rejects_llm_judge() {
        let j = judge(JudgeKind::Llm(WireFormat::OpenAiChat));
        let err = j.decision_request(&decision_request()).unwrap_err();
        assert!(err.contains("LLM judge"), "{err}");
    }

    #[test]
    fn model_request_rejects_decision_judge() {
        let j = judge(JudgeKind::Decision(DecisionFormat::SystemOne));
        let err = j.model_request(&Request::default()).unwrap_err();
        assert!(err.contains("decision judge"), "{err}");
    }

    #[test]
    fn check_status_accepts_2xx_only() {
        let j = judge(JudgeKind::Llm(WireFormat::OpenAiChat));
        for status in [200, 204, 299] {
            assert!(j
                .check_status(&HttpReply {
                    status,
                    body: vec![]
                })
                .is_ok());
        }
        for status in [199, 300, 401, 500] {
            assert!(j
                .check_status(&HttpReply {
                    status,
                    body: vec![]
                })
                .is_err());
        }
    }

    #[test]
    fn check_status_error_has_truncated_body() {
        let j = judge(JudgeKind::Llm(WireFormat::OpenAiChat));
        let body = "x".repeat(ERROR_BODY_EXCERPT + 100).into_bytes();
        let err = j
            .check_status(&HttpReply { status: 503, body })
            .unwrap_err();
        assert!(err.contains("HTTP 503"), "{err}");
        assert!(err.contains(&"x".repeat(ERROR_BODY_EXCERPT)));
        assert!(!err.contains(&"x".repeat(ERROR_BODY_EXCERPT + 1)));
    }

    #[test]
    fn decode_fails_on_bad_status_before_decoding() {
        let reply = HttpReply {
            status: 500,
            body: b"boom".to_vec(),
        };
        let d = judge(JudgeKind::Decision(DecisionFormat::SystemOne));
        assert!(d
            .decode_decision(&decision_request(), &reply)
            .unwrap_err()
            .contains("HTTP 500"));
        let m = judge(JudgeKind::Llm(WireFormat::OpenAiChat));
        assert!(m.decode_model(&reply).err().unwrap().contains("HTTP 500"));
    }

    #[test]
    fn debug_redacts_credentials() {
        let mut j = judge(JudgeKind::Llm(WireFormat::AnthropicMessages));
        j.headers.push(("x-api-key".into(), "sk-ant-secret".into()));
        j.headers.push(("x-org".into(), "acme".into()));
        let text = format!("{j:?}");
        assert!(
            !text.contains("sk-secret") && !text.contains("sk-ant-secret"),
            "{text}"
        );
        assert!(text.contains("acme"));
    }
}
