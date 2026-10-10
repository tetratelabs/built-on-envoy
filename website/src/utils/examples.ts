// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// This parser opts generated examples into separate command/output rendering without treating
// arbitrary legacy code strings as Markdown or HTML.
export interface ExampleCodeBlock {
	kind: 'command' | 'output';
	language: 'shell' | 'plaintext';
	code: string;
}

const metadataClauses = [
	/^Line endings: (?:CRLF|LF)(?:,(?:CRLF|LF))*$/,
	/^Bare CR offsets: \d+(?:,\d+)*$/,
	/^Trailing whitespace: [A-Za-z0-9+/]+={0,2}$/,
	/^No trailing newline$/,
];

export function parseExampleCode(code: string): ExampleCodeBlock[] {
	const blocks: ExampleCodeBlock[] = [];
	let cursor = 0;
	let mayReadMetadata = false;

	while (cursor < code.length) {
		cursor = skipWhitespace(code, cursor);
		if (cursor === code.length) {
			return blocks.length > 0 ? blocks : [legacyShellBlock(code)];
		}

		const lineEnd = code.indexOf('\n', cursor);
		const lineStop = lineEnd === -1 ? code.length : lineEnd;
		const line = code.slice(cursor, lineStop).replace(/\r$/, '');
		if (line.startsWith('<!--')) {
			if (!mayReadMetadata || !isGeneratorMetadata(line)) {
				return [legacyShellBlock(code)];
			}
			mayReadMetadata = false;
			cursor = lineEnd === -1 ? code.length : lineEnd + 1;
			continue;
		}

		const opening = /^(`{3,})(sh|text)[ \t]*\r?\n/.exec(code.slice(cursor));
		if (!opening) {
			return [legacyShellBlock(code)];
		}
		const fence = opening[1];
		const language = opening[2] === 'sh' ? 'shell' : 'plaintext';
		const contentStart = cursor + opening[0].length;
		const closing = findClosingFence(code, contentStart, fence.length);
		if (!closing) {
			return [legacyShellBlock(code)];
		}

		blocks.push({
			kind: language === 'shell' ? 'command' : 'output',
			language,
			code: code.slice(contentStart, closing.start),
		});
		mayReadMetadata = language === 'plaintext';
		cursor = closing.next;
	}

	return blocks.length > 0 ? blocks : [legacyShellBlock(code)];
}

function isGeneratorMetadata(line: string): boolean {
	if (!line.startsWith('<!-- ') || !line.endsWith(' -->')) {
		return false;
	}
	const [stream, ...clauses] = line.slice(5, -4).split('; ');
	if (stream !== 'stdout' && stream !== 'stderr') {
		return false;
	}
	let previousClause = -1;
	for (const clause of clauses) {
		const clauseIndex = metadataClauses.findIndex(pattern => pattern.test(clause));
		if (clauseIndex <= previousClause) {
			return false;
		}
		previousClause = clauseIndex;
	}
	return true;
}

function legacyShellBlock(code: string): ExampleCodeBlock {
	return { kind: 'command', language: 'shell', code: code.trim() };
}

function skipWhitespace(value: string, start: number): number {
	let cursor = start;
	while (cursor < value.length && /\s/.test(value[cursor])) {
		cursor++;
	}
	return cursor;
}

function findClosingFence(value: string, start: number, minimumLength: number): { start: number; next: number } | null {
	let cursor = start;
	while (cursor <= value.length) {
		const lineEnd = value.indexOf('\n', cursor);
		const lineStop = lineEnd === -1 ? value.length : lineEnd;
		const line = value.slice(cursor, lineStop).replace(/\r$/, '');
		const closing = /^(`+)[ \t]*$/.exec(line);
		if (closing && closing[1].length >= minimumLength) {
			return { start: cursor, next: lineEnd === -1 ? value.length : lineEnd + 1 };
		}
		if (lineEnd === -1) {
			break;
		}
		cursor = lineEnd + 1;
	}
	return null;
}
