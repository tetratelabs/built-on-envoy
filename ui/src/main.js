/*
 * Copyright Built On Envoy
 * SPDX-License-Identifier: Apache-2.0
 * The full text of the Apache license is available in the LICENSE file at
 * the root of the repo.
 */

import { mount } from 'svelte';
import App from './components/App.svelte';

const app = mount(App, { target: document.getElementById('app') });

// Bridge for inline onclick handlers in index.html
// (catalog.runAll(), catalog.toggleRunOrderPopover())
window.catalog = {
    runAll: () => app.runAll?.(),
    toggleRunOrderPopover: (e) => app.toggleRunOrderPopover?.(e),
};
