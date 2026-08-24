import { defineConfig } from 'astro/config';
import mdx from '@astrojs/mdx';
import mermaid from "astro-mermaid";
import { unified } from '@astrojs/markdown-remark';

// The extension pages are generated from these index files, which are read at
// build time and are therefore not tracked by Vite's module graph. Registering
// them as watch files makes Astro restart the dev server whenever they are
// generated again, so the pages pick up the changes.
const extensionIndexes = ['public/extensions.json', 'public/extension-sets.json'];

/** @type {import('astro').AstroIntegration} */
const watchExtensionIndexes = {
	name: 'watch-extension-indexes',
	hooks: {
		'astro:config:setup': ({ command, config, addWatchFile }) => {
			if (command !== 'dev') {
				return;
			}
			for (const index of extensionIndexes) {
				addWatchFile(new URL(index, config.root));
			}
		},
	},
};

// https://astro.build/config
export default defineConfig({
	// Use the classic unified (remark/rehype) Markdown processor instead of
	// Astro's newer Sätteri default. astro-mermaid rewrites ```mermaid fences
	// into raw `<pre class="mermaid">` HTML nodes; on the Sätteri processor those
	// nodes only compile in MDX with `features.rawHtml`, whose full-tree reparse
	// strips code fences' language info and disables Shiki syntax highlighting.
	// The unified processor emits the mermaid HTML and keeps code fences highlighted.
	markdown: {
		processor: unified(),
	},
	integrations: [
		mdx(),
		mermaid({
			theme: "neutral",
			autoTheme: true,
		}),
		watchExtensionIndexes,
	],
});
