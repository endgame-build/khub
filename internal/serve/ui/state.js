// state.js — the shape, the reducer, the diff. Pure: no document, no
// cytoscape, no fetch.
//
// Two decisions carry this file:
//
// 1. The breadcrumb IS the level. One focus stack, not `level` + `currentType`
//    + `currentEntity` + `breadcrumb` — four fields that can disagree, and
//    reconciling them is where this kind of code turns to spaghetti.
//    `focus.length` is the level, and `focus.length <= 2` is an invariant by
//    construction: there is no L3 to reason about.
//
// 2. There is no mode flag. Phase 3's command composer adds state but never a
//    mode, because a mode would make one click mean two things. See panels.js
//    — composing is driven from the inspector's buttons, not from the canvas.
//
// Visibility is deliberately absent: it is derived by model.computeVisible from
// focus + depth + filters. Storing it would mean owning its consistency with
// five inputs.

import { typeOf, composeLink, composeUnlink } from './model.js';

export function initial(baseRelations, derivedRelations) {
  return {
    focus: [],
    depth: 1,
    hiddenTypes: new Set(),
    // The four base predicates (related/sources/references/depends_on) are
    // `to: any` on every type: simultaneously the densest edges in the graph
    // and the least informative. Hidden by default; the Filters panel says so.
    hiddenPredicates: new Set((baseRelations || []).concat(derivedRelations || [])),
    search: '',
    // Three peers over one workspace: 'schema' is what CAN exist, 'graph' and
    // 'list' are what does, as a picture or as rows. Graph is where you land,
    // because you almost always arrive wanting the data; schema is there when
    // you need the contract.
    //
    // Levels apply to the two instance views only — an ontology is flat.
    //
    // Schema lands, not graph. Graph used to open on a synthetic per-type
    // picture; that turned out to be the schema view with different semantics,
    // so it went, and unscoped Graph is now every entity — 2,000 of them, which
    // is the hairball this whole view system exists to avoid showing first.
    view: 'schema',
    // Ordering is state so it survives re-renders. Slug ascending by default:
    // the slug is the argument a khub command takes, so the list and the CLI
    // agree on order. Every sort tie-breaks on slug, which makes the ordering
    // total — Array#sort is stable, so without a tie-break a degree sort would
    // land differently depending on what was sorted before it.
    sort: { key: 'slug', dir: 'asc' },
    inspect: null,
    compose: { a: null, b: null, predicate: null, queue: [] },
    notice: ''
  };
}

function set(state, patch) { return Object.assign({}, state, patch); }

function toggled(setObj, key) {
  var next = new Set(setObj);
  if (next.has(key)) next.delete(key); else next.add(key);
  return next;
}

