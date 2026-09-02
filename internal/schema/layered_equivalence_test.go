package schema

// The layered-shape contract: how the three layers merge, what defaults apply,
// and which cross-layer mistakes are caught. ResolvedType is the firewall — it
// is the merged product of all three layers, so cli/integrity/graph/query/
// reindex/viz never learn that authoring is split.
//
// (The pre-split equivalence tests that gated the Phase 2 preset conversion
// lived here and in preset_equivalence_test.go; both went with legacy.go once
// the parity fixtures pinned the new layout.)

import (
	"fmt"
	"strings"
	"testing"
)

// The fixture exercises every key the layers carry: all four layouts, both
// id_prefix forms, id_date, when/required/orphan, an authored format,
// enum/pattern/default attribute facets, and relations carrying
// many/inverse/acyclic/union/any.

const equivOntology = `
ontology:
  entities:
    domain:
      when: a bounded context is named
      attributes:
        title: { required: true }
        tier:  { enum: [core, supporting], required: true }
      relations:
        depends_on: { to: domain, many: true, acyclic: true }
    adr:
      attributes:
        status: { enum: [proposed, accepted], required: true }
        seq:    { type: number, default: 1 }
        slugish: { type: text, pattern: '^[a-z]+$' }
      relations:
        supersedes: { to: adr, inverse: superseded, acyclic: true }
        affects:    { to: any, many: true }
        touches:    { to: [domain, adr], many: true }
    repo:
      attributes:
        repo: { type: text, required: true }
    prd:
      attributes:
        title: { required: true }
`

const equivPolicy = `
policy:
  prd: { required: true, orphan: true }
`

const equivStorage = `
storage:
  domain: { layout: file,       path: knowledge/domains,    id_prefix: dom }
  adr:
    layout: folder
    path: knowledge/decisions
    id_prefix: { by: status, map: { proposed: prop, accepted: acc } }
    id_date: true
  repo:   { layout: collection, path: knowledge/repos.yaml }
  prd:    { layout: singleton,  path: knowledge/prd.md }
`

// The merge keys on top-level KEY, not on filename, so how many documents carry
// the three layers is not part of the contract.
func TestLayerFileCountIsNotContract(t *testing.T) {
	split := dumpResolved(resolveWithCore(t, equivOntology, equivPolicy, equivStorage))
	oneFile := dumpResolved(resolveWithCore(t, equivOntology+equivPolicy+equivStorage))
	if split != oneFile {
		t.Fatalf("three files differ from one file carrying three blocks:\n%s",
			firstDiff(split, oneFile))
	}
}

// A storage ENTRY must say its layout. The file/<type> default exists for a
// type with no storage entry at all; an entry naming a path but no layout is a
// half-statement, and silently defaulting it to `file` would turn a forgotten
// `layout: collection` into a directory of strays.
func TestStorageEntryRequiresLayout(t *testing.T) {
	le := resolveLocated(t, `
ontology:
  entities:
    note: {}
storage:
  note: { path: notes }
`)
	if !strings.Contains(le.Message, "storage.note.layout") ||
		!strings.Contains(le.Message, "Field required") {
		t.Errorf("message = %q; want Field required at storage.note.layout", le.Message)
	}
}

// The resolver rebuilds its input from the three layer keys, so it must
// reject — not silently drop — everything else. The classic case is the
// pre-split shape itself: top-level `entities:` would otherwise resolve to a
// zero-type schema with every gate green. `version` stays legal (presets
// stamp it).
func TestUnknownTopLevelKeyRejected(t *testing.T) {
	le := resolveLocated(t, "entities:\n  a: {}\n")
	if le.Code != "raw_linkml_smuggled" {
		t.Errorf("code = %q, want raw_linkml_smuggled", le.Code)
	}
	if !strings.Contains(le.Message, "'entities'") {
		t.Errorf("message = %q does not name the key", le.Message)
	}
	if _, err := ResolveWith(nil, writeSchemaFiles(t,
		"version: \"1.0.0\"\nontology:\n  entities:\n    a: {}\n")); err != nil {
		t.Errorf("version at top level must stay legal: %v", err)
	}
}

// Inside `ontology:` only `entities` is authorable (`base` is khub-owned and
// separately rejected); anything else is a smuggled construct, not a key to
// drop.
func TestUnknownOntologyKeyRejected(t *testing.T) {
	le := resolveLocated(t, "ontology:\n  entites:\n    a: {}\n")
	if le.Code != "raw_linkml_smuggled" {
		t.Errorf("code = %q, want raw_linkml_smuggled", le.Code)
	}
	want := "Unknown construct 'entites' (not khub vocabulary) at ontology.entites"
	if le.Message != want {
		t.Errorf("message = %q, want %q", le.Message, want)
	}
}

// A type ontology declares but no storage layer names still resolves: storage
// defaults to one file per entity under a directory named for the type. This is
// what lets an ontology-only workspace run.
func TestStorageDefaultsForUndeclaredType(t *testing.T) {
	s := resolveDocs(t, `
ontology:
  entities:
    note:
      attributes:
        title: { required: true }
`)
	rt := typeOf(t, s, "note")
	if rt.Storage.Layout != LayoutFile {
		t.Errorf("layout = %q; want %q", rt.Storage.Layout, LayoutFile)
	}
	if rt.Storage.Path == nil || *rt.Storage.Path != "note" {
		t.Errorf("path = %v; want \"note\"", rt.Storage.Path)
	}
	if rt.Storage.Fmt != "md" {
		t.Errorf("format = %q; want \"md\"", rt.Storage.Fmt)
	}
}

