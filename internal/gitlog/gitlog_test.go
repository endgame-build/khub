package gitlog

// Ports the library-level rows of tests/test_gitlog.py (TS-INT-003) plus the
// stale row of tests/test_scan_hardening.py (Fix #7). CLI-level assertions are
// left to the golden fixtures.

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// staleWS is test_gitlog.py's stale_ws: four fragments spanning the 30- and
// 90-day windows from the 2026-06-27 run.
func staleWS(t *testing.T) string {
	t.Helper()
	root := freshWS(t)
	for _, f := range []struct{ slug, updated string }{
		{"older-note", "2025-12-01"},
		{"old-note", "2026-01-10"},
		{"mid-note", "2026-04-01"},
		{"fresh-note", "2026-06-20"},
	} {
		seed(t, root, "fragments/"+f.slug+".md",
			kv{"type", "fragment"}, kv{"stage", "raw"}, kv{"updated", f.updated})
	}
	return root
}

func TestStaleThresholdWindows(t *testing.T) {
	root := staleWS(t)
	cases := []struct {
		name string
		days *int
		want []string
	}{
		// TS-INT-003-U01: a 30-day window returns the past set, fresh excluded.
		{"thirty days", ptr(30), []string{"older-note", "old-note", "mid-note"}},
		// TS-INT-003-U03 (REQ-INT003-02, INT-007): --days 90 shifts the window.
		{"ninety days", ptr(90), []string{"older-note", "old-note"}},
		// Fix #7: a nil threshold resolves to the workspace stale_days (90), not
		// a private 30-day default.
		{"workspace default", nil, []string{"older-note", "old-note"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := slugSet(mustStale(t, root, tc.days))
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for _, slug := range tc.want {
				if !got[slug] {
					t.Fatalf("missing %q in %v", slug, got)
				}
			}
		})
	}
}

