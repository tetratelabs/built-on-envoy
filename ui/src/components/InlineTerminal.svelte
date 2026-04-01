<!--
  ~ Copyright Built On Envoy
  ~ SPDX-License-Identifier: Apache-2.0
  ~ The full text of the Apache license is available in the LICENSE file at
  ~ the root of the repo.
-->

<script>
    // InlineTerminal.svelte — per-extension streaming terminal
    //
    // Uses imperative DOM for append performance — streaming 100+ lines/second
    // through Svelte reactive state would be too expensive. Svelte owns the
    // container element via bind:this; the terminal content is managed directly.

    let terminalEl = $state(null);

    // ── Public API ────────────────────────────────────────────────────────────

    /**
     * Append a styled line to the terminal.
     * @param {string} text   HTML content (may contain ANSI-converted spans)
     * @param {string} [cls]  Optional extra CSS class
     */
    export function appendLine(text, cls) {
        if (!terminalEl) return;
        const line = document.createElement('div');
        line.className = 'terminal-line' + (cls ? ' ' + cls : '');
        line.innerHTML = text;
        terminalEl.appendChild(line);
        terminalEl.scrollTop = terminalEl.scrollHeight;
    }

    /** Remove all terminal output. */
    export function clear() {
        if (terminalEl) terminalEl.innerHTML = '';
    }

    /** Returns all current terminal lines as an array of {html, cls} objects. */
    export function getLines() {
        if (!terminalEl) return [];
        return Array.from(terminalEl.querySelectorAll('.terminal-line')).map(el => ({
            html: el.innerHTML,
            cls: el.className.replace('terminal-line', '').trim(),
        }));
    }

    /** Replace terminal content with lines from a previous session (used on popback). */
    export function setLines(lines) {
        clear();
        for (const { html, cls } of lines) appendLine(html, cls);
    }
</script>

<div bind:this={terminalEl} class="terminal ext-inline-terminal"></div>
