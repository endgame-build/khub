// listmodel.js — pure derivations for one type's list: which columns show,
// which rows pass the filters, in what order, and what the facets offer. No
// document, no fetch, no imports, so node --test loads it directly.
//
// The rowset is /api/rows/<type> (internal/explore Rows). The server only
// derives; filtering and sorting happen here, over one response per list load.

var MARKERS = ['incomplete', 'superseded', 'expired', 'orphan', 'draft', 'stale'];
// Review tiers in rising order (internal/trust): unverified < machine < human.
var TRUST_RANK = { unverified: 0, machine: 1, human: 2 };
// FACET_VALUES is how many different values an attribute may hold and still be
// a facet; past it, its values are read in the column and found by text.
var FACET_VALUES = 12;

// What a rowset alone decides is worked out once for it: a filter or a sort on
// tens of thousands of rows asks again at every step.
var known = new WeakMap();
function once(rowset, key, work) {
  if (!known.has(rowset)) known.set(rowset, new Map());
  var mine = known.get(rowset);
  if (!mine.has(key)) mine.set(key, work());
  return mine.get(key);
}

// visibleColumns is the relation columns on screen. The base relations
// (related, references, depends_on) and the relations no row uses fold away
// until the reader asks for all of them; a column a sort or a filter is using
// (keep) always shows.
export function visibleColumns(rowset, all, keep) {
  return (rowset.relations || []).filter(function (c) {
    if (all || (keep && keep.indexOf(c.key) !== -1)) return true;
    return !c.base && once(rowset, 'used:' + c.key, function () {
      return (rowset.rows || []).some(function (row) { return row.counts && row.counts[c.key]; });
    });
  });
}

// visibleAttributes leaves out the attributes no row has a value for, and one
// that only repeats the title or the date every list shows.
export function visibleAttributes(rowset, all) {
  if (all) return rowset.attributes || [];
  return once(rowset, 'attributes', function () {
    return (rowset.attributes || []).filter(function (a) {
      var said = (rowset.rows || []).filter(function (row) { return valuesOf(row, a.name).length; });
      return ['title', 'updated'].every(function (shown) {
        return said.some(function (row) { return attrText(row, a.name) !== row[shown]; });
      });
    });
  });
}

// countTarget is where a count leads: one linked entity's own page; several
// of one type, that type's list filtered to this row from the relation's
// other end; several types, the row's own page, which groups them.
export function countTarget(type, col, row) {
  var linked = (row.links && row.links[col.key]) || [];
  if (!linked.length) return null;
  if (linked.length === 1) return { entity: linked[0] };
  var peer = linked[0].slice(0, linked[0].indexOf('/'));
  if (!linked.every(function (id) { return id.indexOf(peer + '/') === 0; })) return { entity: row.id };
  var rel = col.direction === 'out' ? 'in:' + (col.base ? '*' : type) + '.' + col.predicate : 'out:' + col.predicate;
  return { type: peer, rel: rel, to: row.id };
}

function attrPair(attr) {
  var i = attr.indexOf('=');
  return i > 0 ? { name: attr.slice(0, i), value: attr.slice(i + 1) } : null;
}

// valuesOf is an attribute's values in a row: none, one, or a list's. An
// attribute may be named like a property every object has.
export function valuesOf(row, name) {
  if (!row.attrs || !Object.prototype.hasOwnProperty.call(row.attrs, name)) return [];
  var v = row.attrs[name];
  if (v === undefined || v === null) return [];
  return Array.isArray(v) ? v.map(String) : [String(v)];
}

// attrText is those values as a cell shows them.
export function attrText(row, name) { return valuesOf(row, name).join(', '); }

// textMatches is the free-text filter: title, id and plain attribute values,
// case-insensitively — never the body (the list does not carry one).
function textMatches(row, q) {
  var needle = q.toLowerCase();
  if (String(row.title || '').toLowerCase().indexOf(needle) !== -1) return true;
  if (String(row.id || '').toLowerCase().indexOf(needle) !== -1) return true;
  var attrs = row.attrs || {};
  return Object.keys(attrs).some(function (k) {
    return attrText(row, k).toLowerCase().indexOf(needle) !== -1;
  });
}

// parseCount reads a count bound (`in:requirement.realized_in>=1`), or null.
export function parseCount(spec) {
  var m = /^((?:out|in):.+?)(>=|<=|!=|=|>|<)(\d+)$/.exec(spec || '');
  return m ? { key: m[1], op: m[2], n: parseInt(m[3], 10) } : null;
}

