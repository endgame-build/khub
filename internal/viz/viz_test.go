package viz

// Ports the library-level STORY-PRJ-003 rows of tests/test_projection.py. CLI
// -level assertions (the "Wrote viz.html (N nodes, M edges)" line, --open) are
// left to the golden fixtures.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/index"
)

// TS-PRJ-003-U01 / U02 (REQ-PRJ003-01, PRJ-006): a node serializes tagged with
// its type; an edge serializes labeled with its predicate.
func TestSerializersTagTypeAndLabelPredicate(t *testing.T) {
	g := liveGraph(t, firmOpsWS(t))
	nodes, edges := Select(g, nil)
	els := ToCytoscape(nodes, edges, nil)

	var deal *nodeData
	for _, el := range els.Nodes {
		d := dataOf(t, el)
		if str(t, d, "id") == "opportunity/initech-deal" {
			deal = &nodeData{id: str(t, d, "id"), label: str(t, d, "label"), typ: str(t, d, "type")}
		}
	}
	if deal == nil {
		t.Fatal("opportunity/initech-deal is not in the elements")
	}
	if deal.typ != "opportunity" {
		t.Errorf("type = %q, want opportunity (it drives by-type coloring)", deal.typ)
	}
	if deal.label != "initech-deal" {
		t.Errorf("label = %q, want the slug", deal.label)
	}

	owner := 0
	for _, el := range els.Edges {
		if str(t, dataOf(t, el), "label") == "owner" {
			owner++
		}
	}
	if owner == 0 {
		t.Error("no edge carries the owner predicate as its label")
	}
}

type nodeData struct{ id, label, typ string }

// TS-PRJ-003-U03 (REQ-PRJ003-02, PRJ-005): every asset is inline; no external
// host is referenced.
func TestAssetInlinerHasNoExternalHost(t *testing.T) {
	g := liveGraph(t, firmOpsWS(t))
	nodes, edges := Select(g, nil)
	html, err := RenderHTML(ToCytoscape(nodes, edges, nil), []string{"opportunity"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	noExternalAssets(t, html)
	if !strings.Contains(html, "cytoscape") || len(html) < 200_000 {
		t.Errorf("the library bytes are not inline (len %d)", len(html))
	}
}

// TS-PRJ-003-U04 (REQ-PRJ003-03): the filter keeps the type, its OUTGOING
// edges, and those edges' targets — an inbound-only neighbour is not pulled in.
func TestTypeFilterKeepsOutboundFrontierOnly(t *testing.T) {
	root := firmOpsWS(t)
	seed(t, root, "identity/team/other.md",
		kv{"type", "person"}, kv{"name", "Other"}, kv{"role", "consultant"})
	g := liveGraph(t, root)
	nodes, edges := Select(g, ptr("project"))

	for _, want := range []index.Node{
		{Type: "project", Slug: "initech-pov"},
		{Type: "client", Slug: "initech"}, // the edges' endpoints
		{Type: "person", Slug: "noor"},
	} {
		if !hasNode(nodes, want) {
			t.Errorf("missing %v in %v", want, nodes)
		}
	}
	for _, unwanted := range []index.Node{
		{Type: "meeting", Slug: "kickoff"}, // inbound-only, not pulled in
		{Type: "person", Slug: "other"},    // unrelated
	} {
		if hasNode(nodes, unwanted) {
			t.Errorf("%v should not be rendered", unwanted)
		}
	}
	for _, e := range edges {
		if e.From.Type != "project" {
			t.Errorf("edge %v does not leave a project", e)
		}
	}
}

// TS-PRJ-003-U05 / 01: the default output path is viz.html, and the firm-ops
// seed draws 5 nodes and 5 edges (opportunity client+owner, project
// client+owner, meeting engagement).
func TestOutPathDefaultAndCounts(t *testing.T) {
	root := firmOpsWS(t)
	result := mustViz(t, root, DefaultOut, nil)
	want := filepath.Join(root, "viz.html")
	if result.Path != want {
		t.Errorf("path = %q, want %q", result.Path, want)
	}
	if _, err := os.Stat(result.Path); err != nil {
		t.Fatalf("viz.html not written: %v", err)
	}
	if result.Nodes != 5 || result.Edges != 5 {
		t.Errorf("counts = (%d nodes, %d edges), want (5, 5)", result.Nodes, result.Edges)
	}
	noExternalAssets(t, readRaw(t, root, "viz.html"))
}

// TS-PRJ-003-U06 / 04 (REQ-PRJ003-01, PRJ-005): an empty graph renders a valid,
// self-contained canvas and reports zero counts.
func TestEmptyCanvasRenderer(t *testing.T) {
	root := freshWS(t)
	result := mustViz(t, root, DefaultOut, nil)
	if result.Nodes != 0 || result.Edges != 0 {
		t.Errorf("counts = (%d, %d), want (0, 0)", result.Nodes, result.Edges)
	}
	html := readRaw(t, root, "viz.html")
	noExternalAssets(t, html)
	for _, want := range []string{"var elements = [];", "cytoscape"} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in the empty canvas", want)
		}
	}
}

