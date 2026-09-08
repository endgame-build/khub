// cy.js — the only module that names the `cytoscape` global.
//
// Everything outside this file talks about ids and normalized events, so the
// renderer is swappable and no panel has to learn a graph library's API.
//
// One instance, created once, never destroyed. The reason is identity, not
// speed: the command composer picks A and B across different levels, so with a
// destroy-and-recreate design `compose.a` would be a dangling reference by
// construction and every consumer would need a defensive re-lookup. Here
// `cy.$id(id)` is valid for every id at every level, forever.

var cy = null;
var snapNextFit = false;

// setSnapNextFit makes the next fit instant. Used once at boot.
export function setSnapNextFit() { snapNextFit = true; }

// seed gives every node a deterministic starting position. Never-laid-out
// nodes sit at (0,0), and `cose` derives its initial temperature and bounding
// box from the current configuration — an all-at-origin start makes the first
// expansion behave badly. Deterministic (FNV-1a over the id) so a reload does
// not reshuffle the picture.
function seed(id, box) {
  var h = 2166136261;
  for (var i = 0; i < id.length; i++) {
    h ^= id.charCodeAt(i);
    h = Math.imul(h, 16777619);
  }
  var a = (h >>> 0) / 4294967295;
  var b = (Math.imul(h, 2654435761) >>> 0) / 4294967295;
  return { x: Math.round(box * a), y: Math.round(box * b) };
}

export function mount(container, elements, style) {
  var box = Math.max(1200, Math.sqrt(Math.max(elements.nodes.length, 1)) * 90);
  var nodes = elements.nodes.map(function (n) {
    return { data: n.data, position: seed(n.data.id, box) };
  });
  cy = cytoscape({
    container: container,
    elements: { nodes: nodes, edges: elements.edges },
    style: style,
    layout: { name: 'preset' },
    // Verified present in the vendored 3.33.1 bundle. All three are free and
    // none of them matters below a few hundred nodes.
    textureOnViewport: true,
    hideEdgesOnViewport: true,
    motionBlur: false,
    wheelSensitivity: 0.2
  });
  return cy;
}

// showOnly hides everything not in `ids`.
//
// A class toggle against one stylesheet rule, never a per-element style
// bypass: a bypass allocates an override object per element and forces a full
// style merge, which is invisible at 300 elements and costly at 6,000. One
// pass, inside one batch, so styling is marked dirty once.
export function showOnly(ids) {
  cy.batch(function () {
    cy.elements().forEach(function (ele) {
      ele.toggleClass('off', !ids.has(ele.id()));
    });
  });
}

// setOverlay replaces the synthetic elements a view adds beside the instance
// graph. Only the ontology uses it now — the per-type aggregate it also served
// was deleted once the schema view made it the same picture with different
// meaning. They are added and removed rather than left resident: synthetics
// living alongside 2,000 entities would force every selector and handler to
// discriminate, and that leaks into every call site.
//
// Rebuilt rather than detached-and-restored, because which edges the ontology
// shows depends on which predicates are hidden.
export function setOverlay(elements) {
  // Schema ids live in their own `schema:` namespace and can never collide
  // with an entity's `type/slug`. Clearing here is what stops a view switch
  // leaving a stale node behind — a collision would throw inside cy.add,
  // mid-render, with state already committed.
  cy.remove('.sch');
  if (!elements) return [];
  var added = cy.add(elements.nodes.concat(elements.edges));
  // The caller folds these into the visible set: showOnly hides everything it
  // is not given, and freshly added elements would otherwise vanish.
  return added.map(function (ele) { return ele.id(); });
}

