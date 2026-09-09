// Registry assertions ported from tests/test_init.py (the resolve_preset /
// known_presets rows), plus the shipped-preset shape assertions from
// tests/test_firm_ops.py, tests/test_build_lite.py and tests/test_build_hub.py.
// Those preset tests belong here in the Go port: this package owns the embedded
// tree, so a preset that stops resolving fails where it is shipped.
package presets

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
)

// --- registry ------------------------------------------------------------------

func TestKnownPresetsFromPackage(t *testing.T) {
	got := Known(Embedded())
	want := []string{"build-hub", "firm-ops"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Known() = %v, want %v", got, want)
	}
}

func TestResolveKnownPresetFromPackage(t *testing.T) {
	// TS-WS-001-U01: the packaged firm-ops preset resolves (dir layout).
	got, err := Resolve("firm-ops", Embedded())
	if err != nil {
		t.Fatal(err)
	}
	if got != "firm-ops/ontology.yaml" {
		t.Fatalf("Resolve = %q", got)
	}
}

func TestResolvePresetFromSource(t *testing.T) {
	// TS-WS-001-U01: a preset resolves from --preset-source, through the same
	// discovery logic over os.DirFS.
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "note", "ontology.yaml"),
		"version: \"9.9.9\"\nontology: {entities: {note: {}}}\n")
	src := Source(dir)
	got, err := Resolve("note", src)
	if err != nil {
		t.Fatal(err)
	}
	if got != "note/ontology.yaml" {
		t.Fatalf("Resolve = %q", got)
	}
	if k := Known(src); !reflect.DeepEqual(k, []string{"note"}) {
		t.Fatalf("Known = %v", k)
	}
}

func TestUnknownPresetListsKnown(t *testing.T) {
	// TS-WS-001-U06: an unknown preset is rejected, listing the known presets.
	_, err := Resolve("bogus", Embedded())
	var located *errs.Located
	if !errors.As(err, &located) {
		t.Fatalf("err = %v", err)
	}
	if located.Code != "unknown_preset" {
		t.Fatalf("code = %s", located.Code)
	}
	want := "Unknown preset 'bogus'. Known presets: build-hub, firm-ops"
	if located.Message != want {
		t.Fatalf("message = %q", located.Message)
	}
}

func TestUnknownPresetSourceResolvesNothing(t *testing.T) {
	// pathlib globs a nonexistent directory into an empty list rather than
	// raising; fs.Glob over os.DirFS must behave the same.
	src := Source(filepath.Join(t.TempDir(), "absent"))
	if k := Known(src); len(k) != 0 {
		t.Fatalf("Known = %v", k)
	}
	if _, err := Resolve("note", src); err == nil {
		t.Fatal("expected unknown_preset")
	}
}

func TestTemplatesShipPerPreset(t *testing.T) {
	// kb test_every_shipped_template_parses: every build-hub md type ships a
	// template, one per type — prd, arc42 and adr carry a heading contract;
	// the rest carry a hint and lenses. firm-ops ships none at all.
	for _, tc := range []struct {
		preset string
		want   []string
	}{
		{"build-hub", []string{
			"actor.yaml", "adr.yaml", "api.yaml", "arc42.yaml", "capability.yaml",
			"component.yaml", "prd.yaml", "repo.yaml", "requirement.yaml",
			"system.yaml", "use-case.yaml",
		}},
		{"firm-ops", nil},
	} {
		var got []string
		for _, p := range Templates(tc.preset, Embedded()) {
			got = append(got, filepath.Base(p))
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s templates = %v, want %v", tc.preset, got, tc.want)
		}
	}
	// One template per type: a type added without one is a body contract an
	// agent never sees.
	hub := resolvePreset(t, "build-hub")
	if got, want := len(Templates("build-hub", Embedded())), len(hub.Types.Keys()); got != want {
		t.Errorf("build-hub templates = %d, want one per type (%d)", got, want)
	}
}

// --- shipped preset shapes -----------------------------------------------------

