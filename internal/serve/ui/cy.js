// cy.js — the only module that names the `cytoscape` global.
//
// It draws one picture: the ontology overview, at the positions layout.js
// computed. Nothing here moves a node — there is no physics and no layout run
// in the browser — so the picture the tests hold free of overlaps is the one
// on screen. Everything outside this file talks about ids and normalized
// events, so no view has to learn a graph library's API.

var cy = null;

export function destroy() {
  if (cy) { cy.destroy(); cy = null; }
}

// MAX_ZOOM keeps a small schema from being blown up to fill the canvas.
var MAX_ZOOM = 1.25;

// mount draws placed elements (layout.placeElements) and fits them.
export function mount(container, elements, style) {
  destroy();
  cy = cytoscape({
    container: container,
    elements: { nodes: elements.nodes, edges: elements.edges },
    style: style,
    layout: { name: 'preset' },
    // The layout is the contract: dragging a box would put it back on top of
    // another, so boxes stay where they were placed. Pan and zoom remain.
    autoungrabify: true,
    boxSelectionEnabled: false
  });
  fit();
  return cy;
}

// fit frames the whole diagram, never zooming a small one past MAX_ZOOM.
export function fit() {
  if (!cy) return;
  cy.fit(cy.elements(), 32);
  if (cy.zoom() > MAX_ZOOM) {
    cy.zoom(MAX_ZOOM);
    cy.center();
  }
}

// select marks the selected elements and, around them, their neighbourhood:
// a selected type keeps its relations and the types at their other ends in
// full colour, and everything else fades. An empty selection clears both.
export function select(ids) {
  if (!cy) return;
  var picked = cy.collection();
  ids.forEach(function (id) { picked = picked.union(cy.getElementById(id)); });
  var keep = picked.union(picked.nodes().connectedEdges())
    .union(picked.nodes().connectedEdges().connectedNodes())
    .union(picked.edges().connectedNodes());
  // A self-relation's tag and its type's box belong together, both ways.
  picked.nodes('.tag').forEach(function (t) { keep = keep.union(cy.getElementById('schema:' + t.data('from'))); });
  picked.nodes('.sch-type').forEach(function (n) {
    keep = keep.union(cy.nodes('.tag').filter(function (t) { return 'schema:' + t.data('from') === n.id(); }));
  });
  cy.batch(function () {
    cy.elements().forEach(function (ele) {
      ele.toggleClass('picked', picked.has(ele));
      ele.toggleClass('faded', picked.length > 0 && !keep.has(ele) && !ele.hasClass('grp'));
    });
  });
}

