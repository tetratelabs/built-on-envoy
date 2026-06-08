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

    import ExtensionRow from './ExtensionRow.svelte';

    // Props
    let { extensions = [], onRunAllPanelRequest, onClearRunAllError } = $props();

    // Filter state
    let search   = $state('');
    let category = $state('');

    // All unique categories across all extensions (sorted)
    let allCategories = $derived(
        [...new Set(extensions.flatMap(e => e.categories))].sort()
    );

    // Filtered list
    let filtered = $derived(() => {
        const q = search.toLowerCase();
        return extensions.filter(ext => {
            if (q && !ext.name.toLowerCase().includes(q) &&
                     !ext.description.toLowerCase().includes(q) &&
                     !ext.tags.some(t => t.toLowerCase().includes(q))) {
                return false;
            }
            if (category && !ext.categories.includes(category)) return false;
            return true;
        });
    });

    // Public API — row refs keyed by extension name
    let rowRefs = $state({});

    export function getRowRef(name) { return rowRefs[name]; }
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
                <ExtensionRow
                    bind:this={rowRefs[ext.name]}
                    {ext}
                    {onRunAllPanelRequest}
                    {onClearRunAllError}
                />
            {/each}
        {/if}
    </div>
</div>
