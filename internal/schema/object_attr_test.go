package schema

import "testing"

const objectOntology = `
ontology:
  entities:
    note:
      attributes:
        generated:
          type: object
          fields:
            by: { required: true }
            at: { type: datetime, pattern: '.*Z' }
        verified:
          type: list
          items:
            type: object
            fields:
              by: { required: true }
              rev: {}
        scores: { type: list, items: { type: number } }
`

func TestObjectFieldsAndListItemsResolve(t *testing.T) {
	note := typeOf(t, resolveWithCore(t, objectOntology), "note")

	gen := attrOf(t, note, "generated")
	if gen.BaseType != "object" || gen.Fields == nil || !eqStrings(gen.Fields.Keys(), []string{"by", "at"}) {
		t.Fatalf("generated = %s fields %v", gen.BaseType, gen.Fields)
	}
	by, _ := gen.Fields.Get("by")
	at, _ := gen.Fields.Get("at")
	if !by.Required || by.BaseType != "text" || at.BaseType != "datetime" || !at.MatchPattern("2026-01-01T00:00:00Z") {
		t.Errorf("generated fields: by=%+v at=%+v", by, at)
	}

	ver := attrOf(t, note, "verified")
	if ver.Items == nil || ver.Items.BaseType != "object" || ver.Items.Name != "verified" ||
		!eqStrings(ver.Items.Fields.Keys(), []string{"by", "rev"}) {
		t.Fatalf("verified.items = %+v", ver.Items)
	}
	if !ver.HoldsObjects() || !gen.HoldsObjects() || attrOf(t, note, "scores").HoldsObjects() {
		t.Error("HoldsObjects: want generated and verified only")
	}
	if attrOf(t, note, "title").Fields != nil || attrOf(t, note, "tags").Items != nil {
		t.Error("an attribute that nests nothing must carry nil Fields and Items")
	}
}

func TestFieldsAndItemsOnTheWrongType(t *testing.T) {
	cases := []struct{ doc, want string }{
		{
			"ontology:\n  entities:\n    a:\n      attributes:\n        g: { fields: { by: {} } }\n",
			"Invalid schema at ontology.entities.a.attributes.g.fields: Value error, " +
				"'fields' applies only to type: object; declare type: object or drop the key",
		},
		{
			"ontology:\n  entities:\n    a:\n      attributes:\n        g: { type: object, items: { type: text } }\n",
			"Invalid schema at ontology.entities.a.attributes.g.items: Value error, " +
				"'items' applies only to type: list; declare type: list or drop the key",
		},
		{
			"ontology:\n  entities:\n    a:\n      attributes:\n        g: { type: list, items: { type: object, fields: { x: { items: {} } } } }\n",
			"Invalid schema at ontology.entities.a.attributes.g.items.fields.x.items: Value error, " +
				"'items' applies only to type: list; declare type: list or drop the key",
		},
		{
			"ontology:\n  entities:\n    a:\n      attributes:\n        g: { type: object, fields: { x: { pattern: '(' } } }\n",
			"Invalid pattern at ontology.entities.a.attributes.g.fields.x: " +
				"error parsing regexp: missing closing ): `\\A(?:()\\z`",
		},
		{
			"ontology:\n  entities:\n    a:\n      attributes:\n        g: { type: object, fields: { x: { kind: 1 } } }\n",
			"Unknown construct 'kind' (not khub vocabulary) at ontology.entities.a.attributes.g.fields.x.kind",
		},
	}
	for _, c := range cases {
		if got := resolveLocatedWithCore(t, c.doc).Message; got != c.want {
			t.Errorf("message = %q\nwant      %q", got, c.want)
		}
	}
}

func TestOverrideReplacesFieldsWhole(t *testing.T) {
	base := loadYAMLDoc(t, `
ontology:
  base:
    attributes:
      type: { type: text, required: true }
      generated: { type: object, fields: { by: { required: true }, at: {} } }
      verified: { type: list, items: { type: object, fields: { by: {} } } }
`)
	files := writeSchemaFiles(t, `
ontology:
  entities:
    keep:
      attributes:
        generated: { required: true }
    swap:
      attributes:
        generated: { fields: { who: {} } }
        verified: { items: { type: text } }
    flat:
      attributes:
        generated: { type: text }
`)
	s, err := ResolveWith(base, files)
	if err != nil {
		t.Fatal(err)
	}
	keep := attrOf(t, typeOf(t, s, "keep"), "generated")
	if !keep.Required || !eqStrings(keep.Fields.Keys(), []string{"by", "at"}) {
		t.Errorf("a facet override keeps the base's fields: %v", keep.Fields.Keys())
	}
	swap := typeOf(t, s, "swap")
	if got := attrOf(t, swap, "generated").Fields.Keys(); !eqStrings(got, []string{"who"}) {
		t.Errorf("redeclared fields = %v, want [who] (whole replace)", got)
	}
	if it := attrOf(t, swap, "verified").Items; it.BaseType != "text" || it.Fields != nil {
		t.Errorf("redeclared items = %+v, want a bare text element", it)
	}
	if flat := attrOf(t, typeOf(t, s, "flat"), "generated"); flat.BaseType != "text" || flat.Fields != nil {
		t.Errorf("retyped override = %s with fields %v, want text without fields", flat.BaseType, flat.Fields)
	}
}

func TestAttributeAndRelationMayNotShareAName(t *testing.T) {
	doc := "ontology:\n  entities:\n    a:\n      attributes:\n        related: { type: text }\n"
	want := "Invalid schema at ontology.entities.a: 'related' is declared as both an attribute " +
		"and a relation; rename one"
	le := resolveLocatedWithCore(t, doc)
	if le.Code != "invalid_schema" || le.Message != want {
		t.Errorf("got %s %q\nwant invalid_schema %q", le.Code, le.Message, want)
	}
}

func TestNavigationFilesAreNeverEntities(t *testing.T) {
	note := typeOf(t, resolveWithCore(t, "ontology:\n  entities:\n    note: {}\n"), "note")
	for rel, want := range map[string]bool{
		"note/a.md": true, "note/index.md": false, "note/log.md": false, "note/_index.md": false,
	} {
		if got := note.AcceptsEntityPath(rel); got != want {
			t.Errorf("AcceptsEntityPath(%s) = %v, want %v", rel, got, want)
		}
	}
}
