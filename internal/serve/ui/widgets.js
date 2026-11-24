// widgets.js — small pieces several views share, all built through dom.js.

import { el, text } from './dom.js';

// What each marker and review tier says, in one line. docs/cli.md defines them.
export var MEANING = {
  draft: 'Not published: check does not judge it',
  stale: 'Not updated within the workspace\'s stale window, or past its own stale_after date',
  orphan: 'No relation joins it to another entity',
  incomplete: 'A required attribute or relation is missing',
  unverified: 'No review holds: none was recorded, or the content changed since',
  machine: 'A review by a process or an agent holds',
  human: 'A review by a person holds'
};

// markerTags renders the status markers that are set, plus the trust tier when
// it is anything but unverified. An empty result is an empty span, so a
// table cell keeps its height.
export function markerTags(m, withTrust) {
  var wrap = el('span', { class: 'tags' });
  if (!m) return wrap;
  ['draft', 'incomplete', 'orphan', 'stale'].forEach(function (k) {
    if (m[k]) wrap.appendChild(el('span', { class: 'tag tag-' + k, title: MEANING[k] }, k));
  });
  if (withTrust && m.trust && m.trust !== 'unverified') {
    wrap.appendChild(el('span', { class: 'tag tag-trust', title: MEANING[m.trust] }, m.trust));
  }
  return wrap;
}

export function trustTag(trust) {
  var tier = trust || 'unverified';
  return el('span', { class: 'trust trust-' + tier, title: MEANING[tier] }, tier);
}

// copyButton puts text on the clipboard and says so on the button. Loopback is
// a trustworthy origin, so the Clipboard API is there over plain HTTP.
export function copyButton(label, value) {
  var btn = el('button', { type: 'button', class: 'copy', title: value, 'aria-live': 'polite' }, label);
  var say = function (word) {
    text(btn, word);
    setTimeout(function () { text(btn, label); }, 1500);
  };
  btn.addEventListener('click', function () {
    if (!navigator.clipboard) { say('not available'); return; }
    navigator.clipboard.writeText(value).then(function () { say('copied'); }, function () { say('not copied'); });
  });
  return btn;
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
