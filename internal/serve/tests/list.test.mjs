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
      attrs: { kind: 'service', stack: ['go', 'postgres'] },
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
const none = { sort: 'title', dir: 'asc', q: '', rel: '', to: '', count: [], attr: [], status: [], trust: [], by: [], not: [], base: false, open: '' };
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
  // markers: what a reviewer acts on first leads, incomplete before orphan before draft before stale
  assert.deepEqual(ids(m.sortRows(rowset.rows, 'status', 'desc')), ['component/d', 'component/c', 'component/a', 'component/b']);
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
  assert.deepEqual(ids(m.filterRows(rowset, { ...served, status: ['draft'] })), ['component/c']);
  assert.deepEqual(ids(m.filterRows(rowset, { ...served, attr: ['kind=service'] })), ['component/b']);
  // a list value matches on any element
  assert.deepEqual(ids(m.filterRows(rowset, { ...none, attr: ['stack=postgres'] })), ['component/b']);
  assert.deepEqual(ids(m.filterRows(rowset, { ...none, status: ['incomplete'] })), ['component/d']);
  // a hidden base column still filters
  assert.deepEqual(ids(m.filterRows(rowset, { ...none, rel: 'out:related', to: 'adr/x' })), ['component/a']);
});

test('count bounds filter by how many entities a row links through a column', () => {
  assert.deepEqual(ids(m.filterRows(rowset, { ...none, count: ['in:use-case.served_by=0'] })), ['component/a', 'component/d']);
  assert.deepEqual(ids(m.filterRows(rowset, { ...none, count: ['in:use-case.served_by>=2'] })), ['component/b', 'component/c']);
  assert.deepEqual(ids(m.filterRows(rowset, { ...none, count: ['out:system<1'] })), ['component/c', 'component/d']);
  assert.deepEqual(ids(m.filterRows(rowset, { ...none, count: ['out:system!=1'] })), ['component/c', 'component/d']);
  assert.deepEqual(ids(m.filterRows(rowset, { ...none, count: ['out:related>0'] })), ['component/a']); // a hidden base column still bounds
  // combines with the other filters
  assert.deepEqual(ids(m.filterRows(rowset, { ...none, count: ['in:use-case.served_by>=2'], status: ['draft'] })), ['component/c']);
  assert.equal(m.parseCount('nonsense'), null);
  assert.deepEqual(m.parseCount('in:*.related>=10'), { key: 'in:*.related', op: '>=', n: 10 });
});

test('N of M, link targets and attribute values', () => {
  assert.equal(m.countLine(2, 4), '2 of 4');
  assert.equal(m.countLine(4, 4), '4');
  assert.deepEqual(m.linkTargets(rowset, 'in:use-case.served_by'), ['use-case/pay', 'use-case/refund', 'use-case/report']);
  assert.deepEqual(m.attrValues(rowset, 'kind'), ['service', 'library', 'external']); // the declared enum, in order
  assert.deepEqual(m.attrValues(rowset, 'stack'), ['go', 'postgres']); // observed, split, sorted
  assert.equal(m.columnTitle(rowset.relations[1]), '← serves');
  assert.equal(m.columnTitle(rowset.relations[0]), '→ system');
});

test('trust filters, and combines with the rest', () => {
  assert.deepEqual(ids(m.filterRows(rowset, { ...none, trust: ['unverified'] })), ['component/b', 'component/d']);
  assert.deepEqual(ids(m.filterRows(rowset, { ...none, trust: ['unverified'], status: ['incomplete'] })), ['component/d']);
  assert.deepEqual(ids(m.filterRows(rowset, { ...none, trust: ['human'] })), ['component/a']);
});

test('a relation nothing uses folds away, unless it is asked for or in force', () => {
  const empty = { key: 'out:repo', label: 'repo', direction: 'out', predicate: 'repo', peer_types: ['repo'], base: false };
  const rs = { ...rowset, relations: rowset.relations.concat([empty]) };
  const shown = (all, keep) => m.visibleColumns(rs, all, keep).map(c => c.key);
  assert.deepEqual(shown(false), ['out:system', 'in:use-case.served_by']);
  assert.deepEqual(shown(false, ['out:repo', 'out:related']), ['out:system', 'in:use-case.served_by', 'out:related', 'out:repo']);
  assert.deepEqual(shown(true), ['out:system', 'in:use-case.served_by', 'out:related', 'out:repo']);
});

