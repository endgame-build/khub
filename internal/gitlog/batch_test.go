package gitlog

import (
	"fmt"
	"path/filepath"
	"testing"
)

func assertBatchMatchesHelpers(t *testing.T, root string, paths []string) {
	t.Helper()
	requests := make([]DateRequest, 0, len(paths))
	for _, path := range paths {
		requests = append(requests, DateRequest{Path: path, First: true, Last: true})
	}
	got, err := CommitDates(root, requests)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		first, _, err := FirstCommitDate(root, path)
		if err != nil {
			t.Fatal(err)
		}
		last, _, err := LastCommitDate(root, path)
		if err != nil {
			t.Fatal(err)
		}
		if got[path] != (Dates{First: first, Last: last}) {
			t.Fatalf("%q: got %v, want %v / %v", path, got[path], first, last)
		}
	}
}

func TestCommitDatesLinearNestedAndRenames(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)
	paths := []string{"a.md", "white space.md", "tab\tname.md", "line\nname.md", "\nleading.md", "é.md", "old.md", "untracked.md"}
	for _, path := range paths[:len(paths)-1] {
		writeRaw(t, root, "nested/"+path, "first\n")
	}
	commit(t, root, "first", "2026-06-01T12:00:00+02:00")
	writeRaw(t, root, "nested/a.md", "second\n")
	git(t, root, "", "add", "-A")
	// Author and committer dates differ; history order differs from date order.
	git(t, root, "2026-01-01T12:00:00+02:00", "commit", "--date=2025-01-01T00:00:00Z", "-m", "second")
	git(t, root, "", "mv", "nested/old.md", "nested/new.md")
	commit(t, root, "rename", "2026-02-01T12:00:00Z")
	writeRaw(t, root, "nested/untracked.md", "never committed\n")
	paths = append(paths, "new.md")
	assertBatchMatchesHelpers(t, filepath.Join(root, "nested"), paths)
	got, err := CommitDates(filepath.Join(root, "nested"), []DateRequest{{Path: "a.md", Last: true}, {Path: "new.md", First: true}})
	if err != nil {
		t.Fatal(err)
	}
	if !got["a.md"].First.IsZero() || got["a.md"].Last.Format("2006-01-02") != "2026-01-01" || !got["new.md"].Last.IsZero() {
		t.Fatal(got)
	}
	assertBatchMatchesHelpers(t, filepath.Join(root, "nested"), []string{"new.md"})
	duplicates, err := CommitDates(filepath.Join(root, "nested"), []DateRequest{{Path: "new.md", First: true}, {Path: "new.md", Last: true}})
	if err != nil {
		t.Fatal(err)
	}
	if duplicates["new.md"].First.IsZero() || duplicates["new.md"].Last.IsZero() {
		t.Fatal(duplicates)
	}

}

func TestCommitDatesMergeFallback(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)
	writeRaw(t, root, "a", "first")
	writeRaw(t, root, "b", "first")
	commit(t, root, "base", "2026-01-01T00:00:00Z")
	git(t, root, "", "checkout", "-b", "side")
	writeRaw(t, root, "a", "side")
	commit(t, root, "side", "2026-03-01T00:00:00Z")
	git(t, root, "", "checkout", "-b", "main-test", "HEAD~1")
	writeRaw(t, root, "b", "main")
	commit(t, root, "main", "2026-02-01T00:00:00Z")
	git(t, root, "2026-04-01T00:00:00Z", "merge", "--no-ff", "side", "-m", "merge")
	assertBatchMatchesHelpers(t, root, []string{"a", "b"})
}

func TestCommitDatesNoRequestsDoesNotRunGit(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	for _, requests := range [][]DateRequest{nil, {{Path: "unused"}}} {
		if _, err := CommitDates(t.TempDir(), requests); err != nil {
			t.Fatal(err)
		}
	}
}

func BenchmarkStaleDates(b *testing.B) {
	for _, dated := range []bool{true, false} {
		b.Run(fmt.Sprint(dated), func(b *testing.B) {
			root := freshWS(b)
			for i := range 100 {
				text := "---\ntype: fragment\nstage: raw\n"
				if dated {
					text += "updated: 2026-01-01\n"
				}
				text += "---\n"
				writeRaw(b, root, fmt.Sprintf("fragments/n%d.md", i), text)
			}
			gitInit(b, root)
			commit(b, root, "seed", "2026-01-01T00:00:00Z")
			b.ResetTimer()
			for b.Loop() {
				if _, err := Stale(root, ptr(30), now); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
