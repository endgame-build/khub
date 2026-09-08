// model.js — pure derivations over the graph payload. No document, no
// cytoscape, no fetch.

export function typeOf(id) {
  var i = id.indexOf('/');
  return i === -1 ? id : id.slice(0, i);
}

// SEP joins composite keys. A khub id, type and predicate are all drawn from a
// safe charset that cannot contain a NUL, so keys built with it are
// unambiguous by construction rather than by convention.
var SEP = '\u0000';

export function buildModel(payload) {
  var byType = new Map();
  var haystack = [];
  var ids = new Set();
  var byId = new Map();
  payload.nodes.forEach(function (n) {
    var d = n.data;
    if (!byType.has(d.type)) byType.set(d.type, []);
    byType.get(d.type).push(d.id);
    ids.add(d.id);
    byId.set(d.id, d);
    // What a human reads. `title` is optional in the schema, so the slug is the
    // fallback rather than a blank node. Written onto the node's own data
    // because cytoscape styles from data keys and cannot express a fallback in
    // a selector.
    d.display = d.title || d.label;
    // Search matches the name AND the slug: the name is what you remember, the
    // slug is what you paste into a command.
    haystack.push({
      id: d.id, label: d.display,
      lc: (d.id + ' ' + d.label + ' ' + d.display).toLowerCase()
    });
  });

  // Dedupe on the (source, predicate, target) triple BEFORE synthesizing ids.
  //
  // internal/graph/graph.go appends predicates to its adjacency without
  // deduplicating, so a relation list that resolves twice to the same node
  // yields two identical edge records. Synthesized ids would then collide and
  // cytoscape throws "Can not create second element with id", taking the whole
  // page down with it. Hand-authored corpora never trip this; generated ones do.
  var seen = new Set(), edges = [], dropped = 0;
  payload.edges.forEach(function (e) {
    var d = e.data;
    var key = d.source + SEP + d.label + SEP + d.target;
    if (seen.has(key)) { dropped++; return; }
    seen.add(key);
    edges.push({ data: { id: 'e' + SEP + key, source: d.source, target: d.target, label: d.label } });
  });

  // Undirected adjacency, built once at load. /api/graph carries stored forward
  // edges only, so the reverse direction is synthesized here. Scanning the edge
  // array inside a depth walk instead would be tens of millions of comparisons
  // at 2000 entities.
  var adj = new Map();
  function push(from, to, predicate, dir) {
    if (!adj.has(from)) adj.set(from, []);
    adj.get(from).push({ id: to, predicate: predicate, dir: dir });
  }
  edges.forEach(function (e) {
    push(e.data.source, e.data.target, e.data.label, 'out');
    push(e.data.target, e.data.source, e.data.label, 'in');
  });

  return {
    nodes: payload.nodes, edges: edges, adj: adj, byType: byType,
    types: Array.from(byType.keys()).sort(),
    ids: ids, byId: byId,
    haystack: haystack, droppedDuplicateEdges: dropped
  };
}

// indexSchema turns /api/schema into a lookup, and records which predicates are
// universal so the Filters panel can say why they start hidden.
export function indexSchema(payload) {
  var byName = new Map();
  (payload.types || []).forEach(function (t) { byName.set(t.name, t); });
  // The inverse names khub derives at read time. They are real ontology, but
  // drawing them alongside their forward halves doubles every edge without
  // adding structure — the same relation, read backwards — so they start hidden
  // and the filter panel offers them like any other predicate.
  var derived = [];
  (payload.edges || []).forEach(function (e) {
    if (e.derived && derived.indexOf(e.predicate) === -1) derived.push(e.predicate);
  });
  return {
    byName: byName,
    declaredTypes: (payload.types || []).map(function (t) { return t.name; }),
    baseRelations: (payload.base_relations || []).map(function (r) { return r.predicate; }),
    derivedRelations: derived
  };
}

// neighbourhood walks out from seeds, hop by hop.
//
// Hidden predicates are skipped DURING the walk, never filtered afterwards.
// Filtering after both computes a closure that is then thrown away and answers
// the wrong question: the operator said "don't show me `related`", and a
// walk-then-filter would still have used `related` to decide what counts as a
// neighbour.
// Returns a Map of id -> hop distance from the seeds. The distance is not
// bookkeeping: it is what the L2 layout draws rings from, so the picture says
// how far each node is rather than merely which nodes are near.
export function neighbourhood(model, seeds, depth, hiddenPredicates) {
  var seen = new Map();
  seeds.forEach(function (id) { seen.set(id, 0); });
  var frontier = seeds.slice();
  for (var d = 0; d < depth && frontier.length; d++) {
    var next = [];
    frontier.forEach(function (id) {
      var links = model.adj.get(id);
      if (!links) return;
      links.forEach(function (link) {
        if (hiddenPredicates.has(link.predicate) || seen.has(link.id)) return;
        seen.set(link.id, d + 1);
        next.push(link.id);
      });
    });
    frontier = next;
  }
  return seen;
}

