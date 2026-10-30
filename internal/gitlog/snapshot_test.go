package gitlog

// The --at snapshot: which commit a value resolves to, what the scratch tree
// holds, and the refusals. The CLI surface is pinned by parity/cases/point-in-time.

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/errs"
)

func revParse(t *testing.T, root, rev string) string {
	t.Helper()
	out, code, err := run(root, "rev-parse", rev)
	if err != nil || code != 0 {
		t.Fatalf("rev-parse %s: %v (exit %d)", rev, err, code)
	}
	return strings.TrimSpace(out)
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	var located *errs.Located
	if !errors.As(err, &located) || located.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}

// historyRepo is a repository whose khub workspace lives in hub/, with two
// commits a day apart and one uncommitted change.
// materialize runs Materialize into a fresh scratch directory.
func materialize(t *testing.T, root, spec string) (*Snapshot, error) {
	t.Helper()
	return Materialize(context.Background(), root, spec, t.TempDir())
}

func historyRepo(t *testing.T) (repo, ws string) {
	t.Helper()
	repo = t.TempDir()
	gitInit(t, repo)
	writeRaw(t, repo, "README.md", "outside the workspace\n")
	ws = filepath.Join(repo, "hub")
	initPreset(t, ws, "firm-ops")
	seed(t, ws, "fragments/first.md", kv{"type", "fragment"}, kv{"stage", "raw"})
	commit(t, repo, "first", "2026-03-01T10:00:00Z")
	seed(t, ws, "fragments/second.md", kv{"type", "fragment"}, kv{"stage", "raw"})
	commit(t, repo, "second", "2026-03-02T23:30:00Z")
	seed(t, ws, "fragments/dirty.md", kv{"type", "fragment"}, kv{"stage", "raw"})
	return repo, ws
}

func TestResolveAt(t *testing.T) {
	repo, ws := historyRepo(t)
	head, first := revParse(t, repo, "HEAD"), revParse(t, repo, "HEAD~1")
	for _, tc := range []struct{ spec, want string }{
		{"HEAD", head},
		{"HEAD~1", first},
		{first[:12], first},
		{"2026-03-01", first},
		// End of day UTC: the 23:30Z commit belongs to its own day.
		{"2026-03-02", head},
		{"2027-01-01", head},
	} {
		got, err := ResolveAt(context.Background(), ws, tc.spec)
		if err != nil {
			t.Fatalf("%s: %v", tc.spec, err)
		}
		if got != tc.want {
			t.Fatalf("%s resolved to %s, want %s", tc.spec, got, tc.want)
		}
	}
	for _, spec := range []string{"2026-02-28", "no-such-branch", "--output=x", "2026-13-45"} {
		_, err := ResolveAt(context.Background(), ws, spec)
		wantCode(t, err, "unknown_revision")
	}
}

func TestMaterializeReadsTheCommitNotTheWorkingTree(t *testing.T) {
	_, ws := historyRepo(t)
	snap, err := materialize(t, ws, "HEAD~1")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = snap.Close() }()
	if _, err := os.Stat(filepath.Join(snap.Root, ".khub", "ontology.yaml")); err != nil {
		t.Fatalf("snapshot lacks the schema: %v", err)
	}
	if _, err := os.Stat(filepath.Join(snap.Root, "fragments", "first.md")); err != nil {
		t.Fatalf("snapshot lacks first.md: %v", err)
	}
	for _, absent := range []string{"fragments/second.md", "fragments/dirty.md", "README.md", "hub"} {
		if _, err := os.Stat(filepath.Join(snap.Root, filepath.FromSlash(absent))); !os.IsNotExist(err) {
			t.Fatalf("%s should not be in the snapshot (err %v)", absent, err)
		}
	}
	want, _ := os.ReadFile(filepath.Join(ws, "fragments", "first.md"))
	got, _ := os.ReadFile(filepath.Join(snap.Root, "fragments", "first.md"))
	if string(got) != string(want) {
		t.Fatalf("first.md differs:\n%s\nwant\n%s", got, want)
	}
	root := snap.Root
	if err := snap.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("Close left %s behind", root)
	}
}

