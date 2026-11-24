// list.js — one type's entities as a table: markers, trust, the type's own
// attributes and a count of linked entities per relation, each count a way to
// what it counts. Facets stand beside it, and the entity being read. Rows are
// drawn a window at a time. Sorting and every filter live in the address
// (`#/type/<name>?sort=…`); the controls only rewrite it, and rendering only
// reads it.

import * as api from './api.js';
import * as panel from './panel.js';
import * as rail from './facets.js';
import { el, fill, text } from './dom.js';
import { same, markerTags, trustTag, notice, entityLink, rowOpens, reveal, when, copyButton, skeleton, MEANING, MARKERS, markerName, num } from './widgets.js';
import {
  visibleColumns, visibleAttributes, filterRows, sortRows, countLine, linkTargets,
  columnTitle, countTarget, columnsInUse, applied, highFirst, attrText
} from './listmodel.js';
import { TIERS, format, listRoute, overviewOf, openId, opening } from './route.js';
import { queryCommand } from './shell.js';

// WINDOW is how many rows are drawn at a time: a list of tens of thousands
// answers at once, and draws more as the reader reaches the end.
var WINDOW = 300;
// NARROW is where the filters would push the rows off the screen (app.css).
var NARROW = window.matchMedia('(max-width: 1099.98px)');
NARROW.addEventListener('change', function () { if (view && view.ui) view.ui.fold(); });
var TYPING_MS = 120; // how long typing must pause before the rows follow

var view = null; // { type, loading, latest, rowset, root, ui, list, rows, key, pendingRel, local, typing, watch, open, shown }

function unwatch() {
  if (view && view.watch) { view.watch.disconnect(); view.watch = null; }
}

export function unmount() { unwatch(); view = null; panel.unmount(); }

// order is the rows next and previous follow.
export function order() { return view && view.rows ? view.rows : []; }

// show renders the list for route. Entering a type loads its rows fresh; a
// change of sort or filter on the open list re-renders from the rows in hand.
export function show(main, route, ctx, force) {
  var crumbs = ctx.crumbs([{ label: 'Types', route: { view: 'types' } }, { label: route.type }]);
  if (!force && view && view.type === route.type) {
    if (view.root && view.root.isConnected) { update(route, ctx, false); ctx.ready(); return; }
    // Still loading this type: the load renders whatever the address says
    // when it lands, so a sort or filter chosen meanwhile is not lost.
    if (view.loading) { view.latest = route; return; }
  }
  var kept = force && view && view.type === route.type && view.root && view.root.isConnected ? view : null;
  if (!kept) unwatch();
  var mine = { type: route.type, loading: true, latest: route, open: kept ? kept.open : Object.create(null), root: kept ? kept.root : null };
  view = kept || mine;
  if (!kept) fill(main, [skeleton('Loading ' + route.type, 10)]);
  api.getRows(route.type).then(function (rowset) {
    if (view !== (kept || mine)) return;
    ctx.loaded(rowset.malformed);
    if (kept && same(kept, rowset)) { ctx.ready(); panel.sync(kept.ui.aside, ctx.route(), ctx, kept.rows, true); return; }
    if (!kept) same(mine, rowset); else mine.seen = kept.seen;
    unwatch();
    view = mine;
    mine.loading = false;
    var now = kept ? ctx.route() : mine.latest;
    try {
      var place = kept && placeIn(kept.ui);
      build(main, rowset, crumbs, ctx, kept && kept.ui.aside);
      update(now, ctx, true);
      if (place) place(view.ui);
      ctx.ready();
    } catch (bug) {
      // A page bug, not a workspace problem: say so, and keep the stack.
      console.error(bug);
      fill(main, [notice('This list failed to render', String(bug))]);
    }
  }, function (err) {
    if (view !== (kept || mine)) return;
    if (kept) { ctx.failed(err); return; }
    mine.loading = false; // a later visit to this address tries again
    var links = (err.expected || []).map(function (t) { return { label: t, href: format(listRoute(t)) }; });
    fill(main, [notice(err.code === 'unknown_type' ? 'No such type' : 'Could not load ' + route.type,
      err.message || String(err), links, err.code === 'unknown_type' ? null : ctx.refresh)]);
  });
}

