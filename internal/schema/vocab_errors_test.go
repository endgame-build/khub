package schema

// Pins for the vocabulary walk's error catalog. The messages and mechanics —
// pydantic-core catalog strings, the smart-union branch tags "constrained-str"
// / "IdPrefixDecl" / "str", and precedence (extra_forbidden anywhere beats the
// first value error; base errors precede entity errors; first entity in input
// order wins) — were originally captured from the retired Python
// implementation (khub 0.18.x, pydantic v2). The layered shape never existed
// in Python, so the locs below are khub's OWN contract now; the catalog is
// inherited, the parity framing is over.

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
			doc: matrixBase + `  entities:
    a: {}
storage:
  a: { layout: file, path: as, id_prefix: 'AD' }
`,
			wantCode: "invalid_schema",
			wantMsg: "Invalid schema at storage.a.id_prefix.constrained-str: " +
				"String should match pattern '^[a-z][a-z0-9]*$'",
		},
		{
			name: "smuggled key inside id_prefix carries the union tag",
			doc: matrixBase + `  entities:
    a:
      attributes:
        kind: { enum: [x] }
storage:
  a: { layout: file, id_prefix: { by: kind, map: { x: y }, nope: 1 } }
`,
			wantCode: "raw_linkml_smuggled",
			wantMsg: "Unknown construct 'nope' (not khub vocabulary) " +
				"at storage.a.id_prefix.IdPrefixDecl.nope",
		},
		{
			name:     "layout outside the literal set",
			doc:      matrixBase + "  entities:\n    a: {}\nstorage:\n  a: { layout: nope }\n",
			wantCode: "invalid_schema",
			wantMsg: "Invalid schema at storage.a.layout: " +
				"Input should be 'file', 'folder', 'collection' or 'singleton'",
		},
		{
			name:     "non-string to reports the str union branch first",
			doc:      matrixBase + "  entities:\n    a:\n      relations:\n        r: { to: 42 }\n",
			wantCode: "invalid_schema",
			wantMsg:  "Invalid schema at ontology.entities.a.relations.r.to.str: Input should be a valid string",
		},
		{
			name:     "format must be a string",
			doc:      matrixBase + "  entities:\n    a: {}\nstorage:\n  a: { layout: file, format: 42 }\n",
			wantCode: "invalid_schema",
			wantMsg:  "Invalid schema at storage.a.format: Input should be a valid string",
		},
		{
			name:     "required rejects an uninterpretable int",
			doc:      matrixBase + "  entities:\n    a: {}\npolicy:\n  a: { required: 5 }\n",
			wantCode: "invalid_schema",
			wantMsg: "Invalid schema at policy.a.required: " +
				"Input should be a valid boolean, unable to interpret input",
		},
		{
			name:     "enum must be a list",
			doc:      matrixBase + "  entities:\n    a:\n      attributes:\n        f: { enum: x }\n",
			wantCode: "invalid_schema",
			wantMsg:  "Invalid schema at ontology.entities.a.attributes.f.enum: Input should be a valid list",
		},
		{
			name:     "first entity in input order wins",
			doc:      matrixBase + "  entities:\n    a: { when: 1 }\n    b: { when: 2 }\n",
			wantCode: "invalid_schema",
			wantMsg:  "Invalid schema at ontology.entities.a.when: Input should be a valid string",
		},
		{
			name: "id_prefix mapping missing map reports the str branch first",
			doc: matrixBase + `  entities:
    a:
      attributes:
        kind: { enum: [x] }
storage:
  a: { layout: file, id_prefix: { by: kind } }
`,
			wantCode: "invalid_schema",
			wantMsg: "Invalid schema at storage.a.id_prefix.constrained-str: " +
				"Input should be a valid string",
		},
		{
			name:     "when must be a string",
			doc:      matrixBase + "  entities:\n    a: { when: 42 }\n",
			wantCode: "invalid_schema",
			wantMsg:  "Invalid schema at ontology.entities.a.when: Input should be a valid string",
		},
		{
			name:     "a null attribute declaration is not a model",
			doc:      matrixBase + "  entities:\n    a:\n      attributes:\n        f:\n",
			wantCode: "invalid_schema",
			wantMsg: "Invalid schema at ontology.entities.a.attributes.f: " +
				"Input should be a valid dictionary or instance of AttrDecl",
		},
		{
			name:     "a relation needs to",
			doc:      matrixBase + "  entities:\n    a:\n      relations:\n        r: { many: true }\n",
			wantCode: "invalid_schema",
			wantMsg:  "Invalid schema at ontology.entities.a.relations.r.to: Field required",
		},
		{
			name:     "a bad list item fails both union branches str-first",
			doc:      matrixBase + "  entities:\n    a:\n      relations:\n        r: { to: [a, 42] }\n",
			wantCode: "invalid_schema",
			wantMsg:  "Invalid schema at ontology.entities.a.relations.r.to.str: Input should be a valid string",
		},
		{
			name:     "any inside a list is a type name, not the any kind",
			doc:      matrixBase + "  entities:\n    a:\n      relations:\n        r: { to: [any] }\n",
			wantCode: "unknown_relation_target",
			wantMsg:  "Type 'a' relation 'r' targets unknown type 'any'",
		},
		{
			name: "extra_forbidden anywhere beats an earlier value error",
			doc: matrixBase + `  entities:
    a: { when: 42 }
    z:
      attributes:
        n: { range: string }
`,
			wantCode: "raw_linkml_smuggled",
			wantMsg: "Unknown construct 'range' (not khub vocabulary) " +
				"at ontology.entities.z.attributes.n.range",
		},
		{
			name: "an ontology error beats a later storage error",
			doc: matrixBase + `  entities:
    a: { when: 42 }
storage:
  a: { layout: nope }
`,
			wantCode: "invalid_schema",
			wantMsg:  "Invalid schema at ontology.entities.a.when: Input should be a valid string",
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
	// Pydantic lax-mode acceptances, inherited: a quoted "yes" coerces to a
	// bool.
	schema := resolveDocs(t, matrixBase+
		"  entities:\n    a: {}\n    b: {}\n"+
		"policy:\n  a: { required: 'yes' }\n  b: { orphan: 'true' }\n"+
		"storage:\n  a: { layout: singleton, path: p.md }\n")
	if !typeOf(t, schema, "a").Required {
		t.Error("required: 'yes' should lax-coerce to true")
	}
	if !typeOf(t, schema, "b").Orphan {
		t.Error("orphan: 'true' should lax-coerce to true")
	}
}

func TestBrokenBaseDocumentStillValidated(t *testing.T) {
	// The base arrives as ResolveWith's document (authored base is forbidden),
	// but it still goes through the vocabulary walk — and base errors precede
	// entity errors, as they always have.
	badBase := loadYAMLDoc(t, "ontology:\n  base:\n    attributes:\n      t: { type: nope }\n")
	files := writeSchemaFiles(t, "ontology:\n  entities:\n    a:\n      attributes:\n        f: { enum: x }\n")
	_, err := ResolveWith(badBase, files)
	le := asLocatedErr(t, err)
	want := "Invalid schema at ontology.base.attributes.t.type: " +
		"Input should be 'text', 'number', 'date', 'datetime', 'bool' or 'list'"
	if le.Message != want {
		t.Errorf("message = %q,\nwant      %q", le.Message, want)
	}
}
