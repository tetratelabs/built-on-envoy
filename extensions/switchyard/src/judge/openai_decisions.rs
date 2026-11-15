// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

//! The OpenAI-style decisions format.

use std::collections::BTreeMap;

use serde::Deserialize;
use serde_json::{json, Value};
use switchyard_protocol::{
    BooleanEstimate, DecisionAnswer, DecisionKind, DecisionRequest, DecisionResponse,
    DecisionValue, ModelId, Probability, ProviderConfidence, ScoreValue, Usage,
};

/// Encodes a decision request as an OpenAI decisions request body.
pub fn encode(model: &str, request: &DecisionRequest) -> Result<Value, String> {
    let questions: Vec<_> = request
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
                    wire["choices"] = Value::Array(
                        options
                            .iter()
                            .map(|option| {
                                let mut choice = json!({"value": option.id});
                                if let Some(description) = &option.description {
                                    choice["description"] = json!(text(description));
                                }
                                choice
                            })
                            .collect(),
                    );
                }
                DecisionKind::Score { levels } => {
                    wire["type"] = json!("score");
                    wire["levels"] = Value::Array(
                        levels
                            .iter()
                            .map(|level| json!({"label": text(level)}))
                            .collect(),
                    );
                }
            }
            wire
        })
        .collect();
    Ok(json!({"model": model, "input": text(&request.context), "questions": questions}))
}

