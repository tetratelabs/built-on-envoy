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
	featured?: boolean;
	composerVersion?: string;
	minEnvoyVersion?: string;
	maxEnvoyVersion?: string;
	examples?: Example[];
	sourcePath: string;
	sourceUrl: string;
}

/**
 * Load all extensions from the extensions.json file.
 */
export function loadExtensions(): Extension[] {
	const jsonPath = path.join(process.cwd(), 'public', 'extensions.json');
	const content = fs.readFileSync(jsonPath, 'utf-8');
	const extensions = JSON.parse(content) as Omit<Extension, 'sourceUrl'>[];

	return extensions
		.map(ext => ({
			...ext,
			sourceUrl: `${GITHUB_REPO}/tree/main/${EXTENSIONS_PATH}/${ext.sourcePath}`,
		}))
		.sort((a, b) => a.name.localeCompare(b.name));
}

