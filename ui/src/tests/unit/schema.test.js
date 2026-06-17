/*
 * Copyright Built On Envoy
 * SPDX-License-Identifier: Apache-2.0
 * The full text of the Apache license is available in the LICENSE file at
 * the root of the repo.
 */

import { describe, it, expect } from 'vitest';
import { flattenSchema } from '../../lib/schema.js';

// ── Real schema fixtures ────────────────────────────────────────────────────

const cedarSchema = {
    "$schema": "https://json-schema.org/draft/2020-12/schema",
    "title": "Cedar Authorization Configuration",
    "type": "object",
    "required": ["policy", "principal_type", "principal_id_header"],
    "additionalProperties": false,
    "properties": {
        "policy": { "$ref": "#/$defs/DataSource", "description": "Cedar policy set to evaluate." },
        "principal_type": { "type": "string" },
        "principal_id_header": { "type": "string" },
        "deny_status": { "type": "integer", "minimum": 100, "maximum": 599, "default": 403 },
        "fail_open": { "type": "boolean" },
    },
    "$defs": {
        "DataSource": {
            "type": "object",
            "description": "A data source. Exactly one must be set.",
            "additionalProperties": false,
            "properties": {
                "inline": { "type": "string", "description": "Data provided directly as a string." },
                "file": { "type": "string", "description": "Path to a file." }
            },
            "oneOf": [
                { "required": ["inline"] },
                { "required": ["file"] }
            ]
        }
    }
};

const opaSchema = {
    "$schema": "https://json-schema.org/draft/2020-12/schema",
    "title": "OPA Authorization Configuration",
    "type": "object",
    "required": ["policies"],
    "additionalProperties": false,
    "properties": {
        "policies": {
            "type": "array",
            "items": { "$ref": "#/$defs/DataSource" },
            "minItems": 1
        },
        "fail_open": { "type": "boolean" },
    },
    "$defs": {
        "DataSource": {
            "type": "object",
            "additionalProperties": false,
            "properties": {
                "inline": { "type": "string" },
                "file": { "type": "string" }
            },
            "oneOf": [
                { "required": ["inline"] },
                { "required": ["file"] }
            ]
        }
    }
};

const fileServerSchema = {
    "$schema": "https://json-schema.org/draft/2020-12/schema",
    "title": "File Server Configuration",
    "type": "object",
    "required": ["path_mappings"],
    "additionalProperties": false,
    "properties": {
        "path_mappings": {
            "type": "array",
            "items": {
                "type": "object",
                "required": ["request_path_prefix", "file_path_prefix"],
                "additionalProperties": false,
                "properties": {
                    "request_path_prefix": { "type": "string" },
                    "file_path_prefix": { "type": "string" }
                }
            }
        },
        "content_types": {
            "type": "object",
            "additionalProperties": { "type": "string" }
        },
        "default_content_type": { "type": "string" }
    }
};

// ── Tests ───────────────────────────────────────────────────────────────────