/// Decodes an OpenAI decisions response body, checking it against the request.
pub fn decode(request: &DecisionRequest, body: &[u8]) -> Result<DecisionResponse, String> {
    let response: WireResponse = serde_json::from_slice(body)
        .map_err(|e| format!("could not decode OpenAI Decisions response: {e}"))?;
    let mut questions = request.questions.clone();
    let mut answers = BTreeMap::new();
    for answer in response.answers {
        let invalid = || {
            format!(
                "OpenAI Decisions answer {:?} has an unexpected question name, kind, or value",
                answer.name
            )
        };
        let question = questions.remove(&answer.name).ok_or_else(invalid)?;
        let value = match (&question.kind, answer.value) {
            (DecisionKind::Boolean { .. }, WireValue::Predicate { probability }) => {
                DecisionValue::Boolean(BooleanEstimate::ProbabilityTrue(probability))
            }
            (
                DecisionKind::Choice { options },
                WireValue::Choice {
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
                    || !options.iter().any(|option| option.id == choice)
                    || options
                        .iter()
                        .any(|option| !probabilities.contains_key(&option.id))
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
                WireValue::Score {
                    score,
                    mut probabilities,
                },
            ) => {
                probabilities.sort_by_key(|p| p.value);
                if probabilities.len() != levels.len()
                    || probabilities.iter().enumerate().any(|(i, p)| p.value != i)
                    || score.0 < 0.0
                    || score.0 > (levels.len() - 1) as f64
                {
                    return Err(invalid());
                }
                DecisionValue::Score {
                    value: score,
                    probabilities: Some(probabilities.into_iter().map(|p| p.probability).collect()),
                }
            }
            (_, WireValue::Refusal) => {
                return Err(format!(
                    "OpenAI Decisions refused question {:?}",
                    answer.name
                ))
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
        return Err(
            "OpenAI Decisions answer names do not match the requested question keys".into(),
        );
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

fn text(value: &Value) -> String {
    value
        .as_str()
        .map(str::to_owned)
        .unwrap_or_else(|| value.to_string())
}

#[derive(Deserialize)]
struct WireResponse {
    model: ModelId,
    answers: Vec<WireAnswer>,
    usage: WireUsage,
}
#[derive(Deserialize)]
struct WireAnswer {
    name: String,
    #[serde(flatten)]
    value: WireValue,
    confidence: Option<ProviderConfidence>,
}
#[derive(Deserialize)]
#[serde(tag = "type", rename_all = "snake_case")]
enum WireValue {
    Predicate {
        probability: Probability,
    },
    Choice {
        choice: String,
        probabilities: Vec<WireProbability<String>>,
    },
    Score {
        score: ScoreValue,
        probabilities: Vec<WireProbability<usize>>,
    },
    Refusal,
}
#[derive(Deserialize)]
struct WireProbability<T> {
    value: T,
    probability: Probability,
}
#[derive(Deserialize)]
struct WireUsage {
    input_tokens: u64,
    output_tokens: u64,
    total_tokens: u64,
    input_tokens_details: InputTokenDetails,
    output_tokens_details: OutputTokenDetails,
}
#[derive(Deserialize)]
struct InputTokenDetails {
    cached_tokens: u64,
    cache_write_tokens: u64,
}
#[derive(Deserialize)]
struct OutputTokenDetails {
    reasoning_tokens: u64,
}

#[cfg(test)]
mod tests {
    use super::*;
    use switchyard_protocol::{ChoiceOption, DecisionQuestion};

    fn request() -> DecisionRequest {
        DecisionRequest {
            model: None,
            context: json!("Route this request"),
            questions: BTreeMap::from([(
                "route".into(),
                DecisionQuestion {
                    instructions: json!({"task": "Choose a profile."}),
                    kind: DecisionKind::Choice {
                        options: vec![
                            ChoiceOption {
                                id: "capable_first".into(),
                                description: Some(json!("Start capable")),
                            },
                            ChoiceOption {
                                id: "efficient_first".into(),
                                description: Some(json!("Start efficient")),
                            },
                        ],
                    },
                },
            )]),
        }
    }

    #[test]
    fn encodes_openai_decisions_request_like_wire_example() {
        let encoded = encode("gpt-6-luna", &request()).unwrap();
        assert_eq!(
            encoded,
            json!({
                "model": "gpt-6-luna", "input": "Route this request", "questions": [{
                    "name": "route", "type": "choice",
                    "instructions": "{\"task\":\"Choose a profile.\"}",
                    "choices": [
                        {"value": "capable_first", "description": "Start capable"},
                        {"value": "efficient_first", "description": "Start efficient"}
                    ]
                }]
            })
        );
    }

    #[test]
    fn decodes_choice_response_and_usage_from_wire_example() {
        let body = br#"{"model":"gpt-6-luna","answers":[{"type":"choice","name":"route","choice":"capable_first","confidence":0.99,"probabilities":[{"value":"capable_first","probability":0.91},{"value":"efficient_first","probability":0.09}]}],"usage":{"input_tokens":42,"input_tokens_details":{"cached_tokens":10,"cache_write_tokens":2},"output_tokens":3,"output_tokens_details":{"reasoning_tokens":1},"total_tokens":45}}"#;
        let decoded = decode(&request(), body).unwrap();
        assert_eq!(decoded.model.as_deref(), Some("gpt-6-luna"));
        assert_eq!(
            decoded.answers["route"].value,
            DecisionValue::Choice {
                selected: "capable_first".into(),
                probabilities: Some(BTreeMap::from([
                    ("capable_first".into(), Probability(0.91)),
                    ("efficient_first".into(), Probability(0.09)),
                ])),
            }
        );
        assert_eq!(decoded.usage.input_tokens, Some(30));
        assert_eq!(decoded.usage.cache, Usage::cache_details(Some(10), Some(2)));
    }

    #[test]
    fn rejects_missing_answers_and_invalid_choice_distribution() {
        let missing = br#"{"model":"m","answers":[],"usage":{"input_tokens":0,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":0,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":0}}"#;
        assert!(decode(&request(), missing)
            .unwrap_err()
            .contains("answer names do not match"));
        let invalid = br#"{"model":"m","answers":[{"type":"choice","name":"route","choice":"outside","probabilities":[{"value":"capable_first","probability":0.5},{"value":"efficient_first","probability":0.5}]}],"usage":{"input_tokens":0,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":0,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":0}}"#;
        assert!(decode(&request(), invalid)
            .unwrap_err()
            .contains("unexpected question name, kind, or value"));
    }
}
