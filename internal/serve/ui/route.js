// route.js — the page's address: parse and format the URL fragment.
//
// Pure: no document, no fetch, no imports, so node --test loads it directly.
// Every view's whole state lives here, which is what
// makes a reload, back/forward or a pasted link restore the same view. The
// fragment never reaches the server, so filters never touch the request path
// the Host guard sees.
//
//   #/                         the review home   ?days
//   #/types                    every type
//   #/ontology                 the diagram       ?sel=type:<name> | rel:<from>.<predicate> ?focus ?map=1
//   #/type/<name>              one type's list   ?sort ?dir ?q ?rel+to ?count… ?attr… ?status… ?trust… ?by… ?not… ?base ?open
//   #/query/<name>             a saved query     ?open
//   #/entity/<type>/<slug>     one entity        ?open
//
// A parameter marked … may repeat. `#/?sel=…` still opens the diagram.
//
// `?aspect=` from `khub serve --aspect` rides on location.search, not the
// fragment, so it never reaches this parser.

// The markers in the order a reviewer acts on them.
export var STATUSES = ['incomplete', 'superseded', 'expired', 'orphan', 'draft', 'stale'];
// Review tiers, lowest first (internal/trust Tiers).
export var TIERS = ['unverified', 'machine', 'human'];
export var DAYS = 7; // the review home's window when none is asked for
var MAX_DAYS = 3650;
var DEFAULT_SORT = 'title';
var DEFAULT_DIR = 'asc';
// Format order: fixed, so one view has one address.
var LIST_KEYS = ['sort', 'dir', 'q', 'rel', 'to', 'count', 'attr', 'status', 'trust', 'by', 'not', 'base', 'open'];
var MANY = ['count', 'attr', 'status', 'trust', 'by', 'not'];
// A count bound, as `khub query --count` spells one: <column key><op><n>.
var COUNT_RE = /^((?:out|in):.+?)(>=|<=|!=|=|>|<)(\d+)$/;
// What `not` excludes: a filter of one of these kinds, spelled as its own key
// spells it.
var NOT_RE = /^(attr|status|trust|by|count):(.+)$/;

function decode(seg) {
  try { return decodeURIComponent(seg); } catch (e) { return null; }
}

function emptyList() {
  return {
    sort: DEFAULT_SORT, dir: DEFAULT_DIR, q: '', rel: '', to: '',
    count: [], attr: [], status: [], trust: [], by: [], not: [], base: false, open: ''
  };
}

// valid says whether one value of a repeating key means anything.
function valid(key, v) {
  switch (key) {
    case 'count': return COUNT_RE.test(v);
    case 'attr': return v.indexOf('=') > 0;
    case 'status': return STATUSES.indexOf(v) !== -1;
    case 'trust': return TIERS.indexOf(v) !== -1;
    case 'by': return v !== '';
    case 'not': {
      var m = NOT_RE.exec(v);
      return !!m && valid(m[1], m[2]);
    }
  }
  return false;
}

