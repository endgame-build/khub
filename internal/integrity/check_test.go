package integrity

// Ports the library-level `check` rows of tests/test_integrity.py (TS-INT-002)
// and tests/test_scan_hardening.py. CLI-level assertions are left to the golden
// fixtures.

import (
	"reflect"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/entity"
)

// TS-INT-002-01 (AC-001) + INT-SHARED-003: a sound graph passes both gates —
// including `stage: prospect`, a legal enum value the gate cannot know is
// semantically wrong.
func TestCheckSoundGraphAndFalseButLegal(t *testing.T) {
	root := cleanWS(t)
	if v := mustValidate(t, root, nil, false); !v.OK() {
		t.Fatalf("validate: %v", v.Errors)
	}
	if report := mustCheck(t, root, false); !report.Passed() {
		t.Fatalf("check failed: %+v", report)
	}
}

// TS-INT-002-U03/U04 (INT-004, INT-012): the flag and the verdict move apart —
// completeness comes from the schema, not the `draft` flag.
func TestCheckActiveIncompleteAndDraftExempt(t *testing.T) {
	root := cleanWS(t)
	create(t, root, "project", "orphaned-pov", "client", "initech")
	createDraft(t, root, "project", "draft-pov", "client", "initech")

	incomplete := incompleteByID(mustCheck(t, root, false))
	got, ok := incomplete["project/orphaned-pov"]
	if !ok {
		t.Fatalf("the active project is not incomplete: %v", incomplete)
	}
	if !reflect.DeepEqual(got.MissingRelations, []string{"owner"}) {
		t.Errorf("missing_relations = %v, want [owner]", got.MissingRelations)
	}
	if _, ok := incomplete["project/draft-pov"]; ok {
		t.Error("a draft with the same gap was reported")
	}
}

// TS-INT-002-U05 (INT-005): an active entity whose required owner is a draft
// fails; the draft itself stays exempt.
func TestCheckDraftTargetDoesNotSatisfy(t *testing.T) {
	root := cleanWS(t)
	createDraft(t, root, "person", "newhire", "name", "New Hire", "role", "consultant")
	create(t, root, "opportunity", "deal2",
		"stage", "prospect", "client", "initech", "owner", "newhire")

	incomplete := incompleteByID(mustCheck(t, root, false))
	got, ok := incomplete["opportunity/deal2"]
	if !ok {
		t.Fatalf("the active opportunity is not incomplete: %v", incomplete)
	}
	if !reflect.DeepEqual(got.MissingRelations, []string{"owner"}) {
		t.Errorf("missing_relations = %v, want [owner]", got.MissingRelations)
	}
	if _, ok := incomplete["person/newhire"]; ok {
		t.Error("the draft itself was reported incomplete")
	}
}

// Fix #5 (scan hardening): draft reads via as_bool — `draft: "false"` is
// PUBLISHED, `draft: yes` is a draft.
func TestCheckDraftStringReadsViaAsBool(t *testing.T) {
	root := freshWS(t)
	seed(t, root, "clients/c.md", kv{"type", "client"}, kv{"name", "C"},
		kv{"created", "2026-06-01"}, kv{"updated", "2026-06-01"})
	seed(t, root, "projects/pub/_index.md", kv{"type", "project"}, kv{"client", "c"},
		kv{"draft", "false"}, kv{"created", "2026-06-01"}, kv{"updated", "2026-06-01"})
	seed(t, root, "projects/hidden/_index.md", kv{"type", "project"}, kv{"client", "c"},
		kv{"draft", "yes"}, kv{"created", "2026-06-01"}, kv{"updated", "2026-06-01"})

	incomplete := incompleteByID(mustCheck(t, root, false))
	if _, ok := incomplete["project/pub"]; !ok {
		t.Error(`draft: "false" was treated as a draft`)
	}
	if _, ok := incomplete["project/hidden"]; ok {
		t.Error("draft: yes was treated as published")
	}
}

