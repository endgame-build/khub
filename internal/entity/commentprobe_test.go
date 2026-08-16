package entity

// The comment-preservation regression that started as an env-gated probe.
// Python khub edits through ruamel's round-trip loader, so a hand-written
// frontmatter comment survives an edit; canon's splice write path
// (internal/canon/splice.go) now does the same. Both expectations below are
// the literal bytes Python khub 0.18 wrote for the same fixture and edit.

import (
	"path/filepath"
	"strings"
	"testing"
)

// commentedClient is the hand-authored fixture: the comment sits on a line the
// edit never touches, which is the shape a whole-document re-emit destroyed.
const commentedClient = "---\n" +
	"type: client\n" +
	"name: Comment Co   # keep this comment\n" +
	"description: before\n" +
	"created: 2026-01-01\n" +
	"updated: 2026-01-01\n" +
	"---\n"

func TestCommentSurvivesEdit(t *testing.T) {
	ws := newWS(t, "firm-ops")
	path := filepath.Join(ws, "clients", "comment-co.md")
	mkdirAll(t, filepath.Dir(path))
	writeFile(t, path, commentedClient)

	if _, err := Update(ws, "client/comment-co",
		UpdateOpts{Fields: fields("description", "touched-by-go")}); err != nil {
		t.Fatalf("update: %v", err)
	}
	want := "---\n" +
		"type: client\n" +
		"name: Comment Co   # keep this comment\n" +
		"description: touched-by-go\n" +
		"created: 2026-01-01\n" +
		"updated: " + today().ISO + "\n" +
		"---\n"
	if got := readFile(t, path); got != want {
		t.Fatalf("edit output\n got: %q\nwant: %q", got, want)
	}
}

// A comment on the edited key's OWN line is re-seated at its recorded column,
// exactly as ruamel's Emitter.write_comment does (pad back to the column, never
// fewer than one space).
func TestInlineCommentKeepsItsColumn(t *testing.T) {
	ws := newWS(t, "firm-ops")
	path := filepath.Join(ws, "clients", "edge-co.md")
	mkdirAll(t, filepath.Dir(path))
	writeFile(t, path, "---\n"+
		"type: client\n"+
		"name: Edge Co\n"+
		"stage:     prospect  # gapped\n"+
		"created: 2026-01-01\n"+
		"updated: 2026-01-01\n"+
		"---\n")

	if _, err := Update(ws, "client/edge-co",
		UpdateOpts{Fields: fields("stage", "won")}); err != nil {
		t.Fatalf("update: %v", err)
	}
	// Python: `stage:     prospect  # gapped` -> `stage: won           # gapped`
	// (the run after the colon collapses to one space; `#` keeps column 21).
	want := "---\n" +
		"type: client\n" +
		"name: Edge Co\n" +
		"stage: won           # gapped\n" +
		"created: 2026-01-01\n" +
		"updated: " + today().ISO + "\n" +
		"---\n"
	if got := readFile(t, path); got != want {
		t.Fatalf("edit output\n got: %q\nwant: %q", got, want)
	}
}

// A value that overruns the comment's recorded column gets exactly one space,
// never a negative pad.
func TestInlineCommentFallsBackToOneSpace(t *testing.T) {
	ws := newWS(t, "firm-ops")
	path := filepath.Join(ws, "clients", "exact-co.md")
	mkdirAll(t, filepath.Dir(path))
	writeFile(t, path, "---\n"+
		"type: client\n"+
		"name: Exact Co\n"+
		"stage: prospect # one space\n"+
		"created: 2026-01-01\n"+
		"updated: 2026-01-01\n"+
		"---\n")

	if _, err := Update(ws, "client/exact-co",
		UpdateOpts{Fields: fields("stage", "prospecting")}); err != nil {
		t.Fatalf("update: %v", err)
	}
	want := "---\n" +
		"type: client\n" +
		"name: Exact Co\n" +
		"stage: prospecting # one space\n" +
		"created: 2026-01-01\n" +
		"updated: " + today().ISO + "\n" +
		"---\n"
	if got := readFile(t, path); got != want {
		t.Fatalf("edit output\n got: %q\nwant: %q", got, want)
	}
}

