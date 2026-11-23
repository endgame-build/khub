// list.js — one type's entities as a table: markers, trust, the type's own
// attributes and a count of linked entities per relation. Sorting and every
// filter live in the address (`#/type/<name>?sort=…`); the controls only
// rewrite it, and rendering only reads it.

import * as api from './api.js';
import { el, fill, text } from './dom.js';
import { markerTags, trustTag, notice } from './widgets.js';
import {
  visibleColumns, columnByKey, filterRows, sortRows, countLine, linkTargets,
  attrValues, columnTitle
} from './listmodel.js';
import { STATUSES, format, listRoute, entityRoute } from './route.js';

var view = null; // { type, rowset, root, ui, pendingRel, pendingAttr }

export function unmount() { view = null; }

// show renders the list for route. Entering a type loads its rows fresh; a
// change of sort or filter on the open list re-renders from the rows in hand.
export function show(main, route, ctx) {
  ctx.crumbs([{ label: 'Ontology', route: { view: 'overview', sel: '' } }, { label: route.type }]);
  if (view && view.type === route.type) {
    if (view.root && view.root.isConnected) { update(route, ctx); return; }
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
    on: { input: function () { ctx.replace(listRoute(view.type, view.list, { q: ui.q.value })); } }
  });
  bar.appendChild(ui.q);

  // Linked to: a relation column, then an entity reached through it.
  ui.rel = el('select', {
    'aria-label': 'Linked through relation',
    on: { change: function () {
      view.pendingRel = ui.rel.value;
      if (!ui.rel.value) { ctx.nav(listRoute(view.type, view.list, { rel: '', to: '' })); return; }
      fillTargets(ui.rel.value, '');
      ui.to.disabled = false;
      ui.to.focus();
    } }
  });
  ui.to = el('select', {
    'aria-label': 'Linked entity',
    on: { change: function () {
      if (!ui.to.value) return;
      ctx.nav(listRoute(view.type, view.list, { rel: ui.rel.value, to: ui.to.value }));
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
      if (!ui.attr.value) { ctx.nav(listRoute(view.type, view.list, { attr: '' })); return; }
      fillValues(ui.attr.value, '');
      ui.val.disabled = false;
      ui.val.focus();
    } }
  });
  ui.val = el('select', {
    'aria-label': 'Attribute value',
    on: { change: function () {
      if (!ui.val.value) return;
      ctx.nav(listRoute(view.type, view.list, { attr: ui.attr.value + '=' + ui.val.value }));
    } }
  });
  var attrGroup = el('span', { class: 'fgroup' });
  attrGroup.appendChild(el('span', { class: 'flabel' }, 'Where'));
  attrGroup.appendChild(ui.attr);
  attrGroup.appendChild(ui.val);
  if ((rowset.attributes || []).length) bar.appendChild(attrGroup);

  ui.status = el('select', {
    'aria-label': 'Status',
    on: { change: function () { ctx.nav(listRoute(view.type, view.list, { status: ui.status.value })); } }
  });
  ui.status.appendChild(option('', 'Any status'));
  STATUSES.forEach(function (s) { ui.status.appendChild(option(s, s)); });
  bar.appendChild(ui.status);

  ui.base = el('input', {
    type: 'checkbox', id: 'basecols',
    on: { change: function () { ctx.nav(listRoute(view.type, view.list, { base: ui.base.checked })); } }
  });
  var baseLabel = el('label', { class: 'check', htmlFor: 'basecols' });
  baseLabel.appendChild(ui.base);
  baseLabel.appendChild(el('span', null, 'Base relations'));
  bar.appendChild(baseLabel);

  ui.clear = el('button', {
    type: 'button', class: 'btn-ghost',
    on: { click: function () {
      view.pendingRel = '';
      view.pendingAttr = '';
      ctx.nav(listRoute(view.type, { sort: view.list.sort, dir: view.list.dir, base: view.list.base }));
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

  if (ui.q.value !== list.q && document.activeElement !== ui.q) ui.q.value = list.q;
  ui.base.checked = !!list.base;
  ui.status.value = list.status;

  var cols = visibleColumns(rowset, list.base);
  // A filter on a column the toggle hides is still honoured, so keep it on
  // offer while it is in force.
  var relCols = cols.slice();
  if (list.rel && !relCols.some(function (c) { return c.key === list.rel; })) {
    var hidden = columnByKey(rowset, list.rel);
    if (hidden) relCols.push(hidden);
  }
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
  ui.clear.disabled = !(list.q || list.rel || list.attr || list.status);

  var rows = sortRows(filterRows(rowset, list), list.sort, list.dir);
  text(ui.count, countLine(rows.length, rowset.total));
  fill(ui.table, [table(rowset, cols, rows, list, ctx)]);
}

function header(label, key, list, ctx, extraClass, title) {
  var active = list.sort === key;
  var th = el('th', {
    scope: 'col', class: (extraClass || '') + (active ? ' sorted' : ''),
    'aria-sort': active ? (list.dir === 'desc' ? 'descending' : 'ascending') : 'none'
  });
  var btn = el('button', {
    type: 'button', title: title || ('Sort by ' + label),
    on: { click: function () {
      var dir = active && list.dir === 'asc' ? 'desc' : (active ? 'asc' : (extraClass === 'num' ? 'desc' : 'asc'));
      ctx.nav(listRoute(view.type, list, { sort: key, dir: dir }));
    } }
  }, label + (active ? (list.dir === 'desc' ? ' ↓' : ' ↑') : ''));
  th.appendChild(btn);
  return th;
}

function table(rowset, cols, rows, list, ctx) {
  if (!rowset.total) {
    return el('p', { class: 'hint pad' }, 'No ' + rowset.type + ' entities yet.');
  }
  var t = el('table', { class: 'rows' });
  var thead = el('thead');
  var tr = el('tr');
  tr.appendChild(header('Title', 'title', list, ctx));
  tr.appendChild(header('ID', 'id', list, ctx));
  tr.appendChild(header('Status', 'status', list, ctx));
  tr.appendChild(header('Trust', 'trust', list, ctx));
  (rowset.attributes || []).forEach(function (a) {
    tr.appendChild(header(a.name, 'attr:' + a.name, list, ctx, 'attr'));
  });
  cols.forEach(function (c) {
    var th = header(columnTitle(c), c.key, list, ctx, 'num',
      (c.direction === 'in' ? 'Incoming from ' : 'Outgoing to ') + c.peer_types.join(' | ') + ' — sort by count');
    tr.appendChild(th);
  });
  thead.appendChild(tr);
  t.appendChild(thead);

  var tbody = el('tbody');
  rows.forEach(function (row) {
    var r = el('tr');
    var titleCell = el('td', { class: 'title' });
    titleCell.appendChild(el('a', { href: format(entityRoute(row.id)) }, row.title));
    r.appendChild(titleCell);
    r.appendChild(el('td', { class: 'mono id' }, row.id));
    var status = el('td');
    status.appendChild(markerTags(row, false));
    r.appendChild(status);
    var trust = el('td');
    trust.appendChild(trustTag(row.trust));
    r.appendChild(trust);
    (rowset.attributes || []).forEach(function (a) {
      var v = row.attrs ? row.attrs[a.name] : undefined;
      r.appendChild(el('td', { class: 'attr' + (v === undefined ? ' empty' : ''), title: v === undefined ? null : v },
        v === undefined ? '' : v));
    });
    cols.forEach(function (c) {
      var n = row.counts ? row.counts[c.key] || 0 : 0;
      r.appendChild(el('td', { class: 'num' + (n ? '' : ' zero') }, String(n)));
    });
    tbody.appendChild(r);
  });
  t.appendChild(tbody);
  if (!rows.length) {
    var empty = el('div');
    empty.appendChild(t);
    empty.appendChild(el('p', { class: 'hint pad' }, 'No ' + rowset.type + ' matches these filters.'));
    return empty;
  }
  return t;
}
