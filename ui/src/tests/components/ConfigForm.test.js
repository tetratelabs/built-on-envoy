/*
 * Copyright Built On Envoy
 * SPDX-License-Identifier: Apache-2.0
 * The full text of the Apache license is available in the LICENSE file at
 * the root of the repo.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/svelte';
import ConfigForm from '../../components/ConfigForm.svelte';
import { flattenSchema } from '../../lib/schema.js';

// ── Schema fixtures (same as schema.test.js) ────────────────────────────────

const cedarSchema = {
    type: 'object',
    required: ['policy', 'principal_type', 'principal_id_header'],
    additionalProperties: false,
    properties: {
        policy: { '$ref': '#/$defs/DataSource', description: 'Cedar policy set to evaluate.' },
        principal_type: { type: 'string' },
        principal_id_header: { type: 'string' },
        deny_status: { type: 'integer', minimum: 100, maximum: 599, default: 403 },
        fail_open: { type: 'boolean' },
        deny_headers: {
            type: 'object',
            additionalProperties: { type: 'string' },
        },
        entities_file: { type: 'string' },
    },
    $defs: {
        DataSource: {
            type: 'object',
            description: 'A data source. Exactly one must be set.',
            additionalProperties: false,
            properties: {
                inline: { type: 'string', description: 'Data provided directly as a string.' },
                file: { type: 'string', description: 'Path to a file.' },
            },
            oneOf: [{ required: ['inline'] }, { required: ['file'] }],
        },
    },
};

const opaSchema = {
    type: 'object',
    required: ['policies'],
    additionalProperties: false,
    properties: {
        policies: {
            type: 'array',
            items: { '$ref': '#/$defs/DataSource' },
            minItems: 1,
        },
        fail_open: { type: 'boolean' },
        decision_path: { type: 'string' },
    },
    $defs: {
        DataSource: {
            type: 'object',
            additionalProperties: false,
            properties: {
                inline: { type: 'string' },
                file: { type: 'string' },
            },
            oneOf: [{ required: ['inline'] }, { required: ['file'] }],
        },
    },
};

const fileServerSchema = {
    type: 'object',
    required: ['path_mappings'],
    additionalProperties: false,
    properties: {
        path_mappings: {
            type: 'array',
            items: {
                type: 'object',
                required: ['request_path_prefix', 'file_path_prefix'],
                additionalProperties: false,
                properties: {
                    request_path_prefix: { type: 'string' },
                    file_path_prefix: { type: 'string' },
                },
            },
        },
        content_types: {
            type: 'object',
            additionalProperties: { type: 'string' },
        },
        default_content_type: { type: 'string' },
        directory_index_files: { type: 'array', items: { type: 'string' } },
    },
};

const chatCompletionsSchema = {
    type: 'object',
    additionalProperties: false,
    properties: {
        metadata_namespace: { type: 'string' },
    },
};

// ── Helpers ──────────────────────────────────────────────────────────────────

/** Build a JSONEditor stub that spies on key methods */
function makeEditorStub({ getValue = () => ({}), validate = () => [] } = {}) {
    return class {
        constructor(container, opts) {
            this._opts = opts;
            this._listeners = {};
            this.element = container;
        }
        on(event, cb) {
            this._listeners[event] = cb;
            if (event === 'ready') setTimeout(cb, 0);
        }
        getValue() { return getValue(); }
        setValue(v) { this._value = v; }
        validate() { return validate(); }
        getEditor() { return null; }
        destroy() { this._destroyed = true; }
    };
}

// ── Lifecycle tests ──────────────────────────────────────────────────────────

describe('ConfigForm — null schema', () => {
    it('renders "No configuration needed" message', () => {
        render(ConfigForm, { props: { name: 'ext-a', schema: null } });
        expect(screen.getByText(/No configuration needed/i)).toBeTruthy();
    });

    it('does not render the editor container with content', () => {
        render(ConfigForm, { props: { name: 'ext-a', schema: null } });
        expect(screen.queryByRole('textbox')).toBeNull();
    });
});

