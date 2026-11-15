// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

//! The TypeSafe System One decision format, served by Jev.

use std::collections::BTreeMap;

use serde::Deserialize;
use serde_json::{json, Value};
use switchyard_protocol::{
    BooleanEstimate, DecisionAnswer, DecisionKind, DecisionRequest, DecisionResponse,
    DecisionValue, ModelId, Probability, ProviderConfidence, ScoreValue, Usage,
};

/// Encodes a decision request as a System One request body.
pub fn encode(model: &str, request: &DecisionRequest) -> Result<Value, String> {
    let mut questions = serde_json::Map::new();
    for (id, question) in &request.questions {
        let (kind, criteria) = match &question.kind {
            DecisionKind::Boolean {
                true_description,
                false_description,
            } => {
                let mut criteria = serde_json::Map::new();
                for (key, description) in [("true", true_description), ("false", false_description)]
                {
                    if let Some(description) = description {
                        criteria.insert(key.into(), description.clone());
                    }
                }
                ("noul", Value::Object(criteria))
            }
            DecisionKind::Choice { options } => {
                let mut criteria = serde_json::Map::new();
                for option in options {
                    let description = option.description.clone().unwrap_or(Value::Null);
                    if criteria.insert(option.id.clone(), description).is_some() {
                        return Err(format!(
                            "duplicate option {:?} in questions.{id}",
                            option.id
                        ));
                    }
                }
                ("choice", Value::Object(criteria))
            }
            DecisionKind::Score { levels } => ("score", json!(levels)),
        };
        questions.insert(
            id.clone(),
            json!({
                "type": kind, "instructions": question.instructions, "criteria": criteria,
            }),
        );
    }
    Ok(json!({"model": model, "state": request.context, "questions": questions}))
}

/// Decodes a System One response body, checking it against the request.
pub fn decode(request: &DecisionRequest, body: &[u8]) -> Result<DecisionResponse, String> {
    let response: WireResponse = serde_json::from_slice(body)
        .map_err(|source| format!("System One response is not valid JSON: {source}"))?;
    if !response.answers.keys().eq(request.questions.keys()) {
        return Err("System One answer keys do not match the requested question keys".into());
    }
    let answers = response
        .answers
        .into_iter()
        .map(|(id, answer)| {
            let kind = &request.questions[&id].kind;
            let value = decode_value(&id, kind, answer.value)?;
            Ok((
                id,
                DecisionAnswer {
                    value,
                    provider_confidence: answer.confidence,
                },
            ))
        })
        .collect::<Result<BTreeMap<_, _>, String>>()?;
    Ok(DecisionResponse {
        id: response.id,
        model: response.model,
        answers,
        usage: response.usage,
    })
}

fn decode_value(id: &str, kind: &DecisionKind, value: WireValue) -> Result<DecisionValue, String> {
    match (kind, value) {
        (DecisionKind::Boolean { .. }, WireValue::Noul { noul }) => Ok(DecisionValue::Boolean(
            BooleanEstimate::ProbabilityTrue(noul),
        )),
        (
            DecisionKind::Choice { options },
            WireValue::Choice {
                choice,
                probabilities,
            },
        ) => {
            if !options.iter().any(|option| option.id == choice) {
                return Err(format!(
                    "answer {id:?} selects an option missing from its question"
                ));
            }
            Ok(DecisionValue::Choice {
                selected: choice,
                probabilities,
            })
        }
        (
            DecisionKind::Score { levels },
            WireValue::Score {
                score,
                probabilities,
            },
        ) => {
            if levels.is_empty() || !(0.0..=(levels.len() - 1) as f64).contains(&score.0) {
                return Err(format!("score for {id:?} lies outside its rubric"));
            }
            let probabilities = probabilities
                .map(|probabilities| order_by_rubric(id, levels.len(), probabilities))
                .transpose()?;
            Ok(DecisionValue::Score {
                value: score,
                probabilities,
            })
        }
        _ => Err(format!(
            "answer {id:?} has an unexpected question ID, kind, or value"
        )),
    }
}

