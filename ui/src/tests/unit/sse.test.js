/*
 * Copyright Built On Envoy
 * SPDX-License-Identifier: Apache-2.0
 * The full text of the Apache license is available in the LICENSE file at
 * the root of the repo.
 */

import { describe, it, expect } from 'vitest';
import { parseSSEStream } from '../../lib/sse.js';

// Helper: build a mock Response from an array of text chunks
function mockResponse(chunks) {
    const encoder = new TextEncoder();
    let idx = 0;
    const readable = new ReadableStream({
        pull(controller) {
            if (idx < chunks.length) {
                controller.enqueue(encoder.encode(chunks[idx++]));
            } else {
                controller.close();
            }
        },
    });
    return { body: readable };
}

async function collect(response) {
    const events = [];
    for await (const event of parseSSEStream(response)) {
        events.push(event);
    }
    return events;
}

describe('parseSSEStream', () => {
    it('parses a single output event', async () => {
        const resp = mockResponse(['event: output\ndata: hello world\n\n']);
        const events = await collect(resp);
        expect(events).toEqual([{ type: 'output', data: 'hello world' }]);
    });

    it('parses a status event', async () => {
        const resp = mockResponse(['event: status\ndata: completed\n\n']);
        const events = await collect(resp);
        expect(events).toEqual([{ type: 'status', data: 'completed' }]);
    });

    it('parses an error event', async () => {
        const resp = mockResponse(['event: error\ndata: something went wrong\n\n']);
        const events = await collect(resp);
        expect(events).toEqual([{ type: 'error', data: 'something went wrong' }]);
    });

    it('parses multiple events in sequence', async () => {
        const body = [
            'event: status\ndata: started\n\n',
            'event: output\ndata: line 1\n\n',
            'event: output\ndata: line 2\n\n',
            'event: status\ndata: completed\n\n',
        ].join('');
        const resp = mockResponse([body]);
        const events = await collect(resp);
        expect(events).toEqual([
            { type: 'status', data: 'started' },
            { type: 'output', data: 'line 1' },
            { type: 'output', data: 'line 2' },
            { type: 'status', data: 'completed' },
        ]);
    });

    it('handles stream split across multiple chunks', async () => {
        // Split the SSE message across two chunks
        const chunks = [
            'event: output\n',
            'data: split line\n\n',
        ];
        const resp = mockResponse(chunks);
        const events = await collect(resp);
        expect(events).toEqual([{ type: 'output', data: 'split line' }]);
    });

    it('does not emit incomplete events at end of stream', async () => {
        // Trailing data with no terminating newline — should not be emitted
        const resp = mockResponse(['event: output\ndata: partial']);
        const events = await collect(resp);
        expect(events).toHaveLength(0);
    });

    it('defaults type to "output" when event line is missing', async () => {
        const resp = mockResponse(['data: bare data line\n\n']);
        const events = await collect(resp);
        expect(events).toEqual([{ type: 'output', data: 'bare data line' }]);
    });

    it('returns no events for an empty stream', async () => {
        const resp = mockResponse([]);
        const events = await collect(resp);
        expect(events).toHaveLength(0);
    });

    it('resets event type after a blank line', async () => {
        // Two separate events, second has no explicit event type
        const body = 'event: status\ndata: started\n\ndata: plain output\n\n';
        const resp = mockResponse([body]);
        const events = await collect(resp);
        expect(events[0].type).toBe('status');
        expect(events[1].type).toBe('output');
    });
});
