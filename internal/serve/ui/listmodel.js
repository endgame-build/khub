// listmodel.js — pure derivations for one type's list: which columns show,
// which rows pass the filters, in what order. No document, no fetch, no
// imports, so node --test loads it directly.
//
// The rowset is /api/rows/<type> (internal/explore Rows). The server only
// derives; filtering and sorting happen here, over one response per list load.

var MARKERS = ['draft', 'stale', 'orphan', 'incomplete'];
// Review tiers in rising order (internal/trust): unverified < machine < human.
var TRUST_RANK = { unverified: 0, machine: 1, human: 2 };

// visibleColumns is the relation columns on screen: the base relations
// (related, references, depends_on) only when the reader asked for them.
export function visibleColumns(rowset, base) {
  return (rowset.relations || []).filter(function (c) { return base || !c.base; });
}

// columnByKey finds a relation column by key, or null.
export function columnByKey(rowset, key) {
  var cols = rowset.relations || [];
  for (var i = 0; i < cols.length; i++) if (cols[i].key === key) return cols[i];
  return null;
}

function attrPair(attr) {
  var i = attr.indexOf('=');
  return i > 0 ? { name: attr.slice(0, i), value: attr.slice(i + 1) } : null;
}

// attrMatches holds a row's attribute against one value; a list value (joined
// with ", " by the server) matches when any element does.
function attrMatches(row, name, value) {
  var v = row.attrs ? row.attrs[name] : undefined;
  if (v === undefined || v === null) return false;
  if (v === value) return true;
  return String(v).split(', ').indexOf(value) !== -1;
}

// textMatches is the free-text filter: title, id and plain attribute values,
// case-insensitively — never the body (the list does not carry one).
function textMatches(row, q) {
  var needle = q.toLowerCase();
  if (String(row.title || '').toLowerCase().indexOf(needle) !== -1) return true;
  if (String(row.id || '').toLowerCase().indexOf(needle) !== -1) return true;
  var attrs = row.attrs || {};
  return Object.keys(attrs).some(function (k) {
    return String(attrs[k]).toLowerCase().indexOf(needle) !== -1;
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

// filterRows keeps the rows passing every set filter (AND).
export function filterRows(rowset, list) {
  var pair = list.attr ? attrPair(list.attr) : null;
  var bound = list.count ? parseCount(list.count) : null;
  return (rowset.rows || []).filter(function (row) {
    if (list.q && !textMatches(row, list.q)) return false;
    if (list.rel && list.to) {
      var linked = (row.links && row.links[list.rel]) || [];
      if (linked.indexOf(list.to) === -1) return false;
    }
    if (bound && !holds((row.counts && row.counts[bound.key]) || 0, bound.op, bound.n)) return false;
    if (pair && !attrMatches(row, pair.name, pair.value)) return false;
    if (list.status && !row[list.status]) return false;
    return true;
  });
}

// sortValue reads the value a sort key orders by.
function sortValue(row, key) {
  if (key === 'title') return String(row.title || '').toLowerCase();
  if (key === 'id') return String(row.id || '').toLowerCase();
  if (key === 'trust') return TRUST_RANK[row.trust] || 0;
  if (key === 'status') {
    // One number per row, weighted so the marker a reviewer acts on first
    // (draft, then incomplete) dominates the order.
    return (row.draft ? 8 : 0) + (row.incomplete ? 4 : 0) + (row.orphan ? 2 : 0) + (row.stale ? 1 : 0);
  }
  if (MARKERS.indexOf(key) !== -1) return row[key] ? 1 : 0;
  if (key.indexOf('attr:') === 0) {
    var v = row.attrs ? row.attrs[key.slice(5)] : undefined;
    return v === undefined || v === null ? null : String(v).toLowerCase();
  }
  if (key.indexOf('out:') === 0 || key.indexOf('in:') === 0) {
    return row.counts && typeof row.counts[key] === 'number' ? row.counts[key] : 0;
  }
  return String(row.title || '').toLowerCase();
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

// countLine is the "N of M" header.
export function countLine(shown, total) { return shown + ' of ' + total; }

// linkTargets lists the entities some row links to through one column — the
// choices the "linked to" filter offers — sorted, each once.
export function linkTargets(rowset, key) {
  var seen = {};
  var out = [];
  (rowset.rows || []).forEach(function (row) {
    ((row.links && row.links[key]) || []).forEach(function (id) {
      if (!seen[id]) { seen[id] = true; out.push(id); }
    });
  });
  return out.sort();
}

// attrValues lists the values an attribute filter offers: the declared enum,
// or else the values the rows hold.
export function attrValues(rowset, name) {
  var attrs = rowset.attributes || [];
  for (var i = 0; i < attrs.length; i++) {
    if (attrs[i].name === name && attrs[i].enum && attrs[i].enum.length) return attrs[i].enum.slice();
  }
  var seen = {};
  var out = [];
  (rowset.rows || []).forEach(function (row) {
    var v = row.attrs ? row.attrs[name] : undefined;
    if (v === undefined || v === null) return;
    String(v).split(', ').forEach(function (x) {
      if (!seen[x]) { seen[x] = true; out.push(x); }
    });
  });
  return out.sort();
}

// columnTitle is a relation column's header: its label with the direction.
export function columnTitle(col) {
  return (col.direction === 'in' ? '← ' : '→ ') + col.label;
}