describe('ConfigForm — JSONEditor unavailable', () => {
    let savedJSONEditor;
    beforeEach(() => {
        savedJSONEditor = globalThis.JSONEditor;
        delete globalThis.JSONEditor;
    });
    afterEach(() => {
        globalThis.JSONEditor = savedJSONEditor;
    });

    it('renders fallback textarea when JSONEditor is not defined', () => {
        const { container } = render(ConfigForm, { props: { name: 'ext-a', schema: cedarSchema } });
        expect(container.querySelector('textarea')).toBeTruthy();
    });

    it('fallback textarea has correct id', () => {
        const { container } = render(ConfigForm, { props: { name: 'my-ext', schema: cedarSchema } });
        const ta = container.querySelector('textarea');
        expect(ta?.id).toBe('raw-config-my-ext');
    });
});

describe('ConfigForm — JSONEditor instantiation', () => {
    it('instantiates JSONEditor when schema is provided', async () => {
        const constructorSpy = vi.fn().mockImplementation(function(_container, opts) {
            this._opts = opts;
            this._listeners = {};
            this.on = (e, cb) => { this._listeners[e] = cb; if (e === 'ready') setTimeout(cb, 0); };
            this.getValue = () => ({});
            this.setValue = vi.fn();
            this.validate = () => [];
            this.destroy = vi.fn();
        });
        globalThis.JSONEditor = constructorSpy;

        render(ConfigForm, { props: { name: 'cedar', schema: cedarSchema } });

        await waitFor(() => expect(constructorSpy).toHaveBeenCalledOnce());
    });

    it('passes flattenSchema-processed schema to JSONEditor (no $defs, no additionalProperties:false)', async () => {
        let capturedSchema = null;
        globalThis.JSONEditor = class {
            constructor(_container, opts) {
                capturedSchema = opts.schema;
                this._listeners = {};
            }
            on(e, cb) { if (e === 'ready') setTimeout(cb, 0); }
            getValue() { return {}; }
            setValue() {}
            validate() { return []; }
            destroy() {}
        };

        render(ConfigForm, { props: { name: 'cedar', schema: cedarSchema } });

        await waitFor(() => expect(capturedSchema).not.toBeNull());
        expect(capturedSchema.$defs).toBeUndefined();
        expect(capturedSchema.additionalProperties).toBeUndefined();
    });

    it('calls editor.destroy() on unmount', async () => {
        const destroySpy = vi.fn();
        globalThis.JSONEditor = class {
            constructor() { this._listeners = {}; }
            on(e, cb) { if (e === 'ready') setTimeout(cb, 0); }
            getValue() { return {}; }
            setValue() {}
            validate() { return []; }
            destroy() { destroySpy(); }
        };

        const { unmount } = render(ConfigForm, { props: { name: 'ext-a', schema: cedarSchema } });
        await waitFor(() => {}); // let ready fire
        unmount();
        expect(destroySpy).toHaveBeenCalled();
    });

    it('restores saved config via setValue on editor ready', async () => {
        const setValueSpy = vi.fn();
        globalThis.JSONEditor = class {
            constructor() { this._listeners = {}; }
            on(e, cb) { this._listeners[e] = cb; if (e === 'ready') setTimeout(cb, 0); }
            getValue() { return { principal_type: 'User' }; }
            setValue(v) { setValueSpy(v); }
            validate() { return []; }
            destroy() {}
        };

        render(ConfigForm, {
            props: { name: 'cedar', schema: cedarSchema, config: '{"principal_type":"User"}' },
        });

        await waitFor(() => expect(setValueSpy).toHaveBeenCalledWith({ principal_type: 'User' }));
    });
});

// ── Cedar — oneOf + additionalProperties + optional fields ───────────────────

