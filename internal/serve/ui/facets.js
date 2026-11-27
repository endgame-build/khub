// facets.js — the rail beside a list: each facet's values with how many rows
// each would keep. A click keeps a value, Shift-click leaves it out.

import { el, fill } from './dom.js';
import { num, markerName } from './widgets.js';
import { facets, toggle } from './listmodel.js';

// SHOWN is how many values of a facet show before the reader asks for the rest.
var SHOWN = 8;

// draw fills host with the facets of rowset under list. go takes a change to
// the list's filters; open remembers which facets the reader unfolded.
export function draw(host, rowset, list, go, open) {
  var all = facets(rowset, list, list.base);
  var nodes = [], group = '';
  all.forEach(function (f) {
    if (f.group && f.group !== group) { group = f.group; nodes.push(el('h2', null, group)); }
    var box = el('fieldset', { class: 'facet' });
    box.appendChild(el('legend', null, f.label));
    var ul = el('ul');
    var values = open[f.key] ? f.values : f.values.filter(function (v, i) { return i < SHOWN || v.on || v.not; });
    var most = Math.max.apply(null, f.values.map(function (v) { return v.n; }).concat([1]));
    values.forEach(function (v) {
      var li = el('li');
      var b = el('button', {
        type: 'button', class: 'fv' + (v.on ? ' on' : '') + (v.not ? ' not' : ''), 'data-fv': v.kind + ':' + v.value,
        'aria-pressed': v.not ? 'mixed' : String(v.on), title: v.not ? 'Left out. Click to keep only it' : 'Shift-click to leave it out',
        on: { click: function (evt) { go(toggle(list, v.kind, v.value, evt.shiftKey)); } }
      });
      b.appendChild(el('span', { class: 'share', 'aria-hidden': 'true', style: { width: (100 * v.n / most) + '%' } }));
      b.appendChild(el('span', { class: 'fl' }, (v.not ? 'not ' : '') + (f.key === 'status' ? markerName(v.label) : v.label)));
      b.appendChild(el('span', { class: 'fn' }, num(v.n)));
      li.appendChild(b);
      ul.appendChild(li);
    });
    box.appendChild(ul);
    if (values.length < f.values.length) {
      box.appendChild(el('button', { type: 'button', class: 'copy', on: { click: function () {
        open[f.key] = true;
        draw(host, rowset, list, go, open);
      } } }, (f.values.length - values.length) + ' more'));
    }
    nodes.push(box);
  });
  if (!nodes.length) nodes.push(el('p', { class: 'hint' }, 'Every row answers alike, so there is nothing to narrow by.'));
  fill(host, nodes);
}
