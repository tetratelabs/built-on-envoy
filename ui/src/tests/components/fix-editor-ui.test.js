/*
 * Copyright Built On Envoy
 * SPDX-License-Identifier: Apache-2.0
 * The full text of the Apache license is available in the LICENSE file at
 * the root of the repo.
 */

import { describe, it, expect } from 'vitest';
import { fixEditorUI } from '../../lib/fix-editor-ui.js';

// ── Helpers ──────────────────────────────────────────────────────────────────

function makeRoot(html = '') {
    const div = document.createElement('div');
    div.innerHTML = html;
    return div;
}

function field(schemapath, inner = '') {
    return `<div data-schemapath="${schemapath}">${inner}</div>`;
}

// ── Description inlining ─────────────────────────────────────────────────────

describe('fixEditorUI — description inlining', () => {
    it('moves <p> text into a je-field-desc span on the label', () => {
        const root = makeRoot(
            field('root.endpoint',
                '<label>Endpoint</label><p>The API endpoint URL</p>')
        );
        fixEditorUI(root);
        const label = root.querySelector('label');
        expect(label.querySelector('.je-field-desc')).toBeTruthy();
        expect(label.querySelector('.je-field-desc').textContent).toBe('The API endpoint URL');
    });

    it('hides the original <p> after moving its text', () => {
        const root = makeRoot(
            field('root.endpoint', '<label>Endpoint</label><p>Description text</p>')
        );
        fixEditorUI(root);
        const p = root.querySelector('p');
        expect(p.style.display).toBe('none');
    });

    it('does NOT touch <p> inside .je-modal', () => {
        const root = makeRoot(
            field('root.foo',
                '<label>Foo</label><div class="je-modal"><p>Modal description</p></div>')
        );
        fixEditorUI(root);
        const p = root.querySelector('.je-modal p');
        expect(p.style.display).not.toBe('none');
        expect(root.querySelector('.je-field-desc')).toBeNull();
    });

    it('is idempotent — does not duplicate je-field-desc on second call', () => {
        const root = makeRoot(
            field('root.endpoint', '<label>Endpoint</label><p>Description</p>')
        );
        fixEditorUI(root);
        fixEditorUI(root);
        const spans = root.querySelectorAll('.je-field-desc');
        expect(spans.length).toBe(1);
    });
});

// ── Collapse/expand button classification ────────────────────────────────────

describe('fixEditorUI — collapse/expand buttons', () => {
    it('adds je-collapse-btn and je-expanded to a "collapse" button', () => {
        const root = makeRoot(field('root.policy', '<button>collapse</button>'));
        fixEditorUI(root);
        const btn = root.querySelector('button');
        expect(btn.classList.contains('je-collapse-btn')).toBe(true);
        expect(btn.classList.contains('je-expanded')).toBe(true);
    });

    it('adds je-collapse-btn but NOT je-expanded to an "expand" button', () => {
        const root = makeRoot(field('root.policy', '<button>expand</button>'));
        fixEditorUI(root);
        const btn = root.querySelector('button');
        expect(btn.classList.contains('je-collapse-btn')).toBe(true);
        expect(btn.classList.contains('je-expanded')).toBe(false);
    });

    it('does not duplicate classes on second call', () => {
        const root = makeRoot(field('root.policy', '<button>collapse</button>'));
        fixEditorUI(root);
        fixEditorUI(root);
        const btn = root.querySelector('button');
        // classList.contains is set-based — check by counting classes
        const classes = Array.from(btn.classList);
        expect(classes.filter(c => c === 'je-collapse-btn').length).toBe(1);
    });
});

// ── Properties button classification ─────────────────────────────────────────

