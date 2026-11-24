// marks.js — the entities a reader has read and means to sign off. A mark holds
// the content revision that was read, and lasts until the page closes. Copying
// asks for every revision again, so what changed after it was read is never
// signed.

import * as api from './api.js';
import { el, fill, text } from './dom.js';
import { num, plural, copyLater } from './widgets.js';
import { verifyCommand } from './shell.js';
import { withName } from './reviewer.js';

var BATCH = 200; // how many revisions one request answers (serve.go)
var NAMED = 3;   // how many left-out entities are named
var marks = new Map();
var drawn = [];  // { node, draw } of what shows marks

// follow draws node now and at every change of the marks, while it is on the
// page.
function follow(node, draw) {
  drawn.push({ node: node, draw: draw });
  draw();
  return node;
}

// changed redraws what shows marks: after a mark, or after a view's rows moved.
export function changed() {
  drawn = drawn.filter(function (d) { return d.node.isConnected; });
  drawn.forEach(function (d) { d.draw(); });
}

export function has(id) { return marks.has(id); }

export function toggle(id, rev) {
  if (marks.has(id)) marks.delete(id); else marks.set(id, rev);
  changed();
}

// settle drops the marks of rows a person's review now holds for.
export function settle(rows) {
  var before = marks.size;
  if (!before) return;
  rows.forEach(function (r) {
    if ((r.markers ? r.markers.trust : r.trust) === 'human') marks.delete(r.id);
  });
  if (marks.size !== before) changed();
}

// tag shows on a row that it is marked.
export function tag(id) {
  var t = el('span', { class: 'tag tag-read', 'data-mark': id, hidden: !marks.has(id) }, 'marked');
  return t;
}

// paint brings every row's tag in line, whichever view drew it.
function paint() {
  Array.prototype.forEach.call(document.querySelectorAll('[data-mark]'), function (t) {
    t.hidden = !marks.has(t.getAttribute('data-mark'));
  });
}

// button marks one entity, as read at rev.
export function button(id, rev) {
  var btn = el('button', { type: 'button', class: 'copy', title: 'Mark to sign off with others (Space)', on: { click: function () { toggle(id, rev); } } });
  return follow(btn, function () {
    btn.setAttribute('aria-pressed', String(marks.has(id)));
    text(btn, marks.has(id) ? 'Marked as read' : 'Mark as read');
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
    text(said, '');
    changed();
  } } }, 'Clear marks');
  var box = el('div', { class: 'markbar', role: 'group', 'aria-label': 'Marked entities' });
  fill(box, [count, copyBtn, clear, said]);
  function mine() {
    return rows().map(function (r) { return r.id; }).filter(function (id) { return marks.has(id); });
  }
  function send(name) {
    var ids = mine();
    copyLater(copyBtn, gather(ids, name).then(function (g) {
      text(said, [
        g.lines.length ? 'Copied ' + plural(g.lines.length, 'line') + '.' : 'Nothing was copied.',
        left(g.changed, 'changed after it was marked', 'changed after they were marked'),
        left(g.unsafe, 'has a name a terminal would act on', 'have names a terminal would act on')
      ].filter(Boolean).join(' '));
      return g.lines.join('\n');
    }), function (err) { text(said, err.message || String(err)); });
  }
  return follow(box, function () {
    var n = mine().length;
    box.hidden = !n && !said.textContent;
    text(count, num(n) + ' of ' + num(rows().length) + ' marked');
    text(copyBtn, 'Copy ' + plural(n, 'verify line'));
    copyBtn.disabled = clear.disabled = !n;
  });
}

function left(ids, one, many) {
  if (!ids.length) return '';
  return num(ids.length) + ' ' + (ids.length === 1 ? one : many) + ': ' + ids.slice(0, NAMED).join(', ') +
    (ids.length > NAMED ? ' and ' + num(ids.length - NAMED) + ' more' : '') + '.';
}

// gather asks for every marked revision and keeps the commands of those that
// are still what was read. A mark that moved on is dropped.
function gather(ids, name) {
  var asks = [];
  for (var i = 0; i < ids.length; i += BATCH) asks.push(api.getRevs(ids.slice(i, i + BATCH)));
  return Promise.all(asks).then(function (answers) {
    var now = Object.assign.apply(null, [Object.create(null)].concat(answers.map(function (a) { return a.revs; })));
    var out = { lines: [], changed: [], unsafe: [] };
    ids.forEach(function (id) {
      if (now[id] !== marks.get(id)) { out.changed.push(id); marks.delete(id); return; }
      var line = verifyCommand(id, name);
      if (line) out.lines.push(line); else out.unsafe.push(id);
    });
    changed();
    return out;
  });
}

follow(document.documentElement, paint);
