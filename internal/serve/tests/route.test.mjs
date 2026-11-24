import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
// Data import keeps tests dependency-free on Node versions that treat .js as CJS.
const code = await readFile(new URL('../ui/route.js', import.meta.url), 'utf8');
const { parse, format, listRoute, opening, openId } = await import('data:text/javascript;base64,' + Buffer.from(code).toString('base64'));

const home = { view: 'home', days: 7 };
const overview = (sel, more) => ({ view: 'overview', sel, focus: '', map: false, ...more });
const empty = { sort: 'title', dir: 'asc', q: '', rel: '', to: '', count: [], attr: [], status: [], trust: [], by: [], not: [], base: false, open: '' };

test('home: bare, slash, and its window', () => {
  assert.deepEqual(parse(''), home);
  assert.deepEqual(parse('#/'), home);
  assert.deepEqual(parse('#/?days=30'), { view: 'home', days: 30 });
  assert.equal(format({ view: 'home', days: 30 }), '#/?days=30');
  assert.equal(format(home), '#/');
  ['x', '-1', '99999', ''].forEach(d => assert.deepEqual(parse('#/?days=' + d), home));
  assert.deepEqual(parse('#/types'), { view: 'types' });
  assert.equal(format({ view: 'types' }), '#/types');
});

test('overview: a selection, a focus, the whole map, and its old address', () => {
  assert.deepEqual(parse('#/ontology'), overview(''));
  assert.deepEqual(parse('#/ontology?sel=type%3Acomponent&focus=component'), overview('type:component', { focus: 'component' }));
  assert.deepEqual(parse('#/ontology?map=1'), overview('', { map: true }));
  assert.deepEqual(parse('#/ontology?sel=bogus'), overview(''));
  assert.deepEqual(parse('#/?sel=type%3Acomponent'), overview('type:component'));
  assert.deepEqual(parse('#/?sel=rel:use-case.served_by'), overview('rel:use-case.served_by'));
  assert.equal(format(overview('type:component')), '#/ontology?sel=type%3Acomponent');
  [overview(''), overview('rel:a.b', { focus: 'a' }), overview('', { map: true })].forEach(r => assert.deepEqual(parse(format(r)), r));
});

test('list: defaults are omitted, every parameter round-trips', () => {
  const plain = parse('#/type/component');
  assert.deepEqual(plain, { view: 'list', type: 'component', list: empty });
  assert.equal(format(plain), '#/type/component');
  const full = listRoute('use-case', null, {
    sort: 'in:requirement.use_cases', dir: 'desc', q: 'pay me', rel: 'out:served_by', to: 'component/cmp-ledger',
    count: ['in:requirement.use_cases>=1', 'out:a=0'], attr: ['trigger=human', 'trigger=event'], status: ['incomplete'],
    trust: ['unverified'], by: ['agent/claude-code'], not: ['attr:trigger=x', 'trust:human'], base: true, open: 'use-case/uc-pay'
  });
  assert.deepEqual(parse(format(full)), full);
});

test('list: an address an earlier page wrote still means the same', () => {
  const r = parse('#/type/use-case?sort=id&count=in:requirement.use_cases>=1&attr=trigger%3Dhuman&status=stale&trust=human&base=1');
  assert.deepEqual(r.list, { ...empty, sort: 'id', count: ['in:requirement.use_cases>=1'], attr: ['trigger=human'], status: ['stale'], trust: ['human'], base: true });
});

test('list: half a pair, repeats and unknown values are dropped', () => {
  const r = parse('#/type/x?rel=out:a&dir=sideways&status=shiny&trust=blind&base=yes&attr=noequals&count=out:a~3&not=zzz&not=trust:blind&by=&zzz=1');
  assert.deepEqual(r.list, empty);
  assert.deepEqual(parse('#/type/x?trust=human&trust=human').list.trust, ['human']);
  assert.deepEqual(parse('#/type/x?count=sideways:a%3D1').list.count, []);
  assert.deepEqual(parse('#/type/x?count=in:*.related%3D0').list.count, ['in:*.related=0']);
  assert.equal(parse('#/type/x?rel=sideways:a&to=a/b').list.rel, '');
});

test('query and what is open beside a view', () => {
  assert.deepEqual(parse('#/query/open-things?open=a/b'), { view: 'query', name: 'open-things', open: 'a/b' });
  assert.equal(format({ view: 'query', name: 'open-things', open: '' }), '#/query/open-things');
  const l = parse('#/type/x?trust=human');
  assert.equal(openId(opening(l, 'x/y')), 'x/y');
  assert.deepEqual(opening(opening(l, 'x/y'), ''), l);
  const e = parse('#/entity/a/b');
  assert.equal(format(opening(e, 'c/d')), '#/entity/a/b?open=c%2Fd');
  assert.equal(openId(e), '');
});

test('entity: ids with a slash and escapes round-trip', () => {
  assert.deepEqual(parse('#/entity/component/cmp-ledger'), { view: 'entity', id: 'component/cmp-ledger', open: '' });
  assert.equal(format({ view: 'entity', id: 'component/cmp-ledger', open: '' }), '#/entity/component/cmp-ledger');
  const odd = { view: 'entity', id: 'note/a b?c#d', open: '' };
  assert.deepEqual(parse(format(odd)), odd);
  assert.deepEqual(parse('#/entity/acme'), { view: 'entity', id: 'acme', open: '' });
});

test('malformed addresses are not-found, never a throw', () => {
  assert.equal(parse('#/entity/%E0').view, 'notfound');
  assert.equal(parse('#/type/%E0').view, 'notfound');
  assert.equal(parse('#/nowhere').view, 'notfound');
  assert.equal(parse('#/type/').view, 'notfound');
  assert.equal(parse('#/entity/').view, 'notfound');
});

test('the aspect query parameter is not part of the fragment', () => {
  // `khub serve --aspect both` prints http://…/?aspect=both — location.search,
  // not location.hash — which app.js turns into the diagram's own address.
  assert.deepEqual(parse(''), home);
});