// computeVisible derives the whole picture from focus + depth + filters. It
// returns the layout alongside the sets deliberately: the level decides both,
// and splitting them means two switches on level that can drift apart.
export function computeVisible(state, model) {
  var level = state.focus.length;
  if (level === 0) {
    // Unscoped Graph is every entity. It used to be one synthetic node per
    // type, but that drew the same picture as the schema view with different
    // meaning — allowed edges there, actual edges here — and two views that
    // look identical are worse than one. The ontology owns the type level now.
    var all = new Set();
    model.byId.forEach(function (_d, id) {
      if (!state.hiddenTypes.has(typeOf(id))) all.add(id);
    });
    return withEdges(state, model, all, 0);
  }

  var seeds = level === 1
    ? (model.byType.get(state.focus[0].name) || []).slice()
    : [state.focus[1].id];
  // Both levels walk state.depth. A type opens at 0 (itself alone) and an
  // entity at 1, and the +/- controls move either. Taking one hop by default at
  // a type seeded the walk with every entity of it and dragged in everything
  // they touched — 400 components became 1,650 nodes of mixed types — so that
  // view is reachable, just not automatic.
  var depth = state.depth;

  var hops = neighbourhood(model, seeds, depth, state.hiddenPredicates);

  // The type filter applies after the walk, unlike the predicate filter:
  // hiding a type should remove it from view, not silently change what counts
  // as a neighbour of something else.
  if (state.hiddenTypes.size) {
    var keep = new Map();
    hops.forEach(function (d, id) { if (!state.hiddenTypes.has(typeOf(id))) keep.set(id, d); });
    hops = keep;
  }
  var nodes = new Set(hops.keys());
  return withEdges(state, model, nodes, level, hops);
}

// withEdges resolves the edges, the layout and the notice for a node set. One
// place, so an unscoped graph and a walked neighbourhood cannot drift on what
// the predicate filter means or when the layout falls back.
function withEdges(state, model, nodes, level, hops) {
  // Count what the predicate filter suppresses among the nodes actually in
  // view, so an empty picture can explain itself. Endpoints are checked first
  // so the tally is "edges you would have seen here", not a global figure.
  var edges = new Set();
  var suppressed = 0;
  var suppressedBy = new Set();
  model.edges.forEach(function (e) {
    var d = e.data;
    if (!nodes.has(d.source) || !nodes.has(d.target)) return;
    if (state.hiddenPredicates.has(d.label)) {
      suppressed++;
      suppressedBy.add(d.label);
      return;
    }
    edges.add(d.id);
  });

  var layout, notice = '';
  if (level === 2 && edges.size && hops) {
    // Rings by hop distance, focus at the centre. breadthfirst was wrong here:
    // it lays out one ROW per depth, so a star-shaped neighbourhood — which is
    // what depth 1 always is — became a single line of overlapping labels.
    layout = { name: 'concentric', hops: hops };
  } else if (!edges.size) {
    // No edges means no forces, and cose with nothing to push against collapses
    // into a tall ribbon — measured 241x2768 in a 904x792 canvas. A grid is
    // what an unconnected set actually is: it fills the space, it is O(n), and
    // it stays readable at any count.
    layout = { name: 'grid' };
  } else if (nodes.size > 400) {
    // cose is the only force-directed layout in the vendored 3.33.1 bundle and
    // it is superlinear — at this size it blocks for tens of seconds. Fall back
    // and say so, rather than appearing to hang.
    layout = { name: 'concentric' };
    notice = nodes.size + ' nodes — using the fast layout';
  } else {
    layout = { name: 'cose' };
  }

  // The four universal predicates are hidden by default because at L0 they
  // connect almost everything to almost everything. Inside one type that
  // reasoning does not hold — depends_on between components IS the structure
  // you opened the type to see — so the default can empty the view completely.
  // Rather than make the filter mean different things at different levels, say
  // what is being hidden and leave the fix one click away.
  if (!edges.size && suppressed) {
    var hint = suppressed + (suppressed === 1 ? ' edge is' : ' edges are') +
      ' hidden here by the predicate filter (' +
      Array.from(suppressedBy).sort().join(', ') + ')';
    notice = notice ? notice + ' · ' + hint : hint;
  }
  return { level: level, nodes: nodes, edges: edges, layout: layout, notice: notice };
}

