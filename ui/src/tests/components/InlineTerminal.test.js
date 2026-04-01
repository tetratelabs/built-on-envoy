/*
 * Copyright Built On Envoy
 * SPDX-License-Identifier: Apache-2.0
 * The full text of the Apache license is available in the LICENSE file at
 * the root of the repo.
 */

import { describe, it, expect } from 'vitest';
import { render } from '@testing-library/svelte';
import InlineTerminal from '../../components/InlineTerminal.svelte';

describe('InlineTerminal', () => {
    it('renders an empty terminal div', () => {
        const { container } = render(InlineTerminal);
        const terminal = container.querySelector('.terminal');
        expect(terminal).toBeTruthy();
        expect(terminal.children.length).toBe(0);
    });

    it('appendLine adds a terminal-line div', async () => {
        const { component, container } = render(InlineTerminal);
        component.appendLine('hello world');
        const lines = container.querySelectorAll('.terminal-line');
        expect(lines.length).toBe(1);
        expect(lines[0].innerHTML).toBe('hello world');
    });

    it('appendLine with class adds the extra CSS class', async () => {
        const { component, container } = render(InlineTerminal);
        component.appendLine('Error occurred', 'terminal-error');
        const line = container.querySelector('.terminal-line');
        expect(line.classList.contains('terminal-error')).toBe(true);
    });

    it('appendLine without class has no extra class', () => {
        const { component, container } = render(InlineTerminal);
        component.appendLine('Normal output');
        const line = container.querySelector('.terminal-line');
        expect(line.className).toBe('terminal-line');
    });

    it('multiple appendLine calls accumulate lines in order', () => {
        const { component, container } = render(InlineTerminal);
        component.appendLine('line 1');
        component.appendLine('line 2');
        component.appendLine('line 3');
        const lines = container.querySelectorAll('.terminal-line');
        expect(lines.length).toBe(3);
        expect(lines[0].innerHTML).toBe('line 1');
        expect(lines[2].innerHTML).toBe('line 3');
    });

    it('clear() removes all terminal lines', () => {
        const { component, container } = render(InlineTerminal);
        component.appendLine('line 1');
        component.appendLine('line 2');
        component.clear();
        expect(container.querySelectorAll('.terminal-line').length).toBe(0);
    });

    it('getLines() returns current lines as {html, cls} objects', () => {
        const { component } = render(InlineTerminal);
        component.appendLine('<span>output</span>', 'terminal-status');
        component.appendLine('plain', '');
        const lines = component.getLines();
        expect(lines.length).toBe(2);
        expect(lines[0].html).toBe('<span>output</span>');
        expect(lines[0].cls).toBe('terminal-status');
        expect(lines[1].cls).toBe('');
    });

    it('setLines() replaces terminal content', () => {
        const { component, container } = render(InlineTerminal);
        component.appendLine('old line');
        component.setLines([
            { html: 'new line 1', cls: '' },
            { html: 'new line 2', cls: 'terminal-error' },
        ]);
        const lines = container.querySelectorAll('.terminal-line');
        expect(lines.length).toBe(2);
        expect(lines[0].innerHTML).toBe('new line 1');
        expect(lines[1].classList.contains('terminal-error')).toBe(true);
    });

});
