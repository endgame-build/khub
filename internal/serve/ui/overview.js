// overview.js — the ontology: a diagram of the workspace's types and the
// relations between them, with the selected type's or relation's details
// beside it. The selection lives in the address (`#/?sel=…`).

import * as api from './api.js';
import * as gfx from './cy.js';
import { el, fill, text, tn } from './dom.js';
import { notice } from './widgets.js';
import {
  buildModel, indexSchema, buildSchemaGraph, usedEdgeCounts,
  selectionIds, typeDetails, baseDetails, relationDetails,
  incomingRelations, relationCoverage
} from './model.js';
import { layoutOntology, placeElements, BOX_H, GROUP_PAD } from './layout.js';
import { format, typeRoute, listRoute } from './route.js';

var RELAYOUT_MS = 120; // how long the canvas must hold a size before a relayout
var DOUBLE_MS = 600;   // how close a click and the double-click it began are

var view = null; // { root, canvas, details, latest, sch, schema, model, used, style, seq, entry, size, watch, timer, pushed, follow }

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
  if (view.entry && view.size) view.entry.viewport = { at: gfx.viewport(), size: view.size };
}

// show renders the overview for route. Entering the view loads fresh data;
// a selection change on the open view only redraws the details.
export function show(main, route, ctx) {
  ctx.crumbs([{ label: 'Ontology' }]);
  if (view && view.root.isConnected) {
    // Still loading: the load lands on whatever the address says by then.
    view.latest = route;
    update(route, ctx);
    return;
  }
  unmount();
  var canvas = el('div', { id: 'cy', role: 'img', 'aria-label': 'Ontology diagram: types and the relations between them' });
  var diagram = el('div', { class: 'diagram' });
  diagram.appendChild(canvas);
  diagram.appendChild(el('button', {
    type: 'button', class: 'btn-ghost fitbtn', title: 'Fit the whole diagram in view',
    on: { click: function () { gfx.fit(); } }
  }, 'Fit'));
  diagram.appendChild(legend());
  var details = el('aside', { class: 'details', 'aria-live': 'polite' });
  var root = el('section', { class: 'overview' });
  root.appendChild(diagram);
  root.appendChild(details);
  fill(main, [root]);
  fill(details, [el('p', { class: 'hint' }, 'Loading the ontology…')]);
  view = { root: root, canvas: canvas, details: details, seq: 0, latest: route };
  var mine = view;

  Promise.all([api.getGraph(), api.getSchema()]).then(function (r) {
    if (view !== mine) return;
    try { draw(r, ctx); } catch (bug) {
      // A page bug, not a workspace problem: say so, and keep the stack.
      console.error(bug);
      fill(main, [notice('The ontology failed to render', String(bug))]);
    }
  }, function (err) {
    if (view !== mine) return;
    fill(main, [notice('Could not load the ontology', err.message || String(err))]);
  });
}

// legend explains the notation, as a diagram's legend should, beside it.
function legend() {
  var box = el('div', { class: 'legend', 'aria-label': 'Legend' });
  [
    ['solid', 'required relation'],
    ['dashed', 'optional relation'],
    ['dotted', 'declared, not used yet'],
    ['ghost', 'any type'],
    ['tag', 'relation to its own type']
  ].forEach(function (row) {
    var item = el('span', { class: 'lg' });
    item.appendChild(el('span', { class: 'sw sw-' + row[0], 'aria-hidden': 'true' }));
    item.appendChild(el('span', null, row[1]));
    box.appendChild(item);
  });
  box.appendChild(el('span', { class: 'lg hint' }, 'Click a type for details · double-click to open its list'));
  return box;
}

function draw(r, ctx) {
  var canvas = view.canvas;
  var model = buildModel(r[0]);
  var used = usedEdgeCounts(model);
  var schema = indexSchema(r[1]);
  // Present types first, sorted, then declared-but-empty ones — viz's own
  // colour order, so a type is the same colour here and in a viz file.
  var present = Array.from(model.byType.keys()).sort();
  var typeOrder = present.concat(schema.declaredTypes.filter(function (t) {
    return !model.byType.has(t);
  }).sort());
  var sch = buildSchemaGraph(schema, schema.edges, used, model.byType);
  view.model = model;
  view.schema = schema;
  view.sch = sch;
  view.used = used;
  view.style = gfx.buildStyle(typeOrder, ctx.colors, ctx.ink, BOX_H, GROUP_PAD);
  // The details fill first: below the diagram on a narrow window, they are
  // part of what decides the canvas's shape.
  view.entry = ctx.entry();
  var kept = view.entry.viewport;
  update(view.latest, ctx);
  place(ctx);
  if (kept && kept.at && kept.size.w === view.size.w && kept.size.h === view.size.h) gfx.viewport(kept.at);
  var mine = view;
  view.watch = new ResizeObserver(function () {
    if (view !== mine) return;
    clearTimeout(mine.timer);
    mine.timer = setTimeout(function () {
      if (view === mine && (canvas.clientWidth !== mine.size.w || canvas.clientHeight !== mine.size.h)) place(ctx);
    }, RELAYOUT_MS);
  });
  view.watch.observe(canvas);
  ctx.ready();
}