export function search(model, text, limit) {
  var q = text.trim().toLowerCase();
  if (!q) return { hits: [], total: 0 };
  var hits = [], total = 0;
  for (var i = 0; i < model.haystack.length; i++) {
    if (model.haystack[i].lc.indexOf(q) === -1) continue;
    total++;
    if (hits.length < limit) hits.push(model.haystack[i]);
  }
  return { hits: hits, total: total };
}

// legalPredicates is the schema doing the work: only relations declared on the
// source type whose targets admit the destination type. The composer's <select>
// is built from this, so an illegal link cannot be composed — the option is not
// in the control.
export function legalPredicates(schema, fromType, toType) {
  var t = schema.byName.get(fromType);
  if (!t) return [];
  return (t.relations || []).filter(function (r) {
    return r.kind === 'any' || (r.targets || []).indexOf(toType) !== -1;
  }).map(function (r) { return r.predicate; });
}

// legalTargetTypes returns the types some predicate on fromType can reach, or
// null when an `any` relation makes every type legal. Drives the highlight that
// shows which nodes are pickable as B before B is picked.
export function legalTargetTypes(schema, fromType) {
  var t = schema.byName.get(fromType);
  if (!t) return null;
  var out = new Set(), anyAllowed = false;
  (t.relations || []).forEach(function (r) {
    if (r.kind === 'any') { anyAllowed = true; return; }
    (r.targets || []).forEach(function (x) { out.add(x); });
  });
  return anyAllowed ? null : out;
}

