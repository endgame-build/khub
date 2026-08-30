package reindex

// Ports the library-level STORY-PRJ-001 rows of tests/test_projection.py plus
// the link-escaping row of tests/test_scan_hardening.py (Fix #10). CLI-level
// assertions are left to the golden fixtures.

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
)

// TS-PRJ-001-U01 (REQ-PRJ001-01, PRJ-001): one section per type, in schema
// order, slugs sorted within a type.
func TestGroupByTypeStableOrder(t *testing.T) {
	resolved, idx, _ := live(t, firmOpsWS(t))
	groups := GroupByType(idx.Nodes, resolved)

	var order []string
	for _, g := range groups {
		order = append(order, g.Type)
	}
	// firm-ops declares opportunity, project, meeting, ... person, client.
	want := []string{"opportunity", "project", "meeting", "person", "client"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("section order = %v, want %v", order, want)
	}
	if !reflect.DeepEqual(groups[0].Slugs, []string{"initech-deal"}) {
		t.Errorf("opportunity slugs = %v", groups[0].Slugs)
	}
}

// TS-PRJ-001-U02 (REQ-PRJ001-01): a link per resolved edge; slug fallback when
// the target carries no title.
func TestCrossLinksEdgesAndSlugFallback(t *testing.T) {
	root := freshWS(t)
	seed(t, root, "fragments/titled.md",
		kv{"type", "fragment"}, kv{"stage", "raw"}, kv{"title", "Titled"})
	seed(t, root, "fragments/plain.md", kv{"type", "fragment"}, kv{"stage", "raw"})
	seed(t, root, "fragments/src.md", kv{"type", "fragment"}, kv{"stage", "raw"},
		kv{"related", []any{"titled", "plain"}})

	resolved, idx, g := live(t, root)
	links := CrossLinks(root, resolved, idx, g, index.Node{Type: "fragment", Slug: "src"})
	for _, want := range []string{
		"related → [Titled](fragments/titled.md)", // title used when present
		"related → [plain](fragments/plain.md)",   // slug fallback when absent
	} {
		if !containsStr(links, want) {
			t.Errorf("missing %q in %v", want, links)
		}
	}
}

// TS-PRJ-001-U03 (REQ-PRJ001-01, PRJ-002) + U06: the OKF version and the entity
// count are stamped, and an empty workspace still renders a valid zero-row index.
func TestIndexStampsOKFVersion(t *testing.T) {
	cases := []struct {
		name      string
		root      func(*testing.T) string
		wantCount int64
		wantBody  string
	}{
		{"seeded", firmOpsWS, 5, "initech-deal"},
		{"empty", freshWS, 0, "_No entities._"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			content, count, err := BuildIndexDoc(tc.root(t))
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			if int64(count) != tc.wantCount {
				t.Fatalf("count = %d, want %d", count, tc.wantCount)
			}
			meta := frontmatterOf(t, content)
			if v, _ := meta.Get("okf_version"); v != OKFVersion {
				t.Errorf("okf_version = %v, want %q", v, OKFVersion)
			}
			if v, _ := meta.Get("entity_count"); v != tc.wantCount {
				t.Errorf("entity_count = %v, want %d", v, tc.wantCount)
			}
			if !strings.Contains(content, tc.wantBody) {
				t.Errorf("body does not contain %q:\n%s", tc.wantBody, content)
			}
		})
	}
}

// TS-PRJ-001-U04 (REQ-PRJ001-03, PRJ-001): the index is built from the graph,
// never from a stored copy.
func TestReindexDerivedNotStored(t *testing.T) {
	root := firmOpsWS(t)
	writeRaw(t, root, "index.md",
		"---\nokf_version: '0.1'\n---\n# Index\n\nSTALE-MARKER only\n")
	content, _, err := BuildIndexDoc(root)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if strings.Contains(content, "STALE-MARKER") {
		t.Error("the prior index.md was read as input")
	}
	if !strings.Contains(content, "initech-deal") {
		t.Error("the live graph did not drive the output")
	}
}

// TS-PRJ-001-U05 (REQ-PRJ001-02): the dry-run diff is computed and nothing is
// written; an up-to-date index yields an empty diff.
func TestDryRunDiffComputedNoWrite(t *testing.T) {
	root := firmOpsWS(t)
	path := filepath.Join(root, "index.md")
	writeRaw(t, root, "index.md",
		"---\nokf_version: '0.1'\nentity_count: 0\n---\n# Index\n\n_No entities._\n")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	result := mustReindex(t, root, true)
	if result.Wrote {
		t.Error("dry-run reported a write")
	}
	if result.Diff == "" {
		t.Fatal("dry-run produced no diff")
	}
	if !strings.Contains(result.Diff, "initech-deal") {
		t.Errorf("the added rows are not in the diff:\n%s", result.Diff)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("dry-run wrote to index.md")
	}

	// An up-to-date index diffs to nothing (the CLI turns that into "up to date").
	mustReindex(t, root, false)
	if diff := mustReindex(t, root, true).Diff; diff != "" {
		t.Errorf("an up-to-date index produced a diff:\n%s", diff)
	}
}

