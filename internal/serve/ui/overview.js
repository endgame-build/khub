// overview.js — the ontology: a diagram of the workspace's types and the
// relations between them, whole or around one type, with every type beside it
// on the left and the selected type's or relation's details on the right. The
// selection and the focus live in the address (`#/ontology?sel=…&focus=…`).

import * as api from './api.js';
import * as gfx from './cy.js';
import { el, fill, text, tn } from './dom.js';
import { same, notice, num, plural, skeleton } from './widgets.js';
import {
  indexSchema, indexOverview, relationOf, buildSchemaGraph, focusOn, mostConnected,
  selectionIds, typeDetails, baseDetails, relationDetails, incomingRelations, schemaId
} from './model.js';
import { layoutOntology, placeElements, BOX_H, GROUP_PAD } from './layout.js';
import { format, typeRoute, listRoute, overviewOf } from './route.js';

var RELAYOUT_MS = 120; // how long the canvas must hold a size before a relayout
var DOUBLE_MS = 600;   // how close a click and the double-click it began are

var view = null; // { root, canvas, details, rail, bar, latest, sch, schema, numbers, style, seq, entry, size, shown, watch, timer, pushed, follow }

// unmount releases the diagram when another view takes the page, and leaves
// its viewport with the history entry that showed it.
export function unmount() {
  if (view) {
    if (view.watch) view.watch.disconnect();
    clearTimeout(view.timer);
    keepViewport();
  }
  gfx.destroy();
  view = null;
}

// keepViewport leaves the diagram's zoom and pan with the entry on screen.
function keepViewport() {
  if (view.entry && view.size) view.entry.viewport = { at: gfx.viewport(), size: view.size, shown: view.shown };
}

// show renders the overview for route. Entering the view loads fresh data;
// a selection change on the open view only redraws the details.
export function show(main, route, ctx, force) {
  ctx.crumbs([{ label: 'Ontology' }]);
  if (!force && view && view.root.isConnected) {
    // Still loading: the load lands on whatever the address says by then.
    view.latest = route;
    update(route, ctx);
    return;
  }
  if (force && view && view.sch) {
    var kept = view;
    load(ctx, function () { return view === kept; }, function (err) { ctx.failed(err); });
    return;
  }
  unmount();
  var canvas = el('div', { id: 'cy', role: 'img', 'aria-label': 'Diagram of the types and the relations between them. The list beside it names every type.' });
  var diagram = el('div', { class: 'diagram' });
  var bar = el('div', { class: 'dbar' });
  diagram.appendChild(bar);
  diagram.appendChild(canvas);
  diagram.appendChild(legend());
  var rail = el('nav', { class: 'typerail', 'aria-label': 'Types' });
  var details = el('section', { class: 'details', 'aria-label': 'Details' });
  var root = el('section', { class: 'overview' });
  root.appendChild(el('h1', { class: 'unseen' }, 'Ontology'));
  root.appendChild(rail);
  root.appendChild(diagram);
  root.appendChild(details);
  fill(main, [root]);
  fill(details, [skeleton('Loading the ontology', 6)]);
  view = { root: root, canvas: canvas, details: details, rail: rail, bar: bar, seq: 0, latest: route };
  var mine = view;
  load(ctx, function () { return view === mine; }, function (err) {
    fill(main, [notice('Could not load the ontology', err.message || String(err), [], ctx.refresh)]);
  });
}

function load(ctx, ours, failed) {
  Promise.all([api.getOverview(), api.getSchema()]).then(function (r) {
    if (!ours()) return;
    ctx.loaded(r[0].malformed);
    if (same(view, r) && view.sch) { ctx.ready(); return; }
    try { draw(r, ctx); } catch (bug) {
      // A page bug, not a workspace problem: say so, and keep the stack.
      console.error(bug);
      fill(view.root, [notice('The ontology failed to render', String(bug))]);
    }
  }, function (err) { if (ours()) failed(err); });
}

