// list.js — one type's entities as a table: markers, trust, the type's own
// attributes and a count of linked entities per relation, each count a way to
// what it counts. Rows are drawn a window at a time. Sorting and every
// filter live in the address (`#/type/<name>?sort=…`); the controls only
// rewrite it, and rendering only reads it.

import * as api from './api.js';
import { el, fill, text } from './dom.js';
import { markerTags, trustTag, notice, MEANING } from './widgets.js';
import {
  visibleColumns, filterRows, sortRows, countLine, linkTargets,
  attrValues, columnTitle, parseCount, countTarget
} from './listmodel.js';
import { STATUSES, TIERS, format, listRoute, entityRoute } from './route.js';

// WINDOW is how many rows are drawn at a time: a list of tens of thousands
// answers at once, and draws more as the reader reaches the end.
var WINDOW = 300;

var view = null; // { type, loading, latest, rowset, root, ui, list, fillTargets, fillValues, pendingRel, pendingAttr, local, watch }

function unwatch() {
  if (view && view.watch) { view.watch.disconnect(); view.watch = null; }
}

export function unmount() { unwatch(); view = null; }

// show renders the list for route. Entering a type loads its rows fresh; a
// change of sort or filter on the open list re-renders from the rows in hand.
export function show(main, route, ctx) {
  ctx.crumbs([{ label: 'Ontology', route: { view: 'overview', sel: '' } }, { label: route.type }]);
  if (view && view.type === route.type) {
    if (view.root && view.root.isConnected) { update(route, ctx); ctx.ready(); return; }
    // Still loading this type: the load renders whatever the address says
    // when it lands, so a sort or filter chosen meanwhile is not lost.
    if (view.loading) { view.latest = route; return; }
  }
  var mine = { type: route.type, loading: true, latest: route };
  view = mine;
  fill(main, [el('p', { class: 'hint pad' }, 'Loading ' + route.type + '…')]);
  api.getRows(route.type).then(function (rowset) {
    if (view !== mine) return;
    mine.loading = false;
    try {
      build(main, rowset, mine.latest, ctx);
      update(mine.latest, ctx);
      ctx.ready();
    } catch (bug) {
      // A page bug, not a workspace problem: say so, and keep the stack.
      console.error(bug);
      fill(main, [notice('This list failed to render', String(bug))]);
    }
  }, function (err) {
    if (view !== mine) return;
    mine.loading = false; // a later visit to this address tries again
    var links = (err.expected || []).map(function (t) { return { label: t, href: format(listRoute(t)) }; });
    fill(main, [notice(err.code === 'unknown_type' ? 'No such type' : 'Could not load ' + route.type,
      err.message || String(err), links)]);
  });
}

function option(value, label) { return el('option', { value: value }, label); }

// go is a step the reader takes with this list's own controls. A half-made
// filter survives those, and no other way of getting here.
function go(ctx, route) {
  view.local = true;
  return ctx.nav(route);
}