describe('flattenSchema — Cedar', () => {
    it('resolves $ref and inlines DataSource definition', () => {
        const { schema } = flattenSchema(cedarSchema);
        const policy = schema.properties.policy;
        // $ref should be gone; inline/file properties should be present
        expect(policy.$ref).toBeUndefined();
        expect(policy.properties.inline).toBeDefined();
        expect(policy.properties.file).toBeDefined();
    });

    it('preserves parent description when resolving $ref', () => {
        const { schema } = flattenSchema(cedarSchema);
        expect(schema.properties.policy.description).toBe('Cedar policy set to evaluate.');
    });

    it('marks the resolved DataSource with _jeOneOf', () => {
        const { schema } = flattenSchema(cedarSchema);
        expect(schema.properties.policy._jeOneOf).toBe(true);
    });

    it('records a oneOf constraint for the policy field', () => {
        const { oneOfConstraints } = flattenSchema(cedarSchema);
        expect(oneOfConstraints.length).toBeGreaterThan(0);
        const policyConstraint = oneOfConstraints.find(c => c.path === 'policy');
        expect(policyConstraint).toBeDefined();
        expect(policyConstraint.branches).toEqual([['inline'], ['file']]);
    });

    it('preserves deny_status default value', () => {
        const { schema } = flattenSchema(cedarSchema);
        expect(schema.properties.deny_status.default).toBe(403);
    });

    it('removes additionalProperties:false from root', () => {
        const { schema } = flattenSchema(cedarSchema);
        expect(schema.additionalProperties).toBeUndefined();
    });

    it('removes $defs from the output', () => {
        const { schema } = flattenSchema(cedarSchema);
        expect(schema.$defs).toBeUndefined();
    });

    it('detects optional properties at the root level', () => {
        const { optionalPropsPaths } = flattenSchema(cedarSchema);
        // Root has required fields but also optional ones (fail_open, deny_status, etc.)
        expect(optionalPropsPaths).toContain('');
    });

    it('does NOT include oneOf-flattened node in optionalPropsPaths', () => {
        const { optionalPropsPaths } = flattenSchema(cedarSchema);
        // "policy" is a oneOf-flattened DataSource — should not appear as optional props
        expect(optionalPropsPaths).not.toContain('policy');
    });
});

describe('flattenSchema — OPA', () => {
    it('records oneOf constraint for items of the policies array', () => {
        const { oneOfConstraints } = flattenSchema(opaSchema);
        const constraint = oneOfConstraints.find(c => c.path === 'policies.__items__');
        expect(constraint).toBeDefined();
        expect(constraint.branches).toEqual([['inline'], ['file']]);
    });

    it('marks array item DataSource with _jeOneOf', () => {
        const { schema } = flattenSchema(opaSchema);
        expect(schema.properties.policies.items._jeOneOf).toBe(true);
    });
});

describe('flattenSchema — File Server', () => {
    it('detects content_types as an additionalProperties path', () => {
        const { additionalPropsPaths } = flattenSchema(fileServerSchema);
        expect(additionalPropsPaths).toContain('content_types');
    });

    it('does NOT mark content_types as optionalPropsPaths (it has no fixed properties)', () => {
        const { optionalPropsPaths } = flattenSchema(fileServerSchema);
        expect(optionalPropsPaths).not.toContain('content_types');
    });

    it('removes additionalProperties:false from array item objects', () => {
        const { schema } = flattenSchema(fileServerSchema);
        expect(schema.properties.path_mappings.items.additionalProperties).toBeUndefined();
    });
});

// Schema that mimics dns-gateway.json: root-level oneOf with $ref branches,
// one branch has required fields, the other does not.
const dnsGatewayLikeSchema = {
    title: 'DNS Gateway Configuration',
    description: 'Pass with --filter-type udp_listener or --filter-type network.',
    $defs: {
        DnsGatewayConfig: {
            type: 'object',
            required: ['domains'],
            additionalProperties: false,
            properties: {
                domains: {
                    type: 'array',
                    minItems: 1,
                    items: {
                        type: 'object',
                        required: ['domain', 'base_ip'],
                        additionalProperties: false,
                        properties: {
                            domain: { type: 'string' },
                            base_ip: { type: 'string' },
                        },
                    },
                },
                fail_open: { type: 'boolean', default: false },
            },
        },
        CacheLookupConfig: {
            type: 'object',
            additionalProperties: false,
            properties: {
                filter_state_prefix: { type: 'string', default: 'io.builtonenvoy.dns_gateway' },
            },
        },
    },
    oneOf: [
        { $ref: '#/$defs/DnsGatewayConfig' },
        { $ref: '#/$defs/CacheLookupConfig' },
    ],
};

