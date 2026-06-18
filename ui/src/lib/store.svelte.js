/*
 * Copyright Built On Envoy
 * SPDX-License-Identifier: Apache-2.0
 * The full text of the Apache license is available in the LICENSE file at
 * the root of the repo.
 */

// BOE Extension Manager — Application State (Svelte 5)
//
// Single source of truth for all mutable application state.
// Uses Svelte 5 module-level $state runes — reactive in any .svelte
// file or .svelte.js module that imports these getters.
// All mutations go through the exported functions — no direct writes
// from other modules.
//
// Instance model: the same extension can be selected multiple times. Each
// selection creates an instance keyed by `instanceId` (e.g. "foo#1", "foo#2").
// `instanceExtName(instanceId)` returns the real extension name for API payloads.

let extensions  = $state([]);
let selected    = $state(new Map());   // Map<instanceId, ext>
let configs     = $state(new Map());   // Map<instanceId, config string>
let runOrder    = $state([]);          // string[] of instanceIds
let running     = $state(false);

// Non-reactive counter: tracks cumulative instance count per extension name.
// Never decrements — avoids ID reuse when instances are removed and re-added.
const _instanceCounter = new Map();

export const getExtensions = () => extensions;
export const getSelected   = () => selected;
export const getConfigs    = () => configs;
export const getRunOrder   = () => runOrder;
export const isRunning     = () => running;

// Returns the extension name for a given instanceId by stripping the "#N" suffix.
export function instanceExtName(instanceId) {
    const idx = instanceId.lastIndexOf('#');
    return idx === -1 ? instanceId : instanceId.slice(0, idx);
}

// Returns all instanceIds in runOrder that belong to the given extension name.
export function getInstancesOf(name) {
    return runOrder.filter(id => instanceExtName(id) === name);
}

// Returns a user-facing display label for an instanceId.
// Shows "ext-name" for a single instance, "ext-name #N" when multiple instances exist.
export function getDisplayLabel(instanceId) {
    const name = instanceExtName(instanceId);
    const instances = getInstancesOf(name);
    if (instances.length <= 1) return name;
    return `${name} #${parseInt(instanceId.split('#')[1], 10)}`;
}

// Add a new instance of an extension. Always creates a fresh instance — not
// idempotent. Returns the new instanceId.
export function addExtensionInstance(ext) {
    const count = (_instanceCounter.get(ext.name) || 0) + 1;
    _instanceCounter.set(ext.name, count);
    const instanceId = `${ext.name}#${count}`;
    selected = new Map([...selected, [instanceId, ext]]);
    runOrder = [...runOrder, instanceId];
    return instanceId;
}

// Remove all instances of the named extension in a single reactive update.
// Resets the instance counter so re-adding the extension starts from #1.
export function deselectAllInstancesOf(name) {
    const toRemove = new Set(getInstancesOf(name));
    if (toRemove.size === 0) return;
    const nextSelected = new Map(selected);
    const nextConfigs  = new Map(configs);
    for (const id of toRemove) {
        nextSelected.delete(id);
        nextConfigs.delete(id);
    }
    selected = nextSelected;
    configs  = nextConfigs;
    runOrder = runOrder.filter(id => !toRemove.has(id));
    _instanceCounter.delete(name);
}

// Remove an instance by instanceId.
// Resets the instance counter when this was the last instance of that extension.
export function deselectExtension(instanceId) {
    const name = instanceExtName(instanceId);
    const nextSelected = new Map(selected);
    nextSelected.delete(instanceId);
    selected = nextSelected;
    const nextConfigs = new Map(configs);
    nextConfigs.delete(instanceId);
    configs = nextConfigs;
    runOrder = runOrder.filter(id => id !== instanceId);
    if (getInstancesOf(name).length === 0) {
        _instanceCounter.delete(name);
    }
}

// Save or clear a config string for an instance.
export function setConfig(instanceId, cfg) {
    const next = new Map(configs);
    if (cfg) {
        next.set(instanceId, cfg);
    } else {
        next.delete(instanceId);
    }
    configs = next;
}

// Reorder an item in runOrder by moving it from fromIdx to toIdx.
export function reorderRun(fromIdx, toIdx) {
    const arr = [...runOrder];
    const [moved] = arr.splice(fromIdx, 1);
    arr.splice(toIdx, 0, moved);
    runOrder = arr;
}

// Set the running flag (true while a command is executing).
export function setRunning(active) {
    running = active;
}

// Set the full extensions list (called once after API fetch).
export function setExtensions(exts) {
    extensions = exts;
}

// Reset all mutable state to initial values. Intended for test isolation only.
export function clearStore() {
    extensions = [];
    selected  = new Map();
    configs   = new Map();
    runOrder  = [];
    running   = false;
    _instanceCounter.clear();
}