// gapsWS is test_integrity.py's gaps_ws: an orphan, a dangling edge (target
// force-removed), and a stray file.
func gapsWS(t *testing.T) string {
	t.Helper()
	root := cleanWS(t)
	create(t, root, "person", "orphan-person", "name", "Orphan", "role", "consultant")
	if _, err := entity.Delete(root, "client/initech", true); err != nil {
		t.Fatalf("delete: %v", err)
	}
	writeRaw(t, root, "clients/notes.md", "loose notes, not an entity\n")
	return root
}

// TS-INT-002-U01 / U06 / U07 (INT-006, INT-011, REQ-INT002-04): orphan,
// dangling, and stray in one sweep; a doc outside every layout is skipped.
func TestCheckReportsOrphanDanglingStray(t *testing.T) {
	report := mustCheck(t, gapsWS(t), false)

	if !containsID(report.Orphans, "person/orphan-person") {
		t.Errorf("orphans = %v", report.Orphans)
	}
	found := false
	for _, d := range report.Dangling {
		if d.ID() == "project/initech-pov" && d.Predicate == "client" && d.Target == "initech" {
			found = true
		}
	}
	if !found {
		t.Errorf("dangling = %v", report.Dangling)
	}
	if !anySuffix(report.Strays, "clients/notes.md") {
		t.Errorf("strays = %v", report.Strays)
	}
	for _, s := range report.Strays {
		if strings.Contains(s, "mission.md") {
			t.Errorf("a file outside every layout was reported stray: %s", s)
		}
	}
}

// Review #3: an edge pointing at a stray dangles instead of silently resolving.
func TestCheckEdgeToStrayDangles(t *testing.T) {
	root := cleanWS(t)
	writeRaw(t, root, "clients/initech.md", "---\ntype: person\nname: Sys\n---\n")

	report := mustCheck(t, root, false)
	found := false
	for _, d := range report.Dangling {
		if d.ID() == "project/initech-pov" && d.Predicate == "client" {
			found = true
		}
	}
	if !found {
		t.Errorf("dangling = %v", report.Dangling)
	}
	if !anySuffix(report.Strays, "clients/initech.md") {
		t.Errorf("strays = %v", report.Strays)
	}
}

// Review #9: an `any` relation stored as a qualified type/slug resolves.
func TestQualifiedAnyRelationResolves(t *testing.T) {
	root := cleanWS(t)
	create(t, root, "person", "w", "name", "W", "role", "consultant")
	create(t, root, "fragment", "frag",
		"stage", "raw", "owner", "w", "depends_on", "project/initech-pov")

	for _, d := range mustCheck(t, root, false).Dangling {
		if d.ID() == "fragment/frag" {
			t.Fatalf("a qualified any-edge dangled: %v", d)
		}
	}
}

// --- cycles ------------------------------------------------------------------

// TS-INT-002-U08 (REQ-INT002-04): a depends_on cycle terminates and returns the
// exact ids. Only set membership and count are contract — rotation is
// implementation-defined on both sides.
func TestCheckDependsOnCycle(t *testing.T) {
	root := freshWS(t)
	create(t, root, "person", "writer", "name", "Writer", "role", "consultant")
	for _, pair := range [][2]string{{"a", "b"}, {"b", "c"}, {"c", "a"}} {
		seed(t, root, "fragments/"+pair[0]+".md", kv{"type", "fragment"}, kv{"stage", "raw"},
			kv{"owner", "writer"}, kv{"depends_on", []any{pair[1]}}, kv{"created", "2026-06-01"})
	}
	cycles := mustCheck(t, root, false).Cycles
	if len(cycles) != 1 {
		t.Fatalf("cycles = %v, want exactly one", cycles)
	}
	want := []string{"fragment/a", "fragment/b", "fragment/c"}
	if got := cycleSet(cycles[0]); !reflect.DeepEqual(got, want) {
		t.Fatalf("cycle = %v, want %v", got, want)
	}
}