// sh quotes an argument for a POSIX shell. khub slugs use a safe charset, so
// this is almost always a no-op — but the output is a command the operator
// pastes into a real shell, and "almost always" is not the standard to hold
// that to.
function sh(s) {
  return /^[A-Za-z0-9_@%+=:,.\/-]+$/.test(s) ? s : "'" + String(s).replace(/'/g, "'\\''") + "'";
}

export function composeLink(a, predicate, b) {
  return 'khub link ' + sh(a) + ' ' + sh(predicate) + ' ' + sh(b);
}

export function composeUnlink(a, predicate, b) {
  return 'khub unlink ' + sh(a) + ' ' + sh(predicate) + ' ' + sh(b);
}

// HTTP resolves targets using the source relation's schema. Never guess from a
// bare slug: two types may share one, even when the relation allows only one.
export function edgeTargets(model, edge) {
  return (edge.resolved_targets || []).filter(function (id) { return model.ids.has(id); });
}

// degreeOf counts an entity's edges, skipping predicates the filter hides.
//
// Excluding them is not a detail: model.adj holds every predicate, so a raw
// count would print "degree 12" on a row sitting beside a graph that draws no
// edges at all, because the universal predicates are hidden by default. The
// number has to answer the same question the picture does.
export function degreeOf(model, id, hiddenPredicates) {
  var links = model.adj.get(id);
  if (!links) return 0;
  var n = 0;
  for (var i = 0; i < links.length; i++) {
    if (!hiddenPredicates.has(links[i].predicate)) n++;
  }
  return n;
}

// buildRows turns the visible id set into rows the list can sort and render.
// Degree is resolved here, once per row, never inside a comparator.
export function buildRows(model, ids, hiddenPredicates) {
  var rows = [];
  ids.forEach(function (id) {
    var d = model.byId.get(id);
    if (!d) return;
    rows.push({
      id: id, name: d.display || d.label, slug: d.label, type: d.type,
      draft: !!d.draft, orphan: !!d.orphan,
      degree: degreeOf(model, id, hiddenPredicates)
    });
  });
  return rows;
}

// sortRows orders rows in place and returns them. Pure, so the ordering can be
// reasoned about without a DOM.
//
// Slugs and types compare by byte order (< / >), matching how Go sorts them
// everywhere else in khub; only the human name uses localeCompare, because it
// is the one column a person reads as language rather than as an identifier.
// Every key falls through to slug so the order is total and idempotent.
export function sortRows(rows, sort) {
  var dir = sort.dir === 'desc' ? -1 : 1;
  var key = sort.key;
  rows.sort(function (a, b) {
    var c = 0;
    if (key === 'name') {
      c = a.name.localeCompare(b.name);
    } else if (key === 'degree') {
      c = a.degree - b.degree;
    } else if (key === 'flags') {
      // Problems first: orphans outrank drafts, both outrank a healthy row.
      c = (a.orphan * 2 + a.draft) - (b.orphan * 2 + b.draft);
    } else if (key === 'type') {
      c = a.type < b.type ? -1 : a.type > b.type ? 1 : 0;
    } else {
      c = a.slug < b.slug ? -1 : a.slug > b.slug ? 1 : 0;
    }
    if (c !== 0) return c * dir;
    return a.slug < b.slug ? -1 : a.slug > b.slug ? 1 : 0;
  });
  return rows;
}

// SCHEMA_ANY is the wildcard node. A relation declared `to: any` really does
// point at anything, and drawing that by expansion would put an edge from its
// source to every type — for depends_on, every type to every type. One node
// says the same thing and stays readable.
export var SCHEMA_ANY = 'schema:*';

// schemaId keeps ontology nodes in their own namespace, distinct from an
// entity's `type/slug`. A collision throws inside cy.add, mid-render, with
// state already committed — a frozen half-drawn page rather than an error.
export function schemaId(type) { return 'schema:' + type; }

// buildSchemaGraph turns /api/schema's `edges` — which is introspect.EdgesView,
// the same projection `khub schema edges` prints — into cytoscape elements.
//
// `used` counts instance edges per (fromType, predicate, toType) so a declared
// relation nothing has ever used can be dimmed. That gap is the interesting
// part of comparing a contract against its data.
export function buildSchemaGraph(schema, edges, hiddenPredicates, used, byType) {
  var nodes = [], seen = new Set(), out = [];
  // The count goes in the label, not into the node's area. Seven boxes cannot
  // express "958 versus 1" by size at any scale that still fits on screen —
  // 958 and 1 rendered as near-identical rectangles, which is worse than not
  // encoding it. A number is exact and costs nothing.
  function node(id, label, kind) {
    if (seen.has(id)) return;
    seen.add(id);
    var n = (byType && byType.get(label)) ? byType.get(label).length : 0;
    nodes.push({
      data: {
        id: id, label: kind === 'any' ? label : label + '  ' + n,
        type: label, schemaKind: kind, count: n
      },
      classes: 'sch' + (kind !== 'any' && n === 0 ? ' emptytype' : '')
    });
  }
  schema.declaredTypes.forEach(function (t) { node(schemaId(t), t, 'type'); });

  edges.forEach(function (row) {
    if (hiddenPredicates.has(row.predicate)) return;
    // `from: ["any"]` means a base relation, declared on every type. `to:
    // ["any"]` means it may point at any type. They are different claims and
    // `affects` is the second without being the first, so neither can stand in
    // for the other.
    var froms = row.from.indexOf('any') !== -1 ? [SCHEMA_ANY] : row.from.map(schemaId);
    var tos = row.to.indexOf('any') !== -1 ? [SCHEMA_ANY] : row.to.map(schemaId);
    if (froms[0] === SCHEMA_ANY || tos[0] === SCHEMA_ANY) node(SCHEMA_ANY, 'any', 'any');

    froms.forEach(function (f) {
      tos.forEach(function (t) {
        var count = used ? countUses(used, f, row.predicate, t) : 0;
        out.push({
          data: {
            id: 'schemaedge:' + f + ' ' + row.predicate + ' ' + t + (row.derived ? ':d' : ''),
            source: f, target: t,
            label: row.predicate + (row.many ? ' *' : ''),
            predicate: row.predicate, kind: row.kind,
            many: !!row.many, required: !!row.required,
            inverse: row.inverse || '', acyclic: !!row.acyclic,
            derived: !!row.derived, used: count
          },
          classes: 'sch' + (row.derived ? ' derived' : '') +
            (row.required ? ' required' : '') + (count ? '' : ' unused') +
            (f === t ? ' selfloop' : '')
        });
      });
    });
  });
  return { nodes: nodes, edges: out };
}

// usedEdgeCounts tallies the instance graph by (fromType, predicate, toType),
// which is what tells a declared relation apart from a used one.
export function usedEdgeCounts(model) {
  var used = new Map();
  model.edges.forEach(function (e) {
    var k = typeOf(e.data.source) + SEP + e.data.label + SEP + typeOf(e.data.target);
    used.set(k, (used.get(k) || 0) + 1);
  });
  return used;
}

// countUses resolves a declared edge against the instance tally.
//
// A wildcard endpoint has to sum across every concrete type rather than look
// itself up: instances of an `any` relation point at real types, never at
// "any", so a literal lookup would report every such relation as unused —
// dimming `affects` as dead when it is in daily use.
function countUses(used, from, predicate, to) {
  var f = from === SCHEMA_ANY ? null : from.slice(7);
  var t = to === SCHEMA_ANY ? null : to.slice(7);
  if (f !== null && t !== null) return used.get(f + SEP + predicate + SEP + t) || 0;
  var total = 0;
  used.forEach(function (n, key) {
    var parts = key.split(SEP);
    if (parts[1] !== predicate) return;
    if (f !== null && parts[0] !== f) return;
    if (t !== null && parts[2] !== t) return;
    total += n;
  });
  return total;
}
