package schema

// Differential pins for the pydantic-emulating vocabulary walk. Every expected
// string below was captured from the Python implementation (khub 0.18.x,
// pydantic v2) on 2026-08-15 — codes, dotted locs (including the smart-union
// branch tags "constrained-str" / "IdPrefixDecl" / "str"), catalog messages,
// and error precedence (extra_forbidden anywhere beats the first value error;
// base errors precede entity errors; first entity in input order wins).

import "testing"

func TestVocabErrorParity(t *testing.T) {
	cases := []struct {
		name     string
		doc      string
		wantCode string
		wantMsg  string
	}{
		{
			name: "id_prefix pattern violation names the constrained-str branch",
			doc: matrixBase + `entities:
  a:
    layout: file
    path: as
    id_prefix: 'AD'
`,
			wantCode: "invalid_schema",
			wantMsg: "Invalid schema at entities.a.id_prefix.constrained-str: " +
				"String should match pattern '^[a-z][a-z0-9]*$'",
		},
		{
			name: "smuggled key inside id_prefix carries the union tag",
			doc: matrixBase + `entities:
  a:
    layout: file
    id_prefix: { by: kind, map: { x: y }, nope: 1 }
    attributes:
      kind: { enum: [x] }
`,
			wantCode: "raw_linkml_smuggled",
			wantMsg: "Unknown construct 'nope' (not khub vocabulary) " +
				"at entities.a.id_prefix.IdPrefixDecl.nope",
		},
		{
			name:     "layout outside the literal set",
			doc:      matrixBase + "entities:\n  a: { layout: nope }\n",
			wantCode: "invalid_schema",
			wantMsg: "Invalid schema at entities.a.layout: " +
				"Input should be 'file', 'folder', 'collection' or 'singleton'",
		},
		{
			name:     "layout is required",
			doc:      matrixBase + "entities:\n  a: { path: x }\n",
			wantCode: "invalid_schema",
			wantMsg:  "Invalid schema at entities.a.layout: Field required",
		},
		{
			name:     "non-string to reports the str union branch first",
			doc:      matrixBase + "entities:\n  a:\n    layout: file\n    relations:\n      r: { to: 42 }\n",
			wantCode: "invalid_schema",
			wantMsg:  "Invalid schema at entities.a.relations.r.to.str: Input should be a valid string",
		},
		{
			name:     "format must be a string",
			doc:      matrixBase + "entities:\n  a: { layout: file, format: 42 }\n",
			wantCode: "invalid_schema",
			wantMsg:  "Invalid schema at entities.a.format: Input should be a valid string",
		},
		{
			name:     "required rejects an uninterpretable int",
			doc:      matrixBase + "entities:\n  a: { layout: file, required: 5 }\n",
			wantCode: "invalid_schema",
			wantMsg: "Invalid schema at entities.a.required: " +
				"Input should be a valid boolean, unable to interpret input",
		},
		{
			name:     "enum must be a list",
			doc:      matrixBase + "entities:\n  a:\n    layout: file\n    attributes:\n      f: { enum: x }\n",
			wantCode: "invalid_schema",
			wantMsg:  "Invalid schema at entities.a.attributes.f.enum: Input should be a valid list",
		},
		{
			name:     "base errors precede entity errors",
			doc:      "base:\n  attributes:\n    t: { type: nope }\nentities:\n  a: { layout: nope }\n",
			wantCode: "invalid_schema",
			wantMsg: "Invalid schema at base.attributes.t.type: " +
				"Input should be 'text', 'number', 'date', 'datetime', 'bool' or 'list'",
		},
		{
			name:     "first entity in input order wins",
			doc:      matrixBase + "entities:\n  a: { layout: nope }\n  b: { layout: also }\n",
			wantCode: "invalid_schema",
			wantMsg: "Invalid schema at entities.a.layout: " +
				"Input should be 'file', 'folder', 'collection' or 'singleton'",
		},
		{
			name: "id_prefix mapping missing map reports the str branch first",
			doc: matrixBase + `entities:
  a:
    layout: file
    id_prefix: { by: kind }
    attributes:
      kind: { enum: [x] }
`,
			wantCode: "invalid_schema",
			wantMsg: "Invalid schema at entities.a.id_prefix.constrained-str: " +
				"Input should be a valid string",
		},
		{
			name:     "when must be a string",
			doc:      matrixBase + "entities:\n  a: { layout: file, when: 42 }\n",
			wantCode: "invalid_schema",
			wantMsg:  "Invalid schema at entities.a.when: Input should be a valid string",
		},
		{
			name:     "a null attribute declaration is not a model",
			doc:      matrixBase + "entities:\n  a:\n    layout: file\n    attributes:\n      f:\n",
			wantCode: "invalid_schema",
			wantMsg: "Invalid schema at entities.a.attributes.f: " +
				"Input should be a valid dictionary or instance of AttrDecl",
		},
		{
			name:     "a relation needs to",
			doc:      matrixBase + "entities:\n  a:\n    layout: file\n    relations:\n      r: { many: true }\n",
			wantCode: "invalid_schema",
			wantMsg:  "Invalid schema at entities.a.relations.r.to: Field required",
		},
		{
			name:     "a bad list item fails both union branches str-first",
			doc:      matrixBase + "entities:\n  a:\n    layout: file\n    relations:\n      r: { to: [a, 42] }\n",
			wantCode: "invalid_schema",
			wantMsg:  "Invalid schema at entities.a.relations.r.to.str: Input should be a valid string",
		},
		{
			name:     "any inside a list is a type name, not the any kind",
			doc:      matrixBase + "entities:\n  a:\n    layout: file\n    relations:\n      r: { to: [any] }\n",
			wantCode: "unknown_relation_target",
			wantMsg:  "Type 'a' relation 'r' targets unknown type 'any'",
		},
		{
			name:     "a null base is an absent base",
			doc:      "base:\nentities:\n  a: { layout: file }\n",
			wantCode: "missing_base",
			wantMsg:  "Schema declares entities but no base block; base attributes are missing",
		},
		{
			name: "extra_forbidden anywhere beats an earlier value error",
			doc: matrixBase + `entities:
  a: { layout: collection }
  z:
    layout: file
    attributes:
      n: { range: string }
`,
			wantCode: "raw_linkml_smuggled",
			wantMsg: "Unknown construct 'range' (not khub vocabulary) " +
				"at entities.z.attributes.n.range",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := resolveLocated(t, tc.doc)
			if e.Code != tc.wantCode {
				t.Errorf("code = %q, want %q", e.Code, tc.wantCode)
			}
			if e.Message != tc.wantMsg {
				t.Errorf("message = %q,\nwant      %q", e.Message, tc.wantMsg)
			}
		})
	}
}

func TestVocabLaxCoercionsParity(t *testing.T) {
	// Pydantic lax-mode acceptances captured from Python: a quoted "yes"
	// coerces to a bool; an empty base mapping is a present (empty) base.
	schema := resolveDocs(t, matrixBase+
		"entities:\n  a: { layout: singleton, path: p.md, required: 'yes' }\n  b: { layout: file, orphan: 'true' }\n")
	if !typeOf(t, schema, "a").Required {
		t.Error("required: 'yes' should lax-coerce to true")
	}
	if !typeOf(t, schema, "b").Orphan {
		t.Error("orphan: 'true' should lax-coerce to true")
	}

	emptyBase := resolveDocs(t, "base: {}\nentities:\n  a: { layout: file }\n")
	if got := typeOf(t, emptyBase, "a").Attributes.Len(); got != 0 {
		t.Errorf("empty base merged %d attributes, want 0 (base present but empty)", got)
	}
}
