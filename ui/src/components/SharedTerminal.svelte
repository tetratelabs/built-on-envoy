<!--
  ~ Copyright Built On Envoy
  ~ SPDX-License-Identifier: Apache-2.0
  ~ The full text of the Apache license is available in the LICENSE file at
  ~ the root of the repo.
-->

<script>
    // SharedTerminal.svelte — fixed-position bottom panel terminal
    //
    // Conditionally rendered by App.svelte. Uses the same imperative DOM
    // append strategy as InlineTerminal for streaming performance.

    // Props
    // visible  — controls display:none; kept mounted so the ref is always bound
    // onclose  — called when Close button clicked
    // onpopin  — called when "pop back in" button clicked (only present after popout)
    // running  — if true, show Stop button instead of Close
    // onstop   — called when Stop button clicked
    let { title = '', visible = false, onclose, onpopin, running = false, onstop } = $props();

    let terminalEl = $state(null);
    let minimized = $state(false);

    // ── Public API ────────────────────────────────────────────────────────────

    export function appendLine(text, cls) {
        if (!terminalEl) return;
        const line = document.createElement('div');
        line.className = 'terminal-line' + (cls ? ' ' + cls : '');
        line.innerHTML = text;
        terminalEl.appendChild(line);
        terminalEl.scrollTop = terminalEl.scrollHeight;
    }

    export function clear() {
        if (terminalEl) terminalEl.innerHTML = '';
    }

    export function setLines(lines) {
        clear();
        for (const { html, cls } of lines) appendLine(html, cls);
    }

    export function getLines() {
        if (!terminalEl) return [];
        return Array.from(terminalEl.querySelectorAll('.terminal-line')).map(el => ({
            html: el.innerHTML,
            cls: el.className.replace('terminal-line', '').trim(),
        }));
    }

    function toggleMinimize() {
        minimized = !minimized;
    }
</script>

<div class="run-all-panel" class:minimized style={visible ? '' : 'display:none'}>
    <div class="run-all-panel-header">
        <span class="run-all-panel-title">{title}</span>
        <div class="run-all-panel-controls">
            {#if running}
                <button class="btn btn-danger btn-sm" onclick={onstop}>Stop</button>
            {:else if onclose}
                <button class="btn btn-secondary btn-sm" onclick={onclose}>Close</button>
            {/if}
            {#if onpopin}
                <button
                    class="btn-icon run-all-popin"
                    title="Move back to extension"
                    onclick={onpopin}
                >
                    <svg width="13" height="13" viewBox="0 0 16 16" fill="currentColor">
                        <path d="M13 3h-4V1L5 4l4 3V5h4v6H7v2h6a1 1 0 001-1V4a1 1 0 00-1-1z"/>
                    </svg>
                </button>
            {/if}
            <button
                class="btn-icon run-all-minimize"
                title={minimized ? 'Restore' : 'Minimize'}
                onclick={toggleMinimize}
            >
                {#if minimized}
                    <svg width="14" height="14" viewBox="0 0 14 14" fill="currentColor">
                        <path d="M7 4l5 6H2z"/>
                    </svg>
                {:else}
                    <svg width="14" height="14" viewBox="0 0 14 14" fill="currentColor">
                        <path d="M7 10L2 4h10z"/>
                    </svg>
                {/if}
            </button>
        </div>
    </div>
    <div bind:this={terminalEl} class="terminal"></div>
</div>
