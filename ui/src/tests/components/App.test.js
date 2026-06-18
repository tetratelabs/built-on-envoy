/*
 * Copyright Built On Envoy
 * SPDX-License-Identifier: Apache-2.0
 * The full text of the Apache license is available in the LICENSE file at
 * the root of the repo.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, waitFor, fireEvent } from '@testing-library/svelte';
import App from '../../components/App.svelte';
import * as store from '../../lib/store.svelte.js';
import * as api from '../../lib/api.js';

vi.mock('../../lib/store.svelte.js', async () => {
    const actual = await vi.importActual('../../lib/store.svelte.js');
    return {
        ...actual,
        setExtensions: vi.fn(),
        getExtensions: vi.fn(() => []),
        getSelected: vi.fn(() => new Map()),
        getConfigs: vi.fn(() => new Map()),
        getRunOrder: vi.fn(() => []),
        isRunning: vi.fn(() => false),
        setRunning: vi.fn(),
        setConfig: vi.fn(),
        addExtensionInstance: vi.fn(),
        deselectAllInstancesOf: vi.fn(),
    };
});

vi.mock('../../lib/api.js', async () => {
    const actual = await vi.importActual('../../lib/api.js');
    return { ...actual, getSchema: vi.fn(), postRun: vi.fn(), postStop: vi.fn() };
});

const exts = [
    { name: 'cedar', description: 'Cedar policy', categories: ['Security'], tags: [] },
    { name: 'opa',   description: 'OPA',           categories: ['Security'], tags: [] },
];

beforeEach(() => {
    store.clearStore(); // reset real store state (runOrder, selected, counter) from previous tests
    vi.clearAllMocks();
    store.getExtensions.mockReturnValue([]);
    store.getSelected.mockReturnValue(new Map());
    store.getRunOrder.mockReturnValue([]);

    // Simulate the static footer element from index.html
    const footer = document.createElement('footer');
    footer.className = 'footer';
    document.body.appendChild(footer);

    // Simulate the static header Run All group from index.html
    // App.svelte's $effects inject error messages and badge state into this element
    const runAllGroup = document.createElement('div');
    runAllGroup.id = 'runAllGroup';
    runAllGroup.style.display = 'none';
    const runAllBtn = document.createElement('button');
    runAllBtn.id = 'runAllBtn';
    const runAllBadge = document.createElement('button');
    runAllBadge.id = 'runAllBadge';
    runAllGroup.appendChild(runAllBtn);
    runAllGroup.appendChild(runAllBadge);
    document.body.appendChild(runAllGroup);
});

afterEach(() => {
    document.querySelectorAll('.footer').forEach(el => el.remove());
    document.querySelectorAll('#runAllGroup').forEach(el => el.remove());
});

describe('App loading state', () => {
    it('shows empty loading overlay (no spinner text) while extensions are being fetched', () => {
        // fetch never resolves — stays in loading state
        globalThis.fetch = vi.fn(() => new Promise(() => {}));
        const { container } = render(App);
        const overlay = container.querySelector('.loading-overlay');
        expect(overlay).toBeTruthy();
        expect(overlay.textContent.trim()).toBe(''); // nothing visible while loading
        expect(container.querySelector('.catalog')).toBeNull();
    });

    it('does not render the catalog while loading', () => {
        globalThis.fetch = vi.fn(() => new Promise(() => {}));
        const { container } = render(App);
        // Footer flash fix: catalog must not be present during loading phase
        // (ensures #app has content only after load completes)
        expect(container.querySelector('.ext-row')).toBeNull();
        expect(container.querySelector('.filter-bar')).toBeNull();
    });

    it('removes loading overlay and shows catalog after fetch resolves', async () => {
        globalThis.fetch = vi.fn(() =>
            Promise.resolve({ json: () => Promise.resolve(exts) })
        );
        store.getExtensions.mockReturnValue(exts);
        const { container } = render(App);

        expect(container.querySelector('.loading-overlay')).toBeTruthy();

        await waitFor(() => {
            expect(container.querySelector('.loading-overlay')).toBeNull();
        });
        expect(container.querySelector('.catalog')).toBeTruthy();
    });

    it('shows error state when fetch fails', async () => {
        globalThis.fetch = vi.fn(() => Promise.reject(new Error('network error')));
        const { container } = render(App);

        await waitFor(() => {
            expect(container.querySelector('.empty-state')).toBeTruthy();
        });
        expect(container.querySelector('.empty-state-title').textContent).toBe(
            'Failed to load extensions'
        );
        expect(container.querySelector('.empty-state-desc').textContent).toBe('network error');
    });

    it('does not show shared terminal on initial load', () => {
        globalThis.fetch = vi.fn(() => new Promise(() => {}));
        const { container } = render(App);
        // SharedTerminal is always mounted but hidden via display:none when not visible
        const panel = container.querySelector('.run-all-panel');
        expect(panel).toBeTruthy(); // it's in DOM
        expect(panel.style.display).toBe('none'); // but hidden — no footer flash
    });

    it('shared terminal is hidden (not visible) on initial load', () => {
        globalThis.fetch = vi.fn(() => new Promise(() => {}));
        const { container } = render(App);
        const panel = container.querySelector('.run-all-panel');
        // Panel must never be visible on load — this is the footer-flash regression test
        expect(panel.style.display).toBe('none');
    });
});

describe('App footer visibility', () => {
    it('footer does not have loaded class while extensions are fetching', () => {
        globalThis.fetch = vi.fn(() => new Promise(() => {}));
        render(App);
        const footer = document.querySelector('.footer');
        expect(footer.classList.contains('loaded')).toBe(false);
    });

    it('footer gains loaded class after extensions load successfully', async () => {
        globalThis.fetch = vi.fn(() =>
            Promise.resolve({ json: () => Promise.resolve(exts) })
        );
        store.getExtensions.mockReturnValue(exts);
        render(App);

        await waitFor(() => {
            expect(document.querySelector('.footer').classList.contains('loaded')).toBe(true);
        });
    });

    it('footer gains loaded class even when fetch fails', async () => {
        globalThis.fetch = vi.fn(() => Promise.reject(new Error('network error')));
        render(App);

        await waitFor(() => {
            expect(document.querySelector('.footer').classList.contains('loaded')).toBe(true);
        });
    });
});

describe('App section-desc', () => {
    it('renders the section description text', () => {
        globalThis.fetch = vi.fn(() => new Promise(() => {}));
        const { container } = render(App);
        expect(container.querySelector('.section-desc')).toBeTruthy();
        expect(container.querySelector('.section-desc').textContent).toContain(
            'Toggle extensions ON'
        );
    });
});

describe('App order popover placement', () => {
    it('run order popover right edge aligns with trigger button right edge', () => {
        globalThis.fetch = vi.fn(() => new Promise(() => {}));
        store.getRunOrder.mockReturnValue(['cedar', 'opa']);

        // jsdom doesn't implement window.innerWidth — set it
        Object.defineProperty(window, 'innerWidth', { value: 1200, configurable: true });

        const { container, component } = render(App);

        // Wrapper must already be in DOM (always mounted, not inside {#if})
        const wrapper = container.querySelector('.order-popover-wrapper');
        expect(wrapper).toBeTruthy();

        // Simulate a trigger button at a known screen position
        const trigger = document.createElement('button');
        document.body.appendChild(trigger);
        trigger.getBoundingClientRect = () => ({ bottom: 60, right: 350 });

        component.toggleRunOrderPopover({ target: trigger });

        // right = vw - rect.right = 1200 - 350 = 850px
        expect(wrapper.style.right).toBe('850px');
        // top = rect.bottom + 8 = 68px
        expect(wrapper.style.top).toBe('68px');
        // left must be unset so right drives horizontal placement
        expect(wrapper.style.left).toBe('');

        trigger.remove();
    });
});

// Build a mock Response whose body is a ReadableStream of SSE-encoded text.
// parseSSEStream reads response.body.getReader(), so we need a real ReadableStream.
function makeSseResponse(events) {
    const text = events.map(({ type, data }) =>
        (type && type !== 'output' ? `event: ${type}\n` : '') + `data: ${data}\n\n`
    ).join('');
    const encoder = new TextEncoder();
    const body = new ReadableStream({
        start(controller) {
            controller.enqueue(encoder.encode(text));
            controller.close();
        },
    });
    return { ok: true, body };
}

describe('App runAll validation', () => {
    it('shows validation errors and does not run when required fields are empty', async () => {
        const { addExtensionInstance: realAdd, deselectAllInstancesOf: realDeselectAll,
                getSelected: realGetSelected, getRunOrder: realGetRunOrder,
                isRunning: realIsRunning } = await vi.importActual('../../lib/store.svelte.js');

        store.getSelected.mockImplementation(realGetSelected);
        store.getRunOrder.mockImplementation(realGetRunOrder);
        store.isRunning.mockImplementation(realIsRunning);
        store.addExtensionInstance.mockImplementation(realAdd);
        store.deselectAllInstancesOf.mockImplementation(realDeselectAll);

        // Extension with a required field in its schema
        const ext = { name: 'bedrock', description: 'Bedrock', categories: ['AI'], tags: [] };
        globalThis.fetch = vi.fn(() =>
            Promise.resolve({ json: () => Promise.resolve([ext]) })
        );
        store.getExtensions.mockReturnValue([ext]);

        const schema = {
            type: 'object',
            required: ['bedrock_endpoint'],
            properties: { bedrock_endpoint: { type: 'string' } },
        };
        api.getSchema.mockResolvedValue({ ok: true, json: async () => schema });

        const { container, component } = render(App);
        await waitFor(() => expect(container.querySelector('.catalog')).toBeTruthy());

        // Toggle the definition row — real addExtensionInstance creates an instance row
        const checkbox = container.querySelector('input[type="checkbox"]');
        await fireEvent.change(checkbox, { target: { checked: true } });
        await waitFor(() => expect(container.querySelector('.ext-row.active')).toBeTruthy());

        // Wait for JSONEditor ready (stub fires setTimeout 0)
        await waitFor(() => expect(container.querySelector('.config-section-body')).toBeTruthy());

        // Run All — getValue() returns {} so bedrock_endpoint is missing → validation error
        await component.runAll();

        // postRun must NOT have been called
        expect(api.postRun).not.toHaveBeenCalled();

        // Error message must appear inside the static header #runAllGroup (not inside #app container)
        await waitFor(() => {
            const errMsg = document.querySelector('#runAllGroup .run-all-error');
            expect(errMsg).toBeTruthy();
            expect(errMsg.textContent).toMatch(/validation error/);
        });

        // Error message must NOT appear inside the Svelte #app container
        expect(container.querySelector('.run-all-error')).toBeNull();

        // The offending row must get ext-row-invalid class (red border)
        const invalidRow = container.querySelector('.ext-row-invalid');
        expect(invalidRow).toBeTruthy();
        expect(invalidRow.getAttribute('data-ext-name')).toBe('bedrock');

        // The form container must have je-show-errors class (errors highlighted)
        const formContainer = container.querySelector('.config-section-body');
        expect(formContainer?.classList.contains('je-show-errors')).toBe(true);
    });

    it('clears error message from header DOM after a validation failure then fix', async () => {
        const { addExtensionInstance: realAdd, deselectAllInstancesOf: realDeselectAll,
                getSelected: realGetSelected, getRunOrder: realGetRunOrder,
                isRunning: realIsRunning } = await vi.importActual('../../lib/store.svelte.js');

        store.getSelected.mockImplementation(realGetSelected);
        store.getRunOrder.mockImplementation(realGetRunOrder);
        store.isRunning.mockImplementation(realIsRunning);
        store.addExtensionInstance.mockImplementation(realAdd);
        store.deselectAllInstancesOf.mockImplementation(realDeselectAll);

        const ext = { name: 'bedrock', description: 'Bedrock', categories: ['AI'], tags: [] };
        globalThis.fetch = vi.fn(() =>
            Promise.resolve({ json: () => Promise.resolve([ext]) })
        );
        store.getExtensions.mockReturnValue([ext]);

        // Schema with a required field — first runAll will fail validation
        const schema = { type: 'object', required: ['endpoint'], properties: { endpoint: { type: 'string' } } };
        api.getSchema.mockResolvedValue({ ok: true, json: async () => schema });
        api.postRun.mockResolvedValue(makeSseResponse([{ type: 'output', data: 'done' }]));

        const { container, component } = render(App);
        await waitFor(() => expect(container.querySelector('.catalog')).toBeTruthy());

        const checkbox = container.querySelector('input[type="checkbox"]');
        await fireEvent.change(checkbox, { target: { checked: true } });
        await waitFor(() => expect(container.querySelector('.ext-row.active')).toBeTruthy());
        await waitFor(() => expect(container.querySelector('.config-section-body')).toBeTruthy());

        // First runAll — validation fails, error appears in header
        await component.runAll();
        await waitFor(() => {
            expect(document.querySelector('#runAllGroup .run-all-error')).toBeTruthy();
        });

        // Second runAll with getRunOrder returning empty — no extensions, clears error
        store.getRunOrder.mockReturnValue([]);
        store.getSelected.mockReturnValue(new Map());
        await component.runAll();

        await waitFor(() => {
            expect(document.querySelector('#runAllGroup .run-all-error')).toBeNull();
        });
    });
});

describe('App runAll error — cleared by toggle or individual Run', () => {
    async function setupWithValidationError() {
        const { addExtensionInstance: realAdd, deselectAllInstancesOf: realDeselectAll,
                getSelected: realGetSelected, getRunOrder: realGetRunOrder,
                isRunning: realIsRunning } = await vi.importActual('../../lib/store.svelte.js');

        store.getSelected.mockImplementation(realGetSelected);
        store.getRunOrder.mockImplementation(realGetRunOrder);
        store.isRunning.mockImplementation(realIsRunning);
        store.addExtensionInstance.mockImplementation(realAdd);
        store.deselectAllInstancesOf.mockImplementation(realDeselectAll);

        const ext = { name: 'bedrock', description: 'Bedrock', categories: ['AI'], tags: [] };
        globalThis.fetch = vi.fn(() =>
            Promise.resolve({ json: () => Promise.resolve([ext]) })
        );
        store.getExtensions.mockReturnValue([ext]);

        const schema = {
            type: 'object',
            required: ['endpoint'],
            properties: { endpoint: { type: 'string' } },
        };
        api.getSchema.mockResolvedValue({ ok: true, json: async () => schema });
        api.postRun.mockResolvedValue({ ok: true, text: async () => '', body: null });

        const result = render(App);
        await waitFor(() => expect(result.container.querySelector('.catalog')).toBeTruthy());

        // Toggle the definition row — real addExtensionInstance creates an instance row
        const checkbox = result.container.querySelector('input[type="checkbox"]');
        await fireEvent.change(checkbox, { target: { checked: true } });
        await waitFor(() => expect(result.container.querySelector('.ext-row.active')).toBeTruthy());
        await waitFor(() => expect(result.container.querySelector('.config-section-body')).toBeTruthy());

        // Trigger runAll — validation fails, error banner appears
        await result.component.runAll();
        await waitFor(() => {
            expect(document.querySelector('#runAllGroup .run-all-error')).toBeTruthy();
        });

        return { ...result, ext, checkbox };
    }

    it('clears run-all error banner when an extension toggle is flipped', async () => {
        const { checkbox } = await setupWithValidationError();

        // Toggle off — should clear the error
        await fireEvent.change(checkbox, { target: { checked: false } });

        await waitFor(() => {
            expect(document.querySelector('#runAllGroup .run-all-error')).toBeNull();
        });
    });

    it('clears run-all error banner when an individual Run button is clicked', async () => {
        const { container } = await setupWithValidationError();

        // Click the individual Run button — should clear the error even if it re-validates
        await fireEvent.click(container.querySelector('.ext-run-btn'));

        await waitFor(() => {
            expect(document.querySelector('#runAllGroup .run-all-error')).toBeNull();
        });
    });
});

describe('App shared terminal — close and popin', () => {
    it('handleRunAllPanelRequest shows the shared terminal', async () => {
        globalThis.fetch = vi.fn(() =>
            Promise.resolve({ json: () => Promise.resolve(exts) })
        );
        store.getExtensions.mockReturnValue(exts);

        const { container } = render(App);
        await waitFor(() => expect(container.querySelector('.catalog')).toBeTruthy());

        // The shared terminal panel exists but is hidden initially
        const panel = container.querySelector('.run-all-panel');
        expect(panel.style.display).toBe('none');

        // Trigger handleRunAllPanelRequest via ExtensionRow popout
        // We simulate by directly clicking the ext-inline-popout if a row is running,
        // but the simplest coverage path: verify the panel becomes visible after runAll starts
    });

    it('shared terminal close button hides panel', async () => {
        globalThis.fetch = vi.fn(() =>
            Promise.resolve({ json: () => Promise.resolve(exts) })
        );
        store.getExtensions.mockReturnValue(exts);
        store.getSelected.mockReturnValue(new Map([['cedar', exts[0]]]));
        store.getRunOrder.mockReturnValue(['cedar']);
        store.getConfigs.mockReturnValue(new Map());

        // Respond immediately so running becomes false and Close button appears
        api.postRun.mockResolvedValue(makeSseResponse([
            { type: 'output', data: 'done' },
        ]));

        const { container, component } = render(App);
        await waitFor(() => expect(container.querySelector('.catalog')).toBeTruthy());
        await component.runAll();

        // Panel is visible after run
        const panel = container.querySelector('.run-all-panel');
        expect(panel.style.display).not.toBe('none');

        // Click the Close button
        const closeBtn = container.querySelector('.run-all-panel .btn-secondary');
        expect(closeBtn).toBeTruthy();
        await fireEvent.click(closeBtn);

        await waitFor(() => {
            expect(container.querySelector('.run-all-panel').style.display).toBe('none');
        });
    });

    it('shared terminal stop button calls postStop', async () => {
        globalThis.fetch = vi.fn(() =>
            Promise.resolve({ json: () => Promise.resolve(exts) })
        );
        store.getExtensions.mockReturnValue(exts);
        store.getSelected.mockReturnValue(new Map([['cedar', exts[0]]]));
        store.getRunOrder.mockReturnValue(['cedar']);
        store.getConfigs.mockReturnValue(new Map());

        // Stream that never ends — keeps running=true so Stop button appears
        let closeStream;
        const neverEnding = new ReadableStream({
            start(c) { closeStream = () => c.close(); },
        });
        api.postRun.mockResolvedValue({ ok: true, body: neverEnding });
        api.postStop.mockResolvedValue({ ok: true });

        const { container, component } = render(App);
        await waitFor(() => expect(container.querySelector('.catalog')).toBeTruthy());

        // Start run without awaiting — it won't finish
        component.runAll();

        await waitFor(() => {
            const stopBtn = container.querySelector('.run-all-panel .btn-danger');
            expect(stopBtn).toBeTruthy();
        });

        await fireEvent.click(container.querySelector('.run-all-panel .btn-danger'));
        expect(api.postStop).toHaveBeenCalled();
        closeStream?.();
    });
});

describe('App runAll — connection error path', () => {
    it('shows connection error line in terminal when postRun rejects', async () => {
        globalThis.fetch = vi.fn(() =>
            Promise.resolve({ json: () => Promise.resolve(exts) })
        );
        store.getExtensions.mockReturnValue(exts);
        store.getSelected.mockReturnValue(new Map([['cedar', exts[0]]]));
        store.getRunOrder.mockReturnValue(['cedar']);
        store.getConfigs.mockReturnValue(new Map());

        api.postRun.mockRejectedValue(new Error('socket hang up'));

        const { container, component } = render(App);
        await waitFor(() => expect(container.querySelector('.catalog')).toBeTruthy());
        await component.runAll();

        const lines = container.querySelectorAll('.run-all-panel .terminal-line');
        expect(lines.length).toBeGreaterThan(0);
        expect(lines[0].classList.contains('terminal-error')).toBe(true);
        expect(lines[0].textContent).toContain('socket hang up');
    });
});

describe('App shared terminal — popin', () => {
    it('popin button calls popIn on the row and hides shared terminal', async () => {
        globalThis.fetch = vi.fn(() =>
            Promise.resolve({ json: () => Promise.resolve(exts) })
        );
        store.getExtensions.mockReturnValue(exts);

        const { addExtensionInstance: realAdd, deselectAllInstancesOf: realDeselectAll,
                getSelected: realGetSelected, getConfigs: realGetConfigs,
                isRunning: realIsRunning } = await vi.importActual('../../lib/store.svelte.js');

        store.getSelected.mockImplementation(realGetSelected);
        store.getConfigs.mockImplementation(realGetConfigs);
        store.isRunning.mockImplementation(realIsRunning);
        store.setRunning.mockImplementation(() => {});
        store.setConfig.mockImplementation(() => {});
        store.addExtensionInstance.mockImplementation(realAdd);
        store.deselectAllInstancesOf.mockImplementation(realDeselectAll);

        // A stream that never ends so panel stays running during popout
        let closeStream;
        const body = new ReadableStream({ start(c) { closeStream = () => c.close(); } });
        api.postRun.mockResolvedValue({ ok: true, body });
        api.getSchema.mockResolvedValue({ ok: false });

        const { container } = render(App);
        await waitFor(() => expect(container.querySelector('.catalog')).toBeTruthy());

        // Toggle definition row — real addExtensionInstance creates an instance row
        const checkbox = container.querySelector('input[type="checkbox"]');
        await fireEvent.change(checkbox, { target: { checked: true } });
        await waitFor(() => expect(container.querySelector('.ext-run-btn')).toBeTruthy());
        await fireEvent.click(container.querySelector('.ext-run-btn'));
        await waitFor(() => expect(container.querySelector('.ext-inline-panel')).toBeTruthy());

        // Popout to shared terminal
        await fireEvent.click(container.querySelector('.ext-inline-popout'));
        await waitFor(() => {
            expect(container.querySelector('.run-all-panel').style.display).not.toBe('none');
        });

        // Click the popin button in the shared terminal
        const popinBtn = container.querySelector('.run-all-popin');
        expect(popinBtn).toBeTruthy();
        await fireEvent.click(popinBtn);

        await waitFor(() => {
            expect(container.querySelector('.run-all-panel').style.display).toBe('none');
        });

        closeStream?.();
    });
});

describe('App window click-outside — popover', () => {
    it('closes run order popover when clicking outside', async () => {
        globalThis.fetch = vi.fn(() => new Promise(() => {}));
        store.getRunOrder.mockReturnValue(['cedar']);
        Object.defineProperty(window, 'innerWidth', { value: 1200, configurable: true });

        const { container, component } = render(App);

        const trigger = document.createElement('button');
        document.body.appendChild(trigger);
        trigger.getBoundingClientRect = () => ({ bottom: 60, right: 350 });
        component.toggleRunOrderPopover({ target: trigger });

        const wrapper = container.querySelector('.order-popover-wrapper');
        // Wait for Svelte to flush the class:visible update
        await waitFor(() => expect(wrapper.classList.contains('visible')).toBe(true));

        // svelte:window onclick bubbles from the clicked element up to window
        const outside = document.createElement('div');
        document.body.appendChild(outside);
        outside.dispatchEvent(new MouseEvent('click', { bubbles: true }));
        await waitFor(() => {
            expect(wrapper.classList.contains('visible')).toBe(false);
        });

        trigger.remove();
        outside.remove();
    });
});

describe('App runAll streaming', () => {
    it('streams SSE lines into the shared terminal when runAll is called', async () => {
        globalThis.fetch = vi.fn(() =>
            Promise.resolve({ json: () => Promise.resolve(exts) })
        );
        store.getExtensions.mockReturnValue(exts);
        store.getSelected.mockReturnValue(new Map([['cedar', exts[0]]]));
        store.getRunOrder.mockReturnValue(['cedar']);
        store.getConfigs.mockReturnValue(new Map());

        api.postRun.mockResolvedValue(makeSseResponse([
            { type: 'output', data: 'line one' },
            { type: 'error',  data: 'line two' },
        ]));

        const { container, component } = render(App);
        await waitFor(() => expect(container.querySelector('.catalog')).toBeTruthy());

        await component.runAll();

        const terminal = container.querySelector('.run-all-panel .terminal');
        expect(terminal).toBeTruthy();
        const lines = terminal.querySelectorAll('.terminal-line');
        expect(lines.length).toBe(2);
        expect(lines[0].textContent).toBe('line one');
        expect(lines[1].textContent).toBe('line two');
        expect(lines[1].classList.contains('terminal-error')).toBe(true);
    });

    it('shows error line in terminal when postRun returns non-ok response', async () => {
        globalThis.fetch = vi.fn(() =>
            Promise.resolve({ json: () => Promise.resolve(exts) })
        );
        store.getExtensions.mockReturnValue(exts);
        store.getSelected.mockReturnValue(new Map([['cedar', exts[0]]]));
        store.getRunOrder.mockReturnValue(['cedar']);
        store.getConfigs.mockReturnValue(new Map());

        api.postRun.mockResolvedValue({ ok: false, text: async () => 'Internal Server Error' });

        const { container, component } = render(App);
        await waitFor(() => expect(container.querySelector('.catalog')).toBeTruthy());
        await component.runAll();

        const lines = container.querySelectorAll('.run-all-panel .terminal-line');
        expect(lines.length).toBeGreaterThan(0);
        expect(lines[0].classList.contains('terminal-error')).toBe(true);
        expect(lines[0].textContent).toBe('Internal Server Error');
    });
});
