// entity.js — one entity: what it says, whether it has been reviewed, what it
// links to and what links to it, and how it came to be. The page and the side
// panel render from the same parts.

import * as api from './api.js';
import * as panel from './panel.js';
import * as marks from './marks.js';
import { el, fill, tn } from './dom.js';
import { same, markerTags, trustTag, notice, copyButton, entityLink, opensBeside, skeleton, problem, when, plural, num, MARKERS, markerName } from './widgets.js';
import { renderBlocks, shallowest } from './blocks.js';
import { words, step } from './listmodel.js';
import { format, entityRoute } from './route.js';
import { word } from './shell.js';
import { verifyButton } from './reviewer.js';

// GROUP is how many entities of one relation are drawn before the reader asks
// for the rest; COMMITS, how many commits.
var GROUP = 24;
var COMMITS = 6;

// FOLD is how many changed lines of a body show without opening them.
var FOLD = 8;

// What a commit did, where it lists no fields.
var KINDS = {
  bookkeeping: 'dates, stamps or reviews', created: 'created', removed: 'removed',
  unparsable: 'left it unparsable', unknown: 'change not known here'
};

var view = null; // { id, root, aside, links, more, seen }

// order is the entities next and previous follow: the page's links, as listed.
export function order() { return view && view.links ? view.links : []; }

export function unmount() { view = null; panel.unmount(); }

export function show(main, route, ctx, force) {
  if (!force && view && view.id === route.id && view.root && view.root.isConnected) {
    panel.sync(view.aside, route, ctx, view.links, false);
    ctx.ready();
    return;
  }
  var kept = force && view && view.id === route.id && view.root && view.root.isConnected;
  var mine = kept ? Object.assign({}, view) : { id: route.id };
  view = mine;
  if (!kept) fill(main, [skeleton('Loading ' + route.id, 8)]);
  api.getEntity(route.id).then(function (e) {
    if (view !== mine) return;
    ctx.loaded(e.malformed);
    var unchanged = same(mine, e) && kept;
    try {
      if (unchanged) mine.more(mine); else draw(main, mine, e, ctx);
      panel.sync(mine.aside, ctx.route(), ctx, mine.links, force);
      ctx.ready();
    } catch (bug) {
      // A page bug, not a workspace problem: say so, and keep the stack.
      console.error(bug);
      fill(main, [notice('This page failed to render', String(bug))]);
    }
  }, function (err) {
    if (view !== mine) return;
    if (kept && err.status !== 404) { ctx.failed(err); return; }
    // An entity that is gone does not end the walk it was part of.
    var top = head(ctx, [{ label: 'Types', route: { view: 'types' } }], route.id, route.id);
    if (err.code === 'ambiguity_error') {
      fill(main, [top, notice('More than one entity answers to ' + route.id, 'Pick the one you meant.',
        (err.expected || []).map(function (id) { return { label: id, href: format(entityRoute(id)) }; }))]);
      return;
    }
    var gone = err.status === 404;
    var links = [{ label: 'All types', href: '#/types' }];
    if (err.suggestion) links.unshift({ label: 'Did you mean ' + err.suggestion + '?', href: format(entityRoute(err.suggestion)) });
    fill(main, [top, notice(gone ? 'No such entity' : 'Could not load ' + route.id, err.message || String(err), links, gone ? null : ctx.refresh)]);
    hold(main, ctx);
  });
}