function holds(v, op, n) {
  switch (op) {
    case '>=': return v >= n;
    case '<=': return v <= n;
    case '>': return v > n;
    case '<': return v < n;
    case '!=': return v !== n;
    default: return v === n;
  }
}

// test says whether a row passes one filter value of one kind.
function test(row, kind, value) {
  switch (kind) {
    case 'status': return !!row[value];
    case 'trust': return row.trust === value;
    case 'by': return (row.by || '') === value;
    case 'attr': {
      var pair = attrPair(value);
      return !!pair && valuesOf(row, pair.name).indexOf(pair.value) !== -1;
    }
    case 'count': {
      var b = parseCount(value);
      return !!b && holds((row.counts && row.counts[b.key]) || 0, b.op, b.n);
    }
  }
  return true;
}

// dimension is the facet a filter value belongs to: values of one facet add
// up, and facets narrow each other.
function dimension(kind, value) {
  if (kind === 'attr') return 'attr:' + (attrPair(value) || { name: '' }).name;
  if (kind === 'count') return 'count:' + (parseCount(value) || { key: '' }).key;
  return kind;
}

// wanted groups the applied filter values by facet.
function wanted(list) {
  var dims = new Map();
  ['status', 'trust', 'by', 'attr', 'count'].forEach(function (kind) {
    (list[kind] || []).forEach(function (value) {
      var d = dimension(kind, value);
      if (!dims.has(d)) dims.set(d, []);
      dims.get(d).push({ kind: kind, value: value });
    });
  });
  return dims;
}

function splitNot(v) {
  var i = v.indexOf(':');
  return { kind: v.slice(0, i), value: v.slice(i + 1) };
}

// passing sorts the rows into those passing every filter and those one facet
// alone turns away: the two together are what that facet counts its choices
// over.
function passing(rowset, list) {
  var dims = wanted(list);
  var nots = (list.not || []).map(splitNot).map(function (n) { return { dim: dimension(n.kind, n.value), kind: n.kind, value: n.value }; });
  var all = [], alone = new Map();
  (rowset.rows || []).forEach(function (row) {
    if (list.q && !textMatches(row, list.q)) return;
    if (list.rel && list.to) {
      var linked = (row.links && row.links[list.rel]) || [];
      if (linked.indexOf(list.to) === -1) return;
    }
    var failed = null;
    function fail(dim) {
      if (failed !== null && failed !== dim) return true;
      failed = dim;
      return false;
    }
    for (var entry of dims) {
      if (!entry[1].some(function (f) { return test(row, f.kind, f.value); }) && fail(entry[0])) return;
    }
    for (var n of nots) {
      if (test(row, n.kind, n.value) && fail(n.dim)) return;
    }
    if (failed === null) { all.push(row); return; }
    if (!alone.has(failed)) alone.set(failed, []);
    alone.get(failed).push(row);
  });
  return { all: all, alone: alone };
}

// filterRows keeps the rows passing every set filter.
export function filterRows(rowset, list) { return passing(rowset, list).all; }

