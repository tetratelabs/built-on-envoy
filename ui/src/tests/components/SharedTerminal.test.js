/*
 * Copyright Built On Envoy
 * SPDX-License-Identifier: Apache-2.0
 * The full text of the Apache license is available in the LICENSE file at
 * the root of the repo.
 */

import { describe, it, expect, vi } from 'vitest';
import { render, fireEvent } from '@testing-library/svelte';
import SharedTerminal from '../../components/SharedTerminal.svelte';

describe('SharedTerminal', () => {
    it('renders with the given title', () => {
        const { container } = render(SharedTerminal, { props: { title: 'Run: cedar' } });
        expect(container.querySelector('.run-all-panel-title').textContent).toBe('Run: cedar');
    });

    it('renders fixed-position panel', () => {
        const { container } = render(SharedTerminal);
        expect(container.querySelector('.run-all-panel')).toBeTruthy();
    });

    it('appendLine adds a line to the terminal', () => {
        const { component, container } = render(SharedTerminal);
        component.appendLine('hello');
        expect(container.querySelectorAll('.terminal-line').length).toBe(1);
    });

    it('clear() removes all lines', () => {
        const { component, container } = render(SharedTerminal);
        component.appendLine('line 1');
        component.clear();
        expect(container.querySelectorAll('.terminal-line').length).toBe(0);
    });

    it('minimize button toggles minimized class', async () => {
        const { container } = render(SharedTerminal);
        const panel = container.querySelector('.run-all-panel');
        const minimizeBtn = container.querySelector('.run-all-minimize');

        expect(panel.classList.contains('minimized')).toBe(false);
        await fireEvent.click(minimizeBtn);
        expect(panel.classList.contains('minimized')).toBe(true);
        await fireEvent.click(minimizeBtn);
        expect(panel.classList.contains('minimized')).toBe(false);
    });

    it('calls onclose when close button is clicked', async () => {
        const onclose = vi.fn();
        const { container } = render(SharedTerminal, { props: { onclose } });
        const closeBtn = container.querySelector('.btn-secondary');
        await fireEvent.click(closeBtn);
        expect(onclose).toHaveBeenCalledOnce();
    });

    it('does not render close button when onclose is not provided', () => {
        const { container } = render(SharedTerminal);
        expect(container.querySelector('.btn-secondary')).toBeNull();
    });

    it('setLines replaces content', () => {
        const { component, container } = render(SharedTerminal);
        component.appendLine('old');
        component.setLines([{ html: 'new', cls: 'terminal-status' }]);
        const lines = container.querySelectorAll('.terminal-line');
        expect(lines.length).toBe(1);
        expect(lines[0].classList.contains('terminal-status')).toBe(true);
    });

    it('getLines() returns lines as {html, cls} objects', () => {
        const { component } = render(SharedTerminal);
        component.appendLine('hello', 'terminal-status');
        component.appendLine('world', '');
        const lines = component.getLines();
        expect(lines).toHaveLength(2);
        expect(lines[0].html).toBe('hello');
        expect(lines[0].cls).toBe('terminal-status');
        expect(lines[1].html).toBe('world');
        expect(lines[1].cls).toBe('');
    });

    it('getLines() returns empty array when terminal is empty', () => {
        const { component } = render(SharedTerminal);
        expect(component.getLines()).toEqual([]);
    });

    it('shows Stop button when running prop is true', () => {
        const { container } = render(SharedTerminal, { props: { running: true, onstop: () => {} } });
        expect(container.querySelector('.btn-danger')).toBeTruthy();
        expect(container.querySelector('.btn-secondary')).toBeNull();
    });

    it('calls onstop when stop button is clicked', async () => {
        const onstop = vi.fn();
        const { container } = render(SharedTerminal, { props: { running: true, onstop } });
        await fireEvent.click(container.querySelector('.btn-danger'));
        expect(onstop).toHaveBeenCalledOnce();
    });

    it('shows popin button when onpopin prop is provided', () => {
        const { container } = render(SharedTerminal, { props: { onpopin: () => {} } });
        expect(container.querySelector('.run-all-popin')).toBeTruthy();
    });

    it('calls onpopin when popin button is clicked', async () => {
        const onpopin = vi.fn();
        const { container } = render(SharedTerminal, { props: { onpopin } });
        await fireEvent.click(container.querySelector('.run-all-popin'));
        expect(onpopin).toHaveBeenCalledOnce();
    });
});
