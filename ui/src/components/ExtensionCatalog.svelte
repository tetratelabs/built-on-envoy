<!--
  ~ Copyright Built On Envoy
  ~ SPDX-License-Identifier: Apache-2.0
  ~ The full text of the Apache license is available in the LICENSE file at
  ~ the root of the repo.
-->

<script>
    // ExtensionCatalog.svelte — filter bar + scrollable extension list
    //
    // Accepts the full extensions array as a prop; owns search/category filter
    // state and derives the filtered list reactively.
    //
    // For each extension the catalog renders:
    //   1. The main row (always first; toggle activates/deactivates, "+" adds instances).
    //   2. Any active instance rows directly below (each has a "×" remove button).

    import ExtensionRow from './ExtensionRow.svelte';
    import { getInstancesOf } from '../lib/store.svelte.js';

    // Props
    let { extensions = [], onRunAllPanelRequest, onClearRunAllError } = $props();

    // Filter state
    let search   = $state('');
    let category = $state('');

    // All unique categories across all extensions (sorted)
    let allCategories = $derived(
        [...new Set(extensions.flatMap(e => e.categories))].sort()
    );

    // Filtered list — deduplicated by name so duplicate server entries never
    // produce each_key_duplicate errors (local extensions override catalog ones).
    let filtered = $derived(() => {
        const q = search.toLowerCase();
        const seen = new Set();
        return extensions.filter(ext => {
            if (seen.has(ext.name)) return false;
            seen.add(ext.name);
            if (q && !ext.name.toLowerCase().includes(q) &&
                     !ext.description.toLowerCase().includes(q) &&
                     !ext.tags.some(t => t.toLowerCase().includes(q))) {
                return false;
            }
            if (category && !ext.categories.includes(category)) return false;
            return true;
        });
    });

    // Public API — row refs keyed by instanceId (for instance rows) or ext.name
    // (for the definition row). App.svelte looks up instance rows by instanceId.
    let rowRefs = $state({});

    export function getRowRef(key) { return rowRefs[key]; }
</script>

<div class="catalog">
    <!-- Filter bar -->
    <div class="filter-bar">
        <input
            class="search-input"
            type="search"
            placeholder="Search extensions…"
            bind:value={search}
        />
        <select class="category-select" bind:value={category}>
            <option value="">All categories</option>
            {#each allCategories as cat (cat)}
                <option value={cat}>{cat}</option>
            {/each}
        </select>
    </div>

    <!-- Extension list -->
    <div class="ext-list">
        {#if filtered().length === 0}
            <div class="empty-state">
                <div class="empty-state-title">No extensions found</div>
                <div class="empty-state-desc">Try adjusting your search or filters.</div>
            </div>
        {:else}
            {#each filtered() as ext (ext.name)}
                <!-- Group: main row + any active instance rows -->
                <div class="ext-group" class:ext-group-active={getInstancesOf(ext.name).length > 0}>
                    <!-- Main row: always first; owns the enable/disable toggle and "+" button -->
                    <ExtensionRow
                        bind:this={rowRefs[ext.name]}
                        {ext}
                        instanceId={null}
                        {onRunAllPanelRequest}
                        {onClearRunAllError}
                    />
                    <!-- Instance rows: nested in a body section below the header -->
                    {#if getInstancesOf(ext.name).length > 0}
                        <div class="ext-group-body">
                            {#each getInstancesOf(ext.name) as instanceId (instanceId)}
                                <ExtensionRow
                                    bind:this={rowRefs[instanceId]}
                                    {ext}
                                    {instanceId}
                                    {onRunAllPanelRequest}
                                    {onClearRunAllError}
                                />
                            {/each}
                        </div>
                    {/if}
                </div>
            {/each}
        {/if}
    </div>
</div>
