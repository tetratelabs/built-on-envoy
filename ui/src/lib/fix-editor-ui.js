/*
 * Copyright Built On Envoy
 * SPDX-License-Identifier: Apache-2.0
 * The full text of the Apache license is available in the LICENSE file at
 * the root of the repo.
 */

// BOE Extension Manager — JSONEditor UI Fixup
//
// Pure DOM function that corrects JSONEditor's generated HTML to match the
// application's design system. Called after editor ready and on every DOM
// mutation via MutationObserver. Has no Svelte dependencies — can be imported
// directly in unit tests without any component rendering.

/**
 * Apply all UI corrections to a json-editor root element.
 * @param {HTMLElement} root
 */
export function fixEditorUI(root) {
    // 0. Hide inline color:red error elements unless je-show-errors is active
    const container = root.closest('.config-section-body') || root;
    if (!container.classList.contains('je-show-errors')) {
        root.querySelectorAll('[data-schemapath] > *').forEach(el => {
            const c = el.style && el.style.color;
            if (c && (c === 'red' || c.startsWith('rgb(255, 0') || c.startsWith('rgb(255,0'))) {
                el.setAttribute('data-je-err-hidden', '1');
                el.style.setProperty('display', 'none', 'important');
            }
        });
    } else {
        root.querySelectorAll('[data-je-err-hidden]').forEach(el => {
            el.style.removeProperty('display');
            el.removeAttribute('data-je-err-hidden');
        });
    }

    // 1. Inline descriptions: move <p> text next to the nearest label/heading
    root.querySelectorAll('p').forEach(p => {
        if (p.dataset.jeFixed) return;
        const text = p.textContent.trim();
        if (!text) return;
        if (p.closest('.je-modal') || p.classList.contains('je-error')) return;

        const field = p.closest('[data-schemapath]');
        if (!field) return;

        let target = null;
        const candidates = field.querySelectorAll(':scope > label, :scope > h3, :scope > h4, :scope > h5, :scope > h6, :scope > div > label, :scope > div > h3, :scope > div > h4');
        for (const c of candidates) {
            if (!c.querySelector('.je-field-desc')) { target = c; break; }
        }

        if (target) {
            const descSpan = document.createElement('span');
            descSpan.className = 'je-field-desc';
            descSpan.textContent = text;
            target.appendChild(descSpan);
        }
        p.dataset.jeFixed = '1';
        p.style.display = 'none';
    });

    // 2. Classify collapse/expand and properties buttons
    root.querySelectorAll('button').forEach(btn => {
        const text = btn.textContent.trim().toLowerCase();
        if (text === 'collapse' || text === 'expand') {
            btn.classList.add('je-collapse-btn');
            btn.classList.toggle('je-expanded', text === 'collapse');
        } else if (text === 'object properties' || text === 'properties') {
            if (!btn.classList.contains('je-properties-btn') &&
                !btn.classList.contains('je-add-props-btn') &&
                !btn.classList.contains('je-optional-props-btn')) {
                const immediateField = btn.closest('[data-schemapath]');
                const inAdd = immediateField?.classList.contains('je-add-props-field') ? immediateField : null;
                const inOpt = immediateField?.classList.contains('je-optional-props-field') ? immediateField : null;
                if (inAdd && inOpt) {
                    if (!btn.closest('[data-schemapath]').querySelector('.je-add-props-btn')) {
                        const addBtn = btn.cloneNode(true);
                        addBtn.textContent = '+ Properties';
                        addBtn.classList.add('je-add-props-btn');
                        btn.after(addBtn);
                    }
                    btn.textContent = 'Optional Properties';
                    btn.classList.add('je-optional-props-btn');
                } else if (inAdd) {
                    btn.textContent = 'Properties';
                    btn.classList.add('je-add-props-btn');
                } else if (inOpt) {
                    btn.textContent = 'Optional Properties';
                    btn.classList.add('je-optional-props-btn');
                } else {
                    btn.classList.add('je-properties-btn');
                }
            }
        }
    });

    // 3. Force consistent styles on all labels and headings
    const labelStyle = 'font-family:-apple-system,BlinkMacSystemFont,Segoe UI,Roboto,sans-serif;font-size:13px;font-weight:600;color:#475569';
    const descStyle = 'font-weight:400;font-size:12px;color:#94a3b8';
    root.querySelectorAll('label, h3, h4, h5, h6').forEach(el => {
        if (el.closest('.je-modal')) return;
        el.style.cssText += ';' + labelStyle;
        el.querySelectorAll('*').forEach(child => {
            if (child.classList.contains('je-field-desc')) {
                child.style.cssText += ';' + descStyle;
            } else if (child.tagName !== 'BUTTON' && child.tagName !== 'SVG' &&
                       child.tagName !== 'INPUT' && child.tagName !== 'path') {
                child.style.cssText += ';' + labelStyle;
            }
        });
    });

    // 4. Hide required (disabled) checkboxes in properties modals
    root.querySelectorAll('.je-modal input[type="checkbox"]').forEach(cb => {
        if (cb.disabled || cb.hasAttribute('disabled')) {
            const label = cb.closest('label');
            if (label) label.style.display = 'none';
        }
    });

    // 5a. Optional Properties button: hide everything in modal except checkboxes
    root.querySelectorAll('.je-optional-props-btn').forEach(btn => {
        if (btn._jeOptModalFixed) return;
        btn._jeOptModalFixed = true;
        btn.addEventListener('click', () => {
            const field = btn.closest('[data-schemapath]');
            if (!field || field.classList.contains('je-add-props-field')) return;
            const modal = [...field.querySelectorAll('.je-modal')]
                .find(m => m.querySelector('.property-selector'));
            if (!modal) return;
            [...modal.children].forEach(child => {
                if (!child.classList.contains('property-selector')) {
                    child.style.setProperty('display', 'none', 'important');
                }
            });
        });
    });

    // 5b. Additional Properties modal: keep input and checkbox list both visible
    root.querySelectorAll('.je-add-props-field .je-modal').forEach(modal => {
        modal.querySelectorAll(':scope > div').forEach(div => {
            div.style.removeProperty('display');
        });
        modal.querySelectorAll('input[type="text"]').forEach(input => {
            if (input._jeFilterNeutralized) return;
            input._jeFilterNeutralized = true;
            input.addEventListener('input', e => {
                e.stopImmediatePropagation();
                const list = input.previousSibling && input.previousSibling.previousSibling;
                if (list) {
                    list.childNodes.forEach(item => {
                        if (item.style) item.style.removeProperty('display');
                    });
                }
            }, true);
            input.focus();
        });
        modal.querySelectorAll('button').forEach(btn => {
            if (btn._jeAddClearAttached) return;
            if (btn.textContent.trim().toLowerCase() === 'add property' ||
                btn.textContent.trim().toLowerCase() === 'add') {
                btn._jeAddClearAttached = true;
                btn.addEventListener('click', () => {
                    const input = modal.querySelector('input[type="text"]');
                    if (input) { input.value = ''; input.focus(); }
                });
            }
        });
    });
}