describe('flattenSchema — dns-gateway-like (oneOf of $ref branches)', () => {
    it('merges properties from all $ref branches into root', () => {
        const { schema } = flattenSchema(dnsGatewayLikeSchema);
        expect(schema.properties.domains).toBeDefined();
        expect(schema.properties.fail_open).toBeDefined();
        expect(schema.properties.filter_state_prefix).toBeDefined();
    });

    it('sets type to object on the root when missing', () => {
        const { schema } = flattenSchema(dnsGatewayLikeSchema);
        expect(schema.type).toBe('object');
    });

    it('removes oneOf from the output', () => {
        const { schema } = flattenSchema(dnsGatewayLikeSchema);
        expect(schema.oneOf).toBeUndefined();
    });

    it('removes $defs from the output', () => {
        const { schema } = flattenSchema(dnsGatewayLikeSchema);
        expect(schema.$defs).toBeUndefined();
    });

    it('records no oneOf constraint (CacheLookupConfig has no required fields)', () => {
        const { oneOfConstraints } = flattenSchema(dnsGatewayLikeSchema);
        expect(oneOfConstraints).toHaveLength(0);
    });

    it('does NOT mark root as _jeOneOf (no constraint recorded → optional-props detection runs)', () => {
        const { schema } = flattenSchema(dnsGatewayLikeSchema);
        expect(schema._jeOneOf).toBeUndefined();
    });

    it('detects root as having optional properties', () => {
        const { optionalPropsPaths } = flattenSchema(dnsGatewayLikeSchema);
        expect(optionalPropsPaths).toContain('');
    });

    it('strips minItems from the merged domains array', () => {
        const { schema } = flattenSchema(dnsGatewayLikeSchema);
        expect(schema.properties.domains.minItems).toBeUndefined();
    });

    it('removes additionalProperties from the root', () => {
        const { schema } = flattenSchema(dnsGatewayLikeSchema);
        expect(schema.additionalProperties).toBeUndefined();
    });

    it('removes additionalProperties:false from nested branch items', () => {
        const { schema } = flattenSchema(dnsGatewayLikeSchema);
        expect(schema.properties.domains.items.additionalProperties).toBeUndefined();
    });

    it('does not mutate the original schema', () => {
        const original = JSON.parse(JSON.stringify(dnsGatewayLikeSchema));
        flattenSchema(dnsGatewayLikeSchema);
        expect(dnsGatewayLikeSchema).toEqual(original);
    });
});

describe('flattenSchema — does not mutate the original schema', () => {
    it('original schema is unchanged after flattening', () => {
        const original = JSON.parse(JSON.stringify(cedarSchema));
        flattenSchema(cedarSchema);
        expect(cedarSchema).toEqual(original);
    });
});

describe('flattenSchema — simple schema with no special features', () => {
    it('returns schema unchanged for a plain string field', () => {
        const simple = { type: 'object', properties: { name: { type: 'string' } } };
        const { schema, oneOfConstraints, additionalPropsPaths, optionalPropsPaths } = flattenSchema(simple);
        expect(schema.properties.name.type).toBe('string');
        expect(oneOfConstraints).toHaveLength(0);
        expect(additionalPropsPaths).toHaveLength(0);
        // All properties are optional (none are required)
        expect(optionalPropsPaths).toContain('');
    });
});

describe('flattenSchema — branch coverage', () => {
    it('preserves title from $ref node when merging into resolved definition', () => {
        const schema = {
            type: 'object',
            properties: {
                source: {
                    $ref: '#/$defs/Source',
                    title: 'My Source Title',
                    description: 'desc',
                },
            },
            $defs: {
                Source: { type: 'object', properties: { url: { type: 'string' } } },
            },
        };
        const { schema: out } = flattenSchema(schema);
        expect(out.properties.source.title).toBe('My Source Title');
    });

    it('resolves additionalProperties that is an object (not boolean)', () => {
        const schema = {
            type: 'object',
            properties: {
                headers: {
                    type: 'object',
                    additionalProperties: { type: 'string', minLength: 1 },
                },
            },
        };
        const { schema: out } = flattenSchema(schema);
        // additionalProperties should be recursed and kept (not removed)
        expect(out.properties.headers.additionalProperties).toBeDefined();
        expect(out.properties.headers.additionalProperties.type).toBe('string');
    });
});

