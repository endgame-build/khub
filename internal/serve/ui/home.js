// home.js — the review home: what needs attention, what check fails on, what
// changed, and every type with how much of it is reviewed. Each block is its
// own request, so a slow one does not hold the rest.

import * as api from './api.js';
import { el, fill, text, tn } from './dom.js';
import { same, markerTags, trustTag, trustBar, trustKey, entityLink, markerName, MEANING, MARKERS, plural, num, notice, skeleton, problem, copyButton } from './widgets.js';
import { format, listRoute, typeRoute, homeRoute, markedRoute, DAYS } from './route.js';
import { words } from './listmodel.js';

var SHOWN = 3; // how many types a tile names before the reader asks for the rest
var MOST = 1000; // how many changes are listed on request
var WINDOWS = [0, DAYS, 30, 90];

// What each check bucket means to a reader.
var BUCKETS = {
  malformed: 'Files that do not parse', incomplete: 'Incomplete', dangling: 'Links to nothing', strays: 'Stored as the wrong type',
  misplaced: 'Stored outside every type', body_shape: 'Body sections missing', cycles: 'Relations that loop',
  alias_conflicts: 'Aliases two entities share', missing_singletons: 'Required and missing',
  draft_required_singletons: 'Required and still a draft', stray_templates: 'Templates no type uses',
  missing_templates: 'Templates with no file', template_invalid: 'Templates that do not parse',
  flag_shadowed: 'Fields a flag hides', stale_waivers: 'Waivers that match nothing'
};

var view = null;

export function unmount() { view = null; }

export function show(main, route, ctx, force) {
  ctx.crumbs([{ label: 'Review' }]);
  if (!force && view && view.root.isConnected) {
    if (view.days !== route.days) { view.days = route.days; view.most = ctx.entry().most || 0; recent(view, ctx); }
    ctx.ready();
    return;
  }
  var kept = force && view && view.root.isConnected ? view : null;
  var mine = { days: route.days, most: ctx.entry().most || 0, from: 0, drawn: kept ? kept.drawn : false, root: kept ? kept.root : null, blocks: kept ? kept.blocks : null, seen: kept ? kept.seen : { overview: {}, findings: {}, recent: {}, queries: {} } };
  view = mine;
  if (!mine.root) {
    mine.blocks = {
      attention: el('div', { class: 'attention' }), findings: el('section', { class: 'card findings', hidden: true }),
      recent: el('section', { class: 'card' }), types: el('section', { class: 'card' }), queries: el('section', { class: 'card', hidden: true })
    };
    var head = el('header', { class: 'viewhead' });
    head.appendChild(el('h1', null, 'Review'));
    mine.sub = el('p', { class: 'sub' });
    head.appendChild(mine.sub);
    var left = el('div', { class: 'col' }), right = el('div', { class: 'col' });
    left.appendChild(mine.blocks.findings);
    left.appendChild(mine.blocks.recent);
    right.appendChild(mine.blocks.types);
    right.appendChild(mine.blocks.queries);
    var cols = el('div', { class: 'homecols' });
    cols.appendChild(left);
    cols.appendChild(right);
    mine.root = el('section', { class: 'home' });
    mine.root.appendChild(head);
    mine.root.appendChild(mine.blocks.attention);
    mine.root.appendChild(cols);
    fill(main, [mine.root]);
    fill(mine.blocks.attention, [skeleton('Loading the workspace', 2)]);
    fill(mine.blocks.types, [el('h2', null, 'Types'), skeleton('Loading types', 6)]);
  } else mine.sub = mine.root.querySelector('.sub');

  api.getOverview().then(function (o) {
    if (view !== mine) return;
    ctx.loaded(o.malformed);
    if (same(mine.seen.overview, o)) { ctx.ready(); return; }
    attention(mine, o);
    typesBlock(mine, o);
    ctx.ready();
  }, function (err) {
    if (view !== mine) return;
    if (kept) { ctx.failed(err); return; }
    fill(main, [notice('Could not read the workspace', err.message, [], ctx.refresh)]);
  });
  api.getFindings().then(function (f) { if (view === mine && !same(mine.seen.findings, f)) findings(mine, f, ctx); },
    function (err) { if (view === mine && !kept) { mine.blocks.findings.hidden = false; fill(mine.blocks.findings, [el('h2', null, 'Check'), problem(err)]); } });
  api.getQueries().then(function (q) { if (view === mine && !same(mine.seen.queries, q)) queries(mine, q); },
    function (err) { if (view === mine && !kept) { mine.blocks.queries.hidden = false; fill(mine.blocks.queries, [el('h2', null, 'Saved queries'), problem(err)]); } });
  recent(mine, ctx);
}

