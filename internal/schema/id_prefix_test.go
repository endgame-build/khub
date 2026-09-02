package schema

// Ports the schema/unit halves of tests/test_id_prefix.py — prefixed and dated
// ids: the id_prefix/id_date vocabulary, the enum-coverage gate, the
// singleton gate, the resolved IdPrefix shapes and IdShape. The minting tests
// (create/init) belong to the entity layer and are covered there / by
// fixtures.

import (
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/omap"
)

func TestByValuePrefixMustMatchItsEnum(t *testing.T) {
	// A map that misses an enum member would mint no id for that member.
	doc := `
ontology:
  entities:
    requirement:
      attributes:
        kind: { enum: [functional, constraint], required: true }
storage:
  requirement: { layout: file, path: reqs, id_prefix: { by: kind, map: { functional: fr } } }
`
	e := resolveLocated(t, doc)
	if !strings.Contains(e.Message, "id_prefix.map must cover exactly kind's enum") {
		t.Errorf("message = %q", e.Message)
	}
	if e.Code != "schema_error" {
		t.Errorf("code = %q, want schema_error", e.Code)
	}
	// Pin the full message including the Python list reprs.
	want := "requirement.id_prefix.map must cover exactly kind's enum; missing ['constraint'], unknown []"
	if e.Message != want {
		t.Errorf("message = %q, want %q", e.Message, want)
	}
}

func TestByValuePrefixNeedsAnEnumAttribute(t *testing.T) {
	doc := `
ontology:
  entities:
    requirement:
      attributes:
        kind: { enum: [functional], required: true }
storage:
  requirement: { layout: file, path: reqs, id_prefix: { by: nope, map: { a: x } } }
`
	e := resolveLocated(t, doc)
	if !strings.Contains(e.Message, "must name an attribute of this type that declares an enum") {
		t.Errorf("message = %q", e.Message)
	}
	want := "requirement.id_prefix.by 'nope' must name an attribute of this type that declares an enum"
	if e.Message != want {
		t.Errorf("message = %q, want %q", e.Message, want)
	}
}

func TestAPrefixMustBeASlugToken(t *testing.T) {
	// It is concatenated into a filename: empty mints a leading hyphen, and a
	// hyphenated one cannot be read back out of the id.
	for _, value := range []string{`''`, `'AD'`, `'a-d'`, `'1st'`, `'  '`} {
		t.Run(value, func(t *testing.T) {
			doc := `
ontology:
  entities:
    a: {}
storage:
  a:
    layout: file
    path: as
    id_prefix: ` + value + "\n"
			e := resolveLocated(t, doc)
			if e.Code != "invalid_schema" {
				t.Errorf("code = %q, want invalid_schema", e.Code)
			}
		})
	}
}

func TestByValuePrefixMayDecideOnABaseAttribute(t *testing.T) {
	// The deciding attribute can come from the base block, or be an override
	// that tightens only `required` — neither is visible before the base is
	// merged. The base arrives as ResolveWith's document, the only legal source.
	base := loadYAMLDoc(t, `
ontology:
  base:
    attributes:
      kind: { enum: [functional, constraint] }
`)
	doc := `
ontology:
  entities:
    requirement:
      attributes:
        kind: { required: true }
storage:
  requirement:
    layout: file
    path: reqs
    id_prefix: { by: kind, map: { functional: fr, constraint: cst } }
`
	resolved, err := ResolveWith(base, writeSchemaFiles(t, doc))
	if err != nil {
		t.Fatalf("ResolveWith: %v", err)
	}
	rtype := typeOf(t, resolved, "requirement")
	if rtype.IdPrefix == nil {
		t.Fatal("requirement.IdPrefix = nil")
	}
	attrs := omap.New()
	attrs.Set("kind", "constraint")
	if got, ok := rtype.IdPrefix.Resolve(attrs); !ok || got != "cst" {
		t.Errorf("Resolve(kind=constraint) = (%q, %v), want (cst, true)", got, ok)
	}
}

func TestResolvedPrefixShapes(t *testing.T) {
	ad := "ad"
	literal := &IdPrefix{Literal: &ad}
	if got, ok := literal.Resolve(omap.New()); !ok || got != "ad" {
		t.Errorf("literal.Resolve({}) = (%q, %v), want (ad, true)", got, ok)
	}
	if !eqStrings(literal.All(), []string{"ad"}) {
		t.Errorf("literal.All() = %v", literal.All())
	}

	kind := "kind"
	byKind := &IdPrefix{By: &kind, Members: []PrefixMember{
		{Value: "functional", Prefix: "fr"},
		{Value: "constraint", Prefix: "cst"},
	}}
	attrs := omap.New()
	attrs.Set("kind", "constraint")
	if got, ok := byKind.Resolve(attrs); !ok || got != "cst" {
		t.Errorf("byKind.Resolve(constraint) = (%q, %v)", got, ok)
	}
	unknown := omap.New()
	unknown.Set("kind", "unknown")
	if _, ok := byKind.Resolve(unknown); ok {
		t.Error("byKind.Resolve(unknown) resolved; want none")
	}
	if _, ok := byKind.Resolve(omap.New()); ok {
		t.Error("byKind.Resolve({}) resolved; want none")
	}
	if !eqStrings(byKind.All(), []string{"fr", "cst"}) {
		t.Errorf("byKind.All() = %v", byKind.All())
	}

	// All() deduplicates, first occurrence first (dict.fromkeys order).
	shared := &IdPrefix{By: &kind, Members: []PrefixMember{
		{Value: "a", Prefix: "x"}, {Value: "b", Prefix: "x"}, {Value: "c", Prefix: "y"},
	}}
	if !eqStrings(shared.All(), []string{"x", "y"}) {
		t.Errorf("shared.All() = %v, want [x y]", shared.All())
	}
}

