package schema

// Shared helpers for the schema tests, mirroring tests/conftest.py: the
// minimal core base block and the write_schema fixture (files written in
// argument order, resolved in that order).

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/omap"
)

// coreBase mirrors the embedded presets/core/ontology.yaml (a minimal base
// block). An authored ontology.base is forbidden, so tests supply it the way
// production does — as ResolveWith's base document (resolveWithCore), never as
// an authored file.
const coreBase = `
ontology:
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
	schema, err := ResolveWith(nil, writeSchemaFiles(t, docs...))
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	return schema
}

// resolveWithCore resolves docs over the coreBase document, supplied the way
// production supplies the embedded base — as ResolveWith's base doc, never as
// an authored file (an authored ontology.base is forbidden).
func resolveWithCore(t *testing.T, docs ...string) *ResolvedSchema {
	t.Helper()
	schema, err := ResolveWith(loadYAMLDoc(t, coreBase), writeSchemaFiles(t, docs...))
	if err != nil {
		t.Fatalf("ResolveWith failed: %v", err)
	}
	return schema
}

// resolveLocated resolves docs expecting an *errs.Located failure.
func resolveLocated(t *testing.T, docs ...string) *errs.Located {
	t.Helper()
	_, err := ResolveWith(nil, writeSchemaFiles(t, docs...))
	return asLocatedErr(t, err)
}

// resolveLocatedWithCore is resolveLocated over the coreBase document.
func resolveLocatedWithCore(t *testing.T, docs ...string) *errs.Located {
	t.Helper()
	_, err := ResolveWith(loadYAMLDoc(t, coreBase), writeSchemaFiles(t, docs...))
	return asLocatedErr(t, err)
}

func asLocatedErr(t *testing.T, err error) *errs.Located {
	t.Helper()
	if err == nil {
		t.Fatal("Resolve succeeded; want a LocatedError")
	}
	var le *errs.Located
	ok := errors.As(err, &le)
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

// presetsDir locates the authored presets (the integration fixtures).
func presetsDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join("..", "..", "presets")
	if _, err := os.Stat(filepath.Join(dir, "core", "ontology.yaml")); err != nil {
		t.Fatalf("presets dir not found at %s: %v", dir, err)
	}
	return dir
}

// presetPaths lists an authored preset's layer files — whichever of the three
// the preset ships. The core base is NOT among them: it goes to ResolveWith as
// the base document (corePresetDoc), the only legal source of a base.
func presetPaths(t *testing.T, name string) []string {
	t.Helper()
	presets := presetsDir(t)
	var paths []string
	for _, layer := range []string{"ontology.yaml", "policy.yaml", "storage.yaml"} {
		p := filepath.Join(presets, name, layer)
		if _, err := os.Stat(p); err == nil {
			paths = append(paths, p)
		}
	}
	return paths
}

// corePresetDoc loads the real embedded base document
// (presets/core/ontology.yaml) the way introspect supplies it.
func corePresetDoc(t *testing.T) *omap.Map {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(presetsDir(t), "core", "ontology.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return loadYAMLDoc(t, string(raw))
}

// loadYAMLDoc parses one inline YAML document through the canon choke point —
// the shape ResolveWith takes its embedded-base fallback in.
func loadYAMLDoc(t *testing.T, text string) *omap.Map {
	t.Helper()
	v, err := canon.LoadDocMode(text, canon.Mode12)
	if err != nil {
		t.Fatalf("parse inline doc: %v", err)
	}
	m, ok := v.(*omap.Map)
	if !ok {
		t.Fatalf("inline doc top level is %T, not a mapping", v)
	}
	return m
}
