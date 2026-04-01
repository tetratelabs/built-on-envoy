/*
 * Copyright Built On Envoy
 * SPDX-License-Identifier: Apache-2.0
 * The full text of the Apache license is available in the LICENSE file at
 * the root of the repo.
 */

import js from '@eslint/js';
import svelte from 'eslint-plugin-svelte';
import globals from 'globals';

export default [
    js.configs.recommended,
    ...svelte.configs['flat/recommended'],
    {
        languageOptions: {
            globals: { ...globals.browser, ...globals.node },
        },
    },
    // Terminal components intentionally manage DOM directly for streaming performance.
    // ConfigForm bootstraps a third-party JSONEditor imperatively inside a $effect.
    {
        files: ['**/InlineTerminal.svelte', '**/SharedTerminal.svelte', '**/ConfigForm.svelte'],
        rules: { 'svelte/no-dom-manipulating': 'off' },
    },
    // JSONEditor is a runtime global injected via a <script> tag in index.html.
    {
        files: ['**/ConfigForm.svelte'],
        languageOptions: { globals: { JSONEditor: 'readonly' } },
    },
    // store.svelte.js uses full Map/Set reassignment (correct Svelte 5 reactivity pattern).
    // ConfigForm uses a local non-reactive Set — SvelteSet is not needed here.
    {
        rules: { 'svelte/prefer-svelte-reactivity': 'off' },
    },
    {
        ignores: ['node_modules/', 'dist/', 'coverage/'],
    },
];
