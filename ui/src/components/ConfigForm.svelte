<!--
  ~ Copyright Built On Envoy
  ~ SPDX-License-Identifier: Apache-2.0
  ~ The full text of the Apache license is available in the LICENSE file at
  ~ the root of the repo.
-->

<script>
    // ConfigForm.svelte — JSON Schema form component
    //
    // Wraps the JSONEditor library imperatively. The editor instance is a
    // plain variable (NOT $state) so Svelte never attempts to reconcile the
    // editor's DOM subtree. All editor lifecycle is managed via $effect.

    import { flattenSchema } from '../lib/schema.js';
    import { validateAll, validateRequired } from '../lib/validation.js';
    import { fixEditorUI }   from '../lib/fix-editor-ui.js';
    import { getConfigs, setConfig } from '../lib/store.svelte.js';

    // Props
    // name       — extension name (used for raw-textarea id and schema lookups)
    // instanceId — store key for this config slot (falls back to name if not provided)
    // schema     — raw JSON schema object from the API, or null
    // config     — $bindable: current config JSON string; parent reads this back
    let { name, schema, instanceId = null, config = $bindable(null) } = $props();

    // The key used to read/write configs in the store.
    const configKey = () => instanceId ?? name;

    // DOM ref for the editor container div
    let containerEl = $state(null);

    // Editor instance — NOT $state; Svelte must not track its DOM
    let editor = null;

    // Schema-derived metadata (component-local)
    let oneOfConstraints     = [];
    let additionalPropsPaths = [];
    let optionalPropsPaths   = [];

    // ── Editor lifecycle ────────────────────────────────────────────────────

    $effect(() => {
        if (!containerEl) return;

        // Destroy any prior editor (e.g. schema prop changed)
        _destroyEditor();

        if (!schema) return;

        if (typeof JSONEditor === 'undefined') return; // fallback textarea used instead

        const {
            schema: processedSchema,
            oneOfConstraints: ofc,
            additionalPropsPaths: addPaths,
            optionalPropsPaths: optPaths,
        } = flattenSchema(schema);

        oneOfConstraints     = ofc;
        additionalPropsPaths = addPaths;
        optionalPropsPaths   = optPaths;

        const editorContainer = document.createElement('div');
        containerEl.appendChild(editorContainer);

        editor = new JSONEditor(editorContainer, {
            schema: processedSchema,
            theme: 'html',
            iconlib: null,
            disable_collapse: false,
            disable_edit_json: true,
            disable_properties: false,
            no_additional_properties: false,
            show_opt_in: false,
            prompt_before_delete: false,
            object_layout: 'normal',
            show_errors: 'never',
            label_as_title: false,
        });

        editor.on('ready', () => {
            const saved = config ?? getConfigs().get(configKey());
            if (saved) {
                try { editor.setValue(JSON.parse(saved)); } catch { /* ignore */ }
            }
            editorContainer.dataset.extName = name;
            _markAddPropFields(editorContainer);
            _markOptionalPropFields(editorContainer);
            fixEditorUI(editorContainer);
            _observeChanges(editorContainer);
        });

        editor.on('change', () => {
            if (!editor) return;
            const val = editor.getValue();
            const hasValues = Object.keys(val).some(k => {
                const v = val[k];
                return v !== undefined && v !== null && v !== '';
            });
            config = hasValues ? JSON.stringify(val) : null;
            setConfig(configKey(), config);
        });

        // $effect cleanup: runs before re-run or on unmount
        return () => _destroyEditor();
    });

    function _destroyEditor() {
        if (editor) {
            editor.destroy();
            editor = null;
        }
        oneOfConstraints     = [];
        additionalPropsPaths = [];
        optionalPropsPaths   = [];
    }

    // ── Public methods (callable from parent via bind:this) ─────────────────

    export function getEditor() { return editor; }

    export function getOneOfConstraints() { return oneOfConstraints; }

    export function getCurrentConfig() {
        if (editor) {
            const val = editor.getValue();
            const hasValues = Object.keys(val).some(k => {
                const v = val[k];
                return v !== undefined && v !== null && v !== '';
            });
            return hasValues ? JSON.stringify(val) : '';
        }
        const textarea = containerEl?.querySelector(`#raw-config-${name}`);
        if (textarea?.value.trim()) return textarea.value.trim();
        return getConfigs().get(configKey()) || '';
    }

    export function validate() {
        const val = editor ? editor.getValue() : {};
        const requiredErrors = validateRequired(schema, val);
        if (!editor) {
            return requiredErrors.length > 0 ? [{ name, errors: requiredErrors }] : [];
        }
        const allErrors = validateAll(
            new Map([[name, editor]]),
            new Map([[name, true]]),
            new Map([[name, oneOfConstraints]]),
        );
        if (requiredErrors.length > 0) {
            const existing = allErrors.find(r => r.name === name);
            if (existing) existing.errors.push(...requiredErrors);
            else allErrors.push({ name, errors: requiredErrors });
        }
        return allErrors;
    }

    export function showErrors() {
        if (!containerEl) return;
        containerEl.classList.add('je-show-errors');
        const errors = validate();
        const editorErrors = editor ? editor.validate() : [];

        const errorPaths = new Set();
        for (const e of editorErrors) {
            if (e.path) errorPaths.add(e.path.replace(/^root\.?/, ''));
        }
        for (const ext of errors) {
            for (const err of ext.errors) {
                if (err.path) {
                    // validateAll paths are 'root.field'; strip prefix to match schemapath
                    errorPaths.add(err.path.replace(/^root\.?/, ''));
                }
                const m = err.message?.match(/['"]([^'"]+)['"]/);
                if (m) errorPaths.add(m[1]);
            }
        }

        containerEl.querySelectorAll('[data-schemapath]').forEach(el => {
            const sp = el.getAttribute('data-schemapath');
            const fieldPath = sp === 'root' ? '' : sp.replace(/^root\.?/, '');
            if (errorPaths.has(fieldPath)) {
                el.classList.add('je-validation-error');
                const input = el.querySelector('input, textarea, select');
                if (input) input.classList.add('je-validation-error');
            }
        });

        const editorRoot = containerEl.querySelector('[data-ext-name]') || containerEl;
        fixEditorUI(editorRoot);
    }

    export function clearErrors() {
        if (!containerEl) return;
        containerEl.classList.remove('je-show-errors');
        containerEl.querySelectorAll('.je-validation-error')
            .forEach(el => el.classList.remove('je-validation-error'));
    }

    // ── Private helpers ──────────────────────────────────────────────────────

    // Convert a schema path (which uses "__items__" for array positions) to a
    // regex that matches JSONEditor's rendered data-schemapath (which uses actual
    // numeric indices like "domains.0.metadata").
    function _pathToRegex(pattern) {
        const escaped = pattern.split('.').map(seg =>
            seg === '__items__' ? '\\d+' : seg.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
        ).join('\\.');
        return new RegExp('^' + escaped + '$');
    }

    function _markAddPropFields(root) {
        if (additionalPropsPaths.length === 0) return;
        const patterns = additionalPropsPaths.map(_pathToRegex);
        root.querySelectorAll('[data-schemapath]').forEach(el => {
            const sp = el.getAttribute('data-schemapath');
            const fieldPath = sp === 'root' ? '' : sp.replace(/^root\.?/, '');
            if (patterns.some(re => re.test(fieldPath))) el.classList.add('je-add-props-field');
        });
    }

    function _markOptionalPropFields(root) {
        if (optionalPropsPaths.length === 0) return;
        const patterns = optionalPropsPaths.map(_pathToRegex);
        root.querySelectorAll('[data-schemapath]').forEach(el => {
            const sp = el.getAttribute('data-schemapath');
            const fieldPath = sp === 'root' ? '' : sp.replace(/^root\.?/, '');
            if (patterns.some(re => re.test(fieldPath))) el.classList.add('je-optional-props-field');
        });
    }

    function _observeChanges(root) {
        if (root._jeObserver) return;
        let pending = false;
        root._jeObserver = new MutationObserver(() => {
            if (!pending) {
                pending = true;
                requestAnimationFrame(() => {
                    _markAddPropFields(root);
                    _markOptionalPropFields(root);
                    fixEditorUI(root);
                    pending = false;
                });
            }
        });
        root._jeObserver.observe(root, { childList: true, subtree: true });
    }
</script>

{#if !schema}
    <div class="no-schema-msg">No configuration needed for this extension.</div>
{:else if typeof JSONEditor === 'undefined'}
    <div style="color: var(--warning); margin-bottom: 8px; font-size: 13px;">
        JSON Schema form library not loaded. Enter configuration as raw JSON:
    </div>
    <textarea
        id="raw-config-{name}"
        rows="8"
        style="width:100%; font-family:var(--font-mono); font-size:13px; padding:8px; border:1px solid var(--border); border-radius:var(--radius-sm);"
        placeholder='&#123;"key": "value"&#125;'
    >{getConfigs().get(configKey()) ?? ''}</textarea>
{/if}

<div bind:this={containerEl} class="config-section-body"></div>
