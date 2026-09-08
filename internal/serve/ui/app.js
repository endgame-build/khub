// app.js — boot, dispatch, the render dispatcher, the effect runner.
//
// The only module allowed to reach for everything. Every state change in the
// application goes through the one dispatch below: reduce (pure) → render (the
// only place that touches the document or cytoscape) → effects (the only place
// that fetches).

import * as api from './api.js';
import * as gfx from './cy.js';
import * as panels from './panels.js';
import { initial, reduce, diff } from './state.js';
import { buildModel, indexSchema, computeVisible, legalTargetTypes, typeOf,
         buildSchemaGraph, usedEdgeCounts } from './model.js';
import { el, fill, text } from './dom.js';

var state, model, schema, colors, ink, typeOrder;
var lastLayout = '';
// The view's own explanation of what is on screen (a layout fallback, edges the
// predicate filter is hiding). Held because it outlives the render that
// produced it and must not be destroyed by a passing message.
var viewNotice = '';
// computeVisible walks every edge, and render() used to call it on every
// dispatch — including each search keystroke. It depends on exactly three
// slices, so it is recomputed only when one of them moves.
var lastVisible = null;
var lastListMs = 0;
// Where the list was scrolled, per type. Without this, clicking a row and
// coming back drops you at the top, which ruins the scan-check-scan job the
// list exists for.
var listScroll = {};
// The ontology, as introspect.EdgesView built it. Fetched once with the schema.
var schemaEdges = [];
var dom = {};

function $(id) { return document.getElementById(id); }

// readColors resolves Paper's sanctioned graph ramp out of the stylesheet.
// app.js holds no colour literal of its own: the token sheet is the source, and
// cytoscape needs hex because it parses colours itself and understands neither
// custom properties nor lab().
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

function dispatch(action) {
  if (action.type === 'NAV_ENTITY') rememberScroll();
  var next = reduce(state, action);
  if (next === state) return;
  var changed = diff(state, next);
  state = next;
  render(changed);
  effects(changed);
}

function render(changed) {
  if (!lastVisible || changed.focus || changed.depth || changed.filters) {
    lastVisible = computeVisible(state, model);
  }
  var visible = lastVisible;
  // View decides first, level second. computeVisible describes the instance
  // graph and says nothing about an ontology, so schema must not be reasoned
  // about through it.
  var asSchema = state.view === 'schema';
  var asList = !asSchema && state.view === 'list';
  var moved = changed.focus || changed.depth || changed.filters || changed.view;

  // A view switch changes which population is on the canvas, and lastLayout is
  // one memo across all of them. Left stale, returning to a view whose layout
  // name happens to match declines to run — leaving freshly added elements
  // stacked at the origin.
  if (changed.view) lastLayout = '';

  if (asSchema && moved) {
    var sch = buildSchemaGraph(schema, schemaEdges, state.hiddenPredicates,
                               usedEdgeCounts(model), model.byType);
    var schIds = new Set(gfx.setOverlay(sch));
    gfx.setLevelClass(0);
    gfx.showOnly(schIds);
    // An ontology is layered, not a force field: requirement -> component ->
    // repo is a chain, and cose with three connected pairs and three isolates
    // has almost nothing to push against, so it scattered them — a connected
    // type ended up further from its neighbour than the unconnected ones.
    gfx.runLayout({ name: 'schema' });
    viewNotice = schemaNotice(sch);
    renderNotice();
  }

  if (!asSchema && !asList && moved) {
    // No synthetic elements in the instance graph any more — the ontology is
    // where the type level lives.
    gfx.setOverlay(null);
    var ids = new Set();
    visible.nodes.forEach(function (id) { ids.add(id); });
    visible.edges.forEach(function (id) { ids.add(id); });

    gfx.setLevelClass(visible.level);
    gfx.showOnly(ids);

    // Navigation relayouts; filters do not. Re-running a force-directed layout
    // because someone toggled a legend row is both slow and maddening. The
    // exception is a change of layout ALGORITHM: un-hiding a predicate can turn
    // an edgeless set into a connected one, and a grid laid out for no edges is
    // the wrong shape for a graph that now has a hundred.
    if (changed.focus || changed.depth || changed.view ||
        visible.layout.name !== lastLayout) {
      lastLayout = visible.layout.name;
      gfx.runLayout(visible.layout);
    }
  }

  if (moved || changed.sort) {
    dom.list.hidden = !asList;
    if (asList) {
      var t0 = performance.now();
      panels.renderList(dom.list, state, model, visible, colors, typeOrder);
      // focus is empty at the top level, where the list spans the workspace and
      // there is no per-type scroll position to restore.
      dom.list.scrollTop = state.focus.length ? (listScroll[state.focus[0].name] || 0) : 0;
      lastListMs = Math.round(performance.now() - t0);
    }
    // The notice explains the canvas — a layout fallback, edges a filter is
    // hiding. It says nothing about a list, and #notice floats above one.
    viewNotice = (asList || asSchema) ? '' : visible.notice;
    if (!asSchema) renderNotice();
  }

  if (changed.focus) panels.renderCrumbs(dom.crumbs, state, model);
  if (changed.filters) panels.renderFilters(dom.filters, state, model, schema, colors, typeOrder);
  if (changed.search) panels.renderSearch(dom.results, state, model);
  if (changed.focus || changed.filters || changed.depth) {
    panels.renderCounts(dom.counts, state, model, visible);
  }
  if (changed.inspect || changed.focus) panels.renderInspector(dom.inspector, state, model);
  if (changed.compose) {
    panels.renderComposer(dom.composer, state, schema);
    gfx.mark(state.compose.a, state.compose.b,
      state.compose.a ? legalTargetTypes(schema, typeOf(state.compose.a)) : null);
  }
  if (changed.notice) renderNotice();
  if (changed.focus || changed.view) {
    // All three are always available now — they are peers over one workspace,
    // not a toggle that only means something at one level.
    dom.asschema.className = 'btn-ghost' + (state.view === 'schema' ? ' on' : '');
    dom.asgraph.className = 'btn-ghost' + (state.view === 'graph' ? ' on' : '');
    dom.aslist.className = 'btn-ghost' + (state.view === 'list' ? ' on' : '');
    // Levels and depth belong to the instance views; an ontology is flat.
    dom.crumbs.hidden = asSchema;
    dom.depthctl.hidden = asSchema;
  }
  if ((changed.depth || changed.focus || changed.view) && !asSchema) {
    // Live at both levels now: at a type the + control is how other types come
    // back into view.
    dom.expand.disabled = !state.focus.length || state.depth >= 3;
    dom.contract.disabled = !state.focus.length || state.depth <= 0;
    text(dom.depth, state.focus.length
      ? (state.depth === 0 ? 'this type only' : 'depth ' + state.depth)
      : '');
  }
}

