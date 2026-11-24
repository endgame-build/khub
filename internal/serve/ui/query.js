// query.js — a saved query's rows, as `khub run` lists them.

import * as api from './api.js';
import * as panel from './panel.js';
import { el, fill } from './dom.js';
import { same, markerTags, trustTag, notice, entityLink, rowOpens, plural, num, copyButton, skeleton } from './widgets.js';
import { homeRoute, openId } from './route.js';
import { runCommand } from './shell.js';

var view = null; // { name, root, rows, aside }

// focusOn puts the keyboard on a row's title.
export function focusOn(id) {
  if (!view || !view.root) return;
  Array.prototype.forEach.call(view.root.querySelectorAll('tbody tr'), function (tr) {
    if (tr.getAttribute('data-id') === id) tr.querySelector('a').focus();
  });
}

export function unmount() { view = null; panel.unmount(); }

// order is the rows next and previous follow.
export function order() { return view && view.rows ? view.rows : []; }

export function show(main, route, ctx, force) {
  var crumbs = ctx.crumbs([{ label: 'Review', route: homeRoute() }, { label: route.name }]);
  if (!force && view && view.name === route.name && view.root && view.root.isConnected) { mark(route, ctx); ctx.ready(); return; }
  var kept = force && view && view.root && view.root.isConnected;
  var mine = { name: route.name };
  if (!kept) fill(main, [skeleton('Loading ' + route.name, 6)]);
  else Object.assign(mine, view);
  view = mine;
  api.getQuery(route.name).then(function (found) {
    if (view !== mine) return;
    ctx.loaded(found.malformed);
    if (same(mine, found) && kept) { mark(ctx.route(), ctx, true); ctx.ready(); return; }
    draw(main, found, crumbs, ctx);
    mark(ctx.route(), ctx, true);
    ctx.ready();
  }, function (err) {
    if (view !== mine) return;
    if (kept) { ctx.failed(err); return; }
    fill(main, [notice('Could not run ' + route.name, err.message, [{ label: 'Go to the review', href: '#/' }], ctx.refresh)]);
  });
}

function draw(main, found, crumbs, ctx) {
  var root = el('section', { class: 'split' });
  var left = el('div', { class: 'pane' });
  var head = el('header', { class: 'viewhead' });
  head.appendChild(crumbs);
  var row = el('div', { class: 'titlerow' });
  row.appendChild(el('h1', null, found.name));
  row.appendChild(el('span', { class: 'count' }, found.rows.length < found.matched ? num(found.rows.length) + ' of ' + num(found.matched) : plural(found.matched, 'entity', 'entities')));
  row.appendChild(copyButton('Copy khub run', runCommand(found.name)));
  head.appendChild(row);
  left.appendChild(head);
  if (!found.rows.length) left.appendChild(el('p', { class: 'empty' }, 'Nothing matches this query today.'));
  else {
    var t = el('table', { class: 'rows', 'aria-label': found.name });
    var tr = el('tr');
    ['Title', 'Type', 'Markers', 'Trust'].forEach(function (h) { tr.appendChild(el('th', { scope: 'col' }, h)); });
    var thead = el('thead');
    thead.appendChild(tr);
    t.appendChild(thead);
    var body = el('tbody');
    found.rows.forEach(function (r) {
      var line = el('tr', { 'data-id': r.id });
      var title = el('th', { scope: 'row', class: 'title' });
      title.appendChild(entityLink(r.id, r.title, ctx));
      title.appendChild(el('div', { class: 'slug mono' }, r.slug));
      line.appendChild(title);
      line.appendChild(el('td', { class: 'ty' }, r.type));
      var m = el('td');
      m.appendChild(markerTags(r.markers, false));
      line.appendChild(m);
      var tier = el('td');
      tier.appendChild(trustTag(r.markers.trust));
      line.appendChild(tier);
      rowOpens(line, r.id, ctx);
      body.appendChild(line);
    });
    t.appendChild(body);
    left.appendChild(t);
  }
  root.appendChild(left);
  view.aside = el('aside', { class: 'peek', 'aria-label': 'Entity', hidden: true });
  root.appendChild(view.aside);
  fill(main, [root]);
  view.root = root;
  view.rows = found.rows;
}

function mark(route, ctx, force) {
  Array.prototype.forEach.call(view.root.querySelectorAll('tbody tr'), function (tr) {
    tr.classList.toggle('open', tr.getAttribute('data-id') === openId(route));
  });
  panel.sync(view.aside, route, ctx, view.rows, force);
}
