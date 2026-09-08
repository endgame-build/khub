package backfill

// Ports the library-level STORY-PRJ-002 rows of tests/test_projection.py. CLI
// -level assertions (the "Backfilled dates on N entities" line) are left to the
// golden fixtures.

import (
	"reflect"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/integrity"
)

// TS-PRJ-002-U01 (REQ-PRJ002-01): an entity missing created/updated is flagged
// for both, in that order.
func TestMissingFieldDetector(t *testing.T) {
	report := mustBackfill(t, datedGitWS(t), nil, true)
	changes := changeKeys(report)
	for _, field := range []string{"created", "updated"} {
		c, ok := changes[[2]string{"undated", field}]
		if !ok {
			t.Fatalf("no change for (undated, %s); got %+v", field, report.Changes)
		}
		if c.Source != SourceGit {
			t.Errorf("(undated, %s) source = %q, want %q", field, c.Source, SourceGit)
		}
	}
	if !report.DryRun || len(report.Changes) == 0 {
		t.Errorf("report = %+v", report)
	}
}

// TS-PRJ-002-U03 (REQ-PRJ002-01, PRJ-004): created ← first commit, updated ←
// last, and the two genuinely differ (not a last-commit alias).
func TestGitDateMapperFirstAndLast(t *testing.T) {
	root := datedGitWS(t)
	report := mustBackfill(t, root, nil, false)

	created := field(t, root, "fragments/undated.md", "created")
	updated := field(t, root, "fragments/undated.md", "updated")
	if got := isoOf(created); got != "2026-01-01" {
		t.Errorf("created = %v, want 2026-01-01 (the first commit)", created)
	}
	if got := isoOf(updated); got != "2026-03-01" {
		t.Errorf("updated = %v, want 2026-03-01 (the last commit)", updated)
	}
	if isoOf(created) == isoOf(updated) {
		t.Error("created and updated collapsed to one date")
	}
	// undated (created+updated) + dated (updated) = 2 entities dated.
	if got := report.DatedEntities(); got != 2 {
		t.Errorf("dated entities = %d, want 2", got)
	}
	if got := report.ScaffoldedEntities(); got != 0 {
		t.Errorf("scaffolded entities = %d, want 0 with no --type", got)
	}
}

// TS-PRJ-002-U02 / U04 (REQ-PRJ002-02, PRJ-003, PRJ-SHARED-002): an authored
// created is never overwritten, key order survives, and the one missing field is
// still added.
func TestPreserveExistingAndKeyOrder(t *testing.T) {
	root := datedGitWS(t)
	mustBackfill(t, root, nil, false)

	text := readRaw(t, root, "fragments/dated.md")
	if got := isoOf(field(t, root, "fragments/dated.md", "created")); got != "2025-01-01" {
		t.Errorf("created = %q, want the hand-kept 2025-01-01", got)
	}
	keys := metaOf(t, root, "fragments/dated.md").Keys()
	want := []string{"type", "stage", "created", "updated"}
	if !reflect.DeepEqual(keys, want) {
		t.Errorf("key order = %v, want %v", keys, want)
	}
	if !strings.Contains(text, "updated:") {
		t.Error("the one missing field was not added")
	}
	if !strings.Contains(text, "body") {
		t.Error("the body did not survive the round trip")
	}
	// The comment half of test_round_trip_preserves_order_and_comment: ruamel
	// keeps the hand-kept inline comment across the round trip, and so does
	// canon's splice write path.
	if !strings.Contains(text, "created: 2025-01-01  # hand-kept, do not touch") {
		t.Errorf("the hand-kept comment vanished:\n%s", text)
	}
}

// TS-PRJ-002-U05 (REQ-PRJ002-04): a workspace with no git history writes no
// dates and says so.
func TestNoGitFallbackSkipsDates(t *testing.T) {
	root := freshWS(t)
	seed(t, root, "fragments/undated.md", kv{"type", "fragment"}, kv{"stage", "raw"})
	report := mustBackfill(t, root, nil, false)

	if report.GitAvailable {
		t.Error("git_available = true without a repo")
	}
	for _, c := range report.Changes {
		if c.Source == SourceGit {
			t.Errorf("a git-sourced change without git: %+v", c)
		}
	}
	meta := metaOf(t, root, "fragments/undated.md")
	for _, key := range []string{"created", "updated"} {
		if _, ok := meta.Get(key); ok {
			t.Errorf("%s was written without git history", key)
		}
	}
}