function draw(main, mine, e, ctx) {
  var p = parts(e, ctx, false);
  var page = el('article', { class: 'entity pane' });
  page.appendChild(head(ctx, [
    { label: 'Types', route: { view: 'types' } },
    { label: e.type, route: ctx.listRoute(e.type) }
  ], title(e), e.id));
  page.appendChild(p.head);
  if (e.stray) {
    page.appendChild(el('p', { class: 'stray' },
      'This file is not an entity of the workspace graph. Its type does not match where it is stored, ' +
      'so khub check reports it as a stray and its links are not shown.'));
  }
  var cols = el('div', { class: 'entitycols' });
  var mainCol = el('div', { class: 'maincol' });
  mainCol.appendChild(p.review);
  mainCol.appendChild(p.since);
  if (p.body) mainCol.appendChild(p.body);
  mainCol.appendChild(p.links);
  var reach = el('section', { class: 'reach' });
  mainCol.appendChild(reach);
  var side = el('aside', { class: 'sidecol', 'aria-label': 'Attributes and history' });
  if (p.attributes) side.appendChild(p.attributes);
  var history = el('section', { class: 'history' });
  side.appendChild(history);
  cols.appendChild(mainCol);
  cols.appendChild(side);
  page.appendChild(cols);

  var root = el('section', { class: 'split' });
  root.appendChild(page);
  mine.aside = el('aside', { class: 'peek', 'aria-label': 'Entity', hidden: true });
  root.appendChild(mine.aside);
  mine.root = root;
  var seen = Object.create(null);
  mine.links = [].concat.apply([], (e.neighbours || []).map(function (g) { return g.items; })).filter(function (i) {
    if (!i.resolved || seen[i.id]) return false;
    return (seen[i.id] = true);
  });
  fill(main, [root]);
  hold(root, ctx);

  if (!e.stray) fill(history, [el('h2', null, 'History'), skeleton('Loading history', 3)]);
  var read = { history: {}, reach: {} };
  // What loads after the page, and again at each refresh: a commit changes the
  // history without changing the entity.
  mine.more = function (now) {
    if (e.stray) return;
    api.getHistory(e.id).then(function (h) {
      if (view !== now || same(read.history, h)) return;
      drawHistory(history, h);
      drawSince(p.since, h.since_review, h.commits.length, p.changed);
      ctx.ready();
    }, function (err) {
      if (view === now && !read.history.seen) fill(history, [el('h2', null, 'History'), problem(err)]);
    });
    api.getReach(e.id).then(function (r) {
      if (view !== now || same(read.reach, r)) return;
      drawReach(reach, r, ctx);
      ctx.ready();
    }, function (err) {
      if (view === now && !read.reach.seen) fill(reach, [el('h2', null, 'Further out'), problem(err)]);
    });
  };
  mine.more(mine);
}

// head is the top of a page: the trail to it, its own unless it was opened from
// a list, and the way through that list.
function head(ctx, trail, label, id) {
  var from = ctx.from();
  var top = el('div', { class: 'ptop' });
  top.appendChild(ctx.crumbs((from ? from.trail : trail).concat([{ label: label }])));
  var walk = from ? walkBar(from.rows, id, ctx) : null;
  if (walk) top.appendChild(walk);
  return top;
}

// hold leaves the keyboard on the walk's button it was on. At an end that button
// is off, and the other one is next.
function hold(root, ctx) {
  var held = ctx.held();
  var now = held && root.querySelector('.walk button[aria-label="' + held + '"]');
  if (now && now.disabled) now = root.querySelector('.walk button:not(:disabled)');
  if (now) now.focus({ preventScroll: true });
}

// walkBar goes through the list an entity was opened from, page by page.
function walkBar(rows, id, ctx) {
  var at = -1;
  rows.forEach(function (r, i) { if (r.id === id) at = i; });
  if (at === -1 || rows.length < 2) return null;
  var bar = el('div', { class: 'walk', role: 'group', 'aria-label': 'The list it was opened from' });
  [['Previous', -1, '[', '‹'], ['Next', 1, ']', '›']].forEach(function (b) {
    bar.appendChild(el('button', {
      type: 'button', class: 'sq', title: b[0] + ' (' + b[2] + ')', 'aria-label': b[0], disabled: step(rows, id, b[1]) === id,
      on: { click: function () { ctx.page(b[1], b[0]); } }
    }, b[3]));
  });
  bar.appendChild(el('span', { class: 'at' }, num(at + 1) + ' of ' + num(rows.length)));
  return bar;
}

