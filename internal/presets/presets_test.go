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
	want := []string{"build-hub", "build-lite", "firm-ops"}
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
	want := "Unknown preset 'bogus'. Known presets: build-hub, build-lite, firm-ops"
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
	// kb test_every_shipped_template_parses: every build-lite md type ships a
	// template — prd, arc42, adr and feature-spec carry a heading contract;
	// requirement (`sections: []`), component and repo (optional sections
	// only) carry a hint and lenses instead. firm-ops ships none at all.
	for _, tc := range []struct {
		preset string
		want   []string
	}{
		{"build-lite", []string{
			"adr.yaml", "arc42.yaml", "component.yaml", "feature-spec.yaml",
			"prd.yaml", "repo.yaml", "requirement.yaml",
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
	// Sixteen of build-hub's seventeen md types: component gained one with the
	// kb port; entity alone stays a frontmatter-shaped record with no body contract.
	if n := len(Templates("build-hub", Embedded())); n != 16 {
		t.Errorf("build-hub templates = %d, want 16", n)
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

func TestBuildLitePreset(t *testing.T) {
	s := resolvePreset(t, "build-lite")
	singletons := []string{"prd", "arc42"}

	assertTypeSet(t, s, []string{"prd", "arc42", "requirement", "adr", "component", "repo", "feature-spec"})
	// The thirteen build-hub types lite drops are absent, not renamed.
	cut := []string{
		"roadmap", "glossary", "erd", "capability", "pdr", "boundary",
		"quality-attribute", "domain", "entity", "contract",
		"baseline", "test-spec", "work-package",
	}
	if len(cut) != 13 {
		t.Fatalf("cut list drifted: %d", len(cut))
	}
	for _, name := range append(cut, "external-system") {
		if s.Types.Has(name) {
			t.Errorf("cut type %s is back", name)
		}
	}
	// Five predicate names beyond the universal four; six declarations
	// (supersedes is declared on both adr and feature-spec).
	assertPredicates(t, s, []string{"affects", "realized_in", "repo", "requirements", "supersedes"})
	if n := countDeclarations(s); n != 6 {
		t.Errorf("declarations = %d, want 6", n)
	}

	// Two narrative docs; prd is the required one — check fails without it.
	for _, name := range singletons {
		ty := typeOf(t, s, name)
		if ty.Storage.Layout != schema.LayoutSingleton || ty.Storage.Fmt != "md" {
			t.Errorf("%s storage = %s/%s", name, ty.Storage.Layout, ty.Storage.Fmt)
		}
		if a, ok := ty.Attributes.Get("title"); !ok || !a.Required {
			t.Errorf("%s.title is not required", name)
		}
		// Narrative roots sit above the top of the durability ladder: without
		// orphan: true they are permanent findings.
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
	for _, name := range []string{"requirement", "adr", "component", "repo", "feature-spec"} {
		if typeOf(t, s, name).Orphan {
			t.Errorf("%s opts out of the orphan sweep", name)
		}
		if got := typeOf(t, s, name).Storage.Layout; got != schema.LayoutFile {
			t.Errorf("%s layout = %s", name, got)
		}
	}

	// boundary and quality-attribute collapse into requirement.kind.
	assertEnum(t, s, "requirement", "kind", []string{"functional", "non-functional", "constraint", "business-rule"})
	if !attr(t, s, "requirement", "kind").Required {
		t.Error("requirement.kind is not required")
	}
	realized := relation(t, s, "requirement", "realized_in")
	if !reflect.DeepEqual(realized.Targets, []string{"component"}) || !realized.Many {
		t.Errorf("requirement.realized_in = %v many=%v", realized.Targets, realized.Many)
	}
	// component carries the ownership boundary; repo is an optional edge to
	// the registry (an external has none).
	assertEnum(t, s, "component", "kind", []string{"service", "library", "external"})
	if !attr(t, s, "component", "kind").Required {
		t.Error("component.kind is not required")
	}
	if typeOf(t, s, "component").Attributes.Has("repo") {
		t.Error("component.repo is still a text attribute")
	}
	if r := relation(t, s, "component", "repo"); !reflect.DeepEqual(r.Targets, []string{"repo"}) || r.Many || r.Required {
		t.Errorf("component.repo = %v many=%v required=%v", r.Targets, r.Many, r.Required)
	}
	if typeOf(t, s, "component").Relations.Has("consumes") {
		t.Error("component declares relation consumes")
	}
	// repo is the codebase registry: name, status, and a title to mint from.
	assertPath(t, s, "repo", "knowledge/repos")
	if a := attr(t, s, "repo", "repo"); a.BaseType != "text" || !a.Required ||
		a.Pattern == nil || *a.Pattern != `^[a-z0-9._-]+(/[a-z0-9._-]+)+$` {
		t.Errorf("repo.repo = %s required=%v pattern=%v", a.BaseType, a.Required, a.Pattern)
	}
	assertEnum(t, s, "repo", "status", []string{"active", "archived"})
	if !attr(t, s, "repo", "status").Required || !attr(t, s, "repo", "title").Required {
		t.Error("repo.status / repo.title are not both required")
	}
	// adr is the only decision type; affects is the blast-radius query.
	assertEnum(t, s, "adr", "status", []string{"proposed", "accepted", "rejected"})
	if !reflect.DeepEqual(relation(t, s, "adr", "supersedes").Targets, []string{"adr"}) {
		t.Error("adr.supersedes is not self-typed")
	}
	affects := relation(t, s, "adr", "affects")
	if affects.Kind != schema.KindAny || !affects.Many {
		t.Errorf("adr.affects kind=%s many=%v", affects.Kind, affects.Many)
	}
	// The FS is the work record: obligation only.
	assertEnum(t, s, "feature-spec", "status", []string{"planned", "active", "done", "dropped"})
	reqs := relation(t, s, "feature-spec", "requirements")
	if !reflect.DeepEqual(reqs.Targets, []string{"requirement"}) || !reqs.Many {
		t.Errorf("feature-spec.requirements = %v many=%v", reqs.Targets, reqs.Many)
	}
	if !reflect.DeepEqual(relation(t, s, "feature-spec", "supersedes").Targets, []string{"feature-spec"}) {
		t.Error("feature-spec.supersedes is not self-typed")
	}
	if typeOf(t, s, "feature-spec").Relations.Has("capabilities") {
		t.Error("feature-spec declares capabilities")
	}
	// superseded is computed; status is always an explicit statement.
	for _, name := range s.Types.Keys() {
		rtype, _ := s.Types.Get(name)
		if rtype.Relations.Has("superseded") {
			t.Errorf("%s stores an inverse", name)
		}
	}
	for _, name := range []string{"adr", "feature-spec"} {
		if attr(t, s, name, "status").Default != nil {
			t.Errorf("%s.status carries a default", name)
		}
	}
	// No collections: one knowledge root plus specs/.
	assertPath(t, s, "requirement", "knowledge/requirements")
	assertPath(t, s, "adr", "knowledge/decisions")
	assertPath(t, s, "component", "knowledge/components")
	assertPath(t, s, "feature-spec", "specs")
}

func TestBuildHubPreset(t *testing.T) {
	s := resolvePreset(t, "build-hub")
	singletons := []string{"prd", "roadmap", "glossary", "arc42", "erd"}
	all := append([]string{
		"capability", "requirement", "pdr", "adr", "domain", "entity", "boundary",
		"quality-attribute", "component", "repo", "contract",
		"baseline", "feature-spec", "test-spec", "work-package",
	}, singletons...)

	assertTypeSet(t, s, all)
	for _, name := range singletons {
		ty := typeOf(t, s, name)
		if ty.Storage.Layout != schema.LayoutSingleton || ty.Storage.Fmt != "md" {
			t.Errorf("%s storage = %s/%s", name, ty.Storage.Layout, ty.Storage.Fmt)
		}
		if a, ok := ty.Attributes.Get("title"); !ok || !a.Required {
			t.Errorf("%s.title is not required", name)
		}
		if !ty.Orphan {
			t.Errorf("%s does not opt out of the orphan sweep", name)
		}
		if name != "prd" && ty.Required {
			t.Errorf("%s is required", name)
		}
	}
	if !typeOf(t, s, "prd").Required {
		t.Error("prd is not required")
	}
	assertPath(t, s, "prd", "knowledge/product/prd.md")
	assertPath(t, s, "arc42", "knowledge/architecture/arc42.md")

	// Seventeen predicate names beyond the universal four; twenty-three
	// declarations (domain.depends_on narrows a base edge, so the base-name
	// filter excludes it from both counts).
	assertPredicates(t, s, []string{
		"affects", "applies_to", "capabilities", "component", "consumes", "domains",
		"drivers", "feature", "owner", "produces", "provider", "reads", "realized_in",
		"repo", "requirements", "supersedes", "verifies",
	})
	if n := countDeclarations(s); n != 23 {
		t.Errorf("declarations = %d, want 23", n)
	}
	d := relation(t, s, "domain", "depends_on")
	if !reflect.DeepEqual(d.Targets, []string{"domain"}) || d.Kind != schema.KindTyped || !d.Many {
		t.Errorf("domain.depends_on = %v %s many=%v", d.Targets, d.Kind, d.Many)
	}

	// entity.owner: exactly one authoritative writer per entity.
	owner := relation(t, s, "entity", "owner")
	if !reflect.DeepEqual(owner.Targets, []string{"domain"}) || !owner.Required || owner.Many {
		t.Errorf("entity.owner = %v required=%v many=%v", owner.Targets, owner.Required, owner.Many)
	}
	reads := relation(t, s, "domain", "reads")
	if !reflect.DeepEqual(reads.Targets, []string{"entity"}) || !reads.Many {
		t.Errorf("domain.reads = %v many=%v", reads.Targets, reads.Many)
	}

	// Governance layer.
	if !attr(t, s, "boundary", "rule").Required {
		t.Error("boundary.rule is not required")
	}
	assertEnum(t, s, "boundary", "scope", []string{"domain", "api", "data", "system"})
	if !attr(t, s, "quality-attribute", "scenario").Required {
		t.Error("quality-attribute.scenario is not required")
	}
	qa := relation(t, s, "quality-attribute", "applies_to")
	if qa.Kind != schema.KindUnion || !reflect.DeepEqual(qa.Targets, []string{"domain", "component"}) {
		t.Errorf("quality-attribute.applies_to = %s %v", qa.Kind, qa.Targets)
	}
	if !reflect.DeepEqual(relation(t, s, "adr", "produces").Targets, []string{"boundary"}) {
		t.Error("adr.produces does not target boundary")
	}
	if !reflect.DeepEqual(relation(t, s, "adr", "drivers").Targets, []string{"quality-attribute"}) {
		t.Error("adr.drivers does not target quality-attribute")
	}

	// Component topology; repo rows are a pure remotes record.
	compRepo := relation(t, s, "component", "repo")
	if !reflect.DeepEqual(compRepo.Targets, []string{"repo"}) || compRepo.Required {
		t.Errorf("component.repo = %v required=%v", compRepo.Targets, compRepo.Required)
	}
	if !reflect.DeepEqual(relation(t, s, "component", "domains").Targets, []string{"domain"}) {
		t.Error("component.domains does not target domain")
	}
	if !reflect.DeepEqual(relation(t, s, "component", "consumes").Targets, []string{"contract"}) {
		t.Error("component.consumes does not target contract")
	}
	if n := len(ownPredicates(s, "repo")); n != 0 {
		t.Errorf("repo declares %d relations", n)
	}
	assertEnum(t, s, "component", "kind", []string{"service", "library", "external"})
	if !attr(t, s, "component", "kind").Required {
		t.Error("component.kind is not required")
	}

	// The hub-authored yaml contract.
	contract := typeOf(t, s, "contract")
	if contract.Storage.Layout != schema.LayoutFile || contract.Storage.Fmt != "yaml" {
		t.Errorf("contract storage = %s/%s", contract.Storage.Layout, contract.Storage.Fmt)
	}
	provider := relation(t, s, "contract", "provider")
	if !reflect.DeepEqual(provider.Targets, []string{"component"}) || provider.Kind != schema.KindTyped ||
		!provider.Required || provider.Many {
		t.Errorf("contract.provider = %v %s required=%v many=%v",
			provider.Targets, provider.Kind, provider.Required, provider.Many)
	}
	if contract.Relations.Has("consumers") {
		t.Error("contract stores consumers")
	}

	// The work spine: feature-spec IS the feature record.
	for _, gone := range []string{"feature", "solution-spec", "external-system"} {
		if s.Types.Has(gone) {
			t.Errorf("type %s is back", gone)
		}
	}
	assertEnum(t, s, "feature-spec", "status", []string{"planned", "active", "done", "dropped"})
	if !reflect.DeepEqual(relation(t, s, "feature-spec", "capabilities").Targets, []string{"capability"}) {
		t.Error("feature-spec.capabilities does not target capability")
	}
	if !relation(t, s, "feature-spec", "requirements").Many {
		t.Error("feature-spec.requirements is not many")
	}
	ts := relation(t, s, "test-spec", "verifies")
	if !reflect.DeepEqual(ts.Targets, []string{"feature-spec"}) || !ts.Required {
		t.Errorf("test-spec.verifies = %v required=%v", ts.Targets, ts.Required)
	}
	wpFeature := relation(t, s, "work-package", "feature")
	if !reflect.DeepEqual(wpFeature.Targets, []string{"feature-spec"}) || !wpFeature.Required {
		t.Errorf("work-package.feature = %v required=%v", wpFeature.Targets, wpFeature.Required)
	}
	if relation(t, s, "work-package", "repo").Many {
		t.Error("work-package.repo is many")
	}
	if typeOf(t, s, "work-package").Relations.Has("requirements") {
		t.Error("work-package declares requirements")
	}

	// adr and pdr share the shape; no stored inverse anywhere.
	for _, name := range []string{"adr", "pdr"} {
		if !reflect.DeepEqual(relation(t, s, name, "supersedes").Targets, []string{name}) {
			t.Errorf("%s.supersedes is not self-typed", name)
		}
		if relation(t, s, name, "affects").Kind != schema.KindAny {
			t.Errorf("%s.affects is not an any edge", name)
		}
		assertEnum(t, s, name, "status", []string{"proposed", "accepted", "rejected"})
	}
	for _, name := range s.Types.Keys() {
		rtype, _ := s.Types.Get(name)
		if rtype.Relations.Has("superseded_by") {
			t.Errorf("%s stores an inverse", name)
		}
	}

	// facet_id rides the four import-target types; pdr carries none.
	for _, name := range []string{"capability", "requirement", "adr", "contract"} {
		if attr(t, s, name, "facet_id").BaseType != "text" {
			t.Errorf("%s.facet_id is not text", name)
		}
	}
	if typeOf(t, s, "pdr").Attributes.Has("facet_id") {
		t.Error("pdr carries facet_id")
	}

	// Coarse work statuses; no status field carries a default.
	for _, name := range []string{"feature-spec", "work-package"} {
		assertEnum(t, s, name, "status", []string{"planned", "active", "done", "dropped"})
	}
	assertEnum(t, s, "contract", "status", []string{"proposed", "active", "deprecated"})
	for _, name := range []string{"feature-spec", "work-package", "adr", "pdr", "contract", "repo"} {
		if attr(t, s, name, "status").Default != nil {
			t.Errorf("%s.status carries a default", name)
		}
	}

	// Two yaml registries; everything else per-item files.
	for _, name := range []string{"repo", "baseline"} {
		st := typeOf(t, s, name).Storage
		if st.Layout != schema.LayoutCollection || st.Fmt != "yaml" {
			t.Errorf("%s storage = %s/%s", name, st.Layout, st.Fmt)
		}
	}
	assertPath(t, s, "repo", "knowledge/architecture/repos.yaml")
	assertPath(t, s, "capability", "knowledge/product/capabilities")
	assertPath(t, s, "adr", "knowledge/architecture/decisions")
	assertPath(t, s, "pdr", "knowledge/product/decisions")
	assertPath(t, s, "feature-spec", "specs/feature-specs")
	assertPath(t, s, "test-spec", "specs/test-specs")
	assertPath(t, s, "work-package", "specs/work-packages")
	for _, name := range all {
		if name == "repo" || name == "baseline" || contains(singletons, name) {
			continue
		}
		if got := typeOf(t, s, name).Storage.Layout; got != schema.LayoutFile {
			t.Errorf("%s layout = %s", name, got)
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

func assertPath(t *testing.T, s *schema.ResolvedSchema, typeName, want string) {
	t.Helper()
	p := typeOf(t, s, typeName).Storage.Path
	if p == nil || *p != want {
		t.Errorf("%s path = %v, want %s", typeName, p, want)
	}
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
