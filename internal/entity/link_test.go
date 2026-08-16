package entity

// Port of the library-level assertions in tests/test_entity_link.py
// (TS-ENT-004 — Link and Unlink Relations).

import (
	"path/filepath"
	"testing"
)

func linkPrereqs(t *testing.T, ws string) {
	t.Helper()
	seed(t, ws, "clients/initech.md", kv("type", "client", "name", "Initech"))
	seed(t, ws, "identity/team/noor.md", kv("type", "person", "name", "Noor", "role", "partner"))
	seed(t, ws, "identity/team/dana.md", kv("type", "person", "name", "Dana", "role", "consultant"))
	seed(t, ws, "partnerships/northwind/_index.md",
		kv("type", "partnership", "partner", "Northwind", "owner", "noor"))
}

func project(t *testing.T, ws string, over ...any) string {
	t.Helper()
	meta := kv(
		"type", "project", "created", "2026-01-01", "updated", "2026-01-01",
		"draft", false, "client", "initech", "owner", "noor",
	)
	for i := 0; i+1 < len(over); i += 2 {
		meta.Set(over[i].(string), over[i+1])
	}
	seed(t, ws, "projects/initech-pov/_index.md", meta)
	return filepath.Join(ws, "projects", "initech-pov", "_index.md")
}

// TS-ENT-004-U01: an undeclared predicate for the source type is rejected.
func TestPredicateLegality(t *testing.T) {
	ws := newWS(t, "firm-ops")
	linkPrereqs(t, ws)
	project(t, ws)
	_, err := Link(ws, "initech-pov", "engagement", "anything")
	e := requireCode(t, err, "illegal_predicate")
	requireMessageContains(t, e, "Predicate 'engagement' is not legal for type 'project'")
}

// TS-ENT-004-U02: a single-valued predicate refuses a second value.
func TestCardinalityEnforced(t *testing.T) {
	ws := newWS(t, "firm-ops")
	linkPrereqs(t, ws)
	project(t, ws) // owner already noor
	_, err := Link(ws, "initech-pov", "owner", "dana")
	e := requireCode(t, err, "cardinality_violation")
	requireMessageContains(t, e, "Predicate 'owner' is single-valued")
	requireMessageContains(t, e, "`khub edit <id> owner <target>` replaces the current value")
}

// TS-ENT-004-U03: the forward edge lands on the source only, never the target.
func TestSingleSidedStorage(t *testing.T) {
	ws := newWS(t, "firm-ops")
	linkPrereqs(t, ws)
	source := project(t, ws)
	target := filepath.Join(ws, "partnerships", "northwind", "_index.md")
	beforeTarget := readFile(t, target)

	_, err := Link(ws, "initech-pov", "partner", "northwind")
	requireNoError(t, err)
	if got := metaValue(t, source, "partner"); got != "northwind" {
		t.Fatalf("partner = %#v", got)
	}
	if readFile(t, target) != beforeTarget {
		t.Fatal("the target file was rewritten")
	}
}

// TS-ENT-004-U04: an unresolvable target is rejected.
func TestTargetResolver(t *testing.T) {
	ws := newWS(t, "firm-ops")
	linkPrereqs(t, ws)
	project(t, ws)
	_, err := Link(ws, "initech-pov", "partner", "ghost")
	requireCode(t, err, "referential_integrity")

	_, err = Link(ws, "initech-pov", "owner", "ghost")
	e := requireCode(t, err, "referential_integrity")
	requireMessageContains(t, e, "No person 'ghost' to satisfy predicate 'owner'")
}

// TS-ENT-004-U05: unlink drops the forward edge (derived inverses recompute).
func TestUnlinkRemovesForwardEdge(t *testing.T) {
	ws := newWS(t, "firm-ops")
	linkPrereqs(t, ws)
	source := project(t, ws, "partner", "northwind")
	res, err := Unlink(ws, "initech-pov", "partner", "northwind")
	requireNoError(t, err)
	if !res.Changed {
		t.Fatal("unlink reported no change")
	}
	if hasKey(t, source, "partner") {
		t.Fatal("the forward edge survived")
	}
}

// A qualified target stores its qualified spelling; a bare one stores the
// canonical slug (never the caller's casing).
func TestLinkStoresTheCanonicalSpelling(t *testing.T) {
	ws := newWS(t, "firm-ops")
	linkPrereqs(t, ws)
	source := project(t, ws)

	res, err := Link(ws, "initech-pov", "partner", "NORTHWIND")
	requireNoError(t, err)
	if res.Target != "northwind" {
		t.Fatalf("stored target = %q, want the canonical slug", res.Target)
	}
	if got := metaValue(t, source, "partner"); got != "northwind" {
		t.Fatalf("partner = %#v", got)
	}

	qualified, err := Link(ws, "initech-pov", "related", "Partnership/Northwind")
	requireNoError(t, err)
	if qualified.Target != "partnership/northwind" {
		t.Fatalf("qualified target = %q", qualified.Target)
	}
}
