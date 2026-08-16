package schema

// Ports the schema/unit halves of tests/test_id_prefix.py — enumerated ids:
// the id_prefix vocabulary, its enum-coverage gate, and the resolved IdPrefix
// shapes. The minting tests (create/init) belong to the entity layer and are
// covered there / by fixtures.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/omap"
)

func TestByValuePrefixMustMatchItsEnum(t *testing.T) {
	// A map that misses an enum member would mint no id for that member.
	doc := `
base:
  attributes:
    type: { type: text, required: true }
entities:
  requirement:
    layout: file
    path: reqs
    id_prefix: { by: kind, map: { functional: fr } }
    attributes:
      kind: { enum: [functional, constraint], required: true }
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
base:
  attributes:
    type: { type: text, required: true }
entities:
  requirement:
    layout: file
    path: reqs
    id_prefix: { by: nope, map: { a: x } }
    attributes:
      kind: { enum: [functional], required: true }
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
base:
  attributes:
    type: { type: text, required: true }
entities:
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
	// merged.
	doc := `
base:
  attributes:
    kind: { enum: [functional, constraint] }
entities:
  requirement:
    layout: file
    path: reqs
    id_prefix: { by: kind, map: { functional: fr, constraint: cst } }
    attributes:
      kind: { required: true }
`
	rtype := typeOf(t, resolveDocs(t, doc), "requirement")
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
	// The presets' prose conventions (ad-, fr-, wp-) and their schemas agree.
	presets := presetsDir(t)
	for _, name := range []string{"build-lite", "build-hub", "firm-ops"} {
		schema, err := Resolve([]string{
			filepath.Join(presets, "core.yaml"),
			filepath.Join(presets, name, "schema.yaml"),
		})
		if err != nil {
			t.Fatalf("%s failed to resolve: %v", name, err)
		}
		for _, type_ := range schema.Types.Keys() {
			rtype, _ := schema.Types.Get(type_)
			if rtype.IdPrefix == nil {
				continue
			}
			if rtype.Storage.Layout == "singleton" {
				t.Errorf("%s/%s: a singleton mints nothing", name, type_)
			}
			for _, p := range rtype.IdPrefix.All() {
				if p == "" || p != strings.ToLower(p) {
					t.Errorf("%s/%s: prefix %q is not a lowercase token", name, type_, p)
				}
			}
		}
	}
}