// TS-PRJ-002-U06 (REQ-PRJ002-03): dry-run lists the changes and touches no file.
func TestDryRunCollectorWritesNothing(t *testing.T) {
	root := datedGitWS(t)
	before := snapshot(t, root+"/fragments")

	report := mustBackfill(t, root, nil, true)
	if !report.DryRun || len(report.Changes) == 0 {
		t.Fatalf("report = %+v", report)
	}
	after := snapshot(t, root+"/fragments")
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("dry run changed files:\n%v\n---\n%v", before, after)
	}
}

// TS-PRJ-002-02 (AC-002): --type scaffolds the incomplete entity, leaves the
// valid one byte-unchanged, and the null placeholders keep the tree
// validate-clean (check still flags the gaps).
func TestTypeScaffolding(t *testing.T) {
	root := scaffoldWS(t)
	valid := "opportunities/initech-deal/_index.md"
	before := readRaw(t, root, valid)

	report := mustBackfill(t, root, ptr("opportunity"), false)
	if got := report.ScaffoldedEntities(); got != 1 {
		t.Errorf("scaffolded entities = %d, want 1", got)
	}

	legacy := metaOf(t, root, "opportunities/legacy-deal/_index.md")
	for _, key := range []string{"name", "stage", "owner", "created", "updated"} {
		v, ok := legacy.Get(key)
		if !ok {
			t.Errorf("%s was not scaffolded (keys %v)", key, legacy.Keys())
			continue
		}
		if v != nil {
			t.Errorf("%s scaffolded as %v, want a null placeholder", key, v)
		}
	}
	if readRaw(t, root, valid) != before {
		t.Error("an already-valid entity was rewritten")
	}

	// null keeps validate clean; the gaps stay check's finding.
	report2, err := integrity.Validate(root, nil, false)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !report2.OK() {
		t.Fatalf("scaffolding broke validate: %v", report2.Errors)
	}
	check, err := integrity.Check(root, false)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	found := false
	for _, i := range check.Incomplete {
		if i.ID() == "opportunity/legacy-deal" {
			found = true
		}
	}
	if !found {
		t.Error("check stopped reporting the scaffolded gaps")
	}
}

// Finding #5: a misspelled --type fails loudly instead of a silent no-op
// success.
func TestUnknownTypeFailsLoudly(t *testing.T) {
	_, err := Backfill(scaffoldWS(t), ptr("opportunty"), false)
	if err == nil {
		t.Fatal("a misspelled --type scaffolded nothing and reported success")
	}
	if code := locatedCode(err); code != "unknown_type" {
		t.Fatalf("code = %q, want unknown_type", code)
	}
	if !strings.Contains(err.Error(), "opportunty") {
		t.Errorf("message does not name the typo: %s", err)
	}
}

// A collection type is skipped and REPORTED — a shared file's commit dates are
// not per-row dates. The dry run must preview exactly what the real run says.
func TestCollectionTypesAreSkippedAndReported(t *testing.T) {
	for _, dryRun := range []bool{true, false} {
		name := "real run"
		if dryRun {
			name = "dry run"
		}
		t.Run(name, func(t *testing.T) {
			root := collectionWS(t)
			create(t, root, "repo", "svc-a", "repo", "acme/a", "status", "active")
			report := mustBackfill(t, root, nil, dryRun)
			if !containsStr(report.SkippedCollections, "repo") {
				t.Fatalf("skipped_collections = %v, want it to name repo", report.SkippedCollections)
			}
			for _, c := range report.Changes {
				if c.Type == "repo" {
					t.Errorf("a collection row was written: %+v", c)
				}
			}
		})
	}
}

// The change record renders a scaffolded placeholder as "" (never "None") and a
// date as its ISO form.
func TestChangeValueRendering(t *testing.T) {
	root := scaffoldWS(t)
	changes := changeKeys(mustBackfill(t, root, ptr("opportunity"), true))
	c, ok := changes[[2]string{"legacy-deal", "stage"}]
	if !ok {
		t.Fatalf("no scaffold change for stage")
	}
	if c.Value != "" || c.Source != SourceScaffold {
		t.Errorf("change = %+v, want an empty value from %q", c, SourceScaffold)
	}

	gitRoot := datedGitWS(t)
	dates := changeKeys(mustBackfill(t, gitRoot, nil, true))
	if got := dates[[2]string{"undated", "created"}].Value; got != "2026-01-01" {
		t.Errorf("created value = %q, want the ISO date", got)
	}
}

func isoOf(v any) string {
	switch x := v.(type) {
	case canon.Date:
		return x.ISO
	case string:
		return x
	default:
		return ""
	}
}