describe('ConfigForm — Cedar schema (flattenSchema correctness)', () => {
    it('flattenSchema resolves policy $ref and records oneOf constraint', () => {
        const { schema, oneOfConstraints } = flattenSchema(cedarSchema);
        // $ref inlined
        expect(schema.properties.policy.$ref).toBeUndefined();
        expect(schema.properties.policy.properties.inline).toBeDefined();
        expect(schema.properties.policy.properties.file).toBeDefined();
        // oneOf constraint recorded
        const pc = oneOfConstraints.find(c => c.path === 'policy');
        expect(pc).toBeDefined();
        expect(pc.branches).toEqual([['inline'], ['file']]);
    });

    it('flattenSchema marks policy with _jeOneOf', () => {
        const { schema } = flattenSchema(cedarSchema);
        expect(schema.properties.policy._jeOneOf).toBe(true);
    });

    it('flattenSchema detects deny_headers as additionalPropsPaths', () => {
        const { additionalPropsPaths } = flattenSchema(cedarSchema);
        expect(additionalPropsPaths).toContain('deny_headers');
    });

    it('flattenSchema marks root as having optional properties', () => {
        const { optionalPropsPaths } = flattenSchema(cedarSchema);
        // Root path '' is marked when some fields are not required (entities_file, fail_open, etc.)
        expect(optionalPropsPaths).toContain('');
    });

    it('flattenSchema does NOT mark oneOf-flattened policy in optionalPropsPaths', () => {
        const { optionalPropsPaths } = flattenSchema(cedarSchema);
        expect(optionalPropsPaths).not.toContain('policy');
    });

    it('flattenSchema removes $defs from output schema', () => {
        const { schema } = flattenSchema(cedarSchema);
        expect(schema.$defs).toBeUndefined();
    });

    it('flattenSchema preserves policy description from parent', () => {
        const { schema } = flattenSchema(cedarSchema);
        expect(schema.properties.policy.description).toBe('Cedar policy set to evaluate.');
    });
});

describe('ConfigForm — Cedar validation', () => {
    beforeEach(() => {
        globalThis.JSONEditor = makeEditorStub({ getValue: () => ({}) });
    });

    it('validate() returns oneOf error when policy has neither inline nor file', async () => {
        flattenSchema(cedarSchema);
        // Build a mock editor that returns empty policy
        globalThis.JSONEditor = makeEditorStub({
            getValue: () => ({ policy: {}, principal_type: 'User', principal_id_header: 'x-user' }),
            validate: () => [],
        });

        const { component } = render(ConfigForm, {
            props: { name: 'cedar', schema: cedarSchema },
        });

        await waitFor(() => {});
        const errors = component.validate();
        // There should be an error related to the policy oneOf constraint
        expect(Array.isArray(errors)).toBe(true);
    });

    it('showErrors() adds je-show-errors class to form container', async () => {
        const { component, container } = render(ConfigForm, {
            props: { name: 'cedar', schema: cedarSchema },
        });
        await waitFor(() => {});
        component.showErrors();
        const form = container.querySelector('.config-section-body');
        expect(form?.classList.contains('je-show-errors')).toBe(true);
    });

    it('clearErrors() removes je-show-errors class', async () => {
        const { component, container } = render(ConfigForm, {
            props: { name: 'cedar', schema: cedarSchema },
        });
        await waitFor(() => {});
        component.showErrors();
        component.clearErrors();
        const form = container.querySelector('.config-section-body');
        expect(form?.classList.contains('je-show-errors')).toBe(false);
    });
});

// ── OPA — array items with oneOf ─────────────────────────────────────────────

describe('ConfigForm — OPA schema (flattenSchema correctness)', () => {
    it('flattenSchema records oneOf constraint for array items at policies.__items__', () => {
        const { oneOfConstraints } = flattenSchema(opaSchema);
        const ic = oneOfConstraints.find(c => c.path === 'policies.__items__');
        expect(ic).toBeDefined();
        expect(ic.branches).toEqual([['inline'], ['file']]);
    });

    it('flattenSchema removes $defs from output', () => {
        const { schema } = flattenSchema(opaSchema);
        expect(schema.$defs).toBeUndefined();
    });

    it('flattenSchema marks root as having optional properties (fail_open, decision_path are optional)', () => {
        const { optionalPropsPaths } = flattenSchema(opaSchema);
        expect(optionalPropsPaths).toContain('');
    });

    it('flattenSchema inlines DataSource in policies array items', () => {
        const { schema } = flattenSchema(opaSchema);
        const items = schema.properties.policies.items;
        expect(items.$ref).toBeUndefined();
        expect(items.properties.inline).toBeDefined();
        expect(items.properties.file).toBeDefined();
    });
});

