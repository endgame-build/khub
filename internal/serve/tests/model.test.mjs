import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
// Data import keeps tests dependency-free on Node versions that treat .js as CJS.
const code = await readFile(new URL('../ui/model.js', import.meta.url), 'utf8');
const m = await import('data:text/javascript;base64,' + Buffer.from(code).toString('base64'));

const graph = {
  nodes: [
    { data: { id: 'use-case/pay', type: 'use-case' } },
    { data: { id: 'use-case/refund', type: 'use-case' } },
    { data: { id: 'component/ledger', type: 'component' } },
    { data: { id: 'adr/a', type: 'adr' } },
    { data: { id: 'adr/b', type: 'adr' } }
  ],
  edges: [
    { data: { source: 'use-case/pay', target: 'component/ledger', label: 'served_by' } },
    { data: { source: 'use-case/pay', target: 'component/ledger', label: 'served_by' } }, // duplicate record
    { data: { source: 'use-case/refund', target: 'component/ledger', label: 'served_by' } },
    { data: { source: 'adr/b', target: 'adr/a', label: 'supersedes' } },
    { data: { source: 'adr/b', target: 'component/ledger', label: 'related' } }
  ]
};
const schemaPayload = {
  types: [{ name: 'use-case' }, { name: 'component' }, { name: 'adr' }, { name: 'api' }],
  base_relations: [{ predicate: 'related' }],
  base: { fields: [{ name: 'title', type: 'text' }], relations: [{ predicate: 'related', to: ['any'], many: true }] },
  edges: [
    { predicate: 'served_by', from: ['use-case'], to: ['component'], kind: 'typed', many: true, required: false, inverse: 'serves', acyclic: false, derived: false },
    { predicate: 'serves', from: ['component'], to: ['use-case'], kind: 'typed', many: true, required: false, inverse: 'served_by', acyclic: false, derived: true },
    { predicate: 'supersedes', from: ['adr'], to: ['adr'], kind: 'typed', many: false, required: false, inverse: 'superseded', acyclic: true, derived: false },
    { predicate: 'provider', from: ['api'], to: ['component'], kind: 'typed', many: false, required: true, inverse: null, acyclic: false, derived: false },
    { predicate: 'related', from: ['any'], to: ['any'], kind: 'any', many: true, required: false, inverse: null, acyclic: false, derived: false }
  ]
};

function build() {
  const model = m.buildModel(graph);
  const schema = m.indexSchema(schemaPayload);
  return { model, schema, sch: m.buildSchemaGraph(schema, schema.edges, m.usedEdgeCounts(model), model.byType) };
}

test('every declared type is a box labelled with its entity count, plus the base box', () => {
  const { sch } = build();
  const labels = Object.fromEntries(sch.nodes.map(n => [n.data.id, n.data.label]));
  assert.deepEqual(labels, {
    'schema:use-case': 'use-case  2', 'schema:component': 'component  1',
    'schema:adr': 'adr  2', 'schema:api': 'api  0', [m.SCHEMA_ANY]: 'any (base)'
  });
  assert.ok(sch.nodes.find(n => n.data.id === 'schema:api').classes.includes('emptytype'));
});

test('derived inverses and base relations are not drawn; usage is counted, duplicates once', () => {
  const { sch } = build();
  const preds = sch.edges.map(e => e.data.predicate);
  assert.deepEqual(preds, ['served_by', 'supersedes', 'provider']);
  const used = Object.fromEntries(sch.edges.map(e => [e.data.predicate, e.data.used]));
  assert.deepEqual(used, { served_by: 2, supersedes: 1, provider: 0 });
  assert.ok(sch.edges.find(e => e.data.predicate === 'provider').classes.includes('unused'));
  assert.ok(sch.edges.find(e => e.data.predicate === 'provider').classes.includes('required'));
  assert.ok(sch.edges.find(e => e.data.predicate === 'supersedes').classes.includes('selfloop'));
});

