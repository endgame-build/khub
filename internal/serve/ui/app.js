// app.js — boot, the router and the frame around every view.
//
// Everything a view shows is derived from the URL fragment (route.js), so
// reload, back/forward and a pasted link all restore it. Each view fetches
// what it needs when it opens, and again when the window comes back to the
// front or the reader asks; nothing polls.

import * as home from './home.js';
import * as types from './types.js';
import * as overview from './overview.js';
import * as list from './list.js';
import * as query from './query.js';
import * as entity from './entity.js';
import * as panel from './panel.js';
import * as find from './find.js';
import * as api from './api.js';
import { parse, format, typeRoute, homeRoute, overviewOf, entityRoute, opening, openId, TIERS } from './route.js';
import { step } from './listmodel.js';
import { el, fill, text } from './dom.js';
import { notice, MARKERS, MEANING, markerName } from './widgets.js';

var VIEWS = { home: home, types: types, overview: overview, list: list, query: query, marked: query, entity: entity };
var TABS = [
  { label: 'Review', views: ['home', 'query', 'marked'], route: homeRoute() },
  { label: 'Types', views: ['types', 'list', 'entity'], route: { view: 'types' } },
  { label: 'Ontology', views: ['overview'], route: overviewOf('') }
];
// REFRESH_MS is how fresh a view must be for the window's return to leave it.
var REFRESH_MS = 5000;

var main, tabs, asof, banner;
var route = null;
var lastKey = '';
var workspace = ''; // its name, once /api/schema answers
var page = '';      // what the tab title calls the view on screen
var lists = Object.create(null); // type → the list address last shown for it
var loadedAt = 0;   // when the view on screen was last read

// What the page remembers per history entry: scroll, rows drawn, the
// diagram's viewport. An entry is stamped the first time it is shown, so back
// and forward find it again; a reload starts with nothing remembered.
var entries = {};
var entry = {};
var stamps = 0;
var restore = null;   // the scroll to put back once the view has drawn
var carried = null;   // what a new entry of the same view starts from
var fresh = false;    // the entry on screen was just made
var trail = [];       // the breadcrumb on screen
var held = '';        // the page control the keyboard is to stay on
var refreshedAt = 0;
var settled = false;  // no scroll is recorded while a view is still loading

function enter() {
  var id = history.state && history.state.khub;
  fresh = !id;
  if (!id) {
    stamps += 1;
    id = Date.now() + '-' + stamps;
    history.replaceState({ khub: id }, '');
  }
  entries[id] = entries[id] || carried || {};
  carried = null;
  entry = entries[id];
}

function setTitle() {
  document.title = [page, workspace, 'khub'].filter(Boolean).join(' · ');
}

function $(id) { return document.getElementById(id); }

// readColors resolves the graph ramp out of the stylesheet. app.js holds no
// colour literal of its own: the token sheet is the source, and cytoscape
// needs hex because it parses colours itself and understands neither custom
// properties nor lab().
function readColors() {
  var cs = getComputedStyle(document.documentElement);
  function v(name) { return cs.getPropertyValue(name).trim(); }
  var ramp = [];
  for (var i = 1; i <= 10; i++) ramp.push(v('--graph-' + i));
  return {
    ramp: ramp,
    ink: {
      mid: v('--graph-ink-mid'), soft: v('--graph-ink-soft'),
      line: v('--graph-line'), navy: v('--graph-navy'), warn: v('--graph-amber')
    }
  };
}

// setCrumbs builds the trail to where the reader is — every item but the last
// is a link — and names the browser tab after the page it ends on.
function setCrumbs(items) {
  trail = items;
  page = items.length ? items[items.length - 1].label : '';
  setTitle();
  var nav = el('nav', { class: 'crumbs', 'aria-label': 'Breadcrumb' });
  items.slice(0, -1).forEach(function (it) {
    nav.appendChild(el('a', { href: format(it.route) }, it.label));
    nav.appendChild(el('span', { class: 'sep', 'aria-hidden': 'true' }, '›'));
  });
  if (items.length) nav.appendChild(el('span', { 'aria-current': 'page' }, items[items.length - 1].label));
  return nav;
}