// place lays the diagram out against the canvas's shape and draws it.
function place(ctx) {
  var canvas = view.canvas;
  view.size = { w: canvas.clientWidth, h: canvas.clientHeight };
  var aspect = view.size.w > 0 && view.size.h > 0 ? view.size.w / view.size.h : 1.2;
  var lay = layoutOntology(window.dagre, view.sch, aspect);
  gfx.mount(canvas, placeElements(view.sch, lay), view.style);
  gfx.select(selectionIds(view.sch, view.latest.sel));
  gfx.on(function (evt) {
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
    if (ctx.nav({ view: 'overview', sel: sel })) view.pushed = { type: evt.data && evt.data.type, at: Date.now() };
  });
}

function update(route, ctx) {
  if (!view || !view.sch) return;
  view.latest = route;
  if (view.entry !== ctx.entry()) keepViewport();
  view.entry = ctx.entry();
  view.ready = ctx.ready;
  gfx.select(selectionIds(view.sch, route.sel));
  // A choice made in this panel replaces the panel; the keyboard follows it.
  view.follow = focusIsOurs() && (view.follow || document.activeElement !== document.body);
  var seq = ++view.seq;
  var sel = route.sel || '';
  if (sel.indexOf('type:') === 0) {
    var name = sel.slice(5);
    if (name === 'any') {
      renderType(baseDetails(view.schema.base), ctx, true);
      return;
    }
    fill(view.details, [el('p', { class: 'hint' }, 'Loading ' + name + '…')]);
    api.getType(name).then(function (tv) {
      if (!view || seq !== view.seq) return;
      var count = view.model.byType.get(name) ? view.model.byType.get(name).length : 0;
      try {
        renderType(typeDetails(tv, count, view.schema.base), ctx, false);
      } catch (bug) {
        console.error(bug);
        panel([el('p', { class: 'err' }, 'These details failed to render: ' + String(bug))]);
      }
    }, function (err) {
      if (!view || seq !== view.seq) return;
      panel([el('p', { class: 'err' }, err.message || String(err)), index(ctx)]);
    });
    return;
  }
  if (sel.indexOf('rel:') === 0) {
    var rd = relationDetails(view.sch, sel);
    if (rd) { renderRelation(rd, ctx); return; }
  }
  panel([
    el('div', { class: 'eyebrow' }, 'Ontology'),
    el('h2', null, view.schema.declaredTypes.length + ' types'),
    el('p', { class: 'hint' }, 'Select a type in the diagram, or below, to see its attributes and relations; double-click it to open its list. Select an arrow to see a relation.'),
    index(ctx)
  ]);
}

// index lists every type with its count — the keyboard path into the diagram.
function index(ctx) {
  var ul = el('ul', { class: 'typeindex' });
  view.schema.declaredTypes.forEach(function (t) {
    var n = view.model.byType.get(t) ? view.model.byType.get(t).length : 0;
    var li = el('li');
    li.appendChild(el('a', { href: format({ view: 'overview', sel: 'type:' + t }), class: 'tname' }, t));
    li.appendChild(el('a', { href: format(typeRoute(t)), class: 'tcount', title: 'Open the list of ' + t }, n + ' →'));
    ul.appendChild(li);
  });
  return ul;
}

function table(headers, rows) {
  var t = el('table', { class: 'facts' });
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
  if (!name || name === 'any') return el('span', { class: 'mono' }, name || '');
  return el('a', { href: format({ view: 'overview', sel: 'type:' + name }), class: 'mono' }, name);
}

function typeLinks(list) {
  var span = el('span');
  list.split(' | ').forEach(function (t, i) {
    if (i) span.appendChild(el('span', { class: 'sep' }, ' | '));
    span.appendChild(typeLink(t));
  });
  return span;
}

