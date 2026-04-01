/*
 * Copyright Built On Envoy
 * SPDX-License-Identifier: Apache-2.0
 * The full text of the Apache license is available in the LICENSE file at
 * the root of the repo.
 */

import { describe, it, expect, beforeEach, vi } from 'vitest';

// Reset module state between tests by re-importing fresh each time.
// Svelte 5 module-level $state is module-scoped, so vi.resetModules()
// gives each test suite a clean slate.
let getExtensions, getSelected, getConfigs, getRunOrder, isRunning;
let selectExtension, deselectExtension, setConfig, reorderRun, setRunning, setExtensions;

const extA = { name: 'ext-a', categories: ['security'] };
const extB = { name: 'ext-b', categories: ['routing'] };
const extC = { name: 'ext-c', categories: ['observability'] };

beforeEach(async () => {
    vi.resetModules();
    const store = await import('../../lib/store.svelte.js');
    getExtensions    = store.getExtensions;
    getSelected      = store.getSelected;
    getConfigs       = store.getConfigs;
    getRunOrder      = store.getRunOrder;
    isRunning        = store.isRunning;
    selectExtension  = store.selectExtension;
    deselectExtension = store.deselectExtension;
    setConfig        = store.setConfig;
    reorderRun       = store.reorderRun;
    setRunning       = store.setRunning;
    setExtensions    = store.setExtensions;
});

describe('selectExtension', () => {
    it('adds extension to selected and runOrder', () => {
        selectExtension(extA);
        expect(getSelected().has('ext-a')).toBe(true);
        expect(getRunOrder()).toEqual(['ext-a']);
    });

    it('is idempotent — selecting twice has no effect', () => {
        selectExtension(extA);
        selectExtension(extA);
        expect(getSelected().size).toBe(1);
        expect(getRunOrder()).toEqual(['ext-a']);
    });

    it('appends to existing order', () => {
        selectExtension(extA);
        selectExtension(extB);
        expect(getRunOrder()).toEqual(['ext-a', 'ext-b']);
    });
});

describe('deselectExtension', () => {
    beforeEach(() => {
        selectExtension(extA);
        selectExtension(extB);
    });

    it('removes from selected and runOrder', () => {
        deselectExtension('ext-a');
        expect(getSelected().has('ext-a')).toBe(false);
        expect(getRunOrder()).toEqual(['ext-b']);
    });

    it('leaves remaining extensions intact', () => {
        deselectExtension('ext-a');
        expect(getSelected().has('ext-b')).toBe(true);
    });

    it('is safe to call for an extension that is not selected', () => {
        expect(() => deselectExtension('unknown')).not.toThrow();
    });
});

describe('setConfig', () => {
    it('sets a config string', () => {
        setConfig('ext-a', '{"key":"val"}');
        expect(getConfigs().get('ext-a')).toBe('{"key":"val"}');
    });

    it('deletes config when given null', () => {
        setConfig('ext-a', '{"key":"val"}');
        setConfig('ext-a', null);
        expect(getConfigs().has('ext-a')).toBe(false);
    });

    it('deletes config when given empty string', () => {
        setConfig('ext-a', '{"key":"val"}');
        setConfig('ext-a', '');
        expect(getConfigs().has('ext-a')).toBe(false);
    });
});

describe('reorderRun', () => {
    beforeEach(() => {
        selectExtension(extA);
        selectExtension(extB);
        selectExtension(extC);
    });

    it('moves item from first to last', () => {
        reorderRun(0, 2);
        expect(getRunOrder()).toEqual(['ext-b', 'ext-c', 'ext-a']);
    });

    it('moves item from last to first', () => {
        reorderRun(2, 0);
        expect(getRunOrder()).toEqual(['ext-c', 'ext-a', 'ext-b']);
    });

    it('moves item from middle to first', () => {
        reorderRun(1, 0);
        expect(getRunOrder()).toEqual(['ext-b', 'ext-a', 'ext-c']);
    });
});

describe('setRunning', () => {
    it('sets running to true', () => {
        setRunning(true);
        expect(isRunning()).toBe(true);
    });

    it('sets running to false', () => {
        setRunning(true);
        setRunning(false);
        expect(isRunning()).toBe(false);
    });
});

describe('setExtensions', () => {
    it('replaces the extensions list', () => {
        const exts = [{ name: 'cedar' }, { name: 'opa' }];
        setExtensions(exts);
        expect(getExtensions()).toEqual(exts);
    });

    it('starts empty before setExtensions is called', () => {
        expect(getExtensions()).toEqual([]);
    });
});