// escape steps out: from one type to the whole map, then from a selection to
// none.
export function escape(ctx) {
  if (!view || !view.sch) return;
  if (view.shown) ctx.nav(overviewOf('', { map: true }));
  else if (view.latest.sel) ctx.nav(overviewOf('', { map: view.latest.map }));
}

// legend explains the notation, as a diagram's legend should, beside it.
function legend() {
  var box = el('div', { class: 'legend' });
  [
    ['solid', 'required'],
    ['dashed', 'optional'],
    ['base', 'inherited from the base'],
    ['dotted', 'not used yet'],
    ['tag', 'to its own type'],
    ['many', 'holds many']
  ].forEach(function (row) {
    var item = el('span', { class: 'lg' });
    item.appendChild(el('span', { class: 'sw sw-' + row[0], 'aria-hidden': 'true' }));
    item.appendChild(el('span', null, row[1]));
    box.appendChild(item);
  });
  return box;
}

function draw(r, ctx) {
  var numbers = indexOverview(r[0]);
  var schema = indexSchema(r[1]);
  // Present types first, sorted, then declared-but-empty ones — viz's own
  // colour order, so a type is the same colour here and in a viz file.
  var present = r[0].types.filter(function (t) { return t.count; }).map(function (t) { return t.name; }).sort();
  var typeOrder = present.concat(schema.declaredTypes.filter(function (t) { return present.indexOf(t) === -1; }).sort());
  view.numbers = numbers;
  view.schema = schema;
  view.sch = buildSchemaGraph(schema, schema.edges, numbers);
  view.style = gfx.buildStyle(typeOrder, ctx.colors, ctx.ink, BOX_H, GROUP_PAD);
  drawRail();
  // The details fill first: below the diagram on a narrow window, they are
  // part of what decides the canvas's shape.
  if (view.entry) keepViewport();
  view.entry = ctx.entry();
  var kept = view.entry.viewport;
  update(view.latest, ctx, true);
  if (kept && kept.at && kept.shown === view.shown && kept.size.w === view.size.w && kept.size.h === view.size.h) gfx.viewport(kept.at);
  if (!view.watch) {
    var mine = view, canvas = view.canvas;
    view.watch = new ResizeObserver(function () {
      if (view !== mine) return;
      clearTimeout(mine.timer);
      mine.timer = setTimeout(function () {
        if (view === mine && (canvas.clientWidth !== mine.size.w || canvas.clientHeight !== mine.size.h)) place(ctx);
      }, RELAYOUT_MS);
    });
    view.watch.observe(canvas);
  }
  ctx.ready();
}

function countOf(name) {
  var t = view.numbers.types.get(name);
  return t ? t.count : 0;
}

// drawRail lists every type: the keyboard path into the diagram, and the way
// to a type by name.
function drawRail() {
  var typed = view.rail.querySelector('input');
  var typing = typed && document.activeElement === typed;
  var find = el('input', { type: 'search', class: 'q', placeholder: 'Find a type', 'aria-label': 'Find a type', autocomplete: 'off', spellcheck: false, value: typed ? typed.value : '', on: { input: narrow } });
  var ul = el('ul', { class: 'typeindex' });
  var none = el('p', { class: 'hint', hidden: true }, 'No type has this in its name.');
  view.schema.declaredTypes.slice().sort(function (a, b) {
    return (countOf(a) ? 0 : 1) - (countOf(b) ? 0 : 1) || (a < b ? -1 : 1);
  }).forEach(function (t) {
    var li = el('li', { 'data-type': t, class: countOf(t) ? null : 'none' });
    li.appendChild(el('a', { class: 'tname' }, t));
    li.appendChild(el('span', { class: 'n' }, num(countOf(t))));
    ul.appendChild(li);
  });
  fill(view.rail, [find, ul, none]);
  if (typing) find.focus();
  function narrow() {
    var q = find.value.trim().toLowerCase(), left = 0;
    Array.prototype.forEach.call(ul.children, function (li) {
      li.hidden = li.getAttribute('data-type').toLowerCase().indexOf(q) === -1;
      if (!li.hidden) left += 1;
    });
    none.hidden = left > 0;
  }
  narrow();
}