// Fix #9 (scan hardening): a stored self-reference on an acyclic predicate is a
// one-node cycle — BuildGraph skips self-edges, so it never reaches the
// detector.
func TestCheckSelfLoopIsACycle(t *testing.T) {
	root := freshWS(t)
	seed(t, root, "clients/selfie.md", kv{"type", "client"}, kv{"name", "Selfie"},
		kv{"depends_on", []any{"selfie"}}, kv{"created", "2026-06-01"},
		kv{"updated", "2026-06-01"})

	found := false
	for _, c := range mustCheck(t, root, false).Cycles {
		if reflect.DeepEqual(c, []string{"client/selfie"}) {
			found = true
		}
	}
	if !found {
		t.Fatalf("no self-cycle reported: %v", mustCheck(t, root, false).Cycles)
	}
}

// 0.11.0: cycle detection was hardcoded to depends_on, so three ADRs each
// superseding the next passed clean. build-hub marks `supersedes` acyclic.
func TestCheckSupersedesCycle(t *testing.T) {
	root := wsFor(t, "build-hub")
	for _, id := range []string{"ad-1", "ad-2", "ad-3"} {
		create(t, root, "adr", id, "title", strings.ToUpper(id), "status", "accepted")
	}
	link(t, root, "ad-3", "supersedes", "ad-2")
	link(t, root, "ad-2", "supersedes", "ad-1")
	link(t, root, "ad-1", "supersedes", "ad-3") // closes the cycle

	report := mustCheck(t, root, false)
	if report.Passed() {
		t.Fatal("a supersedes cycle passed the gate")
	}
	if len(report.Cycles) != 1 {
		t.Fatalf("cycles = %v, want exactly one", report.Cycles)
	}
	want := []string{"adr/ad-1", "adr/ad-2", "adr/ad-3"}
	if got := cycleSet(report.Cycles[0]); !reflect.DeepEqual(got, want) {
		t.Fatalf("cycle = %v, want %v", got, want)
	}
}

// A hand-written self-supersession must be reported for EVERY acyclic
// predicate, not just depends_on.
func TestCheckSelfSupersessionIsReported(t *testing.T) {
	root := wsFor(t, "build-hub")
	seed(t, root, "knowledge/architecture/decisions/solo.md",
		kv{"type", "adr"}, kv{"title", "Solo"}, kv{"status", "accepted"},
		kv{"supersedes", "solo"}, kv{"created", "2026-01-01"})

	found := false
	for _, c := range mustCheck(t, root, false).Cycles {
		if reflect.DeepEqual(c, []string{"adr/solo"}) {
			found = true
		}
	}
	if !found {
		t.Fatal("a self-superseding ADR was not reported")
	}
}

// Regression guard: generalising the check must not lose the original
// predicate, and a workspace made before `acyclic:` existed must keep it.
func TestCheckDependsOnStaysAcyclicWithAndWithoutTheFlag(t *testing.T) {
	for _, stripFlag := range []bool{false, true} {
		name := "with acyclic flag"
		if stripFlag {
			name = "pre-0.11 workspace, flag absent"
		}
		t.Run(name, func(t *testing.T) {
			root := wsFor(t, "build-hub")
			if stripFlag {
				// The base block (and its acyclic flag) is embedded in the
				// binary now, so the workspace files are what a stripped
				// schema looks like; assert none of them smuggles the flag
				// back in. build-hub's domain.depends_on redeclares the edge
				// WITHOUT acyclic (relations whole-replace), so cycle
				// detection below rests on the built-in backstop alone.
				text := readRaw(t, root, ".khub/ontology.yaml")
				text = strings.ReplaceAll(text, ", acyclic: true", "")
				text = strings.ReplaceAll(text, "acyclic: true", "")
				writeRaw(t, root, ".khub/ontology.yaml", text)
				if strings.Contains(readRaw(t, root, ".khub/ontology.yaml"), "acyclic") {
					t.Fatal("the flag survived the strip")
				}
			}
			for _, name := range []string{"alpha", "beta", "gamma"} {
				create(t, root, "domain", name, "title", name)
			}
			link(t, root, "alpha", "depends_on", "beta")
			link(t, root, "beta", "depends_on", "gamma")
			link(t, root, "gamma", "depends_on", "alpha")

			found := false
			for _, c := range mustCheck(t, root, false).Cycles {
				if len(c) == 3 {
					found = true
				}
			}
			if !found {
				t.Fatalf("no 3-node depends_on cycle: %v", mustCheck(t, root, false).Cycles)
			}
		})
	}
}

