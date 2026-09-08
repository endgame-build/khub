// panels.js — the slice renderers.
//
// Grouped by capability, not by screen region: everything here reads state and
// builds DOM through dom.js, and none of it touches cytoscape or fetch. Each
// renderer is idempotent and reads only from the arguments it is given.

import { el, fill, kv } from './dom.js';
import { search, legalPredicates, edgeTargets, typeOf, buildRows, sortRows } from './model.js';

var dispatch = function () {};
export function init(d) { dispatch = d; }

var SEARCH_LIMIT = 50;

export function renderCrumbs(node, state, model) {
  var parts = [
    el('button', { class: 'crumb', on: { click: function () { dispatch({ type: 'NAV_ROOT' }); } } }, 'All types')
  ];
  state.focus.forEach(function (f, i) {
    parts.push(el('span', { class: 'sep' }, '›'));
    var label = f.kind === 'type' ? f.name : (nameOf(model, f.id) || f.id);
    parts.push(el('button', {
      class: 'crumb',
      on: { click: function () { dispatch({ type: 'NAV_CRUMB', i: i + 1 }); } }
    }, label));
  });
  fill(node, parts);
}

export function renderCounts(node, state, model, visible) {
  var text = model.nodes.length + ' entities · ' + model.edges.length + ' edges';
  if (state.focus.length) text += ' · showing ' + visible.nodes.size;
  fill(node, [el('span', null, text)]);
}

// renderFilters is one panel with two sections sharing one row shape. The
// legend and the predicate filter were separate widgets doing the same job; a
// single Filters panel is also where "the universal predicates start hidden"
// can actually be read.
export function renderFilters(node, state, model, schema, colors, typeOrder) {
  var rows = [];

  rows.push(el('div', { class: 'cap' }, 'Types'));
  var types = typeOrder;
  types.forEach(function (t, i) {
    var count = (model.byType.get(t) || []).length;
    var hidden = state.hiddenTypes.has(t);
    var row = el('button', {
      class: 'frow' + (hidden ? ' hidden' : ''),
      on: { click: function () { dispatch({ type: 'TOGGLE_FILTER', axis: 'types', key: t }); } }
    });
    var sw = el('span', { class: 'sw' });
    // CSSOM, not a style attribute: CSP's style-src 'self' blocks the
    // attribute, while property setters are not CSP-governed.
    sw.style.backgroundColor = colors[i % colors.length];
    row.appendChild(sw);
    row.appendChild(el('span', { class: 'nm' }, t));
    row.appendChild(el('span', { class: 'bdg' }, String(count)));
    rows.push(row);
  });

  rows.push(el('div', { class: 'cap' }, 'Predicates'));
  rows.push(el('div', { class: 'hint' },
    'The four universal edges are available on every type, so they start hidden — shown, they connect almost everything to almost everything.'));

  var predicates = new Set();
  model.edges.forEach(function (e) { predicates.add(e.data.label); });
  // Derived inverses have no instance edges of their own — they are computed
  // from the forward half — so the schema is the only place they appear.
  (schema.derivedRelations || []).forEach(function (p) { predicates.add(p); });
  Array.from(predicates).sort().forEach(function (p) {
    var hidden = state.hiddenPredicates.has(p);
    var row = el('button', {
      class: 'frow' + (hidden ? ' hidden' : ''),
      on: { click: function () { dispatch({ type: 'TOGGLE_FILTER', axis: 'predicates', key: p }); } }
    });
    row.appendChild(el('span', { class: 'nm mono' }, p));
    if (schema.baseRelations.indexOf(p) !== -1) row.appendChild(el('span', { class: 'tag' }, 'base'));
    if ((schema.derivedRelations || []).indexOf(p) !== -1) row.appendChild(el('span', { class: 'tag' }, 'derived'));
    rows.push(row);
  });

  fill(node, rows);
}

