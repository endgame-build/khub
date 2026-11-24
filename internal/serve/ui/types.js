// types.js — every declared type: what it is for, how many entities it holds,
// how many are reviewed and what they are marked with.

import * as api from './api.js';
import { el, fill } from './dom.js';
import { same, trustBar, trustKey, markerName, MARKERS, MEANING, notice, skeleton, num } from './widgets.js';
import { format, typeRoute, listRoute, overviewOf } from './route.js';

var view = null;

export function unmount() { view = null; }

export function show(main, route, ctx, force) {
  ctx.crumbs([{ label: 'Types' }]);
  if (!force && view && view.root && view.root.isConnected) { ctx.ready(); return; }
  var kept = force && view && view.root;
  var mine = { root: kept ? view.root : null, seen: kept ? view.seen : null };
  view = mine;
  if (!kept) fill(main, [skeleton('Loading types', 8)]);
  Promise.all([api.getOverview(), api.getSchema()]).then(function (r) {
    if (view !== mine) return;
    ctx.loaded(r[0].malformed);
    if (same(mine, r) && kept) { ctx.ready(); return; }
    mine.root = draw(r[0], r[1]);
    fill(main, [mine.root]);
    ctx.ready();
  }, function (err) {
    if (view !== mine) return;
    if (kept) { ctx.failed(err); return; }
    fill(main, [notice('Could not read the types', err.message, [], ctx.refresh)]);
  });
}

function draw(o, schema) {
  var when = new Map();
  schema.types.forEach(function (t) { when.set(t.name, t.when); });
  var root = el('section', { class: 'page' });
  var head = el('header', { class: 'viewhead' });
  head.appendChild(el('h1', null, 'Types'));
  head.appendChild(el('p', { class: 'sub' }, 'What this workspace can hold, and what it holds.'));
  root.appendChild(head);
  var used = o.types.filter(function (t) { return t.count; });
  var empty = o.types.filter(function (t) { return !t.count; });
  root.appendChild(table(used.concat(empty), when));
  return root;
}

function table(list, when) {
  var t = el('table', { class: 'plain typetable' });
  var thead = el('thead'), tr = el('tr');
  ['Type', 'Captured when', 'Entities', 'Reviewed', 'Marked', 'Schema'].forEach(function (h) {
    tr.appendChild(el('th', { scope: 'col', class: { Entities: 'num', Schema: 'go' }[h] || null }, h));
  });
  thead.appendChild(tr);
  t.appendChild(thead);
  var body = el('tbody');
  list.forEach(function (ty) {
    var r = el('tr', { class: ty.count ? null : 'none' });
    var name = el('th', { scope: 'row' });
    name.appendChild(el('a', { href: format(typeRoute(ty.name)) }, ty.name));
    r.appendChild(name);
    r.appendChild(el('td', { class: 'cue' }, when.get(ty.name) || ''));
    r.appendChild(el('td', { class: 'num' }, num(ty.count)));
    var bar = el('td', { class: 'reviewed' });
    if (ty.count) bar.appendChild(trustBar(ty.trust, ty.count));
    r.appendChild(bar);
    var marks = el('td', { class: 'marks' });
    MARKERS.forEach(function (k) {
      if (!ty.markers[k]) return;
      marks.appendChild(el('a', { href: format(listRoute(ty.name, null, { status: [k] })), class: 'tag tag-' + k, title: MEANING[k] },
        num(ty.markers[k]) + ' ' + markerName(k)));
    });
    r.appendChild(marks);
    var go = el('td', { class: 'go' });
    go.appendChild(el('a', { href: format(overviewOf('type:' + ty.name)), 'aria-label': 'View the schema of ' + ty.name }, 'View'));
    r.appendChild(go);
    body.appendChild(r);
  });
  t.appendChild(body);
  var card = el('div', { class: 'card' });
  card.appendChild(t);
  card.appendChild(trustKey());
  return card;
}