export function runLayout(spec) {
  // Always the visible collection, never cy.layout(): the core-level call
  // operates on ALL elements including the hidden ones.
  var eles = cy.elements().not('.off');
  if (!eles.length) return;

  var opts = {
    name: spec.name, animate: false, fit: false, padding: 30,
    // Every layout here packs by node geometry, and a khub node is a 16px dot
    // with an ~84px name hanging under it. Without this the layout solves a
    // problem the reader does not have — the circles clear each other while the
    // labels pile up. Supported by grid, circle, concentric and cose alike.
    nodeDimensionsIncludeLabels: true
  };
  if (spec.name === 'schema') {
    // A ring. Tried and rejected: cose, which with three connected pairs and
    // three isolates has almost nothing to push against and flung a connected
    // type into a corner; and breadthfirst, which lays out one row per depth
    // and — with label-aware sizing on eight boxes — spread rows far past the
    // viewport. A ring cannot produce an outlier: every type is equidistant,
    // all of them fit, and the same schema draws the same picture every time.
    opts.name = 'circle';
    opts.spacingFactor = 1.1;
    opts.avoidOverlap = true;
  } else if (spec.name === 'grid') {
    opts.avoidOverlap = true;
    opts.condense = false;
  } else if (spec.name === 'circle') {
    opts.spacingFactor = 1.4;
  } else if (spec.name === 'concentric') {
    // Highest value goes innermost, so an inverted hop distance puts the focus
    // at the centre and each hop in its own ring.
    var hops = spec.hops;
    opts.concentric = hops
      ? function (n) { return 100 - (hops.has(n.id()) ? hops.get(n.id()) : 99); }
      : function (n) { return n.degree(false); };
    // Banded, never one ring per distinct value: with unique values that puts a
    // single node in every ring, and a lone node in a ring sits at angle 0 —
    // the whole graph collapses into a vertical line.
    opts.levelWidth = hops
      ? function () { return 1; }   // one ring per hop — the values are already banded
      : function (nodes) { return Math.max(1, nodes.maxDegree(false) / 4); };
    // Rings need room for the labels hanging below each node, not just the
    // nodes themselves.
    opts.minNodeSpacing = hops ? 46 : 24;
  } else if (spec.name === 'cose') {
    // Tuned against a measured count of overlapping label boxes, not by eye.
    opts.idealEdgeLength = 120;
    opts.nodeRepulsion = 12000;
    opts.nodeOverlap = 24;
    opts.numIter = 600;
    opts.componentSpacing = 120;
  }
  eles.layout(opts).run();
  // One viewport tween instead of an animated layout: layout animation above
  // ~150 nodes is jank, and this reads as motion for four lines.
  // Fit, then refuse to zoom out past the point where labels stop drawing.
  //
  // A label-aware layout spreads 110 nodes over ~1750x2400, and fitting all of
  // that into the canvas lands at zoom 0.28 — below min-zoomed-font-size, so
  // every name silently disappears. Showing the whole graph and showing the
  // names are in direct conflict at this size; names win, because a graph of
  // anonymous dots answers nothing. Beyond the floor you pan, or use search, or
  // open one entity at L2 — which is what L2 is for.
  if (snapNextFit) {
    snapNextFit = false;
    cy.fit(eles, 40);
    clampZoom(eles);
    return;
  }
  cy.animate({
    fit: { eles: eles, padding: 40 }, duration: 220,
    complete: function () { clampZoom(eles); }
  });
}

// LABEL_FONT and LABEL_MIN_ZOOM must agree with the node style below: a label
// is drawn only while font-size * zoom >= min-zoomed-font-size, so this is the
// zoom at which names appear.
var LABEL_FONT = 9;
var LABEL_MIN_ZOOM = 5;
var ZOOM_FLOOR = LABEL_MIN_ZOOM / LABEL_FONT;

function clampZoom(eles) {
  if (cy.zoom() >= ZOOM_FLOOR) return;
  cy.zoom({ level: ZOOM_FLOOR, renderedPosition: { x: cy.width() / 2, y: cy.height() / 2 } });
  cy.center(eles);
}

// mark paints the composer's picks and, once A is chosen, dims every node whose
// type no predicate on A can reach. legal === null means an `any` relation
// makes every type legal, so nothing is dimmed.
export function mark(a, b, legal) {
  cy.batch(function () {
    cy.nodes().forEach(function (n) {
      var id = n.id();
      n.toggleClass('pick-a', id === a);
      n.toggleClass('pick-b', id === b);
      n.toggleClass('illegal', !!a && legal !== null && !n.hasClass('sch') && !legal.has(n.data('type')));
    });
  });
}