// --- orphans: informational by default, gated under --strict -----------------

func TestOrphansAreInformationalUntilStrict(t *testing.T) {
	root := cleanWS(t)
	create(t, root, "client", "dormant-co", "name", "Dormant Co")

	open := mustCheck(t, root, false)
	if !containsID(open.Orphans, "client/dormant-co") {
		t.Fatalf("orphans = %v", open.Orphans)
	}
	if !open.Passed() {
		t.Error("a disconnected entity failed the default gate")
	}
	strict := mustCheck(t, root, true)
	if !containsID(strict.Orphans, "client/dormant-co") {
		t.Fatalf("strict orphans = %v", strict.Orphans)
	}
	if strict.Passed() {
		t.Error("--strict passed with an orphan")
	}
}

// `orphan: true` exempts a type from the sweep — the regression the flag exists
// for is that a correct, freshly-initialised workspace can satisfy --strict.
func TestOrphanFlaggedTypesAreExempt(t *testing.T) {
	root := wsFor(t, "build-lite") // prd + arc42, both orphan: true, edge-less
	fresh := mustCheck(t, root, false)
	if len(fresh.Orphans) != 0 {
		t.Fatalf("orphans = %v, want none", fresh.Orphans)
	}
	if !fresh.Passed() {
		t.Fatalf("a fresh workspace failed the gate: %+v", fresh)
	}
	if !mustCheck(t, root, true).Passed() {
		t.Fatal("a fresh workspace failed --strict")
	}

	// The exemption is narrow: an edge-less entity of an unflagged type in the
	// same workspace is still an orphan, and still fails --strict.
	create(t, root, "component", "cmp-dangling", "title", "Dangling Service", "kind", "service")
	report := mustCheck(t, root, false)
	if !reflect.DeepEqual(report.Orphans, []string{"component/cmp-dangling"}) {
		t.Fatalf("orphans = %v", report.Orphans)
	}
	if !report.Passed() {
		t.Error("the orphan failed the default gate")
	}
	if mustCheck(t, root, true).Passed() {
		t.Error("--strict passed with an unflagged orphan")
	}
}

// The flag removes no signal: a flagged type missing a required field is still
// active-but-incomplete, which names the field the orphan line never did.
func TestOrphanFlaggedTypeStillReportsCompleteness(t *testing.T) {
	root := wsFor(t, "build-lite")
	writeRaw(t, root, "knowledge/prd.md", "---\ntype: prd\ncreated: 2026-06-01\ndraft: false\n---\n")

	incomplete := incompleteByID(mustCheck(t, root, false))
	got, ok := incomplete["prd/prd"]
	if !ok {
		t.Fatalf("prd/prd not reported: %v", incomplete)
	}
	if !reflect.DeepEqual(got.MissingFields, []string{"title"}) {
		t.Errorf("missing_fields = %v, want [title]", got.MissingFields)
	}
}