function build(main, rowset, route, ctx) {
  var ui = {};
  var root = el('section', { class: 'listview' });
  var head = el('header', { class: 'viewhead' });
  head.appendChild(el('div', { class: 'eyebrow' }, 'Type'));
  var titleRow = el('div', { class: 'titlerow' });
  titleRow.appendChild(el('h1', null, rowset.type));
  ui.count = el('span', { class: 'count', 'aria-live': 'polite' });
  titleRow.appendChild(ui.count);
  titleRow.appendChild(el('a', { href: format({ view: 'overview', sel: 'type:' + rowset.type }), class: 'btn-ghost' }, 'Schema'));
  head.appendChild(titleRow);

  var bar = el('div', { class: 'filters', role: 'search' });
  // Free text: title, id and plain attribute values. Typing replaces the
  // address rather than pushing one entry per keystroke.
  ui.q = el('input', {
    type: 'search', class: 'q', placeholder: 'Filter by title, id or value',
    'aria-label': 'Filter by title, id or attribute value', autocomplete: 'off', spellcheck: false,
    on: { input: function () { view.local = true; ctx.entry().rows = 0; ctx.replace(listRoute(view.type, view.list, { q: ui.q.value })); } }
  });
  bar.appendChild(ui.q);

  // Linked to: a relation column, then an entity reached through it.
  ui.rel = el('select', {
    'aria-label': 'Linked through relation',
    on: { change: function () {
      // A relation alone filters nothing, so the filter in force goes: the
      // controls never show one thing and filter by another.
      view.pendingRel = ui.rel.value;
      go(ctx, listRoute(view.type, view.list, { rel: '', to: '' }));
      if (!ui.rel.value) return;
      fillTargets(ui.rel.value, '');
      ui.to.disabled = false;
      ui.to.focus();
    } }
  });
  ui.to = el('select', {
    'aria-label': 'Linked entity',
    on: { change: function () {
      view.pendingRel = ui.rel.value;
      go(ctx, listRoute(view.type, view.list, ui.to.value ? { rel: ui.rel.value, to: ui.to.value } : { rel: '', to: '' }));
    } }
  });
  var linkGroup = el('span', { class: 'fgroup' });
  linkGroup.appendChild(el('span', { class: 'flabel' }, 'Linked'));
  linkGroup.appendChild(ui.rel);
  linkGroup.appendChild(ui.to);
  bar.appendChild(linkGroup);

  // Attribute = value.
  ui.attr = el('select', {
    'aria-label': 'Attribute',
    on: { change: function () {
      view.pendingAttr = ui.attr.value;
      go(ctx, listRoute(view.type, view.list, { attr: '' }));
      if (!ui.attr.value) return;
      fillValues(ui.attr.value, '');
      ui.val.disabled = false;
      ui.val.focus();
    } }
  });
  ui.val = el('select', {
    'aria-label': 'Attribute value',
    on: { change: function () {
      view.pendingAttr = ui.attr.value;
      go(ctx, listRoute(view.type, view.list, { attr: ui.val.value ? ui.attr.value + '=' + ui.val.value : '' }));
    } }
  });
  var attrGroup = el('span', { class: 'fgroup' });
  attrGroup.appendChild(el('span', { class: 'flabel' }, 'Where'));
  attrGroup.appendChild(ui.attr);
  attrGroup.appendChild(ui.val);
  if ((rowset.attributes || []).length) bar.appendChild(attrGroup);

  // How many: a relation column, a comparison, a number — the bound
  // `khub query --count` takes. "→ realized_in = 0" lists what nothing realizes.
  var applyCount = function () {
    var n = parseInt(ui.cnum.value, 10);
    if (!ui.ccol.value || !(n >= 0)) {
      if (view.list.count) go(ctx, listRoute(view.type, view.list, { count: '' }));
      return;
    }
    go(ctx, listRoute(view.type, view.list, { count: ui.ccol.value + ui.cop.value + n }));
  };
  ui.ccol = el('select', { 'aria-label': 'Count of relation', on: { change: applyCount } });
  ui.cop = el('select', { 'aria-label': 'Comparison', on: { change: applyCount } });
  [['=', '='], ['>=', '≥'], ['<=', '≤'], ['>', '>'], ['<', '<']].forEach(function (o) { ui.cop.appendChild(option(o[0], o[1])); });
  ui.cnum = el('input', { type: 'number', min: 0, step: 1, class: 'cnum', 'aria-label': 'Count', on: { change: applyCount } });
  var countGroup = el('span', { class: 'fgroup' });
  countGroup.appendChild(el('span', { class: 'flabel' }, 'Count'));
  countGroup.appendChild(ui.ccol);
  countGroup.appendChild(ui.cop);
  countGroup.appendChild(ui.cnum);
  if ((rowset.relations || []).length) bar.appendChild(countGroup);

  ui.status = el('select', {
    'aria-label': 'Marker',
    on: { change: function () { go(ctx, listRoute(view.type, view.list, { status: ui.status.value })); } }
  });
  ui.status.appendChild(option('', 'Any marker'));
  STATUSES.forEach(function (s) { ui.status.appendChild(option(s, s)); });
  bar.appendChild(ui.status);

  ui.trust = el('select', {
    'aria-label': 'Trust',
    on: { change: function () { go(ctx, listRoute(view.type, view.list, { trust: ui.trust.value })); } }
  });
  ui.trust.appendChild(option('', 'Any trust'));
  TIERS.forEach(function (t) { ui.trust.appendChild(option(t, t)); });
  bar.appendChild(ui.trust);

  ui.base = el('input', {
    type: 'checkbox', id: 'basecols',
    on: { change: function () { go(ctx, listRoute(view.type, view.list, { base: ui.base.checked })); } }
  });
  var baseLabel = el('label', { class: 'check', htmlFor: 'basecols' });
  baseLabel.appendChild(ui.base);
  ui.folded = el('span', null, 'All relations');
  baseLabel.appendChild(ui.folded);
  bar.appendChild(baseLabel);

  ui.clear = el('button', {
    type: 'button', class: 'btn-ghost',
    on: { click: function () {
      view.pendingRel = '';
      view.pendingAttr = '';
      ui.ccol.value = '';
      ui.cnum.value = '';
      go(ctx, listRoute(view.type, { sort: view.list.sort, dir: view.list.dir, base: view.list.base }));
    } }
  }, 'Clear filters');
  bar.appendChild(ui.clear);
  head.appendChild(bar);
  root.appendChild(head);

  ui.table = el('div', { class: 'tblwrap listtable' });
  root.appendChild(ui.table);
  fill(main, [root]);

  view.rowset = rowset;
  view.root = root;
  view.ui = ui;
  view.pendingRel = '';
  view.pendingAttr = '';

  function fillTargets(rel, selected) {
    fill(ui.to, [option('', 'any entity…')].concat(linkTargets(rowset, rel).map(function (id) {
      return option(id, id);
    })));
    ui.to.value = selected;
  }
  function fillValues(name, selected) {
    fill(ui.val, [option('', 'any value…')].concat(attrValues(rowset, name).map(function (v) {
      return option(v, v);
    })));
    ui.val.value = selected;
  }
  view.fillTargets = fillTargets;
  view.fillValues = fillValues;
}

