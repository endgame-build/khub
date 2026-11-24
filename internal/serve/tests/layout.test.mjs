import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import vm from 'node:vm';

// The same vendored engine the page loads from /static/dagre.min.js, run in a
// bare context holding only what a browser page would give it: it defines the
// `dagre` global and touches nothing else.
const dagreSrc = await readFile(new URL('../../assets/dagre.min.js', import.meta.url), 'utf8');
const sandbox = { structuredClone };
vm.createContext(sandbox);
vm.runInContext(dagreSrc, sandbox);
const dagre = sandbox.dagre;

const load = async (name) => import('data:text/javascript;base64,' +
  Buffer.from(await readFile(new URL('../ui/' + name, import.meta.url), 'utf8')).toString('base64'));
const model = await load('model.js');
const L = await load('layout.js');
const fixtures = JSON.parse(await readFile(new URL('./fixtures/schemas.json', import.meta.url), 'utf8'));

function graphOf(schema, counts, relations) {
  const s = model.indexSchema({ types: schema.types.map(name => ({ name })), edges: schema.edges });
  const numbers = model.indexOverview({
    types: schema.types.map(name => ({ name, count: (counts && counts[name]) || 0 })), relations: relations || []
  });
  return model.buildSchemaGraph(s, s.edges, numbers);
}

const rect = (n, extraRight = 0) => ({ x1: n.x - n.w / 2, y1: n.y - n.h / 2, x2: n.x + n.w / 2 + extraRight, y2: n.y + n.h / 2 });
const hit = (a, b, gap) => a.x1 < b.x2 + gap && b.x1 < a.x2 + gap && a.y1 < b.y2 + gap && b.y1 < a.y2 + gap;
const finite = (...xs) => xs.every(Number.isFinite);

// checkLayout holds every property the picture promises, for one schema.
function checkLayout(name, sch, lay) {
  // Every node is placed, and so is a tag for every relation from a type to
  // itself.
  const selfIds = sch.edges.filter(e => e.data.source === e.data.target).map(e => e.data.id);
  const ids = sch.nodes.map(n => n.data.id).concat(selfIds);
  assert.equal(lay.nodes.size, ids.length, `${name}: every node and tag is placed`);
  const rects = ids.map(id => {
    const n = lay.nodes.get(id);
    assert.ok(n && finite(n.x, n.y, n.w, n.h), `${name}: ${id} has a finite position`);
    return { id, r: rect(n), group: n.group, owner: n.owner };
  });
  // No two boxes or tags overlap, with a margin to spare.
  for (let i = 0; i < rects.length; i++) {
    for (let j = i + 1; j < rects.length; j++) {
      assert.ok(!hit(rects[i].r, rects[j].r, 8), `${name}: ${rects[i].id} overlaps ${rects[j].id}`);
    }
  }
  // Every relation between two different boxes is routed, and its label has
  // space of its own: clear of every box and of every other label.
  const labels = [];
  sch.edges.forEach(e => {
    const d = e.data;
    if (d.source === d.target) return;
    const r = lay.edges.get(d.id);
    assert.ok(r && r.points.length >= 1, `${name}: ${d.id} is routed`);
    r.points.forEach(p => assert.ok(finite(p.x, p.y), `${name}: ${d.id} has finite bends`));
    const lr = rect(r.label);
    rects.forEach(b => assert.ok(!hit(lr, b.r, 0), `${name}: label of ${d.id} overlaps box ${b.id}`));
    labels.push({ id: d.id, r: lr });
  });
  for (let i = 0; i < labels.length; i++) {
    for (let j = i + 1; j < labels.length; j++) {
      assert.ok(!hit(labels[i].r, labels[j].r, 0), `${name}: labels of ${labels[i].id} and ${labels[j].id} overlap`);
    }
  }
  // A frame is never narrower than its title.
  lay.groups.forEach(g => assert.ok(g.x2 - g.x1 - 2 * L.GROUP_PAD >= L.textWidth(g.label, 10), `${name}: ${g.id} fits its title`));
  // Group frames never overlap, and every box sits inside its own frame.
  const frames = new Map(lay.groups.map(g => [g.id, g]));
  for (let i = 0; i < lay.groups.length; i++) {
    for (let j = i + 1; j < lay.groups.length; j++) {
      assert.ok(!hit(lay.groups[i], lay.groups[j], 0), `${name}: groups ${lay.groups[i].id} and ${lay.groups[j].id} overlap`);
    }
  }
  rects.forEach(b => {
    const f = frames.get(b.group);
    assert.ok(f, `${name}: ${b.id} belongs to a group`);
    assert.ok(b.r.x1 >= f.x1 + L.GROUP_PAD - 0.5 && b.r.x2 <= f.x2 - L.GROUP_PAD + 0.5 &&
      b.r.y1 >= f.y1 + L.GROUP_PAD + L.GROUP_LABEL - 0.5 && b.r.y2 <= f.y2 - L.GROUP_PAD + 0.5,
    `${name}: ${b.id} lies inside its group frame`);
  });
  // A tag sits just right of its own box, in the same group.
  rects.filter(b => b.owner).forEach(t => {
    const o = rects.find(b => b.id === t.owner);
    const beside = lay.sideways ? t.r.y1 > o.r.y2 && Math.abs(t.r.x1 + t.r.x2 - o.r.x1 - o.r.x2) < 1e-6 : t.r.x1 > o.r.x2 && t.r.x1 - o.r.x2 < 20;
    assert.ok(beside && t.group === o.group, `${name}: tag ${t.id} sits beside ${t.owner}`);
  });
  // Placed elements carry geometry cytoscape can draw; no loop is drawn.
  const placed = L.placeElements(sch, lay);
  placed.edges.forEach(e => {
    assert.notEqual(e.data.source, e.data.target, `${name}: ${e.data.id} is drawn as a tag, not a loop`);
    assert.ok(e.classes.includes('routed'), `${name}: ${e.data.id} is drawn routed`);
    assert.equal(e.data.segW.length, e.data.segD.length);
    e.data.segW.concat(e.data.segD).forEach(v => assert.ok(Number.isFinite(v)));
  });
  const tags = placed.nodes.filter(n => n.data.schemaKind === 'self').map(n => n.data.id);
  assert.deepEqual(tags.sort(), selfIds.slice().sort(), `${name}: one tag per self-relation`);
}

