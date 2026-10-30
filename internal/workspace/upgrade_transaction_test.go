package workspace

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/introspect"
)

func upgradeTree(t *testing.T, root string) map[string]string {
	t.Helper()
	tree := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		info, err := d.Info()
		if err != nil {
			return err
		}
		tree[rel] = info.Mode().String()
		if !d.IsDir() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			tree[rel] += string(data)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}
func TestUpgradeDryRunUsesCandidateWithoutArtifacts(t *testing.T) {
	src := presetSource(t)
	root := filepath.Join(t.TempDir(), "ws")
	mustInit(t, "note", root, InitOptions{PresetSource: src})
	growPreset(t, src)
	writeFile(t, filepath.Join(root, "notes", "kept.md"), "Existing corpus\n")
	before := upgradeTree(t, root)
	visited := false
	got := mustUpgrade(t, root, UpgradeOptions{DryRun: true, Preview: func(candidate string) error {
		visited = true
		if data, err := os.ReadFile(filepath.Join(candidate, "notes", "kept.md")); err != nil || string(data) != "Existing corpus\n" {
			t.Fatalf("candidate lost corpus: %q %v", data, err)
		}
		if _, err := os.Stat(filepath.Join(candidate, "memos")); err != nil {
			t.Fatal(err)
		}
		return nil
	}})
	if !visited || !got.DryRun || got.VersionTo != "10.0.0" || len(got.Config) == 0 {
		t.Fatalf("result: %+v visited=%v", got, visited)
	}
	if !reflect.DeepEqual(before, upgradeTree(t, root)) {
		t.Fatal("dry run changed workspace")
	}
}
func TestUpgradeInvalidCandidateLeavesOriginals(t *testing.T) {
	for _, bad := range []string{"schema", "template"} {
		t.Run(bad, func(t *testing.T) {
			src := presetSource(t)
			root := filepath.Join(t.TempDir(), "ws")
			mustInit(t, "note", root, InitOptions{PresetSource: src})
			growPreset(t, src)
			if bad == "schema" {
				writeFile(t, filepath.Join(src, "note", "storage.yaml"), "storage:\n  note: {layout: file, path: ../escape}\n")
			} else {
				writeFile(t, filepath.Join(src, "note", "templates", "note.yaml"), "sections: invalid\n")
			}
			before := upgradeTree(t, root)
			if _, err := Upgrade(root, UpgradeOptions{DryRun: true}); err == nil {
				t.Fatal("invalid candidate accepted")
			}
			if !reflect.DeepEqual(before, upgradeTree(t, root)) {
				t.Fatal("candidate failure changed workspace")
			}
		})
	}
}
func TestUpgradeRollbackRestoresFilesBackupsModesAndCreations(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".khub", "config.yaml"), "old version\n")
	writeFile(t, filepath.Join(root, ".khub", "config.yaml.bak"), "earlier backup\n")
	if err := os.Chmod(filepath.Join(root, ".khub", "config.yaml"), 0o640); err != nil {
		t.Fatal(err)
	}
	old, err := ReadFileState(root, ".khub/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	bak, err := ReadFileState(root, ".khub/config.yaml.bak")
	if err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() == 0 {
		t.Skip("permission failure requires unprivileged user")
	}
	if runtime.GOOS == "windows" {
		t.Skip("a read-only directory still accepts new files on Windows")
	}
	blocked := filepath.Join(root, "blocked")
	if err := os.Mkdir(blocked, 0o555); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(blocked, 0o755) }()
	// Read succeeds, publication fails, after earlier files have been published.
	writes := []FileChange{
		{Path: ".khub/config.yaml", Before: old, After: &FileState{[]byte("new version\n"), old.Mode}},
		{Path: ".khub/config.yaml.bak", Before: bak, After: &FileState{old.Data, bak.Mode}},
		{Path: "new/singleton.md", After: &FileState{[]byte("new singleton\n"), 0o644}},
		{Path: "blocked/file", After: &FileState{[]byte("impossible"), 0o644}},
	}
	if err := PublishChanges(root, writes, []string{"new"}, UpgradeJournal); err == nil {
		t.Fatal("failure expected")
	}
	now, _ := ReadFileState(root, ".khub/config.yaml")
	backup, _ := ReadFileState(root, ".khub/config.yaml.bak")
	if !sameFileState(now, old) || !sameFileState(backup, bak) {
		t.Fatal("rollback did not preserve bytes and modes")
	}
	if _, err := os.Stat(filepath.Join(root, "new")); !os.IsNotExist(err) {
		t.Fatalf("created directory remains: %v", err)
	}
	journals, _ := filepath.Glob(filepath.Join(root, ".khub", "generated", "upgrade-*"))
	if len(journals) != 0 {
		t.Fatalf("ordinary rollback retained journals: %v", journals)
	}
}
func TestUpgradeConcurrentEditPreservesEvidence(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".khub", "config.yaml"), "outside edit\n")
	writes := []FileChange{{Path: ".khub/config.yaml", Before: &FileState{[]byte("original\n"), 0o644}, After: &FileState{[]byte("replacement\n"), 0o644}}}
	err := PublishChanges(root, writes, nil, UpgradeJournal)
	if err == nil || located(t, err).Code != "upgrade_recovery_failed" {
		t.Fatalf("error = %v", err)
	}
	current, _ := os.ReadFile(filepath.Join(root, ".khub", "config.yaml"))
	if !bytes.Equal(current, []byte("outside edit\n")) {
		t.Fatal("external edit overwritten")
	}
	journals, _ := filepath.Glob(filepath.Join(root, ".khub", "generated", "upgrade-*"))
	if len(journals) != 1 || !strings.Contains(err.Error(), filepath.ToSlash(journals[0])) {
		t.Fatalf("missing recovery location: %v %v", journals, err)
	}
	for _, file := range []string{"manifest.yaml", "0.original", "0.replacement"} {
		if _, err := os.Stat(filepath.Join(journals[0], file)); err != nil {
			t.Fatal(err)
		}
	}
}
func TestUpgradeReportsRemovedTypesAndKeepsEntities(t *testing.T) {
	src := presetSource(t)
	growPreset(t, src)
	root := filepath.Join(t.TempDir(), "ws")
	mustInit(t, "note", root, InitOptions{PresetSource: src})
	writeFile(t, filepath.Join(root, "memos", "kept.md"), "keep me\n")
	writeFile(t, filepath.Join(src, "note", "ontology.yaml"), "version: '11.0.0'\nontology:\n  entities:\n    note:\n      attributes:\n        body: {type: text}\n")
	writeFile(t, filepath.Join(src, "note", "storage.yaml"), "storage:\n  note: {layout: file, path: notes}\n")
	result := mustUpgrade(t, root, UpgradeOptions{})
	if !reflect.DeepEqual(result.RemovedTypes, []string{"memo"}) {
		t.Fatalf("removed = %v", result.RemovedTypes)
	}
	if _, err := os.Stat(filepath.Join(root, "memos", "kept.md")); err != nil {
		t.Fatal(err)
	}
}