func TestFirmOpsPreset(t *testing.T) {
	s := resolvePreset(t, "firm-ops")

	// TS-SCH-004-01: nine types.
	assertTypeSet(t, s, []string{
		"opportunity", "project", "meeting", "transcript", "fragment",
		"case-study", "partnership", "person", "client",
	})
	// TS-SCH-004-02: the 10 firm-ops predicates beyond the universal four.
	assertPredicates(t, s, []string{
		"client", "engagement", "origin_opportunity", "owner", "partner",
		"related_opportunities", "related_projects", "source_project", "team", "transcript",
	})

	eng := relation(t, s, "meeting", "engagement")
	if eng.Kind != schema.KindUnion {
		t.Errorf("meeting.engagement kind = %s", eng.Kind)
	}
	if !reflect.DeepEqual(eng.Targets, []string{"opportunity", "project", "partnership"}) {
		t.Errorf("meeting.engagement targets = %v", eng.Targets)
	}
	if !relation(t, s, "opportunity", "owner").Required {
		t.Error("opportunity.owner is not required")
	}
	if !relation(t, s, "partnership", "related_projects").Many {
		t.Error("partnership.related_projects is not many")
	}

	// TS-SCH-004-03: the four porting notes.
	for _, name := range s.Types.Keys() {
		rtype, _ := s.Types.Get(name)
		if rtype.Relations.Has("superseded_by") {
			t.Errorf("%s declares a stored inverse superseded_by", name)
		}
		attr, ok := rtype.Attributes.Get("draft")
		if !ok || attr.BaseType != "bool" {
			t.Errorf("%s.draft is not the core bool", name)
		}
	}
	if _, ok := typeOf(t, s, "client").Relations.Get("partner"); !ok {
		t.Error("client has no partner relation")
	}
	if got := typeOf(t, s, "meeting").Storage.Layout; got != schema.LayoutFile {
		t.Errorf("meeting layout = %s", got)
	}
}

