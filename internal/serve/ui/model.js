// model.js — pure derivations for the ontology overview. No document, no
// cytoscape, no fetch, no imports, so node --test loads it directly.

export function typeOf(id) {
  var i = id.indexOf('/');
  return i === -1 ? id : id.slice(0, i);
}

// SEP joins composite keys. A khub id, type and predicate are all drawn from a
// safe charset that cannot contain a NUL, so keys built with it are
// unambiguous by construction rather than by convention.
var SEP = '\u0000';

// buildModel reduces /api/graph to what the overview needs: entities per type,
// for the counts in the type boxes, and the instance edges, deduplicated on the
// (source, predicate, target) triple, for each relation's usage count.
export function buildModel(payload) {
  var byType = new Map();
  (payload.nodes || []).forEach(function (n) {
    var d = n.data;
    if (!byType.has(d.type)) byType.set(d.type, []);
    byType.get(d.type).push(d.id);
  });
  var seen = new Set(), edges = [];
  (payload.edges || []).forEach(function (e) {
    var d = e.data;
    var key = d.source + SEP + d.label + SEP + d.target;
    if (seen.has(key)) return;
    seen.add(key);
    edges.push({ data: { source: d.source, target: d.target, label: d.label } });
  });
  return { byType: byType, edges: edges };
}

// indexSchema turns /api/schema into a lookup.
export function indexSchema(payload) {
  var byName = new Map();
  (payload.types || []).forEach(function (t) { byName.set(t.name, t); });
  return {
    byName: byName,
    declaredTypes: (payload.types || []).map(function (t) { return t.name; }),
    base: payload.base || { fields: [], relations: [] },
    edges: payload.edges || []
  };
}

// `schema-any`, not `schema:*`: a type box is `schema:<name>`, and the loader
// accepts a type named `*`, whose box then took this id — cy.add drops the
// duplicate and the base relations landed on that type. No `schema:<name>`
// can equal this one. viz.SchemaAny is the same id.
export var SCHEMA_ANY = 'schema-any';

// schemaId keeps ontology nodes in their own namespace.
export function schemaId(type) { return 'schema:' + type; }

// ghostId is the "any type" box a type's any-target relations point at.
// One per source type: a shared box would be a hub joining every type that
// can point anywhere, and the layout would group unrelated types around it.
export function ghostId(type) { return 'schema-anyof:' + type; }