// A real write lands the rendered content and reports it.
func TestReindexWritesTheIndex(t *testing.T) {
	root := firmOpsWS(t)
	result := mustReindex(t, root, false)
	if !result.Wrote || result.Count != 5 || result.Diff != "" {
		t.Fatalf("result = %+v", result)
	}
	text := readRaw(t, root, "index.md")
	for _, heading := range []string{
		"## opportunity", "## project", "## meeting", "## person", "## client",
	} {
		if !strings.Contains(text, heading) {
			t.Errorf("missing %q", heading)
		}
	}
	for _, xlink := range []string{
		"owner → [Noor P](identity/team/noor.md)",
		"engagement → [Initech PoV](projects/initech-pov/_index.md)",
	} {
		if !strings.Contains(text, xlink) {
			t.Errorf("missing cross-link %q", xlink)
		}
	}
}

// Finding #2: a title with `](url)` is escaped, never emitted as a live link
// target.
func TestReindexEscapesMarkdownTitle(t *testing.T) {
	root := freshWS(t)
	seed(t, root, "fragments/evil.md", kv{"type", "fragment"}, kv{"stage", "raw"},
		kv{"title", "Deal](http://evil.example)"})
	content, _, err := BuildIndexDoc(root)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(content, `Deal\](http://evil.example)`) {
		t.Errorf("the ] was not escaped:\n%s", content)
	}
	if strings.Contains(content, "[Deal](http://evil.example)") {
		t.Errorf("an injected link target survived:\n%s", content)
	}
}

// Fix #10 (scan hardening): a link path with a space is wrapped in <...>; a
// clean path is left bare.
func TestEntityLinkWrapsProblematicPaths(t *testing.T) {
	root := t.TempDir()
	resolved := resolveInline(t,
		"ontology:\n  entities:\n    doc: {}\n    note: {}\n"+
			"storage:\n  doc: { layout: file, path: 'my docs' }\n"+
			"  note: { layout: file, path: clean }\n")
	cases := []struct{ typeName, want string }{
		{"doc", "<my docs/foo.md>"},
		{"note", "clean/foo.md"},
	}
	for _, tc := range cases {
		if got := EntityLink(root, resolved, tc.typeName, "foo"); got != tc.want {
			t.Errorf("EntityLink(%s) = %q, want %q", tc.typeName, got, tc.want)
		}
	}
}

// 0.11.0: a malformed file is not in the graph, so reindex used to write an
// index with an entire type erased and exit 0 — a silent partial write.
func TestReindexRefusesWhenACollectionIsMalformed(t *testing.T) {
	root := wsFor(t, "build-hub")
	create(t, root, "repo", "svc-a", "repo", "acme/a", "status", "active")
	mustReindex(t, root, false)
	if !strings.Contains(readRaw(t, root, "index.md"), "svc-a") {
		t.Fatal("the healthy index did not carry the repo row")
	}
	before := readRaw(t, root, "index.md")

	collection := "knowledge/architecture/repos.yaml"
	writeRaw(t, root, collection,
		readRaw(t, root, collection)+"svc-a:\n  repo: acme/dup\n  status: active\n")

	_, err := Reindex(root, false)
	if err == nil {
		t.Fatal("reindex wrote a partial index over a malformed scan")
	}
	if code := locatedCode(err); code != "malformed_projection" {
		t.Fatalf("code = %q, want malformed_projection", code)
	}
	if readRaw(t, root, "index.md") != before {
		t.Error("the stale index was not left intact")
	}
}

// --- helpers -----------------------------------------------------------------

func frontmatterOf(t *testing.T, content string) *omap.Map {
	t.Helper()
	meta, _, err := canon.Parse(content, "md")
	if err != nil {
		t.Fatalf("parse rendered index: %v", err)
	}
	return meta
}

// resolveInline resolves a layered inline doc over the embedded core base,
// the way LoadSchema does for a live workspace: the core document goes to
// ResolveWith as the base doc — an authored ontology.base is forbidden.
func resolveInline(t *testing.T, presetYAML string) *schema.ResolvedSchema {
	t.Helper()
	coreRaw, err := os.ReadFile(filepath.Join("..", "..", "presets", "core", "ontology.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	coreVal, err := canon.LoadDocMode(string(coreRaw), canon.Mode12)
	if err != nil {
		t.Fatalf("parse core base: %v", err)
	}
	coreDoc, ok := coreVal.(*omap.Map)
	if !ok {
		t.Fatalf("core base top level is %T, not a mapping", coreVal)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "ontology.yaml")
	if err := os.WriteFile(path, []byte(presetYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	resolved, err := schema.ResolveWith(coreDoc, []string{path})
	if err != nil {
		t.Fatalf("resolve inline schema: %v", err)
	}
	return resolved
}