func TestBuildHubPreset(t *testing.T) {
	s := resolvePreset(t, "build-hub")
	singletons := []string{"prd", "arc42"}
	minted := []string{
		"capability", "actor", "use-case", "requirement", "adr",
		"system", "component", "api", "repo",
	}

	// Eleven types: the two narrative docs and nine minting registries. What
	// 0.6.0 cut stays cut — the seventeen-type hub's extras and the delivery
	// layer both build presets dropped before the merge.
	assertTypeSet(t, s, append(append([]string(nil), singletons...), minted...))
	for _, gone := range []string{
		"roadmap", "glossary", "erd", "pdr", "boundary", "quality-attribute",
		"domain", "entity", "contract", "baseline", "external-system",
		"feature-spec", "test-spec", "work-package",
	} {
		if s.Types.Has(gone) {
			t.Errorf("cut type %s is back", gone)
		}
	}
	// Twelve predicate names beyond the universal four, one declaration each.
	assertPredicates(t, s, []string{
		"actor", "affects", "capabilities", "capability", "consumes", "provider",
		"realized_in", "repo", "served_by", "supersedes", "system", "use_cases",
	})
	if n := countDeclarations(s); n != 12 {
		t.Errorf("declarations = %d, want 12", n)
	}

	// Two narrative docs above the top of the durability ladder: singleton md,
	// exempt from the orphan sweep; prd is the one `check` demands.
	for _, name := range singletons {
		ty := typeOf(t, s, name)
		if ty.Storage.Layout != schema.LayoutSingleton || ty.Storage.Fmt != "md" {
			t.Errorf("%s storage = %s/%s", name, ty.Storage.Layout, ty.Storage.Fmt)
		}
		if !ty.Orphan {
			t.Errorf("%s does not opt out of the orphan sweep", name)
		}
	}
	if !typeOf(t, s, "prd").Required {
		t.Error("prd is not required")
	}
	if typeOf(t, s, "arc42").Required {
		t.Error("arc42 is required")
	}
	assertPath(t, s, "prd", "knowledge/prd.md")
	assertPath(t, s, "arc42", "knowledge/arc42.md")

	// Every other type is a directory of md files under the one knowledge
	// root — no collections — minting a lowercase prefixed slug from its
	// title; adr alone is dated, because only a decision recurs under one
	// title.
	for _, name := range minted {
		ty := typeOf(t, s, name)
		if ty.Storage.Layout != schema.LayoutFile || ty.Storage.Fmt != "md" {
			t.Errorf("%s storage = %s/%s", name, ty.Storage.Layout, ty.Storage.Fmt)
		}
		if ty.Orphan || ty.Required {
			t.Errorf("%s orphan=%v required=%v", name, ty.Orphan, ty.Required)
		}
	}
	for _, name := range s.Types.Keys() {
		if a, ok := typeOf(t, s, name).Attributes.Get("title"); !ok || !a.Required {
			t.Errorf("%s.title is not required", name)
		}
	}
	for _, tc := range []struct{ typ, path, shape string }{
		{"capability", "knowledge/capabilities", "cap-slug"},
		{"actor", "knowledge/actors", "act-slug"},
		{"use-case", "knowledge/use-cases", "uc-slug"},
		{"requirement", "knowledge/requirements", "req-slug"},
		{"adr", "knowledge/decisions", "ad-YYYY-MM-DD-slug"},
		{"system", "knowledge/systems", "sys-slug"},
		{"component", "knowledge/components", "cmp-slug"},
		{"api", "knowledge/apis", "api-slug"},
		{"repo", "knowledge/repos", "rp-slug"},
	} {
		assertPath(t, s, tc.typ, tc.path)
		if got := typeOf(t, s, tc.typ).IDShape(); got != tc.shape {
			t.Errorf("%s id shape = %q, want %q", tc.typ, got, tc.shape)
		}
	}

	// The map, the people and the grouping: title only; nothing points out
	// of them. system.owner is slug-form text until `team` turns it into an
	// edge.
	for _, name := range []string{"capability", "actor", "system"} {
		if n := len(ownPredicates(s, name)); n != 0 {
			t.Errorf("%s declares %d relations", name, n)
		}
	}
	assertText(t, s, "system", "owner", false)

	// use-case: the trigger is required, the actor is not — a scheduled or
	// event-driven flow needs no actor called "System". served_by is the
	// blueprint edge; its inversion answers what breaks for users.
	assertEnum(t, s, "use-case", "trigger", []string{"human", "scheduled", "event", "external"})
	if !attr(t, s, "use-case", "trigger").Required {
		t.Error("use-case.trigger is not required")
	}
	assertRelation(t, s, "use-case", "actor", []string{"actor"}, false, false)
	assertRelation(t, s, "use-case", "capability", []string{"capability"}, false, false)
	assertRelation(t, s, "use-case", "served_by", []string{"component"}, true, false)

	// requirement: boundary and quality-attribute collapse into kind;
	// enforcement says what holds the rule up.
	assertEnum(t, s, "requirement", "kind", []string{"functional", "non-functional", "constraint", "business-rule"})
	if !attr(t, s, "requirement", "kind").Required {
		t.Error("requirement.kind is not required")
	}
	assertEnum(t, s, "requirement", "enforcement", []string{"ui", "backend", "database", "external", "review"})
	if attr(t, s, "requirement", "enforcement").Required {
		t.Error("requirement.enforcement is required")
	}
	assertRelation(t, s, "requirement", "capabilities", []string{"capability"}, true, false)
	assertRelation(t, s, "requirement", "use_cases", []string{"use-case"}, true, false)
	assertRelation(t, s, "requirement", "realized_in", []string{"component"}, true, false)

	// adr is the only decision type; affects is the blast-radius query.
	assertEnum(t, s, "adr", "status", []string{"proposed", "accepted", "rejected"})
	if !attr(t, s, "adr", "status").Required {
		t.Error("adr.status is not required")
	}
	assertRelation(t, s, "adr", "supersedes", []string{"adr"}, false, false)
	if r := relation(t, s, "adr", "supersedes"); r.Inverse == nil || *r.Inverse != "superseded" || !r.Acyclic {
		t.Errorf("adr.supersedes inverse=%v acyclic=%v", r.Inverse, r.Acyclic)
	}
	if r := relation(t, s, "adr", "affects"); r.Kind != schema.KindAny || !r.Many {
		t.Errorf("adr.affects kind=%s many=%v", r.Kind, r.Many)
	}

	// component carries the ownership boundary (kind), who to ask (owner)
	// and whether to build on it (lifecycle, tier). repo is an edge to the
	// registry an external simply omits; consumes is the churny side of an
	// api, its consumers computed.
	assertEnum(t, s, "component", "kind", []string{"service", "library", "external"})
	if !attr(t, s, "component", "kind").Required {
		t.Error("component.kind is not required")
	}
	assertText(t, s, "component", "stack", false)
	assertText(t, s, "component", "owner", false)
	assertEnum(t, s, "component", "lifecycle", []string{"experimental", "production", "deprecated"})
	assertEnum(t, s, "component", "tier", []string{"tier-1", "tier-2", "tier-3"})
	for _, name := range []string{"lifecycle", "tier"} {
		if attr(t, s, "component", name).Required {
			t.Errorf("component.%s is required", name)
		}
	}
	if typeOf(t, s, "component").Attributes.Has("repo") {
		t.Error("component.repo is a text attribute, not an edge")
	}
	assertRelation(t, s, "component", "system", []string{"system"}, false, false)
	assertRelation(t, s, "component", "repo", []string{"repo"}, false, false)
	assertRelation(t, s, "component", "consumes", []string{"api"}, true, false)
	if r := relation(t, s, "component", "consumes"); r.Inverse == nil || *r.Inverse != "consumed_by" {
		t.Errorf("component.consumes inverse = %v", r.Inverse)
	}

	// api: provider is required so `check` catches an interface nobody owns.
	assertEnum(t, s, "api", "kind", []string{"rest", "graphql", "grpc", "events", "data"})
	assertEnum(t, s, "api", "status", []string{"proposed", "active", "deprecated"})
	for _, name := range []string{"kind", "status"} {
		if !attr(t, s, "api", name).Required {
			t.Errorf("api.%s is not required", name)
		}
	}
	assertRelation(t, s, "api", "provider", []string{"component"}, false, true)

	// repo is the codebase registry: org/name, status, a title to mint from;
	// ownership is derived from the components naming it, never stored.
	if a := attr(t, s, "repo", "repo"); a.BaseType != "text" || !a.Required ||
		a.Pattern == nil || *a.Pattern != `^[a-z0-9._-]+(/[a-z0-9._-]+)+$` {
		t.Errorf("repo.repo = %s required=%v pattern=%v", a.BaseType, a.Required, a.Pattern)
	}
	assertEnum(t, s, "repo", "status", []string{"active", "archived"})
	if !attr(t, s, "repo", "status").Required {
		t.Error("repo.status is not required")
	}
	if n := len(ownPredicates(s, "repo")); n != 0 {
		t.Errorf("repo declares %d relations", n)
	}

	// Inverses are computed at read time, never stored; status is always an
	// explicit statement.
	for _, name := range s.Types.Keys() {
		rtype, _ := s.Types.Get(name)
		for _, inverse := range []string{"superseded", "superseded_by", "consumed_by", "consumers"} {
			if rtype.Relations.Has(inverse) || rtype.Attributes.Has(inverse) {
				t.Errorf("%s stores the inverse %s", name, inverse)
			}
		}
	}
	for _, name := range []string{"adr", "api", "repo"} {
		if attr(t, s, name, "status").Default != nil {
			t.Errorf("%s.status carries a default", name)
		}
	}
}