test('a count leads to what it counts', () => {
  const col = (key, peer_types, base = false) => ({ key, direction: key.split(':')[0], predicate: key.split(':')[1].split('.').pop(), peer_types, base });
  const row = links => ({ id: 'component/a', links });
  const to = (c, linked) => m.countTarget('component', c, row({ [c.key]: linked }));
  assert.equal(to(col('out:system', ['system']), []), null);
  // one: the entity itself
  assert.deepEqual(to(col('out:system', ['system']), ['system/core']), { entity: 'system/core' });
  // several of one type: that type's list, filtered to this row from the other end
  assert.deepEqual(to(col('out:consumes', ['api']), ['api/a', 'api/b']), { type: 'api', rel: 'in:component.consumes', to: 'component/a' });
  assert.deepEqual(to(col('in:use-case.served_by', ['use-case']), ['use-case/a', 'use-case/b']), { type: 'use-case', rel: 'out:served_by', to: 'component/a' });
  // base relations collapse their incoming side
  assert.deepEqual(to(col('out:depends_on', ['any'], true), ['component/b', 'component/c']), { type: 'component', rel: 'in:*.depends_on', to: 'component/a' });
  assert.deepEqual(to(col('in:*.depends_on', ['*'], true), ['api/b', 'api/c']), { type: 'api', rel: 'out:depends_on', to: 'component/a' });
  // several types: the row's own page lists them
  assert.deepEqual(to(col('out:affects', ['any']), ['component/b', 'repo/c']), { entity: 'component/a' });
});

test('a value named like an Object method is a value like any other', () => {
  const rs = { ...rowset, rows: [{ id: 'component/z', title: 'z', attrs: { stack: 'constructor' }, counts: {}, links: { 'out:system': ['toString'] } }] };
  assert.deepEqual(m.attrValues(rs, 'stack'), ['constructor']);
  assert.deepEqual(m.linkTargets(rs, 'out:system'), ['toString']);
});

const rows3 = {
  type: 'req', total: 4,
  attributes: [{ name: 'kind', enum: ['a', 'b'] }, { name: 'note', enum: [] }, { name: 'empty', enum: [] }, { name: 'name', enum: [] }],
  relations: [
    { key: 'out:p', label: 'p_q', direction: 'out', predicate: 'p_q', peer_types: ['x'], base: false },
    { key: 'in:t.r', label: 't.r', direction: 'in', predicate: 'r', peer_types: ['t'], base: false }
  ],
  rows: [
    { id: 'req/1', title: 'One', trust: 'human', by: 'ana', stale: true, attrs: { kind: 'a', name: 'One' }, counts: { 'out:p': 1, 'in:t.r': 0 }, links: {} },
    { id: 'req/2', title: 'Two', trust: 'unverified', by: 'bot', stale: true, draft: true, attrs: { kind: 'a', name: 'Two' }, counts: { 'out:p': 0, 'in:t.r': 2 }, links: {} },
    { id: 'req/3', title: 'Three', trust: 'unverified', by: 'bot', stale: true, attrs: { kind: 'b', name: 'Three' }, counts: { 'out:p': 2, 'in:t.r': 0 }, links: {} },
    { id: 'req/4', title: 'Four', trust: 'unverified', by: '', stale: true, attrs: { name: 'Four' }, counts: { 'out:p': 0, 'in:t.r': 0 }, links: {} }
  ]
};
const facet = (list, key) => {
  const f = m.facets(rows3, { ...none, ...list }).find(x => x.key === key);
  return f && Object.fromEntries(f.values.map(v => [v.label, v.n]));
};

test('a facet counts over the rows the other facets leave', () => {
  assert.deepEqual(facet({}, 'trust'), { unverified: 3, human: 1 });
  assert.deepEqual(facet({ attr: ['kind=a'] }, 'trust'), { unverified: 1, human: 1 });
  // its own filter does not narrow its own choices
  assert.deepEqual(facet({ trust: ['human'] }, 'trust'), { unverified: 3, human: 1 });
  assert.deepEqual(facet({}, 'by'), { ana: 1, bot: 2 });
  assert.deepEqual(facet({}, 'attr:kind'), { a: 2, b: 1 });
  assert.deepEqual(facet({}, 'count:out:p'), { none: 2, 'one or more': 2 });
});

test('a facet every row answers alike is left out, unless it is in use', () => {
  assert.deepEqual(facet({}, 'status'), { draft: 1, stale: 4 });
  assert.equal(m.facets({ ...rows3, rows: rows3.rows.slice(2, 3) }, none).length, 0);
  assert.ok(m.facets({ ...rows3, rows: rows3.rows.slice(2, 3) }, { ...none, trust: ['human'] }).some(f => f.key === 'trust'));
});

test('values of one facet add up, facets narrow each other, and a value can be left out', () => {
  const got = list => ids(m.filterRows(rows3, { ...none, ...list }));
  assert.deepEqual(got({ attr: ['kind=a', 'kind=b'] }), ['req/1', 'req/2', 'req/3']);
  assert.deepEqual(got({ attr: ['kind=a', 'kind=b'], trust: ['unverified'] }), ['req/2', 'req/3']);
  assert.deepEqual(got({ by: ['ana', 'bot'], count: ['out:p>=1'] }), ['req/1', 'req/3']);
  assert.deepEqual(got({ count: ['out:p=0', 'out:p>=2'] }), ['req/2', 'req/3', 'req/4']);
  assert.deepEqual(got({ not: ['attr:kind=a'] }), ['req/3', 'req/4']);
  assert.deepEqual(got({ not: ['by:bot', 'trust:human'] }), ['req/4']);
});

