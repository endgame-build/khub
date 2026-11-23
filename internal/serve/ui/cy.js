// cy.js — the only module that names the `cytoscape` global.
//
// It draws one picture now: the ontology overview. Everything outside this
// file talks about ids and normalized events, so the renderer is swappable and
// no view has to learn a graph library's API.
//
// One instance per visit to the overview: mounted when the view opens,
// destroyed when it closes, so a reload of the data is a fresh draw rather than
// a diff against stale elements.

var cy = null;

export function destroy() {
  if (cy) { cy.destroy(); cy = null; }
}

// seed gives every node a deterministic starting position. Never-laid-out
// nodes sit at (0,0), and `cose` derives its initial temperature and bounding
// box from the current configuration — an all-at-origin start leaves the boxes
// piled on each other. Deterministic (FNV-1a over the id) so a reload draws
// the same picture.
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

// mount draws the elements in container and lays them out.
export function mount(container, elements, style, layoutName) {
  destroy();
  var box = Math.max(600, Math.sqrt(Math.max(elements.nodes.length, 1)) * 160);
  cy = cytoscape({
    container: container,
    elements: {
      nodes: elements.nodes.map(function (n) {
        return { data: n.data, classes: n.classes, position: seed(n.data.id, box) };
      }),
      edges: elements.edges
    },
    style: style,
    layout: { name: 'preset' }
  });
  runLayout(layoutName);
  return cy;
}

function runLayout(name) {
  var opts = {
    name: name, animate: false, fit: true, padding: 40,
    // A type box is a labelled slab; pack by the label, not a dot.
    nodeDimensionsIncludeLabels: true
  };
  if (name === 'circle') {
    // A ring cannot produce an outlier: every type is equidistant, all of
    // them fit, and the same schema draws the same picture every time.
    opts.spacingFactor = 1.1;
    opts.avoidOverlap = true;
  } else if (name === 'cose') {
    // Past a dozen types a ring strings its labels out over a huge
    // circumference; a force layout keeps related types near each other.
    // Deterministic: no randomized start, so a reload draws the same picture.
    // Tuned by eye against a 16-type schema: stronger gravity pulls the
    // unconnected singletons in instead of leaving a long tail that shrinks
    // the fit until the labels are unreadable.
    opts.randomize = false;
    opts.idealEdgeLength = 90;
    opts.nodeRepulsion = 9000;
    opts.gravity = 2.5;
    opts.nodeOverlap = 30;
    opts.numIter = 2500;
    opts.componentSpacing = 60;
  }
  cy.layout(opts).run();
}

// select marks the selected elements and clears the rest.
export function select(ids) {
  if (!cy) return;
  var set = new Set(ids);
  cy.batch(function () {
    cy.elements().forEach(function (ele) { ele.toggleClass('picked', set.has(ele.id())); });
  });
}

// on forwards normalized taps. Nothing outside this file sees a cytoscape
// event object.
export function on(handler) {
  cy.on('tap', 'node', function (evt) {
    handler({ target: 'node', id: evt.target.id(), data: evt.target.data() });
  });
  cy.on('tap', 'edge', function (evt) {
    handler({ target: 'edge', id: evt.target.id(), data: evt.target.data() });
  });
  cy.on('tap', function (evt) {
    if (evt.target === cy) handler({ target: 'background' });
  });
  // The relation name sharpens under the pointer.
  cy.on('mouseover', 'edge', function (e) { e.target.addClass('hot'); });
  cy.on('mouseout', 'edge', function (e) { e.target.removeClass('hot'); });
}

// buildStyle takes already-resolved colour strings — cy.js never reads CSS.
// Cytoscape parses colours itself and understands neither custom properties nor
// lab(), which is why app.js hands it hex.
export function buildStyle(types, colors, ink) {
  var style = [
    {
      selector: 'node.sch',
      style: {
        'label': 'data(label)', 'font-size': 12, 'font-weight': 600,
        'shape': 'round-rectangle', 'width': 'data(w)', 'height': 30,
        'text-valign': 'center', 'text-halign': 'center', 'color': ink.mid,
        'background-color': ink.soft, 'background-opacity': 0.16,
        'min-zoomed-font-size': 0, 'text-max-width': 220, 'text-wrap': 'none'
      }
    },
    // A type the schema declares but nothing has been written for.
    { selector: 'node.sch.emptytype', style: { 'background-opacity': 0.05, 'color': ink.soft } },
    { selector: 'node.sch[schemaKind = "any"]', style: { 'background-opacity': 0.05, 'color': ink.soft, 'border-width': 1, 'border-style': 'dashed', 'border-color': ink.soft } },
    {
      selector: 'edge.sch',
      style: {
        'label': 'data(label)', 'font-size': 9, 'color': ink.soft,
        'text-rotation': 'autorotate', 'text-background-color': '#ffffff',
        'text-background-opacity': 0.8, 'text-background-padding': 2,
        'width': 0.9, 'line-color': ink.mid, 'target-arrow-color': ink.mid,
        'target-arrow-shape': 'triangle', 'arrow-scale': 0.7,
        'curve-style': 'bezier', 'opacity': 0.6,
        // Optional by default; required is drawn solid below.
        'line-style': 'dashed'
      }
    },
    { selector: 'edge.sch.required', style: { 'line-style': 'solid' } },
    // Declared and never used. The gap between a contract and its data.
    { selector: 'edge.sch.unused', style: { 'opacity': 0.3, 'line-style': 'dotted' } },
    // A type relating to its own kind (supersedes) loops beside its box. A
    // bezier edge whose ends meet draws as a loop by itself; there is no
    // `loop` curve-style, and naming one stops cytoscape computing the
    // geometry of every edge, not just this one.
    {
      selector: 'edge.sch.selfloop',
      style: {
        'loop-direction': '0deg', 'loop-sweep': '-40deg',
        'control-point-step-size': 40, 'text-margin-y': -6
      }
    },
    {
      selector: 'edge.sch.hot',
      style: {
        'font-size': 11, 'color': ink.mid, 'opacity': 1, 'width': 2,
        'text-background-opacity': 0.9
      }
    },
    {
      selector: 'node.picked',
      style: { 'border-width': 3, 'border-color': ink.navy, 'border-opacity': 1, 'border-style': 'solid' }
    },
    {
      selector: 'edge.picked',
      style: { 'width': 3, 'opacity': 1, 'line-color': ink.navy, 'target-arrow-color': ink.navy, 'color': ink.navy }
    }
  ];
  types.forEach(function (name, i) {
    style.push({
      selector: 'node[type="' + name + '"]',
      style: { 'background-color': colors[i % colors.length] }
    });
  });
  return style;
}