func TestEveryPresetPrefixIsDeclaredOnARealType(t *testing.T) {
	// The presets' prose conventions (ad-, req-, wp-) and their schemas agree,
	// and every id key sits on a type that mints — a singleton's shape is "".
	for _, name := range []string{"build-lite", "build-hub", "firm-ops"} {
		schema, err := ResolveWith(corePresetDoc(t), presetPaths(t, name))
		if err != nil {
			t.Fatalf("%s failed to resolve: %v", name, err)
		}
		for _, type_ := range schema.Types.Keys() {
			rtype, _ := schema.Types.Get(type_)
			if rtype.Storage.Layout == LayoutSingleton {
				if rtype.IdPrefix != nil || rtype.IdDate {
					t.Errorf("%s/%s: a singleton mints nothing", name, type_)
				}
				if rtype.IdShape() != "" {
					t.Errorf("%s/%s: singleton shape = %q, want empty", name, type_, rtype.IdShape())
				}
				continue
			}
			if !strings.HasSuffix(rtype.IdShape(), "slug") {
				t.Errorf("%s/%s: shape = %q does not end in the slug", name, type_, rtype.IdShape())
			}
			if rtype.IdPrefix == nil {
				continue
			}
			for _, p := range rtype.IdPrefix.All() {
				if p == "" || p != strings.ToLower(p) {
					t.Errorf("%s/%s: prefix %q is not a lowercase token", name, type_, p)
				}
			}
		}
	}
}

// Only a decision legitimately recurs under one title, so only the decision
// types are dated; firm-ops mints bare slugs. Pinned so a preset edit that
// dates a registry type (or undates a decision) is a deliberate change.
func TestShippedDatedTypes(t *testing.T) {
	want := map[string][]string{
		"build-lite": {"adr"},
		"build-hub":  {"pdr", "adr"},
		"firm-ops":   nil,
	}
	for _, name := range []string{"build-lite", "build-hub", "firm-ops"} {
		schema, err := ResolveWith(corePresetDoc(t), presetPaths(t, name))
		if err != nil {
			t.Fatalf("%s failed to resolve: %v", name, err)
		}
		var dated, prefixed []string
		for _, type_ := range schema.Types.Keys() {
			rtype, _ := schema.Types.Get(type_)
			if rtype.IdDate {
				dated = append(dated, type_)
			}
			if rtype.IdPrefix != nil {
				prefixed = append(prefixed, type_)
			}
		}
		if !eqStrings(dated, want[name]) {
			t.Errorf("%s dated types = %v, want %v", name, dated, want[name])
		}
		if name == "firm-ops" && len(prefixed) > 0 {
			t.Errorf("firm-ops declares prefixes on %v; it mints bare slugs", prefixed)
		}
	}
}

// A singleton's id is its type name; an id scheme on one declares a prefix or
// a date nothing would ever mint.
func TestIdKeysAreRefusedOnASingleton(t *testing.T) {
	for _, tc := range []struct{ name, key string }{
		{"id_prefix", "id_prefix: pr"},
		{"id_date", "id_date: true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := resolveLocated(t, `
ontology:
  entities:
    prd: {}
storage:
  prd: { layout: singleton, path: knowledge/prd.md, `+tc.key+` }
`)
			if e.Code != "invalid_schema" {
				t.Errorf("code = %q, want invalid_schema", e.Code)
			}
			want := "Invalid schema at storage.prd: Value error, 'id_prefix' and 'id_date' " +
				"apply only to minting types (a singleton's id is its type name, so it " +
				"mints nothing); drop the key"
			if e.Message != want {
				t.Errorf("message = %q, want %q", e.Message, want)
			}
		})
	}
}

// IdShape is the one rendering of a type's id pattern: the bad_id finding and
// `schema show` both print it, so it is pinned here rather than in each.
func TestIdShape(t *testing.T) {
	s := resolveDocs(t, `
ontology:
  entities:
    adr: {}
    requirement:
      attributes:
        kind: { enum: [functional, constraint, business-rule] }
    client: {}
    prd: {}
    both:
      attributes:
        kind: { enum: [a, b] }
storage:
  adr:         { layout: file, path: decisions, id_prefix: ad, id_date: true }
  requirement: { layout: file, path: reqs, id_prefix: { by: kind, map: { functional: fr, constraint: cst, business-rule: br } } }
  client:      { layout: file, path: clients }
  prd:         { layout: singleton, path: prd.md }
  both:        { layout: collection, path: both.yaml, id_prefix: { by: kind, map: { a: x, b: y } }, id_date: true }
`)
	for _, tc := range []struct{ typ, want string }{
		{"adr", "ad-YYYY-MM-DD-slug"},
		{"requirement", "fr|cst|br-slug"},
		{"client", "slug"},
		{"prd", ""},
		{"both", "x|y-YYYY-MM-DD-slug"},
	} {
		if got := typeOf(t, s, tc.typ).IdShape(); got != tc.want {
			t.Errorf("%s: IdShape() = %q, want %q", tc.typ, got, tc.want)
		}
	}
}
