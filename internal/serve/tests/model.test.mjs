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
