/*
 * Copyright Built On Envoy
 * SPDX-License-Identifier: Apache-2.0
 * The full text of the Apache license is available in the LICENSE file at
 * the root of the repo.
 */

// BOE Extension Manager — SSE Stream Parser
//
// Pure business logic: no DOM.
// Reads a fetch Response body and yields typed SSE event objects.
// The presentation layer (ui-terminal.js) consumes these events and renders them.

/**
 * Parse a Server-Sent Events stream from a fetch Response.
 * Yields objects of the form { type: string, data: string }.
 *
 * Recognized event types: 'output', 'status', 'error'.
 * Unknown event types are also yielded as-is.
 *
 * @param {Response} response - A fetch Response with a readable body
 * @yields {{ type: string, data: string }}
 */
export async function* parseSSEStream(response) {
    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let buffer = '';
    let eventType = '';

    while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        buffer += decoder.decode(value, { stream: true });
        const lines = buffer.split('\n');
        // Keep the last (possibly incomplete) line in the buffer
        buffer = lines.pop();

        for (const line of lines) {
            if (line.startsWith('event: ')) {
                eventType = line.slice(7).trim();
            } else if (line.startsWith('data: ')) {
                yield { type: eventType || 'output', data: line.slice(6) };
                eventType = '';
            }
            // Blank lines separate events — reset event type
            else if (line === '') {
                eventType = '';
            }
        }
    }
}
