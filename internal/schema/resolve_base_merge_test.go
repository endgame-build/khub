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
ontology:
  entities:
    client: {}
    project: {}
`
	schema := resolveWithCore(t, preset)
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
ontology:
  entities:
    a:
      attributes:
        updated: { required: true }
    b: {}
`
	schema := resolveWithCore(t, preset)
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
	schema := resolveWithCore(t, "ontology: { entities: { a: {} } }")
	a := typeOf(t, schema, "a")
	if !a.Attributes.Has("type") {
		t.Error("a missing attribute 'type'")
	}
	if attrOf(t, a, "draft").BaseType != "bool" {
		t.Errorf("draft.BaseType = %q, want bool", attrOf(t, a, "draft").BaseType)
	}
}

func TestEmbeddedBaseIsTheOnlyBaseSource(t *testing.T) {
	// The base block is embedded in the binary and supplied by the caller as
	// ResolveWith's base document — the ONLY legal source. An authored
	// ontology.base is rejected with an error teaching the real override path
	// (redeclare the attribute on the type); the base is khub's own plumbing,
	// and redefining it whole would silently change what every gate reads.
	base := loadYAMLDoc(t, coreBase)

	paths := writeSchemaFiles(t, "ontology: { entities: { a: {} } }")
	withBase, err := ResolveWith(base, paths)
	if err != nil {
		t.Fatalf("ResolveWith: %v", err)
	}
	if !typeOf(t, withBase, "a").Attributes.Has("created") {
		t.Error("the embedded base was not merged")
	}

	declared := `
ontology:
  base:
    attributes:
      type: { type: text, required: true }
  entities:
    a: {}
`
	wantMsg := "Invalid schema at ontology.base: the base block is khub-owned; " +
		"override a base attribute by redeclaring it on the type (see `khub schema base`)"
	for name, doc := range map[string]string{
		"declared": declared,
		// Even an EMPTY authored base is the author reaching for the block.
		"empty": "ontology:\n  base: {}\n  entities:\n    a: {}\n",
	} {
		_, err = ResolveWith(base, writeSchemaFiles(t, doc))
		le := asLocatedErr(t, err)
		if le.Code != "invalid_schema" {
			t.Errorf("%s base: code = %q, want invalid_schema", name, le.Code)
		}
		if le.Message != wantMsg {
			t.Errorf("%s base: message = %q,\nwant %q", name, le.Message, wantMsg)
		}
	}

	// A SUPPLIED document that yields no base is a loud error, never a silent
	// empty base: the caller passing one is promising khub's plumbing, and a
	// mis-nested embedded document must fail here rather than let every gate
	// quietly change meaning.
	_, err = ResolveWith(loadYAMLDoc(t, "ontology:\n  entities: {}\n"),
		writeSchemaFiles(t, "ontology: { entities: { a: {} } }"))
	yieldErr := asLocatedErr(t, err)
	if yieldErr.Code != "schema_error" ||
		yieldErr.Message != "Base document carries no ontology.base block" {
		t.Errorf("base-less base doc: %s (%s)", yieldErr.Code, yieldErr.Message)
	}

	// No base document at all, no authored base: entities resolve against an
	// empty base — a nil base document is the base-less fixture path.
	bare, err := ResolveWith(nil, writeSchemaFiles(t, "ontology: { entities: { a: {} } }"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if bare.BaseAttributes.Len() != 0 {
		t.Errorf("bare base attrs = %v, want none", bare.BaseAttributes.Keys())
	}
}

func TestUniversalEdgesWithoutRedeclaration(t *testing.T) {
	// TS-SCH-002-04 / U05: the universal edges are available on every type
	// without that type redeclaring the predicate.
	preset := `
ontology:
  entities:
    a: {}
    b: {}
`
	schema := resolveWithCore(t, preset)
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
ontology:
  entities:
    meeting:
      attributes:
        created: { required: false }
    client:
      attributes:
        created: {}
`
	schema := resolveWithCore(t, preset)
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