// renderSearch is a finder, not a second filter. It never dims the graph:
// that would be a filter axis competing with the level system, and the
// expensive one at that.
export function renderSearch(node, state, model) {
  if (!state.search.trim()) { node.hidden = true; fill(node, []); return; }
  var res = search(model, state.search, SEARCH_LIMIT);
  var rows = res.hits.map(function (h) {
    var row = el('button', {
      class: 'hit',
      on: { click: function () { dispatch({ type: 'NAV_ENTITY', id: h.id }); } }
    });
    row.appendChild(el('span', { class: 'nm' }, h.label));
    row.appendChild(el('span', { class: 'tag' }, typeOf(h.id)));
    return row;
  });
  if (!res.total) rows.push(el('div', { class: 'hint' }, 'Nothing matches'));
  else if (res.total > res.hits.length) {
    rows.push(el('div', { class: 'hint' }, '…and ' + (res.total - res.hits.length) + ' more'));
  }
  node.hidden = false;
  fill(node, rows);
}

export function renderInspector(node, state, model) {
  // Empty state carries the level's context rather than an instruction nobody
  // needs twice.
  if (!state.inspect) { fill(node, levelContext(state, model)); return; }
  if (state.inspect.kind === 'relation') { fill(node, declaredRelation(state.inspect.data)); return; }
  if (state.inspect.kind === 'type') { fill(node, declaredType(state.inspect, model)); return; }
  if (state.inspect.status === 'loading') {
    fill(node, [el('div', { class: 'cap' }, 'Entity'), el('div', { class: 'hint' }, 'Loading…')]);
    return;
  }
  if (state.inspect.status === 'error') {
    fill(node, [
      el('div', { class: 'cap' }, 'Entity'),
      el('div', { class: 'err' }, state.inspect.error)
    ]);
    return;
  }

  var d = state.inspect.data;
  var parts = [el('div', { class: 'cap' }, 'Entity')];

  var head = el('div', { class: 'card' });
  var name = (d.frontmatter && d.frontmatter.title) || d.slug;
  head.appendChild(el('div', { class: 'ttl' }, name));
  // The slug rides beside the type: it is the name, but the slug is the
  // argument every khub command actually takes.
  head.appendChild(el('div', { class: 'sub' }, d.type + ' · ' + d.slug));
  head.appendChild(el('div', { class: 'path mono' }, d.path));
  var picks = el('div', { class: 'btnrow' });
  picks.appendChild(el('button', {
    class: 'btn-ghost',
    on: { click: function () { dispatch({ type: 'COMPOSE_PICK', slot: 'a', id: d.id }); } }
  }, 'Set as A'));
  picks.appendChild(el('button', {
    class: 'btn-ghost',
    on: { click: function () { dispatch({ type: 'COMPOSE_PICK', slot: 'b', id: d.id }); } }
  }, 'Set as B'));
  head.appendChild(picks);
  parts.push(head);

  var meta = d.frontmatter || {};
  var keys = Object.keys(meta).filter(function (k) { return k !== 'type'; });
  if (keys.length) {
    var facts = el('div', { class: 'card' });
    keys.forEach(function (k) { facts.appendChild(kv(k, format(meta[k]))); });
    parts.push(facts);
  }

  if (d.edges && d.edges.length) {
    parts.push(el('div', { class: 'cap' }, 'Edges'));
    var list = el('div', { class: 'card' });
    d.edges.forEach(function (e) {
      var targets = edgeTargets(model, e);
      var row = el('div', { class: 'erow' });
      row.appendChild(el('span', { class: 'k mono' }, e.predicate));
      if (targets.length) {
        targets.forEach(function (target) {
          row.appendChild(el('button', {
            class: 'link',
            on: { click: function () { dispatch({ type: 'NAV_ENTITY', id: target }); } }
          }, targets.length > 1 ? target : e.target));
        });
      } else {
        row.appendChild(el('span', { class: 'v' }, e.target));
      }
      row.appendChild(el('span', { class: 'tag' }, e.derived ? 'derived' : 'stored'));
      if (!e.derived) {
        row.appendChild(el('button', {
          class: 'link warn',
          on: {
            click: function () {
              dispatch({ type: 'COMPOSE_UNLINK', source: d.id, predicate: e.predicate, target: e.target });
            }
          }
        }, 'unlink'));
      }
      list.appendChild(row);
    });
    parts.push(list);
  }

  if (d.body && d.body.trim()) {
    parts.push(el('div', { class: 'cap' }, 'Body'));
    parts.push(el('pre', { class: 'body' }, d.body));
  }

  fill(node, parts);
}