function drawTabs() {
  fill(tabs, TABS.map(function (t) {
    // An entity belongs to the list it was opened from.
    var here = route && t.views.indexOf(route.view === 'entity' && entry.from ? entry.from.route.view : route.view) !== -1;
    return el('a', { href: format(t.route), class: here ? 'on' : null, 'aria-current': here ? 'true' : null }, t.label);
  }));
}

// showBanner says which files are missing from everything on screen.
function showBanner(files) {
  banner.hidden = !files.length;
  if (!files.length) { fill(banner, []); return; }
  var shown = files.slice(0, 3).join(', ') + (files.length > 3 ? ' and ' + (files.length - 3) + ' more' : '');
  fill(banner, [
    el('b', null, files.length === 1 ? 'One file does not parse. ' : files.length + ' files do not parse. '),
    el('span', null, 'Counts and markers leave ' + (files.length === 1 ? 'it' : 'them') + ' out: '),
    el('span', { class: 'mono' }, shown)
  ]);
}

// drawAsOf says when the view was read, or why it is older than it looks.
function drawAsOf(err) {
  var at = new Date(loadedAt);
  var two = function (n) { return (n < 10 ? '0' : '') + n; };
  if (!asof.firstChild) {
    fill(asof, [el('span'), el('button', { type: 'button', class: 'copy', title: 'Read the workspace again (r)', on: { click: refresh } }, 'Refresh'),
      el('button', { type: 'button', class: 'copy', title: 'Keys and terms (?)', 'aria-label': 'Keys and terms', on: { click: keys } }, '?')]);
  }
  var said = asof.firstChild;
  said.className = err ? 'late' : '';
  asof.title = err ? err.message || String(err) : '';
  text(said, err ? 'not refreshed' : 'as of ' + two(at.getHours()) + ':' + two(at.getMinutes()));
}

var colors = readColors();

var ctx = {
  colors: colors.ramp,
  ink: colors.ink,
  crumbs: setCrumbs,
  // nav pushes a history entry, so back returns to the previous view; it
  // reports whether it pushed one.
  nav: function (to) {
    var h = format(to);
    if (h === location.hash || (h === '#/' && location.hash === '')) { render(); return false; }
    location.hash = h;
    return true;
  },
  // replace rewrites the address without a history entry: typing into a
  // filter box is one step back, not one per keystroke.
  replace: function (to) {
    history.replaceState(history.state, '', location.pathname + location.search + format(to));
    render(true);
  },
  // entry is what the page remembers about the history entry on screen.
  entry: function () { return entry; },
  // ready says the view has drawn, so its scroll can come back.
  ready: function () {
    if (restore !== null) {
      main.scrollTop = restore;
      // A block still to load may be what makes the page long enough.
      if (main.scrollTop >= restore - 1) restore = null;
    }
    settled = true;
    entry.scroll = main.scrollTop;
  },
  // loaded says the view on screen was read just now, and which files the
  // reading had to leave out.
  loaded: function (malformed) {
    loadedAt = Date.now();
    drawAsOf(null);
    if (malformed) showBanner(malformed);
  },
  // failed is a refresh that did not answer: what is on screen stays.
  failed: function (err) { drawAsOf(err); },
  refresh: refresh,
  // listRoute is the list of a type as the reader last had it, with nothing
  // open beside it.
  listRoute: function (type) { return lists[type] ? opening(lists[type], '') : typeRoute(type); },
  // canOpen says whether an entity can be read beside the view on screen.
  canOpen: function (id) {
    return ['home', 'list', 'query', 'marked', 'entity'].indexOf(route.view) !== -1 && route.id !== id && panel.hasRoom();
  },
  open: function (id) {
    var to = opening(route, id);
    if (openId(route)) { ctx.replace(to); return; }
    // The same rows and groups stay drawn under the new entry.
    carried = { rows: entry.rows, groups: entry.groups };
    ctx.nav(to);
  },
  close: function () { ctx.replace(opening(route, '')); },
  // from is the list the entity on screen was opened from: its breadcrumb and
  // its rows. null for a pasted link, and for an entity the list does not hold.
  from: function () { return entry.from || null; },
  // page goes to the next or previous entity of that list, as a page. It takes
  // the entry over, so Back leaves the walk in one step.
  page: function (by, label) {
    var id = route.view === 'entity' && entry.from ? step(entry.from.rows, route.id, by) : '';
    if (!id || id === route.id) return;
    held = label || '';
    entry.groups = null;
    ctx.replace(entityRoute(id));
  },
  // held is the page control to put the keyboard back on, once.
  held: function () { var h = held; held = ''; return h; },
  route: function () { return route; }
};

