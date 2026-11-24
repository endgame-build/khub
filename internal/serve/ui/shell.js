// shell.js — text the reader may paste into a terminal. Pure, no imports, so
// node --test loads it directly.

// PLAIN is a word every shell reads as itself (internal/cli shellPlain).
var PLAIN = /^[A-Za-z0-9_./-]+$/;

// word renders a value as one shell word. An id is a file's name, and a file
// can be named anything: whatever it holds reaches the shell as text.
export function word(value) {
  var s = String(value);
  return PLAIN.test(s) ? s : "'" + s.replace(/'/g, "'\\''") + "'";
}

// verifyCommand records a review of id. The reviewer puts a name in place of
// <name>: left as it is, the command is a syntax error to every shell, so
// pasting it alone records nothing.
export function verifyCommand(id) {
  return 'khub verify ' + word(id) + ' --by human:<name>';
}