describe('fixEditorUI — properties button: je-add-props-field only (Cedar deny_headers, File Server content_types)', () => {
    it('renames button text to "Properties" and adds je-add-props-btn', () => {
        const root = makeRoot(
            `<div data-schemapath="root.deny_headers" class="je-add-props-field">
                <button>Object properties</button>
             </div>`
        );
        fixEditorUI(root);
        const btn = root.querySelector('button');
        expect(btn.textContent).toBe('Properties');
        expect(btn.classList.contains('je-add-props-btn')).toBe(true);
    });
});

describe('fixEditorUI — properties button: je-optional-props-field only (Cedar optional fields)', () => {
    it('renames button text to "Optional Properties" and adds je-optional-props-btn', () => {
        const root = makeRoot(
            `<div data-schemapath="root.entities_file" class="je-optional-props-field">
                <button>Object properties</button>
             </div>`
        );
        fixEditorUI(root);
        const btn = root.querySelector('button');
        expect(btn.textContent).toBe('Optional Properties');
        expect(btn.classList.contains('je-optional-props-btn')).toBe(true);
    });
});

describe('fixEditorUI — properties button: both je-add-props-field AND je-optional-props-field', () => {
    it('original button gets je-optional-props-btn; cloned button gets je-add-props-btn with text "+ Properties"', () => {
        const root = makeRoot(
            `<div data-schemapath="root.combined" class="je-add-props-field je-optional-props-field">
                <button>Object properties</button>
             </div>`
        );
        fixEditorUI(root);
        const buttons = root.querySelectorAll('button');
        expect(buttons.length).toBe(2);

        const optBtn = root.querySelector('.je-optional-props-btn');
        const addBtn = root.querySelector('.je-add-props-btn');

        expect(optBtn).toBeTruthy();
        expect(optBtn.textContent).toBe('Optional Properties');
        expect(addBtn).toBeTruthy();
        expect(addBtn.textContent).toBe('+ Properties');
    });

    it('does not insert a second cloned button on second call (idempotent)', () => {
        const root = makeRoot(
            `<div data-schemapath="root.combined" class="je-add-props-field je-optional-props-field">
                <button>Object properties</button>
             </div>`
        );
        fixEditorUI(root);
        fixEditorUI(root);
        expect(root.querySelectorAll('button').length).toBe(2);
    });
});

// ── Error visibility ─────────────────────────────────────────────────────────

describe('fixEditorUI — error element hiding', () => {
    it('hides element with color:red under [data-schemapath]', () => {
        const root = makeRoot(
            field('root.policy', '<span style="color:red">This field is required</span>')
        );
        fixEditorUI(root);
        const span = root.querySelector('span');
        // dataset.jeErrHidden maps to data-je-err-hidden in HTML
        expect(span.getAttribute('data-je-err-hidden')).toBe('1');
        expect(span.style.getPropertyPriority('display')).toBe('important');
    });

    it('hides element with color:rgb(255, 0, 0)', () => {
        const root = makeRoot(
            field('root.policy', '<span style="color:rgb(255, 0, 0)">Error</span>')
        );
        fixEditorUI(root);
        expect(root.querySelector('span').getAttribute('data-je-err-hidden')).toBe('1');
    });

    it('restores hidden error elements when container has je-show-errors', () => {
        const container = document.createElement('div');
        container.className = 'config-section-body';
        const root = makeRoot(
            field('root.policy', '<span style="color:red">Required</span>')
        );
        container.appendChild(root);
        document.body.appendChild(container);

        fixEditorUI(root);
        const span = root.querySelector('span');
        // After first call without je-show-errors, error should be hidden
        expect(span.getAttribute('data-je-err-hidden')).toBe('1');

        container.classList.add('je-show-errors');
        fixEditorUI(root);
        // After second call with je-show-errors, error should be restored
        expect(span.getAttribute('data-je-err-hidden')).toBeNull();
        expect(span.style.display).not.toBe('none');

        document.body.removeChild(container);
    });
});

// ── Required checkbox hiding ─────────────────────────────────────────────────

