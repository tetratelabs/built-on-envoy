/*
 * Copyright Built On Envoy
 * SPDX-License-Identifier: Apache-2.0
 * The full text of the Apache license is available in the LICENSE file at
 * the root of the repo.
 */

import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

export default defineConfig({
    plugins: [svelte()],
    resolve: {
        // Resolve the browser (client) build of Svelte rather than the SSR build.
        // Required for @testing-library/svelte to work with jsdom.
        conditions: ['browser'],
    },
    build: {
        outDir: '../compiled',
        emptyOutDir: false,
        rollupOptions: {
            input: 'main.js',
            output: {
                entryFileNames: 'bundle.js',
                format: 'iife',
                name: '_boeApp',
            },
        },
    },
    test: {
        environment: 'jsdom',
        globals: false,
        setupFiles: ['./test-setup.js'],
        coverage: {
            exclude: ['main.js', 'vite.config.js', 'eslint.config.js'],
            reporter: ['text', 'lcov'],
            thresholds: {
                statements: 80,
                branches: 80,
                functions: 80,
                lines: 80,
            },
        },
    },
});