// placeIn remembers what a rebuild would lose of the reader's place: the rail's
// scroll, and the keyboard in the text box or on a row. It returns what puts
// them back.
function placeIn(ui) {
  var box = ui.rail.parentNode, top = box.scrollTop, open = box.open, more = ui.more.open;
  var at = document.activeElement;
  var typing = at === ui.q ? [ui.q.value, ui.q.selectionStart, ui.q.selectionEnd] : null;
  var row = ui.table.contains(at) && at.closest('tbody tr') ? at.closest('tr').getAttribute('data-id') : '';
  return function (now) {
    now.rail.parentNode.open = open;
    now.more.open = more;
    now.rail.parentNode.scrollTop = top;
    if (row) focusOn(row);
    if (!typing) return;
    now.q.value = typing[0];
    now.q.focus({ preventScroll: true });
    now.q.setSelectionRange(typing[1], typing[2]);
  };
}

// go is a step the reader takes with this list's own controls. A half-made
// filter survives those, and no other way of getting here.
function go(ctx, changes) {
  view.local = true;
  return ctx.nav(listRoute(view.type, view.list, changes));
}

// focusOn puts the keyboard on a row's title.
export function focusOn(id) {
  if (!view || !view.ui) return;
  Array.prototype.forEach.call(view.ui.table.querySelectorAll('tbody tr'), function (tr) {
    if (tr.getAttribute('data-id') === id) tr.querySelector('a').focus({ preventScroll: true });
  });
}

// build draws the frame of the list. aside, when given, is the panel to keep.
function build(main, rowset, crumbs, ctx, aside) {
  var ui = {};
  var root = el('section', { class: 'split listview' });

  ui.rail = el('div', { class: 'railbody' });
  var railBox = el('details', { class: 'rail', open: true });
  railBox.appendChild(el('summary', null, 'Filters'));
  railBox.appendChild(el('button', { type: 'button', class: 'skip', on: { click: function () {
    var first = ui.table.querySelector('tbody a');
    if (first) first.focus();
  } } }, 'Skip to the rows'));
  railBox.appendChild(ui.rail);
  // Folded to start with where it would push the rows off the screen.
  railBox.open = !NARROW.matches;
  ui.fold = function () { railBox.open = !NARROW.matches; };

  var pane = el('div', { class: 'pane' });
  var head = el('header', { class: 'viewhead' });
  head.appendChild(crumbs);
  var row = el('div', { class: 'titlerow' });
  row.appendChild(el('h1', null, rowset.type));
  ui.count = el('span', { class: 'count', 'aria-live': 'polite' });
  row.appendChild(ui.count);
  row.appendChild(el('a', { href: format(overviewOf('type:' + rowset.type)), class: 'btn-ghost' }, 'Schema'));
  ui.command = el('span');
  row.appendChild(ui.command);
  head.appendChild(row);

  var bar = el('div', { class: 'filters' });
  // Free text: title, id and plain attribute values. Typing replaces the
  // address rather than pushing one entry per keystroke.
  ui.q = el('input', {
    type: 'search', class: 'q', placeholder: 'Filter by title, id or value',
    'aria-label': 'Filter by title, id or attribute value', autocomplete: 'off', spellcheck: false,
    on: { input: function () {
      clearTimeout(view.typing);
      var mine = view;
      view.typing = setTimeout(function () {
        if (view !== mine) return;
        view.local = true;
        ctx.entry().rows = 0;
        ctx.replace(listRoute(view.type, view.list, { q: ui.q.value }));
      }, TYPING_MS);
    } }
  });
  bar.appendChild(ui.q);
  ui.chips = el('span', { class: 'chips' });
  bar.appendChild(ui.chips);
  head.appendChild(bar);
  pane.appendChild(head);
  ui.table = el('div', { class: 'listtable' });
  pane.appendChild(ui.table);

  root.appendChild(railBox);
  root.appendChild(pane);
  ui.aside = aside || el('aside', { class: 'peek', 'aria-label': 'Entity', hidden: true });
  root.appendChild(ui.aside);
  ui.facets = el('div');
  fill(ui.rail, [ui.facets, more(rowset, ui, ctx)]);
  fill(main, [root]);

  view.rowset = rowset;
  view.root = root;
  view.ui = ui;
  view.key = null;
  view.pendingRel = '';
}