// Hovering an ontology edge names it. Ten declared relations over seven types
// cannot all carry a label at once without colliding, so the label follows the
// pointer instead.
export function hoverLabels() {
  cy.on('mouseover', 'edge.sch', function (e) { e.target.addClass('hot'); });
  cy.on('mouseout', 'edge.sch', function (e) { e.target.removeClass('hot'); });
}

// on forwards normalized events. Nothing outside this file sees a cytoscape
// event object.
export function on(handler) {
  cy.on('tap', 'node', function (evt) {
    var n = evt.target;
    handler({ target: 'node', id: n.id(), data: n.data(), schema: n.hasClass('sch') });
  });
  cy.on('tap', 'edge', function (evt) {
    var e = evt.target;
    handler({ target: 'edge', id: e.id(), data: e.data(), schema: e.hasClass('sch') });
  });
  cy.on('tap', function (evt) {
    if (evt.target === cy) handler({ target: 'background' });
  });
}

// buildStyle takes already-resolved colour strings — cy.js never reads CSS.
// Cytoscape parses colours itself and understands neither custom properties nor
// lab(), which is why app.js hands it hex.
export function buildStyle(types, colors, ink) {
  var style = [
    {
      selector: 'node',
      style: {
        'label': 'data(display)', 'font-size': LABEL_FONT, 'color': ink.mid,
        'text-valign': 'bottom', 'text-margin-y': 3,
        'background-color': ink.soft, 'width': 16, 'height': 16,
        'min-zoomed-font-size': LABEL_MIN_ZOOM, 'text-background-opacity': 0,
        // khub slugs run long ("req-capture-fx-quotes-before-the-cutoff"), and
        // at any useful ring radius the full labels collide into an unreadable
        // stripe. One truncation point, ellipsis, full name in the inspector.
        'text-max-width': 84, 'text-wrap': 'ellipsis', 'text-overflow-wrap': 'anywhere'
      }
    },
    {
      selector: 'edge',
      style: {
        // 40% transparent, off --graph-ink-mid rather than a lighter token.
        //
        // The original --graph-line measured 1.42:1 against the canvas, well
        // under the 3:1 floor for a graphic that carries meaning — and these
        // edges carry the structure. Transparency lowers contrast, so the base
        // colour has to absorb it: ink-mid at 40% transparent lands at 3.26:1,
        // where ink-soft would have missed at 2.51.
        //
        // Contrast, not weight, is what makes an edge visible here: at the zoom
        // floor a 1px stroke renders 0.56px and a 1.4px one 0.78px, and the old
        // faint colour was invisible at both. 1.4 only stops the stroke thinning
        // further when zoomed out.
        'width': 1.4, 'line-color': ink.mid, 'target-arrow-color': ink.mid,
        'opacity': 0.6,
        'target-arrow-shape': 'triangle', 'arrow-scale': 0.7,
        // straight, not bezier: bezier recomputes control points per edge per
        // frame and bundles O(k^2) within each parallel group. L2 opts back in
        // below, where the set is small and direction reads matter most.
        'curve-style': 'straight'
      }
    },
    { selector: 'edge.l2', style: { 'curve-style': 'bezier', 'label': 'data(label)', 'font-size': 8, 'color': ink.soft } },
    { selector: '.off', style: { 'display': 'none' } },
    { selector: 'node.draft', style: { 'opacity': 0.45 } },
    { selector: 'node.orphan', style: { 'border-width': 2, 'border-color': ink.warn, 'border-opacity': 1 } },
    { selector: 'node.illegal', style: { 'opacity': 0.15 } },
    { selector: 'node.pick-a', style: { 'border-width': 3, 'border-color': ink.navy, 'border-opacity': 1 } },
    { selector: 'node.pick-b', style: { 'border-width': 3, 'border-color': ink.navy, 'border-opacity': 1, 'border-style': 'dashed' } },
    // ——— ontology ———
    // A declared type reads as a labelled slab, not a dot: it is a kind of
    // thing, and the name is the point. Sized by how many entities it holds.
    {
      selector: 'node.sch',
      style: {
        'label': 'data(label)', 'font-size': 12, 'font-weight': 600,
        'shape': 'round-rectangle', 'width': 'label', 'height': 26,
        'padding': 10, 'text-valign': 'center', 'color': ink.mid,
        'background-opacity': 0.16, 'min-zoomed-font-size': 0,
        'text-max-width': 200, 'text-wrap': 'none'
      }
    },
    // A type the schema declares but nothing has been written for.
    { selector: 'node.sch.emptytype', style: { 'background-opacity': 0.05, 'color': ink.soft } },
    { selector: 'node.sch[schemaKind = "any"]', style: { 'background-opacity': 0.05, 'color': ink.soft } },
    {
      selector: 'edge.sch',
      style: {
        // Named at rest. A relation diagram whose relations are anonymous
        // answers nothing, and there are only ten of them — the earlier
        // collision was the layout packing them, not the labels existing.
        'label': 'data(label)', 'font-size': 9, 'color': ink.soft,
        'text-rotation': 'autorotate', 'text-background-color': '#ffffff',
        'text-background-opacity': 0.8, 'text-background-padding': 2,
        // Thin and light: the ontology is a small, sparse picture where the
        // node names carry the meaning and the lines only need to say what
        // connects to what. Held at 0.6 rather than lighter because ink-mid
        // below that drops under 3:1 against the canvas, and these lines are
        // the structure, not decoration.
        'width': 0.9, 'line-color': ink.mid, 'target-arrow-color': ink.mid,
        'target-arrow-shape': 'triangle', 'arrow-scale': 0.7,
        'curve-style': 'bezier', 'opacity': 0.6,
        // Optional by default; required is drawn solid below.
        'line-style': 'dashed'
      }
    },
    { selector: 'edge.sch.required', style: { 'line-style': 'solid' } },
    // A derived inverse is real ontology — khub computes it at read time from
    // the forward edge — so it is drawn, but distinctly: nobody authors it.
    { selector: 'edge.sch.derived', style: { 'line-style': 'dotted', 'target-arrow-shape': 'vee' } },
    // Declared and never used. The gap between a contract and its data.
    { selector: 'edge.sch.unused', style: { 'opacity': 0.3, 'color': ink.soft, 'line-style': 'dotted' } },
    // A type relating to its own kind is ordinary ontology (supersedes), but
    // the default curve draws it as a knot over the node.
    {
      selector: 'edge.sch.selfloop',
      style: {
        'curve-style': 'loop', 'loop-direction': '0deg', 'loop-sweep': '-40deg',
        // Pinned, not derived from the node box: a loop scaled off node width
        // was a long arc on `adr 480` and a stub on `feature-spec 0`, so the
        // same relation looked like two different things.
        'control-point-step-size': 32, 'text-margin-y': -6
      }
    },
    // The name only appears where it can be read: under the pointer, or on the
    // edge you have selected.
    {
      selector: 'edge.sch.hot',
      style: {
        'label': 'data(label)', 'font-size': 10, 'color': ink.mid,
        'opacity': 1, 'width': 2, 'font-size': 11, 'color': ink.mid,
        'text-background-color': '#ffffff', 'text-background-opacity': 0.85,
        'text-background-padding': 2
      }
    },
  ];
  types.forEach(function (name, i) {
    style.push({
      selector: 'node[type="' + name + '"]',
      style: { 'background-color': colors[i % colors.length] }
    });
  });
  return style;
}

// flags paints the per-node state the server sent. Applied once at load: draft
// and orphan are properties of the entity, not of the current view.
export function applyFlags() {
  cy.batch(function () {
    cy.nodes().forEach(function (n) {
      if (n.data('draft')) n.addClass('draft');
      if (n.data('orphan')) n.addClass('orphan');
    });
  });
}

export function setLevelClass(level) {
  cy.batch(function () {
    cy.edges().toggleClass('l2', level === 2);
  });
}
