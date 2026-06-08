/*
 * Copyright Built On Envoy
 * SPDX-License-Identifier: Apache-2.0
 * The full text of the Apache license is available in the LICENSE file at
 * the root of the repo.
 */

// BOE Extension Manager — Validation Logic
//
// Pure business logic: no DOM.
// validateAll() checks all selected extensions for oneOf constraint violations.
// showValidationErrors() is in ui-validation.js (presentation layer).

/**
 * Expand path segments containing __items__ to actual array indices.
 * For example, path "policies.__items__.inline" with val [{inline:"x"}]
 * yields [{ val: "x", path: "policies.0.inline" }].
 *
 * @param {string} pathStr - Dot-separated path, may contain __items__
 * @param {*} rootVal - The root object to resolve the path against
 * @returns {Array<{val, path}>}
 */
export function expandPaths(pathStr, rootVal) {
    const parts = pathStr.split('.');

    const expand = (parts, obj, resolvedPath) => {
        if (parts.length === 0) return [{ val: obj, path: resolvedPath }];
        const [head, ...rest] = parts;
        if (head === '__items__') {
            if (!Array.isArray(obj)) return [];
            return obj.flatMap((item, i) =>
                expand(rest, item, resolvedPath ? resolvedPath + '.' + i : String(i))
            );
        }
        return expand(
            rest,
            obj && obj[head],
            resolvedPath ? resolvedPath + '.' + head : head,
        );
    };

    return expand(parts, rootVal, '');
}

/**
 * Validate all selected extensions against their oneOf constraints.
 * Returns an array of { name, errors } for extensions that fail validation.
 *
 * Standard JSONEditor errors are merged with custom oneOf errors.
 *
 * @param {Map<string, object>} editors - Map of extension name → JSONEditor instance
 * @param {Map<string, object>} selected - Map of selected extension names
 * @param {Map<string, Array>} oneOfConstraints - Map of extension name → constraints
 * @returns {Array<{name: string, errors: Array}>}
 */
/**
 * Check required fields in a JSON Schema against an actual value object.
 * JSONEditor considers `required` satisfied if the key exists (even as ""),
 * so we must check that required string/array fields are non-empty.
 *
 * @param {object} schema - The raw (pre-flatten) JSON schema
 * @param {object} val    - The current editor value
 * @param {string} prefix - Path prefix for nested fields
 * @returns {Array<{path, property, message}>}
 */
export function validateRequired(schema, val, prefix = '') {
    if (!schema || typeof schema !== 'object' || !val || typeof val !== 'object') return [];
    const errors = [];
    for (const field of (schema.required || [])) {
        const path = prefix ? `${prefix}.${field}` : field;
        const v = val[field];
        if (v === undefined || v === null || v === '' || (Array.isArray(v) && v.length === 0)) {
            errors.push({ path: 'root.' + path, property: 'required', message: `"${field}" is required` });
        } else if (schema.properties?.[field]?.type === 'object' && typeof v === 'object') {
            errors.push(...validateRequired(schema.properties[field], v, path));
        }
    }
    return errors;
}

export function validateAll(editors, selected, oneOfConstraints) {
    const results = [];

    for (const [name, editor] of editors) {
        if (!selected.has(name)) continue;

        const errors = [...editor.validate()];
        const constraints = oneOfConstraints.get(name) || [];
        const rootVal = editor.getValue();

        for (const { path, branches } of constraints) {
            const hint = branches.map(b => b.join(' + ')).join(', or ');
            const allFields = [...new Set(branches.flat())];

            const instances = path
                ? expandPaths(path, rootVal)
                : [{ val: rootVal, path: '' }];

            for (const { val, path: resolvedPath } of instances) {
                if (!val || typeof val !== 'object') continue;
                const branchSatisfied = branches.some(reqFields =>
                    reqFields.every(f => val[f] !== undefined && val[f] !== null && val[f] !== '')
                );
                if (!branchSatisfied) {
                    for (const field of allFields) {
                        const fieldPath = resolvedPath ? resolvedPath + '.' + field : field;
                        errors.push({
                            path: 'root.' + fieldPath,
                            property: 'oneOf',
                            message: `One of ${hint} is required`,
                        });
                    }
                }
            }
        }

        if (errors.length > 0) {
            results.push({ name, errors });
        }
    }

    return results;
}
