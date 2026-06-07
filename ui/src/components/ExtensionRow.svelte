<!--
  ~ Copyright Built On Envoy
  ~ SPDX-License-Identifier: Apache-2.0
  ~ The full text of the Apache license is available in the LICENSE file at
  ~ the root of the repo.
-->

<script>
    // ExtensionRow.svelte — a single extension card in the catalog list
    //
    // Manages the toggle (select/deselect), lazy schema loading, config form,
    // and inline terminal for per-extension execution.

    import { categoryClass } from '../lib/utils.js';
    import { getConfigs, getSelected, isRunning, setRunning,
             selectExtension, deselectExtension, setConfig } from '../lib/store.svelte.js';
    import { getSchema, postRun, postStop } from '../lib/api.js';
    import { parseSSEStream } from '../lib/sse.js';
    import ConfigForm from './ConfigForm.svelte';
    import InlineTerminal from './InlineTerminal.svelte';

    // Props
    let { ext, onRunAllPanelRequest, onClearRunAllError } = $props();

    // Local state
    let isOn       = $derived(getSelected().has(ext.name));
    let collapsed  = $state(false);
    let schema     = $state(undefined);   // undefined = not yet loaded, null = no schema
    let schemaLoading = $state(false);
    let invalid       = $state(false);
    let runErrorMsg   = $state('');

    // Component refs
    let formRef     = $state(null);
    let terminalRef = $state(null);

    // Inline panel visibility
    let showPanel       = $state(false);
    let panelRunning    = $state(false);
    let poppedOut       = $state(false);  // true while output is in shared terminal
    let _sharedAppendFn  = null;          // set by handlePopout, cleared on popIn
    let _sharedRunningFn = null;          // notifies App when running state changes

    // ── Toggle ────────────────────────────────────────────────────────────────

    async function handleToggle(e) {
        onClearRunAllError?.();
        const checked = e.target.checked;
        if (checked) {
            selectExtension(ext);
            collapsed = false;
            await _loadSchema();
        } else {
            // Save config before deselecting
            const cfg = formRef?.getCurrentConfig?.();
            if (cfg) setConfig(ext.name, cfg);
            deselectExtension(ext.name);
            invalid = false;
            runErrorMsg = '';
            showPanel = false;
        }
    }

    function handleHeaderClick(e) {
        if (e.target.closest('.ext-toggle') || e.target.closest('a')) return;
        if (isOn) collapsed = !collapsed;
    }

    // ── Schema loading ────────────────────────────────────────────────────────

    async function _loadSchema() {
        if (schema !== undefined) return; // already loaded (null means no schema)
        schemaLoading = true;
        try {
            const resp = await getSchema(ext.name);
            if (resp.ok) {
                schema = await resp.json();
            } else {
                schema = null;
            }
        } catch {
            schema = null;
        } finally {
            schemaLoading = false;
        }
    }

    // ── Execution ─────────────────────────────────────────────────────────────

    async function handleRun() {
        onClearRunAllError?.();
        // Sync config from form
        const cfg = formRef?.getCurrentConfig?.() ?? '';
        if (cfg) setConfig(ext.name, cfg);

        // Validate
        const errors = formRef?.validate?.() ?? [];
        if (errors.length > 0) {
            formRef?.showErrors?.();
            invalid = true;
            const count = errors.reduce((s, e) => s + e.errors.length, 0);
            runErrorMsg = `${count} validation error${count > 1 ? 's' : ''} found.`;
            return;
        }
        invalid = false;
        runErrorMsg = '';
        formRef?.clearErrors?.();

        showPanel       = true;
        poppedOut       = false;
        _sharedAppendFn  = null;
        _sharedRunningFn = null;
        terminalRef?.clear();
        panelRunning    = true;
        setRunning(true);

        const payload = {
            extensions: [{ name: ext.name, config: getConfigs().get(ext.name) || '' }],
        };

        try {
            const resp = await postRun(payload);
            if (!resp.ok) {
                const msg = await resp.text();
                terminalRef?.appendLine(msg, 'terminal-error');
                _sharedAppendFn?.(msg, 'terminal-error');
                return;
            }
            for await (const { type, data } of parseSSEStream(resp)) {
                const cls = type === 'error' ? 'terminal-error' :
                            type === 'status' ? 'terminal-status' : '';
                terminalRef?.appendLine(data, cls);
                _sharedAppendFn?.(data, cls);
            }
        } catch (err) {
            const msg = `Connection error: ${err.message}`;
            terminalRef?.appendLine(msg, 'terminal-error');
            _sharedAppendFn?.(msg, 'terminal-error');
        } finally {
            panelRunning = false;
            setRunning(false);
            _sharedRunningFn?.(false);
        }
    }

    async function handleStop() {
        try { await postStop(); } catch { /* ignore */ }
    }

    function handleClose() {
        showPanel = false;
        poppedOut = false;
    }

    function handlePopout() {
        const lines = terminalRef?.getLines() ?? [];
        poppedOut = true;
        onRunAllPanelRequest?.({
            title: `Run: ${ext.name}`,
            lines,
            running: panelRunning,
            setSharedAppend:   (fn) => { _sharedAppendFn  = fn; },
            setSharedRunning:  (fn) => { _sharedRunningFn = fn; },
            popIn: () => { poppedOut = false; _sharedAppendFn = null; _sharedRunningFn = null; },
        });
    }

    // ── Public: called by App when user clicks "pop back in" ─────────────────
    export function popIn() { poppedOut = false; _sharedAppendFn = null; _sharedRunningFn = null; }

    // ── Public API (callable from parent) ────────────────────────────────────

    export function getName() { return ext.name; }
    export function getFormRef() { return formRef; }
    export function getOneOfConstraints() { return formRef?.getOneOfConstraints?.() ?? []; }
    export function getCurrentConfig() { return formRef?.getCurrentConfig?.() ?? ''; }
    export function validate() { return formRef?.validate?.() ?? []; }
    export function showErrors() { formRef?.showErrors?.(); invalid = true; }
    export function clearErrors() { formRef?.clearErrors?.(); invalid = false; }
    export function markInvalid() { invalid = true; }