func TestMaterializeRefusals(t *testing.T) {
	// No git at all.
	plain := freshWS(t)
	_, err := materialize(t, plain, "HEAD")
	wantCode(t, err, "no_git_history")

	// A commit from before the workspace existed.
	repo := t.TempDir()
	gitInit(t, repo)
	writeRaw(t, repo, "README.md", "x\n")
	commit(t, repo, "before", "2026-01-01T00:00:00Z")
	ws := filepath.Join(repo, "hub")
	initPreset(t, ws, "firm-ops")
	commit(t, repo, "workspace", "2026-02-01T00:00:00Z")
	_, err = materialize(t, ws, "HEAD~1")
	wantCode(t, err, "no_workspace")
	snap, err := materialize(t, ws, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	_ = snap.Close()
}

func TestStaleFromReadsDatesUpToTheCommit(t *testing.T) {
	repo := freshWS(t)
	gitInit(t, repo)
	seed(t, repo, "fragments/note.md", kv{"type", "fragment"}, kv{"stage", "raw"})
	commit(t, repo, "seed", "2026-01-01T10:00:00Z")
	seed(t, repo, "fragments/note.md", kv{"type", "fragment"}, kv{"stage", "processed"})
	commit(t, repo, "touch", "2026-06-01T10:00:00Z")

	snap, err := materialize(t, repo, "HEAD~1")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = snap.Close() }()
	report, err := StaleFrom(snap.Root, snap.History(), ptr(30), now)
	if err != nil {
		t.Fatal(err)
	}
	if !report.GitAvailable || len(report.Entries) != 1 {
		t.Fatalf("want one git-dated entry, got %+v", report)
	}
	if got := report.Entries[0].EffectiveDate.Format("2006-01-02"); got != "2026-01-01" {
		t.Fatalf("effective date %s, want the snapshot commit's 2026-01-01", got)
	}
	// The same read at HEAD sees the later commit.
	live := mustStale(t, repo, ptr(10))
	if got := live.Entries[0].EffectiveDate.Format("2006-01-02"); got != "2026-06-01" {
		t.Fatalf("live effective date %s, want 2026-06-01", got)
	}
}

func TestMaterializeKeepsModesAndSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes and symlinks are a unix checkout's")
	}
	repo := freshWS(t)
	gitInit(t, repo)
	writeRaw(t, repo, "tools/run.md", "#!/bin/sh\n")
	if err := os.Chmod(filepath.Join(repo, "tools", "run.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("run.md", filepath.Join(repo, "tools", "link")); err != nil {
		t.Fatal(err)
	}
	commit(t, repo, "seed", "2026-01-01T00:00:00Z")
	snap, err := materialize(t, repo, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = snap.Close() }()
	info, err := os.Stat(filepath.Join(snap.Root, "tools", "run.md"))
	if err != nil || info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("run.md lost its executable bit: %v %v", info, err)
	}
	target, err := os.Readlink(filepath.Join(snap.Root, "tools", "link"))
	if err != nil || target != "run.md" {
		t.Fatalf("link: %q %v", target, err)
	}
}

func TestSafeTreePath(t *testing.T) {
	for p, want := range map[string]bool{
		"a/b.md": true, ".khub/ontology.yaml": true, "a..b/c": true,
		"": false, "/etc/passwd": false, "../x": false, "a/../b": false,
		"a//b": false, "./a": false, ".git/config": false, "a/.GIT/x": false,
	} {
		for _, windows := range []bool{false, true} {
			if got := safeTreePath(p, windows); got != want {
				t.Fatalf("safeTreePath(%q, %v) = %v, want %v", p, windows, got, want)
			}
		}
	}
	// A backslash is a filename character on unix and a separator on Windows.
	if !safeTreePath(`notes\back.md`, false) || safeTreePath(`notes\back.md`, true) {
		t.Fatal("backslash: want legal on unix, refused on Windows")
	}
}

// stage records path with content in the index without touching the working
// tree, so a test can commit a tree no checkout on this filesystem could hold.
func stage(t *testing.T, root, relpath, content string) {
	t.Helper()
	cmd := exec.Command("git", "-C", root, "hash-object", "-w", "--stdin")
	cmd.Stdin = strings.NewReader(content)
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	git(t, root, "", "update-index", "--add", "--cacheinfo", "100644,"+strings.TrimSpace(string(out))+","+relpath)
}

