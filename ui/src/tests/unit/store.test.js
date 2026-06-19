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
let addExtensionInstance, deselectExtension, deselectAllInstancesOf, setConfig, reorderRun, setRunning, setExtensions;
let instanceExtName, getInstancesOf, getDisplayLabel;

const extA = { name: 'ext-a', categories: ['security'] };
const extB = { name: 'ext-b', categories: ['routing'] };
const extC = { name: 'ext-c', categories: ['observability'] };

beforeEach(async () => {
    vi.resetModules();
    const store = await import('../../lib/store.svelte.js');
    getExtensions       = store.getExtensions;
    getSelected         = store.getSelected;
    getConfigs          = store.getConfigs;
    getRunOrder         = store.getRunOrder;
    isRunning           = store.isRunning;
    addExtensionInstance     = store.addExtensionInstance;
    deselectExtension        = store.deselectExtension;
    deselectAllInstancesOf   = store.deselectAllInstancesOf;
    setConfig           = store.setConfig;
    reorderRun          = store.reorderRun;
    setRunning          = store.setRunning;
    setExtensions       = store.setExtensions;
    instanceExtName     = store.instanceExtName;
    getInstancesOf      = store.getInstancesOf;
    getDisplayLabel     = store.getDisplayLabel;
});

describe('addExtensionInstance', () => {
    it('adds an instance to selected and runOrder', () => {
        const id = addExtensionInstance(extA);
        expect(getSelected().has(id)).toBe(true);
        expect(getRunOrder()).toEqual([id]);
    });

    it('returns a unique instanceId containing the extension name', () => {
        const id = addExtensionInstance(extA);
        expect(instanceExtName(id)).toBe('ext-a');
    });

    it('creates a second instance when called twice — not idempotent', () => {
        const id1 = addExtensionInstance(extA);
        const id2 = addExtensionInstance(extA);
        expect(id1).not.toBe(id2);
        expect(getSelected().size).toBe(2);
        expect(getRunOrder()).toHaveLength(2);
    });

    it('instances from different extensions are independent', () => {
        const idA = addExtensionInstance(extA);
        const idB = addExtensionInstance(extB);
        expect(getRunOrder()).toEqual([idA, idB]);
        expect(getSelected().size).toBe(2);
    });
});

describe('deselectExtension', () => {
    it('removes the specified instance from selected and runOrder', () => {
        const id1 = addExtensionInstance(extA);
        const id2 = addExtensionInstance(extA);
        deselectExtension(id1);
        expect(getSelected().has(id1)).toBe(false);
        expect(getSelected().has(id2)).toBe(true);
        expect(getRunOrder()).toEqual([id2]);
    });

    it('leaves other extensions intact', () => {
        const idA = addExtensionInstance(extA);
        const idB = addExtensionInstance(extB);
        deselectExtension(idA);
        expect(getSelected().has(idB)).toBe(true);
    });

    it('is safe to call for an instance that is not selected', () => {
        expect(() => deselectExtension('unknown#1')).not.toThrow();
    });

    it('resets counter when the last instance is removed', () => {
        const id1 = addExtensionInstance(extA);  // ext-a#1
        const id2 = addExtensionInstance(extA);  // ext-a#2

        // Remove first: id2 still exists — counter must NOT reset
        deselectExtension(id1);
        const id3 = addExtensionInstance(extA);  // ext-a#3 (counter was at 2, now 3)
        expect(id3).toBe('ext-a#3');

        // Remove all remaining: counter resets
        deselectExtension(id2);
        deselectExtension(id3);
        expect(addExtensionInstance(extA)).toBe('ext-a#1');
    });
});

describe('instanceExtName', () => {
    it('strips the #N suffix', () => {
        expect(instanceExtName('ext-a#1')).toBe('ext-a');
        expect(instanceExtName('ext-a#42')).toBe('ext-a');
    });

    it('returns the original string if no suffix', () => {
        expect(instanceExtName('ext-a')).toBe('ext-a');
    });

    it('handles extension names with hyphens and numbers', () => {
        expect(instanceExtName('my-ext-2#3')).toBe('my-ext-2');
    });
});

