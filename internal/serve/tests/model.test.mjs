import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
// Data import keeps tests dependency-free on Node versions that treat .js as CJS.
const code = await readFile(new URL('../ui/model.js', import.meta.url), 'utf8');
const { buildModel, edgeTargets, composeUnlink } = await import('data:text/javascript;base64,' + Buffer.from(code).toString('base64'));
const model = buildModel({ nodes: ['a/shared', 'b/shared', 'c/source'].map(id => ({ data: { id, type: id.split('/')[0], label: id.split('/')[1] } })), edges: [] });
test('schema-resolved target wins over same slug on another type', () => {
 assert.deepEqual(edgeTargets(model, { target: 'shared', resolved_targets: ['b/shared'] }), ['b/shared']);
 assert.deepEqual(edgeTargets(model, { target: 'shared', resolved_targets: [] }), []);
 assert.deepEqual(edgeTargets(model, { target: 'shared', resolved_targets: ['a/shared', 'b/shared'] }), ['a/shared', 'b/shared']);
 assert.deepEqual(edgeTargets(model, { target: 'shared' }), []);
 assert.equal(composeUnlink('c/source', 'owns', 'b/shared'), 'khub unlink c/source owns b/shared');
});
test('aspect query parameter picks the landing view', async () => {
 const { aspectView } = await import('data:text/javascript;base64,' + Buffer.from(code).toString('base64'));
 assert.equal(aspectView('?aspect=schema'), 'schema');
 assert.equal(aspectView('?aspect=instances'), 'graph');
 assert.equal(aspectView('?x=1&aspect=both'), 'both');
 assert.equal(aspectView('?aspect=bogus'), null);
 assert.equal(aspectView('?aspect=toString'), null);
 assert.equal(aspectView(''), null);
 assert.equal(aspectView('?aspect=%E0'), null);
 assert.equal(aspectView('?aspect=schema%'), null);
 assert.equal(aspectView('?aspect=%62oth'), 'both');
});
test('both view boxes only the types on screen and links each entity to its type', async () => {
 const { buildBothGraph, schemaId, SCHEMA_ANY } = await import('data:text/javascript;base64,' + Buffer.from(code).toString('base64'));
 const sch = {
  nodes: ['a', 'b', 'c'].map(t => ({ data: { id: schemaId(t) } })).concat([{ data: { id: SCHEMA_ANY } }]),
  edges: [
   { data: { source: schemaId('a'), target: schemaId('b') } },
   { data: { source: schemaId('a'), target: schemaId('c') } },
   { data: { source: schemaId('b'), target: SCHEMA_ANY } },
   { data: { source: SCHEMA_ANY, target: SCHEMA_ANY } }
  ]
 };
 const both = buildBothGraph(sch, ['a/x', 'b/y', 'b/z']);
 assert.deepEqual(both.nodes.map(n => n.data.id), [schemaId('a'), schemaId('b'), SCHEMA_ANY]);
 const rel = both.edges.filter(e => !e.data.instanceOf).map(e => e.data.source + '>' + e.data.target);
 assert.deepEqual(rel, ['schema:a>schema:b', 'schema:b>' + SCHEMA_ANY]);
 const inst = both.edges.filter(e => e.data.instanceOf).map(e => e.data.source + '>' + e.data.target);
 assert.deepEqual(inst, ['a/x>schema:a', 'b/y>schema:b', 'b/z>schema:b']);
 assert.deepEqual(buildBothGraph(sch, []), { nodes: [], edges: [] });
});