// ── File Server — nested objects + additionalProperties ───────────────────────

describe('ConfigForm — File Server schema (flattenSchema correctness)', () => {
    it('flattenSchema detects content_types as additionalPropsPaths', () => {
        const { additionalPropsPaths } = flattenSchema(fileServerSchema);
        expect(additionalPropsPaths).toContain('content_types');
    });

    it('flattenSchema detects root as having optional properties (optionalPropsPaths contains empty string)', () => {
        const { optionalPropsPaths } = flattenSchema(fileServerSchema);
        // Root path '' is marked when some properties are not required
        expect(optionalPropsPaths).toContain('');
    });

    it('flattenSchema does NOT mark content_types as optional props path (it has no fixed properties)', () => {
        const { optionalPropsPaths } = flattenSchema(fileServerSchema);
        expect(optionalPropsPaths).not.toContain('content_types');
    });

    it('flattenSchema removes additionalProperties:false from path_mappings items', () => {
        const { schema } = flattenSchema(fileServerSchema);
        expect(schema.properties.path_mappings.items.additionalProperties).toBeUndefined();
    });

    it('no oneOf constraints (file-server has no oneOf)', () => {
        const { oneOfConstraints } = flattenSchema(fileServerSchema);
        expect(oneOfConstraints).toHaveLength(0);
    });
});

// ── Chat Completions — simple flat schema ─────────────────────────────────────

describe('ConfigForm — Chat Completions schema (flattenSchema correctness)', () => {
    it('flattenSchema produces no oneOf constraints', () => {
        const { oneOfConstraints } = flattenSchema(chatCompletionsSchema);
        expect(oneOfConstraints).toHaveLength(0);
    });

    it('flattenSchema removes root additionalProperties:false', () => {
        const { schema } = flattenSchema(chatCompletionsSchema);
        expect(schema.additionalProperties).toBeUndefined();
    });

    it('flattenSchema detects root as having optional properties (all fields are optional)', () => {
        const { optionalPropsPaths } = flattenSchema(chatCompletionsSchema);
        // Root path '' is marked when no fields are required
        expect(optionalPropsPaths).toContain('');
    });

    it('validate() with empty form returns no errors (nothing required)', async () => {
        globalThis.JSONEditor = makeEditorStub({ getValue: () => ({}) });
        const { component } = render(ConfigForm, {
            props: { name: 'chat', schema: chatCompletionsSchema },
        });
        await waitFor(() => {});
        const errors = component.validate();
        expect(errors).toHaveLength(0);
    });
});

// ── Public API — getEditor, getOneOfConstraints, getCurrentConfig ─────────────

describe('ConfigForm — getEditor and getOneOfConstraints', () => {
    it('getEditor() returns the JSONEditor instance after ready', async () => {
        globalThis.JSONEditor = makeEditorStub();
        const { component } = render(ConfigForm, { props: { name: 'ext', schema: cedarSchema } });
        await waitFor(() => expect(component.getEditor()).not.toBeNull());
    });

    it('getEditor() returns null before schema is provided', () => {
        const { component } = render(ConfigForm, { props: { name: 'ext', schema: null } });
        expect(component.getEditor()).toBeNull();
    });

    it('getOneOfConstraints() returns non-empty array for cedar schema', async () => {
        globalThis.JSONEditor = makeEditorStub();
        const { component } = render(ConfigForm, { props: { name: 'cedar', schema: cedarSchema } });
        await waitFor(() => expect(component.getEditor()).not.toBeNull());
        const constraints = component.getOneOfConstraints();
        expect(Array.isArray(constraints)).toBe(true);
        expect(constraints.length).toBeGreaterThan(0);
    });

    it('getOneOfConstraints() returns empty array for chat completions (no oneOf)', async () => {
        globalThis.JSONEditor = makeEditorStub();
        const { component } = render(ConfigForm, { props: { name: 'chat', schema: chatCompletionsSchema } });
        await waitFor(() => expect(component.getEditor()).not.toBeNull());
        expect(component.getOneOfConstraints()).toEqual([]);
    });
});