</script>

<div
    class="ext-row"
    class:active={isOn}
    class:collapsed={isOn && collapsed}
    class:ext-row-invalid={invalid}
    data-ext-name={ext.name}
>
    <!-- Header -->
    <div class="ext-row-header" onclick={handleHeaderClick} role="presentation">
        <span class="ext-row-chevron">
            <svg width="12" height="12" viewBox="0 0 12 12" fill="currentColor">
                <path d="M4.5 2L9 6l-4.5 4V2z"/>
            </svg>
        </span>

        <div class="ext-row-info">
            {#if !ext.local}
                <a
                    class="ext-row-name"
                    href="https://builtonenvoy.io/extensions/{encodeURIComponent(ext.name)}"
                    target="_blank"
                    rel="noopener noreferrer"
                >{ext.name}</a>
            {:else}
                <span>{ext.name}</span>
            {/if}
            <div class="ext-row-desc">{ext.description}</div>
        </div>

        <div class="ext-row-categories">
            {#each ext.categories as cat (cat)}
                <span class={categoryClass(cat)}>{cat}</span>
            {/each}
        </div>

        <label class="ext-toggle">
            <input type="checkbox" checked={isOn} onchange={handleToggle}>
            <span class="ext-toggle-slider"></span>
        </label>
    </div>

    <!-- Body (only shown when active) -->
    {#if isOn}
        <div class="ext-row-body">
            <!-- Config form -->
            {#if schemaLoading}
                <div class="loading-overlay" style="padding:20px 0">
                    <div class="spinner"></div> Loading schema...
                </div>
            {:else}
                <ConfigForm bind:this={formRef} name={ext.name} {schema} />
            {/if}

            <!-- Actions -->
            <div class="ext-row-actions">
                <button
                    class="btn btn-primary ext-run-btn"
                    disabled={isRunning()}
                    onclick={handleRun}
                >
                    &#9654; Run Extension
                </button>
                {#if runErrorMsg}
                    <div class="run-all-error ext-run-error">{runErrorMsg}</div>
                {/if}

                <!-- Inline terminal panel — kept in DOM while popped out so the
                     stream can continue writing; hidden via display:none -->
                {#if showPanel}
                    <div class="ext-inline-panel" style={poppedOut ? 'display:none' : ''}>
                        <div class="ext-inline-panel-header">
                            {#if panelRunning}
                                <button class="btn btn-danger btn-sm" onclick={handleStop}>Stop</button>
                            {:else}
                                <button class="btn btn-secondary btn-sm" onclick={handleClose}>Close</button>
                            {/if}
                            {#if !poppedOut}
                                <button
                                    class="btn-icon ext-inline-popout"
                                    title="Move to shared console"
                                    onclick={handlePopout}
                                >
                                    <svg width="13" height="13" viewBox="0 0 16 16" fill="currentColor">
                                        <path d="M3 3h4V1H1v6h2V3zm6-2v2h4v4h2V1H9zm-1 6H3v6h6V7zm-4 4V9h2v2H4z"/>
                                    </svg>
                                </button>
                            {/if}
                        </div>
                        <InlineTerminal bind:this={terminalRef} />
                    </div>
                {/if}
            </div>
        </div>
    {/if}
</div>
