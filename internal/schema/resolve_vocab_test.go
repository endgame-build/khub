package schema

// Ports tests/test_resolve_vocab.py — STORY-SCH-001: declare a type in khub
// vocabulary (vocabulary parsing). TS-SCH-001-01/02, U01–U04.

import "testing"

func TestWellFormedTypeResolves(t *testing.T) {
	// TS-SCH-001-01 / U01 / U02: attributes, enums, typed relations, storage
	// resolve; each relation's field name is its predicate.
	preset := `
entities:
  client:  { layout: file }
  person:  { layout: file }
  project:
    layout: folder
    attributes:
      stage: { enum: [diagnose, prove, scale, complete], required: true }
    relations:
      client: { to: client, required: true }
      owner:  { to: person, required: true }
`
	schema := resolveDocs(t, coreBase, preset)
	proj := typeOf(t, schema, "project")

	// attributes accepted as scalars and enums
	stage := attrOf(t, proj, "stage")
	if !eqStrings(stage.Enum, []string{"diagnose", "prove", "scale", "complete"}) {
		t.Errorf("stage.Enum = %v", stage.Enum)
	}
	if !stage.Required {
		t.Error("stage.Required = false, want true")
	}

	// storage config accepted
	if proj.Storage.Layout != "folder" {
		t.Errorf("layout = %q, want folder", proj.Storage.Layout)
	}

	// relations accepted as typed edges with a `to:` target and cardinality;
	// the field name IS the predicate
	client := relOf(t, proj, "client")
	if client.Predicate != "client" {
		t.Errorf("client.Predicate = %q", client.Predicate)
	}
	if !eqStrings(client.Targets, []string{"client"}) {
		t.Errorf("client.Targets = %v", client.Targets)
	}
	if client.Kind != KindTyped {
		t.Errorf("client.Kind = %q, want typed", client.Kind)
	}
	if !client.Required {
		t.Error("client.Required = false, want true")
	}
	if owner := relOf(t, proj, "owner"); !eqStrings(owner.Targets, []string{"person"}) {
		t.Errorf("owner.Targets = %v", owner.Targets)
	}
}

func TestRelationTargetSingleListOrAny(t *testing.T) {
	// U03 / SCH-003: a `to:` target may be a single type, a list (union), or `any`.
	preset := `
entities:
  a: { layout: file }
  b: { layout: file }
  meeting:
    layout: file
    relations:
      one:        { to: a }
      engagement: { to: [a, b], required: true }
      anything:   { to: any }
`
	m := typeOf(t, resolveDocs(t, coreBase, preset), "meeting")
	one := relOf(t, m, "one")
	if one.Kind != KindTyped || !eqStrings(one.Targets, []string{"a"}) {
		t.Errorf("one = %q %v", one.Kind, one.Targets)
	}
	engagement := relOf(t, m, "engagement")
	if engagement.Kind != KindUnion {
		t.Errorf("engagement.Kind = %q, want union", engagement.Kind)
	}
	if !eqStrings(engagement.Targets, []string{"a", "b"}) {
		t.Errorf("engagement.Targets = %v", engagement.Targets)
	}
	if anything := relOf(t, m, "anything"); anything.Kind != KindAny {
		t.Errorf("anything.Kind = %q, want any", anything.Kind)
	}
}

func TestConstraintsCaptured(t *testing.T) {
	// TS-SCH-001-02 (resolver capture): enum, pattern, and `many` cardinality
	// are carried on the resolved model.
	preset := `
entities:
  person: { layout: file }
  thing:
    layout: file
    attributes:
      airtable_id: { type: text, pattern: '^rec[A-Za-z0-9]+$' }
    relations:
      team: { to: person, many: true }
`
	thing := typeOf(t, resolveDocs(t, coreBase, preset), "thing")
	pat := attrOf(t, thing, "airtable_id").Pattern
	if pat == nil || *pat != "^rec[A-Za-z0-9]+$" {
		t.Errorf("airtable_id.Pattern = %v", pat)
	}
	if !relOf(t, thing, "team").Many {
		t.Error("team.Many = false, want true")
	}
}

func TestRejectSmuggledRawLinkML(t *testing.T) {
	// U04 / SCH-001: a declaration using a raw LinkML construct (not khub
	// vocab) is rejected with a located error.
	preset := `
entities:
  thing:
    layout: file
    attributes:
      name: { range: string }
`
	e := resolveLocated(t, coreBase, preset)
	if e.Code != "raw_linkml_smuggled" {
		t.Errorf("code = %q, want raw_linkml_smuggled", e.Code)
	}
	want := "Unknown construct 'range' (not khub vocabulary) at entities.thing.attributes.name.range"
	if e.Message != want {
		t.Errorf("message = %q, want %q", e.Message, want)
	}
	if e.Type != "thing" || e.Target != "range" {
		t.Errorf("located fields = (%q, %q), want (thing, range)", e.Type, e.Target)
	}
}

func TestOrphanFlagParsesOnAnyLayout(t *testing.T) {
	// `orphan: true` declares that edge-less is a type's expected state. It
	// defaults to false and, unlike `required`, is NOT singleton-only.
	preset := `
entities:
  charter:
    layout: singleton
    path: charter.md
    orphan: true
  note:
    layout: file
    orphan: true
  client:
    layout: file
`
	schema := resolveDocs(t, coreBase, preset)
	if !typeOf(t, schema, "charter").Orphan {
		t.Error("charter.Orphan = false, want true")
	}
	if !typeOf(t, schema, "note").Orphan {
		t.Error("note.Orphan = false, want true")
	}
	if typeOf(t, schema, "client").Orphan {
		t.Error("client.Orphan = true, want false (default: swept like anything else)")
	}
}
