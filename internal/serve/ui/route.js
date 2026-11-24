// route.js — the page's address: parse and format the URL fragment.
//
// Pure: no document, no fetch, no imports, so node --test loads it directly.
// Every view's whole state lives here, which is what
// makes a reload, back/forward or a pasted link restore the same view. The
// fragment never reaches the server, so filters never touch the request path
// the Host guard sees.
//
//   #/                         overview          ?sel=type:<name> | rel:<from>.<predicate>
//   #/type/<name>              one type's list   ?sort ?dir ?q ?rel+to ?count ?attr ?status ?trust ?base
//   #/entity/<type>/<slug>     one entity
//
// `?aspect=` from `khub serve --aspect` rides on location.search, not the
// fragment, so it never reaches this parser: every aspect lands on the
// overview.

export var STATUSES = ['draft', 'stale', 'orphan', 'incomplete'];
// Review tiers, lowest first (internal/trust Tiers).
export var TIERS = ['unverified', 'machine', 'human'];
var DEFAULT_SORT = 'title';
var DEFAULT_DIR = 'asc';
// Format order: fixed, so one view has one address.
var LIST_KEYS = ['sort', 'dir', 'q', 'rel', 'to', 'count', 'attr', 'status', 'trust', 'base'];
// A count bound, as `khub query --count` spells one: <column key><op><n>.
export var COUNT_RE = /^((?:out|in):.+?)(>=|<=|!=|=|>|<)(\d+)$/;

function decode(seg) {
  try { return decodeURIComponent(seg); } catch (e) { return null; }
}

function emptyList() {
  return { sort: DEFAULT_SORT, dir: DEFAULT_DIR, q: '', rel: '', to: '', count: '', attr: '', status: '', trust: '', base: false };
}

// parse reads a location.hash ('' or '#/...') into a route. A malformed
// address is the not-found view, never a throw: a typo in the address bar must
// not leave the page unmounted.
export function parse(hash) {
  var h = (hash || '').replace(/^#/, '');
  if (h === '' || h === '/') return { view: 'overview', sel: '' };
  var qi = h.indexOf('?');
  var pathPart = qi === -1 ? h : h.slice(0, qi);
  // URLSearchParams decodes a malformed escape to U+FFFD instead of throwing.
  var params = new URLSearchParams(qi === -1 ? '' : h.slice(qi + 1));
  var segs = pathPart.replace(/^\//, '').split('/');

  if (segs.length === 1 && segs[0] === '') {
    return { view: 'overview', sel: overviewSel(params.get('sel')) };
  }
  if (segs[0] === 'type' && segs.length === 2 && segs[1] !== '') {
    var name = decode(segs[1]);
    if (name === null) return { view: 'notfound' };
    return { view: 'list', type: name, list: listParams(params) };
  }
  if (segs[0] === 'entity' && segs.length >= 2) {
    var parts = segs.slice(1).map(decode);
    if (parts.some(function (p) { return p === null || p === ''; })) return { view: 'notfound' };
    return { view: 'entity', id: parts.join('/') };
  }
  return { view: 'notfound' };
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
  var count = p.get('count');
  if (count && COUNT_RE.test(count)) out.count = count;
  var attr = p.get('attr');
  if (attr && attr.indexOf('=') > 0) out.attr = attr;
  var status = p.get('status');
  if (STATUSES.indexOf(status) !== -1) out.status = status;
  var trust = p.get('trust');
  if (TIERS.indexOf(trust) !== -1) out.trust = trust;
  if (p.get('base') === '1') out.base = true;
  return out;
}

function encodeId(id) {
  return id.split('/').map(encodeURIComponent).join('/');
}

// format writes a route back as a fragment. Defaults are omitted, so the
// default view of a type is just #/type/<name>.
export function format(route) {
  if (route.view === 'overview') {
    return route.sel ? '#/?' + new URLSearchParams({ sel: route.sel }).toString() : '#/';
  }
  if (route.view === 'entity') return '#/entity/' + encodeId(route.id);
  if (route.view === 'list') {
    var l = Object.assign(emptyList(), route.list || {});
    var q = new URLSearchParams();
    LIST_KEYS.forEach(function (k) {
      var v = l[k];
      if (k === 'sort' && v === DEFAULT_SORT) return;
      if (k === 'dir' && v === DEFAULT_DIR) return;
      if ((k === 'rel' || k === 'to') && !(l.rel && l.to)) return;
      if (k === 'base') { if (v) q.set('base', '1'); return; }
      if (v) q.set(k, v);
    });
    var s = q.toString();
    return '#/type/' + encodeURIComponent(route.type) + (s ? '?' + s : '');
  }
  return '#/';
}

// listRoute is a list route for type with some parameters changed.
export function listRoute(type, list, changes) {
  return { view: 'list', type: type, list: Object.assign(emptyList(), list || {}, changes || {}) };
}

export function entityRoute(id) { return { view: 'entity', id: id }; }
export function typeRoute(type) { return listRoute(type, null, null); }