// update brings the controls in line with the route and redraws the table.
function update(route, ctx) {
  var list = route.list;
  var rowset = view.rowset;
  var ui = view.ui;
  view.list = list;

  // Back and forward show the list as it was: no half-made filter, and the
  // text box as the address has it even under the cursor.
  var local = view.local;
  view.local = false;
  if (!local) {
    view.pendingRel = '';
    view.pendingAttr = '';
    ui.ccol.value = '';
    ui.cnum.value = '';
    ui.q.value = list.q;
  }
  ui.base.checked = !!list.base;
  ui.status.value = list.status;
  ui.trust.value = list.trust;

  var bound = parseCount(list.count);
  var cols = visibleColumns(rowset, list.base, [list.sort, list.rel, bound && bound.key]);
  var folded = (rowset.relations || []).length - cols.length;
  text(ui.folded, 'All relations' + (folded ? ' (' + folded + ' more)' : ''));
  // Every declared relation can be counted, used or not: "none" is an answer.
  // A base one shows when asked for, in force, or half-chosen.
  var relCols = (rowset.relations || []).filter(function (c) {
    return list.base || !c.base || cols.indexOf(c) !== -1 || c.key === view.pendingRel || c.key === ui.ccol.value;
  });
  fill(ui.rel, [option('', 'through…')].concat(relCols.map(function (c) {
    return option(c.key, columnTitle(c));
  })));
  var rel = list.rel || view.pendingRel || '';
  ui.rel.value = rel;
  if (rel) {
    view.fillTargets(rel, list.rel ? list.to : '');
    ui.to.disabled = false;
  } else {
    fill(ui.to, [option('', 'any entity…')]);
    ui.to.disabled = true;
  }

  fill(ui.attr, [option('', 'attribute…')].concat((rowset.attributes || []).map(function (a) {
    return option(a.name, a.name);
  })));
  var attrName = list.attr ? list.attr.slice(0, list.attr.indexOf('=')) : (view.pendingAttr || '');
  ui.attr.value = attrName;
  if (attrName) {
    view.fillValues(attrName, list.attr ? list.attr.slice(list.attr.indexOf('=') + 1) : '');
    ui.val.disabled = false;
  } else {
    fill(ui.val, [option('', 'any value…')]);
    ui.val.disabled = true;
  }
  // The count bound's controls follow the address too; a column picked but
  // not yet bounded survives a re-render.
  var pendingCol = ui.ccol.value;
  fill(ui.ccol, [option('', 'relation…')].concat(relCols.map(function (c) { return option(c.key, columnTitle(c)); })));
  ui.ccol.value = bound ? bound.key : pendingCol;
  if (bound) { ui.cop.value = bound.op; if (document.activeElement !== ui.cnum) ui.cnum.value = String(bound.n); }
  ui.clear.disabled = !(list.q || list.rel || list.count || list.attr || list.status || list.trust ||
    view.pendingRel || view.pendingAttr);

  var rows = sortRows(filterRows(rowset, list), list.sort, list.dir);
  text(ui.count, countLine(rows.length, rowset.total));
  // A sort rebuilds the header the keyboard is on, so focus goes back to it.
  var at = document.activeElement;
  var focused = at && ui.table.contains(at) ? at.getAttribute('data-key') : null;
  unwatch();
  fill(ui.table, [table(rowset, cols, rows, list, ctx)]);
  if (focused) {
    Array.prototype.forEach.call(ui.table.querySelectorAll('th button'), function (b) {
      if (b.getAttribute('data-key') === focused) b.focus({ preventScroll: true });
    });
  }
}

