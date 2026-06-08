/*
 * Copyright Built On Envoy
 * SPDX-License-Identifier: Apache-2.0
 * The full text of the Apache license is available in the LICENSE file at
 * the root of the repo.
 */

// BOE Extension Manager — API Client
//
// Thin fetch wrappers for all server API calls.
// Returns Response objects; callers decide how to handle errors and body parsing.

/**
 * Fetch the list of all available extensions.
 * @returns {Promise<Response>}
 */
export function getExtensions() {
    return fetch('/api/extensions');
}

/**
 * Fetch the JSON schema for a specific extension.
 * @param {string} name - Extension name
 * @returns {Promise<Response>}
 */
export function getSchema(name) {
    return fetch(`/api/extensions/${encodeURIComponent(name)}/schema`);
}

/**
 * Start a run for the given extensions. Returns a streaming SSE Response.
 * @param {{ extensions: Array<{name: string, config: string}> }} payload
 * @returns {Promise<Response>}
 */
export function postRun(payload) {
    return fetch('/api/run', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
    });
}

/**
 * Stop the currently running command.
 * @returns {Promise<Response>}
 */
export function postStop() {
    return fetch('/api/stop', { method: 'POST' });
}