fn order_by_rubric(
    id: &str,
    levels: usize,
    mut probabilities: BTreeMap<String, Probability>,
) -> Result<Vec<Probability>, String> {
    let invalid = || format!("score probabilities for {id:?} do not match its rubric");
    if probabilities.len() != levels {
        return Err(invalid());
    }
    // JSON object keys are strings, so rubric indexes are looked up as "0".."N-1".
    (0..levels)
        .map(|index| probabilities.remove(&index.to_string()).ok_or_else(invalid))
        .collect()
}

#[derive(Deserialize)]
struct WireResponse {
    id: Option<String>,
    model: Option<ModelId>,
    answers: BTreeMap<String, WireAnswer>,
    #[serde(default)]
    usage: Usage,
}

#[derive(Deserialize)]
struct WireAnswer {
    #[serde(flatten)]
    value: WireValue,
    confidence: Option<ProviderConfidence>,
}

#[derive(Deserialize)]
#[serde(tag = "type", rename_all = "snake_case")]
enum WireValue {
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

#[cfg(test)]
mod tests {
    use super::*;
    use switchyard_protocol::{ChoiceOption, DecisionQuestion};

    fn question(kind: DecisionKind) -> DecisionQuestion {
        DecisionQuestion {
            instructions: json!("judge this"),
            kind,
        }
    }

    fn tier_kind() -> DecisionKind {
        DecisionKind::Choice {
            options: vec![
                ChoiceOption {
                    id: "efficient".into(),
                    description: None,
                },
                ChoiceOption {
                    id: "capable".into(),
                    description: Some(json!("hard work")),
                },
            ],
        }
    }

    fn risk_kind() -> DecisionKind {
        DecisionKind::Score {
            levels: vec![json!("low"), json!("medium"), json!("high")],
        }
    }

    fn ok_kind() -> DecisionKind {
        DecisionKind::Boolean {
            true_description: Some(json!("yes")),
            false_description: None,
        }
    }

    fn request(questions: Vec<(&str, DecisionKind)>) -> DecisionRequest {
        DecisionRequest {
            model: None,
            context: json!({"prompt": "hi"}),
            questions: questions
                .into_iter()
                .map(|(id, kind)| (id.to_string(), question(kind)))
                .collect(),
        }
    }

    const REAL_BODY: &[u8] = br#"{"model":"jev-1.14.0","answers":{"tier":{"type":"choice","choice":"capable","confidence":1.0,"probabilities":{"efficient":0.0,"capable":1.0}}},"usage":{"input_tokens":342,"output_tokens":33}}"#;

    #[test]
    fn encode_boolean_uses_noul_with_true_criteria() {
        let body = encode("jev-latest", &request(vec![("ok", ok_kind())])).unwrap();
        assert_eq!(body["model"], "jev-latest");
        assert_eq!(body["state"], json!({"prompt": "hi"}));
        assert_eq!(body["questions"]["ok"]["type"], "noul");
        assert_eq!(body["questions"]["ok"]["criteria"], json!({"true": "yes"}));
        assert_eq!(body["questions"]["ok"]["instructions"], "judge this");
    }

    #[test]
    fn encode_choice_maps_option_descriptions_to_criteria() {
        let body = encode("jev-latest", &request(vec![("tier", tier_kind())])).unwrap();
        assert_eq!(body["questions"]["tier"]["type"], "choice");
        assert_eq!(
            body["questions"]["tier"]["criteria"],
            json!({"efficient": null, "capable": "hard work"})
        );
    }

    #[test]
    fn encode_score_uses_levels_as_criteria() {
        let body = encode("jev-latest", &request(vec![("risk", risk_kind())])).unwrap();
        assert_eq!(body["questions"]["risk"]["type"], "score");
        assert_eq!(
            body["questions"]["risk"]["criteria"],
            json!(["low", "medium", "high"])
        );
    }