for (const [name, schema] of Object.entries(fixtures)) {
  test(`${name}: no box, label or group overlaps another`, () => {
    const sch = graphOf(schema, { requirement: 958, component: 400 });
    for (const aspect of [0.6, 1.2, 2.4]) checkLayout(`${name}@${aspect}`, sch, L.layoutOntology(dagre, sch, aspect));
  });
}

test('the layout is deterministic: one schema, one picture', () => {
  const sch = graphOf(fixtures['aml-hub']);
  const snap = lay => JSON.stringify({ n: [...lay.nodes], e: [...lay.edges], g: lay.groups });
  assert.equal(snap(L.layoutOntology(dagre, sch, 1.2)), snap(L.layoutOntology(dagre, graphOf(fixtures['aml-hub']), 1.2)));
});

test('grouping: related types together, standalone types apart, no "any" hub', () => {
  const sch = graphOf(fixtures['build-hub']);
  const lay = L.layoutOntology(dagre, sch, 1.2);
  const groupOf = t => lay.nodes.get(model.schemaId(t)).group;
  const related = ['capability', 'actor', 'use-case', 'requirement', 'system', 'component', 'api', 'repo'];
  related.forEach(t => assert.equal(groupOf(t), groupOf('component'), `${t} sits with component`));
  // adr relates only to itself and to any type: standalone, not joined to
  // the related group through a shared "any" box.
  ['prd', 'arc42', 'adr'].forEach(t => assert.equal(groupOf(t), 'group:standalone', `${t} is standalone`));
  assert.equal(lay.nodes.get(model.SCHEMA_ANY).group, 'group:standalone');
  assert.equal(lay.nodes.get(model.ghostId('adr')).group, 'group:standalone', "adr's any-type box travels with it");
  // Relations run downward: a use-case sits above the component serving it.
  assert.ok(lay.nodes.get(model.schemaId('use-case')).y < lay.nodes.get(model.schemaId('component')).y);
  const placed = L.placeElements(sch, lay);
  const frames = placed.nodes.filter(n => n.data.schemaKind === 'group').map(n => n.data.label);
  assert.deepEqual(frames, ['Related types', 'Not linked to another type']);
});

