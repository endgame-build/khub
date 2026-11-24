// shell.js — text the reader may paste into a terminal. Pure, no imports, so
// node --test loads it directly.

// PLAIN is a word every shell reads as itself (internal/cli shellPlain).
var PLAIN = /^[A-Za-z0-9_./-]+$/;

// CONTROL is what a terminal acts on before a shell reads it. Quoting does not
// help: an escape sequence can end the paste and the line it is quoted in.
var CONTROL = /[\u0000-\u001f\u007f-\u009f]/;

// word renders a value as one shell word. An id is a file's name, and a file
// can be named anything: whatever it holds reaches the shell as text. A value
// holding a control character has no word; it answers ''.
export function word(value) {
  var s = String(value);
  if (CONTROL.test(s)) return '';
  return PLAIN.test(s) ? s : "'" + s.replace(/'/g, "'\\''") + "'";
}

// verifyCommand records a review of id. The reviewer puts a name in place of
// <name>: left as it is, the command is a syntax error to every shell, so
// pasting it alone records nothing. '' for an id that cannot be pasted.
export function verifyCommand(id) {
  return command(['khub verify', word(id), '--by human:<name>']);
}

// runCommand replays a saved query.
export function runCommand(name) { return command(['khub run', word(name)]); }

// command joins words, or is '' when one of them cannot be pasted.
function command(words) { return words.every(Boolean) ? words.join(' ') : ''; }

// queryCommand is the `khub query` that lists what a list shows, or, as why,
// what the command cannot say. rowset is /api/rows/<type>.
export function queryCommand(rowset, list) {
  var out = ['khub query --type ' + word(rowset.type)];
  var why = '';
  function only(name, values, say) {
    if (values.length > 1) why = why || 'more than one value of ' + name;
    else if (values.length) out.push(say(values[0]));
  }
  if (list.q) why = 'text over the columns';
  if (list.rel) why = why || 'a link to one entity';
  if ((list.not || []).length) why = why || 'a value left out';
  if ((list.by || []).length) why = why || 'who wrote it';
  var fields = new Map();
  (list.attr || []).forEach(function (a) {
    var i = a.indexOf('='), name = a.slice(0, i);
    fields.set(name, (fields.get(name) || []).concat([a.slice(i + 1)]));
  });
  fields.forEach(function (values, name) { only(name, values, function (v) {
    if (!word(name) || !word(v)) why = why || 'a value holding control characters';
    return '--' + word(name) + ' ' + word(v);
  }); });
  only('trust', list.trust || [], function (v) { return '--trust ' + v; });
  only('markers', list.status || [], function (v) {
    if (v === 'draft' || v === 'orphan' || v === 'stale') return '--' + v;
    why = why || 'the ' + v + ' marker';
    return '';
  });
  var counts = new Map();
  (list.count || []).forEach(function (c) {
    var m = /^((?:out|in):.+?)(>=|<=|!=|=|>|<)(\d+)$/.exec(c);
    counts.set(m[1], (counts.get(m[1]) || []).concat([m[2] + m[3]]));
  });
  counts.forEach(function (bounds, key) {
    var col = (rowset.relations || []).filter(function (c) { return c.key === key; })[0];
    only(key, bounds, function (b) {
      // An incoming relation has a name query knows only as a declared inverse.
      if (!col || (col.direction === 'in' && col.label.indexOf('.') !== -1)) { why = why || 'an incoming relation with no inverse'; return ''; }
      return '--count ' + word(col.label + b);
    });
  });
  return why ? { command: '', why: why } : { command: out.filter(Boolean).join(' '), why: '' };
}