// An explanation of the current view always wins over a transient message.
// They shared one slot before, so copying a command at L1 erased "101 edges are
// hidden here" — the answer to a question the reader was still asking.
function renderNotice() { panels.renderNotice(dom.notice, viewNotice || state.notice); }

function effects(changed) {
  if (changed.inspect && state.inspect && state.inspect.kind === 'type' && state.inspect.status === 'loading') {
    var name = state.inspect.name;
    api.getType(name).then(
      function (data) { dispatch({ type: 'INSPECT_TYPE_OK', name: name, data: data }); },
      function (err) { dispatch({ type: 'INSPECT_TYPE_ERR', name: name, message: err.message }); }
    );
    return;
  }
  if (changed.inspect && state.inspect && state.inspect.status === 'loading') {
    var id = state.inspect.id;
    api.getEntity(id).then(
      function (data) { dispatch({ type: 'INSPECT_OK', id: id, data: data }); },
      function (err) { dispatch({ type: 'INSPECT_ERR', id: id, message: err.message }); }
    );
  }
}

// onGraph is the single meaning of a canvas click: focus what was clicked.
// There is no selection mode — the command composer is driven from the
// inspector's buttons, so one click never has two meanings.
// schemaNotice says what the ontology is not showing. Unhiding the universal
// predicates draws every type against every type, so their absence is worth
// explaining rather than leaving as a silently partial picture.
function schemaNotice(sch) {
  var base = schema.baseRelations.filter(function (p) { return state.hiddenPredicates.has(p); });
  var derived = (schema.derivedRelations || []).filter(function (p) { return state.hiddenPredicates.has(p); });
  var parts = [];
  if (base.length) parts.push(base.length + ' universal (' + base.join(', ') + ') — shown, they connect every type to every type');
  if (derived.length) parts.push(derived.length + ' derived inverse' + (derived.length === 1 ? '' : 's') + ' (' + derived.join(', ') + ')');
  return parts.length ? 'Hidden: ' + parts.join(' · ') : '';
}

function rememberScroll() {
  if (state.focus.length === 1 && state.view === 'list') {
    listScroll[state.focus[0].name] = dom.list.scrollTop;
  }
}