// sortButton is one header's sort control; a count sorts high to low first.
function sortButton(label, key, list, ctx, title, numeric) {
  var active = list.sort === key;
  return el('button', {
    type: 'button', 'data-key': key, class: active ? 'on' : null, title: title || ('Sort by ' + label),
    on: { click: function () {
      var dir = active && list.dir === 'asc' ? 'desc' : (active ? 'asc' : (numeric ? 'desc' : 'asc'));
      go(ctx, listRoute(view.type, list, { sort: key, dir: dir }));
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
  th.appendChild(sortButton(label, key, list, ctx, title, extraClass === 'num'));
  return th;
}

// countCell is a count and the way to what it counts.
function countCell(row, col) {
  var n = row.counts ? row.counts[col.key] || 0 : 0;
  var td = el('td', { class: 'num' + (n ? '' : ' zero') });
  var to = countTarget(view.type, col, row);
  if (!to) { text(td, String(n)); return td; }
  var href = to.entity ? format(entityRoute(to.entity)) : format(listRoute(to.type, null, { rel: to.rel, to: to.to }));
  td.appendChild(el('a', { href: href, title: to.entity || ('The ' + n + ' of type ' + to.type) }, String(n)));
  return td;
}

function rowOf(row, rowset, cols) {
  var r = el('tr');
  var title = el('th', { scope: 'row', class: 'title' });
  title.appendChild(el('a', { href: format(entityRoute(row.id)) }, row.title));
  title.appendChild(el('div', { class: 'slug mono' }, row.slug));
  r.appendChild(title);
  var markers = el('td');
  markers.appendChild(markerTags(row, false));
  r.appendChild(markers);
  var trust = el('td');
  trust.appendChild(trustTag(row.trust));
  r.appendChild(trust);
  (rowset.attributes || []).forEach(function (a) {
    var v = row.attrs ? row.attrs[a.name] : undefined;
    r.appendChild(el('td', { class: 'attr' + (v === undefined ? ' empty' : ''), title: v === undefined ? null : v },
      v === undefined ? '' : v));
  });
  cols.forEach(function (c) { r.appendChild(countCell(row, c)); });
  return r;
}

function table(rowset, cols, rows, list, ctx) {
  if (!rowset.total) {
    return el('p', { class: 'hint pad' }, 'No ' + rowset.type + ' entities yet.');
  }
  var wrap = el('div');
  var t = el('table', { class: 'rows', 'aria-label': rowset.type + ' entities' });
  var thead = el('thead');
  var tr = el('tr');
  var title = headerCell(['title', 'id'], list, 'title');
  title.appendChild(sortButton('Title', 'title', list, ctx));
  title.appendChild(sortButton('ID', 'id', list, ctx));
  tr.appendChild(title);
  tr.appendChild(header('Markers', 'status', list, ctx, '',
    ['draft', 'incomplete', 'orphan', 'stale'].map(function (k) { return k + ': ' + MEANING[k]; }).join('\n')));
  tr.appendChild(header('Trust', 'trust', list, ctx, '',
    TIERS.map(function (k) { return k + ': ' + MEANING[k]; }).join('\n')));
  (rowset.attributes || []).forEach(function (a) {
    tr.appendChild(header(a.name, 'attr:' + a.name, list, ctx, 'attr'));
  });
  cols.forEach(function (c) {
    tr.appendChild(header(columnTitle(c), c.key, list, ctx, 'num',
      (c.direction === 'in' ? 'Incoming from ' : 'Outgoing to ') + c.peer_types.join(' | ') + ' — sort by count'));
  });
  thead.appendChild(tr);
  t.appendChild(thead);
  var tbody = el('tbody');
  t.appendChild(tbody);
  wrap.appendChild(t);
  if (!rows.length) {
    wrap.appendChild(el('p', { class: 'hint pad' }, 'No ' + rowset.type + ' matches these filters.'));
    return wrap;
  }

  var drawn = 0;
  var more = el('button', { type: 'button', class: 'btn-ghost more', on: { click: function () { draw(drawn + WINDOW); } } });
  var mine = view;
  function draw(upto) {
    if (view !== mine) return;
    for (; drawn < Math.min(upto, rows.length); drawn++) tbody.appendChild(rowOf(rows[drawn], rowset, cols));
    ctx.entry().rows = drawn;
    var left = rows.length - drawn;
    more.hidden = !left;
    text(more, 'Show ' + Math.min(WINDOW, left) + ' more of ' + left);
  }
  wrap.appendChild(more);
  // Back to a list draws as far as the reader had gone, so its scroll fits.
  draw(Math.max(WINDOW, ctx.entry().rows || 0));
  if (drawn < rows.length) {
    view.watch = new IntersectionObserver(function (seen) {
      if (seen.some(function (s) { return s.isIntersecting; })) draw(drawn + WINDOW);
    }, { root: view.root.parentNode, rootMargin: '600px' });
    view.watch.observe(more);
  }
  return wrap;
}