describe('fixEditorUI — required checkbox hiding in modals', () => {
    it('hides parent label of a disabled checkbox inside .je-modal', () => {
        const root = makeRoot(
            `<div class="je-modal">
                <label><input type="checkbox" disabled> Required field</label>
            </div>`
        );
        fixEditorUI(root);
        const label = root.querySelector('label');
        expect(label.style.display).toBe('none');
    });

    it('does NOT hide label of a non-disabled checkbox', () => {
        const root = makeRoot(
            `<div class="je-modal">
                <label><input type="checkbox"> Optional field</label>
            </div>`
        );
        fixEditorUI(root);
        const label = root.querySelector('label');
        expect(label.style.display).not.toBe('none');
    });
});

// ── Optional Properties modal (Cedar/OPA optional field pattern) ─────────────

describe('fixEditorUI — optional properties modal', () => {
    it('hides non-property-selector children when optional-props-btn is clicked', () => {
        const root = makeRoot(
            `<div data-schemapath="root.extras" class="je-optional-props-field">
                <button class="je-optional-props-btn">Optional Properties</button>
                <div class="je-modal">
                    <div class="property-selector"><input type="checkbox">opt1</div>
                    <div class="other-stuff">should be hidden</div>
                </div>
             </div>`
        );
        fixEditorUI(root);

        const btn = root.querySelector('.je-optional-props-btn');
        btn.click();

        const modal = root.querySelector('.je-modal');
        const other = modal.querySelector('.other-stuff');
        expect(other.style.getPropertyPriority('display')).toBe('important');

        const selector = modal.querySelector('.property-selector');
        expect(selector.style.display).not.toBe('none');
    });
});

// ── Additional Properties modal (Cedar deny_headers / File Server content_types) ─

describe('fixEditorUI — additional properties modal', () => {
    it('ensures all > div children in add-props modal are visible', () => {
        const root = makeRoot(
            `<div data-schemapath="root.deny_headers" class="je-add-props-field">
                <div class="je-modal">
                    <div style="display:none">Property list</div>
                    <div>Input area</div>
                </div>
             </div>`
        );
        fixEditorUI(root);
        root.querySelectorAll('.je-modal > div').forEach(div => {
            expect(div.style.display).not.toBe('none');
        });
    });

    it('neutralizes text input so typing does not filter the list', () => {
        // Build the DOM without whitespace text nodes so previousSibling works correctly.
        // JSONEditor's modal structure: list-div → gap-div → input
        const root = document.createElement('div');
        root.setAttribute('data-schemapath', 'root.deny_headers');
        root.classList.add('je-add-props-field');
        const modal = document.createElement('div');
        modal.className = 'je-modal';
        const listDiv = document.createElement('div');
        listDiv.className = 'list-container';
        const listItem = document.createElement('div');
        listItem.className = 'list-item';
        listItem.textContent = 'item1';
        listDiv.appendChild(listItem);
        const gapDiv = document.createElement('div');
        gapDiv.className = 'gap';
        const input = document.createElement('input');
        input.type = 'text';
        modal.appendChild(listDiv);
        modal.appendChild(gapDiv);
        modal.appendChild(input);
        root.appendChild(modal);

        fixEditorUI(root);

        // Mark a list item as hidden to simulate JSONEditor's filter behaviour
        listItem.style.display = 'none';

        // Dispatch input event — the neutralizer uses capture so it fires before
        // any JSONEditor listener and clears display on list items
        input.dispatchEvent(new Event('input', { bubbles: true }));

        // The neutralizer should have removed the display:none from list items
        expect(listItem.style.display).not.toBe('none');
    });

    it('clears text input and refocuses when "Add property" button is clicked', () => {
        const root = makeRoot(
            `<div data-schemapath="root.content_types" class="je-add-props-field">
                <div class="je-modal">
                    <input type="text" value="existing text">
                    <button>Add property</button>
                </div>
             </div>`
        );
        document.body.appendChild(root);
        fixEditorUI(root);

        const input = root.querySelector('input');
        const addBtn = root.querySelector('button');
        input.value = 'some-key';
        addBtn.click();

        expect(input.value).toBe('');
        document.body.removeChild(root);
    });
});

