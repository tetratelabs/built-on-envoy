/*
 * Copyright Built On Envoy
 * SPDX-License-Identifier: Apache-2.0
 * The full text of the Apache license is available in the LICENSE file at
 * the root of the repo.
 */

import { describe, it, expect, vi } from 'vitest';
import { render, fireEvent } from '@testing-library/svelte';
import OrderPopover from '../../components/OrderPopover.svelte';

const makeOrder = () => ['cedar', 'opa', 'file-server'];

describe('OrderPopover', () => {
    it('renders with given title', () => {
        const { container } = render(OrderPopover, {
            props: { title: 'Run Order', order: makeOrder(), reorderFn: vi.fn() },
        });
        expect(container.querySelector('.order-popover-title').textContent).toBe('Run Order');
    });

    it('renders all order items', () => {
        const { container } = render(OrderPopover, {
            props: { title: 'Run Order', order: makeOrder(), reorderFn: vi.fn() },
        });
        const items = container.querySelectorAll('.order-popover-item');
        expect(items.length).toBe(3);
        const names = [...items].map(i => i.querySelector('.order-popover-name').textContent);
        expect(names).toEqual(['cedar', 'opa', 'file-server']);
    });

    it('renders drag handles', () => {
        const { container } = render(OrderPopover, {
            props: { title: 'Run Order', order: makeOrder(), reorderFn: vi.fn() },
        });
        const handles = container.querySelectorAll('.order-popover-handle');
        expect(handles.length).toBe(3);
    });

    it('items are draggable', () => {
        const { container } = render(OrderPopover, {
            props: { title: 'Run Order', order: makeOrder(), reorderFn: vi.fn() },
        });
        const items = container.querySelectorAll('.order-popover-item');
        for (const item of items) {
            expect(item.getAttribute('draggable')).toBe('true');
        }
    });

    it('calls reorderFn when item is dragged and dropped onto another', async () => {
        const reorderFn = vi.fn();
        const { container } = render(OrderPopover, {
            props: { title: 'Run Order', order: makeOrder(), reorderFn },
        });
        const items = container.querySelectorAll('.order-popover-item');

        // Drag item at index 0 (cedar) over item at index 2 (file-server)
        await fireEvent.dragStart(items[0]);
        await fireEvent.dragOver(items[2]);
        await fireEvent.drop(items[2]);

        expect(reorderFn).toHaveBeenCalledWith(0, 2);
    });

    it('does not call reorderFn when dropped on self', async () => {
        const reorderFn = vi.fn();
        const { container } = render(OrderPopover, {
            props: { title: 'Run Order', order: makeOrder(), reorderFn },
        });
        const items = container.querySelectorAll('.order-popover-item');

        await fireEvent.dragStart(items[1]);
        await fireEvent.dragOver(items[1]);
        await fireEvent.drop(items[1]);

        expect(reorderFn).not.toHaveBeenCalled();
    });

    it('adds dragging class to item being dragged', async () => {
        const { container } = render(OrderPopover, {
            props: { title: 'Run Order', order: makeOrder(), reorderFn: vi.fn() },
        });
        const items = container.querySelectorAll('.order-popover-item');
        await fireEvent.dragStart(items[0]);
        expect(items[0].classList.contains('dragging')).toBe(true);
    });

    it('removes dragging class on dragend', async () => {
        const { container } = render(OrderPopover, {
            props: { title: 'Run Order', order: makeOrder(), reorderFn: vi.fn() },
        });
        const items = container.querySelectorAll('.order-popover-item');
        await fireEvent.dragStart(items[0]);
        await fireEvent.dragEnd(items[0]);
        expect(items[0].classList.contains('dragging')).toBe(false);
    });

    it('adds drag-over class when dragging over another item', async () => {
        const { container } = render(OrderPopover, {
            props: { title: 'Run Order', order: makeOrder(), reorderFn: vi.fn() },
        });
        const items = container.querySelectorAll('.order-popover-item');
        await fireEvent.dragStart(items[0]);
        await fireEvent.dragOver(items[2]);
        expect(items[2].classList.contains('drag-over')).toBe(true);
    });

    it('removes drag-over class on dragleave', async () => {
        const { container } = render(OrderPopover, {
            props: { title: 'Run Order', order: makeOrder(), reorderFn: vi.fn() },
        });
        const items = container.querySelectorAll('.order-popover-item');
        await fireEvent.dragStart(items[0]);
        await fireEvent.dragOver(items[2]);
        await fireEvent.dragLeave(items[2]);
        expect(items[2].classList.contains('drag-over')).toBe(false);
    });

    it('calls onclose on Escape key', async () => {
        const onclose = vi.fn();
        const { container } = render(OrderPopover, {
            props: { title: 'Run Order', order: makeOrder(), reorderFn: vi.fn(), onclose },
        });
        const popover = container.querySelector('.order-popover');
        await fireEvent.keyDown(popover, { key: 'Escape' });
        expect(onclose).toHaveBeenCalledOnce();
    });

    it('renders empty list when order is empty', () => {
        const { container } = render(OrderPopover, {
            props: { title: 'Run Order', order: [], reorderFn: vi.fn() },
        });
        expect(container.querySelectorAll('.order-popover-item').length).toBe(0);
    });
});
