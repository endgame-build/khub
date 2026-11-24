// reviewer.js — who signs off. The name goes into the command the page copies;
// the browser keeps it, and it is all the page keeps there.

import { el, fill, text } from './dom.js';
import { copy } from './widgets.js';
import { verifyCommand, reviewerFault, word } from './shell.js';

var KEY = 'khub.reviewer';
var name = read();
var drawn = []; // { box, draw } of every button on the page

function read() {
  var kept = '';
  try { kept = localStorage.getItem(KEY) || ''; } catch (e) { /* storage may be off */ }
  return reviewerFault(kept) ? '' : kept;
}

function keep(value) {
  name = value;
  try { localStorage.setItem(KEY, value); } catch (e) { /* kept for this page alone */ }
  drawn = drawn.filter(function (d) { return d.box.isConnected; });
  drawn.forEach(function (d) { d.draw(); });
}

// verifyButton copies the command that records a review of id, asking for the
// reviewer's name the first time.
export function verifyButton(id) {
  var box = el('span', { class: 'verify' });
  var btn = el('button', { type: 'button', class: 'btn', 'aria-live': 'polite' }, 'Copy verify command');
  var who = el('button', { type: 'button', class: 'copy', on: { click: function () { ask(function () { who.focus(); }); } } });
  btn.addEventListener('click', function () {
    if (name) { copy(btn, verifyCommand(id, name)); return; }
    ask(function () { btn.focus(); copy(btn, verifyCommand(id, name)); });
  });
  function draw() {
    var pasted = word(id);
    btn.disabled = !pasted;
    btn.title = !pasted ? 'Its name holds control characters a terminal would act on' : verifyCommand(id, name) || 'Asks for your name first';
    text(who, name ? 'as ' + name : 'Set name');
    who.title = name ? 'Change the reviewer\'s name' : 'Set the reviewer\'s name';
  }
  draw();
  drawn.push({ box: box, draw: draw });
  fill(box, [btn, who]);
  return box;
}

// ask takes the reviewer's name, and then calls back.
function ask(then) {
  var input = el('input', { type: 'text', id: 'reviewer', value: name, autocomplete: 'username', spellcheck: false, maxLength: 80 });
  var wrong = el('p', { class: 'err', role: 'alert' });
  var form = el('form', { on: { submit: function (evt) {
    evt.preventDefault();
    var value = input.value.trim(), fault = reviewerFault(value);
    if (fault) { text(wrong, fault); input.focus(); return; }
    keep(value);
    box.close();
    then();
  } } });
  form.appendChild(el('label', { htmlFor: 'reviewer' }, 'Your name, as reviews record it'));
  form.appendChild(input);
  form.appendChild(wrong);
  var row = el('div', { class: 'dialogrow' });
  row.appendChild(el('button', { type: 'submit', class: 'btn' }, 'Use this name'));
  row.appendChild(el('button', { type: 'button', class: 'btn-ghost', on: { click: function () { box.close(); } } }, 'Cancel'));
  form.appendChild(row);
  var box = el('dialog', { class: 'keybox', 'aria-label': 'Reviewer' });
  box.appendChild(form);
  box.addEventListener('close', function () { box.remove(); });
  document.body.appendChild(box);
  box.showModal();
  input.select();
}
