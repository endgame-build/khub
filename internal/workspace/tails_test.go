package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/fsio"
)

// lockHeld probes the workspace lock from a second handle: the lock is held
// per open file (flock's open file description, LockFileEx's handle), so a
// non-blocking request from a fresh one is refused exactly when another
// handle holds it — the hook's own. tryLock is per platform (lockprobe_*).
func lockHeld(t *testing.T, root string) bool {
	t.Helper()
	f, err := os.OpenFile(fsio.WorkspaceLockPath(root), os.O_RDWR, 0o644)
	if err != nil {
		t.Fatalf("lock file: %v", err)
	}
	defer f.Close()
	got, err := tryLock(f)
	if err != nil {
		t.Fatalf("lock probe: %v", err)
	}
	if got {
		_ = releaseLock(f)
	}
	return !got
}

// The tails a CLI hangs on init (wire, index) run inside the same lock as the
// scaffold: on a fresh workspace the lock is taken once .khub/ exists, on a
// re-init it is the one the scaffold already holds.
func TestInitRunsTailsUnderTheWorkspaceLock(t *testing.T) {
	src := presetSource(t)
	root := filepath.Join(t.TempDir(), "ws")
	for _, again := range []bool{false, true} {
		calls := 0
		mustInit(t, "note", root, InitOptions{PresetSource: src, Force: again, Tails: func(got string) {
			calls++
			if got != root {
				t.Fatalf("hook root = %q, want %q", got, root)
			}
			if !lockHeld(t, root) {
				t.Fatalf("tails ran without the workspace lock (re-init=%v)", again)
			}
		}})
		if calls != 1 {
			t.Fatalf("tails ran %d times (re-init=%v), want once", calls, again)
		}
	}
	if lockHeld(t, root) {
		t.Fatal("lock still held after Init returned")
	}
}

// Upgrade publishes the version last and only then runs its tails, still
// inside its lock: the hook must see the new version on disk and a held lock.
func TestUpgradeRunsTailsUnderTheWorkspaceLock(t *testing.T) {
	src := presetSource(t)
	root := filepath.Join(t.TempDir(), "ws")
	mustInit(t, "note", root, InitOptions{PresetSource: src})
	growPreset(t, src)
	calls := 0
	mustUpgrade(t, root, UpgradeOptions{Tails: func(got string) {
		calls++
		if got != root {
			t.Fatalf("hook root = %q, want %q", got, root)
		}
		if !lockHeld(t, root) {
			t.Fatal("tails ran without the workspace lock")
		}
		if cfg := readFile(t, filepath.Join(root, ".khub", "config.yaml")); !strings.Contains(cfg, "10.0.0") {
			t.Fatalf("tails ran before the version was published:\n%s", cfg)
		}
	}})
	if calls != 1 {
		t.Fatalf("tails ran %d times, want once", calls)
	}
	if lockHeld(t, root) {
		t.Fatal("lock still held after Upgrade returned")
	}
}

// A dry run holds no lock and must not run the real-upgrade tails at all.
func TestUpgradeDryRunSkipsTails(t *testing.T) {
	src := presetSource(t)
	root := filepath.Join(t.TempDir(), "ws")
	mustInit(t, "note", root, InitOptions{PresetSource: src})
	growPreset(t, src)
	mustUpgrade(t, root, UpgradeOptions{DryRun: true, Tails: func(string) {
		t.Fatal("Tails ran on a dry run")
	}})
}
