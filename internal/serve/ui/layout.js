// layout.js — places the ontology diagram so that no box ever overlaps
// another, and related types sit together.
//
// Pure: the layered-layout engine (dagre, vendored at /static/dagre.min.js) is
// passed in, and every size is computed from text length rather than measured
// in a browser, so node --test runs exactly the code the page runs and can
// hold the no-overlap property over hundreds of generated schemas.
//
// The picture is built in three steps:
//
//  1. Partition. Types joined by a relation (in either direction, ignoring a
//     type's relations to itself and relations to "any type") form connected
//     groups. A type with no relation to another type is standalone; the base
//     box ("any (base)") is standalone too. A relation whose target is any
//     type points at a small "any type" box owned by its source, so it never
//     pulls unrelated types together through a shared hub.
//  2. Layer. Each group, and each standalone type with its "any type" boxes,
//     is laid out by dagre (a Sugiyama layered layout): relations run downward,
//     or sideways around one type, rows are kept apart by rank separation and boxes within a row
//     by node separation, and every relation label gets space of its own.
//  3. Pack. Groups are shelf-packed left to right, largest first; standalone
//     types are gridded inside one group, each row and column sized to its
//     contents. The grid's shape and the shelf width are chosen together, as
//     the arrangement that fits the canvas at the largest scale. Packing works
//     on padded bounding boxes, so group frames cannot overlap either.
//
// Edges are routed along dagre's polylines: a long relation bends around the
// rows it crosses instead of cutting through their boxes. A relation from a
// type to itself becomes a tag beside the type's box.

export var BOX_H = 30;
export var LABEL_H = 16;
// A relation from a type to itself is not drawn as a loop — a loop's shape
// and label are placed by the renderer, beyond the layout's control — but as
// a small tag ("↻ supersedes") beside its box. Tags are boxes like any other:
// the layout reserves room for them on the box's right and stacks several.
export var TAG_H = 18;
var TAG_GAP = 10;   // between a box and its tags
var TAG_STEP = TAG_H + 10;
export function tagLabel(label) { return '\u21bb ' + label; }
export function tagWidth(label) { return textWidth(tagLabel(label), 10) + 14; }
var NODE_SEP = 36;
var RANK_SEP = 44;
var EDGE_SEP = 14;
export var GROUP_PAD = 22;   // must match the compound padding in cy.js
export var GROUP_LABEL = 22; // room above a group's content for its title
var GROUP_GAP = 56;
var CELL_GAP = 34;

// Monospace text advances about 0.6em per character in every common
// monospace face; 0.62 keeps a margin, so a label never outgrows its box.
export function textWidth(text, px) { return Math.ceil(String(text).length * px * 0.62); }
export function boxWidth(label) { return textWidth(label, 12) + 26; }
export function edgeLabelWidth(label) { return textWidth(label, 10) + 10; }

function kindOf(n) { return n.data.schemaKind; }

// partition splits the diagram's nodes into connected groups of types and the
// standalone remainder.
function partition(sch) {
  var order = new Map();
  sch.nodes.forEach(function (n, i) { order.set(n.data.id, i); });
  var parent = new Map();
  function find(x) {
    while (parent.get(x) !== x) { parent.set(x, parent.get(parent.get(x))); x = parent.get(x); }
    return x;
  }
  function union(a, b) {
    var ra = find(a), rb = find(b);
    if (ra === rb) return;
    // The root is the earlier-declared type, so groups are stable.
    if (order.get(ra) < order.get(rb)) parent.set(rb, ra); else parent.set(ra, rb);
  }
  var types = sch.nodes.filter(function (n) { return kindOf(n) === 'type'; }).map(function (n) { return n.data.id; });
  types.forEach(function (id) { parent.set(id, id); });
  var isType = new Set(types);
  sch.edges.forEach(function (e) {
    var s = e.data.source, t = e.data.target;
    if (s !== t && isType.has(s) && isType.has(t)) union(s, t);
  });
  var byRoot = new Map();
  types.forEach(function (id) {
    var r = find(id);
    if (!byRoot.has(r)) byRoot.set(r, []);
    byRoot.get(r).push(id);
  });
  var linked = [], standalone = [];
  byRoot.forEach(function (members) {
    if (members.length > 1) linked.push(members); else standalone.push(members[0]);
  });
  // Largest group first; ties by declaration order.
  linked.sort(function (a, b) { return b.length - a.length || order.get(a[0]) - order.get(b[0]); });
  sch.nodes.forEach(function (n) {
    if (kindOf(n) === 'any') standalone.push(n.data.id);
  });
  standalone.sort(function (a, b) { return order.get(a) - order.get(b); });
  return { linked: linked, standalone: standalone };
}