function renderType(d, ctx, isBase) {
  var head = [el('div', { class: 'eyebrow' }, isBase ? 'Inherited by every type' : 'Type')];
  head.push(el('h2', null, d.name));
  if (d.when) head.push(el('p', { class: 'when' }, 'Captured when ' + d.when));
  if (!isBase) {
    var bar = el('div', { class: 'actions' });
    bar.appendChild(el('span', { class: 'count' }, d.count + (d.count === 1 ? ' entity' : ' entities')));
    bar.appendChild(el('a', { href: format(typeRoute(d.name)), class: 'btn' }, 'Open list →'));
    head.push(bar);
  }
  var attrHead = ['Name', 'Type', 'Required', 'Allowed values'];
  var attrRow = function (a) {
    return [el('span', { class: 'mono' }, a.name), a.type, a.required ? 'yes' : '', a.allowed];
  };
  var relHead = ['Name', 'To', 'Cardinality', 'Inverse'];
  var relRow = function (r) {
    var name = el('span');
    // Stored relations open their own panel: the keyboard path to what a
    // click on an arrow shows.
    if (!r.derived && !r.inherited) {
      name.appendChild(el('a', { class: 'mono', href: format({ view: 'overview', sel: 'rel:' + d.name + '.' + r.predicate }) }, r.predicate));
    } else {
      name.appendChild(el('span', { class: 'mono' }, r.predicate));
    }
    if (r.derived) name.appendChild(el('span', { class: 'tag', title: 'derived at read time from the inverse relation' }, 'derived'));
    return [name, typeLinks(r.to), (r.many ? 'many' : 'one') + (r.required ? ', required' : ''), r.inverse];
  };
  var ownAttrs = d.attributes.filter(function (a) { return isBase || !a.inherited; });
  var baseAttrs = isBase ? [] : d.attributes.filter(function (a) { return a.inherited; });
  var ownRels = d.relations.filter(function (r) { return isBase || !r.inherited; });
  var baseRels = isBase ? [] : d.relations.filter(function (r) { return r.inherited; });

  head.push(el('h3', null, 'Attributes'));
  head.push(ownAttrs.length ? table(attrHead, ownAttrs.map(attrRow)) : el('p', { class: 'hint' }, 'None of its own.'));
  head.push(el('h3', null, 'Relations'));
  head.push(ownRels.length ? table(relHead, ownRels.map(relRow)) : el('p', { class: 'hint' }, 'None of its own.'));
  var incoming = isBase ? [] : incomingRelations(view.schema, d.name, view.used);
  if (incoming.length) {
    head.push(el('h3', null, 'Incoming'));
    head.push(table(['Name', 'From', 'Cardinality', 'Links'], incoming.map(function (r) {
      return [el('a', { class: 'mono', href: format({ view: 'overview', sel: 'rel:' + r.from + '.' + r.predicate }) }, r.predicate),
        typeLink(r.from), (r.many ? 'many' : 'one') + (r.required ? ', required' : ''), String(r.used)];
    })));
  }
  if (baseAttrs.length || baseRels.length) {
    var more = el('details', { class: 'inherited' });
    more.appendChild(el('summary', null, 'Inherited from the base: ' + baseAttrs.length +
      ' attributes, ' + baseRels.length + ' relations'));
    if (baseAttrs.length) more.appendChild(table(attrHead, baseAttrs.map(attrRow)));
    if (baseRels.length) more.appendChild(table(relHead, baseRels.map(relRow)));
    head.push(more);
  }
  panel(head);
}

function renderRelation(d, ctx) {
  var rows = [
    ['From', typeLink(d.from)],
    ['To', typeLinks(d.to)],
    ['Cardinality', d.many ? 'many' : 'one'],
    ['Required', d.required ? 'yes' : 'no'],
    ['Inverse', d.inverse || '—'],
    ['Acyclic', d.acyclic ? 'yes' : 'no'],
    ['Links in use', String(d.used)]
  ];
  var dl = el('dl', { class: 'kvlist' });
  rows.forEach(function (r) {
    dl.appendChild(el('dt', null, r[0]));
    var dd = el('dd');
    if (r[1] instanceof Node) dd.appendChild(r[1]); else text(dd, r[1]);
    dl.appendChild(dd);
  });
  var out = [el('div', { class: 'eyebrow' }, 'Relation'), el('h2', { class: 'mono' }, d.predicate), dl];
  if (d.from !== 'any') {
    out.push(el('h3', null, 'Coverage'));
    out.push(coverage(d));
    out.push(el('a', { href: format(typeRoute(d.from)), class: 'btn' }, 'Open the ' + d.from + ' list →'));
  }
  panel(out);
}

// coverage says how many entities of the relation's type have one, each number
// the way to the entities it counts.
function coverage(d) {
  var c = relationCoverage(view.model, d.from, d.predicate);
  var to = function (bound) { return format(listRoute(d.from, null, { count: 'out:' + d.predicate + bound })); };
  var p = el('p', { class: 'coverage' });
  p.appendChild(el('a', { href: to('>=1') }, c.have + ' of ' + c.total));
  p.appendChild(tn(' have one, '));
  p.appendChild(el('a', { href: to('=0') }, String(c.total - c.have)));
  p.appendChild(tn(c.total - c.have === 1 ? ' does not' : ' do not'));
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
  var heading = view.details.querySelector('h2');
  if (follow && heading) {
    heading.tabIndex = -1;
    heading.focus({ preventScroll: true });
    view.details.scrollTop = 0;
  }
  view.ready();
}
