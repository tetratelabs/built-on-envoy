<!--
  ~ Copyright Built On Envoy
  ~ SPDX-License-Identifier: Apache-2.0
  ~ The full text of the Apache license is available in the LICENSE file at
  ~ the root of the repo.
-->

<script>
    // App.svelte — root component
    //
    // Fetches extensions, owns runAll / shared terminal / order popover state,
    // and bridges window.catalog for inline HTML onclick handlers.

    import { getExtensions, getSelected, getConfigs, getRunOrder, isRunning,
             setExtensions, setRunning, setConfig, reorderRun }
        from '../lib/store.svelte.js';
    import { postRun, postStop } from '../lib/api.js';
    import { parseSSEStream } from '../lib/sse.js';
    import ExtensionCatalog from './ExtensionCatalog.svelte';
    import SharedTerminal from './SharedTerminal.svelte';
    import OrderPopover from './OrderPopover.svelte';

    // ── Fetch state ──────────────────────────────────────────────────────────

    let loading = $state(true);
    let loadError = $state('');

    $effect(() => {
        fetch('/api/extensions')
            .then(r => r.json())
            .then(exts => { setExtensions(exts); loading = false; })
            .catch(err => { loadError = err.message; loading = false; })
            .finally(() => {
                document.querySelector('.footer')?.classList.add('loaded');
            });
    });

    // ── Derived header badge values ──────────────────────────────────────────

    let selectedCount = $derived(getSelected().size);
    let runOrderCount = $derived(getRunOrder().length);

    // ── Shared terminal ──────────────────────────────────────────────────────

    let sharedTerminalVisible = $state(false);
    let sharedTerminalTitle   = $state('');
    let sharedTerminalRunning = $state(false);
    let sharedTerminalRef     = $state(null);
    // Callbacks set when a popout hands over an in-progress run
    let _sharedPopInFn = $state(null);  // ()=>void — call to pop back into row

    // ── Order popovers ───────────────────────────────────────────────────────

    let runPopoverVisible = $state(false);
    let runPopoverEl      = $state(null);

    function positionPopover(el, triggerEl) {
        if (!el || !triggerEl) return;
        const rect = triggerEl.getBoundingClientRect();
        const vw   = window.innerWidth;
        el.style.top    = (rect.bottom + 8) + 'px';
        el.style.right  = (vw - rect.right) + 'px';
        el.style.left   = '';
    }

    // ── Catalog row refs ─────────────────────────────────────────────────────

    let catalogRef = $state(null);

    function getRowRef(name) { return catalogRef?.getRowRef(name); }

    // ── runAll ───────────────────────────────────────────────────────────────

    export async function runAll() {
        // Sync configs from all selected forms
        for (const [name] of getSelected()) {
            const cfg = getRowRef(name)?.getCurrentConfig?.();
            if (cfg) setConfig(name, cfg);
        }

        // Validate each selected extension via its form (includes required + oneOf checks)
        const errors = [];
        for (const [name] of getSelected()) {
            const formRef = getRowRef(name)?.getFormRef?.();
            if (!formRef) continue;
            formRef.clearErrors();
            const extErrors = formRef.validate();
            errors.push(...extErrors);
        }

        if (errors.length > 0) {
            for (const { name } of errors) {
                getRowRef(name)?.showErrors?.();  // sets invalid=true (red border) + form highlights
            }
            const count = errors.reduce((s, e) => s + e.errors.length, 0);
            _setRunAllError(count);
            return;
        }
        _clearRunAllError();

        const extensions = getRunOrder().map(name => ({
            name, config: getConfigs().get(name) || '',
        }));

        const title = `Run: ${getRunOrder().join(', ')}`;
        await _streamRun({ extensions }, title);
    }

    // ── Order popover toggles ─────────────────────────────────────────────────

    export function toggleRunOrderPopover(event) {
        runPopoverVisible = !runPopoverVisible;
        if (runPopoverVisible) positionPopover(runPopoverEl, event?.target);
    }

    // ── SharedTerminal callbacks ──────────────────────────────────────────────

    // Called by ExtensionRow when user clicks the popout button.
    // The row keeps its (hidden) inline panel alive so the stream continues writing
    // to terminalRef. `lines` is the current snapshot; `running` reflects live state.
    // `popIn` un-hides the inline panel and hides the shared terminal.
    function handleRunAllPanelRequest({ title, lines, running, setSharedAppend, setSharedRunning, popIn }) {
        sharedTerminalTitle   = title;
        sharedTerminalRunning = running;
        sharedTerminalVisible = true;
        _sharedPopInFn        = popIn ?? null;
        sharedTerminalRef?.setLines(lines);
        // Wire live mirroring: subsequent lines from the row's run loop come here
        setSharedAppend?.((text, cls) => sharedTerminalRef?.appendLine(text, cls));
        // Wire running state: row notifies us when run finishes so we can show Close
        setSharedRunning?.((r) => { sharedTerminalRunning = r; });
    }

    function handleSharedClose() {
        sharedTerminalVisible = false;
        _sharedPopInFn = null;
    }

    function handleSharedPopIn() {
        _sharedPopInFn?.();
        sharedTerminalVisible = false;
        _sharedPopInFn = null;
    }

    async function handleSharedStop() {
        try { await postStop(); } catch { /* ignore */ }
    }

    // ── Streaming helper ──────────────────────────────────────────────────────

    async function _streamRun(payload, title) {
        sharedTerminalTitle   = title;
        sharedTerminalRunning = true;
        sharedTerminalVisible = true;

        // Capture ref once — avoids repeated $state proxy access inside async loop
        const term = sharedTerminalRef;
        term?.clear();

        setRunning(true);
        try {
            const resp = await postRun(payload);
            if (!resp.ok) {
                term?.appendLine(await resp.text(), 'terminal-error');
                return;
            }
            for await (const { type, data } of parseSSEStream(resp)) {
                const cls = type === 'error' ? 'terminal-error' :
                            type === 'status' ? 'terminal-status' : '';
                term?.appendLine(data, cls);
            }
        } catch (err) {
            term?.appendLine(`Connection error: ${err.message}`, 'terminal-error');
        } finally {
            setRunning(false);
            sharedTerminalRunning = false;
        }
    }

    // ── Run All error display (header) ────────────────────────────────────────

    let runAllErrorMsg = $state('');

    function _setRunAllError(count) {
        runAllErrorMsg = `${count} validation error${count > 1 ? 's' : ''} found`;
    }
    function _clearRunAllError() { runAllErrorMsg = ''; }

    // ── Header badge sync ─────────────────────────────────────────────────────

    $effect(() => {
        const group = document.getElementById('runAllGroup');
        if (group) group.style.display = selectedCount > 0 ? '' : 'none';
        const runBadge = document.getElementById('runAllBadge');
        if (runBadge) runBadge.textContent = runOrderCount;
    });

    $effect(() => {
        const running = isRunning();
        const runBtn  = document.getElementById('runAllBtn');
        const runBadge = document.getElementById('runAllBadge');
        if (runBtn)   runBtn.disabled   = running;
        if (runBadge) runBadge.disabled = running;
    });

    $effect(() => {
        const group = document.getElementById('runAllGroup');
        if (!group) return;
        let errEl = group.querySelector('.run-all-error');
        if (runAllErrorMsg) {
            if (!errEl) {
                errEl = document.createElement('div');
                errEl.className = 'run-all-error';
                group.appendChild(errEl);
            }
            errEl.textContent = runAllErrorMsg;
        } else if (errEl) {
            errEl.remove();
        }
    });

    // ── Click-outside handler for popovers ───────────────────────────────────

    function handleWindowClick(e) {
        if (runPopoverVisible && runPopoverEl && !runPopoverEl.contains(e.target)) {
            const badge = document.getElementById('runAllBadge');
            if (!badge || !badge.contains(e.target)) runPopoverVisible = false;
        }
    }

