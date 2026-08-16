package entity

// Port of the library-level assertions in tests/test_entity_remove.py
// (TS-ENT-005 — Remove an Entity).

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/introspect"
)

// referencedClient seeds a client/initech with three inbound `client` edges.
func referencedClient(t *testing.T, ws string) {
	t.Helper()
	seed(t, ws, "clients/initech.md", kv("type", "client", "name", "Initech"))
	seed(t, ws, "opportunities/o1/_index.md",
		kv("type", "opportunity", "stage", "prospect", "client", "initech"))
	seed(t, ws, "opportunities/o2/_index.md",
		kv("type", "opportunity", "stage", "won", "client", "initech"))
	seed(t, ws, "projects/p1/_index.md", kv("type", "project", "client", "initech"))
}

// TS-ENT-005-U01: every edge resolving to the target is found.
func TestInboundEdgeDetector(t *testing.T) {
	ws := newWS(t, "firm-ops")
	referencedClient(t, ws)
	resolved, err := introspect.LoadSchema(ws)
	requireNoError(t, err)
	idx, err := index.Build(ws, resolved)
	requireNoError(t, err)

	inbound := inboundEdges(idx, resolved, index.Node{Type: "client", Slug: "initech"})
	if len(inbound) != 3 {
		t.Fatalf("inbound = %d, want 3 (%v)", len(inbound), inbound)
	}
	for _, e := range inbound {
		if e.Predicate != "client" {
			t.Fatalf("predicate = %q", e.Predicate)
		}
	}
}

// TS-ENT-005-U02: refused while inbound edges resolve, unless --force.
func TestRemovalGuard(t *testing.T) {
	ws := newWS(t, "firm-ops")
	referencedClient(t, ws)

	refused, err := Delete(ws, "initech", false)
	requireNoError(t, err)
	if refused.Removed || len(refused.Inbound) != 3 {
		t.Fatalf("removed=%v inbound=%d", refused.Removed, len(refused.Inbound))
	}
	if _, statErr := os.Stat(filepath.Join(ws, "clients", "initech.md")); statErr != nil {
		t.Fatal("a refused remove deleted the file")
	}
	forced, err := Delete(ws, "initech", true)
	requireNoError(t, err)
	if !forced.Removed {
		t.Fatal("--force did not remove")
	}
}

// TS-ENT-005-U03: a folder-layout entity deletes its folder; a flat one its file.
func TestFolderAndFlatDeletion(t *testing.T) {
	ws := newWS(t, "firm-ops")
	seed(t, ws, "projects/lonely/_index.md", kv("type", "project", "client", "x", "owner", "y"))
	seed(t, ws, "fragments/note.md", kv("type", "fragment", "stage", "raw", "owner", "y"))

	_, err := Delete(ws, "lonely", false)
	requireNoError(t, err)
	_, err = Delete(ws, "note", false)
	requireNoError(t, err)

	if _, statErr := os.Stat(filepath.Join(ws, "projects", "lonely")); statErr == nil {
		t.Fatal("the folder-layout entity kept its folder")
	}
	if _, statErr := os.Stat(filepath.Join(ws, "fragments", "note.md")); statErr == nil {
		t.Fatal("the flat entity kept its file")
	}
}

// TS-ENT-005-U04: --force deletes and leaves the inbound edges dangling on disk.
func TestForceLeavesDanglingEdges(t *testing.T) {
	ws := newWS(t, "firm-ops")
	referencedClient(t, ws)

	res, err := Delete(ws, "initech", true)
	requireNoError(t, err)
	if !res.Removed || len(res.Inbound) != 3 {
		t.Fatalf("removed=%v inbound=%d", res.Removed, len(res.Inbound))
	}
	if _, statErr := os.Stat(filepath.Join(ws, "clients", "initech.md")); statErr == nil {
		t.Fatal("the entity survived --force")
	}
	// The dangling references survive untouched — check surfaces them.
	for _, rel := range []string{
		"opportunities/o1/_index.md", "opportunities/o2/_index.md", "projects/p1/_index.md",
	} {
		if got := metaValue(t, filepath.Join(ws, filepath.FromSlash(rel)), "client"); got != "initech" {
			t.Fatalf("%s client = %#v, want the dangling value", rel, got)
		}
	}
}

// TS-ENT-005-U05: an unresolvable id raises a lookup error.
func TestLookupErrorForMissing(t *testing.T) {
	ws := newWS(t, "firm-ops")
	_, err := Delete(ws, "ghost", false)
	e := requireCode(t, err, "lookup_error")
	requireMessageContains(t, e, "No entity 'ghost' found")
}

// A singleton's path IS the file: delete used to append the slug to it
// (prd.md/prd.md) and raise NotADirectoryError, leaving the entity on disk.
func TestRemoveSingletonDeletesItsOneFile(t *testing.T) {
	ws := newWS(t, "build-hub")
	created, err := Create(ws, "prd", CreateOpts{Fields: fields("title", "PRD"), UseTemplate: true})
	requireNoError(t, err)
	if _, statErr := os.Stat(created.Path); statErr != nil {
		t.Fatalf("singleton not written: %v", statErr)
	}
	res, err := Delete(ws, "prd", false)
	requireNoError(t, err)
	if !res.Removed {
		t.Fatal("the singleton was not removed")
	}
	if _, statErr := os.Stat(created.Path); statErr == nil {
		t.Fatal("the singleton file survived")
	}
}
