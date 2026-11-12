// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

//! Wire formats of the decision judges.
//!
//! Switchyard algorithms ask typed questions through decision calls. These are served by a decision
//! API, such as a System One endpoint or OpenAI's Decisions API. The request and response formats
//! follow the clients of `switchyard-llm-client`, which cannot be reused because they make their own
//! HTTP calls, while this filter makes them through Envoy.

use std::collections::BTreeMap;

use serde::Deserialize;
use serde_json::{json, Map, Value};
use switchyard_protocol::{
    BooleanEstimate, DecisionAnswer, DecisionKind, DecisionRequest, DecisionResponse,
    DecisionValue, ModelId, Probability, ProviderConfidence, ScoreValue, Usage,
};

/// The API format of a decision judge.
#[derive(Deserialize, Debug, Clone, Copy, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
pub enum DecisionFormat {
    /// A System One endpoint, such as TypeSafe's Jev API.
    SystemOne,
    /// OpenAI's Decisions API.
    OpenaiDecisions,
}

impl DecisionFormat {
    /// Encodes a decision request.
    pub fn encode(self, request: &DecisionRequest) -> Result<Value, String> {
        let model = request
            .model
            .as_ref()
            .ok_or("the decision request has no model")?;
        match self {
            Self::SystemOne => encode_system_one(model, request),
            Self::OpenaiDecisions => Ok(encode_openai(model, request)),
        }
    }

    /// Decodes a successful decision response, checking it answers the request's questions.
    pub fn decode(
        self,
        request: &DecisionRequest,
        body: &[u8],
    ) -> Result<DecisionResponse, String> {
        match self {
            Self::SystemOne => decode_system_one(request, body),
            Self::OpenaiDecisions => decode_openai(request, body),
        }
    }
}

fn encode_system_one(model: &ModelId, request: &DecisionRequest) -> Result<Value, String> {
    let mut questions = Map::new();
    for (id, question) in &request.questions {
        let (kind, criteria) = match &question.kind {
            DecisionKind::Boolean {
                true_description,
                false_description,
            } => {
                let mut criteria = Map::new();
                for (key, description) in [("true", true_description), ("false", false_description)]
                {
                    if let Some(description) = description {
                        criteria.insert(key.into(), description.clone());
                    }
                }
                ("noul", Value::Object(criteria))
            }
            DecisionKind::Choice { options } => {
                let mut criteria = Map::new();
                for option in options {
                    let description = option.description.clone().unwrap_or(Value::Null);
                    if criteria.insert(option.id.clone(), description).is_some() {
                        return Err(format!("duplicate option {:?} in question {id}", option.id));
                    }
                }
                ("choice", Value::Object(criteria))
            }
            DecisionKind::Score { levels } => ("score", json!(levels)),
        };
        questions.insert(
            id.clone(),
            json!({"type": kind, "instructions": question.instructions, "criteria": criteria}),
        );
    }
    Ok(json!({"model": model, "state": request.context, "questions": questions}))
}

#[derive(Deserialize)]
struct SystemOneResponse {
    id: Option<String>,
    model: Option<ModelId>,
    answers: BTreeMap<String, SystemOneAnswer>,
    #[serde(default)]
    usage: Usage,
}

#[derive(Deserialize)]
struct SystemOneAnswer {
    #[serde(flatten)]
    value: SystemOneValue,
    confidence: Option<ProviderConfidence>,
}

#[derive(Deserialize)]
#[serde(tag = "type", rename_all = "snake_case")]
enum SystemOneValue {
    Noul {
        noul: Probability,
    },
    Choice {
        choice: String,
        probabilities: Option<BTreeMap<String, Probability>>,
    },
    Score {
        score: ScoreValue,
        probabilities: Option<BTreeMap<String, Probability>>,
    },
}