function onGraph(evt) {
  // A schema node is a declared type, not an entity and not a drill-down
  // target: clicking it explains the contract rather than switching what you
  // are looking at.
  if (evt.schema) {
    if (evt.target === 'node' && evt.data.schemaKind === 'type') {
      dispatch({ type: 'INSPECT_TYPE', name: evt.data.type });
    } else if (evt.target === 'edge') {
      dispatch({ type: 'INSPECT_RELATION', data: evt.data });
    }
    return;
  }
  if (evt.target === 'node') {
    // NAV_ENTITY inspects as part of navigating, so this is one action.
    dispatch({ type: 'NAV_ENTITY', id: evt.id });
    return;
  }
  if (evt.target === 'edge') {
    // An edge has nothing to drill into, so its click is unambiguous.
    dispatch({
      type: 'COMPOSE_UNLINK',
      source: evt.data.source, predicate: evt.data.label, target: evt.data.target
    });
    dispatch({ type: 'NOTICE', text: 'Queued an unlink for ' + evt.data.label });
  }
}

function boot() {
  dom = {
    cy: $('cy'), crumbs: $('crumbs'), counts: $('counts'), filters: $('filters'),
    inspector: $('inspector'), composer: $('composer'), results: $('results'),
    search: $('search'), notice: $('notice'), expand: $('expand'),
    contract: $('contract'), depth: $('depth'), list: $('list'),
    aslist: $('aslist'), asgraph: $('asgraph'), asschema: $('asschema'),
    depthctl: $('depthctl')
  };

  Promise.all([api.getGraph(), api.getSchema()]).then(function (r) {
    model = buildModel(r[0]);
    schema = indexSchema(r[1]);
    schemaEdges = r[1].edges || [];
    var c = readColors();
    colors = c.ramp;
    ink = c.ink;

    // One ordering, used by both the canvas and the legend — indexing them
    // separately made a zero-count type magenta in the legend and grey on the
    // canvas.
    //
    // Present types first, sorted, which is exactly viz.go's own rule
    // (sortedKeys over the types present). That is what keeps a type the same
    // colour in the served app and in the static artifact. Declared-but-empty
    // types follow, so they still get a colour for their L0 circle; viz never
    // draws them, so the tail cannot disagree with anything.
    typeOrder = model.types.concat(
      schema.declaredTypes.filter(function (t) { return !model.byType.has(t); }).sort()
    );
    state = initial(schema.baseRelations, schema.derivedRelations);
    panels.init(dispatch);

    gfx.mount(dom.cy, { nodes: model.nodes, edges: model.edges },
      gfx.buildStyle(typeOrder, colors, ink));
    gfx.applyFlags();
    gfx.on(onGraph);
    gfx.hoverLabels();

    dom.search.addEventListener('input', function () {
      dispatch({ type: 'SET_SEARCH', text: dom.search.value });
    });
    dom.asschema.addEventListener('click', function () {
      gfx.setSnapNextFit();
      dispatch({ type: 'SET_VIEW', view: 'schema' });
    });
    dom.aslist.addEventListener('click', function () { dispatch({ type: 'SET_VIEW', view: 'list' }); });
    dom.asgraph.addEventListener('click', function () {
      // Snap rather than tween: the canvas sat under the list with a stale
      // viewport, so an animated fit would start from nowhere.
      gfx.setSnapNextFit();
      dispatch({ type: 'SET_VIEW', view: 'graph' });
    });
    dom.expand.addEventListener('click', function () { dispatch({ type: 'EXPAND' }); });
    dom.contract.addEventListener('click', function () { dispatch({ type: 'CONTRACT' }); });

    // Snap the first fit rather than animating it: cytoscape fits to every
    // element at init, so the opening frame is the whole 2,000-node preset
    // spread zoomed out, and animating away from it reads as a flash.
    gfx.setSnapNextFit();
    render({
      focus: true, depth: true, filters: true, search: true,
      inspect: true, compose: true, notice: true, view: true, sort: true
    });

    if (model.droppedDuplicateEdges) {
      dispatch({
        type: 'NOTICE',
        text: 'Collapsed ' + model.droppedDuplicateEdges + ' duplicate edge record(s)'
      });
    }
  }).catch(function (err) {
    fill(dom.inspector, [
      el('div', { class: 'cap' }, 'Could not load'),
      el('div', { class: 'err' }, err.message || String(err))
    ]);
  });
}

boot();
