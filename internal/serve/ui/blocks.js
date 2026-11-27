// blocks.js — builds an entity body from the server's block tree.
//
// The server parses Markdown (internal/explore/mdtree.go) and sends a tree of
// a dozen node kinds; this turns each node into one element through the
// builder it is handed, so every string still reaches the document through
// textContent. Pure apart from the builder, with no imports, so node --test
// drives it with a fake one.
//
// h is { el(tag, attrs, text) → node, text(s) → node, append(parent, child),
//        entityHref(id) → string, link(a, id), shift } where link, when given,
// is handed each link to an entity, and shift is how many levels a
// heading moves down: one by default, since the entity's title is the h1.

var TAGS = {
  p: 'p', ul: 'ul', ol: 'ol', li: 'li', quote: 'blockquote',
  em: 'em', strong: 'strong'
};

export function renderBlocks(blocks, h) {
  return (blocks || []).map(function (b) { return renderBlock(b, h); }).filter(Boolean);
}

// shallowest is the level of a body's highest heading, 1 when it has none.
export function shallowest(blocks) {
  var levels = (blocks || []).filter(function (b) { return b.k === 'h'; }).map(function (b) { return b.level || 1; });
  return levels.length ? Math.min.apply(null, levels) : 1;
}

function children(node, kids, h) {
  renderBlocks(kids, h).forEach(function (c) { h.append(node, c); });
  return node;
}

function renderBlock(b, h) {
  switch (b.k) {
    case 'text': return h.text(b.text || '');
    case 'br': return h.el('br');
    case 'hr': return h.el('hr');
    case 'code': return h.el('code', null, b.text || '');
    case 'pre': {
      var pre = h.el('pre');
      h.append(pre, h.el('code', null, b.text || ''));
      return pre;
    }
    case 'comment': return h.el('p', { class: 'comment', title: 'A comment in the file, which a rendered page would hide' }, b.text || '');
    case 'h': {
      var level = Math.min(6, Math.max(1, b.level || 1));
      var shift = h.shift === undefined ? 1 : h.shift;
      return children(h.el('h' + Math.min(6, level + shift)), b.c, h);
    }
    case 'a': {
      if (b.entity) {
        var a = h.el('a', { href: h.entityHref(b.entity), class: 'elink' });
        if (h.link) h.link(a, b.entity);
        return children(a, b.c, h);
      }
      if (b.href) {
        return children(h.el('a', { href: b.href, rel: 'noopener noreferrer', target: '_blank', class: 'xlink' }), b.c, h);
      }
      // A dropped destination: the text, without a link.
      return children(h.el('span', { class: 'deadlink' }), b.c, h);
    }
    default: {
      var tag = TAGS[b.k];
      if (!tag) return b.c ? children(h.el('span'), b.c, h) : null;
      return children(h.el(tag), b.c, h);
    }
  }
}
