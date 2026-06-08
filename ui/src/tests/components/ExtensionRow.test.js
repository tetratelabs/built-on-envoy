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
        selectExtension: vi.fn(),
        deselectExtension: vi.fn(),
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
    ...overrides,
});

beforeEach(() => {
    vi.clearAllMocks();
    store.getSelected.mockReturnValue(new Map());
    store.getConfigs.mockReturnValue(new Map());
    store.isRunning.mockReturnValue(false);
    api.getSchema.mockResolvedValue({ ok: false });
});

describe('ExtensionRow', () => {
    it('renders extension name and description', () => {
        const ext = makeExt();
        const { container } = render(ExtensionRow, { props: { ext } });
        expect(container.querySelector('.ext-row-name').textContent).toBe('test-ext');
        expect(container.querySelector('.ext-row-desc').textContent).toBe('A test extension');
    });

    it('renders categories as badges', () => {
        const ext = makeExt({ categories: ['Security', 'Auth'] });
        const { container } = render(ExtensionRow, { props: { ext } });
        const badges = container.querySelectorAll('.ext-row-categories span');
        expect(badges.length).toBe(2);
        expect(badges[0].textContent).toBe('Security');
        expect(badges[1].textContent).toBe('Auth');
    });

    it('renders unchecked toggle when extension is not selected', () => {
        store.getSelected.mockReturnValue(new Map());
        const ext = makeExt();
        const { container } = render(ExtensionRow, { props: { ext } });
        const checkbox = container.querySelector('input[type="checkbox"]');
        expect(checkbox.checked).toBe(false);
    });

    it('renders checked toggle when extension is selected', () => {
        store.getSelected.mockReturnValue(new Map([['test-ext', makeExt()]]));
        const ext = makeExt();
        const { container } = render(ExtensionRow, { props: { ext } });
        const checkbox = container.querySelector('input[type="checkbox"]');
        expect(checkbox.checked).toBe(true);
    });

    it('does not show body when extension is not selected', () => {
        store.getSelected.mockReturnValue(new Map());
        const ext = makeExt();
        const { container } = render(ExtensionRow, { props: { ext } });
        expect(container.querySelector('.ext-row-body')).toBeNull();
    });

    it('shows body when extension is selected', () => {
        store.getSelected.mockReturnValue(new Map([['test-ext', makeExt()]]));
        const ext = makeExt();
        const { container } = render(ExtensionRow, { props: { ext } });
        expect(container.querySelector('.ext-row-body')).toBeTruthy();
    });

    it('calls selectExtension when toggle is turned on', async () => {
        store.getSelected.mockReturnValue(new Map());
        const ext = makeExt();
        const { container } = render(ExtensionRow, { props: { ext } });
        const checkbox = container.querySelector('input[type="checkbox"]');
        await fireEvent.change(checkbox, { target: { checked: true } });
        expect(store.selectExtension).toHaveBeenCalledWith(ext);
    });

    it('calls deselectExtension when toggle is turned off', async () => {
        store.getSelected.mockReturnValue(new Map([['test-ext', makeExt()]]));
        const ext = makeExt();
        const { container } = render(ExtensionRow, { props: { ext } });
        const checkbox = container.querySelector('input[type="checkbox"]');
        await fireEvent.change(checkbox, { target: { checked: false } });
        expect(store.deselectExtension).toHaveBeenCalledWith('test-ext');
    });

    it('fetches schema when extension is toggled on', async () => {
        store.getSelected.mockReturnValue(new Map());
        const ext = makeExt();
        const { container } = render(ExtensionRow, { props: { ext } });
        const checkbox = container.querySelector('input[type="checkbox"]');
        await fireEvent.change(checkbox, { target: { checked: true } });
        await waitFor(() => {
            expect(api.getSchema).toHaveBeenCalledWith('test-ext');
        });
    });

    it('does not fetch schema again on second toggle', async () => {
        store.getSelected.mockReturnValue(new Map());
        const ext = makeExt();
        const { container } = render(ExtensionRow, { props: { ext } });
        const checkbox = container.querySelector('input[type="checkbox"]');

        // Toggle on
        await fireEvent.change(checkbox, { target: { checked: true } });
        await waitFor(() => expect(api.getSchema).toHaveBeenCalledTimes(1));

        // Toggle off
        store.getSelected.mockReturnValue(new Map([['test-ext', ext]]));
        await fireEvent.change(checkbox, { target: { checked: false } });

        // Toggle on again
        store.getSelected.mockReturnValue(new Map());
        await fireEvent.change(checkbox, { target: { checked: true } });

        // Should still only be called once
        expect(api.getSchema).toHaveBeenCalledTimes(1);
    });

    it('shows Run Extension button when selected', () => {
        store.getSelected.mockReturnValue(new Map([['test-ext', makeExt()]]));
        const ext = makeExt();
        const { container } = render(ExtensionRow, { props: { ext } });
        const runBtn = container.querySelector('.ext-run-btn');
        expect(runBtn).toBeTruthy();
        expect(runBtn.textContent).toContain('Run Extension');
    });

    it('disables Run button when isRunning is true', () => {
        store.getSelected.mockReturnValue(new Map([['test-ext', makeExt()]]));
        store.isRunning.mockReturnValue(true);
        const ext = makeExt();
        const { container } = render(ExtensionRow, { props: { ext } });
        const runBtn = container.querySelector('.ext-run-btn');
        expect(runBtn.disabled).toBe(true);
    });

    it('enables Run button when isRunning is false', () => {
        store.getSelected.mockReturnValue(new Map([['test-ext', makeExt()]]));
        store.isRunning.mockReturnValue(false);
        const ext = makeExt();
        const { container } = render(ExtensionRow, { props: { ext } });
        const runBtn = container.querySelector('.ext-run-btn');
        expect(runBtn.disabled).toBe(false);
    });

    it('has active class when extension is selected', () => {
        store.getSelected.mockReturnValue(new Map([['test-ext', makeExt()]]));
        const ext = makeExt();
        const { container } = render(ExtensionRow, { props: { ext } });
        expect(container.querySelector('.ext-row').classList.contains('active')).toBe(true);
    });

    it('does not have active class when extension is not selected', () => {
        store.getSelected.mockReturnValue(new Map());
        const ext = makeExt();
        const { container } = render(ExtensionRow, { props: { ext } });
        expect(container.querySelector('.ext-row').classList.contains('active')).toBe(false);
    });

    it('extension name link points to builtonenvoy.io', () => {
        const ext = makeExt({ name: 'my ext' });
        const { container } = render(ExtensionRow, { props: { ext } });
        const link = container.querySelector('.ext-row-name');
        expect(link.getAttribute('href')).toContain('builtonenvoy.io');
        expect(link.getAttribute('href')).toContain(encodeURIComponent('my ext'));
    });

    it('getName() returns the extension name', () => {
        const ext = makeExt({ name: 'cedar' });
        const { component } = render(ExtensionRow, { props: { ext } });
        expect(component.getName()).toBe('cedar');
    });

    it('validate() returns empty array by default', () => {
        const ext = makeExt();
        const { component } = render(ExtensionRow, { props: { ext } });
        expect(component.validate()).toEqual([]);
    });

    it('getCurrentConfig() returns empty string by default', () => {
        const ext = makeExt();
        const { component } = render(ExtensionRow, { props: { ext } });
        expect(component.getCurrentConfig()).toBe('');
    });

    it('markInvalid() adds invalid class', async () => {
        const ext = makeExt();
        const { component, container } = render(ExtensionRow, { props: { ext } });
        component.markInvalid();
        await waitFor(() => {
            expect(container.querySelector('.ext-row').classList.contains('ext-row-invalid')).toBe(true);
        });
    });

    it('clearErrors() removes invalid class', async () => {
        const ext = makeExt();
        const { component, container } = render(ExtensionRow, { props: { ext } });
        component.markInvalid();
        await waitFor(() => {
            expect(container.querySelector('.ext-row').classList.contains('ext-row-invalid')).toBe(true);
        });
        component.clearErrors();
        await waitFor(() => {
            expect(container.querySelector('.ext-row').classList.contains('ext-row-invalid')).toBe(false);
        });
    });

    it('run button click shows inline panel', async () => {
        // Use real store so reactivity works — extension starts selected
        const { selectExtension: realSelect, deselectExtension: realDeselect,
                getSelected: realGetSelected, getConfigs: realGetConfigs,
                isRunning: realIsRunning } = await vi.importActual('../../lib/store.svelte.js');

        store.getSelected.mockImplementation(realGetSelected);
        store.getConfigs.mockImplementation(realGetConfigs);
        store.isRunning.mockImplementation(realIsRunning);
        store.selectExtension.mockImplementation(realSelect);
        store.deselectExtension.mockImplementation(realDeselect);

        api.getSchema.mockResolvedValue({ ok: false });
        api.postRun.mockResolvedValue({ ok: true, text: async () => '', body: null });

        const ext = makeExt();
        const { container } = render(ExtensionRow, { props: { ext } });

        // Toggle on — real store will update, $derived reacts
        const checkbox = container.querySelector('input[type="checkbox"]');
        await fireEvent.change(checkbox, { target: { checked: true } });

        await waitFor(() => expect(api.getSchema).toHaveBeenCalled());
        await waitFor(() => expect(container.querySelector('.ext-run-btn')).toBeTruthy());

        const runBtn = container.querySelector('.ext-run-btn');
        await fireEvent.click(runBtn);

        await waitFor(() => {
            expect(container.querySelector('.ext-inline-panel')).toBeTruthy();
        });
    });

    it('calls onRunAllPanelRequest on popout', async () => {
        const { selectExtension: realSelect, deselectExtension: realDeselect,
                getSelected: realGetSelected, getConfigs: realGetConfigs,
                isRunning: realIsRunning } = await vi.importActual('../../lib/store.svelte.js');

        store.getSelected.mockImplementation(realGetSelected);
        store.getConfigs.mockImplementation(realGetConfigs);
        store.isRunning.mockImplementation(realIsRunning);
        store.selectExtension.mockImplementation(realSelect);
        store.deselectExtension.mockImplementation(realDeselect);

        api.getSchema.mockResolvedValue({ ok: false });
        api.postRun.mockResolvedValue({ ok: true, text: async () => '', body: null });

        const onRunAllPanelRequest = vi.fn();
        const ext = makeExt();
        const { container } = render(ExtensionRow, {
            props: { ext, onRunAllPanelRequest },
        });

        // Toggle on
        const checkbox = container.querySelector('input[type="checkbox"]');
        await fireEvent.change(checkbox, { target: { checked: true } });

        await waitFor(() => expect(api.getSchema).toHaveBeenCalled());
        await waitFor(() => expect(container.querySelector('.ext-run-btn')).toBeTruthy());

        // Click run to open panel
        const runBtn = container.querySelector('.ext-run-btn');
        await fireEvent.click(runBtn);

        await waitFor(() => {
            expect(container.querySelector('.ext-inline-panel')).toBeTruthy();
        });

        // Click popout
        const popoutBtn = container.querySelector('.ext-inline-popout');
        expect(popoutBtn).toBeTruthy();
        await fireEvent.click(popoutBtn);
        expect(onRunAllPanelRequest).toHaveBeenCalledWith(
            expect.objectContaining({ title: 'Run: test-ext' })
        );
    });

    it('clicking header when active collapses the row body', async () => {
        const { selectExtension: realSelect, getSelected: realGetSelected,
                getConfigs: realGetConfigs, isRunning: realIsRunning } =
            await vi.importActual('../../lib/store.svelte.js');

        store.getSelected.mockImplementation(realGetSelected);
        store.getConfigs.mockImplementation(realGetConfigs);
        store.isRunning.mockImplementation(realIsRunning);
        store.selectExtension.mockImplementation(realSelect);

        api.getSchema.mockResolvedValue({ ok: false });

        const ext = makeExt();
        const { container } = render(ExtensionRow, { props: { ext } });

        // Toggle on to make the row active
        await fireEvent.change(container.querySelector('input[type="checkbox"]'), { target: { checked: true } });
        await waitFor(() => expect(container.querySelector('.ext-row.active')).toBeTruthy());

        // Click the header (not the toggle or a link)
        const header = container.querySelector('.ext-row-header');
        await fireEvent.click(header);

        await waitFor(() => {
            expect(container.querySelector('.ext-row').classList.contains('collapsed')).toBe(true);
        });
    });

    it('schema load failure (getSchema throws) sets schema to null (no-schema message shown)', async () => {
        const { selectExtension: realSelect, getSelected: realGetSelected,
                getConfigs: realGetConfigs, isRunning: realIsRunning } =
            await vi.importActual('../../lib/store.svelte.js');

        store.getSelected.mockImplementation(realGetSelected);
        store.getConfigs.mockImplementation(realGetConfigs);
        store.isRunning.mockImplementation(realIsRunning);
        store.selectExtension.mockImplementation(realSelect);

        // Make getSchema throw a network error
        api.getSchema.mockRejectedValue(new Error('network failure'));

        const ext = makeExt();
        const { container } = render(ExtensionRow, { props: { ext } });
        await fireEvent.change(container.querySelector('input[type="checkbox"]'), { target: { checked: true } });

        await waitFor(() => {
            expect(container.querySelector('.no-schema-msg')).toBeTruthy();
        });
    });

    it('individual run shows error line in panel when postRun returns non-ok', async () => {
        const { selectExtension: realSelect, deselectExtension: realDeselect,
                getSelected: realGetSelected, getConfigs: realGetConfigs,
                isRunning: realIsRunning } = await vi.importActual('../../lib/store.svelte.js');

        store.getSelected.mockImplementation(realGetSelected);
        store.getConfigs.mockImplementation(realGetConfigs);
        store.isRunning.mockImplementation(realIsRunning);
        store.selectExtension.mockImplementation(realSelect);
        store.deselectExtension.mockImplementation(realDeselect);

        api.getSchema.mockResolvedValue({ ok: false });
        api.postRun.mockResolvedValue({ ok: false, text: async () => 'Server error' });

        const ext = makeExt();
        const { container } = render(ExtensionRow, { props: { ext } });

        await fireEvent.change(container.querySelector('input[type="checkbox"]'), { target: { checked: true } });
        await waitFor(() => expect(container.querySelector('.ext-run-btn')).toBeTruthy());
        await fireEvent.click(container.querySelector('.ext-run-btn'));

        await waitFor(() => {
            const errorLines = container.querySelectorAll('.terminal-error');
            expect(errorLines.length).toBeGreaterThan(0);
            expect(errorLines[0].textContent).toBe('Server error');
        });
    });

    it('close button hides the inline panel', async () => {
        const { selectExtension: realSelect, deselectExtension: realDeselect,
                getSelected: realGetSelected, getConfigs: realGetConfigs,
                isRunning: realIsRunning } = await vi.importActual('../../lib/store.svelte.js');

        store.getSelected.mockImplementation(realGetSelected);
        store.getConfigs.mockImplementation(realGetConfigs);
        store.isRunning.mockImplementation(realIsRunning);
        store.selectExtension.mockImplementation(realSelect);
        store.deselectExtension.mockImplementation(realDeselect);

        api.getSchema.mockResolvedValue({ ok: false });
        api.postRun.mockResolvedValue({ ok: true, text: async () => '', body: null });

        const ext = makeExt();
        const { container } = render(ExtensionRow, { props: { ext } });

        const checkbox = container.querySelector('input[type="checkbox"]');
        await fireEvent.change(checkbox, { target: { checked: true } });
        await waitFor(() => expect(container.querySelector('.ext-run-btn')).toBeTruthy());
        await fireEvent.click(container.querySelector('.ext-run-btn'));
        await waitFor(() => expect(container.querySelector('.ext-inline-panel')).toBeTruthy());

        // Click close
        const closeBtn = container.querySelector('.btn-secondary');
        expect(closeBtn).toBeTruthy();
        await fireEvent.click(closeBtn);
        await waitFor(() => {
            expect(container.querySelector('.ext-inline-panel')).toBeNull();
        });
    });

    it('stop button calls postStop', async () => {
        const { selectExtension: realSelect, deselectExtension: realDeselect,
                getSelected: realGetSelected, getConfigs: realGetConfigs,
                isRunning: realIsRunning } = await vi.importActual('../../lib/store.svelte.js');

        store.getSelected.mockImplementation(realGetSelected);
        store.getConfigs.mockImplementation(realGetConfigs);
        store.isRunning.mockImplementation(realIsRunning);
        store.selectExtension.mockImplementation(realSelect);
        store.deselectExtension.mockImplementation(realDeselect);

        api.getSchema.mockResolvedValue({ ok: false });

        // postRun returns a stream that never completes so the panel stays in running state
        let closeStream;
        const neverEndingBody = new ReadableStream({
            start(controller) { closeStream = () => controller.close(); },
        });
        api.postRun.mockResolvedValue({ ok: true, body: neverEndingBody });
        api.postStop.mockResolvedValue({ ok: true });

        const ext = makeExt();
        const { container } = render(ExtensionRow, { props: { ext } });

        const checkbox = container.querySelector('input[type="checkbox"]');
        await fireEvent.change(checkbox, { target: { checked: true } });
        await waitFor(() => expect(container.querySelector('.ext-run-btn')).toBeTruthy());
        await fireEvent.click(container.querySelector('.ext-run-btn'));

        await waitFor(() => expect(container.querySelector('.btn-danger')).toBeTruthy());
        await fireEvent.click(container.querySelector('.btn-danger'));
        expect(api.postStop).toHaveBeenCalled();
        closeStream?.();
    });

    it('shows run error message when individual run fails validation', async () => {
        const { selectExtension: realSelect, deselectExtension: realDeselect,
                getSelected: realGetSelected, getConfigs: realGetConfigs,
                isRunning: realIsRunning } = await vi.importActual('../../lib/store.svelte.js');

        store.getSelected.mockImplementation(realGetSelected);
        store.getConfigs.mockImplementation(realGetConfigs);
        store.isRunning.mockImplementation(realIsRunning);
        store.selectExtension.mockImplementation(realSelect);
        store.deselectExtension.mockImplementation(realDeselect);

        // Schema with a required field — JSONEditor.getValue() returns {} so it fails
        const schema = { type: 'object', required: ['endpoint'], properties: { endpoint: { type: 'string' } } };
        api.getSchema.mockResolvedValue({ ok: true, json: async () => schema });

        const ext = makeExt();
        const { container } = render(ExtensionRow, { props: { ext } });

        const checkbox = container.querySelector('input[type="checkbox"]');
        await fireEvent.change(checkbox, { target: { checked: true } });
        await waitFor(() => expect(container.querySelector('.ext-run-btn')).toBeTruthy());

        await fireEvent.click(container.querySelector('.ext-run-btn'));

        await waitFor(() => {
            const errMsg = container.querySelector('.ext-run-error');
            expect(errMsg).toBeTruthy();
            expect(errMsg.textContent).toMatch(/validation error/);
        });
        expect(api.postRun).not.toHaveBeenCalled();
    });

    it('calls onClearRunAllError when toggle is turned on', async () => {
        store.getSelected.mockReturnValue(new Map());
        const onClearRunAllError = vi.fn();
        const ext = makeExt();
        const { container } = render(ExtensionRow, { props: { ext, onClearRunAllError } });
        await fireEvent.change(container.querySelector('input[type="checkbox"]'), { target: { checked: true } });
        expect(onClearRunAllError).toHaveBeenCalled();
    });

    it('calls onClearRunAllError when toggle is turned off', async () => {
        store.getSelected.mockReturnValue(new Map([['test-ext', makeExt()]]));
        const onClearRunAllError = vi.fn();
        const ext = makeExt();
        const { container } = render(ExtensionRow, { props: { ext, onClearRunAllError } });
        await fireEvent.change(container.querySelector('input[type="checkbox"]'), { target: { checked: false } });
        expect(onClearRunAllError).toHaveBeenCalled();
    });

    it('calls onClearRunAllError when individual Run button is clicked', async () => {
        const { selectExtension: realSelect, deselectExtension: realDeselect,
                getSelected: realGetSelected, getConfigs: realGetConfigs,
                isRunning: realIsRunning } = await vi.importActual('../../lib/store.svelte.js');

        store.getSelected.mockImplementation(realGetSelected);
        store.getConfigs.mockImplementation(realGetConfigs);
        store.isRunning.mockImplementation(realIsRunning);
        store.selectExtension.mockImplementation(realSelect);
        store.deselectExtension.mockImplementation(realDeselect);

        api.getSchema.mockResolvedValue({ ok: false });
        api.postRun.mockResolvedValue({ ok: true, text: async () => '', body: null });

        const onClearRunAllError = vi.fn();
        const ext = makeExt();
        const { container } = render(ExtensionRow, { props: { ext, onClearRunAllError } });

        await fireEvent.change(container.querySelector('input[type="checkbox"]'), { target: { checked: true } });
        await waitFor(() => expect(container.querySelector('.ext-run-btn')).toBeTruthy());

        await fireEvent.click(container.querySelector('.ext-run-btn'));
        expect(onClearRunAllError).toHaveBeenCalled();
    });

    it('popout passes setSharedAppend callback that mirrors lines to the shared terminal', async () => {
        const { selectExtension: realSelect, deselectExtension: realDeselect,
                getSelected: realGetSelected, getConfigs: realGetConfigs,
                isRunning: realIsRunning } = await vi.importActual('../../lib/store.svelte.js');

        store.getSelected.mockImplementation(realGetSelected);
        store.getConfigs.mockImplementation(realGetConfigs);
        store.isRunning.mockImplementation(realIsRunning);
        store.selectExtension.mockImplementation(realSelect);
        store.deselectExtension.mockImplementation(realDeselect);

        api.getSchema.mockResolvedValue({ ok: false });

        // postRun returns a stream that holds until we resolve it, so popout can happen first
        let resolveStream;
        const streamReady = new Promise(r => { resolveStream = r; });
        const encoder = new TextEncoder();
        const body = new ReadableStream({
            async start(controller) {
                await streamReady;
                controller.enqueue(encoder.encode('data: hello from stream\n\n'));
                controller.close();
            },
        });
        api.postRun.mockResolvedValue({ ok: true, body });

        const onRunAllPanelRequest = vi.fn();
        const ext = makeExt();
        const { container } = render(ExtensionRow, { props: { ext, onRunAllPanelRequest } });

        // Toggle on and run
        const checkbox = container.querySelector('input[type="checkbox"]');
        await fireEvent.change(checkbox, { target: { checked: true } });
        await waitFor(() => expect(container.querySelector('.ext-run-btn')).toBeTruthy());
        await fireEvent.click(container.querySelector('.ext-run-btn'));
        await waitFor(() => expect(container.querySelector('.ext-inline-panel')).toBeTruthy());

        // Popout before stream resolves — capture setSharedAppend
        await fireEvent.click(container.querySelector('.ext-inline-popout'));
        expect(onRunAllPanelRequest).toHaveBeenCalled();
        const { setSharedAppend } = onRunAllPanelRequest.mock.calls[0][0];
        expect(typeof setSharedAppend).toBe('function');

        // Wire a spy as the shared terminal's appendLine
        const sharedAppend = vi.fn();
        setSharedAppend(sharedAppend);

        // Now let the stream emit — the run loop should forward the line
        resolveStream();

        await waitFor(() => {
            expect(sharedAppend).toHaveBeenCalledWith('hello from stream', '');
        });
    });
});