func TestAliasResolves(t *testing.T) {
	// build-lite became build-hub in 0.6.0. The retired name still resolves —
	// kb's graduation command and every pre-0.6.0 config.yaml spell it — but
	// it is a name that resolves, not a preset anyone is offered.
	got, err := Resolve("build-lite", Embedded())
	if err != nil {
		t.Fatal(err)
	}
	if got != "build-hub/ontology.yaml" {
		t.Fatalf("Resolve(build-lite) = %q", got)
	}
	if c := Canonical("build-lite", Embedded()); c != "build-hub" {
		t.Errorf("Canonical(build-lite) = %q", c)
	}
	for _, name := range []string{"build-hub", "firm-ops", "bogus"} {
		if c := Canonical(name, Embedded()); c != name {
			t.Errorf("Canonical(%s) = %q, want it unchanged", name, c)
		}
	}
	for _, name := range Known(Embedded()) {
		if name == "build-lite" {
			t.Error("Known() lists the alias")
		}
	}
}

func TestEveryShippedPresetDeclaresACaptureTrigger(t *testing.T) {
	// `when` is only useful if every type has one — a gap is a type an agent
	// will not think to record.
	for _, preset := range Known(Embedded()) {
		s := resolvePreset(t, preset)
		for _, name := range s.Types.Keys() {
			ty, _ := s.Types.Get(name)
			if ty.When == nil || *ty.When == "" {
				t.Errorf("%s: type %s has no `when`", preset, name)
			}
		}
	}
}