// to is the address of a selection that keeps how much of the schema is on
// screen: the whole, or one type, which a selected type out of view becomes.
function to(sel) {
  if (view.latest.map) return format(overviewOf(sel, { map: true }));
  if (!view.shown) return format(overviewOf(sel));
  var type = subject(sel);
  var here = selectionIds(view.drawn, sel).length > 0;
  return format(overviewOf(sel, { focus: here || !declared(type) ? view.shown : type }));
}

function declared(name) { return view.schema.byName.has(name); }

// subject is the type a selection is about.
function subject(sel) {
  if (sel.indexOf('type:') === 0) return sel.slice(5);
  if (sel.indexOf('rel:') === 0) return sel.slice(4, sel.indexOf('.'));
  return '';
}

// around is the type the diagram shows alone: the one asked for, or, where the
// whole would be too small to read, the selected one or the most connected.
function around(route) {
  if (declared(route.focus)) return route.focus;
  if (route.map) return '';
  if (gfx.readable(view.whole, view.size.w, view.size.h)) return '';
  return declared(subject(route.sel)) ? subject(route.sel) : mostConnected(view.sch);
}

// place lays the diagram out against the canvas's shape and draws it.
function place(ctx) {
  measure();
  view.shown = around(view.latest);
  var sch = view.shown ? focusOn(view.sch, view.shown) : view.sch;
  view.drawn = sch;
  var aspect = view.size.w > 0 && view.size.h > 0 ? view.size.w / view.size.h : 1.2;
  var lay = view.shown ? layoutOntology(window.dagre, sch, aspect, true) : view.whole;
  gfx.mount(view.canvas, placeElements(sch, lay), view.style, view.shown ? schemaId(view.shown) : '');
  gfx.select(selectionIds(sch, view.latest.sel));
  gfx.on(function (evt) { picked(evt, ctx); });
  drawBar(ctx);
}

function measure() {
  view.size = { w: view.canvas.clientWidth, h: view.canvas.clientHeight };
  view.whole = layoutOntology(window.dagre, view.sch, view.size.w > 0 && view.size.h > 0 ? view.size.w / view.size.h : 1.2);
}

function picked(evt, ctx) {
  var sel = '';
  if (evt.target === 'node') {
    var k = evt.data.schemaKind;
    if (k === 'type' && evt.open) {
      // The double-click's first click already pushed the selection: the
      // list takes that entry over, so Back leaves both in one step.
      var first = view.pushed;
      if (first && first.type === evt.data.type && Date.now() - first.at < DOUBLE_MS) ctx.replace(typeRoute(evt.data.type));
      else ctx.nav(typeRoute(evt.data.type));
      return;
    }
    if (k === 'type') sel = 'type:' + evt.data.type;
    else if (k === 'self') sel = 'rel:' + evt.data.from + '.' + evt.data.predicate;
    else if (k === 'any' || k === 'ghost') sel = 'type:any';
    else return;
  }
  if (evt.target === 'edge') sel = 'rel:' + evt.data.from + '.' + evt.data.predicate;
  // The picture stays where it is under the pointer; only the details change.
  var to = overviewOf(sel, view.shown ? { focus: view.shown } : { map: view.latest.map });
  if (ctx.nav(to)) view.pushed = { type: evt.data && evt.data.type, at: Date.now() };
}