// more is what a facet cannot offer: rows linked to one entity, and the
// columns that fold away.
function more(rowset, ui, ctx) {
  var box = el('fieldset', { class: 'facet' });
  box.appendChild(el('legend', null, 'Linked to one entity'));
  ui.more = el('details', { class: 'morefilters' });
  ui.more.appendChild(el('summary', null, 'More'));
  ui.rel = el('select', {
    'aria-label': 'Linked through relation',
    on: { change: function () {
      // A relation alone filters nothing, so the filter in force goes: the
      // controls never show one thing and filter by another.
      view.pendingRel = ui.rel.value;
      go(ctx, { rel: '', to: '' });
      if (!ui.rel.value) return;
      fillTargets(rowset, ui, ui.rel.value, '');
      ui.to.disabled = false;
      ui.to.focus();
    } }
  });
  ui.to = el('select', {
    'aria-label': 'Linked entity',
    on: { change: function () {
      view.pendingRel = ui.rel.value;
      go(ctx, ui.to.value ? { rel: ui.rel.value, to: ui.to.value } : { rel: '', to: '' });
    } }
  });
  box.appendChild(ui.rel);
  box.appendChild(ui.to);
  var cols = el('label', { class: 'check', htmlFor: 'basecols' });
  ui.base = el('input', { type: 'checkbox', id: 'basecols', on: { change: function () { go(ctx, { base: ui.base.checked }); } } });
  cols.appendChild(ui.base);
  ui.folded = el('span');
  cols.appendChild(ui.folded);
  if ((rowset.relations || []).length) ui.more.appendChild(box);
  ui.more.appendChild(cols);
  return ui.more;
}

function option(value, label) { return el('option', { value: value }, label); }

function fillTargets(rowset, ui, rel, selected) {
  fill(ui.to, [option('', 'any entity…')].concat(linkTargets(rowset, rel).map(function (id) { return option(id, id); })));
  ui.to.value = selected;
}

// update brings the controls in line with the route and redraws what changed.
function update(route, ctx, force) {
  var list = route.list, rowset = view.rowset, ui = view.ui;
  var key = format(opening(route, ''));
  view.list = list;
  if (!force && key === view.key) { mark(route, ctx, false); return; } // only what is open beside it
  view.key = key;

  // Back and forward show the list as it was: no half-made filter, and the
  // text box as the address has it even under the cursor.
  var local = view.local;
  view.local = false;
  if (!local) { view.pendingRel = ''; clearTimeout(view.typing); }
  if (!local || document.activeElement !== ui.q) ui.q.value = list.q;

  var cols = visibleColumns(rowset, list.base, columnsInUse(list));
  var attrs = visibleAttributes(rowset, list.base);
  var folded = (rowset.relations || []).length - cols.length + (rowset.attributes || []).length - attrs.length;
  ui.base.checked = !!list.base;
  text(ui.folded, 'Show every column' + (folded ? ' (' + folded + ' more)' : ''));
  ui.base.parentNode.hidden = !folded && !list.base;

  // A relation no row uses names no entity to be linked to.
  fill(ui.rel, [option('', 'through…')].concat((rowset.relations || []).filter(function (c) {
    return c.key === list.rel || c.key === view.pendingRel || linkTargets(rowset, c.key).length;
  }).map(function (c) { return option(c.key, columnTitle(c)); })));
  var rel = list.rel || view.pendingRel || '';
  ui.rel.value = rel;
  if (rel) fillTargets(rowset, ui, rel, list.rel ? list.to : '');
  else fill(ui.to, [option('', 'any entity…')]);
  ui.to.disabled = !rel;
  if (rel || list.base) ui.more.open = true;

  var rows = sortRows(filterRows(rowset, list), list.sort, list.dir);
  view.rows = rows;
  text(ui.count, countLine(num(rows.length), num(rowset.total)));
  var cmd = queryCommand(rowset, list);
  fill(ui.command, [cmd.command ? copyButton('Copy khub query', cmd.command)
    : el('span', { class: 'hint' }, 'khub query cannot say ' + cmd.why)]);

  // A choice redraws the control the keyboard is on, so focus goes back to it.
  var focused = focusedKey(ui.rail) || focusedKey(ui.table);
  var onChip = chipAt(ui);
  rail.draw(ui.facets, rowset, list, function (changes) { go(ctx, changes); }, view.open);
  unwatch();
  fill(ui.table, [table(rowset, attrs, cols, rows, list, ctx)]);
  chips(rowset, list, ui, ctx);
  if (focused) focusKey(view.root, focused);
  if (onChip !== -1) {
    // The chip is gone: the keyboard goes to the one in its place.
    var left = ui.chips.querySelectorAll('button');
    (left[Math.min(onChip, left.length - 1)] || ui.q).focus({ preventScroll: true });
  }
  mark(route, ctx, force);
}

