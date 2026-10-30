package viz

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/schema"
)

// aspectData splits a render into node and edge data records.
func aspectData(t *testing.T, els *Elements) (nodes, edges []map[string]any) {
	t.Helper()
	for _, el := range els.Nodes {
		d := dataOf(t, el)
		m := map[string]any{}
		for _, k := range d.Keys() {
			m[k], _ = d.Get(k)
		}
		nodes = append(nodes, m)
	}
	for _, el := range els.Edges {
		d := dataOf(t, el)
		m := map[string]any{}
		for _, k := range d.Keys() {
			m[k], _ = d.Get(k)
		}
		edges = append(edges, m)
	}
	return nodes, edges
}

// The instance aspect is Graph, byte for byte: the default must not move.
func TestInstanceAspectIsTheDefaultRender(t *testing.T) {
	root := firmOpsWS(t)
	a := mustViz(t, root, "a.html", nil)
	b, err := VizAspect(root, "b.html", AspectInstances, nil)
	if err != nil {
		t.Fatalf("viz: %v", err)
	}
	if a.Nodes != b.Nodes || a.Edges != b.Edges {
		t.Fatalf("counts differ: %+v vs %+v", a, b)
	}
	if readRaw(t, root, "a.html") != readRaw(t, root, "b.html") {
		t.Error("the instance aspect does not render the default bytes")
	}
	if !strings.Contains(readRaw(t, root, "a.html"), "layout: { name: 'cose' }") {
		t.Error("the default layout literal moved")
	}
}

// The schema aspect draws one box per declared type plus `any`, every type's
// own relations once per target, and the base relations once on `any` — and
// nothing from the entities.
func TestSchemaAspectDrawsTheOntology(t *testing.T) {
	root := firmOpsWS(t)
	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		t.Fatal(err)
	}
	els, types, err := AspectGraph(root, AspectSchema, nil)
	if err != nil {
		t.Fatalf("schema aspect: %v", err)
	}
	nodes, edges := aspectData(t, els)
	if len(nodes) != resolved.Types.Len()+1 {
		t.Fatalf("nodes = %d, want one per declared type plus any (%d)", len(nodes), resolved.Types.Len()+1)
	}
	for i, name := range resolved.Types.Keys() {
		if nodes[i]["id"] != SchemaID(name) || nodes[i]["aspect"] != AspectSchema {
			t.Errorf("node %d = %v, want the %s box", i, nodes[i], name)
		}
	}
	if last := nodes[len(nodes)-1]; last["id"] != SchemaAny {
		t.Errorf("last node = %v, want the any box", last)
	}
	if len(types) != resolved.Types.Len() {
		t.Errorf("palette domain = %v, want every declared type", types)
	}

	want := resolved.BaseRelations.Len()
	for _, name := range resolved.Types.Keys() {
		rtype, _ := resolved.Types.Get(name)
		for _, rel := range ownRelations(resolved, rtype) {
			if rel.Kind == "any" {
				want++
			} else {
				want += len(rel.Targets)
			}
		}
	}
	if len(edges) != want {
		t.Errorf("edges = %d, want %d", len(edges), want)
	}
	for _, e := range edges {
		if e["aspect"] != AspectSchema {
			t.Errorf("edge %v is not a schema edge", e)
		}
		if strings.Contains(e["source"].(string), "/") {
			t.Errorf("edge %v leaves an entity", e)
		}
	}
}

// Attribute lines and relation labels carry UML multiplicity, and a base
// attribute sits on `any`, not on every type.
func TestSchemaNotation(t *testing.T) {
	if got := multiplicity(false, false); got != "[0..1]" {
		t.Errorf("optional single = %s", got)
	}
	if got := multiplicity(true, false); got != "[1]" {
		t.Errorf("required single = %s", got)
	}
	if got := multiplicity(false, true); got != "[0..*]" {
		t.Errorf("optional many = %s", got)
	}
	if got := multiplicity(true, true); got != "[1..*]" {
		t.Errorf("required many = %s", got)
	}

	root := firmOpsWS(t)
	resolved, _ := introspect.LoadSchema(root)
	els, _, err := AspectGraph(root, AspectSchema, nil)
	if err != nil {
		t.Fatal(err)
	}
	nodes, _ := aspectData(t, els)
	base := resolved.BaseAttributes.Keys()
	for _, n := range nodes {
		label := n["label"].(string)
		lines := strings.Split(label, "\n")
		for _, line := range lines[1:] {
			if !strings.HasSuffix(line, "]") {
				t.Errorf("%s: attribute line %q has no multiplicity", n["id"], line)
			}
		}
		if n["id"] == SchemaAny {
			if len(lines) != len(base)+1 {
				t.Errorf("any box lists %d attributes, want the %d base ones", len(lines)-1, len(base))
			}
			continue
		}
		rtype, _ := resolved.Types.Get(n["type"].(string))
		for _, line := range lines[1:] {
			name := line[:strings.Index(line, ":")]
			a, _ := rtype.Attributes.Get(name)
			if resolved.BaseAttributes.Has(name) && !a.OverriddenFromBase {
				t.Errorf("%s repeats the base attribute %s", n["id"], name)
			}
		}
	}
}

