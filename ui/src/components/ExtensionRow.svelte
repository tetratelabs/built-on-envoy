<!--
  ~ Copyright Built On Envoy
  ~ SPDX-License-Identifier: Apache-2.0
  ~ The full text of the Apache license is available in the LICENSE file at
  ~ the root of the repo.
-->

<script>
    // ExtensionRow.svelte — a single extension card in the catalog list
    //
    // Two modes controlled by the `instanceId` prop:
    //
    //   instanceId = null   →  "main row": always visible per extension.
    //                          Toggle ON creates the first instance; toggle OFF
    //                          removes ALL instances. A "+" button (shown when active)
    //                          adds more instances. No config form.
    //
    //   instanceId = "foo#N"  →  "instance row": shows config form, run button, and
    //                            filter type selector. Has a "×" remove button instead
    //                            of a toggle.

    import { untrack } from 'svelte';
    import { categoryClass } from '../lib/utils.js';
    import { getConfigs, getInstancesOf, isRunning, setRunning,
             addExtensionInstance, deselectExtension, deselectAllInstancesOf, setConfig,
             instanceExtName, getDisplayLabel } from '../lib/store.svelte.js';
    import { getSchema, postRun, postStop } from '../lib/api.js';
    import { parseSSEStream } from '../lib/sse.js';
    import ConfigForm from './ConfigForm.svelte';
    import InlineTerminal from './InlineTerminal.svelte';

    // Props
    let { ext, instanceId = null, onRunAllPanelRequest, onClearRunAllError } = $props();

    // Main row active when ≥1 instances exist; instance rows always active.
    let isOn = $derived(instanceId !== null || instanceCount > 0);

    // Instance count for this extension (used by definition row for the count badge)
    let activeInstances = $derived(getInstancesOf(ext.name));
    let instanceCount   = $derived(activeInstances.length);

    // Display label: "ext-name" for single instance, "ext-name #N" for multiples
    let displayLabel = $derived(instanceId !== null ? getDisplayLabel(instanceId) : ext.name);

    // Filter type — only relevant for extensions declaring >1 filter types
    let needsFilterType  = $derived((ext.filterType ?? []).length > 1);
    // Pre-select the first filter type so a valid value is always present.
    // untrack() makes the intentional initial-only read explicit to the compiler.
    let selectedFilterType = $state(untrack(() => (ext.filterType ?? []).length > 1 ? ext.filterType[0] : ''));

    // Local state
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
    let poppedOut       = $state(false);
    let _sharedAppendFn  = null;
    let _sharedRunningFn = null;

    // Load schema at mount for instance rows only (main rows have no config form).
    $effect(() => {
        if (instanceId !== null) _loadSchema();
    });

    // ── Toggle ────────────────────────────────────────────────────────────────

    // Main row toggle: ON creates the first instance, OFF removes all instances.
    function handleToggle() {
        onClearRunAllError?.();
        if (isOn) {
            deselectAllInstancesOf(ext.name);
        } else {
            addExtensionInstance(ext);
        }
    }

    // "+" button on main row: add another instance while one already exists.
    function handleAddInstance(e) {
        e.stopPropagation();
        addExtensionInstance(ext);
        onClearRunAllError?.();
    }

    // "×" button on instance row: remove this instance.
    function handleRemove(e) {
        e.stopPropagation();
        deselectExtension(instanceId);
        invalid = false;
        runErrorMsg = '';
        showPanel = false;
        onClearRunAllError?.();
    }

    function handleHeaderClick(e) {
        if (e.target.closest('.ext-toggle') || e.target.closest('a')) return;
        if (instanceId !== null) collapsed = !collapsed;
    }

    // ── Schema loading ────────────────────────────────────────────────────────

    async function _loadSchema() {
        if (schema !== undefined) return;
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

        // Validate filter type selection when required
        if (needsFilterType && !selectedFilterType) {
            runErrorMsg = 'Select a filter type before running.';
            invalid = true;
            return;
        }

        const cfg = formRef?.getCurrentConfig?.() ?? '';
        if (cfg) setConfig(instanceId, cfg);

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
            extensions: [{
                name: ext.name,
                config: getConfigs().get(instanceId) || '',
                filterType: needsFilterType ? selectedFilterType : '',
            }],
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
            title: `Run: ${displayLabel}`,
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

    export function getName()             { return ext.name; }
    export function getInstanceId()       { return instanceId; }
    export function getFilterType()       { return needsFilterType ? selectedFilterType : ''; }
    export function getFormRef()          { return formRef; }
    export function getOneOfConstraints() { return formRef?.getOneOfConstraints?.() ?? []; }
    export function getCurrentConfig()    { return formRef?.getCurrentConfig?.() ?? ''; }

    export function validate() {
        const errors = formRef?.validate?.() ?? [];
        if (needsFilterType && !selectedFilterType) {
            invalid = true;
            runErrorMsg = 'Select a filter type before running.';
            return [...errors, { name: ext.name, errors: ['Filter type is required'] }];
        }
        return errors;
    }

    export function showErrors() {
        formRef?.showErrors?.();
        invalid = true;
        if (needsFilterType && !selectedFilterType) {
            runErrorMsg = 'Select a filter type before running.';
        }
    }

    export function clearErrors() {
        formRef?.clearErrors?.();
        invalid = false;
        runErrorMsg = '';
    }

    export function markInvalid() { invalid = true; }
</script>

<div
    class="ext-row"
    class:ext-row-main={instanceId === null}
    class:ext-row-instance={instanceId !== null}
    class:active={isOn}
    class:collapsed={isOn && collapsed}
    class:ext-row-invalid={invalid}
    data-ext-name={ext.name}
    data-instance-id={instanceId}
>
    <!-- Header -->
    <div class="ext-row-header" onclick={handleHeaderClick} role="presentation">
        <span class="ext-row-chevron">
            <svg width="12" height="12" viewBox="0 0 12 12" fill="currentColor">
                <path d="M4.5 2L9 6l-4.5 4V2z"/>
            </svg>
        </span>

        {#if instanceId === null}
            <!-- Main row: full info with link, description, and categories -->
            <div class="ext-row-info">
                {#if !ext.local}
                    <a
                        class="ext-row-name"
                        href="https://builtonenvoy.io/extensions/{encodeURIComponent(ext.name)}"
                        target="_blank"
                        rel="noopener noreferrer"
                    >{displayLabel}</a>
                {:else}
                    <span class="ext-row-name">{displayLabel}</span>
                {/if}
                <div class="ext-row-desc">{ext.description}</div>
            </div>

            <div class="ext-row-categories">
                {#each [...new Set(ext.categories ?? [])] as cat (cat)}
                    <span class={categoryClass(cat)}>{cat}</span>
                {/each}
            </div>

            <!-- "+" add instance button, shown when active -->
            {#if isOn}
                <button class="ext-add-btn" onclick={handleAddInstance} title="Add another instance" disabled={isRunning()}>
                    <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
                        <line x1="8" y1="2" x2="8" y2="14"/>
                        <line x1="2" y1="8" x2="14" y2="8"/>
                    </svg>
                </button>
            {/if}

            <label class="ext-toggle">
                <input type="checkbox" checked={isOn} onchange={handleToggle} disabled={isRunning()}>
                <span class="ext-toggle-slider"></span>
            </label>
        {:else}
            <!-- Instance row: compact — just the label and a remove button -->
            <span class="ext-instance-label">{displayLabel}</span>
            <button class="ext-remove-btn" onclick={handleRemove} title="Remove this instance" disabled={isRunning()}>
                <svg width="13" height="13" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <polyline points="3,4 4,4 13,4"/>
                    <path d="M5 4V3a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v1"/>
                    <path d="M13 4l-1 9a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1L3 4"/>
                    <line x1="7" y1="7" x2="7" y2="11"/>
                    <line x1="9" y1="7" x2="9" y2="11"/>
                </svg>
            </button>
        {/if}
    </div>

    <!-- Body (only for instance rows; main row has no config form) -->
    {#if isOn && instanceId !== null}
        <div class="ext-row-body">
            <!-- Filter type selector (only for extensions with multiple filter types) -->
            {#if needsFilterType}
                <div class="filter-type-selector">
                    <label class="filter-type-label">
                        Filter type
                        <select
                            class="filter-type-select"
                            bind:value={selectedFilterType}
                        >
                            {#each [...new Set(ext.filterType ?? [])] as ft (ft)}
                                <option value={ft}>{ft}</option>
                            {/each}
                        </select>
                    </label>
                </div>
            {/if}

            <!-- Config form -->
            {#if schemaLoading}
                <div class="loading-overlay" style="padding:20px 0">
                    <div class="spinner"></div> Loading schema...
                </div>
            {:else}
                <ConfigForm bind:this={formRef} name={ext.name} {instanceId} {schema} />
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
