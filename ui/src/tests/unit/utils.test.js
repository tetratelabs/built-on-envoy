/*
 * Copyright Built On Envoy
 * SPDX-License-Identifier: Apache-2.0
 * The full text of the Apache license is available in the LICENSE file at
 * the root of the repo.
 */

import { describe, it, expect } from 'vitest';
import { categoryClass } from '../../lib/utils.js';

describe('categoryClass', () => {
    it('lowercases the category name', () => {
        expect(categoryClass('Security')).toBe('badge badge-security');
    });

    it('replaces spaces with hyphens', () => {
        expect(categoryClass('AI Gateway')).toBe('badge badge-ai-gateway');
    });

    it('handles multiple spaces', () => {
        expect(categoryClass('Traffic Control')).toBe('badge badge-traffic-control');
    });

    it('handles already lowercase single word', () => {
        expect(categoryClass('examples')).toBe('badge badge-examples');
    });

    it('always starts with badge prefix', () => {
        expect(categoryClass('Observability')).toMatch(/^badge badge-/);
    });
});