// The distinction the flag buys over keying on layout: a singleton that does NOT
// declare `orphan: true` is swept like any other type.
func TestUnflaggedSingletonIsStillSwept(t *testing.T) {
	root := t.TempDir()
	presetDir := t.TempDir()
	writeRaw(t, presetDir, "mini/ontology.yaml",
		"version: '0.1.0'\n"+
			"ontology:\n"+
			"  entities:\n"+
			"    charter:\n"+
			"      attributes:\n"+
			"        title: { required: true }\n")
	// no policy.yaml: no `orphan: true` — deliberately
	writeRaw(t, presetDir, "mini/storage.yaml",
		"storage:\n"+
			"  charter: { layout: singleton, path: charter.md }\n")
	initFrom(t, root, "mini", presetDir)
	// init mints a singleton only from a body template, and `mini` ships none.
	create(t, root, "charter", "", "title", "Charter")

	report := mustCheck(t, root, false)
	if !reflect.DeepEqual(report.Orphans, []string{"charter/charter"}) {
		t.Fatalf("orphans = %v", report.Orphans)
	}
	if mustCheck(t, root, true).Passed() {
		t.Error("--strict passed with an unflagged singleton orphan")
	}
}

// --- singletons: missing vs drafted ------------------------------------------

// A drafted REQUIRED singleton fails the gate — an unpublished PRD must not turn
// the whole gate green — and "drafted" is never folded into "missing".
func TestDraftRequiredSingletonFailsAndIsNotMissing(t *testing.T) {
	root := wsFor(t, "build-hub")
	if got := mustCheck(t, root, false).DraftRequiredSingletons; len(got) != 0 {
		t.Fatalf("draft_required_singletons = %v on a published workspace", got)
	}

	update(t, root, "prd", "draft", "true")
	drafted := mustCheck(t, root, false)
	if !reflect.DeepEqual(drafted.DraftSingletons, []string{"prd"}) {
		t.Errorf("draft_singletons = %v", drafted.DraftSingletons)
	}
	if !reflect.DeepEqual(drafted.DraftRequiredSingletons, []string{"prd"}) {
		t.Errorf("draft_required_singletons = %v", drafted.DraftRequiredSingletons)
	}
	if len(drafted.MissingSingletons) != 0 {
		t.Errorf("drafted was folded into missing: %v", drafted.MissingSingletons)
	}
	if drafted.Passed() {
		t.Error("a drafted required singleton passed the gate")
	}

	update(t, root, "prd", "draft", "false")
	if _, err := entity.Delete(root, "prd", false); err != nil {
		t.Fatalf("delete prd: %v", err)
	}
	absent := mustCheck(t, root, false)
	if !reflect.DeepEqual(absent.MissingSingletons, []string{"prd"}) {
		t.Errorf("missing_singletons = %v", absent.MissingSingletons)
	}
	if len(absent.DraftSingletons) != 0 {
		t.Errorf("draft_singletons = %v on an absent singleton", absent.DraftSingletons)
	}
}

// A drafted NON-required singleton is reported but does not fail: reporting it
// nowhere let `check` pass while the workspace had quietly lost a document.
func TestDraftOptionalSingletonIsInformational(t *testing.T) {
	root := wsFor(t, "build-lite") // arc42 is a singleton, but not required
	update(t, root, "arc42", "draft", "true")

	report := mustCheck(t, root, false)
	if !reflect.DeepEqual(report.DraftSingletons, []string{"arc42"}) {
		t.Errorf("draft_singletons = %v", report.DraftSingletons)
	}
	if len(report.DraftRequiredSingletons) != 0 {
		t.Errorf("draft_required_singletons = %v", report.DraftRequiredSingletons)
	}
	if len(report.MissingSingletons) != 0 {
		t.Errorf("missing_singletons = %v", report.MissingSingletons)
	}
	if !report.Passed() {
		t.Errorf("an optional drafted singleton failed the gate: %+v", report)
	}
}

// --- malformed ---------------------------------------------------------------

// Fix #1 (scan hardening): check lists the malformed path and fails the gate,
// without raising.
func TestCheckReportsMalformedAndFails(t *testing.T) {
	report := mustCheck(t, malformedWS(t), false)
	if !anySuffix(report.Malformed, "clients/broken.md") {
		t.Errorf("malformed = %v", report.Malformed)
	}
	if report.Passed() {
		t.Error("a malformed file passed the gate")
	}
}