func TestUpgradeRepairsInvalidCurrentSchema(t *testing.T) {
	src := presetSource(t)
	root := filepath.Join(t.TempDir(), "ws")
	mustInit(t, "note", root, InitOptions{PresetSource: src})
	writeFile(t, filepath.Join(root, ".khub", "ontology.yaml"), "ontology: [broken\n")
	if _, err := Upgrade(root, UpgradeOptions{NoSchema: true}); err == nil {
		t.Fatal("invalid preserved schema accepted")
	}
	if _, err := Upgrade(root, UpgradeOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(root, ".khub", "ontology.yaml.bak")); got != "ontology: [broken\n" {
		t.Fatalf("original not backed up: %q", got)
	}
}

func TestUpgradeUnverifiableDestinationKeepsRecoveryEvidence(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".khub", "config.yaml"), "old\n")
	old, err := ReadFileState(root, ".khub/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, ".khub", "config.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".khub", "config.yaml"), 0o755); err != nil {
		t.Fatal(err)
	}
	err = PublishChanges(root, []FileChange{{Path: ".khub/config.yaml", Before: old, After: &FileState{[]byte("new\n"), old.Mode}}}, nil, UpgradeJournal)
	if err == nil || located(t, err).Code != "upgrade_recovery_failed" {
		t.Fatalf("err=%v", err)
	}
	journals, _ := filepath.Glob(filepath.Join(root, ".khub", "generated", "upgrade-*"))
	if len(journals) != 1 {
		t.Fatalf("recovery evidence lost: %v", journals)
	}
	if got := readFile(t, filepath.Join(journals[0], "0.original")); got != "old\n" {
		t.Fatalf("original=%q", got)
	}
}

func TestFreshInitCleanupPreservesArrivingWorkspaceLock(t *testing.T) {
	root := t.TempDir()
	khub := filepath.Join(root, ".khub")
	own := filepath.Join(khub, "ontology.yaml")
	writeFile(t, own, "partial\n")
	lock := filepath.Join(khub, "generated", "locks", "workspace.lock")
	writeFile(t, lock, "")
	cleanupInitFiles(root, khub, []string{own}, true)
	if _, err := os.Stat(lock); err != nil {
		t.Fatal("cleanup removed arriving lock", err)
	}
	if _, err := os.Stat(own); !os.IsNotExist(err) {
		t.Fatal("own partial file survived", err)
	}
}

