package schema

// Shared helpers for the schema tests, mirroring tests/conftest.py: the
// minimal core base block and the write_schema fixture (files written in
// argument order, resolved in that order).

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/endgame-build/khub/internal/errs"
)

// coreBase mirrors conftest.CORE_BASE (a minimal src/khub/presets/core.yaml).
const coreBase = `
base:
  attributes:
    type:        { type: text, required: true }
    draft:       { type: bool, default: false }
    author:      { type: text }
    created:     { type: date, required: true }
    updated:     { type: date }
    title:       { type: text }
    description: { type: text }
    resource:    { type: text }
    tags:        { type: list }
  relations:
    related:     { to: any, many: true }
    sources:     { to: any, many: true }
    references:  { to: any, many: true }
    depends_on:  { to: any, many: true }
`

// writeSchemaFiles is the write_schema fixture: each doc becomes one YAML file;
// paths return in argument order.
func writeSchemaFiles(t *testing.T, docs ...string) []string {
	t.Helper()
	dir := t.TempDir()
	paths := make([]string, len(docs))
	for i, doc := range docs {
		p := filepath.Join(dir, fmt.Sprintf("s%d.yaml", i))
		if err := os.WriteFile(p, []byte(doc), 0o644); err != nil {
			t.Fatal(err)
		}
		paths[i] = p
	}
	return paths
}

func resolveDocs(t *testing.T, docs ...string) *ResolvedSchema {
	t.Helper()
	schema, err := Resolve(writeSchemaFiles(t, docs...))
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	return schema
}

// resolveLocated resolves docs expecting an *errs.Located failure.
func resolveLocated(t *testing.T, docs ...string) *errs.Located {
	t.Helper()
	_, err := Resolve(writeSchemaFiles(t, docs...))
	if err == nil {
		t.Fatal("Resolve succeeded; want a LocatedError")
	}
	le, ok := err.(*errs.Located)
	if !ok {
		t.Fatalf("Resolve error is %T (%v); want *errs.Located", err, err)
	}
	return le
}

func typeOf(t *testing.T, s *ResolvedSchema, name string) *ResolvedType {
	t.Helper()
	rt, ok := s.Types.Get(name)
	if !ok {
		t.Fatalf("no type %q in schema (have %v)", name, s.Types.Keys())
	}
	return rt
}

func attrOf(t *testing.T, rt *ResolvedType, name string) *ResolvedAttribute {
	t.Helper()
	a, ok := rt.Attributes.Get(name)
	if !ok {
		t.Fatalf("%s: no attribute %q (have %v)", rt.Name, name, rt.Attributes.Keys())
	}
	return a
}

func relOf(t *testing.T, rt *ResolvedType, name string) *ResolvedRelation {
	t.Helper()
	r, ok := rt.Relations.Get(name)
	if !ok {
		t.Fatalf("%s: no relation %q (have %v)", rt.Name, name, rt.Relations.Keys())
	}
	return r
}

func eqStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// presetsDir locates the authored Python presets (the integration fixtures).
func presetsDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join("..", "..", "presets")
	if _, err := os.Stat(filepath.Join(dir, "core.yaml")); err != nil {
		t.Fatalf("presets dir not found at %s: %v", dir, err)
	}
	return dir
}