// The schema aspect reads only the schema, so a malformed entity file — which
// refuses the instance render — does not stop it.
func TestSchemaAspectIgnoresMalformedEntities(t *testing.T) {
	root := collectionWS(t)
	writeRaw(t, root, "knowledge/architecture/repos.yaml", "a:\n  repo: x/y\na:\n  repo: x/z\n")
	out := filepath.Join(t.TempDir(), "s.html")
	if _, err := VizAspect(root, out, AspectSchema, nil); err != nil {
		t.Fatalf("schema aspect refused over a malformed entity: %v", err)
	}
	if _, err := VizAspect(root, out, AspectBoth, nil); locatedCode(err) != "malformed_projection" {
		t.Errorf("both aspect: err = %v, want malformed_projection", err)
	}
}

// --type in the schema aspect keeps the type and the boxes its own relations
// reach — the outbound frontier, as in the instance aspect.
func TestSchemaTypeFilterKeepsOutboundFrontier(t *testing.T) {
	root := firmOpsWS(t)
	resolved, _ := introspect.LoadSchema(root)
	els, _, err := AspectGraph(root, AspectSchema, ptr("project"))
	if err != nil {
		t.Fatal(err)
	}
	nodes, edges := aspectData(t, els)
	if nodes[0]["id"] != SchemaID("project") {
		t.Errorf("first box = %v, want project", nodes[0]["id"])
	}
	drawn := map[string]bool{}
	for _, n := range nodes {
		drawn[n["id"].(string)] = true
	}
	base := 0
	for _, e := range edges {
		if e["source"] != SchemaID("project") {
			t.Errorf("edge %v does not leave project", e)
		}
		if e["target"] == SchemaAny && resolved.BaseRelations.Has(e["predicate"].(string)) {
			base++
		}
		if !drawn[e["target"].(string)] {
			t.Errorf("edge %v lands on an undrawn box", e)
		}
	}
	rtype, _ := resolved.Types.Get("project")
	if len(ownRelations(resolved, rtype)) > 0 && len(edges) == 0 {
		t.Error("project's relations were not drawn")
	}
	// The base relations are part of the type's frontier: they leave it for any.
	if base != resolved.BaseRelations.Len() {
		t.Errorf("base relations drawn from project = %d, want %d", base, resolved.BaseRelations.Len())
	}
	if !drawn[SchemaAny] {
		t.Error("the any box is missing under a filter")
	}
}

// A type may be named anything the loader accepts, `*` included; no type box
// id can be the any box's.
func TestAnyBoxIDCannotCollideWithATypeBox(t *testing.T) {
	for _, name := range []string{"*", "any", "-any", ""} {
		if SchemaID(name) == SchemaAny {
			t.Errorf("type %q collides with the any box", name)
		}
	}
	if strings.HasPrefix(SchemaAny, "schema:") || strings.Contains(SchemaAny, "/") {
		t.Errorf("SchemaAny %q shares a namespace with types or entities", SchemaAny)
	}
}

