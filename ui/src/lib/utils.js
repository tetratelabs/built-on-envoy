/*
 * Copyright Built On Envoy
 * SPDX-License-Identifier: Apache-2.0
 * The full text of the Apache license is available in the LICENSE file at
 * the root of the repo.
 */

// BOE Extension Manager — Shared Utilities

/**
 * Convert a category name to a CSS badge class.
 * e.g. "AI" → "badge badge-ai"
 * @param {string} cat
 * @returns {string}
 */
export function categoryClass(cat) {
    return 'badge badge-' + cat.toLowerCase().replace(/\s+/g, '-');
}
