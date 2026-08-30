package schema

// Ports tests/test_resolve_targets.py — STORY-SCH-001: target resolution,
// delta-only inheritance, and the real presets (TS-SCH-001-03/04, U05/U06);
// plus Go-side pins for the declaration-order contract, cross-file merge, and
// the duplicate-key hard error.

import (
	"strings"
	"testing"
)

func TestUnknownRelationTargetLocatedError(t *testing.T) {
	// TS-SCH-001-03 / U06 / SCH-003: a relation targeting an unknown type is
	// rejected with a located error carrying type/relation/target.
	preset := `
ontology:
  entities:
    project:
      relations:
        owner: { to: persn }
`
	e := resolveLocatedWithCore(t, preset)
	if e.Code != "unknown_relation_target" {
		t.Errorf("code = %q, want unknown_relation_target", e.Code)
	}
	if e.Type != "project" || e.Relation != "owner" || e.Target != "persn" {
		t.Errorf("located fields = (%q, %q, %q)", e.Type, e.Relation, e.Target)
	}
	want := "Type 'project' relation 'owner' targets unknown type 'persn'"
	if e.Message != want {
		t.Errorf("message = %q, want %q", e.Message, want)
	}
}

func TestDeltaOnlyInheritance(t *testing.T) {
	// TS-SCH-001-04 / U05: a type declares only its domain delta and inherits
	// the base block (type, draft, created, updated, tags, OKF fields).
	preset := `
ontology:
  entities:
    client:
      attributes:
        name:     { required: true }
        industry: {}
`
	c := typeOf(t, resolveWithCore(t, preset), "client")
	// domain delta
	if !c.Attributes.Has("name") {
		t.Error("client missing declared attr 'name'")
	}
	if !c.Attributes.Has("industry") {
		t.Error("client missing declared attr 'industry'")
	}
	// inherited base, not redeclared by client
	for _, a := range []string{"type", "draft", "created", "updated", "tags", "title", "description", "resource"} {
		if !c.Attributes.Has(a) {
			t.Errorf("client missing inherited base attr %s", a)
		}
	}
}

func TestAuthoredPresetsResolve(t *testing.T) {
	// Integration: the authored core base + firm-ops layer files resolve to 9
	// types, with the post-review model (union engagement minus build;
	// project.active).
	schema, err := ResolveWith(corePresetDoc(t), presetPaths(t, "firm-ops"))
	if err != nil {
		t.Fatalf("presets failed to resolve: %v", err)
	}
	if schema.Types.Len() != 9 {
		t.Errorf("types = %d (%v), want 9", schema.Types.Len(), schema.Types.Keys())
	}

	eng := relOf(t, typeOf(t, schema, "meeting"), "engagement")
	if eng.Kind != KindUnion {
		t.Errorf("engagement.Kind = %q, want union", eng.Kind)
	}
	if !eqStrings(eng.Targets, []string{"opportunity", "project", "partnership"}) {
		t.Errorf("engagement.Targets = %v (authored order is contract)", eng.Targets)
	}

	project := typeOf(t, schema, "project")
	if !project.Attributes.Has("active") {
		t.Error("project missing attr 'active'")
	}
	if project.Attributes.Has("stage") {
		t.Error("project carries attr 'stage'; the post-review model dropped it")
	}
	// base merged in
	if !project.Attributes.Has("draft") {
		t.Error("project missing base attr 'draft'")
	}

	// Iteration order = declaration order (the file's authored type order).
	wantOrder := []string{
		"opportunity", "project", "meeting", "transcript", "fragment",
		"case-study", "partnership", "person", "client",
	}
	if !eqStrings(schema.Types.Keys(), wantOrder) {
		t.Errorf("type order = %v, want %v", schema.Types.Keys(), wantOrder)
	}
}

func TestCrossFileEntityMergeLastWinsFirstPosition(t *testing.T) {
	// resolve.py merges each file's entities into one dict: a redeclared name
	// takes the later declaration (last wins) but keeps its first position —
	// Python dict-update semantics.
	first := `
ontology:
  entities:
    a:
      attributes:
        kind: { type: text }
    b: {}
`
	second := `
ontology:
  entities:
    a:
      attributes:
        kind: { type: number }
`
	schema := resolveDocs(t, first, second)
	if got := attrOf(t, typeOf(t, schema, "a"), "kind").BaseType; got != "number" {
		t.Errorf("a.kind = %q, want number (last declaration wins)", got)
	}
	if !eqStrings(schema.Types.Keys(), []string{"a", "b"}) {
		t.Errorf("type order = %v, want [a b] (first position kept)", schema.Types.Keys())
	}
}

func TestDuplicateTypeKeyIsAnError(t *testing.T) {
	// A duplicate mapping key inside one file is a hard load error (ruamel
	// DuplicateKeyError; goccy's default duplicate-key rejection) — never
	// last-wins.
	doc := `
ontology:
  entities:
    a: { when: first }
    a: { when: second }
`
	_, err := ResolveWith(nil, writeSchemaFiles(t, doc))
	if err == nil {
		t.Fatal("Resolve succeeded; want a duplicate-key error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "duplicate") {
		t.Errorf("error %q does not name the duplicate key", err)
	}
}
