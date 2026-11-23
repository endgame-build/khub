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

// buildSchemaGraph turns /api/schema's `edges` — which is introspect.EdgesView,
// the same projection `khub schema edges` prints — into cytoscape elements.
//
// Derived inverses are left out of the picture: each is its forward relation
// read backwards, and drawing both doubles every arrow without adding
// structure. The details panel names the inverse instead.
//
// `used` counts instance edges per (fromType, predicate, toType) so a declared
// relation nothing has ever used can be dimmed. That gap is the interesting
// part of comparing a contract against its data.
export function buildSchemaGraph(schema, edges, used, byType) {
  var nodes = [], seen = new Set(), out = [];
  // The count goes in the label, not into the node's area: a number is exact
  // and costs nothing, while sizes cannot express "958 versus 1".
  function node(id, label, kind) {
    if (seen.has(id)) return;
    seen.add(id);
    var n = (byType && byType.get(label)) ? byType.get(label).length : 0;
    var text = kind === 'any' ? 'any (base)' : label + '  ' + n;
    nodes.push({
      data: {
        id: id, label: text,
        type: label, schemaKind: kind, count: n,
        // An explicit box width: cytoscape's `width: label` is deprecated and
        // measures nothing until the first paint, so the layout and every edge
        // endpoint would be computed against zero-sized boxes.
        w: Math.round(text.length * 7.6 + 24)
      },
      classes: 'sch' + (kind !== 'any' && n === 0 ? ' emptytype' : '')
    });
  }
  schema.declaredTypes.forEach(function (t) { node(schemaId(t), t, 'type'); });

  edges.forEach(function (row) {
    if (row.derived) return;
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
            id: 'schemaedge:' + f + ' ' + row.predicate + ' ' + t,
            source: f, target: t,
            label: row.predicate + (row.many ? ' *' : ''),
            predicate: row.predicate, kind: row.kind,
            many: !!row.many, required: !!row.required,
            inverse: row.inverse || '', acyclic: !!row.acyclic,
            used: count,
            from: f === SCHEMA_ANY ? 'any' : f.slice(7)
          },
          classes: 'sch' + (row.required ? ' required' : '') + (count ? '' : ' unused') +
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

// countUses resolves a declared edge against the instance tally. A wildcard
// endpoint sums across every concrete type: instances of an `any` relation
// point at real types, never at "any".
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

// schemaLayout picks the diagram's layout by size: a ring
// below a dozen types, where it cannot produce an outlier; a force layout at a
// dozen or more, where a ring's circumference strings the labels out.
export function schemaLayout(typeCount) {
  return typeCount >= 12 ? 'cose' : 'circle';
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
  var to = edges.map(function (e) { return e.data.target === SCHEMA_ANY ? 'any' : e.data.target.slice(7); });
  return {
    from: d.from, predicate: d.predicate, to: to.join(' | '), kind: d.kind,
    many: d.many, required: d.required, inverse: d.inverse, acyclic: d.acyclic,
    used: edges.reduce(function (n, e) { return n + e.data.used; }, 0)
  };
}