// drawBar says what part of the schema is on screen and offers the other.
function drawBar(ctx) {
  var nodes = [];
  var sel = view.latest.sel, type = sel.indexOf('type:') === 0 ? sel.slice(5) : '';
  if (view.shown) {
    nodes.push(el('span', { class: 'dwhat' }, view.shown + ' and ' + plural(view.drawn.nodes.length - 1, 'neighbor')));
    if (declared(type) && type !== view.shown) nodes.push(el('a', { class: 'btn-ghost', href: format(overviewOf(sel, { focus: type })) }, 'Focus on ' + type));
    nodes.push(el('a', { class: 'btn-ghost', href: format(overviewOf(sel, { map: true })), title: 'Show every type (Esc)' }, 'Full map'));
  } else {
    nodes.push(el('span', { class: 'dwhat' }, plural(view.schema.declaredTypes.length, 'type')));
    if (declared(type)) nodes.push(el('a', { class: 'btn-ghost', href: format(overviewOf(sel, { focus: type })) }, 'Focus on ' + type));
  }
  nodes.push(el('button', { type: 'button', class: 'btn-ghost', title: 'Fit the diagram in view', on: { click: function () { gfx.fit(); } } }, 'Fit'));
  fill(view.bar, nodes);
}

function update(route, ctx, force) {
  if (!view || !view.sch) return;
  if (!force && view.entry !== ctx.entry()) keepViewport();
  view.latest = route;
  view.entry = ctx.entry();
  view.ready = ctx.ready;
  if (force) measure();
  if (force || around(route) !== view.shown) place(ctx);
  else { gfx.select(selectionIds(view.drawn, route.sel)); drawBar(ctx); }
  markRail(route);
  // A choice made in this panel replaces the panel; the keyboard follows it.
  view.follow = focusIsOurs() && (view.follow || document.activeElement !== document.body);
  details(route, ctx);
}

// markRail shows which type is selected, and where each leads from here.
function markRail(route) {
  Array.prototype.forEach.call(view.rail.querySelectorAll('li'), function (li) {
    var on = route.sel === 'type:' + li.getAttribute('data-type');
    li.classList.toggle('on', on);
    var a = li.querySelector('a');
    a.href = to('type:' + li.getAttribute('data-type'));
    if (on) a.setAttribute('aria-current', 'true'); else a.removeAttribute('aria-current');
  });
}

function details(route, ctx) {
  var seq = ++view.seq;
  var sel = route.sel || '';
  if (sel.indexOf('type:') === 0) {
    var name = sel.slice(5);
    if (name === 'any') { renderType(baseDetails(view.schema.base), true); return; }
    fill(view.details, [skeleton('Loading ' + name, 6)]);
    api.getType(name).then(function (tv) {
      if (!view || seq !== view.seq) return;
      try {
        renderType(typeDetails(tv, countOf(name), view.schema.base), false);
      } catch (bug) {
        console.error(bug);
        panel([el('p', { class: 'err' }, 'These details failed to render: ' + String(bug))]);
      }
    }, function (err) {
      if (!view || seq !== view.seq) return;
      panel([el('p', { class: 'err', role: 'alert' }, err.message || String(err))]);
    });
    return;
  }
  var rd = sel.indexOf('rel:') === 0 ? relationDetails(view.sch, sel) : null;
  if (rd) { renderRelation(rd); return; }
  renderWhole();
}

// renderWhole is the panel with nothing selected: how the schema is used, and
// the relations nothing uses.
function renderWhole() {
  var idle = [], used = 0;
  view.numbers.relations.forEach(function (r) {
    if (r.base) return;
    if (r.links) used += 1; else if (countOf(r.from)) idle.push(r);
  });
  var empty = view.schema.declaredTypes.filter(function (t) { return !countOf(t); });
  var nodes = [
    el('h2', null, 'The schema in use'),
    el('p', { class: 'sub' }, plural(view.schema.declaredTypes.length - empty.length, 'type') + ' with entities' + (empty.length ? ', ' + num(empty.length) + ' without' : '') + '. ' +
      plural(used, 'relation') + ' in use.'),
    el('p', { class: 'hint' }, 'Select a type or an arrow for its details. Double-click a type for its list.')
  ];
  if (idle.length) {
    nodes.push(el('h3', null, 'Relations nothing uses yet'));
    var ul = el('ul', { class: 'plainlist' });
    idle.forEach(function (r) {
      var li = el('li');
      li.appendChild(el('a', { href: to('rel:' + r.from + '.' + r.predicate), class: 'mono' }, r.from + '.' + r.predicate));
      ul.appendChild(li);
    });
    nodes.push(ul);
  }
  panel(nodes);
}