// viewKey names what is on screen, ignoring sort, filters and what is open
// beside it: a change of key is a new page, which scrolls to the top.
function viewKey(r) {
  if (r.view === 'list') return 'list:' + r.type;
  if (r.view === 'entity') return 'entity:' + r.id;
  if (r.view === 'query' || r.view === 'marked') return r.view + ':' + r.name;
  return r.view;
}

// render draws the view the address names. here says the entry on screen only
// changed its address, so its place is where the reader is.
function render(here) {
  // A question asked over one view is not the next view's.
  Array.prototype.forEach.call(document.querySelectorAll('dialog[open]'), function (d) { d.close(); });
  var was = route && VIEWS[route.view] && VIEWS[route.view].order ? { route: route, rows: VIEWS[route.view].order(), trail: trail } : null;
  enter();
  route = parse(location.hash);
  if (fresh && was && was.route.view !== 'entity' && route.view === 'entity') comeFrom(was);
  var key = viewKey(route);
  if (key !== lastKey) {
    settled = false;
    restore = here === true ? 0 : entry.scroll || 0;
    main.scrollTop = 0;
    // A new page: move focus to it, so the keys that scroll work at once and a
    // keyboard or screen-reader user starts at the top of what just opened
    // rather than on a link that is gone.
    main.focus({ preventScroll: true });
    lastKey = key;
  } else if (here !== true && entry.scroll !== undefined) {
    restore = entry.scroll;
  }
  if (route.view === 'list') lists[route.type] = route;
  Object.keys(VIEWS).forEach(function (name) { if (VIEWS[name] !== VIEWS[route.view]) VIEWS[name].unmount(); });
  document.body.dataset.view = route.view;
  drawTabs();
  var view = VIEWS[route.view];
  if (view) { view.show(main, route, ctx, false); return; }
  setCrumbs([{ label: 'Not found' }]);
  fill(main, [notice('Nothing at this address', 'The address names no view of this workspace.',
    [{ label: 'Go to the review', href: '#/' }])]);
}

// comeFrom keeps with an entity's entry the list it was opened from, when the
// list holds it.
function comeFrom(was) {
  if (!was.rows.some(function (r) { return r.id === route.id; })) return;
  var back = opening(was.route, '');
  entry.from = {
    route: back,
    rows: was.rows.map(function (r) { return { id: r.id }; }),
    trail: was.trail.slice(0, -1).concat([{ label: was.trail[was.trail.length - 1].label, route: back }])
  };
}

// refresh reads the view on screen again and puts the reader back where they
// were. What is on screen stays until the new reading arrives.
function refresh() {
  var view = route && VIEWS[route.view];
  if (!view) return;
  restore = main.scrollTop;
  refreshedAt = Date.now();
  view.show(main, route, ctx, true);
}

function onReturn() {
  // The window's return raises two events, and a reading may still be on its way.
  if (document.visibilityState === 'visible' && loadedAt && Date.now() - Math.max(loadedAt, refreshedAt) > REFRESH_MS) refresh();
}

function typing(node) {
  return node && (/^(INPUT|SELECT|TEXTAREA)$/.test(node.tagName) || node.isContentEditable);
}

// move opens the next or previous row of the view on screen beside it.
function move(by) {
  var view = VIEWS[route.view];
  var rows = view && view.order ? view.order() : [];
  var id = step(rows, openId(route), by, panel.placeOf(openId(route)));
  if (!id || id === openId(route)) return;
  if (!panel.hasRoom()) return;
  var follow = document.activeElement.closest && document.activeElement.closest('tr, li');
  ctx.open(id);
  // The keyboard on a row moves with the open one, so Enter opens what is open.
  if (follow && main.contains(follow)) setTimeout(function () { if (view.focusOn) view.focusOn(id); }, 0);
}