// parts are the pieces the page and the panel lay out. compact is the panel:
// its headings sit one level down, and it asks for history only when there is
// a change since a review to show.
export function parts(e, ctx, compact) {
  var fm = e.frontmatter || {};
  var t = title(e);
  var lede = typeof fm.description === 'string' && fm.description.trim() !== '';
  var H = compact ? 'h3' : 'h2';

  var head = el('header', { class: 'ehead' });
  head.appendChild(el(compact ? 'h2' : 'h1', null, t));
  var id = el('p', { class: 'idline' });
  id.appendChild(el('span', { class: 'mono' }, e.id));
  id.appendChild(copyButton('Copy id', word(e.id)));
  if (e.path) id.appendChild(copyButton('Copy path', word(e.path)));
  head.appendChild(id);
  if (lede) head.appendChild(el('p', { class: 'lede' }, fm.description));
  var changed = el('span', { class: 'tag tag-superseded', hidden: true }, 'changed since review');
  if (lede) head.lastChild.appendChild(changed);

  var since = el('section', { class: 'since', hidden: true });
  if (compact && Array.isArray(fm.verified) && fm.verified.length) {
    api.getHistory(e.id).then(function (h) { drawSince(since, h.since_review, 0, changed); }, function () {});
  }

  return {
    head: head, review: review(e), since: since, changed: changed,
    body: body(e, ctx, compact, lede), links: links(e, ctx, H), attributes: attributes(e, t, lede, H)
  };
}

function title(e) {
  var fm = e.frontmatter || {};
  return String(fm.title || fm.name || e.slug);
}

// review says who wrote it, who reviewed it, what is wrong with it, and gives
// the command that records a review. The page never writes: khub validates it
// in a terminal.
function review(e) {
  var fm = e.frontmatter || {}, m = e.markers;
  var box = el('div', { class: 'review' });
  var line = el('div', { class: 'rline' });
  if (m) line.appendChild(trustTag(m.trust));
  // One review may be written as a mapping of its own.
  var events = (Array.isArray(fm.verified) ? fm.verified : [fm.verified]).filter(function (ev) { return ev && typeof ev === 'object'; });
  var last = events[events.length - 1];
  line.appendChild(said(last ? (m && m.superseded ? 'last reviewed by' : 'reviewed by') : 'No review yet', last && last.by, last && last.at));
  var made = fm.generated || {};
  if (made.by) line.appendChild(said('written by', made.by, made.at));
  // A write may be younger than the date the entity carries, which is the one
  // staleness is judged by.
  var dated = String(fm.updated || fm.created || '').slice(0, 10);
  if (made.at && dated && civil(made.at) > dated) line.appendChild(said('dated', '', dated));
  if (m && !e.stray) {
    line.appendChild(verifyButton(e.id));
    line.appendChild(marks.button(e.id, e.rev, m.trust));
  }
  box.appendChild(line);
  if (events.length > 1) {
    var d = el('details');
    d.appendChild(el('summary', null, plural(events.length, 'review')));
    var ol = el('ol', { class: 'events' });
    events.forEach(function (ev) {
      var li = el('li');
      li.appendChild(said('', ev.by, ev.at));
      ol.appendChild(li);
    });
    d.appendChild(ol);
    box.appendChild(d);
  }
  var reasons = e.reasons || {};
  var wrong = MARKERS.filter(function (k) { return m && m[k] && reasons[k]; });
  if (wrong.length) {
    var ul = el('ul', { class: 'reasons' });
    wrong.forEach(function (k) {
      var li = el('li');
      li.appendChild(el('span', { class: 'tag tag-' + k }, markerName(k)));
      li.appendChild(el('span', null, reasons[k]));
      ul.appendChild(li);
    });
    box.appendChild(ul);
  }
  return box;
}