// chipAt is which chip the keyboard is on, -1 when on none.
function chipAt(ui) {
  return Array.prototype.indexOf.call(ui.chips.querySelectorAll('button'), document.activeElement);
}

function focusedKey(inside) {
  var at = document.activeElement;
  if (!at || !inside.contains(at)) return null;
  return at.getAttribute('data-key') || at.getAttribute('data-fv');
}

function focusKey(root, key) {
  Array.prototype.forEach.call(root.querySelectorAll('[data-key], [data-fv]'), function (b) {
    if (b.getAttribute('data-key') === key || b.getAttribute('data-fv') === key) b.focus({ preventScroll: true });
  });
}

// chips are the filters in force, each one press from gone.
function chips(rowset, list, ui, ctx) {
  var all = applied(rowset, list).filter(function (c) { return c.name !== 'text'; });
  var nodes = all.map(function (c) {
    var b = el('button', { type: 'button', class: 'chip', title: 'Remove this filter', on: { click: function () { go(ctx, c.off); } } });
    b.appendChild(el('em', null, c.name));
    b.appendChild(el('span', null, c.name === 'marker' ? c.value.replace(/^(not )?(.*)$/, function (_, n, k) { return (n || '') + markerName(k); }) : c.value));
    b.appendChild(el('span', { class: 'x', 'aria-hidden': 'true' }, '✕'));
    return b;
  });
  if (all.length || list.q) {
    nodes.push(el('button', { type: 'button', class: 'btn-ghost', on: { click: function () {
      view.pendingRel = '';
      ctx.nav(listRoute(view.type, { sort: list.sort, dir: list.dir, base: list.base, open: list.open }));
    } } }, 'Clear'));
  }
  fill(ui.chips, nodes);
}

// mark shows which row is open and reads it beside the list.
function mark(route, ctx, force) {
  var open = openId(route);
  var at = null;
  Array.prototype.forEach.call(view.ui.table.querySelectorAll('tbody tr'), function (tr) {
    var on = tr.getAttribute('data-id') === open;
    tr.classList.toggle('open', on);
    if (on) at = tr;
  });
  if (open && !at && view.draw) at = view.draw(open);
  panel.sync(view.ui.aside, route, ctx, view.rows, force);
  if (at && (force || open !== view.shown)) reveal(at);
  view.shown = open;
}

// sortButton is one header's sort control.
function sortButton(label, key, list, ctx, title) {
  var active = list.sort === key;
  return el('button', {
    type: 'button', 'data-key': key, class: active ? 'on' : null, title: title || ('Sort by ' + label),
    on: { click: function () {
      var first = highFirst(key) ? 'desc' : 'asc';
      var dir = active ? (list.dir === 'asc' ? 'desc' : 'asc') : first;
      go(ctx, { sort: key, dir: dir });
    } }
  }, label + (active ? (list.dir === 'desc' ? ' ↓' : ' ↑') : ''));
}

function headerCell(keys, list, extraClass) {
  var active = keys.indexOf(list.sort) !== -1;
  return el('th', {
    scope: 'col', class: (extraClass || '') + (active ? ' sorted' : ''),
    'aria-sort': active ? (list.dir === 'desc' ? 'descending' : 'ascending') : 'none'
  });
}

function header(label, key, list, ctx, extraClass, title) {
  var th = headerCell([key], list, extraClass);
  th.appendChild(sortButton(label, key, list, ctx, title));
  return th;
}

// countCell is a count and the way to what it counts.
function countCell(row, col, ctx) {
  var n = row.counts ? row.counts[col.key] || 0 : 0;
  var td = el('td', { class: 'num' + (n ? '' : ' zero') });
  var to = countTarget(view.type, col, row);
  if (!to) { text(td, String(n)); return td; }
  if (to.entity) { td.appendChild(entityLink(to.entity, String(n), ctx)); td.firstChild.title = to.entity; return td; }
  td.appendChild(el('a', { href: format(listRoute(to.type, null, { rel: to.rel, to: to.to })), title: 'The ' + n + ' of type ' + to.type }, String(n)));
  return td;
}