// declaredType renders what a type is allowed to be — the record `khub schema
// show <type>` prints, which is why the vocabulary here is introspect's.
function declaredType(inspect, model) {
  if (inspect.status === 'loading') {
    return [el('div', { class: 'cap' }, 'Type'), el('div', { class: 'hint' }, 'Loading…')];
  }
  if (inspect.status === 'error') {
    return [el('div', { class: 'cap' }, 'Type'), el('div', { class: 'err' }, inspect.error)];
  }
  var d = inspect.data;
  var parts = [el('div', { class: 'cap' }, 'Type')];

  var head = el('div', { class: 'card' });
  head.appendChild(el('div', { class: 'ttl' }, d.name));
  head.appendChild(kv('stored as', d.layout + ' · ' + (d.format || '')));
  if (d.id_shape) head.appendChild(kv('id', d.id_shape));
  var count = (model.byType.get(d.name) || []).length;
  head.appendChild(kv('entities', String(count)));
  if (count) {
    // The explicit second step out of the ontology and into the data. Clicking
    // the node itself only ever explains the type; nothing about a click
    // silently changes which view you are in.
    var open = el('div', { class: 'btnrow' });
    open.appendChild(el('button', {
      class: 'btn-primary',
      on: { click: function () { dispatch({ type: 'OPEN_TYPE', name: d.name }); } }
    }, 'Open ' + count + ' entities'));
    head.appendChild(open);
  }
  if (d.required) head.appendChild(kv('required', 'yes'));
  if (d.orphan) head.appendChild(kv('orphan-exempt', 'yes'));
  parts.push(head);

  if (d.when) {
    parts.push(el('div', { class: 'cap' }, 'Capture when'));
    parts.push(el('div', { class: 'hint' }, d.when));
  }

  if (d.fields && d.fields.length) {
    parts.push(el('div', { class: 'cap' }, 'Fields'));
    var fcard = el('div', { class: 'card' });
    d.fields.forEach(function (f) {
      var bits = [f.type];
      if (f.required) bits.push('required');
      if (f.enum) bits.push(f.enum.join(' | '));
      if (f.pattern) bits.push('pattern');
      fcard.appendChild(kv(f.name, bits.join(' · ')));
    });
    parts.push(fcard);
  }

  if (d.relations && d.relations.length) {
    parts.push(el('div', { class: 'cap' }, 'Relations'));
    var rcard = el('div', { class: 'card' });
    d.relations.forEach(function (r) {
      var row = el('div', { class: 'erow' });
      row.appendChild(el('span', { class: 'k mono' }, r.predicate));
      row.appendChild(el('span', { class: 'v' }, (r.to || []).join(', ')));
      if (r.many) row.appendChild(el('span', { class: 'tag' }, 'many'));
      if (r.required) row.appendChild(el('span', { class: 'tag' }, 'required'));
      // A derived relation is computed at read time from someone else's
      // forward edge; nobody authors it, and that is worth saying.
      if (r.derived) row.appendChild(el('span', { class: 'tag' }, 'derived from ' + r.inverse));
      rcard.appendChild(row);
    });
    parts.push(rcard);
  }
  return parts;
}

// declaredRelation explains one edge of the ontology, including whether any
// entity has ever used it.
function declaredRelation(d) {
  var card = el('div', { class: 'card' });
  card.appendChild(el('div', { class: 'ttl' }, d.predicate));
  card.appendChild(kv('cardinality', d.many ? 'many' : 'one'));
  card.appendChild(kv('required', d.required ? 'yes' : 'no'));
  if (d.inverse) card.appendChild(kv(d.derived ? 'derived from' : 'inverse', d.inverse));
  if (d.acyclic) card.appendChild(kv('acyclic', 'yes'));
  card.appendChild(kv('in use', d.used ? String(d.used) + ' edges' : 'never'));
  return [
    el('div', { class: 'cap' }, d.derived ? 'Derived relation' : 'Declared relation'),
    card,
    el('div', { class: 'hint' }, d.used
      ? 'Dimmed edges are declared but unused.'
      : 'Nothing uses this relation yet — the contract allows it, the data has not taken it up.')
  ];
}

