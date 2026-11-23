// overview.js — the ontology: a diagram of the workspace's types and the
// relations between them, with the selected type's or relation's details
// beside it. The selection lives in the address (`#/?sel=…`).

import * as api from './api.js';
import * as gfx from './cy.js';
import { el, fill, clear, text } from './dom.js';
import { notice } from './widgets.js';
import {
  buildModel, indexSchema, buildSchemaGraph, usedEdgeCounts, schemaLayout,
  selectionIds, typeDetails, baseDetails, relationDetails
} from './model.js';
import { format, typeRoute } from './route.js';

var view = null; // { root, diagram, details, sch, schema, model, seq }

// unmount releases the diagram when another view takes the page.
export function unmount() {
  gfx.destroy();
  view = null;
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
  var diagram = el('div', { id: 'cy', class: 'diagram', role: 'img', 'aria-label': 'Ontology diagram' });
  var details = el('aside', { class: 'details', 'aria-live': 'polite' });
  var root = el('section', { class: 'overview' });
  root.appendChild(diagram);
  root.appendChild(details);
  fill(main, [root]);
  fill(details, [el('p', { class: 'hint' }, 'Loading the ontology…')]);
  view = { root: root, diagram: diagram, details: details, seq: 0, latest: route };
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

function draw(r, ctx) {
  var diagram = view.diagram;
  var model = buildModel(r[0]);
  var schema = indexSchema(r[1]);
  // Present types first, sorted, then declared-but-empty ones — viz's own
  // colour order, so a type is the same colour here and in a viz file.
  var present = Array.from(model.byType.keys()).sort();
  var typeOrder = present.concat(schema.declaredTypes.filter(function (t) {
    return !model.byType.has(t);
  }).sort());
  var sch = buildSchemaGraph(schema, schema.edges, usedEdgeCounts(model), model.byType);
  view.model = model;
  view.schema = schema;
  view.sch = sch;
  gfx.mount(diagram, sch, gfx.buildStyle(typeOrder, ctx.colors, ctx.ink),
    schemaLayout(schema.declaredTypes.length));
  gfx.on(function (evt) {
    var sel = '';
    if (evt.target === 'node') sel = 'type:' + (evt.data.schemaKind === 'any' ? 'any' : evt.data.type);
    if (evt.target === 'edge') sel = 'rel:' + evt.data.from + '.' + evt.data.predicate;
    ctx.nav({ view: 'overview', sel: sel });
  });
  update(view.latest, ctx);
}

function update(route, ctx) {
  if (!view || !view.sch) return;
  gfx.select(selectionIds(view.sch, route.sel));
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
        fill(view.details, [el('p', { class: 'err' }, 'These details failed to render: ' + String(bug))]);
      }
    }, function (err) {
      if (!view || seq !== view.seq) return;
      fill(view.details, [el('p', { class: 'err' }, err.message || String(err)), index(ctx)]);
    });
    return;
  }
  if (sel.indexOf('rel:') === 0) {
    var rd = relationDetails(view.sch, sel);
    if (rd) { renderRelation(rd, ctx); return; }
  }
  fill(view.details, [
    el('div', { class: 'eyebrow' }, 'Ontology'),
    el('h2', null, view.schema.declaredTypes.length + ' types'),
    el('p', { class: 'hint' }, 'Select a type in the diagram, or below, to see its attributes and relations. Select an arrow to see a relation.'),
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
    name.appendChild(el('span', { class: 'mono' }, r.predicate));
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
  if (baseAttrs.length || baseRels.length) {
    var more = el('details', { class: 'inherited' });
    more.appendChild(el('summary', null, 'Inherited from the base: ' + baseAttrs.length +
      ' attributes, ' + baseRels.length + ' relations'));
    if (baseAttrs.length) more.appendChild(table(attrHead, baseAttrs.map(attrRow)));
    if (baseRels.length) more.appendChild(table(relHead, baseRels.map(relRow)));
    head.push(more);
  }
  fill(view.details, head);
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
    out.push(el('a', { href: format(typeRoute(d.from)), class: 'btn' }, 'Open the ' + d.from + ' list →'));
  }
  clear(view.details);
  fill(view.details, out);
}
