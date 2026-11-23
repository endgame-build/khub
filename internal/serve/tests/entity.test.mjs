import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
const code = await readFile(new URL('../ui/blocks.js', import.meta.url), 'utf8');
const { renderBlocks } = await import('data:text/javascript;base64,' + Buffer.from(code).toString('base64'));

// A fake builder: elements are plain objects, text nodes are strings. The
// renderer must build nothing else.
const h = {
  el(tag, attrs, text) {
    const node = { tag, attrs: attrs || {}, children: [] };
    if (text !== undefined && text !== null) node.children.push(String(text));
    return node;
  },
  text(s) { return String(s); },
  append(parent, child) { parent.children.push(child); },
  entityHref(id) { return '#/entity/' + id; }
};

function serialize(nodes) {
  return nodes.map(n => typeof n === 'string' ? n : `<${n.tag}${n.attrs.href !== undefined ? ' href=' + n.attrs.href : ''}>${serialize(n.children)}</${n.tag}>`).join('');
}

test('every block kind maps to one element or a text node', () => {
  const tree = [
    { k: 'h', level: 1, c: [{ k: 'text', text: 'Title' }] },
    { k: 'p', c: [
      { k: 'text', text: 'a ' }, { k: 'em', c: [{ k: 'text', text: 'b' }] },
      { k: 'strong', c: [{ k: 'text', text: 'c' }] }, { k: 'code', text: 'd' }, { k: 'br' }
    ] },
    { k: 'ul', c: [{ k: 'li', c: [{ k: 'p', c: [{ k: 'text', text: 'x' }] }] }] },
    { k: 'ol', c: [{ k: 'li', c: [] }] },
    { k: 'quote', c: [] }, { k: 'hr' }, { k: 'pre', text: 'code\n' }
  ];
  assert.equal(serialize(renderBlocks(tree, h)),
    '<h2>Title</h2><p>a <em>b</em><strong>c</strong><code>d</code><br></br></p>' +
    '<ul><li><p>x</p></li></ul><ol><li></li></ol><blockquote></blockquote><hr></hr><pre><code>code\n</code></pre>');
});

test('markup inside text stays text', () => {
  const out = renderBlocks([{ k: 'p', c: [{ k: 'text', text: '<script>alert(1)</script>' }] }], h);
  assert.deepEqual(out[0].children, ['<script>alert(1)</script>']);
});

test('links: entity links stay in the page, dropped ones are plain text', () => {
  const out = renderBlocks([{ k: 'p', c: [
    { k: 'a', href: '', entity: 'component/cmp-ledger', c: [{ k: 'text', text: 'ledger' }] },
    { k: 'a', href: 'https://ok.test', c: [{ k: 'text', text: 'out' }] },
    { k: 'a', href: '', c: [{ k: 'text', text: 'dead' }] }
  ] }], h);
  const [entity, external, dead] = out[0].children;
  assert.equal(entity.tag, 'a');
  assert.equal(entity.attrs.href, '#/entity/component/cmp-ledger');
  assert.equal(external.tag, 'a');
  assert.equal(external.attrs.href, 'https://ok.test');
  assert.equal(external.attrs.rel, 'noopener noreferrer');
  assert.equal(dead.tag, 'span');
  assert.equal(dead.attrs.href, undefined);
  assert.deepEqual(dead.children, ['dead']);
});

test('headings shift one level down and clamp at h6', () => {
  const out = renderBlocks([{ k: 'h', level: 6, c: [] }, { k: 'h', level: 3, c: [] }], h);
  assert.deepEqual(out.map(n => n.tag), ['h6', 'h4']);
});

test('an unknown kind keeps its children, a childless one is dropped', () => {
  const out = renderBlocks([{ k: 'mystery', c: [{ k: 'text', text: 'kept' }] }, { k: 'void' }], h);
  assert.equal(out.length, 1);
  assert.deepEqual(out[0].children, ['kept']);
});
