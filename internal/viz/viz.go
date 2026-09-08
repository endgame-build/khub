// Package viz ports src/khub/core/viz.py — `khub viz`, a self-contained
// Cytoscape visualization of the typed graph (WPK-005-1).
//
// It serializes the graph projection to Cytoscape elements (nodes tagged with
// their type for by-type coloring, edges labeled with their predicate —
// PRJ-006) and inlines the whole Cytoscape library plus the elements into one
// HTML file that opens with no network (PRJ-005 / REQ-PRJ003-02). A type filter
// renders only that type and the edges LEAVING it, pulling in just those edges'
// target endpoints (REQ-PRJ003-03). Shares the graph walk with reindex; only
// the render target differs.
package viz

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/endgame-build/khub/internal/assets"
	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/graph"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/values"
	"github.com/endgame-build/khub/internal/workspace"
)

// DefaultOut is viz.DEFAULT_OUT.
const DefaultOut = "viz.html"

// Palette is Paper's --cat-* categorical ramp in its sanctioned hex form,
// cycled over the types present so node color is stable per run and distinct
// across types (PRJ-006).
//
// serve's ui/app.css repeats these as --graph-1..--graph-10, because a browser
// stylesheet cannot read a Go slice. That duplication is pinned by
// TestPaletteMatchesStylesheet in internal/serve, not by this comment: prose
// asking two files to stay in step is how they stop being in step.
//
// Hex rather than the token sheet's lab() because Cytoscape parses colors
// itself and reads neither CSS custom properties nor lab() — the same
// accommodation the token sheet already makes for Mermaid. serve's app.css
// carries these as CSS variables; the two must stay in step.
//
// Ten slots, every one at or above 4.28:1 against Paper's --paper ground.
// --cat-purple, not --cat-violet: violet is indistinguishable from blue at
// tint strength.
var Palette = []string{
	"#00787d", "#8955ab", "#015ba6", "#157e2c", "#357f95",
	"#a03e7c", "#b63d65", "#80572c", "#65761c", "#216548",
}

// Result is viz.VizResult: where the render landed and how much it drew.
type Result struct {
	Path  string
	Nodes int
	Edges int
}

// Elements is the Cytoscape `elements` payload — viz.to_cytoscape's
// {"nodes": [...], "edges": [...]} with the key order that dict never had to
// carry (the two lists are concatenated before serialization).
type Elements struct {
	Nodes []*omap.Map
	Edges []*omap.Map
}

// Graph is the read path `viz` and `serve` share: resolve the schema, rebuild
// the index and the graph from the live files, apply the type filter, and
// return the Cytoscape elements plus the type names present (the palette's
// domain).
//
// Both callers rebuild per invocation — viz once per run, serve once per
// request — so neither can serve a stale graph. That is the same property
// search has for the same reason: khub persists no index.
func Graph(root string, typeFilter *string) (*Elements, []string, error) {
	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		return nil, nil, err
	}
	if typeFilter != nil && !resolved.Types.Has(*typeFilter) {
		// A misspelled --type must fail loudly, not render an empty graph as "success".
		return nil, nil, unknownType(root, resolved, *typeFilter)
	}
	scanned, err := index.Build(root, resolved)
	if err != nil {
		return nil, nil, err
	}
	if err := index.RejectMalformed(scanned, "render the graph"); err != nil {
		return nil, nil, err
	}
	idx := index.Filter(scanned, index.StrayNodes(scanned)) // strays are not entities
	g := graph.BuildGraph(idx)

	nodes, edges := Select(g, typeFilter)
	typeSet := map[string]bool{}
	meta := make(map[index.Node]NodeMeta, len(nodes))
	for _, n := range nodes {
		typeSet[n.Type] = true
		rtype, _ := resolved.Types.Get(n.Type)
		draftRaw, _ := idx.Meta[n].Get("draft")
		titleRaw, _ := idx.Meta[n].Get("title")
		title, _ := titleRaw.(string)
		meta[n] = NodeMeta{
			Title:  title,
			Draft:  values.AsBool(draftRaw),
			Orphan: graph.IsOrphan(g, n, rtype),
		}
	}
	return ToCytoscape(nodes, edges, meta), sortedKeys(typeSet), nil
}

