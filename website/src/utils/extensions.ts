import fs from 'fs';
import path from 'path';

import { GITHUB_REPO, EXTENSIONS_PATH } from '../constants';

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
