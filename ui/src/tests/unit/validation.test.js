/*
 * Copyright Built On Envoy
 * SPDX-License-Identifier: Apache-2.0
 * The full text of the Apache license is available in the LICENSE file at
 * the root of the repo.
 */

import { describe, it, expect } from 'vitest';
import { expandPaths, validateAll, validateRequired } from '../../lib/validation.js';

// ── expandPaths ─────────────────────────────────────────────────────────────

describe('expandPaths', () => {
    it('returns the value at a simple flat path', () => {
        const result = expandPaths('policy', { policy: { inline: 'x' } });
        expect(result).toEqual([{ val: { inline: 'x' }, path: 'policy' }]);
    });

    it('returns the value at a nested path', () => {
        const result = expandPaths('a.b', { a: { b: 42 } });
        expect(result).toEqual([{ val: 42, path: 'a.b' }]);
    });

    it('expands __items__ for each array index', () => {
        const val = { policies: [{ inline: 'a' }, { file: 'b' }] };
        const result = expandPaths('policies.__items__', val);
        expect(result).toEqual([
            { val: { inline: 'a' }, path: 'policies.0' },
            { val: { file: 'b' }, path: 'policies.1' },
        ]);
    });

    it('returns empty array when __items__ path points to a non-array', () => {
        const val = { policies: 'not-an-array' };
        const result = expandPaths('policies.__items__', val);
        expect(result).toEqual([]);
    });

    it('handles nested __items__ path with property after it', () => {
        const val = { items: [{ inline: 'x' }, { file: 'y' }] };
        const result = expandPaths('items.__items__.inline', val);
        expect(result).toEqual([
            { val: 'x', path: 'items.0.inline' },
            { val: undefined, path: 'items.1.inline' },
        ]);
    });

    it('returns undefined val for missing nested property', () => {
        const result = expandPaths('a.b.c', { a: {} });
        expect(result).toEqual([{ val: undefined, path: 'a.b.c' }]);
    });
});

// ── validateAll ─────────────────────────────────────────────────────────────

// Minimal mock editor that returns a value and validates.
function mockEditor(value, validateErrors = []) {
    return {
        getValue: () => value,
        validate: () => validateErrors,
    };
}

describe('validateAll', () => {
    it('returns empty results when no extensions are selected', () => {
        const editors = new Map([['ext-a', mockEditor({})]]);
        const selected = new Map();
        const constraints = new Map();
        expect(validateAll(editors, selected, constraints)).toEqual([]);
    });

    it('returns empty results when selected extension has no errors', () => {
        const editors = new Map([['cedar', mockEditor({ policy: { inline: 'allow all;' } })]]);
        const selected = new Map([['cedar', {}]]);
        const constraints = new Map([['cedar', [
            { path: 'policy', branches: [['inline'], ['file']] },
        ]]]);
        expect(validateAll(editors, selected, constraints)).toEqual([]);
    });

    it('returns errors when oneOf is unsatisfied (neither branch filled)', () => {
        const editors = new Map([['cedar', mockEditor({ policy: {} })]]);
        const selected = new Map([['cedar', {}]]);
        const constraints = new Map([['cedar', [
            { path: 'policy', branches: [['inline'], ['file']] },
        ]]]);
        const results = validateAll(editors, selected, constraints);
        expect(results).toHaveLength(1);
        expect(results[0].name).toBe('cedar');
        const paths = results[0].errors.map(e => e.path);
        expect(paths).toContain('root.policy.inline');
        expect(paths).toContain('root.policy.file');
    });

    it('returns errors for second branch satisfied but not first', () => {
        // file is provided — constraint IS satisfied (file branch)
        const editors = new Map([['cedar', mockEditor({ policy: { file: '/etc/policy.cedar' } })]]);
        const selected = new Map([['cedar', {}]]);
        const constraints = new Map([['cedar', [
            { path: 'policy', branches: [['inline'], ['file']] },
        ]]]);
        const results = validateAll(editors, selected, constraints);
        expect(results).toHaveLength(0);
    });

    it('checks each array item independently for __items__ constraints', () => {
        const value = { policies: [{ inline: 'allow all;' }, {}] };
        const editors = new Map([['opa', mockEditor(value)]]);
        const selected = new Map([['opa', {}]]);
        const constraints = new Map([['opa', [
            { path: 'policies.__items__', branches: [['inline'], ['file']] },
        ]]]);
        const results = validateAll(editors, selected, constraints);
        // Item 0 is OK, item 1 has no branch satisfied
        expect(results).toHaveLength(1);
        const paths = results[0].errors.map(e => e.path);
        // Errors should be on item index 1
        expect(paths).toContain('root.policies.1.inline');
        expect(paths).toContain('root.policies.1.file');
        // No errors on item 0
        expect(paths).not.toContain('root.policies.0.inline');
    });

    it('includes standard editor validation errors', () => {
        const stdError = { path: 'root.name', property: 'required', message: 'Name required' };
        const editors = new Map([['ext', mockEditor({}, [stdError])]]);
        const selected = new Map([['ext', {}]]);
        const constraints = new Map();
        const results = validateAll(editors, selected, constraints);
        expect(results[0].errors).toContain(stdError);
    });

    it('skips editors for extensions that are not selected', () => {
        const editors = new Map([
            ['ext-a', mockEditor({})],
            ['ext-b', mockEditor({}, [{ path: 'root.x', property: 'required', message: 'X required' }])],
        ]);
        const selected = new Map([['ext-a', {}]]); // ext-b not selected
        const constraints = new Map();
        const results = validateAll(editors, selected, constraints);
        expect(results.map(r => r.name)).not.toContain('ext-b');
    });
});