describe('getInstancesOf', () => {
    it('returns instanceIds for the named extension', () => {
        const id1 = addExtensionInstance(extA);
        const id2 = addExtensionInstance(extA);
        addExtensionInstance(extB);
        expect(getInstancesOf('ext-a')).toEqual([id1, id2]);
    });

    it('returns empty array when no instances exist', () => {
        expect(getInstancesOf('ext-a')).toEqual([]);
    });

    it('preserves runOrder order', () => {
        const idA1 = addExtensionInstance(extA);
        const idB  = addExtensionInstance(extB);
        const idA2 = addExtensionInstance(extA);
        // idA1 comes before idA2 in runOrder
        expect(getInstancesOf('ext-a')).toEqual([idA1, idA2]);
        expect(getInstancesOf('ext-b')).toEqual([idB]);
    });
});

describe('deselectAllInstancesOf', () => {
    it('removes all instances of the named extension', () => {
        const id1 = addExtensionInstance(extA);
        const id2 = addExtensionInstance(extA);
        deselectAllInstancesOf('ext-a');
        expect(getSelected().has(id1)).toBe(false);
        expect(getSelected().has(id2)).toBe(false);
        expect(getRunOrder()).toEqual([]);
    });

    it('leaves instances of other extensions intact', () => {
        addExtensionInstance(extA);
        addExtensionInstance(extA);
        const idB = addExtensionInstance(extB);
        deselectAllInstancesOf('ext-a');
        expect(getSelected().has(idB)).toBe(true);
        expect(getRunOrder()).toEqual([idB]);
    });

    it('is a no-op when no instances of the named extension exist', () => {
        const idA = addExtensionInstance(extA);
        deselectAllInstancesOf('ext-b');
        expect(getSelected().has(idA)).toBe(true);
        expect(getRunOrder()).toEqual([idA]);
    });

    it('clears both selected and runOrder in one update', () => {
        addExtensionInstance(extA);
        addExtensionInstance(extA);
        deselectAllInstancesOf('ext-a');
        expect(getSelected().size).toBe(0);
        expect(getRunOrder()).toHaveLength(0);
    });

    it('resets the counter so re-adding the extension starts from #1', () => {
        addExtensionInstance(extA);
        addExtensionInstance(extA);
        deselectAllInstancesOf('ext-a');
        const id = addExtensionInstance(extA);
        expect(id).toBe('ext-a#1');
    });
});

describe('getDisplayLabel', () => {
    it('returns the extension name when there is only one instance', () => {
        const id = addExtensionInstance(extA);
        expect(getDisplayLabel(id)).toBe('ext-a');
    });

    it('returns "name #N" when multiple instances exist', () => {
        const id1 = addExtensionInstance(extA);
        const id2 = addExtensionInstance(extA);
        expect(getDisplayLabel(id1)).toMatch(/^ext-a #\d+$/);
        expect(getDisplayLabel(id2)).toMatch(/^ext-a #\d+$/);
        expect(getDisplayLabel(id1)).not.toBe(getDisplayLabel(id2));
    });
});

describe('setConfig', () => {
    it('sets a config string for an instanceId', () => {
        const id = addExtensionInstance(extA);
        setConfig(id, '{"key":"val"}');
        expect(getConfigs().get(id)).toBe('{"key":"val"}');
    });

    it('deletes config when given null', () => {
        const id = addExtensionInstance(extA);
        setConfig(id, '{"key":"val"}');
        setConfig(id, null);
        expect(getConfigs().has(id)).toBe(false);
    });

    it('deletes config when given empty string', () => {
        const id = addExtensionInstance(extA);
        setConfig(id, '{"key":"val"}');
        setConfig(id, '');
        expect(getConfigs().has(id)).toBe(false);
    });

    it('keeps configs for different instances separate', () => {
        const id1 = addExtensionInstance(extA);
        const id2 = addExtensionInstance(extA);
        setConfig(id1, '{"x":1}');
        setConfig(id2, '{"x":2}');
        expect(getConfigs().get(id1)).toBe('{"x":1}');
        expect(getConfigs().get(id2)).toBe('{"x":2}');
    });
});

describe('reorderRun', () => {
    let ids;
    beforeEach(() => {
        ids = [
            addExtensionInstance(extA),
            addExtensionInstance(extB),
            addExtensionInstance(extC),
        ];
    });

    it('moves item from first to last', () => {
        reorderRun(0, 2);
        expect(getRunOrder()).toEqual([ids[1], ids[2], ids[0]]);
    });

    it('moves item from last to first', () => {
        reorderRun(2, 0);
        expect(getRunOrder()).toEqual([ids[2], ids[0], ids[1]]);
    });

    it('moves item from middle to first', () => {
        reorderRun(1, 0);
        expect(getRunOrder()).toEqual([ids[1], ids[0], ids[2]]);
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
