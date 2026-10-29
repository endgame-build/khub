package entity

// Read-path assertions for the entity port: id resolution (bare, qualified,
// case-folded, ambiguous), read-time inverse-edge derivation, and the layout
// path resolver. Drawn from tests/test_entity_read.py plus the resolve_id
// contract in core/entity.py.

import (
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/schema"
)

func buildIndex(t *testing.T, ws string) (*schema.ResolvedSchema, *index.Index) {
	t.Helper()
	resolved, err := introspect.LoadSchema(ws)
	requireNoError(t, err)
	idx, err := index.Build(ws, resolved)
	requireNoError(t, err)
	return resolved, idx
}

// A bare slug resolves; a qualified type/slug is exact; a bare slug shared by
// two types is ambiguous.
func TestResolveIDBareQualifiedAndAmbiguous(t *testing.T) {
	ws := newWS(t, "firm-ops")
	seed(t, ws, "clients/acme.md", kv("type", "client", "name", "Acme"))
	seed(t, ws, "partnerships/acme/_index.md", kv("type", "partnership", "partner", "Acme"))
	seed(t, ws, "identity/team/pat.md", kv("type", "person", "name", "Pat", "role", "partner"))
	_, idx := buildIndex(t, ws)

	node, err := ResolveID(idx, "pat")
	requireNoError(t, err)
	if node != (index.Node{Type: "person", Slug: "pat"}) {
		t.Fatalf("node = %#v", node)
	}
	node, err = ResolveID(idx, "partnership/acme")
	requireNoError(t, err)
	if node != (index.Node{Type: "partnership", Slug: "acme"}) {
		t.Fatalf("node = %#v", node)
	}
	_, err = ResolveID(idx, "acme")
	e := requireCode(t, err, "ambiguity_error")
	requireMessageContains(t, e, "Slug 'acme' is ambiguous: client/acme, partnership/acme")

	_, err = ResolveID(idx, "ghost")
	_ = requireCode(t, err, "lookup_error")
	_, err = ResolveID(idx, "client/ghost")
	_ = requireCode(t, err, "lookup_error")
}

// Case is resolved leniently as a fallback: writes slugify to lowercase, so an
// agent reusing the --id it passed must still read the entity back.
func TestResolveIDFoldsCaseAsAFallback(t *testing.T) {
	ws := newWS(t, "firm-ops")
	seed(t, ws, "clients/acme-corp.md", kv("type", "client", "name", "Acme"))
	_, idx := buildIndex(t, ws)

	node, err := ResolveID(idx, "ACME-Corp")
	requireNoError(t, err)
	if node.Slug != "acme-corp" {
		t.Fatalf("slug = %q", node.Slug)
	}
	node, err = ResolveID(idx, "Client/ACME-Corp")
	requireNoError(t, err)
	if node != (index.Node{Type: "client", Slug: "acme-corp"}) {
		t.Fatalf("node = %#v", node)
	}
}

// get --edges lists the stored forward edges and the read-time derived
// inverses, the derived ones qualified type/slug.
func TestGetEdgesIncludesDerivedInverses(t *testing.T) {
	ws := newWS(t, "build-hub")
	older, err := Create(ws, "adr", CreateOpts{
		Fields: fields("title", "Old choice", "status", "accepted"), UseTemplate: true})
	requireNoError(t, err)
	newer, err := Create(ws, "adr", CreateOpts{
		Fields: fields("title", "New choice", "status", "accepted"), UseTemplate: true})
	requireNoError(t, err)
	_, err = Link(ws, newer.Slug, "supersedes", older.Slug)
	requireNoError(t, err)

	forward, err := Get(ws, newer.Slug, true)
	requireNoError(t, err)
	if len(forward.Edges) != 1 {
		t.Fatalf("forward edges = %#v", forward.Edges)
	}
	if !reflect.DeepEqual(forward.Edges[0], Edge{Predicate: "supersedes", Target: older.Slug, Derived: false, ResolvedTargets: []string{"adr/" + older.Slug}}) {
		t.Fatalf("stored edge = %#v", forward.Edges[0])
	}

	inverse, err := Get(ws, older.Slug, true)
	requireNoError(t, err)
	if len(inverse.Edges) != 1 {
		t.Fatalf("derived edges = %#v", inverse.Edges)
	}
	want := Edge{Predicate: "superseded", Target: "adr/" + newer.Slug, Derived: true, ResolvedTargets: []string{"adr/" + newer.Slug}}
	if !reflect.DeepEqual(inverse.Edges[0], want) {
		t.Fatalf("derived edge = %#v, want %#v", inverse.Edges[0], want)
	}

	// Without --edges the field stays nil, so the CLI omits the key entirely.
	plain, err := Get(ws, older.Slug, false)
	requireNoError(t, err)
	if plain.Edges != nil {
		t.Fatalf("edges = %#v, want nil", plain.Edges)
	}
}

