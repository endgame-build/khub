// widgets.js — small pieces several views share, all built through dom.js.

import { el, text } from './dom.js';
import { format, entityRoute, STATUSES } from './route.js';

// What each marker and review tier says, in one line. docs/cli.md defines them.
export var MEANING = {
  draft: 'Not published: check does not judge it',
  stale: 'Not updated within the workspace\'s stale window, or past its own stale_after date',
  orphan: 'No relation joins it to another entity',
  incomplete: 'A required attribute or relation is missing',
  expired: 'Past its own stale_after date',
  superseded: 'Its content changed after its newest review',
  unverified: 'No review holds: none was recorded, or the content changed since',
  machine: 'A review by a process or an agent holds',
  human: 'A review by a person holds'
};

// The markers in the order a reviewer acts on them.
export var MARKERS = STATUSES;

// NAMES is how a marker reads on screen, where its key would not say it.
var NAMES = { superseded: 'changed since review' };
export function markerName(key) { return NAMES[key] || key; }

// markerTags renders the status markers that are set, so a list filtered by one
// shows it on every row. An empty result is an empty span, so a table cell keeps
// its height.
export function markerTags(m) {
  var wrap = el('span', { class: 'tags' });
  if (!m) return wrap;
  MARKERS.forEach(function (k) {
    if (!m[k]) return;
    wrap.appendChild(el('span', { class: 'tag tag-' + k, title: MEANING[k] }, markerName(k)));
  });
  return wrap;
}

export function trustTag(trust) {
  var tier = trust || 'unverified';
  return el('span', { class: 'trust trust-' + tier, title: MEANING[tier] }, tier);
}

// trustBar draws a type's review tiers as one bar, with the words beside it.
export function trustBar(tiers, total) {
  var wrap = el('span', { class: 'tbar', title: 'human ' + tiers.human + ', machine ' + tiers.machine + ', unverified ' + tiers.unverified });
  var bar = el('span', { class: 'track', 'aria-hidden': 'true' });
  ['human', 'machine'].forEach(function (tier) {
    if (!tiers[tier]) return;
    bar.appendChild(el('i', { class: 'part-' + tier, style: { width: (100 * tiers[tier] / total) + '%' } }));
  });
  wrap.appendChild(bar);
  wrap.appendChild(el('span', { class: 'pct' }, total ? Math.floor(100 * (tiers.human + tiers.machine) / total) + '%' : '–'));
  return wrap;
}

// trustKey says what the parts of those bars are.
export function trustKey() {
  var p = el('p', { class: 'foot tkey' });
  ['human', 'machine'].forEach(function (tier) {
    p.appendChild(el('i', { class: 'part-' + tier, 'aria-hidden': 'true' }));
    p.appendChild(el('span', null, tier));
  });
  return p;
}

// copyButton puts text on the clipboard and says so on the button. Loopback is
// a trustworthy origin, so the Clipboard API is there over plain HTTP. Text
// that cannot be pasted safely (shell.js answers '') is not offered.
export function copyButton(label, value) {
  if (!value) return el('button', { type: 'button', class: 'copy', disabled: true, title: 'Its name holds control characters a terminal would act on' }, label);
  var btn = el('button', { type: 'button', class: 'copy', title: value, 'aria-live': 'polite' }, label);
  btn.addEventListener('click', function () { copy(btn, value); });
  return btn;
}

// copy puts value on the clipboard and says so on btn for a moment.
export function copy(btn, value) {
  var label = btn.getAttribute('data-label') || btn.textContent;
  btn.setAttribute('data-label', label);
  var say = function (word) {
    text(btn, word);
    setTimeout(function () { text(btn, label); }, 1500);
  };
  if (!navigator.clipboard) { say('Not available'); return; }
  navigator.clipboard.writeText(value).then(function () { say('Copied'); }, function () { say('Not copied'); });
}

// entityLink leads to an entity. A plain click opens it beside the view where
// the view has room for that; the keyboard, a modified click and a narrow
// window go to its page.
export function entityLink(id, label, ctx, cls) {
  return opensBeside(el('a', { href: format(entityRoute(id)), class: cls || null }, label), id, ctx);
}