fn decode_system_one(request: &DecisionRequest, body: &[u8]) -> Result<DecisionResponse, String> {
    let response: SystemOneResponse =
        serde_json::from_slice(body).map_err(|e| format!("invalid System One response: {e}"))?;
    if !response.answers.keys().eq(request.questions.keys()) {
        return Err("the System One answers do not match the questions".into());
    }
    let mut answers = BTreeMap::new();
    for (id, answer) in response.answers {
        let kind = request.questions.get(&id).map(|q| &q.kind);
        let value = match (kind, answer.value) {
            (Some(DecisionKind::Boolean { .. }), SystemOneValue::Noul { noul }) => {
                DecisionValue::Boolean(BooleanEstimate::ProbabilityTrue(noul))
            }
            (
                Some(DecisionKind::Choice { options }),
                SystemOneValue::Choice {
                    choice,
                    probabilities,
                },
            ) if options.iter().any(|o| o.id == choice) => DecisionValue::Choice {
                selected: choice,
                probabilities,
            },
            (
                Some(DecisionKind::Score { levels }),
                SystemOneValue::Score {
                    score,
                    probabilities,
                },
            ) if !levels.is_empty() && (0.0..=(levels.len() - 1) as f64).contains(&score.0) => {
                let probabilities = probabilities
                    .map(|mut probabilities| {
                        if probabilities.len() != levels.len() {
                            return Err(format!(
                                "the score probabilities of {id:?} do not match its levels"
                            ));
                        }
                        (0..levels.len())
                            .map(|index| {
                                probabilities.remove(&index.to_string()).ok_or_else(|| {
                                    format!(
                                        "the score probabilities of {id:?} do not match its levels"
                                    )
                                })
                            })
                            .collect::<Result<Vec<_>, _>>()
                    })
                    .transpose()?;
                DecisionValue::Score {
                    value: score,
                    probabilities,
                }
            }
            _ => return Err(format!("answer {id:?} does not match its question")),
        };
        answers.insert(
            id,
            DecisionAnswer {
                value,
                provider_confidence: answer.confidence,
            },
        );
    }
    Ok(DecisionResponse {
        id: response.id,
        model: response.model,
        answers,
        usage: response.usage,
    })
}

/// OpenAI requires text, so structured content is sent as JSON text.
fn text(value: &Value) -> String {
    value
        .as_str()
        .map(str::to_owned)
        .unwrap_or_else(|| value.to_string())
}

fn encode_openai(model: &ModelId, request: &DecisionRequest) -> Value {
    let questions: Vec<Value> = request
        .questions
        .iter()
        .map(|(name, question)| {
            let mut wire = json!({"name": name, "instructions": text(&question.instructions)});
            match &question.kind {
                DecisionKind::Boolean {
                    true_description,
                    false_description,
                } => {
                    wire["type"] = json!("predicate");
                    if true_description.is_some() || false_description.is_some() {
                        wire["instructions"] = json!(json!({
                            "instructions": question.instructions,
                            "true": true_description,
                            "false": false_description,
                        })
                        .to_string());
                    }
                }
                DecisionKind::Choice { options } => {
                    wire["type"] = json!("choice");
                    wire["choices"] = options
                        .iter()
                        .map(|option| {
                            let mut choice = json!({"value": option.id});
                            if let Some(description) = &option.description {
                                choice["description"] = json!(text(description));
                            }
                            choice
                        })
                        .collect();
                }
                DecisionKind::Score { levels } => {
                    wire["type"] = json!("score");
                    wire["levels"] = levels
                        .iter()
                        .map(|level| json!({"label": text(level)}))
                        .collect();
                }
            }
            wire
        })
        .collect();
    json!({"model": model, "input": text(&request.context), "questions": questions})
}

#[derive(Deserialize)]
struct OpenAiResponse {
    model: ModelId,
    answers: Vec<OpenAiAnswer>,
    usage: OpenAiUsage,
}