// layered lays out one set of nodes with dagre and returns positions relative
// to the set's own top-left corner.
function layered(dagre, ids, sch, info, dir) {
  var members = new Set(ids);
  var sideways = dir === 'LR';
  // A type's "any type" box travels with it.
  sch.nodes.forEach(function (n) {
    if (kindOf(n) === 'ghost' && members.has(n.data.owner)) members.add(n.data.id);
  });
  var g = new dagre.graphlib.Graph({ multigraph: true });
  g.setGraph({ rankdir: dir, nodesep: NODE_SEP, ranksep: RANK_SEP, edgesep: EDGE_SEP, marginx: 0, marginy: 0 });
  g.setDefaultEdgeLabel(function () { return {}; });
  sch.nodes.forEach(function (n) {
    var id = n.data.id;
    if (!members.has(id)) return;
    var it = info.get(id);
    g.setNode(id, sideways ? { width: Math.max(it.w, it.pad - TAG_GAP), height: BOX_H + it.under } : { width: it.w + it.pad, height: it.hh });
  });
  var routed = [];
  sch.edges.forEach(function (e) {
    var d = e.data;
    if (d.source === d.target || !members.has(d.source) || !members.has(d.target)) return;
    g.setEdge(d.source, d.target, { width: edgeLabelWidth(d.label), height: LABEL_H, labelpos: 'c' }, d.id);
    routed.push(d);
  });
  dagre.layout(g);

  var nodes = new Map(), edges = new Map();
  var box = { x1: Infinity, y1: Infinity, x2: -Infinity, y2: -Infinity };
  function grow(x1, y1, x2, y2) {
    box.x1 = Math.min(box.x1, x1); box.y1 = Math.min(box.y1, y1);
    box.x2 = Math.max(box.x2, x2); box.y2 = Math.max(box.y2, y2);
  }
  g.nodes().forEach(function (id) {
    var p = g.node(id), it = info.get(id);
    // dagre centred the box plus its tag room. The box sits at the left of that
    // span and its tags stack on the right, or, where relations run sideways,
    // at its top with the tags under it: either way out of the relations' path.
    var x = sideways ? p.x : p.x - it.pad / 2;
    var y = sideways ? p.y - it.under / 2 : p.y;
    nodes.set(id, { x: x, y: y, w: it.w, h: BOX_H });
    it.tags.forEach(function (t, k) {
      nodes.set(t.id, {
        x: sideways ? x : x + it.w / 2 + TAG_GAP + t.w / 2,
        y: sideways ? y + BOX_H / 2 + TAG_GAP + TAG_H / 2 + k * TAG_STEP : y + (k - (it.tags.length - 1) / 2) * TAG_STEP,
        w: t.w, h: TAG_H, owner: id
      });
    });
    grow(p.x - p.width / 2, p.y - p.height / 2, p.x + p.width / 2, p.y + p.height / 2);
  });
  routed.forEach(function (d) {
    var e = g.edge({ v: d.source, w: d.target, name: d.id });
    var pts = (e.points || []).slice(1, -1).map(function (p) { return { x: p.x, y: p.y }; });
    pts.forEach(function (p) { grow(p.x, p.y, p.x, p.y); });
    var label = { x: e.x, y: e.y, w: edgeLabelWidth(d.label), h: LABEL_H };
    grow(label.x - label.w / 2, label.y - label.h / 2, label.x + label.w / 2, label.y + label.h / 2);
    edges.set(d.id, { points: pts, label: label });
  });
  return shift({ nodes: nodes, edges: edges }, -box.x1, -box.y1, box.x2 - box.x1, box.y2 - box.y1);
}

function shift(part, dx, dy, w, h) {
  part.nodes.forEach(function (n) { n.x += dx; n.y += dy; });
  part.edges.forEach(function (e) {
    e.points.forEach(function (p) { p.x += dx; p.y += dy; });
    e.label.x += dx; e.label.y += dy;
  });
  part.w = w; part.h = h;
  return part;
}

function merge(into, part, dx, dy) {
  part.nodes.forEach(function (n, id) { into.nodes.set(id, { x: n.x + dx, y: n.y + dy, w: n.w, h: n.h, owner: n.owner }); });
  part.edges.forEach(function (e, id) {
    into.edges.set(id, {
      points: e.points.map(function (p) { return { x: p.x + dx, y: p.y + dy }; }),
      label: { x: e.label.x + dx, y: e.label.y + dy, w: e.label.w, h: e.label.h }
    });
  });
}