// An inbound edge through a relation with no declared inverse is listed too,
// named `<source type>.<predicate>` as search counts it, so get --edges agrees
// with neighbors --in (issue #79).
func TestGetEdgesIncludesInboundWithoutInverse(t *testing.T) {
	ws := newWS(t, "build-hub")
	cmp, err := Create(ws, "component", CreateOpts{
		Fields: fields("title", "Search", "kind", "service"), UseTemplate: true})
	requireNoError(t, err)
	uc, err := Create(ws, "use-case", CreateOpts{
		Fields: fields("title", "Find a document", "trigger", "human", "served_by", cmp.Slug), UseTemplate: true})
	requireNoError(t, err)
	req, err := Create(ws, "requirement", CreateOpts{
		Fields: fields("title", "Search answers fast", "kind", "non-functional", "realized_in", cmp.Slug), UseTemplate: true})
	requireNoError(t, err)

	view, err := Get(ws, cmp.Slug, true)
	requireNoError(t, err)
	// Scan order, which follows the schema's type order: use-case, then requirement.
	want := []Edge{
		{Predicate: "serves", Target: "use-case/" + uc.Slug, Derived: true,
			ResolvedTargets: []string{"use-case/" + uc.Slug}},
		{Predicate: "requirement.realized_in", Target: "requirement/" + req.Slug, Derived: true,
			ResolvedTargets: []string{"requirement/" + req.Slug}},
	}
	if !reflect.DeepEqual(view.Edges, want) {
		t.Fatalf("edges = %#v, want %#v", view.Edges, want)
	}
}

// A hand-edited source that names the target in several spellings is one
// inbound edge per (source, predicate), as the graph counts it — with or
// without a declared inverse — and remove's inbound count agrees.
func TestGetEdgesDedupesInboundSpellings(t *testing.T) {
	ws := newWS(t, "build-hub")
	cmp, err := Create(ws, "component", CreateOpts{
		Fields: fields("title", "Search", "kind", "service"), UseTemplate: true})
	requireNoError(t, err)
	uc, err := Create(ws, "use-case", CreateOpts{
		Fields: fields("title", "Find a document", "trigger", "human", "served_by", cmp.Slug), UseTemplate: true})
	requireNoError(t, err)
	req, err := Create(ws, "requirement", CreateOpts{
		Fields: fields("title", "Search answers fast", "kind", "non-functional", "realized_in", cmp.Slug), UseTemplate: true})
	requireNoError(t, err)

	spellings := "[" + cmp.Slug + ", component/" + cmp.Slug + ", " + strings.ToUpper(cmp.Slug) + "]"
	for _, e := range []struct {
		path, field string
	}{{uc.Path, "served_by"}, {req.Path, "realized_in"}} {
		text := readFile(t, e.path)
		re := regexp.MustCompile(`(?m)^` + e.field + `:.*(\n *- .*)*$`)
		if !re.MatchString(text) {
			t.Fatalf("%s: no %s line in\n%s", e.path, e.field, text)
		}
		writeFile(t, e.path, re.ReplaceAllLiteralString(text, e.field+": "+spellings))
	}

	view, err := Get(ws, cmp.Slug, true)
	requireNoError(t, err)
	want := []Edge{
		{Predicate: "serves", Target: "use-case/" + uc.Slug, Derived: true,
			ResolvedTargets: []string{"use-case/" + uc.Slug}},
		{Predicate: "requirement.realized_in", Target: "requirement/" + req.Slug, Derived: true,
			ResolvedTargets: []string{"requirement/" + req.Slug}},
	}
	if !reflect.DeepEqual(view.Edges, want) {
		t.Fatalf("edges = %#v, want %#v", view.Edges, want)
	}

	resolved, idx := buildIndex(t, ws)
	if got := inboundEdges(idx, resolved, index.Node{Type: "component", Slug: cmp.Slug}); len(got) != 2 {
		t.Fatalf("inbound = %#v, want 2 edges", got)
	}
}

