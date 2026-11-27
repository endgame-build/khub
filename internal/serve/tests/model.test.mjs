import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
// Data import keeps tests dependency-free on Node versions that treat .js as CJS.
const code = await readFile(new URL('../ui/model.js', import.meta.url), 'utf8');
const m = await import('data:text/javascript;base64,' + Buffer.from(code).toString('base64'));

const overview = {
  types: [{ name: 'use-case', count: 2 }, { name: 'component', count: 1 }, { name: 'adr', count: 2 }, { name: 'api', count: 0 }],
  relations: [
    { from: 'use-case', predicate: 'served_by', base: false, links: 2, have: 2, total: 2, to: [{ type: 'component', links: 2 }] },
    { from: 'adr', predicate: 'supersedes', base: false, links: 1, have: 1, total: 2, to: [{ type: 'adr', links: 1 }] },
    { from: 'api', predicate: 'provider', base: false, links: 0, have: 0, total: 0, to: [] }
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

function build(more) {
  const numbers = m.indexOverview({ types: overview.types, relations: overview.relations.concat(more || []) });
  const schema = m.indexSchema(schemaPayload);
  return { numbers, schema, sch: m.buildSchemaGraph(schema, schema.edges, numbers) };
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

test('derived inverses and unused base relations are not drawn; usage is counted', () => {
  const { sch } = build();
  const preds = sch.edges.map(e => e.data.predicate);
  assert.deepEqual(preds, ['served_by', 'supersedes', 'provider']);
  const used = Object.fromEntries(sch.edges.map(e => [e.data.predicate, e.data.used]));
  assert.deepEqual(used, { served_by: 2, supersedes: 1, provider: 0 });
  assert.ok(sch.edges.find(e => e.data.predicate === 'provider').classes.includes('unused'));
  assert.ok(sch.edges.find(e => e.data.predicate === 'provider').classes.includes('required'));
  assert.ok(sch.edges.find(e => e.data.predicate === 'supersedes').classes.includes('selfloop'));
});

test('a relation to any type is drawn to the types it reaches, else to its own "any type" box', () => {
  const edges = schemaPayload.edges.concat([
    { predicate: 'affects', from: ['adr'], to: ['any'], kind: 'any', many: true, required: false, inverse: null, acyclic: false, derived: false },
    { predicate: 'touches', from: ['use-case'], to: ['any'], kind: 'any', many: true, required: false, inverse: null, acyclic: false, derived: false }
  ]);
  const schema = m.indexSchema({ ...schemaPayload, edges });
  const numbers = m.indexOverview({ types: overview.types, relations: overview.relations.concat([
    { from: 'adr', predicate: 'affects', base: false, links: 3, have: 2, total: 2, to: [{ type: 'use-case', links: 1 }, { type: 'component', links: 2 }] }
  ]) });
  const sch = m.buildSchemaGraph(schema, schema.edges, numbers);
  const ghosts = sch.nodes.filter(n => n.data.schemaKind === 'ghost').map(n => [n.data.id, n.data.owner]);
  assert.deepEqual(ghosts, [[m.ghostId('use-case'), 'schema:use-case']]);
  assert.deepEqual(sch.edges.filter(e => e.data.predicate === 'affects').map(e => [e.data.target, e.data.used]),
    [['schema:use-case', 1], ['schema:component', 2]]);
  assert.equal(sch.edges.find(e => e.data.predicate === 'touches').data.target, m.ghostId('use-case'));
  assert.equal(m.relationDetails(sch, 'rel:adr.affects').to, 'use-case | component');
  assert.equal(m.relationDetails(sch, 'rel:adr.affects').used, 3);
});

test('a base relation is drawn where it is used', () => {
  const { sch } = build([{ from: 'adr', predicate: 'related', base: true, links: 1, have: 1, total: 2, to: [{ type: 'component', links: 1 }] }]);
  const base = sch.edges.filter(e => e.data.predicate === 'related');
  assert.deepEqual(base.map(e => [e.data.from, e.data.toType, e.classes.includes('base')]), [['adr', 'component', true]]);
});

test('focus keeps one type, its neighbours and the relations between them', () => {
  const { sch } = build();
  const f = m.focusOn(sch, 'component');
  assert.deepEqual(f.nodes.map(n => n.data.id), ['schema:use-case', 'schema:component', 'schema:api']);
  assert.deepEqual(f.edges.map(e => e.data.predicate), ['served_by', 'provider']);
  assert.equal(m.mostConnected(sch), 'use-case'); // meets component; the first declared of equals
  const empty = m.buildSchemaGraph(m.indexSchema(schemaPayload), schemaPayload.edges, m.indexOverview({ types: overview.types, relations: [] }));
  assert.equal(m.mostConnected(empty), 'component'); // nothing in use: what the schema joins most
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
  assert.equal(td.when, '');
  assert.equal(m.typeDetails({ name: 'x', when: 'a rule is stated' }, 0).when, 'a rule is stated');
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

test('incoming relations: the ones no declared inverse already lists', () => {
  const { schema, numbers } = build();
  // served_by declares an inverse, so component lists it as a derived relation
  assert.deepEqual(m.incomingRelations(schema, 'component', numbers), [
    { predicate: 'provider', from: 'api', many: false, required: true, used: 0 }
  ]);
  // a relation to any type comes in everywhere but from its own type
  const any = m.indexSchema({ ...schemaPayload, edges: schemaPayload.edges.concat([
    { predicate: 'affects', from: ['adr'], to: ['any'], kind: 'any', many: true, required: false, inverse: null, acyclic: false, derived: false }
  ]) });
  assert.deepEqual(m.incomingRelations(any, 'use-case', numbers).map(r => r.predicate), ['affects']);
  assert.deepEqual(m.incomingRelations(any, 'adr', numbers), []);
});

test('a relation nothing has used reads as zero', () => {
  const { numbers } = build();
  assert.deepEqual(m.relationOf(numbers, 'use-case', 'served_by').have, 2);
  assert.deepEqual(m.relationOf(numbers, 'x', 'y'), { from: 'x', predicate: 'y', links: 0, have: 0, total: 0, to: [], base: false });
});