// A malformed COLLECTION file removes every row of its type at once, so the
// edges into it are suppressed and counted rather than burying the real error.
func TestMalformedCollectionSuppressesDerivativeDangles(t *testing.T) {
	root := wsFor(t, "build-hub")
	create(t, root, "repo", "svc-a", "repo", "acme/a", "status", "active")
	create(t, root, "component", "cmp-api", "title", "API", "kind", "service", "repo", "svc-a")

	if before := mustCheck(t, root, false); len(before.Dangling) != 0 {
		t.Fatalf("dangling before the break: %v", before.Dangling)
	}
	collection := "knowledge/architecture/repos.yaml"
	writeRaw(t, root, collection,
		readRaw(t, root, collection)+"svc-a:\n  repo: acme/dup\n  status: active\n")

	report := mustCheck(t, root, false)
	if report.Passed() {
		t.Fatal("a malformed collection passed the gate")
	}
	if report.SuppressedDangling == 0 {
		t.Errorf("suppressed_dangling = 0, dangling = %v", report.Dangling)
	}
	for _, d := range report.Dangling {
		if d.Target == "svc-a" {
			t.Errorf("a derivative dangle survived: %v", d)
		}
	}
}

// --- misplaced ---------------------------------------------------------------

// The one breakage a schema-driven scan is blind to by construction: move a
// type's declared path and the old files are not absent, they are unscanned.
func TestMisplacedReportsAFileTheScanCannotReach(t *testing.T) {
	root := wsFor(t, "build-lite")
	create(t, root, "component", "", "title", "API", "kind", "service")

	// The schema now looks somewhere else; the file does not move.
	text := readRaw(t, root, ".khub/storage.yaml")
	writeRaw(t, root, ".khub/storage.yaml", strings.ReplaceAll(text,
		"path: knowledge/components", "path: knowledge/architecture/components"))

	report := mustCheck(t, root, false)
	if report.Passed() {
		t.Fatal("an unscanned entity passed the gate")
	}
	want := []Misplaced{{
		Path:     "knowledge/components/cmp-api.md",
		Type:     "component",
		Expected: "knowledge/architecture/components",
	}}
	if !reflect.DeepEqual(report.Misplaced, want) {
		t.Fatalf("misplaced = %+v, want %+v", report.Misplaced, want)
	}
}

// Narrow on purpose: it takes a file positively claiming a KNOWN type from
// outside every layout.
func TestMisplacedIgnoresEverythingThatIsNotAClaim(t *testing.T) {
	root := wsFor(t, "build-lite")
	writeRaw(t, root, "README.md", "# A repo\n\nNo frontmatter here.\n")
	writeRaw(t, root, "notes.md", "---\ntype: meeting\ntitle: Not our type\n---\n")
	writeRaw(t, root, "docs/guide.md", "---\ntitle: No type key\n---\n")

	if got := mustCheck(t, root, false).Misplaced; len(got) != 0 {
		t.Fatalf("misplaced = %+v, want none", got)
	}
}

// The two findings are mirrors and must never double-report the same file.
func TestFileInsideALayoutStaysAStray(t *testing.T) {
	root := wsFor(t, "build-lite")
	writeRaw(t, root, "knowledge/components/wrong.md",
		"---\ntype: adr\ntitle: In the wrong layout\nstatus: proposed\ncreated: 2026-07-25\n---\n")

	report := mustCheck(t, root, false)
	if !reflect.DeepEqual(report.Strays, []string{"knowledge/components/wrong.md"}) {
		t.Errorf("strays = %v", report.Strays)
	}
	if len(report.Misplaced) != 0 {
		t.Errorf("misplaced = %+v, want none (it is a stray)", report.Misplaced)
	}
}
