// app.js — boot and the router.
//
// The page is three views over one address: the ontology overview, one type's
// list, one entity. Everything a view shows is derived from the URL fragment
// (route.js), so reload, back/forward and a pasted link all restore it. Each
// view fetches what it needs when it opens; nothing polls.

import * as overview from './overview.js';
import * as list from './list.js';
import * as entity from './entity.js';
import * as api from './api.js';
import { parse, format, typeRoute } from './route.js';
import { el, fill } from './dom.js';
import { notice } from './widgets.js';

var main, crumbs;
var lastKey = '';
var workspace = ''; // its name, once /api/schema answers
var page = '';      // what the tab title calls the view on screen
var lists = Object.create(null); // type → the list address last shown for it

// What the page remembers per history entry: scroll, rows drawn, the
// diagram's viewport. An entry is stamped the first time it is shown, so back
// and forward find it again; a reload starts with nothing remembered.
var entries = {};
var entry = {};
var stamps = 0;
var restore = null;   // the scroll to put back once the view has drawn
var settled = false;  // no scroll is recorded while a view is still loading

function enter() {
  var id = history.state && history.state.khub;
  if (!id) {
    stamps += 1;
    id = Date.now() + '-' + stamps;
    history.replaceState({ khub: id }, '');
  }
  entries[id] = entries[id] || {};
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

// setCrumbs draws the breadcrumb trail — every item but the last is a link —
// and names the browser tab after the page it ends on.
function setCrumbs(items) {
  if (items.length) { page = items[items.length - 1].label; setTitle(); }
  var nodes = [];
  items.forEach(function (it, i) {
    if (i) nodes.push(el('span', { class: 'sep', 'aria-hidden': 'true' }, '›'));
    if (it.route && i < items.length - 1) nodes.push(el('a', { class: 'crumb', href: format(it.route) }, it.label));
    else nodes.push(el('span', { class: 'crumb here', 'aria-current': 'page' }, it.label));
  });
  fill(crumbs, nodes);
}

var colors = readColors();

var ctx = {
  colors: colors.ramp,
  ink: colors.ink,
  crumbs: setCrumbs,
  // nav pushes a history entry, so back returns to the previous view; it
  // reports whether it pushed one.
  nav: function (route) {
    var h = format(route);
    if (h === location.hash || (h === '#/' && location.hash === '')) { render(); return false; }
    location.hash = h;
    return true;
  },
  // replace rewrites the address without a history entry: typing into a
  // filter box is one step back, not one per keystroke.
  replace: function (route) {
    history.replaceState(history.state, '', location.pathname + location.search + format(route));
    render();
  },
  // entry is what the page remembers about the history entry on screen.
  entry: function () { return entry; },
  // ready says the view has drawn, so its scroll can come back.
  ready: function () {
    if (restore !== null) main.scrollTop = restore;
    restore = null;
    settled = true;
    entry.scroll = main.scrollTop;
  },
  // listRoute is the list of a type as the reader last had it.
  listRoute: function (type) { return lists[type] || typeRoute(type); }
};

// viewKey names what is on screen, ignoring sort and filters: a change of key
// is a new page, which scrolls to the top.
function viewKey(route) {
  if (route.view === 'list') return 'list:' + route.type;
  if (route.view === 'entity') return 'entity:' + route.id;
  return route.view;
}

function render() {
  enter();
  var route = parse(location.hash);
  var key = viewKey(route);
  if (key !== lastKey) {
    settled = false;
    restore = entry.scroll || 0;
    main.scrollTop = 0;
    // A new page: move focus to it, so the keys that scroll work at once and a
    // keyboard or screen-reader user starts at the top of what just opened
    // rather than on a link that is gone.
    main.focus({ preventScroll: true });
    lastKey = key;
  } else if (entry.scroll !== undefined) {
    restore = entry.scroll;
  }
  if (route.view === 'list') lists[route.type] = route;
  if (route.view !== 'overview') overview.unmount();
  if (route.view !== 'list') list.unmount();
  if (route.view !== 'entity') entity.unmount();
  document.body.dataset.view = route.view;
  switch (route.view) {
    case 'overview': overview.show(main, route, ctx); break;
    case 'list': list.show(main, route, ctx); break;
    case 'entity': entity.show(main, route, ctx); break;
    default:
      setCrumbs([{ label: 'Ontology', route: { view: 'overview', sel: '' } }, { label: 'Not found' }]);
      fill(main, [notice('Nothing at this address', 'The address names no view of this workspace.',
        [{ label: 'Back to the ontology', href: '#/' }])]);
  }
}

function boot() {
  main = $('main');
  crumbs = $('crumbs');
  window.addEventListener('hashchange', render);
  main.addEventListener('scroll', function () { if (settled) entry.scroll = main.scrollTop; }, { passive: true });
  render();
  // Names the workspace: a reviewer of two of them has two of these tabs.
  // Without an answer the header goes unnamed, and the view on screen reports
  // what is wrong through its own request.
  api.getSchema().then(function (schema) {
    workspace = schema.workspace || '';
    fill($('brand'), [el('span', null, 'khub'), workspace ? el('span', { class: 'ws' }, workspace) : null]);
    setTitle();
  }, function () {});
}

boot();
