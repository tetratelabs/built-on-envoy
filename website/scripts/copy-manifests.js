import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';
import { parse } from 'yaml';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const extensionsDir = path.resolve(__dirname, '../../extensions');
const targetDir = path.resolve(__dirname, '../manifests');

/**
 * Recursively find all manifest.yaml files, ignoring 'internal' folders
 */
function findManifests(dir, baseDir = dir) {
	const manifests = [];
	const items = fs.readdirSync(dir);

	for (const item of items) {
		// Skip 'internal' folders
		if (item === 'internal') continue;

		const itemPath = path.join(dir, item);
		const stat = fs.statSync(itemPath);

		if (stat.isDirectory()) {
			// Recursively search subdirectories
			manifests.push(...findManifests(itemPath, baseDir));
		} else if (item === 'manifest.yaml') {
			// Found a manifest file, store with relative path
			const relativePath = path.relative(baseDir, dir);
			manifests.push({
				sourcePath: itemPath,
				relativePath: relativePath
			});
		}
	}

	return manifests;
}

/**
 * Parse all manifests and build a lookup map by name
 */
function loadAllManifests(manifestEntries) {
	const allManifests = new Map();
	for (const { sourcePath, relativePath } of manifestEntries) {
		const rawContent = fs.readFileSync(sourcePath, 'utf-8');
		const manifest = parse(rawContent);
		allManifests.set(manifest.name, { manifest, rawContent, sourcePath, relativePath });
	}
	return allManifests;
}

/**
 * Resolve versions for composer-type manifests that reference a parent.
 * Mirrors the Go resolveVersions logic.
 * Returns YAML lines to append to the original file content.
 */
function resolveVersions(manifest, allManifests) {
	const appendLines = [];
	if (manifest.type === 'composer' && manifest.parent) {
		const parentEntry = allManifests.get(manifest.parent);
		if (!parentEntry) {
			throw new Error(`Failed to find parent manifest ${manifest.parent} for manifest ${manifest.name}`);
		}
		const parent = parentEntry.manifest;
		if (!manifest.version) {
			appendLines.push(`version: ${parent.version}`);
		}
		if (!manifest.composerVersion) {
			appendLines.push(`composerVersion: ${parent.version}`);
		}
	}
	return appendLines;
}

/**
 * Copy manifests to the target directory
 */
function copyManifests() {
	console.log('Copying extension manifests...');

	// Clean and recreate target directory
	if (fs.existsSync(targetDir)) {
		fs.rmSync(targetDir, { recursive: true });
	}
	fs.mkdirSync(targetDir, { recursive: true });

	// Find all manifests
	const manifestEntries = findManifests(extensionsDir);
	console.log(`Found ${manifestEntries.length} manifest(s)`);

	// Parse all manifests so we can resolve parent references
	const allManifests = loadAllManifests(manifestEntries);

	let copied = 0;
	for (const [name, { manifest, rawContent, relativePath }] of allManifests) {
		// Skip extension sets
		if (manifest.extensionSet) {
			console.log(`  Skipped extension set: ${name}`);
			continue;
		}

		// Resolve versions from parent manifests
		const appendLines = resolveVersions(manifest, allManifests);

		const targetPath = path.join(targetDir, relativePath, 'manifest.yaml');
		const targetDirPath = path.dirname(targetPath);

		// Ensure target directory exists
		fs.mkdirSync(targetDirPath, { recursive: true });

		// Write the original content with any resolved fields appended
		let output = rawContent;
		if (appendLines.length > 0) {
			if (!output.endsWith('\n')) {
				output += '\n';
			}
			output += appendLines.join('\n') + '\n';
		}
		fs.writeFileSync(targetPath, output);
		console.log(`  Copied: ${relativePath}/manifest.yaml`);
		copied++;
	}

	console.log(`Copied ${copied} manifest(s) successfully!`);
}

// Run the copy
try {
	copyManifests();
} catch (error) {
	console.error('Error copying manifests:', error);
	process.exit(1);
}