// gridOf arranges cells in cols columns: each column as wide as its widest
// cell, each row as tall as its tallest, every cell centred in its slot.
function gridOf(cells, cols) {
  var colW = [], rowH = [];
  cells.forEach(function (c, i) {
    var col = i % cols, row = Math.floor(i / cols);
    colW[col] = Math.max(colW[col] || 0, c.w);
    rowH[row] = Math.max(rowH[row] || 0, c.h);
  });
  var colX = [0], rowY = [0];
  colW.forEach(function (w, i) { colX[i + 1] = colX[i] + w + CELL_GAP; });
  rowH.forEach(function (h, i) { rowY[i + 1] = rowY[i] + h + CELL_GAP; });
  var out = { nodes: new Map(), edges: new Map() };
  cells.forEach(function (c, i) {
    var col = i % cols, row = Math.floor(i / cols);
    merge(out, c, colX[col] + (colW[col] - c.w) / 2, rowY[row] + (rowH[row] - c.h) / 2);
  });
  out.w = colX[colW.length] - CELL_GAP;
  out.h = rowY[rowH.length] - CELL_GAP;
  return out;
}

// shelf packs framed group sizes left to right, wrapping past width target,
// and reports where each goes and the scale the whole would fit the canvas at.
function shelf(framed, target, aspect) {
  var x = 0, y = 0, rowH = 0, at = [], w = 0, h = 0;
  framed.forEach(function (f) {
    if (x > 0 && x + f.w > target) { x = 0; y += rowH + GROUP_GAP; rowH = 0; }
    at.push({ x: x, y: y });
    w = Math.max(w, x + f.w);
    h = Math.max(h, y + f.h);
    x += f.w + GROUP_GAP;
    rowH = Math.max(rowH, f.h);
  });
  return { at: at, w: w, h: h, scale: Math.min(aspect / w, 1 / h) };
}

// layoutOntology places every node of sch (buildSchemaGraph's output) and
// routes every edge between two different nodes. aspect is the canvas's
// width over its height. Returns absolute positions (box centres), routed
// edges (interior bend points and the label's box) and the group frames.
// With either, relations may run sideways instead of downward: one type and its
// many neighbours make a long row, or a column as tall as the canvas.
export function layoutOntology(dagre, sch, aspect, either) {
  aspect = aspect > 0 ? aspect : 1.2;
  var down = arrange(dagre, sch, aspect, 'TB');
  if (!either) return down;
  var across = arrange(dagre, sch, aspect, 'LR');
  var scale = function (l) { return Math.min(aspect / l.width, 1 / l.height); };
  return scale(across) > scale(down) ? across : down;
}

function arrange(dagre, sch, aspect, dir) {
  var info = new Map();
  var tags = new Map();
  sch.edges.forEach(function (e) {
    if (e.data.source !== e.data.target) return;
    if (!tags.has(e.data.source)) tags.set(e.data.source, []);
    tags.get(e.data.source).push({ id: e.data.id, w: tagWidth(e.data.label) });
  });
  sch.nodes.forEach(function (n) {
    var own = tags.get(n.data.id) || [];
    var widest = 0;
    own.forEach(function (t) { widest = Math.max(widest, t.w); });
    info.set(n.data.id, {
      w: boxWidth(n.data.label), tags: own,
      pad: own.length ? TAG_GAP + widest : 0,
      under: own.length ? TAG_GAP + own.length * TAG_STEP : 0,
      hh: Math.max(BOX_H, own.length * TAG_STEP - (TAG_STEP - TAG_H))
    });
  });

  var parts = partition(sch);
  var groups = parts.linked.map(function (members, i) {
    return { id: 'group:' + i, kind: 'linked', size: members.length, part: layered(dagre, members, sch, info, dir) };
  });
  var linkedCount = groups.length;
  // A frame is at least as wide as its title, so the title never spills over
  // a neighbour; narrower content is centred inside it.
  var frame = function (g) {
    var label = groupLabel(g, linkedCount);
    var cw = Math.max(g.part.w, textWidth(label, 10) + 8);
    return { w: cw + 2 * GROUP_PAD, h: g.part.h + 2 * GROUP_PAD + GROUP_LABEL, inset: (cw - g.part.w) / 2, label: label };
  };

  // Choose the standalone grid's shape and the shelf width together: every
  // combination is packed and the one that fits the canvas at the largest
  // scale wins. Ties keep the earlier candidate, so the choice is stable.
  var cells = parts.standalone.map(function (id) { return layered(dagre, [id], sch, info, dir); });
  var colChoices = cells.length ? cells.map(function (_, i) { return i + 1; }) : [0];
  var best = null;
  colChoices.forEach(function (cols) {
    var grid = cols ? gridOf(cells, cols) : null;
    var all = groups.slice();
    if (grid) all.push({ id: 'group:standalone', kind: 'standalone', size: parts.standalone.length, part: grid });
    var framed = all.map(frame);
    var widest = 0, total = 0, area = 0;
    framed.forEach(function (f) { widest = Math.max(widest, f.w); total += f.w + GROUP_GAP; area += (f.w + GROUP_GAP) * (f.h + GROUP_GAP); });
    var root = Math.sqrt(area * aspect);
    [widest, root * 0.8, root, root * 1.25, root * 1.6, total].forEach(function (target) {
      var packed = shelf(framed, Math.max(widest, target), aspect);
      if (!best || packed.scale > best.packed.scale + 1e-9) best = { all: all, framed: framed, packed: packed };
    });
  });

  var out = { nodes: new Map(), edges: new Map(), groups: [], sideways: dir === 'LR' };
  best.all.forEach(function (g, i) {
    var f = best.framed[i], p = best.packed.at[i];
    merge(out, g.part, p.x + GROUP_PAD + f.inset, p.y + GROUP_PAD + GROUP_LABEL);
    out.groups.push({ id: g.id, kind: g.kind, size: g.size, label: f.label, x1: p.x, y1: p.y, x2: p.x + f.w, y2: p.y + f.h });
    g.part.nodes.forEach(function (_, id) { out.nodes.get(id).group = g.id; });
  });
  var w = 0, h = 0;
  out.groups.forEach(function (g) { w = Math.max(w, g.x2); h = Math.max(h, g.y2); });
  out.width = w;
  out.height = h;
  return out;
}

