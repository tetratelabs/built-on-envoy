import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';

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
	const manifests = findManifests(extensionsDir);

	console.log(`Found ${manifests.length} manifest(s)`);

	// Copy each manifest
	for (const { sourcePath, relativePath } of manifests) {
		const targetPath = path.join(targetDir, relativePath, 'manifest.yaml');
		const targetDirPath = path.dirname(targetPath);

		// Ensure target directory exists
		fs.mkdirSync(targetDirPath, { recursive: true });

		// Copy the file
		fs.copyFileSync(sourcePath, targetPath);
		console.log(`  Copied: ${relativePath}/manifest.yaml`);
	}

	console.log('Manifests copied successfully!');
}

// Run the copy
try {
	copyManifests();
} catch (error) {
	console.error('Error copying manifests:', error);
	process.exit(1);
}