// civil is the day of an instant on the reader's own calendar, as khub writes
// its dates.
function civil(at) {
  var d = new Date(at);
  if (isNaN(d)) return String(at).slice(0, 10);
  var two = function (n) { return (n < 10 ? '0' : '') + n; };
  return d.getFullYear() + '-' + two(d.getMonth() + 1) + '-' + two(d.getDate());
}

function said(what, who, at) {
  var s = el('span', { class: 'said' });
  if (what) s.appendChild(tn(what + ' '));
  if (who) s.appendChild(el('b', null, display(who)));
  if (at) { s.appendChild(tn(' ')); s.appendChild(when(at)); }
  return s;
}

// display renders a frontmatter value as text: scalars as written, lists
// joined, mappings as key: value pairs.
function display(v) {
  if (v === null || v === undefined) return '';
  if (Array.isArray(v)) return v.map(display).join(', ');
  if (typeof v === 'object') {
    return Object.keys(v).map(function (k) { return k + ': ' + display(v[k]); }).join(' · ');
  }
  return String(v);
}

// attributes are the frontmatter keys in file order, without what the rest of
// the page says: the title, the description, the reviews, the writer, and the
// relations when they are listed as links.
function attributes(e, t, lede, H) {
  var fm = e.frontmatter || {};
  var linked = Object.create(null); // a predicate may be named like an Object method
  (e.edges || []).forEach(function (edge) { if (!edge.derived && !e.stray) linked[edge.predicate] = true; });
  var keys = Object.keys(fm).filter(function (k) {
    if (['type', 'verified', 'generated'].indexOf(k) !== -1 || linked[k]) return false;
    if ((k === 'title' || k === 'name') && fm[k] === t) return false;
    return !(k === 'description' && lede);
  });
  if (!keys.length) return null;
  var dl = el('dl', { class: 'kvlist' });
  keys.forEach(function (k) {
    dl.appendChild(el('dt', null, words(k)));
    dl.appendChild(el('dd', null, display(fm[k])));
  });
  var sec = el('section', { class: 'attributes' });
  sec.appendChild(el(H, null, 'Attributes'));
  sec.appendChild(dl);
  return sec;
}

function body(e, ctx, compact, lede) {
  var tree = e.body_tree || [];
  var top = (compact ? 3 : 2) - shallowest(tree);
  // The block renderer's builder: dom.js underneath, so body text reaches the
  // document only through textContent and text nodes.
  var blocks = renderBlocks(tree, {
    link: function (a, id) { opensBeside(a, id, ctx); },
    el: el, text: tn, shift: top,
    append: function (parent, child) { parent.appendChild(child); },
    entityHref: function (id) { return format(entityRoute(id)); }
  });
  if (!blocks.length) return lede ? null : el('p', { class: 'hint' }, 'It has no description and no body.');
  var box = el('div', { class: 'body prose' });
  blocks.forEach(function (b) { box.appendChild(b); });
  return box;
}

// heading names a group of links: the relation in words, and the types at its
// other end.
function heading(g, H) {
  var dot = g.label.indexOf('.');
  var name = words(dot === -1 ? g.label : g.label.slice(dot + 1));
  var peers = Array.from(new Set(g.items.map(function (i) { return i.type; }).filter(Boolean))).join(', ');
  // A relation read backwards under its own name says the wrong thing, so the
  // type leads and the relation follows; a declared inverse reads as it is.
  var backwards = g.direction === 'in' && (dot !== -1 || g.base);
  var first = backwards ? peers : name;
  var h = el(H);
  h.appendChild(tn(first.charAt(0).toUpperCase() + first.slice(1)));
  h.appendChild(el('span', { class: 'n' }, (backwards ? 'through ' + name : peers) + ' (' + num(g.items.length) + ')'));
  return h;
}