// groupLabel names a group frame for the reader.
export function groupLabel(g, linkedCount) {
  if (g.kind === 'standalone') return 'Not linked to another type';
  return linkedCount > 1 ? 'Related types (' + g.size + ')' : 'Related types';
}

// segments turns a polyline's interior points into cytoscape's
// segment-weights and segment-distances, measured from the two box centres
// (`edge-distances: node-position`): the weight is the projection on the
// source→target line, the distance the signed offset from it.
export function segments(s, t, pts) {
  var vx = t.x - s.x, vy = t.y - s.y;
  var l2 = vx * vx + vy * vy;
  if (!pts.length || l2 === 0) return null;
  var l = Math.sqrt(l2);
  var weights = [], distances = [];
  pts.forEach(function (p) {
    var px = p.x - s.x, py = p.y - s.y;
    weights.push(Math.round(((px * vx + py * vy) / l2) * 10000) / 10000);
    distances.push(Math.round(((vx * py - vy * px) / l) * 100) / 100);
  });
  return { weights: weights, distances: distances };
}

// placeElements turns the laid-out diagram into cytoscape elements: group
// frames as compound parents (only when there is more than one group to tell
// apart), every node at its position with its box width, every routed edge
// carrying its segment geometry, and every self-relation as a tag node that
// keeps the relation's data, so selecting it selects the relation.
export function placeElements(sch, lay) {
  var showGroups = lay.groups.length > 1;
  var nodes = [];
  if (showGroups) {
    lay.groups.forEach(function (g) {
      // The drawn frame is pinned to the laid-out one: at least this big,
      // whatever its children's own extent.
      nodes.push({
        data: {
          id: g.id, label: g.label, schemaKind: 'group',
          minW: g.x2 - g.x1 - 2 * GROUP_PAD, minH: g.y2 - g.y1 - 2 * GROUP_PAD
        },
        classes: 'grp grp-' + g.kind
      });
    });
  }
  sch.nodes.forEach(function (n) {
    var p = lay.nodes.get(n.data.id);
    var data = Object.assign({}, n.data, { w: p.w });
    if (showGroups) data.parent = p.group;
    nodes.push({ data: data, classes: n.classes, position: { x: p.x, y: p.y } });
  });
  var edges = [];
  sch.edges.forEach(function (e) {
    var d = Object.assign({}, e.data);
    var classes = e.classes || '';
    if (d.source === d.target) {
      var p = lay.nodes.get(d.id);
      var tag = Object.assign({}, d, { schemaKind: 'self', label: tagLabel(d.label), w: p.w, relation: true });
      delete tag.source;
      delete tag.target;
      if (showGroups) tag.parent = lay.nodes.get(d.source).group;
      nodes.push({ data: tag, classes: classes.replace(/\bselfloop\b/, '') + ' tag', position: { x: p.x, y: p.y } });
      return;
    }
    var r = lay.edges.get(d.id);
    // Sideways, a relation leaves a box's side and enters a side, clear of the
    // tags under it.
    if (lay.sideways) classes += lay.nodes.get(d.source).x < lay.nodes.get(d.target).x ? ' rightward' : ' leftward';
    var seg = r && segments(lay.nodes.get(d.source), lay.nodes.get(d.target), r.points);
    if (seg) {
      d.segW = seg.weights;
      d.segD = seg.distances;
      classes += ' routed';
    }
    edges.push({ data: d, classes: classes });
  });
  return { nodes: nodes, edges: edges };
}