func TestNoCueEmbedsAStorageLink(t *testing.T) {
	// The inversion of the pre-split rule. A cue used to author its own file
	// link, and a test held the link to the declared path — cross-layer string
	// coupling that broke whenever a file moved. Since the split the cue is
	// pure domain language (ontology) and `wire` derives the singleton's edit
	// link from its storage path at render time, so a moved file can never
	// strand a stale link. This holds the ontology side of that bargain: no
	// cue smuggles a markdown link back in.
	for _, preset := range Known(Embedded()) {
		s := resolvePreset(t, preset)
		for _, name := range s.Types.Keys() {
			ty, _ := s.Types.Get(name)
			if ty.When == nil {
				continue
			}
			if strings.Contains(*ty.When, "](") {
				t.Errorf("%s/%s: `when` embeds a markdown link; the layer split derives it from storage at render time", preset, name)
			}
		}
	}
}

// --- helpers -------------------------------------------------------------------

// resolvePreset materializes a preset's layer files out of the embedded tree
// and resolves them over the embedded core base — supplied as ResolveWith's
// base document, exactly as introspect.LoadSchema supplies it to a live
// workspace (an authored ontology.base is forbidden).
func resolvePreset(t *testing.T, preset string) *schema.ResolvedSchema {
	t.Helper()
	coreRaw, err := fs.ReadFile(Embedded(), CorePath)
	if err != nil {
		t.Fatal(err)
	}
	coreVal, err := canon.LoadDocMode(string(coreRaw), canon.Mode12)
	if err != nil {
		t.Fatalf("parse core base: %v", err)
	}
	coreDoc, ok := coreVal.(*omap.Map)
	if !ok {
		t.Fatalf("core base top level is %T, not a mapping", coreVal)
	}
	dir := t.TempDir()
	var paths []string
	for _, layer := range []string{OntologyFile, PolicyFile, StorageFile} {
		if _, err := fs.Stat(Embedded(), preset+"/"+layer); err != nil {
			continue // policy/storage are optional layers
		}
		dest := filepath.Join(dir, preset+"-"+layer)
		copyOut(t, preset+"/"+layer, dest)
		paths = append(paths, dest)
	}
	resolved, err := schema.ResolveWith(coreDoc, paths)
	if err != nil {
		t.Fatalf("%s: %v", preset, err)
	}
	return resolved
}

func copyOut(t *testing.T, name, dest string) {
	t.Helper()
	raw, err := fs.ReadFile(Embedded(), name)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, dest, string(raw))
}

func mustWrite(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o666); err != nil {
		t.Fatal(err)
	}
}

func assertTypeSet(t *testing.T, s *schema.ResolvedSchema, want []string) {
	t.Helper()
	got := append([]string(nil), s.Types.Keys()...)
	sort.Strings(got)
	wantSorted := append([]string(nil), want...)
	sort.Strings(wantSorted)
	if !reflect.DeepEqual(got, wantSorted) {
		t.Errorf("types = %v, want %v", got, wantSorted)
	}
}

// ownPredicates lists a type's relations that are not universal base edges —
// Python's `p for p in t.relations if p not in s.base_relations`.
func ownPredicates(s *schema.ResolvedSchema, typeName string) []string {
	rtype, ok := s.Types.Get(typeName)
	if !ok {
		return nil
	}
	var out []string
	for _, p := range rtype.Relations.Keys() {
		if !s.BaseRelations.Has(p) {
			out = append(out, p)
		}
	}
	return out
}

