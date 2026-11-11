// Extensions highlighted on the home page and the extension catalog.
//
// Highlighted extensions are rendered with a distinct style and shown first,
// in the order they appear in this list. Each entry can optionally set:
//   - label: the text of the badge shown on the card (defaults to "Highlight").
//   - until: an expiry date (YYYY-MM-DD). The highlight is ignored when the
//            site is built after that date, so entries don't go stale.
//
// Names must match an extension in public/extensions.json, or the build fails.

export interface Highlight {
	name: string;
	label?: string;
	until?: string;
}

export const HIGHLIGHTS: Highlight[] = [
	{ name: 'switchyard', label: 'New' },
	{ name: 'dynamic-fault-injection', label: 'New'},
	{ name: 'coraza-waf', label: 'Popular'},
];
