// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

//! LLM judges: chat completion requests and buffered responses.

use serde_json::Value;
use switchyard_protocol::{LlmResponse, ModelId, Request, Response, WireFormat};

/// Encodes a model call as a non-streaming request body for the judge model.
pub fn encode(format: WireFormat, model: &str, request: &Request) -> Result<Value, String> {
    let mut llm_request = request.llm_request.clone();
    switchyard_translation::prepare_request_for_target(
        &mut llm_request,
        &ModelId::new(model),
        None,
    );
    // The filter buffers the reply, so the judge must not stream.
    llm_request.stream = false;
    switchyard_translation::encode_request(&llm_request, format)
        .map_err(|e| format!("failed to encode the judge request: {e}"))
}

/// Decodes a buffered judge response body.
pub fn decode(format: WireFormat, body: &[u8]) -> Result<Response, String> {
    let json: Value = serde_json::from_slice(body)
        .map_err(|e| format!("judge response is not valid JSON: {e}"))?;
    let agg = switchyard_translation::decode_aggregated_response(&json, format)
        .map_err(|e| format!("failed to decode the judge response: {e}"))?;
    Ok(Response {
        llm_response: LlmResponse::Agg(agg),
        metadata: None,
        upstream_headers: Default::default(),
    })
}

#[cfg(test)]
mod tests {
    use serde_json::json;
    use switchyard_protocol::{completion_text, text_request};

    use super::*;

    const FORMATS: [WireFormat; 3] = [
        WireFormat::OpenAiChat,
        WireFormat::OpenAiResponses,
        WireFormat::AnthropicMessages,
    ];

    fn request(stream: bool) -> Request {
        let mut llm_request = text_request(Some("virtual".to_string()), "classify this");
        llm_request.stream = stream;
        Request {
            llm_request,
            ..Request::default()
        }
    }

    fn text_of(response: Response) -> String {
        match response.llm_response {
            LlmResponse::Agg(agg) => completion_text(&agg),
            _ => panic!("expected an aggregated response"),
        }
    }

    #[test]
    fn encode_sets_judge_model_and_disables_streaming_for_all_formats() {
        for format in FORMATS {
            let body = encode(format, "judge-model", &request(true)).unwrap();
            assert_eq!(body["model"], "judge-model", "{format:?}");
            assert!(
                matches!(body.get("stream"), None | Some(Value::Bool(false))),
                "{format:?}: {body}"
            );
        }
    }

    #[test]
    fn decode_openai_chat_completion() {
        let body = json!({
            "id": "chatcmpl-1", "object": "chat.completion", "model": "gpt-4.1-mini",
            "choices": [{"index": 0, "finish_reason": "stop",
                "message": {"role": "assistant", "content": "hello there"}}],
            "usage": {"prompt_tokens": 3, "completion_tokens": 2, "total_tokens": 5}
        });
        let response = decode(WireFormat::OpenAiChat, body.to_string().as_bytes()).unwrap();
        assert_eq!(text_of(response), "hello there");
    }

    #[test]
    fn decode_anthropic_messages() {
        let body = json!({
            "id": "msg_1", "type": "message", "role": "assistant", "model": "claude",
            "content": [{"type": "text", "text": "hi from claude"}],
            "stop_reason": "end_turn",
            "usage": {"input_tokens": 3, "output_tokens": 4}
        });
        let response = decode(WireFormat::AnthropicMessages, body.to_string().as_bytes()).unwrap();
        assert_eq!(text_of(response), "hi from claude");
    }

    #[test]
    fn decode_rejects_invalid_json() {
        for format in FORMATS {
            assert!(decode(format, b"not json").is_err(), "{format:?}");
        }
    }
}