func assertPredicates(t *testing.T, s *schema.ResolvedSchema, want []string) {
	t.Helper()
	seen := map[string]bool{}
	for _, name := range s.Types.Keys() {
		for _, p := range ownPredicates(s, name) {
			seen[p] = true
		}
	}
	got := make([]string, 0, len(seen))
	for p := range seen {
		got = append(got, p)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("predicates = %v, want %v", got, want)
	}
}

func countDeclarations(s *schema.ResolvedSchema) int {
	n := 0
	for _, name := range s.Types.Keys() {
		n += len(ownPredicates(s, name))
	}
	return n
}

func typeOf(t *testing.T, s *schema.ResolvedSchema, name string) *schema.ResolvedType {
	t.Helper()
	ty, ok := s.Types.Get(name)
	if !ok {
		t.Fatalf("no type %s", name)
	}
	return ty
}

func relation(t *testing.T, s *schema.ResolvedSchema, typeName, predicate string) *schema.ResolvedRelation {
	t.Helper()
	rel, ok := typeOf(t, s, typeName).Relations.Get(predicate)
	if !ok {
		t.Fatalf("no relation %s.%s", typeName, predicate)
	}
	return rel
}

func attr(t *testing.T, s *schema.ResolvedSchema, typeName, name string) *schema.ResolvedAttribute {
	t.Helper()
	a, ok := typeOf(t, s, typeName).Attributes.Get(name)
	if !ok {
		t.Fatalf("no attribute %s.%s", typeName, name)
	}
	return a
}

func assertEnum(t *testing.T, s *schema.ResolvedSchema, typeName, name string, want []string) {
	t.Helper()
	if got := attr(t, s, typeName, name).Enum; !reflect.DeepEqual(got, want) {
		t.Errorf("%s.%s enum = %v, want %v", typeName, name, got, want)
	}
}

// assertRelation pins a typed edge's targets, cardinality and gate.
func assertRelation(t *testing.T, s *schema.ResolvedSchema, typeName, predicate string,
	targets []string, many, required bool) {
	t.Helper()
	r := relation(t, s, typeName, predicate)
	if r.Kind != schema.KindTyped || !reflect.DeepEqual(r.Targets, targets) ||
		r.Many != many || r.Required != required {
		t.Errorf("%s.%s = %s %v many=%v required=%v, want %v many=%v required=%v",
			typeName, predicate, r.Kind, r.Targets, r.Many, r.Required, targets, many, required)
	}
}

// assertText pins a free-text attribute and whether it is required.
func assertText(t *testing.T, s *schema.ResolvedSchema, typeName, name string, required bool) {
	t.Helper()
	a := attr(t, s, typeName, name)
	if a.BaseType != "text" || a.Required != required || a.Enum != nil {
		t.Errorf("%s.%s = %s required=%v enum=%v", typeName, name, a.BaseType, a.Required, a.Enum)
	}
}

func assertPath(t *testing.T, s *schema.ResolvedSchema, typeName, want string) {
	t.Helper()
	p := typeOf(t, s, typeName).Storage.Path
	if p == nil || *p != want {
		t.Errorf("%s path = %v, want %s", typeName, p, want)
	}
}

func TestAliasDoesNotShadowAUserDirectory(t *testing.T) {
	// The alias exists for the packaged tree. A --preset-source that carries
	// its own build-lite/ keeps the name: it resolves to that directory, is
	// recorded as itself, and the unknown-preset message never names a
	// directory the caller did not type.
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "build-lite", "ontology.yaml"),
		"version: \"1.0.0\"\nontology: {entities: {note: {}}}\n")
	src := Source(dir)
	if c := Canonical("build-lite", src); c != "build-lite" {
		t.Errorf("Canonical(build-lite, user source) = %q", c)
	}
	got, err := Resolve("build-lite", src)
	if err != nil {
		t.Fatal(err)
	}
	if got != "build-lite/ontology.yaml" {
		t.Errorf("Resolve = %q", got)
	}
	if k := Known(src); !reflect.DeepEqual(k, []string{"build-lite"}) {
		t.Errorf("Known = %v", k)
	}
}
