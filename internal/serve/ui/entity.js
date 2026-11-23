// entity.js — one entity: its attributes, verification history, body, and
// every connection grouped by relation and direction, each neighbour with its
// type, title, markers and a one-line summary linking onward.

import * as api from './api.js';
import { el, fill, tn } from './dom.js';
import { markerTags, trustTag, notice } from './widgets.js';
import { renderBlocks } from './blocks.js';
import { format, entityRoute, listRoute } from './route.js';

var current = null;

export function unmount() { current = null; }

// The block renderer's builder: dom.js underneath, so body text reaches the
// document only through textContent and text nodes.
var builder = {
  el: el,
  text: tn,
  append: function (parent, child) { parent.appendChild(child); },
  entityHref: function (id) { return format(entityRoute(id)); }
};

export function show(main, route, ctx) {
  var mine = { id: route.id };
  current = mine;
  fill(main, [el('p', { class: 'hint pad' }, 'Loading ' + route.id + '…')]);
  api.getEntity(route.id).then(function (e) {
    if (current !== mine) return;
    try {
      render(main, e, ctx);
    } catch (bug) {
      // A page bug, not a workspace problem: say so, and keep the stack.
      console.error(bug);
      fill(main, [notice('This page failed to render', String(bug))]);
    }
  }, function (err) {
    if (current !== mine) return;
    ctx.crumbs([{ label: 'Ontology', route: { view: 'overview', sel: '' } }, { label: route.id }]);
    if (err.code === 'ambiguity_error') {
      fill(main, [notice('More than one entity answers to ' + route.id,
        'Pick the one you meant:',
        (err.expected || []).map(function (id) { return { label: id, href: format(entityRoute(id)) }; }))]);
      return;
    }
    var links = [{ label: 'Back to the ontology', href: '#/' }];
    if (err.suggestion) links.unshift({ label: 'Did you mean ' + err.suggestion + '?', href: format(entityRoute(err.suggestion)) });
    fill(main, [notice(err.status === 404 ? 'No such entity' : 'Could not load ' + route.id,
      err.message || String(err), links)]);
  });
}

// display renders a frontmatter value as text: scalars as written, lists
// joined, mappings as key: value pairs.
function display(v) {
  if (v === null || v === undefined) return '';
  if (Array.isArray(v)) return v.map(display).join(', ');
  if (typeof v === 'object') {
    return Object.keys(v).map(function (k) { return k + ': ' + display(v[k]); }).join(' · ');
  }
  return String(v);
}

function title(e) {
  var fm = e.frontmatter || {};
  return fm.title || fm.name || e.slug;
}

function render(main, e, ctx) {
  var t = title(e);
  ctx.crumbs([
    { label: 'Ontology', route: { view: 'overview', sel: '' } },
    { label: e.type, route: listRoute(e.type) },
    { label: t }
  ]);
  var page = el('article', { class: 'entity' });

  var head = el('header', { class: 'viewhead' });
  var eyebrow = el('div', { class: 'eyebrow' });
  eyebrow.appendChild(el('a', { href: format(listRoute(e.type)) }, e.type));
  eyebrow.appendChild(tn(' · '));
  eyebrow.appendChild(el('span', { class: 'mono' }, e.id));
  head.appendChild(eyebrow);
  var titleRow = el('div', { class: 'titlerow' });
  titleRow.appendChild(el('h1', null, t));
  if (e.markers) {
    titleRow.appendChild(markerTags(e.markers, false));
    titleRow.appendChild(trustTag(e.markers.trust));
  }
  head.appendChild(titleRow);
  if (e.path) head.appendChild(el('div', { class: 'path mono' }, e.path));
  page.appendChild(head);
  if (e.stray) {
    page.appendChild(el('p', { class: 'banner' },
      'This file is not an entity of the workspace graph — its type does not match where it is stored. ' +
      '`khub check` reports it as a stray; its connections are not shown.'));
  }

  var cols = el('div', { class: 'entitycols' });
  var mainCol = el('div', { class: 'maincol' });
  var side = el('aside', { class: 'sidecol' });

  // Attributes: every frontmatter key in file order, except `type` (the
  // discriminator) and `verified` (shown under Verification).
  var fm = e.frontmatter || {};
  var dl = el('dl', { class: 'kvlist' });
  Object.keys(fm).forEach(function (k) {
    if (k === 'type' || k === 'verified') return;
    dl.appendChild(el('dt', { class: 'mono' }, k));
    dl.appendChild(el('dd', null, display(fm[k])));
  });
  side.appendChild(el('h2', null, 'Attributes'));
  side.appendChild(dl);

  // Verification history: who verified it and when, newest last.
  side.appendChild(el('h2', null, 'Verification'));
  var events = Array.isArray(fm.verified) ? fm.verified : [];
  if (events.length) {
    var ol = el('ol', { class: 'events' });
    events.forEach(function (ev) {
      var li = el('li');
      li.appendChild(el('span', { class: 'mono' }, display(ev && ev.by)));
      li.appendChild(tn(' — '));
      li.appendChild(el('span', null, display(ev && ev.at)));
      ol.appendChild(li);
    });
    side.appendChild(ol);
  } else {
    side.appendChild(el('p', { class: 'hint' }, 'Not verified yet.'));
  }

  var body = el('div', { class: 'body prose' });
  var blocks = renderBlocks(e.body_tree || [], builder);
  if (blocks.length) blocks.forEach(function (b) { body.appendChild(b); });
  else body.appendChild(el('p', { class: 'hint' }, 'No body.'));
  mainCol.appendChild(body);

  mainCol.appendChild(el('h2', null, 'Connections'));
  var groups = e.neighbours || [];
  if (!groups.length) mainCol.appendChild(el('p', { class: 'hint' }, 'No connections.'));
  groups.forEach(function (g) {
    var sec = el('section', { class: 'group' + (g.base ? ' base' : '') });
    var h = el('h3');
    h.appendChild(el('span', { class: 'dir', title: g.direction === 'in' ? 'incoming' : 'outgoing' },
      g.direction === 'in' ? '←' : '→'));
    h.appendChild(el('span', { class: 'mono' }, ' ' + g.label));
    h.appendChild(el('span', { class: 'n' }, ' ' + g.items.length));
    sec.appendChild(h);
    var ul = el('ul', { class: 'neighbours' });
    g.items.forEach(function (it) {
      var li = el('li', { class: 'card' + (it.resolved ? '' : ' unresolved') });
      var top = el('div', { class: 'ntop' });
      if (it.resolved) {
        top.appendChild(el('span', { class: 'ntype' }, it.type));
        top.appendChild(el('a', { href: format(entityRoute(it.id)), class: 'ntitle' }, it.title || it.id));
        top.appendChild(markerTags(it.markers, true));
      } else {
        top.appendChild(el('span', { class: 'ntype' }, 'unresolved'));
        top.appendChild(el('span', { class: 'ntitle mono' }, it.id));
      }
      li.appendChild(top);
      if (it.resolved) li.appendChild(el('div', { class: 'nid mono' }, it.id));
      if (it.summary) li.appendChild(el('p', { class: 'nsum' }, it.summary));
      ul.appendChild(li);
    });
    sec.appendChild(ul);
    mainCol.appendChild(sec);
  });

  cols.appendChild(mainCol);
  cols.appendChild(side);
  page.appendChild(cols);
  fill(main, [page]);
}
