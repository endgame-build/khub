// app.js — boot and the router.
//
// The page is three views over one address: the ontology overview, one type's
// list, one entity. Everything a view shows is derived from the URL fragment
// (route.js), so reload, back/forward and a pasted link all restore it. Each
// view fetches what it needs when it opens; nothing polls.

import * as overview from './overview.js';
import * as list from './list.js';
import * as entity from './entity.js';
import { parse, format } from './route.js';
import { el, fill } from './dom.js';
import { notice } from './widgets.js';

var main, crumbs;
var lastKey = '';

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

// setCrumbs draws the breadcrumb trail: every item but the last is a link.
function setCrumbs(items) {
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
  // nav pushes a history entry, so back returns to the previous view.
  nav: function (route) {
    var h = format(route);
    if (h === location.hash || (h === '#/' && location.hash === '')) { render(); return; }
    location.hash = h;
  },
  // replace rewrites the address without a history entry: typing into a
  // filter box is one step back, not one per keystroke.
  replace: function (route) {
    history.replaceState(null, '', location.pathname + location.search + format(route));
    render();
  }
};

// viewKey names what is on screen, ignoring sort and filters: a change of key
// is a new page, which scrolls to the top.
function viewKey(route) {
  if (route.view === 'list') return 'list:' + route.type;
  if (route.view === 'entity') return 'entity:' + route.id;
  return route.view;
}

function render() {
  var route = parse(location.hash);
  var key = viewKey(route);
  if (key !== lastKey) {
    main.scrollTop = 0;
    lastKey = key;
  }
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
  render();
}

boot();