</script>

<svelte:window onclick={handleWindowClick} />

<p class="section-desc">Toggle extensions ON to configure and run them.</p>

{#if loading}
    <div class="loading-overlay"></div>
{:else if loadError}
    <div class="empty-state">
        <div class="empty-state-icon">!</div>
        <div class="empty-state-title">Failed to load extensions</div>
        <div class="empty-state-desc">{loadError}</div>
    </div>
{:else}
    <ExtensionCatalog
        bind:this={catalogRef}
        extensions={getExtensions()}
        onRunAllPanelRequest={handleRunAllPanelRequest}
        onClearRunAllError={_clearRunAllError}
    />
{/if}

<!-- Shared terminal (fixed-position bottom panel).
     Always mounted so sharedTerminalRef is bound; hidden via CSS when not visible. -->
<SharedTerminal
    bind:this={sharedTerminalRef}
    title={sharedTerminalTitle}
    running={sharedTerminalRunning}
    visible={sharedTerminalVisible}
    onstop={handleSharedStop}
    onclose={handleSharedClose}
    onpopin={_sharedPopInFn ? handleSharedPopIn : undefined}
/>

<!-- Order popovers — always mounted so bind:this is always resolved -->
<div class="order-popover-wrapper" class:visible={runPopoverVisible} bind:this={runPopoverEl}>
    <OrderPopover
        title="Run Order"
        order={getRunOrder()}
        reorderFn={reorderRun}
        onclose={() => { runPopoverVisible = false; }}
    />
</div>