// on forwards normalized taps. Nothing outside this file sees a cytoscape
// event object.
export function on(handler) {
  cy.on('tap', 'node', function (evt) {
    handler({ target: 'node', id: evt.target.id(), data: evt.target.data() });
  });
  cy.on('dbltap', 'node', function (evt) {
    handler({ target: 'node', open: true, id: evt.target.id(), data: evt.target.data() });
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

// MONO matches layout.js: box widths are computed from character counts, so
// labels are set in a monospace face whose advance is known.
var MONO = 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace';

// buildStyle takes already-resolved colour strings — cy.js never reads CSS.
// Cytoscape parses colours itself and understands neither custom properties nor
// lab(), which is why app.js hands it hex.
export function buildStyle(types, colors, ink, boxH, groupPad) {
  var style = [
    {
      selector: 'node.sch',
      style: {
        'label': 'data(label)', 'font-family': MONO, 'font-size': 12, 'font-weight': 600,
        'shape': 'round-rectangle', 'width': 'data(w)', 'height': boxH,
        'text-valign': 'center', 'text-halign': 'center', 'color': ink.mid,
        'background-color': ink.soft, 'background-opacity': 0.16,
        'border-width': 1, 'border-color': ink.soft, 'border-opacity': 0.25,
        'min-zoomed-font-size': 0, 'text-wrap': 'none'
      }
    },
    // A type the schema declares but nothing has been written for.
    { selector: 'node.emptytype', style: { 'background-opacity': 0.05, 'color': ink.soft } },
    // The base block: what every type inherits. No relations are drawn to it.
    { selector: 'node.sch-any', style: { 'background-opacity': 0.04, 'color': ink.soft, 'border-style': 'dashed', 'border-opacity': 0.8 } },
    // "any type": where an any-target relation points.
    {
      selector: 'node.sch-ghost',
      style: {
        'font-size': 10, 'font-weight': 400, 'color': ink.soft,
        'background-opacity': 0, 'border-style': 'dashed', 'border-opacity': 0.7
      }
    },
    // Group frames: a quiet tint behind related types, titled at the top.
    {
      selector: 'node.grp',
      style: {
        'shape': 'round-rectangle', 'background-color': ink.soft, 'background-opacity': 0.045,
        'border-width': 0, 'padding': groupPad, 'label': 'data(label)',
        'min-width': 'data(minW)', 'min-height': 'data(minH)',
        'min-width-bias-left': '50%', 'min-width-bias-right': '50%',
        'min-height-bias-top': '50%', 'min-height-bias-bottom': '50%',
        'font-family': MONO, 'font-size': 10, 'color': ink.soft,
        'text-valign': 'top', 'text-halign': 'center',
        'text-margin-y': 18, 'min-zoomed-font-size': 0, 'events': 'no'
      }
    },
    {
      selector: 'edge.sch',
      style: {
        'label': 'data(label)', 'font-family': MONO, 'font-size': 10, 'color': ink.mid,
        'text-background-color': '#ffffff', 'text-background-opacity': 0.9,
        'text-background-padding': 2, 'text-background-shape': 'round-rectangle',
        'width': 1, 'line-color': ink.mid, 'target-arrow-color': ink.mid,
        'target-arrow-shape': 'triangle', 'arrow-scale': 0.8,
        'curve-style': 'bezier', 'opacity': 0.8,
        // Optional by default; required is drawn solid below.
        'line-style': 'dashed', 'line-dash-pattern': [5, 3]
      }
    },
    // A routed edge follows the layered layout's polyline around the rows it
    // crosses; the weights and distances are measured from the box centres.
    {
      selector: 'edge.routed',
      style: {
        'curve-style': 'segments', 'edge-distances': 'node-position',
        'segment-weights': 'data(segW)', 'segment-distances': 'data(segD)',
        'segment-radius': 6
      }
    },
    { selector: 'edge.required', style: { 'line-style': 'solid' } },
    // Declared and never used. The gap between a contract and its data.
    { selector: 'edge.unused', style: { 'opacity': 0.4, 'line-style': 'dotted', 'line-dash-pattern': [1, 3] } },
    // A relation from a type to itself: a tag beside the box, in the
    // relation's own line language (solid border when required).
    {
      selector: 'node.tag',
      style: {
        'shape': 'round-rectangle', 'width': 'data(w)', 'height': 18,
        'label': 'data(label)', 'font-family': MONO, 'font-size': 10, 'font-weight': 400,
        'color': ink.mid, 'text-valign': 'center', 'text-halign': 'center',
        'background-color': '#ffffff', 'background-opacity': 1,
        'border-width': 1, 'border-color': ink.mid, 'border-opacity': 0.6, 'border-style': 'dashed'
      }
    },
    { selector: 'node.tag.required', style: { 'border-style': 'solid' } },
    { selector: 'node.tag.unused', style: { 'opacity': 0.55, 'border-style': 'dotted' } },
    { selector: 'edge.hot', style: { 'font-size': 11, 'color': ink.mid, 'opacity': 1, 'width': 2 } },
    { selector: 'node.picked', style: { 'border-width': 3, 'border-color': ink.navy, 'border-opacity': 1, 'border-style': 'solid' } },
    { selector: 'edge.picked', style: { 'width': 2.5, 'opacity': 1, 'line-color': ink.navy, 'target-arrow-color': ink.navy, 'color': ink.navy } },
    { selector: '.faded', style: { 'opacity': 0.18 } }
  ];
  types.forEach(function (name, i) {
    style.push({
      selector: 'node.sch-type[type="' + name + '"]',
      style: { 'background-color': colors[i % colors.length], 'border-color': colors[i % colors.length] }
    });
  });
  return style;
}
