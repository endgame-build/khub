package schema

// Ports tests/test_resolve_base_merge.py — STORY-SCH-002: merge the base block
// and override. TS-SCH-002-01..04, U01–U05, plus a tri-state pin (`required:
// false` beats an inherited true — the *bool "field was authored" contract).

import "testing"

var baseAttrs = []string{
	"type", "draft", "author", "created", "updated", "title", "description", "resource", "tags",
}

var universal = []string{"related", "sources", "references", "depends_on"}

func TestMergeBaseIntoEveryEntity(t *testing.T) {
	// TS-SCH-002-01 / U01: base attributes and the universal any->any edges
	// are merged into every type; draft defaults to false; merge is at
	// resolve time.
	preset := `
entities:
  client:  { layout: file }
  project: { layout: folder }
`
	schema := resolveDocs(t, coreBase, preset)
	for _, name := range []string{"client", "project"} {
		rt := typeOf(t, schema, name)
		for _, a := range baseAttrs {
			if !rt.Attributes.Has(a) {
				t.Errorf("%s missing base attr %s", name, a)
			}
		}
		draft := attrOf(t, rt, "draft")
		if draft.BaseType != "bool" {
			t.Errorf("%s draft.BaseType = %q, want bool", name, draft.BaseType)
		}
		if draft.Default != false {
			t.Errorf("%s draft.Default = %v, want false", name, draft.Default)
		}
		for _, r := range universal {
			rel := relOf(t, rt, r)
			if rel.Kind != KindAny {
				t.Errorf("%s %s.Kind = %q, want any", name, r, rel.Kind)
			}
			if !rel.Many {
				t.Errorf("%s %s.Many = false, want true", name, r)
			}
		}
	}
}

func TestOverrideBaseAttribute(t *testing.T) {
	// TS-SCH-002-02 / U02: a type overrides a base attribute by redeclaring
	// it; siblings stay inherited unchanged.
	preset := `
entities:
  a:
    layout: file
    attributes:
      updated: { required: true }
  b: { layout: file }
`
	schema := resolveDocs(t, coreBase, preset)
	aUpdated := attrOf(t, typeOf(t, schema, "a"), "updated")
	if !aUpdated.Required {
		t.Error("a.updated.Required = false, want true")
	}
	if !aUpdated.OverriddenFromBase {
		t.Error("a.updated.OverriddenFromBase = false, want true")
	}
	// sibling keeps the base default (optional), unchanged
	bUpdated := attrOf(t, typeOf(t, schema, "b"), "updated")
	if bUpdated.Required {
		t.Error("b.updated.Required = true, want false")
	}
	if bUpdated.OverriddenFromBase {
		t.Error("b.updated.OverriddenFromBase = true, want false")
	}
}

func TestDraftAndTypeGuaranteed(t *testing.T) {
	// U03 / SCH-006: every entity carries `type` and the boolean `draft` flag.
	schema := resolveDocs(t, coreBase, "entities: { a: { layout: file } }")
	a := typeOf(t, schema, "a")
	if !a.Attributes.Has("type") {
		t.Error("a missing attribute 'type'")
	}
	if attrOf(t, a, "draft").BaseType != "bool" {
		t.Errorf("draft.BaseType = %q, want bool", attrOf(t, a, "draft").BaseType)
	}
}

func TestMissingBaseBlockRejected(t *testing.T) {
	// TS-SCH-002-03 / U04: entities with no base block present is rejected.
	e := resolveLocated(t, "entities: { a: { layout: file } }")
	if e.Code != "missing_base" {
		t.Errorf("code = %q, want missing_base", e.Code)
	}
	want := "Schema declares entities but no base block; base attributes are missing"
	if e.Message != want {
		t.Errorf("message = %q, want %q", e.Message, want)
	}
}

func TestUniversalEdgesWithoutRedeclaration(t *testing.T) {
	// TS-SCH-002-04 / U05: the universal edges are available on every type
	// without that type redeclaring the predicate.
	preset := `
entities:
  a: { layout: file }
  b: { layout: file }
`
	schema := resolveDocs(t, coreBase, preset)
	if !typeOf(t, schema, "a").Relations.Has("related") {
		t.Error("a missing universal edge 'related'")
	}
	if !typeOf(t, schema, "b").Relations.Has("depends_on") {
		t.Error("b missing universal edge 'depends_on'")
	}
}

func TestRequiredFalseOverrideBeatsInheritedTrue(t *testing.T) {
	// The tri-state contract (AttrDecl.Required *bool): an explicit
	// `required: false` wins over the base's true, while an override that
	// stays silent inherits it. firm-ops relies on this for meeting.created.
	preset := `
entities:
  meeting:
    layout: file
    attributes:
      created: { required: false }
  client:
    layout: file
    attributes:
      created: {}
`
	schema := resolveDocs(t, coreBase, preset)
	meeting := attrOf(t, typeOf(t, schema, "meeting"), "created")
	if meeting.Required {
		t.Error("meeting.created.Required = true; explicit false must beat inherited true")
	}
	if !meeting.OverriddenFromBase {
		t.Error("meeting.created.OverriddenFromBase = false, want true")
	}
	if meeting.BaseType != "date" {
		t.Errorf("meeting.created.BaseType = %q, want date (facet inherited)", meeting.BaseType)
	}
	client := attrOf(t, typeOf(t, schema, "client"), "created")
	if !client.Required {
		t.Error("client.created.Required = false; an empty override must inherit true")
	}
}