// opensBeside makes a link to an entity that kind of link.
export function opensBeside(a, id, ctx) {
  a.addEventListener('click', function (evt) {
    if (evt.defaultPrevented || evt.button || evt.metaKey || evt.ctrlKey || evt.shiftKey || evt.altKey) return;
    if (evt.detail === 0 || !ctx.canOpen(id)) return;
    evt.preventDefault();
    ctx.open(id);
  });
  return a;
}

// rowOpens makes a whole row a wider target for its title; text in it can still
// be selected.
export function rowOpens(tr, id, ctx) {
  tr.addEventListener('click', function (evt) {
    if (evt.target.closest('a, button') || String(getSelection())) return;
    if (ctx.canOpen(id)) ctx.open(id); else ctx.nav(entityRoute(id));
  });
}

// reveal scrolls a row into view, under its table's sticky header when it is in
// one, and never sideways.
export function reveal(tr) {
  var main = document.getElementById('main'), table = tr.closest('table'), head = table && table.querySelector('thead');
  var box = main.getBoundingClientRect(), row = tr.getBoundingClientRect();
  var top = box.top + (head ? head.getBoundingClientRect().height : 0);
  if (row.top < top) main.scrollTop -= top - row.top;
  else if (row.bottom > box.bottom) main.scrollTop += row.bottom - box.bottom;
}

// when renders a date or a datetime as its day, whole on hover.
export function when(value) {
  var s = String(value || '');
  return el('time', { datetime: s, title: s }, s.slice(0, 10));
}

// skeleton holds a block's place while it loads.
export function skeleton(label, lines) {
  var box = el('div', { class: 'skel', role: 'status', 'aria-label': label });
  for (var i = 0; i < (lines || 3); i++) box.appendChild(el('i', { 'aria-hidden': 'true' }));
  return box;
}

// problem is a load that failed inside a block that otherwise stands.
export function problem(err) {
  return el('p', { class: 'err', role: 'alert' }, err.message || String(err));
}

// notice is the full-width message a view shows instead of its content.
export function notice(title, detail, links, retry) {
  var box = el('div', { class: 'notice-box', role: 'alert' });
  box.appendChild(el('h1', null, title));
  if (detail) box.appendChild(el('p', { class: 'lede' }, detail));
  if (retry) box.appendChild(el('button', { type: 'button', class: 'btn', on: { click: retry } }, 'Try again'));
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

// SAME_ROWS is the most rows a reading is compared over.
var SAME_ROWS = 5000;

// copyLater puts text that is still being worked out on the clipboard. The
// promise goes to the clipboard whole, so the press that asked still counts as
// the reader's own when the answer is slow. Empty text leaves the clipboard be.
export function copyLater(btn, promise, failed) {
  var blob = promise.then(function (value) {
    if (!value) throw new Error('');
    return new Blob([value], { type: 'text/plain' });
  });
  var label = btn.textContent;
  var say = function (word) {
    text(btn, word);
    setTimeout(function () { if (btn.textContent === word) text(btn, label); }, 1500);
  };
  if (!navigator.clipboard) { say('Not available'); blob.catch(function () {}); return; }
  var done = window.ClipboardItem
    ? navigator.clipboard.write([new ClipboardItem({ 'text/plain': blob })])
    : promise.then(function (value) { if (value) return navigator.clipboard.writeText(value); });
  promise.catch(failed);
  done.then(function () { promise.then(function (value) { if (value) say('Copied'); }); },
    function () { promise.then(function (value) { if (value) say('Not copied'); }, function () {}); });
}

// same says whether a view's new reading is the one it already shows, so a
// refresh that found nothing new leaves the page alone.
export function same(view, payload) {
  var now = payload.rows && payload.rows.length > SAME_ROWS ? null : JSON.stringify(payload);
  var was = view.seen;
  view.seen = now;
  return now !== null && now === was;
}

// num groups a count's thousands.
export function num(n) { return Number(n).toLocaleString('en-US'); }

// plural is a count with its noun.
export function plural(n, one, many) { return num(n) + ' ' + (n === 1 ? one : many || one + 's'); }