function attention(mine, o) {
  var total = 0, reviewed = 0;
  o.types.forEach(function (t) { total += t.count; reviewed += t.trust.human + t.trust.machine; });
  text(mine.sub, total
    ? plural(total, 'entity', 'entities') + ' in ' + plural(o.types.filter(function (t) { return t.count; }).length, 'type') + ', ' + num(reviewed) + ' with a review that holds'
    : 'Nothing has been captured yet.');
  var cards = MARKERS.map(function (key) {
    var by = o.types.filter(function (t) { return t.markers[key]; });
    var n = by.reduce(function (s, t) { return s + t.markers[key]; }, 0);
    by.sort(function (a, b) { return b.markers[key] - a.markers[key]; });
    return { key: key, n: n, by: by };
  }).filter(function (c) { return c.n; });
  var unread = o.types.filter(function (t) { return t.trust.unverified; });
  if (unread.length) {
    unread.sort(function (a, b) { return b.trust.unverified - a.trust.unverified; });
    cards.unshift({ key: 'unverified', n: total - reviewed, by: unread, trust: true });
  }
  if (!cards.length) { fill(mine.blocks.attention, [el('p', { class: 'empty' }, total ? 'Every entity is reviewed and nothing is marked.' : 'Agents write entities with khub add; they appear here.')]); return; }
  fill(mine.blocks.attention, cards.map(function (c) {
    var box = el('div', { class: 'tile tile-' + c.key, title: MEANING[c.key] });
    var head = el('a', { class: 'tilehead', href: format(markedRoute(c.key)) });
    head.appendChild(el('b', null, num(c.n)));
    head.appendChild(el('span', { class: 'what' }, c.key === 'unverified' ? 'no review holds' : markerName(c.key)));
    box.appendChild(head);
    var ul = el('ul');
    var line = function (t) {
      var li = el('li');
      var n = c.trust ? t.trust.unverified : t.markers[c.key];
      li.appendChild(el('a', { href: format(listRoute(t.name, null, c.trust ? { trust: ['unverified'] } : { status: [c.key] })) }, t.name));
      li.appendChild(el('span', { class: 'n' }, num(n)));
      ul.appendChild(li);
      return li;
    };
    c.by.slice(0, SHOWN).forEach(line);
    if (c.by.length > SHOWN) {
      var more = el('li', { class: 'more' });
      more.appendChild(el('button', { type: 'button', on: { click: function () {
        more.remove();
        var first = c.by.slice(SHOWN).map(line)[0];
        first.querySelector('a').focus();
      } } }, plural(c.by.length - SHOWN, 'more type')));
      ul.appendChild(more);
    }
    box.appendChild(ul);
    return box;
  }));
}

// gap is the relation fewest entities of a type have, of those that some do:
// one nothing has is not in use, and one all have is no gap. A relation that
// only joins a type to itself is a chain, which most entities are not part of.
function gap(o, type) {
  var worst = null;
  o.relations.forEach(function (r) {
    if (r.from !== type || r.base || !r.have || r.have === r.total) return;
    if (r.to.every(function (t) { return t.type === type; })) return;
    if (!worst || r.have / r.total > worst.have / worst.total) worst = r;
  });
  return worst;
}

function typesBlock(mine, o) {
  var used = o.types.filter(function (t) { return t.count; });
  var t = el('table', { class: 'plain' });
  var thead = el('thead'), tr = el('tr');
  ['Type', 'Entities', 'Reviewed', 'Largest gap'].forEach(function (h, i) { tr.appendChild(el('th', { scope: 'col', class: i === 1 ? 'num' : null }, h)); });
  thead.appendChild(tr);
  t.appendChild(thead);
  var body = el('tbody');
  used.forEach(function (ty) {
    var r = el('tr');
    var name = el('th', { scope: 'row' });
    name.appendChild(el('a', { href: format(typeRoute(ty.name)) }, ty.name));
    r.appendChild(name);
    r.appendChild(el('td', { class: 'num' }, num(ty.count)));
    var bar = el('td');
    bar.appendChild(trustBar(ty.trust, ty.count));
    r.appendChild(bar);
    var g = gap(o, ty.name), cell = el('td', { class: 'gapcell' });
    if (g) cell.appendChild(el('a', {
      href: format(listRoute(ty.name, null, { count: ['out:' + g.predicate + '=0'] })),
      title: g.have + ' of ' + g.total + ' have ' + words(g.predicate)
    }, num(g.total - g.have) + ' without ' + words(g.predicate)));
    r.appendChild(cell);
    body.appendChild(r);
  });
  t.appendChild(body);
  var empty = o.types.length - used.length;
  fill(mine.blocks.types, [el('h2', null, 'Types'), used.length ? t : el('p', { class: 'empty' }, 'No type has entities yet.'), used.length ? trustKey() : null,
    el('p', { class: 'foot' }, empty ? plural(empty, 'type') + ' with no entities. ' : '')]);
  mine.blocks.types.lastChild.appendChild(el('a', { href: '#/types' }, 'All types'));
}