export function reduce(state, a) {
  switch (a.type) {
    case 'NAV_ROOT':
      return set(state, { focus: [], notice: '' });

    // Leaving the ontology for the data: scope to the type AND switch to the
    // list, which is what a type-sized set of entities wants to be.
    case 'OPEN_TYPE':
      return set(state, {
        focus: [{ kind: 'type', name: a.name }], depth: 0, view: 'list',
        inspect: null, notice: ''
      });

    case 'NAV_TYPE':
      // Depth 0: a type opens as itself. The + control pulls neighbours in from
      // there, so the old "everything one hop out" view is a choice rather than
      // the default.
      return set(state, { focus: [{ kind: 'type', name: a.name }], depth: 0, notice: '' });

    case 'NAV_ENTITY': {
      // Keep the type crumb already on the stack so the breadcrumb reads as the
      // path taken ("client > person/noor" when a person was reached from
      // inside the client view). Because ids are `<type>/<slug>`, a cold jump
      // from search reconstructs its own crumb and needs no separate path.
      var crumb = (state.focus.length && state.focus[0].kind === 'type')
        ? state.focus[0]
        : { kind: 'type', name: typeOf(a.id) };
      // Navigating to an entity inspects it. Every route in reaches it because
      // the operator wants to see it — from the canvas, from a search hit, from
      // an edge target in the inspector itself — and the composer's Set as A/B
      // buttons live in that panel, so a navigation that left it empty would
      // make an entity found by search impossible to pick.
      return set(state, {
        focus: [crumb, { kind: 'entity', id: a.id }],
        depth: 1, notice: '',
        inspect: { id: a.id, status: 'loading' }
      });
    }

    case 'NAV_CRUMB': {
      // Coming back up to a type restores that level's own default rather than
      // carrying the entity view's radius with it.
      var back = state.focus.slice(0, a.i);
      return set(state, { focus: back, depth: back.length === 1 ? 0 : state.depth, notice: '' });
    }

    case 'EXPAND':
      return state.depth >= 3 ? state : set(state, { depth: state.depth + 1 });

    // Down to 0, not 1: at a type that means "this type only", and at an entity
    // it means the entity by itself. Both are meaningful floors.
    case 'CONTRACT':
      return state.depth <= 0 ? state : set(state, { depth: state.depth - 1 });

    case 'TOGGLE_FILTER':
      return a.axis === 'types'
        ? set(state, { hiddenTypes: toggled(state.hiddenTypes, a.key) })
        : set(state, { hiddenPredicates: toggled(state.hiddenPredicates, a.key) });

    case 'SET_SEARCH':
      return set(state, { search: a.text });

    case 'SET_VIEW':
      return state.view === a.view ? state : set(state, { view: a.view });

    case 'SET_SORT': {
      if (state.sort.key === a.key) {
        return set(state, { sort: { key: a.key, dir: state.sort.dir === 'asc' ? 'desc' : 'asc' } });
      }
      // First press on a magnitude column shows the interesting end first:
      // most-connected, and problems before healthy rows.
      var dir = (a.key === 'degree' || a.key === 'flags') ? 'desc' : 'asc';
      return set(state, { sort: { key: a.key, dir: dir } });
    }

    case 'INSPECT':
      return set(state, { inspect: { id: a.id, status: 'loading' } });

    // A type is not an entity: it has a name rather than an id, and it is
    // fetched from /api/type. Kept in the same slice so one panel renders
    // whatever is currently being looked at.
    case 'INSPECT_TYPE':
      return set(state, { inspect: { kind: 'type', name: a.name, status: 'loading' } });

    case 'INSPECT_TYPE_OK':
      // Same stale-response guard as entities, keyed on the name.
      if (!state.inspect || state.inspect.name !== a.name) return state;
      return set(state, { inspect: { kind: 'type', name: a.name, status: 'ready', data: a.data } });

    case 'INSPECT_TYPE_ERR':
      if (!state.inspect || state.inspect.name !== a.name) return state;
      return set(state, { inspect: { kind: 'type', name: a.name, status: 'error', error: a.message } });

    // A declared relation carries everything it means in the element's own
    // data — nothing to fetch.
    case 'INSPECT_RELATION':
      return set(state, { inspect: { kind: 'relation', status: 'ready', data: a.data } });

    case 'COMPOSE_PICK': {
      var c = Object.assign({}, state.compose);
      c[a.slot] = a.id;
      // A predicate legal for the old pair may be illegal for the new one.
      c.predicate = null;
      return set(state, { compose: c });
    }

    case 'COMPOSE_PREDICATE':
      return set(state, { compose: Object.assign({}, state.compose, { predicate: a.predicate }) });

    case 'COMPOSE_ENQUEUE': {
      var q = state.compose;
      if (!q.a || !q.b || !q.predicate) return state;
      return set(state, {
        compose: {
          a: null, b: null, predicate: null,
          queue: q.queue.concat([composeLink(q.a, q.predicate, q.b)])
        }
      });
    }

    case 'COMPOSE_UNLINK':
      return set(state, {
        compose: Object.assign({}, state.compose, {
          queue: state.compose.queue.concat([composeUnlink(a.source, a.predicate, a.target)])
        })
      });

    case 'COMPOSE_CLEAR':
      return set(state, { compose: { a: null, b: null, predicate: null, queue: [] } });

    case 'NOTICE':
      return set(state, { notice: a.text });

    default:
      return state;
  }
}

// diff names the slices that moved. Every reducer branch above builds new
// objects and new Sets for what it changes, so identity comparison is enough.
export function diff(prev, next) {
  return {
    focus: prev.focus !== next.focus,
    depth: prev.depth !== next.depth,
    filters: prev.hiddenTypes !== next.hiddenTypes || prev.hiddenPredicates !== next.hiddenPredicates,
    search: prev.search !== next.search,
    view: prev.view !== next.view,
    sort: prev.sort !== next.sort,
    inspect: prev.inspect !== next.inspect,
    compose: prev.compose !== next.compose,
    notice: prev.notice !== next.notice
  };
}