// parse reads a location.hash ('' or '#/...') into a route. A malformed
// address is the not-found view, never a throw: a typo in the address bar must
// not leave the page unmounted.
export function parse(hash) {
  var h = (hash || '').replace(/^#/, '');
  var qi = h.indexOf('?');
  var pathPart = qi === -1 ? h : h.slice(0, qi);
  // URLSearchParams decodes a malformed escape to U+FFFD instead of throwing.
  var params = new URLSearchParams(qi === -1 ? '' : h.slice(qi + 1));
  var segs = pathPart.replace(/^\//, '').split('/');

  if (segs.length === 1 && segs[0] === '') {
    if (params.get('sel') !== null) return overviewRoute(params);
    return { view: 'home', days: days(params.get('days')) };
  }
  if (segs[0] === 'types' && segs.length === 1) return { view: 'types' };
  if (segs[0] === 'ontology' && segs.length === 1) return overviewRoute(params);
  if (segs[0] === 'type' && segs.length === 2 && segs[1] !== '') {
    var name = decode(segs[1]);
    if (name === null) return { view: 'notfound' };
    return { view: 'list', type: name, list: listParams(params) };
  }
  if (segs[0] === 'query' && segs.length === 2 && segs[1] !== '') {
    var q = decode(segs[1]);
    if (q === null) return { view: 'notfound' };
    return { view: 'query', name: q, open: params.get('open') || '' };
  }
  if (segs[0] === 'entity' && segs.length >= 2) {
    var parts = segs.slice(1).map(decode);
    if (parts.some(function (p) { return p === null || p === ''; })) return { view: 'notfound' };
    return { view: 'entity', id: parts.join('/'), open: params.get('open') || '' };
  }
  return { view: 'notfound' };
}

function days(v) {
  var n = /^\d+$/.test(v || '') ? parseInt(v, 10) : DAYS;
  return n <= MAX_DAYS ? n : DAYS;
}

function overviewRoute(p) {
  return { view: 'overview', sel: overviewSel(p.get('sel')), focus: p.get('focus') || '', map: p.get('map') === '1' };
}

function overviewSel(v) {
  if (!v) return '';
  if (/^type:.+/.test(v) || /^rel:[^.]+\..+/.test(v)) return v;
  return '';
}

function listParams(p) {
  var out = emptyList();
  var sort = p.get('sort');
  if (sort) out.sort = sort;
  var dir = p.get('dir');
  if (dir === 'asc' || dir === 'desc') out.dir = dir;
  var q = p.get('q');
  if (q) out.q = q;
  // rel and to only as a pair: half a pair filters nothing.
  var rel = p.get('rel'), to = p.get('to');
  if (rel && to && /^(out|in):.+/.test(rel)) { out.rel = rel; out.to = to; }
  MANY.forEach(function (key) {
    p.getAll(key).forEach(function (v) {
      if (valid(key, v) && out[key].indexOf(v) === -1) out[key].push(v);
    });
  });
  if (p.get('base') === '1') out.base = true;
  out.open = p.get('open') || '';
  return out;
}

function encodeId(id) {
  return id.split('/').map(encodeURIComponent).join('/');
}

function withParams(path, q) {
  var s = q.toString();
  return path + (s ? '?' + s : '');
}

// format writes a route back as a fragment. Defaults are omitted, so the
// default view of a type is just #/type/<name>.
export function format(route) {
  var q = new URLSearchParams();
  switch (route.view) {
    case 'home':
      if (route.days !== undefined && route.days !== DAYS) q.set('days', route.days);
      return withParams('#/', q);
    case 'types': return '#/types';
    case 'overview':
      if (route.sel) q.set('sel', route.sel);
      if (route.focus) q.set('focus', route.focus);
      if (route.map) q.set('map', '1');
      return withParams('#/ontology', q);
    case 'entity':
      if (route.open) q.set('open', route.open);
      return withParams('#/entity/' + encodeId(route.id), q);
    case 'query':
      if (route.open) q.set('open', route.open);
      return withParams('#/query/' + encodeURIComponent(route.name), q);
    case 'list': {
      var l = Object.assign(emptyList(), route.list || {});
      LIST_KEYS.forEach(function (k) {
        var v = l[k];
        if (k === 'sort' && v === DEFAULT_SORT) return;
        if (k === 'dir' && v === DEFAULT_DIR) return;
        if ((k === 'rel' || k === 'to') && !(l.rel && l.to)) return;
        if (k === 'base') { if (v) q.set('base', '1'); return; }
        if (MANY.indexOf(k) !== -1) { v.forEach(function (x) { q.append(k, x); }); return; }
        if (v) q.set(k, v);
      });
      return withParams('#/type/' + encodeURIComponent(route.type), q);
    }
  }
  return '#/';
}

// listRoute is a list route for type with some parameters changed.
export function listRoute(type, list, changes) {
  return { view: 'list', type: type, list: Object.assign(emptyList(), list || {}, changes || {}) };
}

export function homeRoute() { return { view: 'home', days: DAYS }; }
export function overviewOf(sel, more) { return Object.assign({ view: 'overview', sel: sel || '', focus: '', map: false }, more || {}); }
export function entityRoute(id) { return { view: 'entity', id: id, open: '' }; }
export function typeRoute(type) { return listRoute(type, null, null); }

// opening is the same route with another entity open beside it, or none.
export function opening(route, id) {
  if (route.view === 'list') return listRoute(route.type, route.list, { open: id });
  return Object.assign({}, route, { open: id });
}

// openId is the entity a route holds open beside its view.
export function openId(route) {
  return (route.view === 'list' ? route.list.open : route.open) || '';
}
