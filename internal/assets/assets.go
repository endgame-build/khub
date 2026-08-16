// Package assets carries the vendored browser assets khub inlines into its
// derived artifacts — the Go home of src/khub/assets/, which core/viz.py
// reaches through `_ASSETS = .../assets` and reads with `.read_text()`.
//
// Embedding rather than reading at runtime is what keeps `khub viz` a single
// static binary: the Python package shipped the file inside the wheel, and the
// Go build has no wheel to read from.
package assets

import _ "embed"

// CytoscapeJS is the whole cytoscape.min.js library, byte-identical to
// src/khub/assets/cytoscape.min.js. viz inlines it in a <script> tag so the
// rendered HTML opens with no network (PRJ-005 / REQ-PRJ003-02).
//
//go:embed cytoscape.min.js
var CytoscapeJS string
