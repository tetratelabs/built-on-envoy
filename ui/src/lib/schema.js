/*
 * Copyright Built On Envoy
 * SPDX-License-Identifier: Apache-2.0
 * The full text of the Apache license is available in the LICENSE file at
 * the root of the repo.
 */

// BOE Extension Manager — JSON Schema Preprocessing
//
// Pure business logic: no DOM, no side effects on module state.
// flattenSchema() takes a raw JSON schema and returns a processed version
// ready for use with JSONEditor, along with metadata about constraints.

/**
 * Pre-process a JSON schema for use with JSONEditor:
 *  - Resolves all $ref to inline definitions
 *  - Removes additionalProperties:false (which hides fields in the editor)
 *  - Flattens oneOf constraints into optional properties with description hints
 *  - Detects which paths have optional properties, additionalProperties, or oneOf
 *
 * @param {object} schema - Raw JSON schema object (will be deep-copied internally)
 * @returns {{ schema, oneOfConstraints, additionalPropsPaths, optionalPropsPaths }}
 */
export function flattenSchema(schema) {
    // Deep copy to avoid mutating the cached original
    const s = JSON.parse(JSON.stringify(schema));
    const defs = s.$defs || s.definitions || {};
    const oneOfConstraints = [];
    const additionalPropsPaths = [];
    const optionalPropsPaths = [];

    const resolve = (node, path) => {
        if (!node || typeof node !== 'object') return node;
        if (Array.isArray(node)) return node.map(item => resolve(item, path));

        // Remove additionalProperties:false — it causes json-editor to hide
        // defined properties behind a modal rather than rendering them inline.
        if (node.additionalProperties === false) {
            delete node.additionalProperties;
        }

        // Resolve $ref by inlining the definition
        if (node.$ref) {
            const refPath = node.$ref.replace(/^#\/(\$defs|definitions)\//, '');
            const resolved = defs[refPath];
            if (resolved) {
                const merged = { ...resolve(JSON.parse(JSON.stringify(resolved)), path) };
                if (node.description) merged.description = node.description;
                if (node.title) merged.title = node.title;
                return resolve(merged, path);
            }
        }

        // Flatten oneOf on objects: convert branches into optional properties
        // and record the constraint for custom validation.
        if (node.type === 'object' && node.oneOf) {
            const branches = [];
            const oneOfHints = [];
            for (const branch of node.oneOf) {
                if (branch.required) {
                    branches.push(branch.required);
                    oneOfHints.push(branch.required.join(' + '));
                }
            }
            if (oneOfHints.length > 0) {
                node.description = (node.description || '') +
                    ' (provide one of: ' + oneOfHints.join(', or ') + ')';
            }
            if (branches.length > 0) {
                oneOfConstraints.push({ path, branches });
            }
            delete node.oneOf;
            delete node.required;
            delete node.additionalProperties;
            // Mark as oneOf-flattened so optional-props detection skips it
            node._jeOneOf = true;
        }

        // Detect objects with additionalProperties (free-form key/value maps)
        if (node.type === 'object' && node.additionalProperties &&
            typeof node.additionalProperties === 'object') {
            additionalPropsPaths.push(path);
            node.options = node.options || {};
            node.options.je_add_props = true;
        }

        // Detect objects with at least one optional property.
        // Skip oneOf-flattened nodes — their properties look optional but aren't.
        if (node.type === 'object' && node.properties && !node._jeOneOf) {
            const required = new Set(node.required || []);
            const hasOptional = Object.keys(node.properties).some(k => !required.has(k));
            if (hasOptional) {
                optionalPropsPaths.push(path);
            }
        }

        // Recurse into properties
        if (node.properties) {
            for (const key of Object.keys(node.properties)) {
                node.properties[key] = resolve(
                    node.properties[key],
                    path ? path + '.' + key : key,
                );
            }
        }

        // Recurse into array items — mark path with __items__ so validation
        // knows to iterate over array indices rather than a fixed path.
        if (node.items && typeof node.items === 'object') {
            node.items = resolve(node.items, path ? path + '.__items__' : '__items__');
        }

        if (node.additionalProperties && typeof node.additionalProperties === 'object') {
            node.additionalProperties = resolve(node.additionalProperties, path);
        }

        return node;
    };

    const result = resolve(s, '');
    delete result.$defs;
    delete result.definitions;

    return { schema: result, oneOfConstraints, additionalPropsPaths, optionalPropsPaths };
}
