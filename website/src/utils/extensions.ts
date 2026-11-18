import fs from 'fs';
import path from 'path';

import { GITHUB_REPO, EXTENSIONS_PATH } from '../constants';
import { HIGHLIGHTS } from '../data/highlights';

const DEFAULT_HIGHLIGHT_LABEL = 'Highlight';

export interface Example {
	title: string;
	description: string;
	code: string;
}

export interface Extension {
	name: string;
	version: string;
	type: string;
	categories: string[];
	author: string;
	description: string;
	longDescription: string;
	tags: string[];
	license: string;
	parent?: string;
	featured?: boolean;
	composerVersion?: string;
	minEnvoyVersion?: string;
	maxEnvoyVersion?: string;
	examples?: Example[];
	sourcePath: string;
	sourceUrl: string;
	configReferencePath?: string;
	filterType: string[];
	extensionSet?: boolean;
	highlight?: { label: string };
}

/**
 * Load and normalize an extension index file (extensions.json or
 * extension-sets.json), resolving each entry's source URL.
 */
function loadIndex(fileName: string): Extension[] {
	const jsonPath = path.join(process.cwd(), 'public', fileName);
	const content = fs.readFileSync(jsonPath, 'utf-8');
	const extensions = JSON.parse(content) as Omit<Extension, 'sourceUrl'>[];

	return extensions
		.map(ext => ({
			...ext,
			sourceUrl: `${GITHUB_REPO}/tree/main/${EXTENSIONS_PATH}/${ext.sourcePath}`,
		}))
		.sort((a, b) => a.name.localeCompare(b.name));
}

/**
 * Load all extensions from the extensions.json file (the marketplace catalog).
 */
export function loadExtensions(): Extension[] {
	return loadIndex('extensions.json');
}

/**
 * Load all extension sets (bundles) from the extension-sets.json file. These are
 * not shown in the catalog grid but each gets its own documentation page.
 */
export function loadExtensionSets(): Extension[] {
	return loadIndex('extension-sets.json');
}

/**
 * Mark the extensions configured in HIGHLIGHTS and move them to the front of
 * the list, in the order they are configured. The remaining extensions keep
 * their original order. Highlights whose `until` date has passed are ignored.
 * Throws if a highlight references an unknown extension, so typos and removed
 * extensions fail the build.
 */
export function applyHighlights(extensions: Extension[], now: Date = new Date()): Extension[] {
	const byName = new Map(extensions.map(ext => [ext.name, ext]));
	const today = now.toISOString().slice(0, 10);

	const highlighted: Extension[] = [];
	for (const h of HIGHLIGHTS) {
		const ext = byName.get(h.name);
		if (!ext) {
			throw new Error(`Highlighted extension "${h.name}" not found in extensions.json (see src/data/highlights.ts)`);
		}
		if (h.until && h.until < today) {
			continue;
		}
		if (highlighted.some(e => e.name === h.name)) {
			continue;
		}
		highlighted.push({ ...ext, highlight: { label: h.label ?? DEFAULT_HIGHLIGHT_LABEL } });
	}

	const names = new Set(highlighted.map(ext => ext.name));
	return [...highlighted, ...extensions.filter(ext => !names.has(ext.name))];
}