function table(headers, rows, label) {
  var t = el('table', { class: 'facts', 'aria-label': label });
  var thead = el('thead');
  var tr = el('tr');
  headers.forEach(function (h) { tr.appendChild(el('th', { scope: 'col' }, h)); });
  thead.appendChild(tr);
  t.appendChild(thead);
  var tbody = el('tbody');
  rows.forEach(function (cells) {
    var r = el('tr');
    cells.forEach(function (c) {
      var td = el('td');
      if (c instanceof Node) td.appendChild(c); else text(td, c);
      r.appendChild(td);
    });
    tbody.appendChild(r);
  });
  t.appendChild(tbody);
  var wrap = el('div', { class: 'tblwrap' });
  wrap.appendChild(t);
  return wrap;
}

function typeLink(name) {
  if (!name || name === 'any') return el('span', null, name ? 'any type' : '');
  return el('a', { href: to('type:' + name) }, name);
}

function typeLinks(list) {
  var span = el('span');
  list.split(' | ').forEach(function (t, i) {
    if (i) span.appendChild(tn(', '));
    span.appendChild(typeLink(t));
  });
  return span;
}

// shape is how many a relation may hold, and whether it must.
function shape(r) { return (r.many ? 'many' : 'one') + (r.required ? ', required' : ''); }

function renderType(d, isBase) {
  var head = [];
  head.push(el('h2', null, isBase ? 'What every type inherits' : d.name));
  if (d.when) head.push(el('p', { class: 'when' }, 'Captured when ' + d.when));
  if (!isBase) {
    head.push(el('a', { href: format(typeRoute(d.name)), class: 'btn' },
      d.count ? 'Open ' + (d.count === 1 ? 'the 1 entity' : 'the ' + num(d.count) + ' entities') : 'Open the empty list'));
  }
  var attrHead = ['Attribute', 'Type', 'Allowed values'];
  var attrRow = function (a) {
    var name = el('span');
    name.appendChild(el('span', { class: 'mono' }, a.name));
    if (a.required) name.appendChild(el('span', { class: 'tag' }, 'required'));
    return [name, a.type, a.allowed];
  };
  var relHead = ['Relation', 'To', 'Holds', 'Inverse', 'Links'];
  var relRow = function (r) {
    var name = el('span');
    // Stored relations open their own panel: the keyboard path to what a
    // click on an arrow shows.
    if (!r.derived && !r.inherited) {
      name.appendChild(el('a', { class: 'mono', href: to('rel:' + d.name + '.' + r.predicate) }, r.predicate));
    } else {
      name.appendChild(el('span', { class: 'mono' }, r.predicate));
    }
    if (r.derived) name.appendChild(el('span', { class: 'tag', title: 'Read from the inverse relation, never stored' }, 'derived'));
    return [name, typeLinks(r.to), shape(r), r.inverse, r.derived || isBase ? '' : num(relationOf(view.numbers, d.name, r.predicate).links)];
  };
  var ownAttrs = d.attributes.filter(function (a) { return isBase || !a.inherited; });
  var baseAttrs = isBase ? [] : d.attributes.filter(function (a) { return a.inherited; });
  var ownRels = d.relations.filter(function (r) { return isBase || !r.inherited; });
  var baseRels = isBase ? [] : d.relations.filter(function (r) { return r.inherited; });

  head.push(el('h3', null, 'Attributes'));
  head.push(ownAttrs.length ? table(attrHead, ownAttrs.map(attrRow), 'Attributes') : el('p', { class: 'hint' }, 'None of its own.'));
  head.push(el('h3', null, 'Relations'));
  head.push(ownRels.length ? table(relHead, ownRels.map(relRow), 'Relations') : el('p', { class: 'hint' }, 'None of its own.'));
  var incoming = isBase ? [] : incomingRelations(view.schema, d.name, view.numbers);
  if (incoming.length) {
    head.push(el('h3', null, 'Incoming'));
    head.push(table(['Relation', 'From', 'Holds', 'Links'], incoming.map(function (r) {
      return [el('a', { class: 'mono', href: to('rel:' + r.from + '.' + r.predicate) }, r.predicate),
        typeLink(r.from), shape(r), num(r.used)];
    }), 'Incoming relations'));
  }
  if (baseAttrs.length || baseRels.length) {
    var more = el('details', { class: 'inherited' });
    more.appendChild(el('summary', null, 'Inherited from the base: ' + plural(baseAttrs.length, 'attribute') +
      ', ' + plural(baseRels.length, 'relation')));
    if (baseAttrs.length) more.appendChild(table(attrHead, baseAttrs.map(attrRow), 'Inherited attributes'));
    if (baseRels.length) more.appendChild(table(relHead, baseRels.map(relRow), 'Inherited relations'));
    head.push(more);
  }
  panel(head);
}

