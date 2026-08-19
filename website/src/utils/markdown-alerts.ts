import type { MarkedExtension, Tokens } from 'marked';

/**
 * GitHub-flavored alerts (admonitions) for marked.
 *
 * Manifest markdown can use the same syntax GitHub renders natively, so a
 * longDescription looks right both on the website and in the repo:
 *
 *   > [!NOTE]
 *   > Useful information the user should know.
 *
 * Supported kinds: NOTE, TIP, IMPORTANT, WARNING, CAUTION. The marker must be
 * alone on the first line of the blockquote; anything else stays a plain
 * blockquote, matching GitHub's behavior.
 */

const KINDS = ['note', 'tip', 'important', 'warning', 'caution'] as const;

type AlertKind = (typeof KINDS)[number];

const LABELS: Record<AlertKind, string> = {
	note: 'Note',
	tip: 'Tip',
	important: 'Important',
	warning: 'Warning',
	caution: 'Caution',
};

const ICONS: Record<AlertKind, string> = {
	note: '<circle cx="8" cy="8" r="6.9"/><path d="M8 7.2v4.4M8 4.4v.9"/>',
	tip: '<path d="M8 1.3a4.6 4.6 0 0 0-2.8 8.2c.4.4.6.8.6 1.3v.4h4.4v-.4c0-.5.2-1 .6-1.3A4.6 4.6 0 0 0 8 1.3Z"/><path d="M6.4 13.4h3.2M7 15h2"/>',
	important: '<path d="M2 2.6h12v8.6H8.4L5.4 14v-2.8H2Z"/><path d="M8 5v3.2M8 9.7v.6"/>',
	warning: '<path d="M8 1.7 15.2 14H.8Z"/><path d="M8 6.2v3.3M8 11.4v.6"/>',
	caution: '<path d="M5.3 1.3h5.4l3.9 3.9v5.4l-3.9 3.9H5.3l-3.9-3.9V5.2Z"/><path d="M8 4.6v3.6M8 10.7v.6"/>',
};

// A marker line (`> [!NOTE]`) followed by the rest of the blockquote.
const ALERT_RULE = new RegExp(
	`^ {0,3}> *\\[!(${KINDS.join('|')})\\] *(?:\\n|$)((?:> ?[^\\n]*(?:\\n|$))*)`,
	'i',
);

const ALERT_START = / {0,3}> *\[!/;

interface AlertToken extends Tokens.Generic {
	type: 'alert';
	kind: AlertKind;
}

function icon(kind: AlertKind): string {
	return (
		'<svg class="markdown-alert-icon" viewBox="0 0 16 16" width="16" height="16" aria-hidden="true" ' +
		'fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">' +
		`${ICONS[kind]}</svg>`
	);
}

export const markedAlerts: MarkedExtension = {
	extensions: [
		{
			name: 'alert',
			level: 'block',
			start(src: string) {
				return src.match(ALERT_START)?.index;
			},
			tokenizer(src: string) {
				const match = ALERT_RULE.exec(src);
				if (!match) {
					return undefined;
				}

				const [raw, kind, body] = match;
				// Strip the blockquote markers so the body lexes as normal markdown.
				const text = body.replace(/^ {0,3}> ?/gm, '').trim();
				const token: AlertToken = {
					type: 'alert',
					raw,
					kind: kind.toLowerCase() as AlertKind,
					text,
					tokens: [],
				};
				this.lexer.blockTokens(text, token.tokens);
				return token;
			},
			renderer(token) {
				const { kind } = token as AlertToken;
				const body = this.parser.parse(token.tokens ?? []);
				return (
					`<div class="markdown-alert markdown-alert-${kind}">` +
					`<p class="markdown-alert-title">${icon(kind)}${LABELS[kind]}</p>` +
					`${body}</div>\n`
				);
			},
		},
	],
};