// facets lists what the rows can be narrowed by, each value with how many of
// the rows the other filters leave it would keep. A facet every row answers
// the same way says nothing and is left out, unless a filter is using it.
export function facets(rowset, list, all) {
  var rows = rowset.rows || [];
  var left = passing(rowset, list);
  var out = [];
  // said is the filter values of this facet one row answers to, so each facet
  // costs one pass over the rows whatever it offers.
  function add(key, label, group, kind, choices, said) {
    var here = tally(left.all, said);
    tally(left.alone.get(key) || [], said, here);
    var everywhere = once(rowset, 'tally:' + key, function () { return tally(rows, said); });
    var values = choices.map(function (c) {
      return {
        kind: kind, value: c.value, label: c.label, n: here.get(c.value) || 0,
        on: (list[kind] || []).indexOf(c.value) !== -1,
        not: (list.not || []).indexOf(kind + ':' + c.value) !== -1
      };
    });
    var used = values.some(function (v) { return v.on || v.not; });
    var telling = values.some(function (v) {
      var n = everywhere.get(v.value) || 0;
      return n > 0 && n < rows.length;
    });
    if (!used && !telling) return;
    out.push({ key: key, label: label, group: group, values: values.filter(function (v) { return v.n || v.on || v.not; }) });
  }
  function plain(list) { return list.map(function (v) { return { value: v, label: v }; }); }

  add('trust', 'Trust', '', 'trust', plain(Object.keys(TRUST_RANK)), function (r) { return [r.trust]; });
  add('status', 'Markers', '', 'status', plain(MARKERS), function (r) {
    return MARKERS.filter(function (m) { return r[m]; });
  });
  var by = function (r) { return r.by ? [r.by] : []; };
  add('by', 'Written by', '', 'by', plain(once(rowset, 'writers', function () { return distinct(rows.map(function (r) { return r.by || ''; }).filter(Boolean)); })), by);
  visibleAttributes(rowset, all).forEach(function (a) {
    var values = once(rowset, 'values:' + a.name, function () { return attrValues(rowset, a.name); });
    if (!(a.enum && a.enum.length) && (values.length > FACET_VALUES || values.length < 2)) return;
    add('attr:' + a.name, a.name, '', 'attr', values.map(function (v) { return { value: a.name + '=' + v, label: v }; }),
      function (r) { return valuesOf(r, a.name).map(function (v) { return a.name + '=' + v; }); });
  });
  visibleColumns(rowset, all, columnsInUse(list)).forEach(function (c) {
    add('count:' + c.key, columnTitle(c), 'Links', 'count', [
      { value: c.key + '=0', label: 'none' }, { value: c.key + '>=1', label: 'one or more' }
    ], function (r) { return [c.key + (r.counts && r.counts[c.key] ? '>=1' : '=0')]; });
  });
  return out;
}

// tally counts the rows answering to each value, on top of what into holds.
function tally(rows, said, into) {
  var out = into || new Map();
  rows.forEach(function (row) {
    var values = said(row);
    (values.length > 1 ? Array.from(new Set(values)) : values).forEach(function (v) { out.set(v, (out.get(v) || 0) + 1); });
  });
  return out;
}

function distinct(list) {
  return Array.from(new Set(list)).sort();
}

// columnsInUse names the relation columns a sort or a filter is using.
export function columnsInUse(list) {
  var keys = [list.sort, list.rel];
  (list.count || []).forEach(function (c) { var b = parseCount(c); if (b) keys.push(b.key); });
  (list.not || []).forEach(function (n) {
    var f = splitNot(n), b = f.kind === 'count' ? parseCount(f.value) : null;
    if (b) keys.push(b.key);
  });
  return keys.filter(Boolean);
}

// toggle adds or removes one filter value, and one of the two ways it can be
// set: kept or excluded.
export function toggle(list, kind, value, exclude) {
  var key = exclude ? 'not' : kind;
  var mine = exclude ? kind + ':' + value : value;
  var other = exclude ? kind : 'not';
  var others = exclude ? value : kind + ':' + value;
  var changes = {};
  var had = (list[key] || []).indexOf(mine) !== -1;
  changes[key] = had ? list[key].filter(function (v) { return v !== mine; }) : (list[key] || []).concat([mine]);
  changes[other] = (list[other] || []).filter(function (v) { return v !== others; });
  return changes;
}

// applied lists the filters in force as the reader would say them, each with
// the change that takes it away.
export function applied(rowset, list) {
  var out = [];
  function say(kind, value) {
    if (kind === 'attr') { var p = attrPair(value); return { name: p.name, value: p.value }; }
    if (kind === 'trust') return { name: 'trust', value: value };
    if (kind === 'status') return { name: 'marker', value: value };
    if (kind === 'by') return { name: 'written by', value: value };
    var b = parseCount(value);
    var col = (rowset.relations || []).filter(function (c) { return c.key === b.key; })[0];
    var name = col ? columnTitle(col) : b.key;
    if (b.op === '=' && b.n === 0) return { name: name, value: 'none' };
    if (b.op === '>=' && b.n === 1) return { name: name, value: 'one or more' };
    return { name: name, value: b.op + ' ' + b.n };
  }
  if (list.q) out.push({ name: 'text', value: list.q, off: { q: '' } });
  if (list.rel && list.to) {
    var col = (rowset.relations || []).filter(function (c) { return c.key === list.rel; })[0];
    out.push({ name: col ? columnTitle(col) : list.rel, value: list.to, off: { rel: '', to: '' } });
  }
  ['trust', 'status', 'by', 'attr', 'count'].forEach(function (kind) {
    (list[kind] || []).forEach(function (value) {
      out.push(Object.assign(say(kind, value), { off: toggle(list, kind, value, false) }));
    });
  });
  (list.not || []).forEach(function (n) {
    var f = splitNot(n), s = say(f.kind, f.value);
    out.push({ name: s.name, value: 'not ' + s.value, off: toggle(list, f.kind, f.value, true) });
  });
  return out;
}

