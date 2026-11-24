import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
// Data import keeps tests dependency-free on Node versions that treat .js as CJS.
const code = await readFile(new URL('../ui/route.js', import.meta.url), 'utf8');
const { parse, format, listRoute } = await import('data:text/javascript;base64,' + Buffer.from(code).toString('base64'));

test('overview: bare, slash, and a selection', () => {
  assert.deepEqual(parse(''), { view: 'overview', sel: '' });
  assert.deepEqual(parse('#/'), { view: 'overview', sel: '' });
  assert.deepEqual(parse('#/?sel=type%3Acomponent'), { view: 'overview', sel: 'type:component' });
  assert.deepEqual(parse('#/?sel=rel:use-case.served_by'), { view: 'overview', sel: 'rel:use-case.served_by' });
  assert.deepEqual(parse('#/?sel=bogus'), { view: 'overview', sel: '' });
  assert.equal(format({ view: 'overview', sel: '' }), '#/');
  assert.equal(format({ view: 'overview', sel: 'type:component' }), '#/?sel=type%3Acomponent');
});

test('list: defaults are omitted, every parameter round-trips', () => {
  const plain = parse('#/type/component');
  assert.equal(plain.view, 'list');
  assert.equal(plain.type, 'component');
  assert.deepEqual(plain.list, { sort: 'title', dir: 'asc', q: '', rel: '', to: '', count: '', attr: '', status: '', base: false });
  assert.equal(format(plain), '#/type/component');

  const full = listRoute('use-case', null, {
    sort: 'in:requirement.use_cases', dir: 'desc', q: 'pay me', rel: 'out:served_by',
    to: 'component/cmp-ledger', count: 'in:requirement.use_cases>=1', attr: 'trigger=human', status: 'incomplete', base: true
  });
  const h = format(full);
  assert.equal(h, '#/type/use-case?sort=in%3Arequirement.use_cases&dir=desc&q=pay+me&rel=out%3Aserved_by&to=component%2Fcmp-ledger&count=in%3Arequirement.use_cases%3E%3D1&attr=trigger%3Dhuman&status=incomplete&base=1');
  assert.deepEqual(parse(h), full);
});

test('list: half a pair and unknown values are dropped', () => {
  const r = parse('#/type/x?rel=out:a&dir=sideways&status=shiny&base=yes&attr=noequals&count=out:a~3&zzz=1');
  assert.deepEqual(r.list, { sort: 'title', dir: 'asc', q: '', rel: '', to: '', count: '', attr: '', status: '', base: false });
  assert.equal(parse('#/type/x?count=sideways:a%3D1').list.count, '');
  assert.equal(parse('#/type/x?count=in:*.related%3D0').list.count, 'in:*.related=0');
  assert.deepEqual(parse('#/type/x?to=a/b').list.to, '');
  assert.equal(parse('#/type/x?rel=sideways:a&to=a/b').list.rel, '');
});

test('entity: ids with a slash and escapes round-trip', () => {
  assert.deepEqual(parse('#/entity/component/cmp-ledger'), { view: 'entity', id: 'component/cmp-ledger' });
  assert.equal(format({ view: 'entity', id: 'component/cmp-ledger' }), '#/entity/component/cmp-ledger');
  const odd = { view: 'entity', id: 'note/a b?c#d' };
  assert.deepEqual(parse(format(odd)), odd);
  assert.deepEqual(parse('#/entity/acme'), { view: 'entity', id: 'acme' });
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
  // not location.hash — so every aspect lands on the overview.
  assert.deepEqual(parse(''), { view: 'overview', sel: '' });
});
