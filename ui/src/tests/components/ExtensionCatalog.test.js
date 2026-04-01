/*
 * Copyright Built On Envoy
 * SPDX-License-Identifier: Apache-2.0
 * The full text of the Apache license is available in the LICENSE file at
 * the root of the repo.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, fireEvent } from '@testing-library/svelte';
import ExtensionCatalog from '../../components/ExtensionCatalog.svelte';
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
    return { ...actual, getSchema: vi.fn(), postRun: vi.fn(), postStop: vi.fn() };
});

const exts = [
    { name: 'cedar',    description: 'Cedar policy engine', categories: ['Security'], tags: [] },
    { name: 'opa',      description: 'Open Policy Agent',   categories: ['Security'], tags: ['policy'] },
    { name: 'file-srv', description: 'Serve static files',  categories: ['Traffic'],  tags: ['static'] },
    { name: 'chat',     description: 'Chat completions',    categories: ['AI'],       tags: ['llm'] },
];

beforeEach(() => {
    vi.clearAllMocks();
    store.getSelected.mockReturnValue(new Map());
    store.getConfigs.mockReturnValue(new Map());
    store.isRunning.mockReturnValue(false);
    api.getSchema.mockResolvedValue({ ok: false });
});

describe('ExtensionCatalog', () => {
    it('renders all extensions when no filters are applied', () => {
        const { container } = render(ExtensionCatalog, { props: { extensions: exts } });
        const rows = container.querySelectorAll('.ext-row');
        expect(rows.length).toBe(4);
    });

    it('renders extension names', () => {
        const { container } = render(ExtensionCatalog, { props: { extensions: exts } });
        const names = [...container.querySelectorAll('.ext-row-name')].map(el => el.textContent);
        expect(names).toContain('cedar');
        expect(names).toContain('opa');
    });

    it('renders empty state when no extensions provided', () => {
        const { container } = render(ExtensionCatalog, { props: { extensions: [] } });
        expect(container.querySelector('.empty-state')).toBeTruthy();
        expect(container.querySelector('.empty-state-title').textContent).toBe('No extensions found');
    });

    it('renders search input and category select', () => {
        const { container } = render(ExtensionCatalog, { props: { extensions: exts } });
        expect(container.querySelector('.search-input')).toBeTruthy();
        expect(container.querySelector('.category-select')).toBeTruthy();
    });

    it('populates category dropdown from extensions', () => {
        const { container } = render(ExtensionCatalog, { props: { extensions: exts } });
        const options = [...container.querySelectorAll('.category-select option')].map(o => o.value);
        expect(options).toContain('AI');
        expect(options).toContain('Security');
        expect(options).toContain('Traffic');
        // First option is "All categories"
        expect(options[0]).toBe('');
    });

    it('sorts categories alphabetically in dropdown', () => {
        const { container } = render(ExtensionCatalog, { props: { extensions: exts } });
        const options = [...container.querySelectorAll('.category-select option')]
            .map(o => o.value)
            .filter(v => v !== '');
        expect(options).toEqual(['AI', 'Security', 'Traffic']);
    });

    it('filters by name search', async () => {
        const { container } = render(ExtensionCatalog, { props: { extensions: exts } });
        const search = container.querySelector('.search-input');
        await fireEvent.input(search, { target: { value: 'cedar' } });
        const rows = container.querySelectorAll('.ext-row');
        expect(rows.length).toBe(1);
        expect(rows[0].getAttribute('data-ext-name')).toBe('cedar');
    });

    it('filters by description substring', async () => {
        const { container } = render(ExtensionCatalog, { props: { extensions: exts } });
        const search = container.querySelector('.search-input');
        await fireEvent.input(search, { target: { value: 'policy' } });
        const rows = container.querySelectorAll('.ext-row');
        expect(rows.length).toBe(2); // cedar description + opa tag 'policy'
    });

    it('filters by tag', async () => {
        const { container } = render(ExtensionCatalog, { props: { extensions: exts } });
        const search = container.querySelector('.search-input');
        await fireEvent.input(search, { target: { value: 'llm' } });
        const rows = container.querySelectorAll('.ext-row');
        expect(rows.length).toBe(1);
        expect(rows[0].getAttribute('data-ext-name')).toBe('chat');
    });

    it('search is case-insensitive', async () => {
        const { container } = render(ExtensionCatalog, { props: { extensions: exts } });
        const search = container.querySelector('.search-input');
        await fireEvent.input(search, { target: { value: 'CEDAR' } });
        const rows = container.querySelectorAll('.ext-row');
        expect(rows.length).toBe(1);
    });

    it('filters by category', async () => {
        const { container } = render(ExtensionCatalog, { props: { extensions: exts } });
        const select = container.querySelector('.category-select');
        await fireEvent.change(select, { target: { value: 'Security' } });
        const rows = container.querySelectorAll('.ext-row');
        expect(rows.length).toBe(2);
        const names = [...rows].map(r => r.getAttribute('data-ext-name'));
        expect(names).toContain('cedar');
        expect(names).toContain('opa');
    });

    it('combines search and category filter', async () => {
        const { container } = render(ExtensionCatalog, { props: { extensions: exts } });
        const select = container.querySelector('.category-select');
        const search = container.querySelector('.search-input');
        await fireEvent.change(select, { target: { value: 'Security' } });
        await fireEvent.input(search, { target: { value: 'cedar' } });
        const rows = container.querySelectorAll('.ext-row');
        expect(rows.length).toBe(1);
        expect(rows[0].getAttribute('data-ext-name')).toBe('cedar');
    });

    it('shows empty state when search matches nothing', async () => {
        const { container } = render(ExtensionCatalog, { props: { extensions: exts } });
        const search = container.querySelector('.search-input');
        await fireEvent.input(search, { target: { value: 'zzznomatch' } });
        expect(container.querySelector('.empty-state')).toBeTruthy();
        expect(container.querySelector('.ext-row')).toBeNull();
    });

    it('shows empty state when combined filters match nothing', async () => {
        const { container } = render(ExtensionCatalog, { props: { extensions: exts } });
        const select = container.querySelector('.category-select');
        const search = container.querySelector('.search-input');
        // Security category + search that matches nothing in Security
        await fireEvent.change(select, { target: { value: 'Security' } });
        await fireEvent.input(search, { target: { value: 'zzznomatch' } });
        expect(container.querySelector('.empty-state')).toBeTruthy();
    });

    it('restores all results when search is cleared', async () => {
        const { container } = render(ExtensionCatalog, { props: { extensions: exts } });
        const search = container.querySelector('.search-input');
        await fireEvent.input(search, { target: { value: 'cedar' } });
        expect(container.querySelectorAll('.ext-row').length).toBe(1);
        await fireEvent.input(search, { target: { value: '' } });
        expect(container.querySelectorAll('.ext-row').length).toBe(4);
    });

});