    #[test]
    fn encode_rejects_duplicate_choice_option_ids() {
        let kind = DecisionKind::Choice {
            options: vec![
                ChoiceOption {
                    id: "same".into(),
                    description: None,
                },
                ChoiceOption {
                    id: "same".into(),
                    description: None,
                },
            ],
        };
        let err = encode("jev-latest", &request(vec![("tier", kind)])).unwrap_err();
        assert!(err.contains("duplicate option"), "{err}");
    }

    #[test]
    fn decode_real_jev_example() {
        let response = decode(&request(vec![("tier", tier_kind())]), REAL_BODY).unwrap();
        assert_eq!(response.model, Some(ModelId::new("jev-1.14.0")));
        assert_eq!(response.usage.input_tokens, Some(342));
        assert_eq!(response.usage.output_tokens, Some(33));
        let answer = &response.answers["tier"];
        assert_eq!(answer.provider_confidence, Some(ProviderConfidence(1.0)));
        assert_eq!(
            answer.value,
            DecisionValue::Choice {
                selected: "capable".into(),
                probabilities: Some(BTreeMap::from([
                    ("efficient".into(), Probability(0.0)),
                    ("capable".into(), Probability(1.0)),
                ])),
            }
        );
    }

    #[test]
    fn decode_noul_maps_to_probability_true() {
        let body = br#"{"answers":{"ok":{"type":"noul","noul":0.8,"confidence":null}}}"#;
        let response = decode(&request(vec![("ok", ok_kind())]), body).unwrap();
        assert_eq!(
            response.answers["ok"].value,
            DecisionValue::Boolean(BooleanEstimate::ProbabilityTrue(Probability(0.8)))
        );
        assert_eq!(response.answers["ok"].provider_confidence, None);
    }

    #[test]
    fn decode_rejects_answer_key_mismatch() {
        let body = br#"{"answers":{"tier":{"type":"choice","choice":"capable"},"extra":{"type":"choice","choice":"capable"}}}"#;
        let err = decode(&request(vec![("tier", tier_kind())]), body).unwrap_err();
        assert!(err.contains("keys do not match"), "{err}");
    }

    #[test]
    fn decode_rejects_unknown_choice() {
        let body = br#"{"answers":{"tier":{"type":"choice","choice":"genius"}}}"#;
        let err = decode(&request(vec![("tier", tier_kind())]), body).unwrap_err();
        assert!(err.contains("missing from its question"), "{err}");
        assert!(!err.contains("genius"), "{err}");
    }

    #[test]
    fn decode_rejects_out_of_range_score() {
        for score in ["3.0", "-0.5"] {
            let body = format!(r#"{{"answers":{{"risk":{{"type":"score","score":{score}}}}}}}"#);
            let err = decode(&request(vec![("risk", risk_kind())]), body.as_bytes()).unwrap_err();
            assert!(err.contains("outside its rubric"), "{err}");
        }
    }

    #[test]
    fn decode_accepts_score_on_the_rubric_bounds() {
        let body = br#"{"answers":{"risk":{"type":"score","score":2.0}}}"#;
        let response = decode(&request(vec![("risk", risk_kind())]), body).unwrap();
        assert_eq!(
            response.answers["risk"].value,
            DecisionValue::Score {
                value: ScoreValue(2.0),
                probabilities: None,
            }
        );
    }

    #[test]
    fn decode_reorders_score_probabilities_by_rubric_index() {
        let body = br#"{"answers":{"risk":{"type":"score","score":1.5,"probabilities":{"2":0.2,"0":0.5,"1":0.3}}}}"#;
        let response = decode(&request(vec![("risk", risk_kind())]), body).unwrap();
        assert_eq!(
            response.answers["risk"].value,
            DecisionValue::Score {
                value: ScoreValue(1.5),
                probabilities: Some(vec![Probability(0.5), Probability(0.3), Probability(0.2)]),
            }
        );
    }

    #[test]
    fn decode_rejects_invalid_json_without_echoing_body() {
        let err = decode(&request(vec![("tier", tier_kind())]), b"{not json").unwrap_err();
        assert!(
            err.starts_with("System One response is not valid JSON"),
            "{err}"
        );
        assert!(!err.contains("not json"), "{err}");
    }
}