test('segment geometry reproduces the polyline', () => {
  const s = { x: 10, y: 20 }, t = { x: 210, y: 120 };
  const pts = [{ x: 60, y: 90 }, { x: 150, y: 40 }];
  const seg = L.segments(s, t, pts);
  const vx = t.x - s.x, vy = t.y - s.y, len = Math.hypot(vx, vy);
  seg.weights.forEach((w, i) => {
    const d = seg.distances[i];
    const x = s.x + w * vx - d * vy / len, y = s.y + w * vy + d * vx / len;
    assert.ok(Math.abs(x - pts[i].x) < 0.05 && Math.abs(y - pts[i].y) < 0.05, `point ${i}: ${x},${y}`);
  });
  assert.equal(L.segments(s, s, pts), null);
  assert.equal(L.segments(s, t, []), null);
});

// mulberry32: a seeded generator, so a failure names a schema that replays.
function rng(seed) {
  return function () {
    seed |= 0; seed = seed + 0x6D2B79F5 | 0;
    let r = Math.imul(seed ^ seed >>> 15, 1 | seed);
    r = r + Math.imul(r ^ r >>> 7, 61 | r) ^ r;
    return ((r ^ r >>> 14) >>> 0) / 4294967296;
  };
}

function randomSchema(seed) {
  const r = rng(seed);
  const pick = a => a[Math.floor(r() * a.length)];
  const word = (min, max) => {
    const n = min + Math.floor(r() * (max - min + 1));
    let s = '';
    for (let i = 0; i < n; i++) s += 'abcdefghijklmnopqrstuvwxyz-_'[Math.floor(r() * (i === 0 ? 26 : 28))];
    return s;
  };
  const nTypes = 1 + Math.floor(r() * 40);
  const types = [...new Set(Array.from({ length: nTypes }, () => word(2, 24)))];
  const edges = [];
  const used = new Set();
  const nEdges = Math.floor(r() * types.length * 2.2);
  for (let i = 0; i < nEdges; i++) {
    const from = r() < 0.05 ? 'any' : pick(types);
    const predicate = word(2, 18);
    if (used.has(from + ' ' + predicate)) continue;
    used.add(from + ' ' + predicate);
    let to;
    const roll = r();
    if (roll < 0.15) to = ['any'];
    else if (roll < 0.25) to = [from === 'any' ? pick(types) : from];          // self relation
    else if (roll < 0.35) to = [...new Set([pick(types), pick(types), pick(types)])]; // union
    else to = [pick(types)];
    edges.push({
      predicate, from: [from], to, kind: to[0] === 'any' ? 'any' : to.length > 1 ? 'union' : 'typed',
      many: r() < 0.5, required: r() < 0.3, inverse: r() < 0.2 ? word(3, 12) : null,
      acyclic: false, derived: r() < 0.1
    });
  }
  const counts = Object.fromEntries(types.map(t => [t, Math.floor(r() * 3000)]));
  // Some relations to any type are in use, base ones included: they are drawn
  // to the types they reach.
  const relations = [];
  edges.forEach(e => {
    if (e.derived || e.to[0] !== 'any') return;
    (e.from[0] === 'any' ? types : e.from).forEach(from => {
      if (r() < 0.6) return;
      const to = [...new Set(Array.from({ length: 1 + Math.floor(r() * 3) }, () => pick(types)))];
      relations.push({ from, predicate: e.predicate, links: to.length, have: 1, total: 1, to: to.map(type => ({ type, links: 1 })) });
    });
  });
  return { schema: { types, edges }, counts, relations };
}

test('300 generated schemas: the picture never overlaps', () => {
  for (let seed = 1; seed <= 300; seed++) {
    const { schema, counts, relations } = randomSchema(seed);
    const sch = graphOf(schema, counts, relations);
    const aspect = [0.7, 1.2, 1.9][seed % 3];
    checkLayout(`seed ${seed}`, sch, L.layoutOntology(dagre, sch, aspect));
    const at = model.mostConnected(sch);
    checkLayout(`seed ${seed} around ${at}`, model.focusOn(sch, at), L.layoutOntology(dagre, model.focusOn(sch, at), aspect, true));
  }
});