#[derive(Deserialize)]
struct OpenAiAnswer {
    name: String,
    #[serde(flatten)]
    value: OpenAiValue,
    confidence: Option<ProviderConfidence>,
}

#[derive(Deserialize)]
#[serde(tag = "type", rename_all = "snake_case")]
enum OpenAiValue {
    Predicate {
        probability: Probability,
    },
    Choice {
        choice: String,
        probabilities: Vec<OpenAiProbability<String>>,
    },
    Score {
        score: ScoreValue,
        probabilities: Vec<OpenAiProbability<usize>>,
    },
    Refusal,
}

#[derive(Deserialize)]
struct OpenAiProbability<T> {
    value: T,
    probability: Probability,
}

#[derive(Deserialize, Default)]
#[serde(default)]
struct OpenAiUsage {
    input_tokens: u64,
    output_tokens: u64,
    total_tokens: u64,
    input_tokens_details: OpenAiInputDetails,
    output_tokens_details: OpenAiOutputDetails,
}

#[derive(Deserialize, Default)]
#[serde(default)]
struct OpenAiInputDetails {
    cached_tokens: u64,
    cache_write_tokens: u64,
}

#[derive(Deserialize, Default)]
#[serde(default)]
struct OpenAiOutputDetails {
    reasoning_tokens: u64,
}