test('a relation to any type points at its source\'s own "any type" box', () => {
  const model = m.buildModel(graph);
  const schema = m.indexSchema({ ...schemaPayload, edges: schemaPayload.edges.concat([
    { predicate: 'affects', from: ['adr'], to: ['any'], kind: 'any', many: true, required: false, inverse: null, acyclic: false, derived: false },
    { predicate: 'touches', from: ['use-case'], to: ['any'], kind: 'any', many: true, required: false, inverse: null, acyclic: false, derived: false }
  ]) });
  const sch = m.buildSchemaGraph(schema, schema.edges, m.usedEdgeCounts(model), model.byType);
  const ghosts = sch.nodes.filter(n => n.data.schemaKind === 'ghost').map(n => [n.data.id, n.data.owner]);
  assert.deepEqual(ghosts, [[m.ghostId('adr'), 'schema:adr'], [m.ghostId('use-case'), 'schema:use-case']]);
  const affects = sch.edges.find(e => e.data.predicate === 'affects');
  assert.equal(affects.data.target, m.ghostId('adr'));
  assert.equal(affects.data.toType, 'any');
  // adr/b relates to component/ledger: any-target usage sums across real targets.
  assert.equal(affects.data.used, 0);
  assert.deepEqual(m.relationDetails(sch, 'rel:adr.affects').to, 'any');
});

test('selection maps to diagram ids', () => {
  const { sch } = build();
  assert.deepEqual(m.selectionIds(sch, 'type:adr'), ['schema:adr']);
  assert.deepEqual(m.selectionIds(sch, 'type:any'), [m.SCHEMA_ANY]);
  assert.deepEqual(m.selectionIds(sch, 'type:nope'), []);
  assert.deepEqual(m.selectionIds(sch, 'rel:use-case.served_by'), ['schemaedge:schema:use-case served_by schema:component']);
  assert.deepEqual(m.selectionIds(sch, 'rel:any.related'), []); // base relations are not drawn
  assert.deepEqual(m.selectionIds(sch, ''), []);
});

test('details panels: type, base and relation', () => {
  const { sch, schema } = build();
  const td = m.typeDetails({
    name: 'use-case',
    fields: [{ name: 'trigger', type: 'text', required: true, enum: ['human', 'event'] }, { name: 'title', type: 'text', required: false, enum: null }],
    relations: [{ predicate: 'served_by', to: ['component'], many: true, required: false, inverse: 'serves', derived: false }]
  }, 2);
  assert.equal(td.count, 2);
  assert.deepEqual(td.attributes[0], { name: 'trigger', type: 'text', required: true, allowed: 'human, event', inherited: false });
  assert.deepEqual(td.relations[0], { predicate: 'served_by', to: 'component', many: true, required: false, inverse: 'serves', derived: false, inherited: false });
  // With the base block: an attribute exactly as the base declares it is
  // inherited; one the type redeclares (title made required) is its own.
  const withBase = m.typeDetails({
    name: 'use-case',
    fields: [{ name: 'title', type: 'text', required: true, enum: null }, { name: 'created', type: 'date', required: true, enum: null }],
    relations: [{ predicate: 'related', to: ['any'], many: true }, { predicate: 'served_by', to: ['component'], many: true }]
  }, 2, { fields: [{ name: 'title', type: 'text', required: false, enum: null }, { name: 'created', type: 'date', required: true, enum: null }], relations: [{ predicate: 'related' }] });
  assert.deepEqual(withBase.attributes.map(a => [a.name, a.inherited]), [['title', false], ['created', true]]);
  assert.deepEqual(withBase.relations.map(r => [r.predicate, r.inherited]), [['related', true], ['served_by', false]]);
  const bd = m.baseDetails(schema.base);
  assert.equal(bd.name, 'any (base)');
  assert.deepEqual(bd.relations.map(r => r.predicate), ['related']);
  const rd = m.relationDetails(sch, 'rel:use-case.served_by');
  assert.deepEqual(rd, { from: 'use-case', predicate: 'served_by', to: 'component', kind: 'typed', many: true, required: false, inverse: 'serves', acyclic: false, used: 2 });
  assert.equal(m.relationDetails(sch, 'rel:x.y'), null);
});
