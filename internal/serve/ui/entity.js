// entity.js — one entity: what it says, its attributes, verification history,
// body, and every connection grouped by relation and direction, each neighbour with its
// type, title, markers and a one-line summary linking onward.

import * as api from './api.js';
import { el, fill, tn } from './dom.js';
import { markerTags, trustTag, notice, copyButton } from './widgets.js';
import { renderBlocks } from './blocks.js';
import { format, entityRoute } from './route.js';
import { word, verifyCommand } from './shell.js';

// GROUP is how many connections of one relation are drawn before the reader
// asks for the rest.
var GROUP = 24;

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
      ctx.ready();
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

// references maps each stored relation to its targets, as `get --edges` lists
// them.
function references(e) {
  var out = Object.create(null); // a predicate may be named like an Object method
  (e.edges || []).forEach(function (edge) {
    if (edge.derived) return;
    (out[edge.predicate] = out[edge.predicate] || []).push(edge);
  });
  return out;
}

function referenceList(edges) {
  var dd = el('dd');
  edges.forEach(function (edge, i) {
    if (i) dd.appendChild(tn(', '));
    var ids = edge.resolved_targets || [];
    dd.appendChild(ids.length === 1 ? el('a', { href: format(entityRoute(ids[0])) }, edge.target) : tn(edge.target));
  });
  return dd;
}

function copyLine(value, label) {
  var line = el('div', { class: 'path mono' });
  line.appendChild(el('span', null, value));
  line.appendChild(copyButton(label, word(value)));
  return line;
}

function render(main, e, ctx) {
  var t = title(e);
  var fm = e.frontmatter || {};
  ctx.crumbs([
    { label: 'Ontology', route: { view: 'overview', sel: '' } },
    { label: e.type, route: ctx.listRoute(e.type) },
    { label: t }
  ]);
  var page = el('article', { class: 'entity' });

  var head = el('header', { class: 'viewhead' });
  var eyebrow = el('div', { class: 'eyebrow' });
  eyebrow.appendChild(el('a', { href: format(ctx.listRoute(e.type)) }, e.type));
  head.appendChild(eyebrow);
  var titleRow = el('div', { class: 'titlerow' });
  titleRow.appendChild(el('h1', null, t));
  if (e.markers) {
    titleRow.appendChild(markerTags(e.markers, false));
    titleRow.appendChild(trustTag(e.markers.trust));
  }
  head.appendChild(titleRow);
  head.appendChild(copyLine(e.id, 'Copy id'));
  if (e.path) head.appendChild(copyLine(e.path, 'Copy path'));
  var description = fm.description;
  var lede = typeof description === 'string' && description.trim() !== '';
  if (lede) head.appendChild(el('p', { class: 'lede' }, description));
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
  // discriminator), `verified` (shown under Verification) and what the header
  // already says. A relation's values lead to the entities they name.
  var targets = references(e);
  var dl = el('dl', { class: 'kvlist' });
  Object.keys(fm).forEach(function (k) {
    if (k === 'type' || k === 'verified') return;
    if ((k === 'title' || k === 'name') && fm[k] === t) return;
    if (k === 'description' && lede) return;
    dl.appendChild(el('dt', { class: 'mono' }, k));
    dl.appendChild(targets[k] ? referenceList(targets[k]) : el('dd', null, display(fm[k])));
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
  // The page never writes: the review is recorded in a terminal, where khub
  // validates it.
  if (!e.stray) side.appendChild(copyButton('Copy verify command', verifyCommand(e.id)));

  var blocks = renderBlocks(e.body_tree || [], builder);
  if (blocks.length) {
    var body = el('div', { class: 'body prose' });
    blocks.forEach(function (b) { body.appendChild(b); });
    mainCol.appendChild(body);
  } else if (!lede) {
    mainCol.appendChild(el('p', { class: 'hint' }, 'No description and no body.'));
  }

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
    var draw = function (it) {
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
    };
    // A group the reader opened is open again when Back returns to it.
    var opened = ctx.entry().groups = ctx.entry().groups || Object.create(null);
    g.items.slice(0, opened[g.key] ? g.items.length : GROUP).forEach(draw);
    sec.appendChild(ul);
    if (g.items.length > GROUP && !opened[g.key]) {
      sec.appendChild(el('button', {
        type: 'button', class: 'btn-ghost more',
        on: { click: function (evt) {
          evt.currentTarget.remove();
          opened[g.key] = true;
          g.items.slice(GROUP).forEach(draw);
          // The button is gone: the keyboard goes on from the first new one.
          var next = ul.children[GROUP].querySelector('a');
          if (next) next.focus();
        } }
      }, 'Show all ' + g.items.length));
    }
    mainCol.appendChild(sec);
  });

  cols.appendChild(mainCol);
  cols.appendChild(side);
  page.appendChild(cols);
  fill(main, [page]);
}