fn decode_openai(request: &DecisionRequest, body: &[u8]) -> Result<DecisionResponse, String> {
    let response: OpenAiResponse = serde_json::from_slice(body)
        .map_err(|e| format!("invalid OpenAI Decisions response: {e}"))?;
    let mut questions = request.questions.clone();
    let mut answers = BTreeMap::new();
    for answer in response.answers {
        let invalid = || format!("answer {:?} does not match its question", answer.name);
        let question = questions.remove(&answer.name).ok_or_else(invalid)?;
        let value = match (&question.kind, answer.value) {
            (DecisionKind::Boolean { .. }, OpenAiValue::Predicate { probability }) => {
                DecisionValue::Boolean(BooleanEstimate::ProbabilityTrue(probability))
            }
            (
                DecisionKind::Choice { options },
                OpenAiValue::Choice {
                    choice,
                    probabilities,
                },
            ) => {
                let count = probabilities.len();
                let probabilities: BTreeMap<_, _> = probabilities
                    .into_iter()
                    .map(|p| (p.value, p.probability))
                    .collect();
                if count != options.len()
                    || probabilities.len() != options.len()
                    || !options.iter().any(|o| o.id == choice)
                    || options.iter().any(|o| !probabilities.contains_key(&o.id))
                {
                    return Err(invalid());
                }
                DecisionValue::Choice {
                    selected: choice,
                    probabilities: Some(probabilities),
                }
            }
            (
                DecisionKind::Score { levels },
                OpenAiValue::Score {
                    score,
                    mut probabilities,
                },
            ) => {
                probabilities.sort_by_key(|p| p.value);
                if probabilities.len() != levels.len()
                    || probabilities.iter().enumerate().any(|(i, p)| p.value != i)
                {
                    return Err(invalid());
                }
                DecisionValue::Score {
                    value: score,
                    probabilities: Some(probabilities.into_iter().map(|p| p.probability).collect()),
                }
            }
            (_, OpenAiValue::Refusal) => {
                return Err(format!("the judge refused question {:?}", answer.name))
            }
            _ => return Err(invalid()),
        };
        answers.insert(
            answer.name,
            DecisionAnswer {
                value,
                provider_confidence: answer.confidence,
            },
        );
    }
    if !questions.is_empty() {
        return Err("the OpenAI Decisions answers do not match the questions".into());
    }
    let usage = response.usage;
    let cached = usage.input_tokens_details.cached_tokens;
    let created = usage.input_tokens_details.cache_write_tokens;
    Ok(DecisionResponse {
        id: None,
        model: Some(response.model),
        answers,
        usage: Usage {
            input_tokens: Some(
                usage
                    .input_tokens
                    .saturating_sub(cached)
                    .saturating_sub(created),
            ),
            cache: Usage::cache_details(Some(cached), Some(created)),
            output_tokens: Some(usage.output_tokens),
            total_tokens: Some(usage.total_tokens),
            reasoning_tokens: Some(usage.output_tokens_details.reasoning_tokens),
            ..Usage::default()
        },
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    use switchyard_protocol::{ChoiceOption, DecisionQuestion};

    fn request() -> DecisionRequest {
        DecisionRequest {
            model: Some(ModelId::from("judge-1")),
            context: json!({"task": "fix the bug"}),
            questions: BTreeMap::from([
                (
                    "route".to_string(),
                    DecisionQuestion {
                        instructions: json!("Pick a profile"),
                        kind: DecisionKind::Choice {
                            options: vec![
                                ChoiceOption {
                                    id: "capable".into(),
                                    description: Some(json!("Start capable")),
                                },
                                ChoiceOption {
                                    id: "efficient".into(),
                                    description: None,
                                },
                            ],
                        },
                    },
                ),
                (
                    "hard".to_string(),
                    DecisionQuestion {
                        instructions: json!({"rule": "is it hard"}),
                        kind: DecisionKind::Boolean {
                            true_description: Some(json!("hard")),
                            false_description: None,
                        },
                    },
                ),
                (
                    "level".to_string(),
                    DecisionQuestion {
                        instructions: json!("How hard"),
                        kind: DecisionKind::Score {
                            levels: vec![json!("low"), json!("high")],
                        },
                    },
                ),
            ]),
        }
    }

    #[test]
    fn system_one_round_trip() {
        let request = request();
        let wire = DecisionFormat::SystemOne.encode(&request).unwrap();
        assert_eq!(wire["model"], "judge-1");
        assert_eq!(wire["state"]["task"], "fix the bug");
        assert_eq!(wire["questions"]["route"]["type"], "choice");
        assert_eq!(
            wire["questions"]["route"]["criteria"]["capable"],
            "Start capable"
        );
        assert_eq!(
            wire["questions"]["route"]["criteria"]["efficient"],
            Value::Null
        );
        assert_eq!(wire["questions"]["hard"]["type"], "noul");
        assert_eq!(
            wire["questions"]["hard"]["criteria"],
            json!({"true": "hard"})
        );
        assert_eq!(
            wire["questions"]["level"]["criteria"],
            json!(["low", "high"])
        );

        let body = json!({
            "id": "d1", "model": "judge-1",
            "answers": {
                "route": {"type": "choice", "choice": "capable", "confidence": 0.9},
                "hard": {"type": "noul", "noul": 0.7},
                "level": {"type": "score", "score": 1.0, "probabilities": {"0": 0.2, "1": 0.8}}
            },
            "usage": {"input_tokens": 10, "output_tokens": 2, "total_tokens": 12, "reasoning_tokens": null}
        });
        let response = DecisionFormat::SystemOne
            .decode(&request, body.to_string().as_bytes())
            .unwrap();
        assert_eq!(response.usage.input_tokens, Some(10));
        assert!(matches!(
            &response.answers["route"].value,
            DecisionValue::Choice { selected, .. } if selected == "capable"
        ));
        assert!(matches!(
            response.answers["level"].value,
            DecisionValue::Score { probabilities: Some(ref p), .. } if p.len() == 2
        ));
    }

    #[test]
    fn system_one_errors() {
        let request = request();
        for body in [
            json!("not an object"),
            json!({"answers": {"route": {"type": "choice", "choice": "capable"}}}),
            json!({"answers": {
                "route": {"type": "choice", "choice": "other"},
                "hard": {"type": "noul", "noul": 0.7},
                "level": {"type": "score", "score": 1.0}
            }}),
            json!({"answers": {
                "route": {"type": "choice", "choice": "capable"},
                "hard": {"type": "noul", "noul": 0.7},
                "level": {"type": "score", "score": 5.0}
            }}),
            json!({"answers": {
                "route": {"type": "choice", "choice": "capable"},
                "hard": {"type": "noul", "noul": 0.7},
                "level": {"type": "score", "score": 1.0, "probabilities": {"0": 1.0}}
            }}),
        ] {
            assert!(
                DecisionFormat::SystemOne
                    .decode(&request, body.to_string().as_bytes())
                    .is_err(),
                "{body}"
            );
        }
        let mut no_model = request.clone();
        no_model.model = None;
        assert!(DecisionFormat::SystemOne.encode(&no_model).is_err());
    }

    #[test]
    fn openai_round_trip() {
        let request = request();
        let wire = DecisionFormat::OpenaiDecisions.encode(&request).unwrap();
        assert_eq!(wire["model"], "judge-1");
        assert_eq!(wire["input"], r#"{"task":"fix the bug"}"#);
        let questions = wire["questions"].as_array().unwrap();
        let hard = questions.iter().find(|q| q["name"] == "hard").unwrap();
        assert_eq!(hard["type"], "predicate");
        assert!(hard["instructions"]
            .as_str()
            .unwrap()
            .contains("\"true\":\"hard\""));
        let route = questions.iter().find(|q| q["name"] == "route").unwrap();
        assert_eq!(
            route["choices"][0],
            json!({"value": "capable", "description": "Start capable"})
        );
        let level = questions.iter().find(|q| q["name"] == "level").unwrap();
        assert_eq!(
            level["levels"],
            json!([{"label": "low"}, {"label": "high"}])
        );

        let body = json!({
            "model": "judge-1",
            "answers": [
                {"name": "route", "type": "choice", "choice": "efficient", "confidence": 0.5,
                 "probabilities": [{"value": "capable", "probability": 0.4}, {"value": "efficient", "probability": 0.6}]},
                {"name": "hard", "type": "predicate", "probability": 0.1},
                {"name": "level", "type": "score", "score": 0.5,
                 "probabilities": [{"value": 1, "probability": 0.5}, {"value": 0, "probability": 0.5}]}
            ],
            "usage": {"input_tokens": 50, "output_tokens": 3, "total_tokens": 53,
                      "input_tokens_details": {"cached_tokens": 10, "cache_write_tokens": 5},
                      "output_tokens_details": {"reasoning_tokens": 1}}
        });
        let response = DecisionFormat::OpenaiDecisions
            .decode(&request, body.to_string().as_bytes())
            .unwrap();
        assert_eq!(response.usage.input_tokens, Some(35));
        assert_eq!(response.usage.cached_input_tokens(), Some(10));
        assert_eq!(response.usage.output_tokens, Some(3));
        assert_eq!(response.answers.len(), 3);
    }

    #[test]
    fn openai_errors() {
        let request = request();
        let usage = json!({"input_tokens": 1, "output_tokens": 1, "total_tokens": 2});
        for answers in [
            json!([]),
            json!([{"name": "route", "type": "refusal"}]),
            json!([{"name": "unknown", "type": "predicate", "probability": 0.1}]),
            json!([{"name": "route", "type": "choice", "choice": "capable", "probabilities": []}]),
            json!([{"name": "level", "type": "score", "score": 0.5, "probabilities": [{"value": 3, "probability": 1.0}]}]),
            json!([{"name": "hard", "type": "choice", "choice": "x", "probabilities": []}]),
        ] {
            let body = json!({"model": "judge-1", "answers": answers, "usage": usage});
            assert!(
                DecisionFormat::OpenaiDecisions
                    .decode(&request, body.to_string().as_bytes())
                    .is_err(),
                "{answers}"
            );
        }
        assert!(DecisionFormat::OpenaiDecisions
            .decode(&request, b"{}")
            .is_err());
    }
}
