<!--
  ~ Copyright Built On Envoy
  ~ SPDX-License-Identifier: Apache-2.0
  ~ The full text of the Apache license is available in the LICENSE file at
  ~ the root of the repo.
-->

<script>
    // OrderPopover.svelte — drag-and-drop order popover panel
    //
    // Portals itself to document.body on mount so it can be positioned
    // relative to a badge button without being clipped by overflow:hidden
    // ancestors.

    // Props
    let { title = '', order = [], reorderFn, onclose } = $props();

    // Local drag state
    let draggedIdx = $state(null);
    let dragOverIdx = $state(null);

    function handleDragStart(idx) {
        draggedIdx = idx;
    }

    function handleDragEnd() {
        draggedIdx = null;
        dragOverIdx = null;
    }

    function handleDragOver(e, idx) {
        e.preventDefault();
        if (draggedIdx !== null && draggedIdx !== idx) {
            dragOverIdx = idx;
        }
    }

    function handleDragLeave(idx) {
        if (dragOverIdx === idx) dragOverIdx = null;
    }

    function handleDrop(idx) {
        if (draggedIdx !== null && draggedIdx !== idx) {
            reorderFn(draggedIdx, idx);
        }
        draggedIdx = null;
        dragOverIdx = null;
    }

    function handleKeyDown(e) {
        if (e.key === 'Escape') onclose?.();
    }
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
    class="order-popover"
    role="dialog"
    aria-modal="false"
    tabindex="-1"
    onkeydown={handleKeyDown}
>
    <div class="order-popover-title">{title}</div>
    <div class="order-popover-list">
        {#each order as name, idx (name)}
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div
                class="order-popover-item"
                class:dragging={draggedIdx === idx}
                class:drag-over={dragOverIdx === idx}
                draggable="true"
                ondragstart={() => handleDragStart(idx)}
                ondragend={handleDragEnd}
                ondragover={(e) => handleDragOver(e, idx)}
                ondragleave={() => handleDragLeave(idx)}
                ondrop={() => handleDrop(idx)}
            >
                <span class="order-popover-handle">&#9776;</span>
                <span class="order-popover-name">{name}</span>
            </div>
        {/each}
    </div>
</div>