// EntityPath resolves each layout: folder _index, flat file, singleton file,
// collection inventory file.
func TestEntityPathPerLayout(t *testing.T) {
	ws := collectionWS(t)
	_, idx := buildIndex(t, ws)
	cases := []struct{ typeName, slug, want string }{
		{"project", "initech-pov", filepath.Join(ws, "projects", "initech-pov", "_index.md")},
		{"client", "acme", filepath.Join(ws, "clients", "acme.md")},
		{"repo", "anything", filepath.Join(ws, "repo.jsonl")},
	}
	for _, c := range cases {
		node := index.Node{Type: c.typeName, Slug: c.slug}
		if got := EntityPath(ws, idx, node); got != c.want {
			t.Errorf("EntityPath(%s) = %q, want %q", c.typeName, got, c.want)
		}
	}
}

func TestEntityPathTemplatedScanAndCreationFallback(t *testing.T) {
	ws := t.TempDir()
	tp, err := schema.ParseTemplate("work/{epic}", "md")
	requireNoError(t, err)
	rtype := &schema.ResolvedType{
		Name: "story",
		Storage: schema.StorageConfig{
			Layout: schema.LayoutFile, Fmt: "md", Templates: []*schema.PathTemplate{tp},
		},
	}
	types := schema.NewOrdered[*schema.ResolvedType]()
	types.Set("story", rtype)
	idx := &index.Index{Resolved: &schema.ResolvedSchema{Types: types}}
	node := index.Node{Type: "story", Slug: "a"}

	fallback := filepath.Join(ws, "work", "a.md")
	if got := EntityPath(ws, idx, node); got != fallback {
		t.Fatalf("creation fallback = %q, want %q", got, fallback)
	}
	idx.Paths = map[index.Node]string{node: "work/ep-1/a.md"}
	if got := EntityPath(ws, idx, node); got != filepath.Join(ws, "work", "ep-1", "a.md") {
		t.Fatalf("scanned path = %q", got)
	}
}

// A get on a per-item entity returns the file bytes as raw and the parsed
// frontmatter/body split.
func TestGetPerItemRawIsTheWholeFile(t *testing.T) {
	ws := newWS(t, "firm-ops")
	people(t, ws)
	path := filepath.Join(ws, "fragments", "note.md")
	mkdirAll(t, filepath.Dir(path))
	text := "---\ntype: fragment\nstage: raw\nowner: ann\n---\nsome prose\n"
	writeFile(t, path, text)

	view, err := Get(ws, "note", false)
	requireNoError(t, err)
	if view.Raw != text {
		t.Fatalf("raw = %q", view.Raw)
	}
	// The READ altitude strips (python-frontmatter); the EDIT altitude
	// (readDoc/_split_frontmatter) does not — khub keeps both.
	if view.Body != "some prose" {
		t.Fatalf("body = %q", view.Body)
	}
	if view.Locator != "" {
		t.Fatalf("locator = %q, want empty for a per-item entity", view.Locator)
	}
	if got := view.Meta.Keys(); !equalStrings(got, []string{"type", "stage", "owner"}) {
		t.Fatalf("frontmatter keys = %v", got)
	}
}
