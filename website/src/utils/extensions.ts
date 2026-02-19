import fs from 'fs';
import path from 'path';

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
	examples?: Example[];
	/** Relative path from the extensions directory to the extension folder */
	path: string;
}

/**
 * Load all extensions from the extensions.json file.
 */
export function loadExtensions(): Extension[] {
	const jsonPath = path.join(process.cwd(), 'public', 'extensions.json');
	const content = fs.readFileSync(jsonPath, 'utf-8');
	const extensions = JSON.parse(content) as Omit<Extension, 'path'>[];

	return extensions
		.map(ext => ({
			...ext,
			path: ext.type === 'composer' ? `composer/${ext.name}` : `${ext.name}`,
		}))
		.sort((a, b) => a.name.localeCompare(b.name));
}