function onKey(evt) {
  if (evt.metaKey || evt.ctrlKey || evt.altKey) return;
  if (evt.key === 'Escape') {
    if (find.close() || typing(evt.target) || document.querySelector('dialog[open]')) return;
    if (openId(route)) ctx.close();
    else if (VIEWS[route.view] && VIEWS[route.view].escape) VIEWS[route.view].escape(ctx);
    return;
  }
  if (typing(evt.target) || document.querySelector('dialog[open]')) return;
  switch (evt.key) {
    case '/': evt.preventDefault(); find.focus(); break;
    case 'r': refresh(); break;
    case 'j': move(1); break;
    case 'k': move(-1); break;
    case ']': ctx.page(1); break;
    case '[': ctx.page(-1); break;
    case '?': keys(); break;
    case 'Enter':
      if (openId(route) && (evt.target === document.body || evt.target === main)) ctx.nav(entityRoute(openId(route)));
      break;
  }
}

// keys lists the keyboard's shortcuts, and what the page's terms mean.
function keys() {
  var list = function (pairs) {
    var dl = el('dl', { class: 'keys' });
    pairs.forEach(function (k) {
      dl.appendChild(el('dt', null, k[0]));
      dl.appendChild(el('dd', null, k[1]));
    });
    return dl;
  };
  var box = el('dialog', { class: 'keybox', 'aria-label': 'Keys and terms' });
  box.appendChild(el('h2', null, 'Keys'));
  box.appendChild(list(  [['/', 'Search'], ['j, k', 'Open the next or previous row, or link of a page, beside it'], ['], [', 'Go to the next or previous page of the list it was opened from'], ['Enter', 'Open what is beside the list as a page'],
    ['Esc', 'Close what is open, or leave one type for the full map'], ['r', 'Read the workspace again'], ['Shift-click', 'Leave a filter value out'], ['?', 'This list']]));
  box.appendChild(el('h2', null, 'Markers'));
  box.appendChild(list(MARKERS.map(function (k) { return [markerName(k), MEANING[k]]; })));
  box.appendChild(el('h2', null, 'Trust'));
  box.appendChild(list(TIERS.map(function (k) { return [k, MEANING[k]]; })));
  box.appendChild(el('button', { type: 'button', class: 'btn', on: { click: function () { box.close(); } } }, 'Close'));
  box.addEventListener('close', function () { box.remove(); });
  document.body.appendChild(box);
  box.showModal();
}

function boot() {
  main = $('main');
  tabs = $('tabs');
  asof = $('asof');
  banner = $('banner');
  // `khub serve --aspect` prints an address carrying it, and every value of it
  // opens the diagram.
  if (new URLSearchParams(location.search).has('aspect') && /^#?\/?$/.test(location.hash)) {
    history.replaceState(null, '', location.pathname + format(overviewOf('')));
  }
  find.mount($('find'), ctx);
  window.addEventListener('hashchange', render);
  window.addEventListener('focus', onReturn);
  document.addEventListener('visibilitychange', onReturn);
  document.addEventListener('keydown', onKey);
  main.addEventListener('scroll', function () { if (settled) entry.scroll = main.scrollTop; }, { passive: true });
  // A reader who moves the page has taken it over.
  ['wheel', 'touchmove', 'mousedown', 'keydown'].forEach(function (evt) {
    main.addEventListener(evt, function () { restore = null; }, { passive: true });
  });
  render();
  // Names the workspace: a reviewer of two of them has two of these tabs.
  // Without an answer the header goes unnamed, and the view on screen reports
  // what is wrong through its own request.
  api.getSchema().then(function (schema) {
    workspace = schema.workspace || '';
    if (workspace) text($('brand'), workspace);
    $('brand').title = 'khub: ' + workspace;
    setTitle();
  }, function () {});
}

boot();
