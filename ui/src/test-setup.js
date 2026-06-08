/*
 * Copyright Built On Envoy
 * SPDX-License-Identifier: Apache-2.0
 * The full text of the Apache license is available in the LICENSE file at
 * the root of the repo.
 */

import { vi } from 'vitest';

// Mock fetch globally
globalThis.fetch = vi.fn();

// Stub window.JSONEditor — minimal stand-in used by ConfigForm tests.
// Individual tests can override window.JSONEditor with spy-enabled stubs.
globalThis.JSONEditor = class {
    constructor(container, opts) {
        this._opts = opts;
        this._listeners = {};
        this.element = container;
    }
    on(event, cb) {
        this._listeners[event] = cb;
        // Auto-fire 'ready' so $effect-based tests don't need to manually trigger it
        if (event === 'ready') setTimeout(cb, 0);
    }
    getValue() { return {}; }
    setValue(v) { this._value = v; }
    validate() { return []; }
    getEditor() { return null; }
    destroy() {}
};