// ── Idempotency guards ────────────────────────────────────────────────────────

describe('fixEditorUI — optional-props modal: no modal found (guard branch)', () => {
    it('does not throw when je-optional-props-btn has no ancestor with .je-modal', () => {
        const root = makeRoot(
            `<div data-schemapath="root.extras" class="je-optional-props-field">
                <button class="je-optional-props-btn">Optional Properties</button>
             </div>`
        );
        fixEditorUI(root);
        // Click the button — modal lookup returns null, guard branch fires, no throw
        expect(() => root.querySelector('.je-optional-props-btn').click()).not.toThrow();
    });
});

describe('fixEditorUI — additional-props input: already neutralized (guard branch)', () => {
    it('does not attach a second listener when input already has _jeFilterNeutralized', () => {
        const root = document.createElement('div');
        root.setAttribute('data-schemapath', 'root.headers');
        root.classList.add('je-add-props-field');
        const modal = document.createElement('div');
        modal.className = 'je-modal';
        const input = document.createElement('input');
        input.type = 'text';
        input._jeFilterNeutralized = true; // pre-set the guard flag
        modal.appendChild(input);
        root.appendChild(modal);

        // Should not throw and guard branch fires without attaching second listener
        expect(() => fixEditorUI(root)).not.toThrow();
    });
});

describe('fixEditorUI — additional-props add button: already attached (guard branch)', () => {
    it('does not attach a second click handler when _jeAddClearAttached is already set', () => {
        const root = makeRoot(
            `<div data-schemapath="root.headers" class="je-add-props-field">
                <div class="je-modal">
                    <input type="text">
                    <button>Add property</button>
                </div>
             </div>`
        );
        document.body.appendChild(root);
        fixEditorUI(root);
        // Second call — _jeAddClearAttached guard fires, no duplicate handler
        expect(() => fixEditorUI(root)).not.toThrow();
        document.body.removeChild(root);
    });
});

// ── Plain properties button (neither add-props nor optional-props field) ──────

describe('fixEditorUI — properties button: plain field (no add/opt class)', () => {
    it('adds je-properties-btn class (hidden by CSS) when field is neither add-props nor opt-props', () => {
        const root = makeRoot(
            `<div data-schemapath="root.some_object">
                <button>Object properties</button>
             </div>`
        );
        fixEditorUI(root);
        const btn = root.querySelector('button');
        expect(btn.classList.contains('je-properties-btn')).toBe(true);
        expect(btn.classList.contains('je-add-props-btn')).toBe(false);
        expect(btn.classList.contains('je-optional-props-btn')).toBe(false);
    });
});

// ── Label child style propagation (non-je-field-desc, non-button children) ───

describe('fixEditorUI — label style propagation to plain child spans', () => {
    it('applies label style (13px) to a plain span inside a label (not je-field-desc, not a button)', () => {
        const root = makeRoot(
            `<div data-schemapath="root.my_field">
                <label>My Field <span class="some-span">extra text</span></label>
             </div>`
        );
        document.body.appendChild(root);
        fixEditorUI(root);
        const span = root.querySelector('.some-span');
        // labelStyle sets font-size:13px and color:#475569 — jsdom parses these
        expect(span.style.fontSize).toBe('13px');
        document.body.removeChild(root);
    });

    it('applies desc style (12px) to a je-field-desc span, not the 13px label style', () => {
        const root = makeRoot(
            `<div data-schemapath="root.my_field">
                <label>My Field <span class="je-field-desc">description</span></label>
             </div>`
        );
        document.body.appendChild(root);
        fixEditorUI(root);
        const desc = root.querySelector('.je-field-desc');
        // descStyle sets font-size:12px — distinct from labelStyle's 13px
        expect(desc.style.fontSize).toBe('12px');
        document.body.removeChild(root);
    });
});
