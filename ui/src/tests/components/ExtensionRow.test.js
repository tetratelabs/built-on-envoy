/*
 * Copyright Built On Envoy
 * SPDX-License-Identifier: Apache-2.0
 * The full text of the Apache license is available in the LICENSE file at
 * the root of the repo.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, fireEvent, waitFor } from '@testing-library/svelte';
import ExtensionRow from '../../components/ExtensionRow.svelte';
import * as store from '../../lib/store.svelte.js';
import * as api from '../../lib/api.js';

vi.mock('../../lib/store.svelte.js', async () => {
    const actual = await vi.importActual('../../lib/store.svelte.js');
    return {
        ...actual,
        addExtensionInstance: vi.fn(),
        deselectExtension: vi.fn(),
        deselectAllInstancesOf: vi.fn(),
        getInstancesOf: vi.fn(() => []),
        setConfig: vi.fn(),
        getSelected: vi.fn(() => new Map()),
        getConfigs: vi.fn(() => new Map()),
        isRunning: vi.fn(() => false),
    };
});

vi.mock('../../lib/api.js', async () => {
    const actual = await vi.importActual('../../lib/api.js');
    return {
        ...actual,
        getSchema: vi.fn(),
        postRun: vi.fn(),
        postStop: vi.fn(),
    };
});

const makeExt = (overrides = {}) => ({
    name: 'test-ext',
    description: 'A test extension',
    categories: ['Security'],
    tags: [],
    filterType: [],
    ...overrides,
});

// Helper: render an instance row (isOn = true, body shown, schema loaded on mount)
const renderInstance = (ext, overrides = {}) =>
    render(ExtensionRow, { props: { ext, instanceId: 'test-ext#1', ...overrides } });

// Helper: render a definition row (isOn = false, no body)
const renderDefinition = (ext, overrides = {}) =>
    render(ExtensionRow, { props: { ext, instanceId: null, ...overrides } });

beforeEach(() => {
    vi.clearAllMocks();
    store.getSelected.mockReturnValue(new Map());
    store.getConfigs.mockReturnValue(new Map());
    store.getInstancesOf.mockReturnValue([]);
    store.isRunning.mockReturnValue(false);
    api.getSchema.mockResolvedValue({ ok: false });
});

describe('ExtensionRow — rendering', () => {
    it('renders extension name and description', () => {
        const ext = makeExt();
        const { container } = renderDefinition(ext);
        expect(container.querySelector('.ext-row-name').textContent).toBe('test-ext');
        expect(container.querySelector('.ext-row-desc').textContent).toBe('A test extension');
    });

    it('renders categories as badges', () => {
        const ext = makeExt({ categories: ['Security', 'Auth'] });
        const { container } = renderDefinition(ext);
        const badges = container.querySelectorAll('.ext-row-categories span');
        expect(badges.length).toBe(2);
        expect(badges[0].textContent).toBe('Security');
        expect(badges[1].textContent).toBe('Auth');
    });

    it('definition row: toggle is unchecked, no body', () => {
        const ext = makeExt();
        const { container } = renderDefinition(ext);
        expect(container.querySelector('input[type="checkbox"]').checked).toBe(false);
        expect(container.querySelector('.ext-row-body')).toBeNull();
    });

    it('instance row: no toggle, has remove button, body is shown', () => {
        const ext = makeExt();
        const { container } = renderInstance(ext);
        expect(container.querySelector('input[type="checkbox"]')).toBeNull();
        expect(container.querySelector('.ext-remove-btn')).toBeTruthy();
        expect(container.querySelector('.ext-row-body')).toBeTruthy();
    });

    it('instance row: active class is set', () => {
        const { container } = renderInstance(makeExt());
        expect(container.querySelector('.ext-row').classList.contains('active')).toBe(true);
    });

    it('definition row: active class is not set when no instances exist', () => {
        const { container } = renderDefinition(makeExt());
        expect(container.querySelector('.ext-row').classList.contains('active')).toBe(false);
    });

    it('extension name link points to builtonenvoy.io', () => {
        const ext = makeExt({ name: 'my ext' });
        const { container } = renderDefinition(ext);
        const link = container.querySelector('.ext-row-name');
        expect(link.getAttribute('href')).toContain('builtonenvoy.io');
        expect(link.getAttribute('href')).toContain(encodeURIComponent('my ext'));
    });

    it('shows Run Extension button in instance row', () => {
        const { container } = renderInstance(makeExt());
        const runBtn = container.querySelector('.ext-run-btn');
        expect(runBtn).toBeTruthy();
        expect(runBtn.textContent).toContain('Run Extension');
    });

    it('disables Run button when isRunning is true', () => {
        store.isRunning.mockReturnValue(true);
        const { container } = renderInstance(makeExt());
        expect(container.querySelector('.ext-run-btn').disabled).toBe(true);
    });

    it('enables Run button when isRunning is false', () => {
        store.isRunning.mockReturnValue(false);
        const { container } = renderInstance(makeExt());
        expect(container.querySelector('.ext-run-btn').disabled).toBe(false);
    });

    it('does not show filter type selector when extension has ≤1 filter types', () => {
        const { container } = renderInstance(makeExt({ filterType: ['http'] }));
        expect(container.querySelector('.filter-type-selector')).toBeNull();
    });

    it('shows filter type selector when extension has multiple filter types', () => {
        const ext = makeExt({ filterType: ['udp_listener', 'network'] });
        const { container } = renderInstance(ext);
        const selector = container.querySelector('.filter-type-selector');
        expect(selector).toBeTruthy();
        const options = [...selector.querySelectorAll('option')].map(o => o.value).filter(v => v);
        expect(options).toContain('udp_listener');
        expect(options).toContain('network');
    });
});

describe('ExtensionRow — toggle behavior', () => {
    it('definition row toggle ON calls addExtensionInstance', async () => {
        const ext = makeExt();
        const { container } = renderDefinition(ext);
        await fireEvent.change(container.querySelector('input[type="checkbox"]'), { target: { checked: true } });
        expect(store.addExtensionInstance).toHaveBeenCalledWith(ext);
    });

    it('definition row toggle OFF calls deselectAllInstancesOf', async () => {
        store.getInstancesOf.mockReturnValue(['test-ext#1']);
        const ext = makeExt();
        const { container } = renderDefinition(ext);
        await fireEvent.change(container.querySelector('input[type="checkbox"]'), { target: { checked: false } });
        expect(store.deselectAllInstancesOf).toHaveBeenCalledWith(ext.name);
    });

    it('definition row: active and toggle checked when instances exist', () => {
        store.getInstancesOf.mockReturnValue(['test-ext#1']);
        const { container } = renderDefinition(makeExt());
        expect(container.querySelector('.ext-row').classList.contains('active')).toBe(true);
        expect(container.querySelector('input[type="checkbox"]').checked).toBe(true);
    });

    it('definition row shows + button when active', () => {
        store.getInstancesOf.mockReturnValue(['test-ext#1']);
        const { container } = renderDefinition(makeExt());
        expect(container.querySelector('.ext-add-btn')).toBeTruthy();
    });

    it('+ button click calls addExtensionInstance', async () => {
        store.getInstancesOf.mockReturnValue(['test-ext#1']);
        const ext = makeExt();
        const { container } = renderDefinition(ext);
        await fireEvent.click(container.querySelector('.ext-add-btn'));
        expect(store.addExtensionInstance).toHaveBeenCalledWith(ext);
    });

    it('instance row × button calls deselectExtension with the instanceId', async () => {
        const ext = makeExt();
        const { container } = render(ExtensionRow, { props: { ext, instanceId: 'test-ext#2' } });
        await fireEvent.click(container.querySelector('.ext-remove-btn'));
        expect(store.deselectExtension).toHaveBeenCalledWith('test-ext#2');
    });

    it('calls onClearRunAllError when definition row toggle is activated', async () => {
        const onClearRunAllError = vi.fn();
        const ext = makeExt();
        const { container } = renderDefinition(ext, { onClearRunAllError });
        await fireEvent.change(container.querySelector('input[type="checkbox"]'), { target: { checked: true } });
        expect(onClearRunAllError).toHaveBeenCalled();
    });

    it('calls onClearRunAllError when instance row × button is clicked', async () => {
        const onClearRunAllError = vi.fn();
        const { container } = renderInstance(makeExt(), { onClearRunAllError });
        await fireEvent.click(container.querySelector('.ext-remove-btn'));
        expect(onClearRunAllError).toHaveBeenCalled();
    });

    it('clicking header on instance row collapses the body', async () => {
        const { container } = renderInstance(makeExt());
        await waitFor(() => expect(container.querySelector('.ext-row.active')).toBeTruthy());
        await fireEvent.click(container.querySelector('.ext-row-header'));
        await waitFor(() => {
            expect(container.querySelector('.ext-row').classList.contains('collapsed')).toBe(true);
        });
    });
});

describe('ExtensionRow — schema loading', () => {
    it('fetches schema when instance row mounts (instanceId !== null)', async () => {
        renderInstance(makeExt());
        await waitFor(() => {
            expect(api.getSchema).toHaveBeenCalledWith('test-ext');
        });
    });

    it('does not fetch schema for definition row', async () => {
        renderDefinition(makeExt());
        // Brief pause to ensure no async schema fetch is triggered
        await new Promise(r => setTimeout(r, 50));
        expect(api.getSchema).not.toHaveBeenCalled();
    });

    it('schema is loaded exactly once per instance row mount', async () => {
        renderInstance(makeExt());
        await waitFor(() => expect(api.getSchema).toHaveBeenCalledTimes(1));
        await new Promise(r => setTimeout(r, 50));
        expect(api.getSchema).toHaveBeenCalledTimes(1);
    });

    it('schema load failure sets schema to null (no-schema message shown)', async () => {
        api.getSchema.mockRejectedValue(new Error('network failure'));
        const { container } = renderInstance(makeExt());
        await waitFor(() => {
            expect(container.querySelector('.no-schema-msg')).toBeTruthy();
        });
    });
});

describe('ExtensionRow — run functionality', () => {
    it('run button click shows inline panel', async () => {
        api.postRun.mockResolvedValue({ ok: true, text: async () => '', body: null });
        const { container } = renderInstance(makeExt());
        await waitFor(() => expect(container.querySelector('.ext-run-btn')).toBeTruthy());
        await fireEvent.click(container.querySelector('.ext-run-btn'));
        await waitFor(() => {
            expect(container.querySelector('.ext-inline-panel')).toBeTruthy();
        });
    });

    it('shows error line in panel when postRun returns non-ok', async () => {
        api.postRun.mockResolvedValue({ ok: false, text: async () => 'Server error' });
        const { container } = renderInstance(makeExt());
        await waitFor(() => expect(container.querySelector('.ext-run-btn')).toBeTruthy());
        await fireEvent.click(container.querySelector('.ext-run-btn'));
        await waitFor(() => {
            const errorLines = container.querySelectorAll('.terminal-error');
            expect(errorLines.length).toBeGreaterThan(0);
            expect(errorLines[0].textContent).toBe('Server error');
        });
    });

    it('close button hides the inline panel', async () => {
        api.postRun.mockResolvedValue({ ok: true, text: async () => '', body: null });
        const { container } = renderInstance(makeExt());
        await waitFor(() => expect(container.querySelector('.ext-run-btn')).toBeTruthy());
        await fireEvent.click(container.querySelector('.ext-run-btn'));
        await waitFor(() => expect(container.querySelector('.ext-inline-panel')).toBeTruthy());
        await fireEvent.click(container.querySelector('.btn-secondary'));
        await waitFor(() => {
            expect(container.querySelector('.ext-inline-panel')).toBeNull();
        });
    });

    it('stop button calls postStop', async () => {
        let closeStream;
        const neverEndingBody = new ReadableStream({
            start(controller) { closeStream = () => controller.close(); },
        });
        api.postRun.mockResolvedValue({ ok: true, body: neverEndingBody });
        api.postStop.mockResolvedValue({ ok: true });

        const { container } = renderInstance(makeExt());
        await waitFor(() => expect(container.querySelector('.ext-run-btn')).toBeTruthy());
        await fireEvent.click(container.querySelector('.ext-run-btn'));
        await waitFor(() => expect(container.querySelector('.btn-danger')).toBeTruthy());
        await fireEvent.click(container.querySelector('.btn-danger'));
        expect(api.postStop).toHaveBeenCalled();
        closeStream?.();
    });

    it('calls onClearRunAllError when Run button is clicked', async () => {
        api.postRun.mockResolvedValue({ ok: true, text: async () => '', body: null });
        const onClearRunAllError = vi.fn();
        const { container } = renderInstance(makeExt(), { onClearRunAllError });
        await waitFor(() => expect(container.querySelector('.ext-run-btn')).toBeTruthy());
        await fireEvent.click(container.querySelector('.ext-run-btn'));
        expect(onClearRunAllError).toHaveBeenCalled();
    });

    it('pre-selects the first filter type so run proceeds without filter-type error', async () => {
        const ext = makeExt({ filterType: ['udp_listener', 'network'] });
        api.postRun.mockResolvedValue({ ok: true, body: new ReadableStream({ start(c) { c.close(); } }) });
        const { container } = renderInstance(ext);
        await waitFor(() => expect(container.querySelector('.ext-run-btn')).toBeTruthy());
        await fireEvent.click(container.querySelector('.ext-run-btn'));
        // postRun should be called (no filter-type validation error)
        await waitFor(() => expect(api.postRun).toHaveBeenCalled());
        expect(container.querySelector('.ext-run-error')).toBeNull();
    });

    it('shows run error message when validation fails', async () => {
        const schema = { type: 'object', required: ['endpoint'], properties: { endpoint: { type: 'string' } } };
        api.getSchema.mockResolvedValue({ ok: true, json: async () => schema });
        const { container } = renderInstance(makeExt());
        // Wait for schema to load and the form (JSONEditor) to mount
        await waitFor(() => expect(container.querySelector('.config-section-body')).toBeTruthy());
        await fireEvent.click(container.querySelector('.ext-run-btn'));
        await waitFor(() => {
            const errMsg = container.querySelector('.ext-run-error');
            expect(errMsg).toBeTruthy();
            expect(errMsg.textContent).toMatch(/validation error/);
        });
        expect(api.postRun).not.toHaveBeenCalled();
    });

    it('calls onRunAllPanelRequest on popout', async () => {
        let resolveStream;
        const body = new ReadableStream({
            async start(controller) {
                await new Promise(r => { resolveStream = r; });
                controller.close();
            },
        });
        api.postRun.mockResolvedValue({ ok: true, body });

        const onRunAllPanelRequest = vi.fn();
        const { container } = renderInstance(makeExt(), { onRunAllPanelRequest });
        await waitFor(() => expect(container.querySelector('.ext-run-btn')).toBeTruthy());
        await fireEvent.click(container.querySelector('.ext-run-btn'));
        await waitFor(() => expect(container.querySelector('.ext-inline-panel')).toBeTruthy());

        await fireEvent.click(container.querySelector('.ext-inline-popout'));
        expect(onRunAllPanelRequest).toHaveBeenCalledWith(
            expect.objectContaining({ title: 'Run: test-ext' })
        );
        resolveStream?.();
    });

    it('popout passes setSharedAppend callback that mirrors lines to the shared terminal', async () => {
        let resolveStream;
        const encoder = new TextEncoder();
        const body = new ReadableStream({
            async start(controller) {
                await new Promise(r => { resolveStream = r; });
                controller.enqueue(encoder.encode('data: hello from stream\n\n'));
                controller.close();
            },
        });
        api.postRun.mockResolvedValue({ ok: true, body });

        const onRunAllPanelRequest = vi.fn();
        const { container } = renderInstance(makeExt(), { onRunAllPanelRequest });
        await waitFor(() => expect(container.querySelector('.ext-run-btn')).toBeTruthy());
        await fireEvent.click(container.querySelector('.ext-run-btn'));
        await waitFor(() => expect(container.querySelector('.ext-inline-panel')).toBeTruthy());

        await fireEvent.click(container.querySelector('.ext-inline-popout'));
        const { setSharedAppend } = onRunAllPanelRequest.mock.calls[0][0];
        const sharedAppend = vi.fn();
        setSharedAppend(sharedAppend);

        resolveStream();
        await waitFor(() => {
            expect(sharedAppend).toHaveBeenCalledWith('hello from stream', '');
        });
    });
});

describe('ExtensionRow — public API', () => {
    it('getName() returns the extension name', () => {
        const { component } = renderDefinition(makeExt({ name: 'cedar' }));
        expect(component.getName()).toBe('cedar');
    });

    it('getInstanceId() returns null for definition row', () => {
        const { component } = renderDefinition(makeExt());
        expect(component.getInstanceId()).toBeNull();
    });

    it('getInstanceId() returns the instanceId for instance row', () => {
        const { component } = render(ExtensionRow, { props: { ext: makeExt(), instanceId: 'test-ext#3' } });
        expect(component.getInstanceId()).toBe('test-ext#3');
    });

    it('validate() returns empty array by default', () => {
        const { component } = renderDefinition(makeExt());
        expect(component.validate()).toEqual([]);
    });

    it('getCurrentConfig() returns empty string by default', () => {
        const { component } = renderDefinition(makeExt());
        expect(component.getCurrentConfig()).toBe('');
    });

    it('markInvalid() adds invalid class', async () => {
        const { component, container } = renderDefinition(makeExt());
        component.markInvalid();
        await waitFor(() => {
            expect(container.querySelector('.ext-row').classList.contains('ext-row-invalid')).toBe(true);
        });
    });

    it('clearErrors() removes invalid class', async () => {
        const { component, container } = renderDefinition(makeExt());
        component.markInvalid();
        await waitFor(() => {
            expect(container.querySelector('.ext-row').classList.contains('ext-row-invalid')).toBe(true);
        });
        component.clearErrors();
        await waitFor(() => {
            expect(container.querySelector('.ext-row').classList.contains('ext-row-invalid')).toBe(false);
        });
    });

    it('getFilterType() returns empty string when no filter type needed', () => {
        const { component } = renderInstance(makeExt({ filterType: ['http'] }));
        expect(component.getFilterType()).toBe('');
    });

    it('validate() returns no filter type error when first filter type is pre-selected', () => {
        const ext = makeExt({ filterType: ['udp_listener', 'network'] });
        const { component } = renderInstance(ext);
        // First filter type is auto-selected, so no filter type validation error
        const errors = component.validate();
        expect(errors.length).toBe(0);
    });
});