// A NEW top-level key is expressible — ruamel appends it after the last line
// too — so the comments survive that edit as well.
func TestNewTopLevelKeyKeepsComments(t *testing.T) {
	ws := newWS(t, "firm-ops")
	path := filepath.Join(ws, "clients", "grow-co.md")
	mkdirAll(t, filepath.Dir(path))
	writeFile(t, path, "---\n"+
		"type: client\n"+
		"name: Grow Co   # keep this comment\n"+
		"created: 2026-01-01\n"+
		"updated: 2026-01-01\n"+
		"---\n")

	if _, err := Update(ws, "client/grow-co",
		UpdateOpts{Fields: fields("description", "brand new")}); err != nil {
		t.Fatalf("update: %v", err)
	}
	want := "---\n" +
		"type: client\n" +
		"name: Grow Co   # keep this comment\n" +
		"created: 2026-01-01\n" +
		"updated: " + today().ISO + "\n" +
		"description: brand new\n" +
		"---\n"
	if got := readFile(t, path); got != want {
		t.Fatalf("edit output\n got: %q\nwant: %q", got, want)
	}
}

// The docs/collections-design.md promise: editing one row of a hand-maintained
// yaml collection leaves the header, the inter-row comment, the per-row inline
// comments and the trailing note in place. These bytes are Python khub's.
func TestCollectionRowEditKeepsComments(t *testing.T) {
	ws := newWS(t, "build-hub")
	path := filepath.Join(ws, "knowledge", "architecture", "repos.yaml")
	mkdirAll(t, filepath.Dir(path))
	writeFile(t, path, "# repos.yaml — hand-maintained inventory\n"+
		"# second header line\n"+
		"alpha:\n  type: repo\n  repo: acme/alpha   # the main service\n"+
		"  status: active\n  created: 2026-01-01\n  updated: 2026-01-01\n"+
		"# between rows\n"+
		"beta:\n  type: repo\n  repo: acme/beta\n"+
		"  status: active   # keep an eye on this\n"+
		"  created: 2026-01-01\n  updated: 2026-01-01\n"+
		"# trailing note\n")

	if _, err := Update(ws, "repo/alpha",
		UpdateOpts{Fields: fields("status", "archived")}); err != nil {
		t.Fatalf("update: %v", err)
	}
	want := "# repos.yaml — hand-maintained inventory\n" +
		"# second header line\n" +
		"alpha:\n  type: repo\n  repo: acme/alpha   # the main service\n" +
		"  status: archived\n  created: 2026-01-01\n  updated: " + today().ISO + "\n" +
		"# between rows\n" +
		"beta:\n  type: repo\n  repo: acme/beta\n" +
		"  status: active   # keep an eye on this\n" +
		"  created: 2026-01-01\n  updated: 2026-01-01\n" +
		"# trailing note\n"
	if got := readFile(t, path); got != want {
		t.Fatalf("collection edit\n got: %q\nwant: %q", got, want)
	}
}

// The fallback, end to end: an edit whose shape a splice cannot express still
// writes a correct document — it just loses the comments, which is the trade
// canon/splice.go documents. Clearing the body deletes a key, and a deletion
// reorders the mapping, so this document re-emits whole.
func TestUnspliceableEditStillWritesCorrectly(t *testing.T) {
	ws := newWS(t, "build-hub")
	path := filepath.Join(ws, "knowledge", "architecture", "contracts", "orders-api.yaml")
	mkdirAll(t, filepath.Dir(path))
	writeFile(t, path, "type: contract\ntitle: Orders API  # keep this comment\n"+
		"kind: api\nstatus: active\nprovider: checkout\n"+
		"created: 2026-01-01\nupdated: 2026-01-01\nbody: policy prose\n")
	seed(t, ws, "knowledge/architecture/components/checkout.md",
		kv("type", "component", "title", "Checkout", "kind", "service"))

	empty := ""
	if _, err := Update(ws, "contract/orders-api", UpdateOpts{Body: &empty}); err != nil {
		t.Fatalf("update: %v", err)
	}
	after := readFile(t, path)
	if strings.Contains(after, "body:") {
		t.Fatalf("--body '' left the reserved key:\n%s", after)
	}
	if strings.Contains(after, "# keep this comment") {
		t.Fatalf("a deletion is not spliceable; the fallback should have re-emitted:\n%s", after)
	}
	if got, _ := readMeta(t, path).Get("title"); got != "Orders API" {
		t.Fatalf("title = %#v after the fallback write", got)
	}
}