function links(e, ctx, H) {
  var sec = el('section', { class: 'links' });
  var groups = e.neighbours || [];
  if (e.stray) return sec;
  var sub = H === 'h2' ? 'h3' : 'h4';
  [['out', 'Links to', 'nothing'], ['in', 'Linked from', 'nothing']].forEach(function (s) {
    var mine = groups.filter(function (g) { return g.direction === s[0]; });
    var h = el(H, null, s[1]);
    if (!mine.length) h.appendChild(el('span', { class: 'n' }, s[2]));
    sec.appendChild(h);
    mine.forEach(function (g) { sec.appendChild(group(g, ctx, sub)); });
  });
  return sec;
}

function group(g, ctx, H) {
  var sec = el('section', { class: 'group' });
  sec.appendChild(heading(g, H));
  var ul = el('ul', { class: 'linkrows' });
  var draw = function (it) {
    var li = el('li', { class: it.resolved ? null : 'unresolved' });
    if (it.resolved) {
      li.appendChild(entityLink(it.id, it.title || it.id, ctx, 't'));
      li.appendChild(el('span', { class: 'sum', title: it.summary || it.id }, it.summary || it.id));
      li.appendChild(markerTags(it.markers));
      li.appendChild(trustTag(it.markers && it.markers.trust));
    } else {
      li.appendChild(el('span', { class: 't mono' }, it.id));
      li.appendChild(el('span', { class: 'sum' }, 'names no entity'));
    }
    ul.appendChild(li);
  };
  // A group the reader opened is open again when Back returns to it.
  var opened = ctx.entry().groups = ctx.entry().groups || Object.create(null);
  g.items.slice(0, opened[g.key] ? g.items.length : GROUP).forEach(draw);
  sec.appendChild(ul);
  if (g.items.length > GROUP && !opened[g.key]) {
    sec.appendChild(el('button', {
      type: 'button', class: 'btn-ghost more',
      on: { click: function (evt) {
        evt.currentTarget.remove();
        opened[g.key] = true;
        g.items.slice(GROUP).forEach(draw);
        // The button is gone: the keyboard goes on from the first new one.
        var next = ul.children[GROUP].querySelector('a');
        if (next) next.focus();
      } }
    }, 'Show all ' + num(g.items.length)));
  }
  return sec;
}

function drawHistory(box, h) {
  var nodes = [el('h2', null, 'History')];
  if (h.note) nodes.push(el('p', { class: 'hint' }, h.note));
  if (h.commits.length) {
    var ol = el('ol', { class: 'commits' });
    var draw = function (c) {
      var li = el('li');
      li.appendChild(el('b', null, c.changed.length ? c.changed.map(words).join(', ') : KINDS[c.kind] || c.kind));
      var sub = el('span', { class: 'sum', title: c.subject });
      if (c.date) { sub.appendChild(when(c.date)); sub.appendChild(tn(' ')); }
      sub.appendChild(tn(c.subject));
      li.appendChild(sub);
      li.appendChild(el('span', { class: 'who mono' }, ['commit ' + String(c.commit || '').slice(0, 8), c.author ? 'by ' + c.author : ''].filter(function (w) { return w && w !== 'commit '; }).join(' · ')));
      ol.appendChild(li);
    };
    h.commits.slice(0, COMMITS).forEach(draw);
    nodes.push(ol);
    if (h.commits.length > COMMITS) {
      nodes.push(el('button', { type: 'button', class: 'copy', on: { click: function (evt) {
        evt.currentTarget.remove();
        h.commits.slice(COMMITS).forEach(draw);
      } } }, num(h.commits.length - COMMITS) + ' earlier'));
    }
    if (h.more) nodes.push(el('p', { class: 'hint' }, 'Older commits are not shown.'));
  }
  fill(box, nodes);
}

