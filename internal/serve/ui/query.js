// query.js — a list across types: a saved query's rows, as `khub run` lists
// them, or what carries one marker.

import * as api from './api.js';
import * as panel from './panel.js';
import * as marks from './marks.js';
import { el, fill } from './dom.js';
import { same, markerTags, trustTag, notice, entityLink, rowOpens, reveal, plural, num, copyButton, skeleton, markerName, MEANING } from './widgets.js';
import { homeRoute, openId } from './route.js';
import { runCommand } from './shell.js';

// What khub query says to list what carries a marker, where it can.
var COMMANDS = {
  unverified: 'khub query --trust unverified', orphan: 'khub query --orphan', draft: 'khub query --draft', stale: 'khub query --stale'
};

var view = null; // { key, root, rows, aside, seen, shown }

// subject is what a route lists: its title, its rows, its command, and what to
// say when there are none.
function subject(route) {
  if (route.view === 'query') {
    return { title: route.name, read: function () { return api.getQuery(route.name); }, label: 'Copy khub run', command: runCommand(route.name), none: 'Nothing matches this query today.' };
  }
  return {
    title: route.name === 'unverified' ? 'No review holds' : sentence(markerName(route.name)), note: MEANING[route.name],
    read: function () { return api.getMarked(route.name); }, label: 'Copy khub query', command: COMMANDS[route.name], none: 'Nothing is marked with this today.'
  };
}

function sentence(s) { return s.charAt(0).toUpperCase() + s.slice(1); }

// focusOn puts the keyboard on a row's title.
export function focusOn(id) {
  if (!view || !view.root) return;
  Array.prototype.forEach.call(view.root.querySelectorAll('tbody tr'), function (tr) {
    if (tr.getAttribute('data-id') === id) tr.querySelector('a').focus({ preventScroll: true });
  });
}

export function unmount() { view = null; panel.unmount(); }

// order is the rows next and previous follow.
export function order() { return view && view.rows ? view.rows : []; }

export function show(main, route, ctx, force) {
  var what = subject(route), key = route.view + ':' + route.name;
  var crumbs = ctx.crumbs([{ label: 'Review', route: homeRoute() }, { label: what.title }]);
  if (!force && view && view.key === key && view.root && view.root.isConnected) { mark(route, ctx); ctx.ready(); return; }
  var kept = force && view && view.root && view.root.isConnected;
  var mine = { key: key };
  if (!kept) fill(main, [skeleton('Loading ' + what.title, 6)]);
  else Object.assign(mine, view);
  view = mine;
  what.read().then(function (found) {
    if (view !== mine) return;
    ctx.loaded(found.malformed);
    marks.settle(found.rows);
    if (same(mine, found) && kept) { mark(ctx.route(), ctx, true); ctx.ready(); return; }
    var row = kept && document.activeElement.closest('tbody tr');
    draw(main, found, what, crumbs, ctx, kept && mine.aside);
    if (row) { focusOn(row.getAttribute('data-id')); if (document.activeElement === document.body) main.focus({ preventScroll: true }); }
    mark(ctx.route(), ctx, true);
    ctx.ready();
  }, function (err) {
    if (view !== mine) return;
    if (kept) { ctx.failed(err); return; }
    fill(main, [notice('Could not list ' + what.title, err.message, [{ label: 'Go to the review', href: '#/' }], ctx.refresh)]);
  });
}

// draw builds the list. aside, when given, is the panel to keep.
function draw(main, found, what, crumbs, ctx, aside) {
  view.rows = found.rows;
  var root = el('section', { class: 'split' });
  var left = el('div', { class: 'pane' });
  var head = el('header', { class: 'viewhead' });
  head.appendChild(crumbs);
  var row = el('div', { class: 'titlerow' });
  row.appendChild(el('h1', null, what.title));
  row.appendChild(el('span', { class: 'count' }, found.rows.length < found.matched ? num(found.rows.length) + ' of ' + num(found.matched) : plural(found.matched, 'entity', 'entities')));
  if (what.command !== undefined) row.appendChild(copyButton(what.label, what.command));
  head.appendChild(row);
  if (what.note) head.appendChild(el('p', { class: 'sub' }, what.note + '.'));
  head.appendChild(marks.bar(order));
  left.appendChild(head);
  if (!found.rows.length) left.appendChild(el('p', { class: 'empty' }, what.none));
  else {
    var t = el('table', { class: 'rows', 'aria-label': what.title });
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
      title.appendChild(marks.tag(r.id));
      title.appendChild(el('div', { class: 'slug mono' }, r.slug));
      line.appendChild(title);
      line.appendChild(el('td', { class: 'ty' }, r.type));
      var m = el('td');
      m.appendChild(markerTags(r.markers));
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
  view.aside = aside || el('aside', { class: 'peek', 'aria-label': 'Entity', hidden: true });
  root.appendChild(view.aside);
  fill(main, [root]);
  view.root = root;
}

function mark(route, ctx, force) {
  var open = openId(route), at = null;
  Array.prototype.forEach.call(view.root.querySelectorAll('tbody tr'), function (tr) {
    var on = tr.getAttribute('data-id') === open;
    tr.classList.toggle('open', on);
    if (on) at = tr;
  });
  panel.sync(view.aside, route, ctx, view.rows, force);
  if (at && open !== view.shown) reveal(at);
  view.shown = open;
}
