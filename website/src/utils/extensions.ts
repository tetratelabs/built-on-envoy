import fs from 'fs';
import path from 'path';
import { parse } from 'yaml';

export interface Extension {
	name: string;
	version: string;
	type: string;
	categories: string[];
	author: string;
	description: string;
	longDescription: string;
	tags: string[];
	repository: string;
	license: string;
	featured?: boolean;
	composerVersion?: string;
	/** Relative path from the extensions directory to the extension folder */
	path: string;
}

interface ManifestInfo {
	filePath: string;
	relativePath: string;
}

/**
 * Recursively find all manifest.yaml files in the directory
 */
function findManifests(dir: string, baseDir: string = dir): ManifestInfo[] {
	const manifests: ManifestInfo[] = [];

	if (!fs.existsSync(dir)) {
		return manifests;
	}

	const items = fs.readdirSync(dir);

	for (const item of items) {
		const itemPath = path.join(dir, item);
		const stat = fs.statSync(itemPath);

		if (stat.isDirectory()) {
			manifests.push(...findManifests(itemPath, baseDir));
		} else if (item === 'manifest.yaml') {
			const relativePath = path.relative(baseDir, path.dirname(itemPath));
			manifests.push({ filePath: itemPath, relativePath });
		}
	}

	return manifests;
}

/**
 * Load all extension manifests from the copied manifests directory.
 * The manifests directory is populated by the copy-manifests script,
 * which already filters out 'internal' folders.
 */
export function loadExtensions(): Extension[] {
	const manifestsDir = path.join(process.cwd(), 'manifests');
	const manifestInfos = findManifests(manifestsDir);

	return manifestInfos
		.map(({ filePath, relativePath }) => {
			const manifestContent = fs.readFileSync(filePath, 'utf-8');
			const manifest = parse(manifestContent) as Omit<Extension, 'path'>;
			return { ...manifest, path: relativePath };
		})
		.filter((ext): ext is Extension => ext !== null)
		.sort((a, b) => a.name.localeCompare(b.name));
}

/**
 * Get a specific extension by name
 */
export function getExtensionByName(name: string): Extension | null {
	const extensions = loadExtensions();
	return extensions.find(ext => ext.name === name) || null;
}
