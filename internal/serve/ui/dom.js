// dom.js — the only module that creates elements.
//
// Entity text is attacker-influenceable (khub's `get`/`search` output is
// untrusted by construction), so every string that reaches the document does it
// through textContent, and it does it here. Concentrating element CREATION into
// these helpers is what makes that structural rather than a rule people
// remember: no other module has a reason to build or fill a node.
//
// Reading the document is not restricted — app.js resolves its element handles
// with getElementById and reads the token sheet with getComputedStyle. What
// must not spread is writing.
//
// The build gate in internal/serve/serve_test.go scans this tree for the
// markup-parsing sinks and fails on any of them. It is a strict literal match
// with no comment stripping, which is why this note names none of them.

// el builds one element. `attrs` sets properties, never a style attribute:
// CSP's `style-src 'self'` blocks the style ATTRIBUTE, while CSSOM property
// setters are not CSP-governed — so pass `style` as an object and it is applied
// property by property.
export function el(tag, attrs, textContent) {
  var node = document.createElement(tag);
  if (attrs) {
    Object.keys(attrs).forEach(function (k) {
      var v = attrs[k];
      if (v === null || v === undefined || v === false) return;
      if (k === 'style') {
        Object.keys(v).forEach(function (prop) { node.style[prop] = v[prop]; });
      } else if (k === 'class') {
        node.className = v;
      } else if (k === 'on') {
        Object.keys(v).forEach(function (evt) { node.addEventListener(evt, v[evt]); });
      } else if (k in node) {
        node[k] = v;
      } else {
        node.setAttribute(k, v);
      }
    });
  }
  if (textContent !== undefined && textContent !== null) node.textContent = String(textContent);
  return node;
}

// text is the only sanctioned way to set a node's text outside this module.
export function text(node, value) { node.textContent = value === null || value === undefined ? '' : String(value); }

export function clear(node) {
  while (node.firstChild) node.removeChild(node.firstChild);
}

// fill replaces a container's contents in one pass.
export function fill(node, children) {
  clear(node);
  children.forEach(function (c) { if (c) node.appendChild(c); });
}

// kv is the fact row the inspector is built from: mono key, value beside it.
export function kv(key, value) {
  var row = el('div', { class: 'kv' });
  row.appendChild(el('span', { class: 'k' }, key));
  row.appendChild(el('span', { class: 'v' }, value));
  return row;
}