// Viz is viz.viz: render the typed graph to a self-contained Cytoscape HTML at
// out.
//
// typeFilter is Python's `type_filter: str | None`, so an explicitly empty
// string is a type name no schema declares (which fails loudly), not an absent
// filter.
func Viz(root string, out string, typeFilter *string) (*Result, error) {
	elements, types, err := Graph(root, typeFilter)
	if err != nil {
		return nil, err
	}
	html, err := RenderHTML(elements, types)
	if err != nil {
		return nil, err
	}

	path := out
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, out)
	}
	// Honor --out under a new subdirectory.
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(html), 0o666); err != nil {
		return nil, err
	}
	return &Result{Path: path, Nodes: len(elements.Nodes), Edges: len(elements.Edges)}, nil
}

// Select is viz._select: the nodes and edges to render — the whole graph, or
// one type and its OUTGOING edges.
//
// Under a type filter the kept edges are those leaving a node of that type; the
// kept nodes are those nodes plus the endpoints those edges reach. A node
// incident only via an inbound edge (e.g. a meeting pointing at a project) is
// not pulled in — the filter is on the outbound frontier of the type
// (TS-PRJ-003-03).
//
// The unfiltered branch returns graph order verbatim (Python's
// `list(graph.nodes)` / `graph.edges(...)`, never sorted); the filtered branch
// sorts its node set, exactly as Python does.
func Select(g *graph.Graph, typeFilter *string) ([]index.Node, []graph.Edge) {
	allEdges := g.Edges()
	if typeFilter == nil {
		return g.Nodes(), allEdges
	}
	typed := map[index.Node]bool{}
	for _, n := range g.Nodes() {
		if n.Type == *typeFilter {
			typed[n] = true
		}
	}
	edges := []graph.Edge{}
	nodeSet := map[index.Node]bool{}
	for n := range typed {
		nodeSet[n] = true
	}
	for _, e := range allEdges {
		if typed[e.From] {
			edges = append(edges, e)
			nodeSet[e.To] = true
		}
	}
	nodes := make([]index.Node, 0, len(nodeSet))
	for n := range nodeSet {
		nodes = append(nodes, n)
	}
	index.SortNodes(nodes)
	return nodes, edges
}

// NodeMeta is the per-node state the elements payload reports beyond identity:
// `title` is the human name from frontmatter, `draft` the manual publish flag,
// `orphan` graph.IsOrphan. All three are read or derived on every build; none
// is stored.
type NodeMeta struct {
	Title  string
	Draft  bool
	Orphan bool
}

// ToCytoscape is viz.to_cytoscape: nodes and edges as Cytoscape element
// records. Each node carries its type (for by-type coloring) plus its draft and
// orphan flags (so a surface can dim or ring them without a second request);
// each edge carries its predicate as `label` (PRJ-006).
//
// meta may omit a node — the flags then read false and the keys are still
// emitted, so the record shape never varies.
func ToCytoscape(nodes []index.Node, edges []graph.Edge, meta map[index.Node]NodeMeta) *Elements {
	els := &Elements{Nodes: []*omap.Map{}, Edges: []*omap.Map{}}
	for _, n := range nodes {
		data := omap.New()
		data.Set("id", n.ID())
		data.Set("label", n.Slug)
		data.Set("type", n.Type)
		// The slug stays `label` — it is the identifier, and viz's own style
		// draws it. `title` is carried alongside so a surface can show the
		// human name without a second request per node; it is optional in the
		// schema, so it is often empty and the reader falls back to the slug.
		data.Set("title", meta[n].Title)
		data.Set("draft", meta[n].Draft)
		data.Set("orphan", meta[n].Orphan)
		el := omap.New()
		el.Set("data", data)
		els.Nodes = append(els.Nodes, el)
	}
	for _, e := range edges {
		data := omap.New()
		data.Set("source", e.From.ID())
		data.Set("target", e.To.ID())
		data.Set("label", e.Predicate)
		el := omap.New()
		el.Set("data", data)
		els.Edges = append(els.Edges, el)
	}
	return els
}

