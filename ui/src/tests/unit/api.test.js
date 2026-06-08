/*
 * Copyright Built On Envoy
 * SPDX-License-Identifier: Apache-2.0
 * The full text of the Apache license is available in the LICENSE file at
 * the root of the repo.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { getExtensions, getSchema, postRun, postStop } from '../../lib/api.js';

beforeEach(() => {
    globalThis.fetch = vi.fn();
});

describe('getExtensions', () => {
    it('calls fetch with /api/extensions', () => {
        fetch.mockResolvedValue({ ok: true });
        getExtensions();
        expect(fetch).toHaveBeenCalledWith('/api/extensions');
    });

    it('returns the fetch promise', async () => {
        const mockResponse = { ok: true, json: async () => [] };
        fetch.mockResolvedValue(mockResponse);
        const result = await getExtensions();
        expect(result).toBe(mockResponse);
    });
});

describe('getSchema', () => {
    it('calls fetch with the correct URL-encoded extension name', () => {
        fetch.mockResolvedValue({ ok: true });
        getSchema('my ext');
        expect(fetch).toHaveBeenCalledWith('/api/extensions/my%20ext/schema');
    });

    it('works with a plain name (no encoding needed)', () => {
        fetch.mockResolvedValue({ ok: true });
        getSchema('cedar');
        expect(fetch).toHaveBeenCalledWith('/api/extensions/cedar/schema');
    });
});

describe('postRun', () => {
    it('calls fetch with POST method and JSON body', () => {
        fetch.mockResolvedValue({ ok: true });
        const payload = { extensions: [{ name: 'cedar', config: '{}' }] };
        postRun(payload);
        expect(fetch).toHaveBeenCalledWith('/api/run', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload),
        });
    });

    it('returns the fetch promise', async () => {
        const mockResponse = { ok: true };
        fetch.mockResolvedValue(mockResponse);
        const result = await postRun({ extensions: [] });
        expect(result).toBe(mockResponse);
    });
});

describe('postStop', () => {
    it('calls fetch with POST to /api/stop', () => {
        fetch.mockResolvedValue({ ok: true });
        postStop();
        expect(fetch).toHaveBeenCalledWith('/api/stop', { method: 'POST' });
    });

    it('returns the fetch promise', async () => {
        const mockResponse = { ok: true };
        fetch.mockResolvedValue(mockResponse);
        const result = await postStop();
        expect(result).toBe(mockResponse);
    });
});