// step is the row before or after the open one, the first when none is open;
// the ends stay where they are. An open entity that left the rows (it was just
// reviewed) is stepped from its former index, was, where the next row now is.
export function step(rows, open, by, was) {
  if (!rows.length) return '';
  var at = -1;
  for (var i = 0; i < rows.length; i++) if (rows[i].id === open) { at = i; break; }
  if (at === -1 && open && was >= 0) return rows[Math.max(0, Math.min(rows.length - 1, by > 0 ? was : was - 1))].id;
  if (at === -1) return rows[0].id;
  return rows[Math.max(0, Math.min(rows.length - 1, at + by))].id;
}

// sortValue reads the value a sort key orders by.
function sortValue(row, key) {
  if (key === 'title') return String(row.title || '').toLowerCase();
  if (key === 'id') return String(row.id || '').toLowerCase();
  if (key === 'trust') return TRUST_RANK[row.trust] || 0;
  if (key === 'updated' || key === 'by') return row[key] ? String(row[key]).toLowerCase() : null;
  if (key === 'status') {
    // One number per row, weighted so the marker a reviewer acts on first
    // dominates the order.
    return MARKERS.reduce(function (n, m, i) { return n + (row[m] ? 1 << (MARKERS.length - i) : 0); }, 0);
  }
  if (MARKERS.indexOf(key) !== -1) return row[key] ? 1 : 0;
  if (key.indexOf('attr:') === 0) {
    return valuesOf(row, key.slice(5)).length ? attrText(row, key.slice(5)).toLowerCase() : null;
  }
  if (key.indexOf('out:') === 0 || key.indexOf('in:') === 0) {
    return row.counts && typeof row.counts[key] === 'number' ? row.counts[key] : 0;
  }
  return String(row.title || '').toLowerCase();
}

// highFirst says which columns a reader wants from the top: counts, dates and
// the markers to act on.
export function highFirst(key) {
  return key === 'status' || key === 'updated' || key.indexOf('out:') === 0 || key.indexOf('in:') === 0;
}

function compare(a, b) {
  // Missing values sort last whatever the direction is asked for.
  if (a === b) return 0;
  if (a === null) return 1;
  if (b === null) return -1;
  return a < b ? -1 : 1;
}

// sortRows orders rows by one key. dir flips the key's order only; ties fall
// back to the id, ascending, so the order is stable and total.
export function sortRows(rows, key, dir) {
  var sign = dir === 'desc' ? -1 : 1;
  return rows.slice().sort(function (r1, r2) {
    var a = sortValue(r1, key), b = sortValue(r2, key);
    var c;
    if (a === null || b === null) c = compare(a, b);
    else c = sign * compare(a, b);
    if (c !== 0) return c;
    return compare(String(r1.id), String(r2.id));
  });
}

// countLine is the "N of M" header; with nothing filtered it is just M.
export function countLine(shown, total) { return shown === total ? String(total) : shown + ' of ' + total; }

// linkTargets lists the entities some row links to through one column — the
// choices the "linked to" filter offers — sorted, each once.
export function linkTargets(rowset, key) {
  return once(rowset, 'targets:' + key, function () {
    var all = [];
    (rowset.rows || []).forEach(function (row) {
      ((row.links && row.links[key]) || []).forEach(function (id) { all.push(id); });
    });
    return distinct(all);
  });
}

// attrValues lists the values an attribute filter offers: the declared enum,
// or else the values the rows hold.
export function attrValues(rowset, name) {
  var attrs = rowset.attributes || [];
  for (var i = 0; i < attrs.length; i++) {
    if (attrs[i].name === name && attrs[i].enum && attrs[i].enum.length) return attrs[i].enum.slice();
  }
  var all = [];
  (rowset.rows || []).forEach(function (row) {
    valuesOf(row, name).forEach(function (x) { all.push(x); });
  });
  return distinct(all);
}

// words is a predicate as a reader would say it.
export function words(predicate) { return String(predicate).replace(/_/g, ' '); }

// columnTitle is a relation column's header: its label with the direction.
export function columnTitle(col) {
  return (col.direction === 'in' ? '← ' : '→ ') + words(col.label).replace('.', ' ');
}