// RenderHTML is viz.render_html: the elements and the inlined Cytoscape library
// wrapped into one standalone HTML file.
func RenderHTML(elements *Elements, types []string) (string, error) {
	combined := make([]any, 0, len(elements.Nodes)+len(elements.Edges))
	for _, n := range elements.Nodes {
		combined = append(combined, n)
	}
	for _, e := range elements.Edges {
		combined = append(combined, e)
	}
	// json.dumps defaults: ", " / ": " separators, ensure_ascii=True — EncodeCLI.
	payload, err := canon.EncodeCLI(combined)
	if err != nil {
		return "", err
	}
	style, err := canon.EncodeCLI(styleFor(types))
	if err != nil {
		return "", err
	}
	return template(assets.CytoscapeJS, scriptSafe(payload), scriptSafe(style)), nil
}

// scriptSafe escapes a JSON document for embedding inside an HTML <script>
// element.
//
// An HTML parser ends a script element at the first "</script" in its text,
// before any JavaScript runs — so a JSON string containing one closes the tag
// and everything after it becomes markup. Entity titles are authored text, and
// a title of `</script><img src=x onerror=alert(1)>` therefore executes for
// whoever opens the rendered file. That is stored XSS in an artifact people
// share, and it is why this exists.
//
// Escaping `<` as \u003c is a JSON string escape, so the parsed value is
// byte-identical and only the transport changes. Applied to the whole document
// rather than to the dangerous fields, because "which fields are attacker
// influenced" is not a question this function should have to keep answering.
func scriptSafe(doc string) string {
	return strings.ReplaceAll(doc, "<", `\u003c`)
}

// styleFor is viz._style: label nodes/edges, arrow edges, and one color per
// type present.
func styleFor(types []string) []any {
	nodeStyle := omap.New()
	nodeStyle.Set("label", "data(label)")
	nodeStyle.Set("font-size", 8)
	nodeStyle.Set("background-color", "#999")
	nodeStyle.Set("text-valign", "center")
	nodeStyle.Set("color", "#fff")
	nodeStyle.Set("width", 18)
	nodeStyle.Set("height", 18)

	edgeStyle := omap.New()
	edgeStyle.Set("label", "data(label)")
	edgeStyle.Set("font-size", 6)
	edgeStyle.Set("width", 1)
	edgeStyle.Set("line-color", "#ccc")
	edgeStyle.Set("curve-style", "bezier")
	edgeStyle.Set("target-arrow-shape", "triangle")
	edgeStyle.Set("target-arrow-color", "#ccc")

	style := []any{rule("node", nodeStyle), rule("edge", edgeStyle)}
	for i, tname := range types {
		typeStyle := omap.New()
		typeStyle.Set("background-color", Palette[i%len(Palette)])
		style = append(style, rule(`node[type="`+tname+`"]`, typeStyle))
	}
	return style
}

func rule(selector string, style *omap.Map) *omap.Map {
	r := omap.New()
	r.Set("selector", selector)
	r.Set("style", style)
	return r
}

// template is viz._TEMPLATE with its three substitutions.
func template(lib, elements, style string) string {
	return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>khub viz</title>
<style>html,body{margin:0;height:100%}#cy{width:100%;height:100vh;display:block}</style>
<script>` + lib + `</script>
</head>
<body>
<div id="cy"></div>
<script>
var elements = ` + elements + `;
var style = ` + style + `;
cytoscape({
  container: document.getElementById('cy'),
  elements: elements,
  style: style,
  layout: { name: 'cose' }
});
</script>
</body>
</html>
`
}

// unknownType is LocatedError.unknown_type with the workspace's preset name.
func unknownType(root string, resolved *schema.ResolvedSchema, name string) error {
	prov, err := workspace.Provenance(root)
	if err != nil {
		return err
	}
	preset, _ := prov.Get("preset")
	presetName, _ := preset.(string)
	known := append([]string{}, resolved.Types.Keys()...)
	sort.Strings(known)
	return errs.UnknownType(name, presetName, known)
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