// ── validateRequired ─────────────────────────────────────────────────────────

describe('validateRequired', () => {
    it('returns no errors when all required fields are non-empty', () => {
        const schema = { required: ['a', 'b'], properties: { a: { type: 'string' }, b: { type: 'string' } } };
        const val = { a: 'hello', b: 'world' };
        expect(validateRequired(schema, val)).toEqual([]);
    });

    it('returns an error for a required string field that is empty', () => {
        const schema = { required: ['endpoint'], properties: { endpoint: { type: 'string' } } };
        const val = { endpoint: '' };
        const errors = validateRequired(schema, val);
        expect(errors).toHaveLength(1);
        expect(errors[0].path).toBe('root.endpoint');
        expect(errors[0].property).toBe('required');
    });

    it('returns an error for a required field that is null', () => {
        const schema = { required: ['key'] };
        const val = { key: null };
        const errors = validateRequired(schema, val);
        expect(errors[0].path).toBe('root.key');
    });

    it('returns an error for a required field that is undefined', () => {
        const schema = { required: ['key'] };
        const val = {};
        const errors = validateRequired(schema, val);
        expect(errors[0].path).toBe('root.key');
    });

    it('returns an error for a required array field that is empty', () => {
        const schema = { required: ['items'], properties: { items: { type: 'array' } } };
        const val = { items: [] };
        const errors = validateRequired(schema, val);
        expect(errors).toHaveLength(1);
        expect(errors[0].path).toBe('root.items');
    });

    it('returns no errors when a required array field has items', () => {
        const schema = { required: ['items'], properties: { items: { type: 'array' } } };
        const val = { items: [{ id: '1' }] };
        expect(validateRequired(schema, val)).toEqual([]);
    });

    it('returns errors for multiple missing required fields (bedrock-like schema)', () => {
        const schema = {
            required: ['bedrock_endpoint', 'bedrock_cluster', 'bedrock_api_key', 'bedrock_guardrails'],
            properties: {
                bedrock_endpoint:    { type: 'string' },
                bedrock_cluster:     { type: 'string' },
                bedrock_api_key:     { type: 'string' },
                bedrock_guardrails:  { type: 'array' },
            },
        };
        const val = { bedrock_endpoint: '', bedrock_cluster: '', bedrock_api_key: '', bedrock_guardrails: [] };
        const errors = validateRequired(schema, val);
        expect(errors).toHaveLength(4);
        expect(errors.map(e => e.path)).toEqual([
            'root.bedrock_endpoint',
            'root.bedrock_cluster',
            'root.bedrock_api_key',
            'root.bedrock_guardrails',
        ]);
    });

    it('returns no errors when schema has no required field', () => {
        const schema = { properties: { a: { type: 'string' } } };
        const val = { a: '' };
        expect(validateRequired(schema, val)).toEqual([]);
    });

    it('recurses into required nested objects and reports missing inner fields', () => {
        const schema = {
            required: ['address'],
            properties: {
                address: {
                    type: 'object',
                    required: ['street', 'city'],
                    properties: {
                        street: { type: 'string' },
                        city:   { type: 'string' },
                    },
                },
            },
        };
        const val = { address: { street: '', city: '' } };
        const errors = validateRequired(schema, val);
        expect(errors.map(e => e.path)).toEqual(['root.address.street', 'root.address.city']);
    });

    it('returns no errors when nested object has all required fields filled', () => {
        const schema = {
            required: ['address'],
            properties: {
                address: {
                    type: 'object',
                    required: ['street'],
                    properties: { street: { type: 'string' } },
                },
            },
        };
        const val = { address: { street: '123 Main St' } };
        expect(validateRequired(schema, val)).toEqual([]);
    });
});

describe('validateAll — branch coverage', () => {
    function mockEditor(val) {
        return { getValue: () => val, validate: () => [] };
    }

    it('handles empty-path constraint (root-level oneOf) — satisfied', () => {
        // path === '' means the constraint applies at the root object directly
        const editors = new Map([['ext', mockEditor({ inline: 'some value' })]]);
        const selected = new Map([['ext', {}]]);
        const constraints = new Map([['ext', [
            { path: '', branches: [['inline'], ['file']] },
        ]]]);
        expect(validateAll(editors, selected, constraints)).toHaveLength(0);
    });

    it('handles empty-path constraint (root-level oneOf) — unsatisfied', () => {
        const editors = new Map([['ext', mockEditor({})]]);
        const selected = new Map([['ext', {}]]);
        const constraints = new Map([['ext', [
            { path: '', branches: [['inline'], ['file']] },
        ]]]);
        const results = validateAll(editors, selected, constraints);
        expect(results).toHaveLength(1);
        expect(results[0].errors.length).toBeGreaterThan(0);
    });

    it('expandPaths __items__ on a non-array value returns empty', () => {
        // Directly test expandPaths with __items__ against a non-array
        const result = expandPaths('data.__items__', { data: 'not-an-array' });
        expect(result).toEqual([]);
    });

    it('validateAll resolvedPath is empty string when constraint path resolves to root item', () => {
        // An array constraint where items have their own oneOf at path ''
        const editors = new Map([['opa', mockEditor({ policies: [{}] })]]);
        const selected = new Map([['opa', {}]]);
        const constraints = new Map([['opa', [
            { path: 'policies.__items__', branches: [['inline'], ['file']] },
        ]]]);
        const results = validateAll(editors, selected, constraints);
        expect(results).toHaveLength(1);
        // Errors should reference the first item path
        expect(results[0].errors[0].path).toContain('policies.0');
    });
});