// Ontology is the layer that declares which types exist; policy and storage
// only annotate. A name they carry that ontology never declared is a typo or a
// stale entry, and silently ignoring it would leave a gate or a path quietly
// not applying.
func TestAnnotatingLayerCannotDeclareAType(t *testing.T) {
	for _, tc := range []struct{ name, doc string }{
		{"policy", "\npolicy:\n  ghost: { orphan: true }\n"},
		{"storage", "\nstorage:\n  ghost: { layout: file, path: x }\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			le := resolveLocated(t, equivOntology+tc.doc)
			if !strings.Contains(le.Message, "ghost") {
				t.Errorf("message %q does not name the offending type", le.Message)
			}
			if !strings.Contains(le.Message, tc.name) {
				t.Errorf("message %q does not name the layer", le.Message)
			}
		})
	}
}

// storageMatrix cross-checks fields from all three layers, so it can only run
// post-merge — `required` arrives from policy while `layout` arrives from
// storage, and neither document alone can see the conflict.
func TestStorageMatrixRunsAcrossLayers(t *testing.T) {
	le := resolveLocated(t, `
ontology:
  entities:
    note:
      attributes:
        title: { required: true }
policy:
  note: { required: true }
storage:
  note: { layout: file, path: notes }
`)
	if !strings.Contains(le.Message, "singleton-only") {
		t.Errorf("message %q; want the singleton-only storage-matrix error", le.Message)
	}
}

// --- deterministic dump ------------------------------------------------------

// dumpResolved renders a ResolvedSchema to a stable, total string. Every field
// of ResolvedType/ResolvedAttribute/ResolvedRelation is included: a field left
// out here is a field the equivalence test cannot see, which is exactly how a
// split would silently drop something.
func dumpResolved(s *ResolvedSchema) string {
	var b strings.Builder
	dumpAttrs(&b, "base", s.BaseAttributes)
	dumpRels(&b, "base", s.BaseRelations)
	for _, name := range s.Types.Keys() {
		t, _ := s.Types.Get(name)
		fmt.Fprintf(&b, "type %s\n", t.Name)
		fmt.Fprintf(&b, "  storage layout=%q path=%s fmt=%q\n",
			t.Storage.Layout, pstr(t.Storage.Path), t.Storage.Fmt)
		fmt.Fprintf(&b, "  required=%t orphan=%t when=%s\n", t.Required, t.Orphan, pstr(t.When))
		fmt.Fprintf(&b, "  idprefix=%s iddate=%t\n", dumpIDPrefix(t.IdPrefix), t.IdDate)
		fmt.Fprintf(&b, "  template=%s templateoff=%t\n", pstr(t.Template), t.TemplateOff)
		dumpAttrs(&b, "  "+t.Name, t.Attributes)
		dumpRels(&b, "  "+t.Name, t.Relations)
	}
	return b.String()
}

func dumpAttrs(b *strings.Builder, owner string, attrs *Ordered[*ResolvedAttribute]) {
	for _, n := range attrs.Keys() {
		a, _ := attrs.Get(n)
		fmt.Fprintf(b, "%s attr %s base=%q required=%t pattern=%s enum=%v default=%#v overridden=%t\n",
			owner, a.Name, a.BaseType, a.Required, pstr(a.Pattern), a.Enum, a.Default, a.OverriddenFromBase)
	}
}

func dumpRels(b *strings.Builder, owner string, rels *Ordered[*ResolvedRelation]) {
	for _, n := range rels.Keys() {
		r, _ := rels.Get(n)
		fmt.Fprintf(b, "%s rel %s targets=%v kind=%q many=%t required=%t inverse=%s acyclic=%t\n",
			owner, r.Predicate, r.Targets, r.Kind, r.Many, r.Required, pstr(r.Inverse), r.Acyclic)
	}
}

func dumpIDPrefix(p *IdPrefix) string {
	if p == nil {
		return "<nil>"
	}
	var parts []string
	for _, m := range p.Members {
		parts = append(parts, m.Value+"="+m.Prefix)
	}
	return fmt.Sprintf("literal=%s by=%s members=[%s]",
		pstr(p.Literal), pstr(p.By), strings.Join(parts, ","))
}

// firstDiff reports the first differing line of two dumps, with context, so a
// failure names the field that diverged instead of printing two whole schemas.
func firstDiff(a, b string) string {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := 0; i < len(al) || i < len(bl); i++ {
		x, y := lineAt(al, i), lineAt(bl, i)
		if x != y {
			return fmt.Sprintf("line %d:\n  legacy:  %s\n  layered: %s", i+1, x, y)
		}
	}
	return "(dumps equal but comparison failed)"
}

func lineAt(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return "<missing>"
}

// pstr renders an optional string for the dump, distinguishing an absent field
// from an empty one — the tri-state the vocabulary walk depends on.
func pstr(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return `"` + *p + `"`
}