// TS-INT-003-U02: the stale set is sorted oldest first.
func TestStaleSortedOldestFirst(t *testing.T) {
	got := slugOrder(mustStale(t, staleWS(t), ptr(30)))
	want := []string{"older-note", "old-note", "mid-note"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// undatedGitWS is test_gitlog.py's undated_git_ws: a fragment with no `updated`,
// committed with a >30-day-old date.
func undatedGitWS(t *testing.T) string {
	t.Helper()
	root := freshWS(t)
	seed(t, root, "fragments/undated.md", kv{"type", "fragment"}, kv{"stage", "raw"})
	gitInit(t, root)
	commit(t, root, "seed", "2026-01-01T12:00:00")
	return root
}

// TS-INT-003-U04 (REQ-INT003-03): a missing `updated` is read from git for the
// comparison.
func TestStaleBackfillsGitDate(t *testing.T) {
	report := mustStale(t, undatedGitWS(t), nil)
	var found *StaleEntry
	for i := range report.Entries {
		if report.Entries[i].Slug == "undated" {
			found = &report.Entries[i]
		}
	}
	if found == nil {
		t.Fatalf("undated not reported stale: %v", slugOrder(report))
	}
	if found.Source != "git log" {
		t.Errorf("source = %q, want %q", found.Source, "git log")
	}
	want := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if !found.EffectiveDate.Equal(want) {
		t.Errorf("effective date = %v, want %v", found.EffectiveDate, want)
	}
	if !report.GitAvailable {
		t.Error("git_available = false in a repo with history")
	}
}

// TS-INT-003-U05 (INT-008): the git date is read, never written back to disk.
func TestStaleIsReadOnly(t *testing.T) {
	root := undatedGitWS(t)
	path := filepath.Join(root, "fragments", "undated.md")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	mustStale(t, root, nil)
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("file changed:\n%s\n---\n%s", before, after)
	}
	if strings.Contains(string(after), "updated") {
		t.Error("an `updated` key was written back")
	}
}

// TS-INT-003-04 (AC-004, REQ-INT003-04): a non-git workspace falls back to
// `updated` alone and says git was unavailable.
func TestStaleNoGitFallback(t *testing.T) {
	report := mustStale(t, staleWS(t), nil)
	if report.GitAvailable {
		t.Error("git_available = true in a workspace with no repo")
	}
}

// Review #5: stale and status share one staleness definition (created fallback).
func TestStaleUsesCreatedLikeStatus(t *testing.T) {
	root := freshWS(t)
	seed(t, root, "fragments/c.md",
		kv{"type", "fragment"}, kv{"stage", "raw"}, kv{"created", "2025-01-01"})
	report := mustStale(t, root, nil)
	for _, e := range report.Entries {
		if e.Slug == "c" {
			if e.Source != "created" {
				t.Fatalf("source = %q, want %q", e.Source, "created")
			}
			return
		}
	}
	t.Fatalf("c not reported stale: %v", slugOrder(report))
}

// A collection row never takes the shared file's commit date: any row's edit
// bumps it, so attributing it would make every other row read never-stale.
func TestCollectionRowsSkipGitDates(t *testing.T) {
	root := collectionWS(t)
	writeRaw(t, root, "knowledge/architecture/repos.yaml",
		"svc-a:\n  type: repo\n  repo: acme/a\n  status: active\n")
	gitInit(t, root)
	commit(t, root, "seed", "2020-01-01T12:00:00")

	report := mustStale(t, root, ptr(30))
	for _, e := range report.Entries {
		if e.Type == "repo" {
			t.Fatalf("collection row %s took the file's commit date (%v)", e.Slug, e.EffectiveDate)
		}
	}
}

// --- git helpers -------------------------------------------------------------

func TestHasGitHistory(t *testing.T) {
	plain := freshWS(t)
	if ok, err := HasGitHistory(plain); err != nil || ok {
		t.Fatalf("HasGitHistory(non-repo) = %v, %v; want false, nil", ok, err)
	}
	// An unborn HEAD reads the same as a non-repo: neither has history.
	unborn := freshWS(t)
	gitInit(t, unborn)
	if ok, err := HasGitHistory(unborn); err != nil || ok {
		t.Fatalf("HasGitHistory(unborn HEAD) = %v, %v; want false, nil", ok, err)
	}
	committed := undatedGitWS(t)
	if ok, err := HasGitHistory(committed); err != nil || !ok {
		t.Fatalf("HasGitHistory(repo) = %v, %v; want true, nil", ok, err)
	}
}

// created ← first commit, updated ← last commit, and the two genuinely differ.
func TestFirstAndLastCommitDate(t *testing.T) {
	root := freshWS(t)
	seed(t, root, "fragments/undated.md", kv{"type", "fragment"}, kv{"stage", "raw"})
	gitInit(t, root)
	commit(t, root, "seed", "2026-01-01T12:00:00")
	seed(t, root, "fragments/undated.md", kv{"type", "fragment"}, kv{"stage", "mature"})
	commit(t, root, "edit", "2026-03-01T12:00:00")

	rel := filepath.Join("fragments", "undated.md")
	first, ok, err := FirstCommitDate(root, rel)
	if err != nil || !ok {
		t.Fatalf("FirstCommitDate = %v, %v, %v", first, ok, err)
	}
	if got := first.Format("2006-01-02"); got != "2026-01-01" {
		t.Errorf("first = %s, want 2026-01-01", got)
	}
	last, ok, err := LastCommitDate(root, rel)
	if err != nil || !ok {
		t.Fatalf("LastCommitDate = %v, %v, %v", last, ok, err)
	}
	if got := last.Format("2006-01-02"); got != "2026-03-01" {
		t.Errorf("last = %s, want 2026-03-01", got)
	}
}

// An untracked path is Python's None on both helpers, never an error.
func TestCommitDatesOfUntrackedPathAreAbsent(t *testing.T) {
	root := undatedGitWS(t)
	for _, tc := range []struct {
		name string
		fn   func(string, string) (time.Time, bool, error)
	}{
		{"first", FirstCommitDate},
		{"last", LastCommitDate},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, ok, err := tc.fn(root, filepath.Join("fragments", "ghost.md"))
			if err != nil || ok {
				t.Fatalf("got ok=%v err=%v; want false, nil", ok, err)
			}
		})
	}
}

func TestSplitLines(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"a", []string{"a"}},
		{"a\nb", []string{"a", "b"}},
		{"a\r\nb", []string{"a", "b"}},
	}
	for _, tc := range cases {
		if got := splitLines(tc.in); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("splitLines(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