// drawSince shows what changed after the newest review, where one no longer
// holds. flag is beside the description the page leads with, and says it is one
// of the changes.
function drawSince(box, s, commits, flag) {
  box.hidden = !s;
  flag.hidden = !(s && s.found && s.fields.some(function (f) { return f.name === 'description' && f.after; }));
  if (!s) return;
  var head = el('p', { class: 'sincehead' });
  head.appendChild(said(s.found ? 'Changed since the review by' : s.holds ? 'Reviewed by' : 'The review no longer holds, by', s.by, s.at));
  if (!s.found) { fill(box, [head, el('p', { class: 'hint' }, s.reason + '. ' + (commits ? 'History lists what each commit changed.' : ''))]); return; }
  var nodes = [head];
  if (s.fields.length) {
    var dl = el('dl', { class: 'diff' });
    s.fields.forEach(function (f) {
      dl.appendChild(el('dt', null, words(f.name)));
      var dd = el('dd');
      if (f.before) dd.appendChild(el('del', null, f.before));
      if (f.before && f.after) dd.appendChild(tn(' '));
      // The new description is the one the page leads with.
      if (f.name === 'description' && f.after) dd.appendChild(tn(f.before ? ' rewritten, as above' : 'added, as above'));
      else if (f.after) dd.appendChild(el('ins', null, f.after));
      else dd.appendChild(tn(' removed'));
      dl.appendChild(dd);
    });
    nodes.push(dl);
  }
  if (s.body.length) {
    var pre = el('div', { class: 'linediff mono', tabIndex: 0, role: 'group', 'aria-label': 'Body lines changed since the review' });
    s.body.forEach(function (l) {
      var kind = { '+': 'add', '-': 'cut', '…': 'gap' }[l.op] || 'same';
      pre.appendChild(el('div', { class: 'l-' + kind }, (l.op === '…' ? '⋯' : l.op) + ' ' + (l.text || '')));
    });
    var lines = s.body.filter(function (l) { return l.op === '+' || l.op === '-'; }).length;
    if (lines <= FOLD) nodes.push(pre);
    else {
      // The body is on the page below, so a long difference is there to open.
      var fold = el('details', { class: 'fold' });
      fold.appendChild(el('summary', null, num(lines) + ' lines of the body changed'));
      fold.appendChild(pre);
      nodes.push(fold);
    }
  }
  if (s.cut) nodes.push(el('p', { class: 'hint' }, 'The comparison is cut here. Read the rest with git.'));
  if (!s.fields.length && !s.body.length && !s.cut) nodes.push(el('p', { class: 'hint' }, 'Its content is the same as reviewed, under another shape of the type.'));
  fill(box, nodes);
}

function drawReach(box, r, ctx) {
  var nodes = [];
  [['out', 'It leads to, through other entities'], ['in', 'Leads to it, through other entities']].forEach(function (side) {
    var levels = r[side[0]].filter(function (l) { return l.total; });
    if (!levels.length) return;
    nodes.push(el('h3', null, side[1]));
    levels.forEach(function (l) {
      var d = el('details', { class: 'level' });
      var sum = el('summary');
      sum.appendChild(el('b', null, plural(l.depth, 'link') + ' away'));
      sum.appendChild(el('span', { class: 'sum' }, ' ' + l.types.map(function (t) { return num(t.total) + ' ' + t.type; }).join(', ')));
      d.appendChild(sum);
      l.types.forEach(function (t) {
        var p = el('p', { class: 'reached' });
        p.appendChild(el('span', { class: 'ty' }, t.type));
        t.items.forEach(function (i, n) {
          if (n) p.appendChild(tn(', '));
          p.appendChild(entityLink(i.id, i.title, ctx));
        });
        if (t.total > t.items.length) p.appendChild(tn(' and ' + num(t.total - t.items.length) + ' more'));
        d.appendChild(p);
      });
      nodes.push(d);
    });
  });
  if (nodes.length) nodes.unshift(el('h2', null, 'Further out'));
  fill(box, nodes);
}
