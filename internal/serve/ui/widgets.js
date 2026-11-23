// widgets.js — small pieces several views share, all built through dom.js.

import { el } from './dom.js';

var MARKER_TEXT = {
  draft: 'draft', stale: 'stale', orphan: 'orphan', incomplete: 'incomplete'
};

// markerTags renders the status markers that are set, plus the trust tier when
// it is anything but unverified. An empty result is an empty span, so a
// table cell keeps its height.
export function markerTags(m, withTrust) {
  var wrap = el('span', { class: 'tags' });
  if (!m) return wrap;
  ['draft', 'incomplete', 'orphan', 'stale'].forEach(function (k) {
    if (m[k]) wrap.appendChild(el('span', { class: 'tag tag-' + k, title: MARKER_TEXT[k] }, MARKER_TEXT[k]));
  });
  if (withTrust && m.trust && m.trust !== 'unverified') {
    wrap.appendChild(el('span', { class: 'tag tag-trust', title: 'review tier' }, m.trust));
  }
  return wrap;
}

export function trustTag(trust) {
  return el('span', { class: 'trust trust-' + (trust || 'unverified') }, trust || 'unverified');
}

// notice is the full-width message a view shows instead of its content.
export function notice(title, detail, links) {
  var box = el('div', { class: 'notice-box' });
  box.appendChild(el('h1', null, title));
  if (detail) box.appendChild(el('p', { class: 'lede' }, detail));
  if (links && links.length) {
    var ul = el('ul', { class: 'linklist' });
    links.forEach(function (l) {
      var li = el('li');
      li.appendChild(el('a', { href: l.href }, l.label));
      ul.appendChild(li);
    });
    box.appendChild(ul);
  }
  return box;
}