func TestMaterializeCopiesOnlyWhatAScanReads(t *testing.T) {
	seedRepo := func(t *testing.T, policy string) string {
		repo := freshWS(t)
		gitInit(t, repo)
		if policy != "" {
			writeRaw(t, repo, ".khub/policy.yaml", policy)
		}
		writeRaw(t, repo, ".khub/ignore", "examples/\n")
		writeRaw(t, repo, "examples/sample.md", "---\ntype: fragment\n---\n")
		writeRaw(t, repo, "tools/main.go", "package main\n")
		writeRaw(t, repo, "notes/keep.md", "kept\n")
		commit(t, repo, "seed", "2026-01-01T00:00:00Z")
		return repo
	}
	read := func(root, rel string) (string, bool) {
		got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		return string(got), err == nil
	}

	// No relation names a file by path: only what a scan opens is there.
	snap, err := materialize(t, seedRepo(t, ""), "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := read(snap.Root, "notes/keep.md"); !ok || got != "kept\n" {
		t.Fatalf("keep.md: %q", got)
	}
	if got, ok := read(snap.Root, ".khub/ignore"); !ok || got != "examples/\n" {
		t.Fatalf(".khub/ignore: %q", got)
	}
	for _, rel := range []string{"tools/main.go", "examples/sample.md"} {
		if _, ok := read(snap.Root, rel); ok {
			t.Fatalf("%s should be left out", rel)
		}
	}

	// path_refs: every other file exists, empty, so a path reference resolves.
	snap, err = materialize(t, seedRepo(t, "policy:\n  opportunity: {path_refs: true}\n"), "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"tools/main.go", "examples/sample.md"} {
		if got, ok := read(snap.Root, rel); !ok || got != "" {
			t.Fatalf("%s: want an empty placeholder, got %q (exists %v)", rel, got, ok)
		}
	}
}

func TestMaterializeBackslashPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a backslash is a separator on Windows")
	}
	repo := freshWS(t)
	gitInit(t, repo)
	writeRaw(t, repo, `notes\back.md`, "x\n")
	commit(t, repo, "seed", "2026-01-01T00:00:00Z")
	snap, err := materialize(t, repo, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(snap.Root, `notes\back.md`)); err != nil || string(got) != "x\n" {
		t.Fatalf("backslash file: %q %v", got, err)
	}
}

func TestMaterializeMissingBlob(t *testing.T) {
	repo := freshWS(t)
	gitInit(t, repo)
	writeRaw(t, repo, "notes/gone.md", "soon missing\n")
	commit(t, repo, "seed", "2026-01-01T00:00:00Z")
	oid := revParse(t, repo, "HEAD:notes/gone.md")
	loose := filepath.Join(repo, ".git", "objects", oid[:2], oid[2:])
	if err := os.Rename(loose, filepath.Join(repo, ".git", "moved-object")); err != nil {
		t.Fatal(err)
	}
	_, err := materialize(t, repo, "HEAD")
	wantCode(t, err, "snapshot_error")
}

func TestMaterializeCaseFoldedPaths(t *testing.T) {
	repo := freshWS(t)
	gitInit(t, repo)
	git(t, repo, "", "add", "-A")
	stage(t, repo, "notes/Twin.md", "upper\n")
	stage(t, repo, "notes/twin.md", "lower\n")
	git(t, repo, "2026-01-01T00:00:00Z", "commit", "-m", "twins")
	probe := t.TempDir()
	writeRaw(t, probe, "x", "")
	_, statErr := os.Stat(filepath.Join(probe, "X"))
	folds := statErr == nil

	_, err := materialize(t, repo, "HEAD")
	if folds {
		wantCode(t, err, "snapshot_error")
	} else if err != nil {
		t.Fatalf("a case-sensitive filesystem holds both: %v", err)
	}
}

func TestResolveAtUnbornHeadNamesAnotherBranch(t *testing.T) {
	repo := freshWS(t)
	gitInit(t, repo)
	commit(t, repo, "seed", "2026-01-01T00:00:00Z")
	git(t, repo, "", "branch", "kept")
	git(t, repo, "", "checkout", "-q", "--orphan", "fresh")
	if _, err := ResolveAt(context.Background(), repo, "kept"); err != nil {
		t.Fatalf("a branch resolves under an unborn HEAD: %v", err)
	}
	_, err := ResolveAt(context.Background(), repo, "2026-01-01")
	wantCode(t, err, "no_git_history")
}