function levelContext(state, model) {
  if (!state.focus.length) {
    return [
      el('div', { class: 'cap' }, 'Overview'),
      el('div', { class: 'hint' },
        'Each circle is a type, sized by how many entities it holds. Click one to open it.')
    ];
  }
  var t = state.focus[0].name;
  var n = (model.byType.get(t) || []).length;
  var card = el('div', { class: 'card' });
  card.appendChild(el('div', { class: 'ttl' }, t));
  card.appendChild(kv('entities', String(n)));
  return [
    el('div', { class: 'cap' }, 'Type'),
    card,
    el('div', { class: 'hint' }, 'Click an entity to inspect it.')
  ];
}

export function renderComposer(node, state, schema) {
  var c = state.compose;
  var parts = [el('div', { class: 'cap' }, 'Compose')];

  var slots = el('div', { class: 'card' });
  slots.appendChild(kv('A', c.a || 'not set'));
  slots.appendChild(kv('B', c.b || 'not set'));

  var legal = (c.a && c.b) ? legalPredicates(schema, typeOf(c.a), typeOf(c.b)) : [];
  var select = el('select', { class: 'ctl', disabled: !legal.length });
  select.appendChild(el('option', { value: '' }, legal.length ? 'Choose a predicate' : 'No legal predicate'));
  legal.forEach(function (p) {
    var o = el('option', { value: p }, p);
    if (p === c.predicate) o.selected = true;
    select.appendChild(o);
  });
  select.addEventListener('change', function () {
    dispatch({ type: 'COMPOSE_PREDICATE', predicate: select.value || null });
  });
  slots.appendChild(select);

  var row = el('div', { class: 'btnrow' });
  row.appendChild(el('button', {
    class: 'btn-primary',
    disabled: !(c.a && c.b && c.predicate),
    on: { click: function () { dispatch({ type: 'COMPOSE_ENQUEUE' }); } }
  }, 'Queue link'));
  row.appendChild(el('button', {
    class: 'btn-ghost',
    on: { click: function () { dispatch({ type: 'COMPOSE_CLEAR' }); } }
  }, 'Clear'));
  slots.appendChild(row);
  parts.push(slots);

  if (c.a && c.b && !legal.length) {
    parts.push(el('div', { class: 'hint' },
      'The schema declares no relation from ' + typeOf(c.a) + ' to ' + typeOf(c.b) + '.'));
  }

  if (c.queue.length) {
    parts.push(el('div', { class: 'cap' }, 'Queued · ' + c.queue.length));
    // A readonly textarea: native select-all, and .value is a text-only sink,
    // so the queue cannot parse markup no matter what an entity body contains.
    var ta = el('textarea', { class: 'queue mono', readOnly: true, rows: Math.min(8, c.queue.length + 1) });
    ta.value = c.queue.join('\n');
    parts.push(ta);
    var qrow = el('div', { class: 'btnrow' });
    qrow.appendChild(el('button', {
      class: 'btn-primary',
      on: {
        click: function () {
          // Loopback is a potentially-trustworthy origin, so the Clipboard API
          // is available over plain HTTP here. Selecting the textarea is the
          // fallback when it is not.
          if (navigator.clipboard && navigator.clipboard.writeText) {
            navigator.clipboard.writeText(ta.value).then(function () {
              dispatch({ type: 'NOTICE', text: 'Copied ' + c.queue.length + ' command(s)' });
            }, function () { ta.select(); });
          } else {
            ta.select();
          }
        }
      }
    }, 'Copy all'));
    qrow.appendChild(el('button', {
      class: 'btn-ghost',
      on: { click: function () { dispatch({ type: 'COMPOSE_CLEAR' }); } }
    }, 'Discard'));
    parts.push(qrow);
    parts.push(el('div', { class: 'hint' }, 'Paste these into a terminal. khub validates every one.'));
  }

  fill(node, parts);
}