// buildSchemaGraph turns /api/schema's `edges` — which is introspect.EdgesView,
// the same projection `khub schema edges` prints — into the diagram's nodes
// and edges (layout.js places them).
//
// Left out of the picture, each for a reason the details panel makes up for:
// derived inverses (each is its forward relation read backwards; the panel
// names the inverse) and the base relations (declared on every type, so
// drawing them is a complete graph; the base box's panel lists them).
//
// `used` counts instance edges per (fromType, predicate, toType) so a declared
// relation nothing has ever used can be dimmed. That gap is the interesting
// part of comparing a contract against its data.
export function buildSchemaGraph(schema, edges, used, byType) {
  var nodes = [], seen = new Set(), out = [];
  // The count goes in the label, not into the node's area: a number is exact
  // and costs nothing, while sizes cannot express "958 versus 1".
  function node(id, label, kind, extra) {
    if (seen.has(id)) return;
    seen.add(id);
    var n = (kind === 'type' && byType && byType.get(label)) ? byType.get(label).length : 0;
    var data = {
      id: id, label: kind === 'type' ? label + '  ' + n : label,
      type: kind === 'type' ? label : '', schemaKind: kind, count: n
    };
    Object.assign(data, extra || {});
    nodes.push({ data: data, classes: 'sch sch-' + kind + (kind === 'type' && n === 0 ? ' emptytype' : '') });
  }
  schema.declaredTypes.forEach(function (t) { node(schemaId(t), t, 'type'); });
  node(SCHEMA_ANY, 'any (base)', 'any');

  edges.forEach(function (row) {
    if (row.derived || row.from.indexOf('any') !== -1) return;
    var toAny = row.to.indexOf('any') !== -1;
    row.from.forEach(function (from) {
      var f = schemaId(from);
      var targets = toAny ? [{ id: ghostId(from), type: 'any' }] : row.to.map(function (t) { return { id: schemaId(t), type: t }; });
      targets.forEach(function (tg) {
        if (toAny) node(tg.id, 'any type', 'ghost', { owner: f });
        var count = used ? countUses(used, from, row.predicate, toAny ? null : tg.type) : 0;
        out.push({
          data: {
            id: 'schemaedge:' + f + ' ' + row.predicate + ' ' + tg.id,
            source: f, target: tg.id,
            label: row.predicate + (row.many ? ' *' : ''),
            predicate: row.predicate, kind: row.kind,
            many: !!row.many, required: !!row.required,
            inverse: row.inverse || '', acyclic: !!row.acyclic,
            used: count, from: from, toType: tg.type
          },
          classes: 'sch' + (row.required ? ' required' : '') + (count ? '' : ' unused') +
            (f === tg.id ? ' selfloop' : '')
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

// countUses resolves a declared edge against the instance tally. A null
// target means "any type": instances of an any relation point at real types,
// so their uses are summed across every concrete target.
function countUses(used, from, predicate, to) {
  if (to !== null) return used.get(from + SEP + predicate + SEP + to) || 0;
  var total = 0;
  used.forEach(function (n, key) {
    var parts = key.split(SEP);
    if (parts[0] === from && parts[1] === predicate) total += n;
  });
  return total;
}

// incomingRelations lists the relations other types declare toward a type and
// its own record does not show: those with a declared inverse are there as
// derived relations, base relations are every type's, and its relations to
// itself are its own.
export function incomingRelations(schema, name, used) {
  var out = [];
  schema.edges.forEach(function (row) {
    if (row.derived || row.inverse || row.from.indexOf('any') !== -1) return;
    if (row.to.indexOf(name) === -1 && row.to.indexOf('any') === -1) return;
    row.from.forEach(function (from) {
      if (from === name) return;
      out.push({
        predicate: row.predicate, from: from, many: !!row.many, required: !!row.required,
        used: countUses(used, from, row.predicate, name)
      });
    });
  });
  return out;
}

// relationCoverage is how many of a type's entities have a relation at all. An
// entity pointing at itself has none, as the list's columns count it.
export function relationCoverage(model, from, predicate) {
  var have = new Set();
  model.edges.forEach(function (e) {
    var d = e.data;
    if (d.label === predicate && d.source !== d.target && typeOf(d.source) === from) have.add(d.source);
  });
  return { have: have.size, total: (model.byType.get(from) || []).length };
}

// selectionIds maps an overview selection (`type:<name>` or
// `rel:<from>.<predicate>`) to the diagram element ids it highlights.
export function selectionIds(sch, sel) {
  if (!sel) return [];
  if (sel.indexOf('type:') === 0) {
    var name = sel.slice(5);
    var id = name === 'any' ? SCHEMA_ANY : schemaId(name);
    return sch.nodes.some(function (n) { return n.data.id === id; }) ? [id] : [];
  }
  if (sel.indexOf('rel:') === 0) {
    var rest = sel.slice(4);
    var dot = rest.indexOf('.');
    var from = rest.slice(0, dot), predicate = rest.slice(dot + 1);
    return sch.edges.filter(function (e) {
      return e.data.from === from && e.data.predicate === predicate;
    }).map(function (e) { return e.data.id; });
  }
  return [];
}

// typeDetails is the details panel for a selected type, from /api/type/<name>
// (introspect.TypeView, what `khub schema show` prints): its attributes, its
// relations stored and derived, and its entity count.
//
// With `base` (the base block), each attribute and relation the type merely
// inherits is flagged `inherited`, so the panel can lead with what the type
// declares itself. A type that redeclares a base attribute (title, made
// required) declares it: the flag follows the record, not the name alone.
export function typeDetails(view, count, base) {
  var baseFields = new Map(), baseRels = new Set();
  if (base) {
    (base.fields || []).forEach(function (f) { baseFields.set(f.name, f); });
    (base.relations || []).forEach(function (r) { baseRels.add(r.predicate); });
  }
  function sameAsBase(f) {
    var b = baseFields.get(f.name);
    return !!b && b.type === f.type && !!b.required === !!f.required &&
      JSON.stringify(b.enum || null) === JSON.stringify(f.enum || null);
  }
  return {
    name: view.name,
    when: view.when || '',
    count: count,
    attributes: (view.fields || []).map(function (f) {
      return {
        name: f.name, type: f.type, required: !!f.required,
        allowed: f.enum && f.enum.length ? f.enum.join(', ') : '',
        inherited: sameAsBase(f)
      };
    }),
    relations: (view.relations || []).map(function (r) {
      return {
        predicate: r.predicate, to: (r.to || []).join(' | '), many: !!r.many,
        required: !!r.required, inverse: r.inverse || '', derived: !!r.derived,
        inherited: !r.derived && baseRels.has(r.predicate)
      };
    })
  };
}

// baseDetails is the details panel for the `any` box: the base block every
// type inherits (`khub schema base`).
export function baseDetails(base) {
  return typeDetails({ name: 'any (base)', fields: base.fields, relations: base.relations }, null);
}

// relationDetails is the details panel for a selected relation: the edges the
// diagram drew for it, merged into one declaration.
export function relationDetails(sch, sel) {
  var ids = selectionIds(sch, sel);
  var edges = sch.edges.filter(function (e) { return ids.indexOf(e.data.id) !== -1; });
  if (!edges.length) return null;
  var d = edges[0].data;
  var to = edges.map(function (e) { return e.data.toType; });
  return {
    from: d.from, predicate: d.predicate, to: to.join(' | '), kind: d.kind,
    many: d.many, required: d.required, inverse: d.inverse, acyclic: d.acyclic,
    used: edges.reduce(function (n, e) { return n + e.data.used; }, 0)
  };
}