function rowOf(row, attrs, cols, ctx, open) {
  var r = el('tr', { 'data-id': row.id, class: row.id === open ? 'open' : null });
  var title = el('th', { scope: 'row', class: 'title' });
  title.appendChild(entityLink(row.id, row.title, ctx));
  title.appendChild(el('div', { class: 'slug mono' }, row.slug));
  r.appendChild(title);
  var markers = el('td', { class: 'marks' });
  markers.appendChild(markerTags(row));
  r.appendChild(markers);
  var trust = el('td');
  trust.appendChild(trustTag(row.trust));
  r.appendChild(trust);
  attrs.forEach(function (a) {
    var v = attrText(row, a.name);
    r.appendChild(el('td', { class: 'attr', title: v || null }, v));
  });
  cols.forEach(function (c) { r.appendChild(countCell(row, c, ctx)); });
  var date = el('td', { class: 'date' });
  if (row.updated) date.appendChild(when(row.updated));
  r.appendChild(date);
  r.appendChild(el('td', { class: 'by' }, row.by || ''));
  rowOpens(r, row.id, ctx);
  return r;
}

function table(rowset, attrs, cols, rows, list, ctx) {
  view.draw = null;
  if (!rowset.total) return el('p', { class: 'empty' }, 'No ' + rowset.type + ' has been captured yet.');
  if (!rows.length) return el('p', { class: 'empty' }, 'No ' + rowset.type + ' passes these filters.');
  var wrap = el('div', { class: 'tablescroll' });
  var t = el('table', { class: 'rows', 'aria-label': rowset.type + ' entities' });
  var thead = el('thead');
  var tr = el('tr');
  var title = headerCell(['title', 'id'], list, 'title');
  title.appendChild(sortButton('Title', 'title', list, ctx));
  title.appendChild(sortButton('ID', 'id', list, ctx));
  tr.appendChild(title);
  tr.appendChild(header('Markers', 'status', list, ctx, '',
    MARKERS.map(function (k) { return markerName(k) + ': ' + MEANING[k]; }).join('\n')));
  tr.appendChild(header('Trust', 'trust', list, ctx, '',
    TIERS.map(function (k) { return k + ': ' + MEANING[k]; }).join('\n')));
  attrs.forEach(function (a) { tr.appendChild(header(a.name, 'attr:' + a.name, list, ctx, 'attr')); });
  cols.forEach(function (c) {
    tr.appendChild(header(columnTitle(c), c.key, list, ctx, 'num',
      c.predicate + ', ' + (c.direction === 'in' ? 'incoming from ' : 'outgoing to ') + c.peer_types.join(' | ')));
  });
  tr.appendChild(header('Updated', 'updated', list, ctx, 'date'));
  tr.appendChild(header('Written by', 'by', list, ctx, 'by'));
  thead.appendChild(tr);
  t.appendChild(thead);
  var tbody = el('tbody');
  t.appendChild(tbody);
  wrap.appendChild(t);

  var drawn = 0;
  var more = el('button', { type: 'button', class: 'btn-ghost more', on: { click: function () { draw(drawn + WINDOW); } } });
  var mine = view, open = list.open;
  function draw(upto) {
    if (view !== mine) return;
    for (; drawn < Math.min(upto, rows.length); drawn++) tbody.appendChild(rowOf(rows[drawn], attrs, cols, ctx, open));
    ctx.entry().rows = drawn;
    var left = rows.length - drawn;
    more.hidden = !left;
    text(more, 'Show ' + num(Math.min(WINDOW, left)) + ' more of ' + num(left));
  }
  wrap.appendChild(more);
  // draw reaches a row past the window, for next and previous and for an
  // address that names one.
  view.draw = function (id) {
    for (var i = drawn; i < rows.length; i++) {
      if (rows[i].id !== id) continue;
      draw(i + WINDOW / 2);
      tbody.children[i].classList.add('open');
      return tbody.children[i];
    }
    return null;
  };
  // Back to a list draws as far as the reader had gone, so its scroll fits.
  draw(Math.max(WINDOW, ctx.entry().rows || 0));
  if (drawn < rows.length) {
    view.watch = new IntersectionObserver(function (seen) {
      if (seen.some(function (s) { return s.isIntersecting; })) draw(drawn + WINDOW);
    }, { root: document.getElementById('main'), rootMargin: '600px' });
    view.watch.observe(more);
  }
  return wrap;
}
