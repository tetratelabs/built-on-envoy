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

let extensions  = $state([]);
let selected    = $state(new Map());
let configs     = $state(new Map());
let runOrder    = $state([]);
let running     = $state(false);

export const getExtensions = () => extensions;
export const getSelected   = () => selected;
export const getConfigs    = () => configs;
export const getRunOrder   = () => runOrder;
export const isRunning     = () => running;

// Select an extension: adds it to selected map and runOrder.
// Idempotent: calling twice with the same extension has no effect.
export function selectExtension(ext) {
    if (selected.has(ext.name)) return;
    selected = new Map([...selected, [ext.name, ext]]);
    runOrder = [...runOrder, ext.name];
}

// Deselect an extension: removes from selected map and runOrder.
export function deselectExtension(name) {
    const next = new Map(selected);
    next.delete(name);
    selected = next;
    runOrder = runOrder.filter(n => n !== name);
}

// Save or clear a config string for an extension.
export function setConfig(name, cfg) {
    const next = new Map(configs);
    if (cfg) {
        next.set(name, cfg);
    } else {
        next.delete(name);
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