// Enum values that read as notation are quoted; a list's upper bound is *;
// an inverse is named on the edge it is derived from.
func TestSchemaNotationEdges(t *testing.T) {
	a := &schema.ResolvedAttribute{Name: "k", BaseType: "text", Enum: []string{"a", "b|c", "d}", "", " e"}}
	if got, want := attrLine(a), `k: {a|"b|c"|"d}"|""|" e"} [0..1]`; got != want {
		t.Errorf("enum line = %s, want %s", got, want)
	}
	if got := attrLine(&schema.ResolvedAttribute{Name: "tags", BaseType: "list"}); got != "tags: list [0..*]" {
		t.Errorf("list line = %s", got)
	}
	if got := attrLine(&schema.ResolvedAttribute{Name: "tags", BaseType: "list", Required: true}); got != "tags: list [1..*]" {
		t.Errorf("required list line = %s", got)
	}
	inv := "consumed_by"
	e := schemaEdge("a", "b", &schema.ResolvedRelation{Predicate: "consumes", Many: true, Inverse: &inv})
	if label := str(t, dataOf(t, e), "label"); label != "consumes [0..*] / consumed_by" {
		t.Errorf("label = %v", label)
	}
}

// A library caller that skips the CLI's check gets an error, not a render of
// another aspect.
func TestUnknownAspectIsAnError(t *testing.T) {
	root := firmOpsWS(t)
	if _, _, err := AspectGraph(root, "nope", nil); err == nil {
		t.Error("an unknown aspect rendered")
	}
	if _, err := VizAspect(root, "x.html", "", nil); err == nil {
		t.Error("an empty aspect rendered")
	}
}

// The combined view adds one instance-of edge per entity, landing on a drawn
// type box, and draws both halves.
func TestBothAspectLinksEveryEntityToItsType(t *testing.T) {
	root := firmOpsWS(t)
	inst, _, err := AspectGraph(root, AspectInstances, nil)
	if err != nil {
		t.Fatal(err)
	}
	sch, _, err := AspectGraph(root, AspectSchema, nil)
	if err != nil {
		t.Fatal(err)
	}
	both, _, err := AspectGraph(root, AspectBoth, nil)
	if err != nil {
		t.Fatal(err)
	}
	nodes, edges := aspectData(t, both)
	if len(nodes) != len(inst.Nodes)+len(sch.Nodes) {
		t.Errorf("nodes = %d, want %d + %d", len(nodes), len(inst.Nodes), len(sch.Nodes))
	}
	drawn := map[string]bool{}
	for _, n := range nodes {
		drawn[n["id"].(string)] = true
	}
	realized := 0
	for _, e := range edges {
		if e["aspect"] != "instance_of" {
			continue
		}
		realized++
		src := e["source"].(string)
		if e["target"] != SchemaID(src[:strings.Index(src, "/")]) || !drawn[e["target"].(string)] {
			t.Errorf("instance-of edge %v does not land on its type", e)
		}
	}
	if realized != len(inst.Nodes) {
		t.Errorf("instance-of edges = %d, want one per entity (%d)", realized, len(inst.Nodes))
	}
	if len(edges) != len(inst.Edges)+len(sch.Edges)+realized {
		t.Errorf("edges = %d, want both halves plus instance-of", len(edges))
	}
}

// Under --type the combined view still gives every drawn entity a type box,
// including an endpoint of another type.
func TestBothAspectTypeFilterBoxesEveryDrawnType(t *testing.T) {
	root := firmOpsWS(t)
	both, _, err := AspectGraph(root, AspectBoth, ptr("meeting"))
	if err != nil {
		t.Fatal(err)
	}
	nodes, edges := aspectData(t, both)
	drawn := map[string]bool{}
	for _, n := range nodes {
		drawn[n["id"].(string)] = true
	}
	for _, e := range edges {
		if !drawn[e["source"].(string)] || !drawn[e["target"].(string)] {
			t.Errorf("edge %v has an undrawn endpoint", e)
		}
	}
}

// Every aspect renders a self-contained file with its own layout.
func TestAspectRendersAreSelfContained(t *testing.T) {
	root := firmOpsWS(t)
	for aspect, layout := range map[string]string{
		AspectSchema: "name: 'circle'",
		AspectBoth:   "name: 'breadthfirst'",
	} {
		out := aspect + ".html"
		if _, err := VizAspect(root, out, aspect, nil); err != nil {
			t.Fatalf("%s: %v", aspect, err)
		}
		html := readRaw(t, root, out)
		noExternalAssets(t, html)
		if !strings.Contains(html, layout) {
			t.Errorf("%s: layout %q missing", aspect, layout)
		}
		if !strings.Contains(html, `node[aspect=\"schema\"]`) {
			t.Errorf("%s: schema style missing", aspect)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "viz.html")); err == nil {
		t.Error("an aspect render wrote the default path")
	}
}