export function renderNotice(node, text) {
  if (!text) { node.hidden = true; fill(node, []); return; }
  node.hidden = false;
  fill(node, [el('span', null, text)]);
}

// format renders a frontmatter value. Only facts that exist — an absent value
// prints nothing rather than a placeholder dash.
function format(v) {
  if (v === null || v === undefined) return '';
  if (Array.isArray(v)) return v.join(', ');
  if (typeof v === 'object') return Object.keys(v).map(function (k) { return k + ': ' + v[k]; }).join(', ');
  return String(v);
}

// nameOf is the display name of a node in the graph payload, or '' when the id
// names nothing loaded.
function nameOf(model, id) {
  var d = model.byId.get(id);
  return d ? (d.display || '') : '';
}

// COLUMNS is the table's shape in one place: key, header, and whether the value
// is a number (which decides alignment and the first sort direction).
var COLUMNS = [
  { key: 'name', label: 'Name' },
  { key: 'slug', label: 'Slug' },
  { key: 'type', label: 'Type' },
  { key: 'flags', label: 'State' },
  { key: 'degree', label: 'Edges', numeric: true }
];

// renderList is L1's default rendering: a type is a set of entities to pick
// from, not a shape to read. A real table rather than the canvas grid it
// replaces, because sorting, keyboard access and — the point — selecting a slug
// to paste into a khub command are all things a table gives and a canvas cannot.
export function renderList(node, state, model, visible, colors, typeOrder) {
  var rows = sortRows(buildRows(model, visible.nodes, state.hiddenPredicates), state.sort);

  // The type column is dead weight while every row shares a type, which is the
  // whole of depth 0. It earns its place once + pulls neighbours in.
  var types = new Set();
  rows.forEach(function (r) { types.add(r.type); });
  var showType = types.size > 1;
  var cols = COLUMNS.filter(function (c) { return c.key !== 'type' || showType; });

  var table = el('table');
  var head = el('tr');
  cols.forEach(function (c) {
    var th = el('th', { class: c.numeric ? 'num' : null });
    var active = state.sort.key === c.key;
    var glyph = active ? (state.sort.dir === 'asc' ? ' \u2191' : ' \u2193') : '';
    th.appendChild(el('button', {
      class: 'sortbtn' + (active ? ' on' : ''),
      on: { click: function () { dispatch({ type: 'SET_SORT', key: c.key }); } }
    }, c.label + glyph));
    head.appendChild(th);
  });
  var thead = el('thead');
  thead.appendChild(head);
  table.appendChild(thead);

  var body = el('tbody');
  rows.forEach(function (r) {
    var tr = el('tr');

    // Only the name cell is a control. Wrapping the whole row in a button would
    // turn a drag into a click and make the slug impossible to select, which is
    // the one thing this view exists to allow.
    var nameCell = el('td');
    nameCell.appendChild(el('button', {
      class: 'rowlink',
      on: { click: function () { dispatch({ type: 'NAV_ENTITY', id: r.id }); } }
    }, r.name));
    tr.appendChild(nameCell);

    tr.appendChild(el('td', { class: 'mono slug' }, r.slug));

    if (showType) {
      var typeCell = el('td');
      var sw = el('span', { class: 'sw' });
      var i = typeOrder.indexOf(r.type);
      sw.style.backgroundColor = colors[(i < 0 ? 0 : i) % colors.length];
      typeCell.appendChild(sw);
      typeCell.appendChild(el('span', null, r.type));
      tr.appendChild(typeCell);
    }

    var flags = el('td');
    if (r.draft) flags.appendChild(el('span', { class: 'tag' }, 'draft'));
    if (r.orphan) flags.appendChild(el('span', { class: 'tag warn' }, 'orphan'));
    tr.appendChild(flags);

    tr.appendChild(el('td', { class: 'num' }, String(r.degree)));
    body.appendChild(tr);
  });
  table.appendChild(body);

  var parts = [table];
  if (!rows.length) parts = [el('div', { class: 'hint' }, 'Nothing here')];
  fill(node, parts);
}