// The candidate copy prunes vendored trees and skips non-regular files, so a
// node_modules or a stray fifo neither bloats nor aborts an upgrade (review of
// PR #143, finding 2). The whole-tree comparison stays in the dry-run test
// above: reading a fifo would block it.
func TestUpgradeCandidateSkipsVendoredDirsAndSpecialFiles(t *testing.T) {
	src := presetSource(t)
	root := filepath.Join(t.TempDir(), "ws")
	mustInit(t, "note", root, InitOptions{PresetSource: src})
	growPreset(t, src)
	writeFile(t, filepath.Join(root, "node_modules", "pkg", "index.js"), "module.exports = 1\n")
	// Windows has no fifo; the vendored-dir half of the test still runs there.
	special := []string{"node_modules"}
	if makeFifo(t, filepath.Join(root, "pipe")) {
		special = append(special, "pipe")
	}
	visited := false
	got := mustUpgrade(t, root, UpgradeOptions{DryRun: true, Preview: func(candidate string) error {
		visited = true
		for _, skipped := range special {
			if _, err := os.Lstat(filepath.Join(candidate, skipped)); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("candidate copied %s: %v", skipped, err)
			}
		}
		return nil
	}})
	if !visited || !got.DryRun || got.VersionTo != "10.0.0" {
		t.Fatalf("result: %+v visited=%v", got, visited)
	}
	kept := []string{filepath.Join("node_modules", "pkg", "index.js")}
	if len(special) > 1 {
		kept = append(kept, "pipe")
	}
	for _, kept := range kept {
		if _, err := os.Lstat(filepath.Join(root, kept)); err != nil {
			t.Fatalf("workspace lost %s: %v", kept, err)
		}
	}
}

// Only a Windows reparse point inside storage is refused; a fifo there is
// still skipped, as one elsewhere is.
func TestUpgradeCandidateSkipsSpecialFileInStorage(t *testing.T) {
	src := presetSource(t)
	root := filepath.Join(t.TempDir(), "ws")
	mustInit(t, "note", root, InitOptions{PresetSource: src})
	growPreset(t, src)
	if err := os.MkdirAll(filepath.Join(root, "notes"), 0o777); err != nil {
		t.Fatal(err)
	}
	if !makeFifo(t, filepath.Join(root, "notes", "pipe")) {
		t.Skip("no fifo on this platform")
	}
	mustUpgrade(t, root, UpgradeOptions{DryRun: true})
}

func TestInStorageMatchesStoragePathsOnly(t *testing.T) {
	src := presetSource(t)
	root := filepath.Join(t.TempDir(), "ws")
	mustInit(t, "note", root, InitOptions{PresetSource: src})
	old, err := introspect.LoadSchema(root)
	if err != nil {
		t.Fatal(err)
	}
	for rel, want := range map[string]bool{
		"notes":                        true,
		filepath.Join("notes", "a.md"): true,
		"notesx":                       false,
		"README.md":                    false,
	} {
		if got := inStorage(old, rel); got != want {
			t.Errorf("inStorage(%q) = %v, want %v", rel, got, want)
		}
	}
	if inStorage(nil, "notes") {
		t.Error("a nil schema declared a storage path")
	}
}

// A removal is a change with no After: a failure later in the publication
// restores the removed file from its staged original.
func TestPublishChangesRollsBackARemoval(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "notes", "gone.md"), "keep me\n")
	writeFile(t, filepath.Join(root, "outside.md"), "edited outside\n")
	gone, err := ReadFileState(root, filepath.Join("notes", "gone.md"))
	if err != nil {
		t.Fatal(err)
	}
	journal := Journal{Prefix: "migrate-", Noun: "Migration", Code: "migrate_recovery_failed"}
	changes := []FileChange{
		{Path: filepath.Join("notes", "gone.md"), Before: gone},
		{Path: "outside.md", Before: &FileState{[]byte("staged\n"), 0o644}, After: &FileState{[]byte("new\n"), 0o644}},
	}
	err = PublishChanges(root, changes, nil, journal)
	if err == nil || located(t, err).Code != "migrate_recovery_failed" || !strings.Contains(err.Error(), "during migration") {
		t.Fatalf("err = %v", err)
	}
	if got := readFile(t, filepath.Join(root, "notes", "gone.md")); got != "keep me\n" {
		t.Fatalf("removed file not restored: %q", got)
	}
	journals, _ := filepath.Glob(filepath.Join(root, ".khub", "generated", "migrate-*"))
	if len(journals) != 1 {
		t.Fatalf("journals: %v", journals)
	}
	if _, err := os.Stat(filepath.Join(journals[0], "0.replacement")); !os.IsNotExist(err) {
		t.Fatal("a removal staged a replacement")
	}

	// The same removal alone commits, and the journal goes with it.
	if err := PublishChanges(root, changes[:1], nil, journal); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "notes", "gone.md")); !os.IsNotExist(err) {
		t.Fatalf("file not removed: %v", err)
	}
}