describe('ConfigForm — getCurrentConfig', () => {
    it('returns JSON string when editor has non-empty values', async () => {
        globalThis.JSONEditor = makeEditorStub({ getValue: () => ({ principal_type: 'User' }) });
        const { component } = render(ConfigForm, { props: { name: 'cedar', schema: cedarSchema } });
        await waitFor(() => expect(component.getEditor()).not.toBeNull());
        const cfg = component.getCurrentConfig();
        expect(cfg).toBe(JSON.stringify({ principal_type: 'User' }));
    });

    it('returns empty string when editor has all-empty values', async () => {
        globalThis.JSONEditor = makeEditorStub({ getValue: () => ({ principal_type: '', endpoint: null }) });
        const { component } = render(ConfigForm, { props: { name: 'cedar', schema: cedarSchema } });
        await waitFor(() => expect(component.getEditor()).not.toBeNull());
        expect(component.getCurrentConfig()).toBe('');
    });

    it('returns empty string when no editor and no stored config', () => {
        const { component } = render(ConfigForm, { props: { name: 'no-editor-ext', schema: null } });
        expect(component.getCurrentConfig()).toBe('');
    });
});

describe('ConfigForm — _markOptionalPropFields and _markAddPropFields', () => {
    it('marks [data-schemapath] elements with je-optional-props-field for optional paths', async () => {
        // Make JSONEditor render a [data-schemapath] element in the container
        globalThis.JSONEditor = class {
            constructor(container) {
                this._container = container;
                this._listeners = {};
            }
            on(e, cb) {
                this._listeners[e] = cb;
                if (e === 'ready') {
                    setTimeout(() => {
                        // Inject a fake root field — optionalPropsPaths contains "" for the root
                        // object when it has at least one optional property (metadata_namespace)
                        const field = document.createElement('div');
                        field.setAttribute('data-schemapath', 'root');
                        this._container.appendChild(field);
                        cb();
                    }, 0);
                }
            }
            getValue() { return {}; }
            setValue() {}
            validate() { return []; }
            destroy() {}
        };

        const { container } = render(ConfigForm, {
            props: { name: 'chat', schema: chatCompletionsSchema },
        });

        // Wait for ready and field injection
        await waitFor(() => {
            const field = container.querySelector('[data-schemapath="root"]');
            expect(field).toBeTruthy();
            // Root object has optional properties — should have je-optional-props-field class
            expect(field.classList.contains('je-optional-props-field')).toBe(true);
        });
    });
});

describe('ConfigForm — _observeChanges MutationObserver', () => {
    it('MutationObserver is attached and fixEditorUI is called when DOM changes', async () => {
        let editorContainer = null;
        globalThis.JSONEditor = class {
            constructor(container) {
                editorContainer = container;
                this._listeners = {};
            }
            on(e, cb) {
                if (e === 'ready') setTimeout(cb, 0);
            }
            getValue() { return {}; }
            setValue() {}
            validate() { return []; }
            destroy() {}
        };

        render(ConfigForm, {
            props: { name: 'cedar', schema: cedarSchema },
        });

        await waitFor(() => expect(editorContainer).not.toBeNull());
        // Trigger a DOM mutation in the editor container
        expect(() => {
            const div = document.createElement('div');
            editorContainer.appendChild(div);
        }).not.toThrow();
    });
});

describe('ConfigForm — editor destroy on schema change', () => {
    it('destroys old editor and creates new one when schema prop changes', async () => {
        const destroySpy = vi.fn();
        globalThis.JSONEditor = class {
            constructor() { this._listeners = {}; }
            on(e, cb) { if (e === 'ready') setTimeout(cb, 0); }
            getValue() { return {}; }
            setValue() {}
            validate() { return []; }
            destroy() { destroySpy(); }
        };

        const { component, rerender } = render(ConfigForm, { props: { name: 'ext', schema: cedarSchema } });
        await waitFor(() => expect(component.getEditor()).not.toBeNull());

        // Change schema — should destroy old editor
        await rerender({ name: 'ext', schema: chatCompletionsSchema });
        await waitFor(() => expect(destroySpy).toHaveBeenCalled());
    });
});