function findings(mine, f, ctx) {
  mine.blocks.findings.hidden = false;
  if (f.passed) {
    fill(mine.blocks.findings, [el('p', { class: 'pass' }, 'khub check passes.')]);
    return;
  }
  var total = f.buckets.reduce(function (n, b) { return n + b.total; }, 0);
  var nodes = [el('h2', null, 'khub check fails on ' + plural(total, 'finding'))];
  f.buckets.forEach(function (b) {
    var d = el('details', { class: 'bucket', open: f.buckets.length < 3 });
    d.appendChild(el('summary', null, (BUCKETS[b.bucket] || words(b.bucket)) + ' (' + num(b.total) + ')'));
    var ul = el('ul');
    b.items.forEach(function (i) {
      var li = el('li');
      if (i.id) li.appendChild(entityLink(i.id, i.id, ctx, 'mono'));
      else if (i.about) li.appendChild(el('span', { class: 'mono' }, i.about));
      (i.ids || []).forEach(function (id, n) {
        if (n || i.about) li.appendChild(tn(' · '));
        li.appendChild(entityLink(id, id, ctx, 'mono'));
      });
      li.appendChild(el('span', { class: 'why' }, i.reason));
      ul.appendChild(li);
    });
    if (b.total > b.items.length) ul.appendChild(el('li', { class: 'more' }, num(b.total - b.items.length) + ' more'));
    d.appendChild(ul);
    nodes.push(d);
  });
  nodes.push(copyButton('Copy khub check', 'khub check'));
  fill(mine.blocks.findings, nodes);
}

function recent(mine, ctx) {
  var head = function () {
    var h = el('div', { class: 'cardhead' });
    h.appendChild(el('h2', null, 'Changed'));
    var pick = el('span', { class: 'seg', role: 'group', 'aria-label': 'Window' });
    WINDOWS.forEach(function (d) {
      pick.appendChild(el('a', {
        href: format(Object.assign(homeRoute(), { days: d })), 'aria-current': d === mine.days ? 'true' : null
      }, d === 0 ? 'today' : d + ' days'));
    });
    h.appendChild(pick);
    return h;
  };
  var days = mine.days;
  if (!mine.drawn) fill(mine.blocks.recent, [head(), skeleton('Loading changes', 5)]);
  api.getRecent(days, mine.most).then(function (found) {
    if (view !== mine || mine.days !== days) return;
    if (same(mine.seen.recent, found)) return;
    mine.drawn = true;
    var nodes = [head()];
    if (!found.rows.length) nodes.push(el('p', { class: 'empty' }, 'Nothing was written in this window.'));
    else {
      var ul = el('ul', { class: 'feed' });
      var last = '';
      found.rows.forEach(function (r) {
        if (r.date !== last) { last = r.date; ul.appendChild(el('li', { class: 'day' }, dayName(r))); }
        var li = el('li');
        li.appendChild(el('span', { class: 'ty' }, r.type));
        var mid = el('span', { class: 'mid' });
        mid.appendChild(entityLink(r.id, r.title, ctx));
        mid.appendChild(el('span', { class: 'tail' }, [r.new ? 'new' : 'edited', r.by ? 'by ' + r.by : ''].filter(Boolean).join(' ')));
        li.appendChild(mid);
        var marks = markerTags(r.markers);
        marks.appendChild(trustTag(r.markers.trust));
        li.appendChild(marks);
        ul.appendChild(li);
      });
      nodes.push(ul);
      if (found.total > found.rows.length) {
        var foot = el('p', { class: 'foot' }, 'The newest ' + num(found.rows.length) + ' of ' + plural(found.total, 'change') + '. ');
        if (found.rows.length < MOST) {
          foot.appendChild(el('button', { type: 'button', class: 'copy', on: { click: function () {
            view.most = ctx.entry().most = MOST;
            view.from = found.rows.length;
            recent(view, ctx);
          } } },
            'Show ' + (found.total > MOST ? num(MOST) : 'all')));
        }
        nodes.push(foot);
      }
    }
    fill(mine.blocks.recent, nodes);
    if (mine.from) {
      // The button is gone: the keyboard goes on from the first new change.
      var next = mine.blocks.recent.querySelectorAll('.feed .mid a')[mine.from];
      mine.from = 0;
      if (next) next.focus({ preventScroll: true });
    }
    ctx.ready();
  }, function (err) {
    if (view !== mine) return;
    if (!mine.drawn) fill(mine.blocks.recent, [head(), problem(err)]);
  });
}

function dayName(r) {
  if (r.age === 0) return 'Today';
  if (r.age === 1) return 'Yesterday';
  return r.date;
}

function queries(mine, q) {
  mine.blocks.queries.hidden = !q.queries.length;
  if (!q.queries.length) return;
  var ul = el('ul', { class: 'qlist' });
  q.queries.forEach(function (x) {
    var li = el('li');
    li.appendChild(el('a', { href: format({ view: 'query', name: x.name }) }, x.name));
    li.appendChild(x.error ? el('span', { class: 'err', title: x.error }, 'no longer runs') : el('span', { class: 'n' }, num(x.count)));
    ul.appendChild(li);
  });
  fill(mine.blocks.queries, [el('h2', null, 'Saved queries'), ul]);
}