test('a value is kept, excluded or neither, never both', () => {
  const on = m.toggle(none, 'trust', 'human', false);
  assert.deepEqual(on, { trust: ['human'], not: [] });
  const out = m.toggle({ ...none, ...on }, 'trust', 'human', true);
  assert.deepEqual(out, { not: ['trust:human'], trust: [] });
  assert.deepEqual(m.toggle({ ...none, ...out }, 'trust', 'human', true), { not: [], trust: [] });
});

test('applied filters read as words and each can be taken away', () => {
  const list = { ...none, q: 'x', trust: ['human'], attr: ['kind=a'], count: ['out:p=0', 'in:t.r>=1'], not: ['status:stale'] };
  const said = m.applied(rows3, list).map(c => c.name + ': ' + c.value);
  assert.deepEqual(said, ['text: x', 'trust: human', 'kind: a', '→ p q: none', '← t r: one or more', 'marker: not stale']);
  m.applied(rows3, list).forEach(c => assert.equal(m.applied(rows3, { ...list, ...c.off }).length, 5, c.name));
});

test('columns that say nothing fold away', () => {
  assert.deepEqual(m.visibleAttributes(rows3, false).map(a => a.name), ['kind']);
  assert.deepEqual(m.visibleAttributes(rows3, true).length, 4);
  const dated = { attributes: [{ name: 'on' }, { name: 'due' }], rows: [{ updated: '2026-01-01', attrs: { on: '2026-01-01', due: '2026-02-01' } }] };
  assert.deepEqual(m.visibleAttributes(dated, false).map(a => a.name), ['due']);
});

test('a comma is part of a value, and a list is a list', () => {
  const rs = { type: 't', attributes: [{ name: 'owner', enum: [] }, { name: 'tags', enum: [] }, { name: 'constructor', enum: [] }], relations: [],
    rows: [{ id: 't/1', title: 'A', attrs: { owner: 'Smith, John', tags: ['x', 'y'] } }, { id: 't/2', title: 'B', attrs: { owner: 'Ana', tags: ['y'] } }] };
  assert.deepEqual(m.attrValues(rs, 'owner'), ['Ana', 'Smith, John']);
  assert.deepEqual(m.attrValues(rs, 'tags'), ['x', 'y']);
  assert.deepEqual(ids(m.filterRows(rs, { ...none, attr: ['tags=y'] })), ['t/1', 't/2']);
  assert.deepEqual(ids(m.filterRows(rs, { ...none, attr: ['owner=Smith'] })), []);
  assert.deepEqual(ids(m.filterRows(rs, { ...none, attr: ['owner=Smith, John'] })), ['t/1']);
  assert.equal(m.attrText(rs.rows[0], 'tags'), 'x, y');
  // a name every object has is not a value every row has
  assert.deepEqual(m.attrValues(rs, 'constructor'), []);
  assert.deepEqual(m.visibleAttributes(rs, false).map(a => a.name), ['owner', 'tags']);
  assert.deepEqual(ids(m.sortRows(rs.rows, 'attr:constructor', 'asc')), ['t/1', 't/2']);
});

test('next and previous stop at the ends', () => {
  const r = rows3.rows;
  assert.equal(m.step(r, '', 1), 'req/1');
  assert.equal(m.step(r, 'req/1', 1), 'req/2');
  assert.equal(m.step(r, 'req/4', 1), 'req/4');
  assert.equal(m.step(r, 'req/1', -1), 'req/1');
  assert.equal(m.step(r, 'req/gone', 1), 'req/1');
  assert.equal(m.step([], 'req/1', 1), '');
  // reviewed away: the next row has its index
  assert.equal(m.step(r, 'req/gone', 1, 2), 'req/3');
  assert.equal(m.step(r, 'req/gone', -1, 2), 'req/2');
  assert.equal(m.step(r, 'req/gone', 1, 9), 'req/4');
  assert.equal(m.step(r, 'req/gone', -1, 0), 'req/1');
  assert.equal(m.step(r, '', 1, 2), 'req/1');
});

test('dates and writers sort, missing last', () => {
  const r = [{ id: 'a', updated: '2026-01-02' }, { id: 'b', updated: '' }, { id: 'c', updated: '2026-03-01' }];
  assert.deepEqual(ids(m.sortRows(r, 'updated', 'desc')), ['c', 'a', 'b']);
  assert.deepEqual(ids(m.sortRows(r, 'updated', 'asc')), ['a', 'c', 'b']);
});
