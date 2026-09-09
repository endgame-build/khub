package workspace

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
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
	old, err := readUpgradeFile(root, ".khub/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	bak, err := readUpgradeFile(root, ".khub/config.yaml.bak")
	if err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() == 0 {
		t.Skip("permission failure requires unprivileged user")
	}
	blocked := filepath.Join(root, "blocked")
	if err := os.Mkdir(blocked, 0o555); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(blocked, 0o755) }()
	// Read succeeds, publication fails, after earlier files have been published.
	writes := []upgradeWrite{
		{name: ".khub/config.yaml", before: old, after: upgradeFile{[]byte("new version\n"), old.mode}},
		{name: ".khub/config.yaml.bak", before: bak, after: upgradeFile{old.data, bak.mode}},
		{name: "new/singleton.md", after: upgradeFile{[]byte("new singleton\n"), 0o644}},
		{name: "blocked/file", after: upgradeFile{[]byte("impossible"), 0o644}},
	}
	if err := publishUpgrade(root, writes, []string{"new"}); err == nil {
		t.Fatal("failure expected")
	}
	now, _ := readUpgradeFile(root, ".khub/config.yaml")
	backup, _ := readUpgradeFile(root, ".khub/config.yaml.bak")
	if !sameUpgradeFile(now, old) || !sameUpgradeFile(backup, bak) {
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
	writes := []upgradeWrite{{name: ".khub/config.yaml", before: &upgradeFile{[]byte("original\n"), 0o644}, after: upgradeFile{[]byte("replacement\n"), 0o644}}}
	err := publishUpgrade(root, writes, nil)
	if err == nil || located(t, err).Code != "upgrade_recovery_failed" {
		t.Fatalf("error = %v", err)
	}
	current, _ := os.ReadFile(filepath.Join(root, ".khub", "config.yaml"))
	if !bytes.Equal(current, []byte("outside edit\n")) {
		t.Fatal("external edit overwritten")
	}
	journals, _ := filepath.Glob(filepath.Join(root, ".khub", "generated", "upgrade-*"))
	if len(journals) != 1 || !strings.Contains(err.Error(), journals[0]) {
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
	old, err := readUpgradeFile(root, ".khub/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, ".khub", "config.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".khub", "config.yaml"), 0o755); err != nil {
		t.Fatal(err)
	}
	err = publishUpgrade(root, []upgradeWrite{{name: ".khub/config.yaml", before: old, after: upgradeFile{[]byte("new\n"), old.mode}}}, nil)
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
	if err := syscall.Mkfifo(filepath.Join(root, "pipe"), 0o600); err != nil {
		t.Fatal(err)
	}
	visited := false
	got := mustUpgrade(t, root, UpgradeOptions{DryRun: true, Preview: func(candidate string) error {
		visited = true
		for _, skipped := range []string{"node_modules", "pipe"} {
			if _, err := os.Lstat(filepath.Join(candidate, skipped)); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("candidate copied %s: %v", skipped, err)
			}
		}
		return nil
	}})
	if !visited || !got.DryRun || got.VersionTo != "10.0.0" {
		t.Fatalf("result: %+v visited=%v", got, visited)
	}
	for _, kept := range []string{filepath.Join("node_modules", "pkg", "index.js"), "pipe"} {
		if _, err := os.Lstat(filepath.Join(root, kept)); err != nil {
			t.Fatalf("workspace lost %s: %v", kept, err)
		}
	}
}
