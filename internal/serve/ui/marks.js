// marks.js — the entities a reader has read and means to sign off. A mark holds
// the content revision that was read, and lasts until the page closes. The mark
// bar asks for every revision again before it copies, and leaves out what
// changed after it was marked.

import * as api from './api.js';
import { el, fill, text } from './dom.js';
import { num, plural, copyLater } from './widgets.js';
import { signable } from './shell.js';
import { withName } from './reviewer.js';

var BATCH = 200; // how many revisions one request answers (serve.go)
var NAMED = 3;   // how many left-out entities are named
var marks = new Map(); // id → { rev, trust } as marked
var drawn = [];  // { node, draw } of what shows marks
var note = null; // { where, said }: how the last copy went, on the view it was on

// follow draws node now and at every change of the marks.
function follow(node, draw) {
  drawn.push({ node: node, draw: draw });
  draw();
  return node;
}

// changed redraws what shows marks: after a mark, or after a view's rows moved.
export function changed() {
  drawn.forEach(function (d) { d.draw(); });
}

// prune lets go of what left the page, once a view has drawn.
export function prune() {
  drawn = drawn.filter(function (d) { return d.node.isConnected; });
}

function where() { return location.hash.split('?')[0]; }

// toggle marks an entity as read at rev. A mark of an earlier revision becomes
// one of this: the reader has the new text in front of them.
export function toggle(id, rev, trust) {
  var had = marks.get(id);
  if (had && had.rev === rev) marks.delete(id); else marks.set(id, { rev: rev, trust: trust });
  note = null;
  changed();
}

// settle drops the marks of rows a person's review came to hold for.
export function settle(rows) {
  var before = marks.size;
  if (!before) return;
  rows.forEach(function (r) {
    var had = marks.get(r.id);
    if (had && had.trust !== 'human' && (r.markers ? r.markers.trust : r.trust) === 'human') marks.delete(r.id);
  });
  if (marks.size === before) return;
  note = null;
  changed();
}

// tag shows on a row that it is marked.
export function tag(id) {
  return el('span', { class: 'tag tag-read', 'data-mark': id, hidden: !marks.has(id) }, 'marked');
}

// paint brings every row's tag in line, whichever view drew it.
function paint() {
  Array.prototype.forEach.call(document.querySelectorAll('[data-mark]'), function (t) {
    t.hidden = !marks.has(t.getAttribute('data-mark'));
  });
}

// button marks one entity, as read at rev.
export function button(id, rev, trust) {
  var btn = el('button', { type: 'button', class: 'copy', title: 'Mark to sign off with others (Space)', on: { click: function () { toggle(id, rev, trust); } } });
  return follow(btn, function () {
    var had = marks.get(id), here = !!had && had.rev === rev;
    btn.setAttribute('aria-pressed', String(here));
    text(btn, here ? 'Marked as read' : had ? 'Changed since marked: mark again' : 'Mark as read');
  });
}

// bar counts a view's marks and copies their verify commands. rows is how the
// view lists what it holds.
export function bar(rows) {
  var said = el('span', { class: 'said', role: 'status' });
  var count = el('b');
  var copyBtn = el('button', { type: 'button', class: 'btn', on: { click: function () { withName(send); } } });
  var clear = el('button', { type: 'button', class: 'btn-ghost', on: { click: function () {
    mine().forEach(function (id) { marks.delete(id); });
    note = null;
    changed();
  } } }, 'Clear marks');
  var box = el('div', { class: 'markbar', role: 'group', 'aria-label': 'Marked entities' });
  fill(box, [count, copyBtn, clear, said]);
  function mine() {
    return rows().map(function (r) { return r.id; }).filter(function (id) { return marks.has(id); });
  }
  function send(name) {
    var at = where();
    var say = function (s) { note = { where: at, said: s }; changed(); };
    var sorted = gather(mine(), name);
    copyLater(sorted.then(function (g) { return g.lines.join('\n'); })).then(function (copied) {
      return sorted.then(function (g) {
        g.changed.forEach(drop);
        say([
          !g.lines.length ? 'Nothing to copy.' : copied ? 'Copied ' + plural(g.lines.length, 'line') + '.' : 'The clipboard refused the copy.',
          left(g.changed, 'changed after it was marked', 'changed after they were marked'),
          left(g.unsafe, 'has a name a terminal would act on', 'have names a terminal would act on')
        ].filter(Boolean).join(' '));
      });
    }).catch(function (err) { say(err.message || String(err)); });
  }
  return follow(box, function () {
    var n = mine().length, s = note && note.where === where() ? note.said : '';
    var inside = box.contains(document.activeElement);
    text(said, s);
    text(count, num(n) + ' of ' + num(rows().length) + ' marked');
    text(copyBtn, 'Copy ' + plural(n, 'verify line'));
    count.hidden = copyBtn.hidden = clear.hidden = !n;
    box.hidden = !n && !s;
    // The control the keyboard was on is gone.
    if (inside && !n) document.getElementById('main').focus({ preventScroll: true });
  });
}

function left(list, one, many) {
  if (!list.length) return '';
  return num(list.length) + ' ' + (list.length === 1 ? one : many) + ': ' +
    list.slice(0, NAMED).map(function (m) { return m.id; }).join(', ') +
    (list.length > NAMED ? ' and ' + num(list.length - NAMED) + ' more' : '') + '.';
}

// drop takes away a mark that moved on, unless it was made again meanwhile.
function drop(m) {
  var had = marks.get(m.id);
  if (had && had.rev === m.rev) marks.delete(m.id);
}

// gather asks for every marked revision, and sorts what may be signed from what
// may not. What is unmarked while the answer is on its way is left out.
function gather(ids, name) {
  var asked = ids.map(function (id) { return { id: id, rev: marks.get(id).rev }; });
  var asks = [];
  for (var i = 0; i < asked.length; i += BATCH) asks.push(api.getRevs(ids.slice(i, i + BATCH)));
  return Promise.all(asks).then(function (answers) {
    var now = new Map();
    answers.forEach(function (a) { Object.keys(a.revs).forEach(function (id) { now.set(id, a.revs[id]); }); });
    return signable(asked.filter(function (m) {
      var had = marks.get(m.id);
      return had && had.rev === m.rev;
    }), now, name);
  });
}

follow(document.documentElement, paint);
