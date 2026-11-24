// panel.js — one entity read beside a list, a saved query or another entity.
// It is in the address, so back, reload and a pasted link restore it.

import * as api from './api.js';
import { el, fill } from './dom.js';
import { same, copyButton, skeleton, problem } from './widgets.js';
import { openId, entityRoute, format } from './route.js';
import { step } from './listmodel.js';
import { parts } from './entity.js';

var WIDE = '(min-width: 900px)'; // app.css stacks the panel below it
var shown = null; // { host, id }

export function hasRoom() { return window.matchMedia(WIDE).matches; }

export function unmount() { shown = null; }

// sync brings host in line with what the route holds open. rows, when given,
// is the order next and previous follow. force reads the entity again.
export function sync(host, route, ctx, rows, force) {
  var id = openId(route);
  host.hidden = !id;
  if (!id) { fill(host, []); shown = null; return; }
  if (!force && shown && shown.host === host && shown.id === id) { bar(host, id, ctx, rows); return; }
  var again = shown && shown.host === host && shown.id === id;
  var mine = { host: host, id: id, seen: again ? shown.seen : null };
  shown = mine;
  if (!host.firstChild) fill(host, [el('div', { class: 'pbar' }), el('div', { class: 'pbody' })]);
  bar(host, id, ctx, rows);
  var body = host.querySelector('.pbody');
  if (!again) { fill(body, [skeleton('Loading ' + id, 6)]); host.scrollTop = 0; }
  api.getEntity(id).then(function (e) {
    if (shown !== mine || same(mine, e)) return;
    var p = parts(e, ctx, true);
    fill(body, [p.head, p.review, p.since, p.body, p.links, p.attributes].filter(Boolean));
  }, function (err) {
    if (shown !== mine) return;
    // A panel already read keeps what it shows.
    if (mine.seen) ctx.failed(err); else fill(body, [problem(err)]);
  });
}

// bar draws the panel's controls, leaving the keyboard on the one it was on.
function bar(host, id, ctx, rows) {
  var at = -1;
  (rows || []).forEach(function (r, i) { if (r.id === id) at = i; });
  var nodes = [];
  if (at !== -1) {
    [['Previous', -1, 'k', '‹'], ['Next', 1, 'j', '›']].forEach(function (b) {
      nodes.push(el('button', {
        type: 'button', class: 'sq', title: b[0] + ' (' + b[2] + ')', 'aria-label': b[0], disabled: step(rows, id, b[1]) === id,
        on: { click: function () { ctx.open(step(rows, id, b[1])); } }
      }, b[3]));
    });
    nodes.push(el('span', { class: 'at' }, (at + 1) + ' of ' + rows.length));
  }
  nodes.push(el('span', { class: 'gap' }));
  nodes.push(el('a', { class: 'btn-ghost', href: format(entityRoute(id)), title: 'Open as a page (Enter)' }, 'Open page'));
  nodes.push(copyButton('Copy link', location.origin + '/' + format(entityRoute(id))));
  nodes.push(el('button', { type: 'button', class: 'sq', title: 'Close (Esc)', 'aria-label': 'Close', on: { click: ctx.close } }, '✕'));
  var box = host.querySelector('.pbar');
  var held = box.contains(document.activeElement) ? document.activeElement.getAttribute('aria-label') || document.activeElement.textContent : '';
  fill(box, nodes);
  if (!held) return;
  var now = nodes.filter(function (n) { return (n.getAttribute('aria-label') || n.textContent) === held; })[0];
  // At either end of the list the button just used is off: the other is next.
  if (now && now.disabled) now = nodes.filter(function (n) { return n.tagName === 'BUTTON' && !n.disabled; })[0];
  if (now) now.focus({ preventScroll: true });
}
