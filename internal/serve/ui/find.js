// find.js — the search box in the header: entities by id, then by text. The
// server ranks; this asks once the reader pauses, and drops an answer a newer
// question has overtaken.

import * as api from './api.js';
import { el, fill, text } from './dom.js';
import { markerTags, trustTag, plural } from './widgets.js';
import { format, entityRoute } from './route.js';

var PAUSE_MS = 250; // how long typing must pause before a question is asked
var MIN = 2;        // the fewest characters worth asking about

var input, box, ctx, asking, timer;
var hits = [], at = -1;
var answered = ''; // the text the hits answer
var go = false;    // Enter was pressed before they did

export function mount(host, context) {
  ctx = context;
  input = el('input', {
    type: 'search', id: 'q', placeholder: 'Search entities', autocomplete: 'off', spellcheck: false,
    role: 'combobox', 'aria-label': 'Search entities by id or text', 'aria-expanded': 'false',
    'aria-controls': 'hits', 'aria-autocomplete': 'list',
    on: { input: ask, keydown: onKey, focus: function () { if (input.value.trim().length >= MIN && hits.length) open(true); } }
  });
  box = el('div', { id: 'hits', role: 'listbox', 'aria-label': 'Entities found', hidden: true });
  host.appendChild(input);
  host.appendChild(el('span', { class: 'kbd', 'aria-hidden': 'true' }, '/'));
  host.appendChild(box);
  document.addEventListener('mousedown', function (evt) { if (!host.contains(evt.target)) { stop(); open(false); } });
}

export function focus() { input.focus(); input.select(); }

// close reports whether there was something to close.
export function close() {
  var was = !box.hidden || document.activeElement === input;
  stop();
  open(false);
  if (was) { input.value = ''; hits = []; }
  if (document.activeElement === input) document.getElementById('main').focus({ preventScroll: true });
  return was;
}

function open(on) {
  box.hidden = !on;
  input.setAttribute('aria-expanded', String(on));
  if (!on) pick(-1);
}

// stop drops the question on its way.
function stop() {
  clearTimeout(timer);
  if (asking) asking.abort();
  asking = null;
  go = false;
}

function ask() {
  stop();
  var q = input.value.trim();
  if (q.length < MIN) { hits = []; open(false); return; }
  timer = setTimeout(function () {
    asking = new AbortController();
    var mine = asking;
    api.search(q, mine.signal).then(function (found) {
      if (asking !== mine) return;
      draw(found, q);
      if (go && hits.length) take(0);
      go = false;
    }, function (err) {
      if (asking !== mine || err.name === 'AbortError') return;
      hits = [];
      fill(box, [el('p', { class: 'err' }, err.message || String(err))]);
      open(true);
    });
  }, PAUSE_MS);
}

function draw(found, q) {
  hits = found.hits;
  answered = q;
  var nodes = hits.map(function (h, i) {
    var a = el('a', { href: format(entityRoute(h.id)), id: 'hit-' + i, role: 'option', 'aria-selected': 'false', class: 'hit', tabIndex: -1 });
    var top = el('span', { class: 'top' });
    top.appendChild(el('span', { class: 'ty' }, h.type));
    top.appendChild(el('b', null, h.title));
    top.appendChild(markerTags(h.markers));
    top.appendChild(trustTag(h.markers.trust));
    a.appendChild(top);
    a.appendChild(el('span', { class: 'under' }, h.snippet || h.id));
    a.addEventListener('click', done);
    return a;
  });
  if (!hits.length) nodes.push(el('p', { class: 'hint' }, 'Nothing answers to “' + found.q + '”. Search reads the ids, aliases, titles and text of entities.'));
  else if (found.total > hits.length) nodes.push(el('p', { class: 'hint' }, 'The first ' + hits.length + ' of ' + plural(found.total, 'match', 'matches')));
  fill(box, nodes);
  at = -1;
  open(true);
  box.scrollTop = 0;
}

function pick(i) {
  at = i;
  Array.prototype.forEach.call(box.querySelectorAll('.hit'), function (a, n) {
    a.setAttribute('aria-selected', String(n === i));
    if (n === i) a.scrollIntoView({ block: 'nearest' });
  });
  if (i >= 0) input.setAttribute('aria-activedescendant', 'hit-' + i);
  else input.removeAttribute('aria-activedescendant');
}

function onKey(evt) {
  if (evt.key === 'ArrowDown' || evt.key === 'ArrowUp') {
    if (!hits.length) return;
    evt.preventDefault();
    open(true);
    var n = hits.length;
    pick(evt.key === 'ArrowDown' ? (at + 1) % n : (Math.max(at, 0) - 1 + n) % n);
  } else if (evt.key === 'Enter') {
    evt.preventDefault();
    if (input.value.trim() !== answered || box.hidden) go = input.value.trim().length >= MIN;
    else if (hits.length) take(Math.max(0, at));
  }
}

// take opens a hit.
function take(i) {
  ctx.nav(entityRoute(hits[i].id));
  done();
}

function done() {
  stop();
  open(false);
  answered = '';
  input.value = '';
  hits = [];
  text(box, '');
}
