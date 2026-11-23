import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
const code = await readFile(new URL('../ui/listmodel.js', import.meta.url), 'utf8');
const m = await import('data:text/javascript;base64,' + Buffer.from(code).toString('base64'));

const rowset = {
  type: 'component', total: 4,
  attributes: [
    { name: 'kind', value_type: 'text', enum: ['service', 'library', 'external'] },
    { name: 'stack', value_type: 'text', enum: [] }
  ],
  relations: [
    { key: 'out:system', label: 'system', direction: 'out', predicate: 'system', peer_types: ['system'], base: false },
    { key: 'in:use-case.served_by', label: 'serves', direction: 'in', predicate: 'served_by', peer_types: ['use-case'], base: false },
    { key: 'out:related', label: 'related', direction: 'out', predicate: 'related', peer_types: ['any'], base: true }
  ],
  rows: [
    { id: 'component/b', title: 'Beta', trust: 'unverified', draft: false, stale: false, orphan: false, incomplete: false,
      attrs: { kind: 'service', stack: 'go, postgres' },
      counts: { 'out:system': 1, 'in:use-case.served_by': 2, 'out:related': 0 },
      links: { 'out:system': ['system/core'], 'in:use-case.served_by': ['use-case/pay', 'use-case/refund'], 'out:related': [] } },
    { id: 'component/a', title: 'alpha', trust: 'human', draft: false, stale: true, orphan: false, incomplete: false,
      attrs: { kind: 'library' },
      counts: { 'out:system': 1, 'in:use-case.served_by': 0, 'out:related': 1 },
      links: { 'out:system': ['system/core'], 'in:use-case.served_by': [], 'out:related': ['adr/x'] } },
    { id: 'component/c', title: 'Gamma', trust: 'machine', draft: true, stale: false, orphan: true, incomplete: false,
      attrs: {},
      counts: { 'out:system': 0, 'in:use-case.served_by': 2, 'out:related': 0 },
      links: { 'out:system': [], 'in:use-case.served_by': ['use-case/pay', 'use-case/report'], 'out:related': [] } },
    { id: 'component/d', title: 'Delta', trust: 'unverified', draft: false, stale: false, orphan: false, incomplete: true,
      attrs: { kind: 'external' },
      counts: { 'out:system': 0, 'in:use-case.served_by': 0, 'out:related': 0 },
      links: { 'out:system': [], 'in:use-case.served_by': [], 'out:related': [] } }
  ]
};
const none = { sort: 'title', dir: 'asc', q: '', rel: '', to: '', attr: '', status: '', base: false };
const ids = rows => rows.map(r => r.id);

test('base columns are hidden unless asked for', () => {
  assert.deepEqual(m.visibleColumns(rowset, false).map(c => c.key), ['out:system', 'in:use-case.served_by']);
  assert.deepEqual(m.visibleColumns(rowset, true).map(c => c.key), ['out:system', 'in:use-case.served_by', 'out:related']);
});

test('sort by every column kind, both ways, ties broken by id', () => {
  assert.deepEqual(ids(m.sortRows(rowset.rows, 'title', 'asc')), ['component/a', 'component/b', 'component/d', 'component/c']);
  assert.deepEqual(ids(m.sortRows(rowset.rows, 'title', 'desc')), ['component/c', 'component/d', 'component/b', 'component/a']);
  assert.deepEqual(ids(m.sortRows(rowset.rows, 'id', 'desc')), ['component/d', 'component/c', 'component/b', 'component/a']);
  // counts: ties (2, 2) and (0, 0) fall back to the id ascending in both directions
  assert.deepEqual(ids(m.sortRows(rowset.rows, 'in:use-case.served_by', 'desc')), ['component/b', 'component/c', 'component/a', 'component/d']);
  assert.deepEqual(ids(m.sortRows(rowset.rows, 'in:use-case.served_by', 'asc')), ['component/a', 'component/d', 'component/b', 'component/c']);
  // attribute: a missing value sorts last whichever way
  assert.deepEqual(ids(m.sortRows(rowset.rows, 'attr:kind', 'asc')), ['component/d', 'component/a', 'component/b', 'component/c']);
  assert.deepEqual(ids(m.sortRows(rowset.rows, 'attr:kind', 'desc')), ['component/b', 'component/a', 'component/d', 'component/c']);
  // trust ranks human > machine > unverified
  assert.deepEqual(ids(m.sortRows(rowset.rows, 'trust', 'desc')), ['component/a', 'component/c', 'component/b', 'component/d']);
  // status: draft outweighs incomplete outweighs orphan outweighs stale
  assert.deepEqual(ids(m.sortRows(rowset.rows, 'status', 'desc')), ['component/c', 'component/d', 'component/a', 'component/b']);
  assert.deepEqual(ids(m.sortRows(rowset.rows, 'stale', 'desc'))[0], 'component/a');
  // the input is not reordered in place
  assert.equal(rowset.rows[0].id, 'component/b');
});

test('text filter matches title, id and attribute values, not the body', () => {
  assert.deepEqual(ids(m.filterRows(rowset, { ...none, q: 'ALPHA' })), ['component/a']);
  assert.deepEqual(ids(m.filterRows(rowset, { ...none, q: 'component/c' })), ['component/c']);
  assert.deepEqual(ids(m.filterRows(rowset, { ...none, q: 'postgres' })), ['component/b']);
  assert.deepEqual(ids(m.filterRows(rowset, { ...none, q: 'nothing-matches' })), []);
  const withBody = { ...rowset, rows: [{ ...rowset.rows[0], body: 'secret words' }] };
  assert.deepEqual(ids(m.filterRows(withBody, { ...none, q: 'secret' })), []);
});

test('linked-entity, attribute and status filters combine with AND', () => {
  const served = { ...none, rel: 'in:use-case.served_by', to: 'use-case/pay' };
  assert.deepEqual(ids(m.filterRows(rowset, served)), ['component/b', 'component/c']);
  assert.deepEqual(ids(m.filterRows(rowset, { ...served, status: 'draft' })), ['component/c']);
  assert.deepEqual(ids(m.filterRows(rowset, { ...served, attr: 'kind=service' })), ['component/b']);
  // a list value matches on any element
  assert.deepEqual(ids(m.filterRows(rowset, { ...none, attr: 'stack=postgres' })), ['component/b']);
  assert.deepEqual(ids(m.filterRows(rowset, { ...none, status: 'incomplete' })), ['component/d']);
  // a hidden base column still filters
  assert.deepEqual(ids(m.filterRows(rowset, { ...none, rel: 'out:related', to: 'adr/x' })), ['component/a']);
});

test('N of M, link targets and attribute values', () => {
  assert.equal(m.countLine(2, 4), '2 of 4');
  assert.equal(m.countLine(0, 0), '0 of 0');
  assert.deepEqual(m.linkTargets(rowset, 'in:use-case.served_by'), ['use-case/pay', 'use-case/refund', 'use-case/report']);
  assert.deepEqual(m.attrValues(rowset, 'kind'), ['service', 'library', 'external']); // the declared enum, in order
  assert.deepEqual(m.attrValues(rowset, 'stack'), ['go', 'postgres']); // observed, split, sorted
  assert.equal(m.columnTitle(rowset.relations[1]), '← serves');
  assert.equal(m.columnTitle(rowset.relations[0]), '→ system');
});