function renderRelation(d) {
  var rows = [
    ['From', typeLink(d.from)],
    ['To', typeLinks(d.to)],
    ['Holds', d.many ? 'many' : 'one'],
    ['Required', d.required ? 'yes' : 'no'],
    ['Inverse', d.inverse || 'none'],
    ['May loop', d.acyclic ? 'no' : 'yes'],
    ['Links', num(d.used)]
  ];
  var dl = el('dl', { class: 'kvlist' });
  rows.forEach(function (r) {
    dl.appendChild(el('dt', null, r[0]));
    var dd = el('dd');
    if (r[1] instanceof Node) dd.appendChild(r[1]); else text(dd, r[1]);
    dl.appendChild(dd);
  });
  var out = [el('h2', { class: 'mono' }, d.predicate), dl];
  if (d.from !== 'any') {
    out.push(el('h3', null, 'Coverage'));
    out.push(coverage(d));
    out.push(el('a', { href: format(typeRoute(d.from)), class: 'btn' }, 'Open the ' + d.from + ' list'));
  }
  panel(out);
}

// coverage says how many entities of the relation's type have one, each number
// the way to the entities it counts.
function coverage(d) {
  var c = relationOf(view.numbers, d.from, d.predicate);
  var total = countOf(d.from), none = total - c.have;
  var to = function (bound) { return format(listRoute(d.from, null, { count: ['out:' + d.predicate + bound] })); };
  var p = el('p', { class: 'coverage' });
  if (!total) { text(p, 'No ' + d.from + ' has been captured yet.'); return p; }
  p.appendChild(el('a', { href: to('>=1') }, num(c.have) + ' of ' + num(total)));
  p.appendChild(tn(' have one, '));
  p.appendChild(el('a', { href: to('=0') }, num(none)));
  p.appendChild(tn(none === 1 ? ' does not' : ' do not'));
  return p;
}

// focusIsOurs says the keyboard is in the panel, or was on something the panel
// has since replaced: a reader who has moved on keeps the focus.
function focusIsOurs() {
  return document.activeElement === document.body || view.details.contains(document.activeElement);
}

// panel puts a panel on screen and, when the choice came from the panel before
// it, the keyboard on its heading.
function panel(nodes) {
  var follow = view.follow && focusIsOurs();
  view.follow = false;
  fill(view.details, nodes);
  view.details.scrollTop = 0;
  var heading = view.details.querySelector('h2');
  if (follow && heading) {
    heading.tabIndex = -1;
    heading.focus({ preventScroll: true });
  }
  view.ready();
}