// TS-PRJ-003-03: the rendered HTML carries only the filtered nodes.
func TestTypeFilterReachesTheRenderedHTML(t *testing.T) {
	root := firmOpsWS(t)
	seed(t, root, "identity/team/other.md",
		kv{"type", "person"}, kv{"name", "Other"}, kv{"role", "consultant"})
	mustViz(t, root, DefaultOut, ptr("project"))

	html := readRaw(t, root, "viz.html")
	if !strings.Contains(html, `"id": "project/initech-pov"`) {
		t.Error("the filtered type is missing from the payload")
	}
	for _, unwanted := range []string{`"id": "meeting/kickoff"`, `"id": "person/other"`} {
		if strings.Contains(html, unwanted) {
			t.Errorf("%s survived the filter", unwanted)
		}
	}
}

// Finding #4: --out under a non-existent directory creates the directory.
func TestOutCreatesSubdirectory(t *testing.T) {
	root := firmOpsWS(t)
	result := mustViz(t, root, filepath.Join("sub", "deep", "graph.html"), nil)
	want := filepath.Join(root, "sub", "deep", "graph.html")
	if result.Path != want {
		t.Fatalf("path = %q, want %q", result.Path, want)
	}
	if _, err := os.Stat(result.Path); err != nil {
		t.Fatalf("not written: %v", err)
	}
}

// An absolute --out is honored verbatim rather than joined to the root.
func TestAbsoluteOutIsHonored(t *testing.T) {
	root := firmOpsWS(t)
	out := filepath.Join(t.TempDir(), "elsewhere.html")
	if result := mustViz(t, root, out, nil); result.Path != out {
		t.Fatalf("path = %q, want %q", result.Path, out)
	}
}

// Finding #5: a misspelled --type fails loudly instead of rendering an empty
// graph as "success".
func TestUnknownTypeFailsLoudly(t *testing.T) {
	root := firmOpsWS(t)
	_, err := Viz(root, DefaultOut, ptr("bogus"))
	if err == nil {
		t.Fatal("an unknown --type rendered successfully")
	}
	if code := locatedCode(err); code != "unknown_type" {
		t.Fatalf("code = %q, want unknown_type", code)
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("message does not name the type: %s", err)
	}
}

// 0.11.0: a malformed scan must not become a written projection.
func TestVizRefusesWhenACollectionIsMalformed(t *testing.T) {
	root := collectionWS(t)
	writeRaw(t, root, "knowledge/architecture/repos.yaml", "a:\n  repo: x/y\na:\n  repo: x/z\n")
	out := filepath.Join(t.TempDir(), "v.html")

	_, err := Viz(root, out, nil)
	if err == nil {
		t.Fatal("viz rendered a graph missing a whole type")
	}
	if code := locatedCode(err); code != "malformed_projection" {
		t.Fatalf("code = %q, want malformed_projection", code)
	}
	if _, statErr := os.Stat(out); statErr == nil {
		t.Error("the output file was written despite the refusal")
	}
}

// The style block carries one rule per type present, in palette order.
func TestStylePerTypeColors(t *testing.T) {
	style := styleFor([]string{"client", "person"})
	if len(style) != 4 {
		t.Fatalf("style rules = %d, want 4 (node, edge, and one per type)", len(style))
	}
	html, err := RenderHTML(ToCytoscape(nil, nil, nil), []string{"client", "person"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{
		`{"selector": "node[type=\"client\"]", "style": {"background-color": "#00787d"}}`,
		`{"selector": "node[type=\"person\"]", "style": {"background-color": "#8955ab"}}`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing style rule %s", want)
		}
	}
}

func TestRenderHTMLEscapesScriptClose(t *testing.T) {
	// A rendered viz inlines its JSON inside a <script> element. An HTML parser
	// ends that element at the first "</script" in the text, before any
	// JavaScript runs — so an entity title containing one closes the tag and
	// everything after it is parsed as markup. Titles are authored text, and
	// the file gets shared, so that is stored XSS in the artifact.
	//
	// This fires the moment any attacker-influenced field joins the payload.
	// It did: `title` was added and this caught it.
	hostile := `</script><img src=x onerror=alert(1)>`
	nodes := []index.Node{{Type: "component", Slug: "cmp-a"}}
	els := ToCytoscape(nodes, nil, map[index.Node]NodeMeta{
		{Type: "component", Slug: "cmp-a"}: {Title: hostile},
	})
	html, err := RenderHTML(els, []string{"component"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	// Exactly the two closers the template itself emits — the library block and
	// the payload block. A third means the payload broke out.
	if got := strings.Count(html, "</script>"); got != 2 {
		t.Errorf("</script> count = %d, want 2 — the payload escaped its element", got)
	}
	if !strings.Contains(html, `</script`) {
		t.Error("the hostile title was not escaped for script-element embedding")
	}
	// Escaping is transport-only: the value a reader parses must be unchanged.
	var payload []map[string]map[string]any
	start := strings.Index(html, "var elements = ") + len("var elements = ")
	end := strings.Index(html[start:], ";\n")
	if err := json.Unmarshal([]byte(html[start:start+end]), &payload); err != nil {
		t.Fatalf("payload no longer parses as JSON: %v", err)
	}
	if got := payload[0]["data"]["title"]; got != hostile {
		t.Errorf("title round-tripped as %q, want the original %q", got, hostile)
	}
}
