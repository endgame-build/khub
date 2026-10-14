package fsio

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// openLock opens a fresh handle on path, as a second writer would.
func openLock(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

// free reports whether a fresh handle could take the lock right now, and
// leaves it untaken either way.
func free(t *testing.T, path string) bool {
	t.Helper()
	f, err := os.OpenFile(path, os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, err := tryLock(f)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if got {
		if err := releaseLock(f); err != nil {
			t.Fatalf("probe release: %v", err)
		}
	}
	return got
}

// lockFile excludes every other handle until unlockFile, and closing the
// holding handle releases it too: a writer that dies cannot strand the
// workspace.
func TestLockFileExcludesOtherHandlesUntilReleased(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.lock")
	first := openLock(t, path)
	if !free(t, path) {
		t.Fatal("lock held before anyone took it")
	}
	if err := lockFile(first); err != nil {
		t.Fatal(err)
	}
	if free(t, path) {
		t.Fatal("a second handle took the lock while the first held it")
	}
	if err := unlockFile(first); err != nil {
		t.Fatal(err)
	}
	if !free(t, path) {
		t.Fatal("lock still held after unlockFile")
	}

	if err := lockFile(first); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if !free(t, path) {
		t.Fatal("lock still held after its handle closed")
	}
}

// A contended lockFile waits for the holder's release rather than failing.
func TestLockFileBlocksUntilTheHolderReleases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.lock")
	first, second := openLock(t, path), openLock(t, path)
	if err := lockFile(first); err != nil {
		t.Fatal(err)
	}
	got := make(chan error, 1)
	go func() { got <- lockFile(second) }()
	select {
	case err := <-got:
		t.Fatalf("second lockFile returned (%v) while the first held the lock", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := unlockFile(first); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-got:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("second lockFile still blocked after the release")
	}
	if free(t, path) {
		t.Fatal("second handle does not hold the lock it was granted")
	}
	if err := unlockFile(second); err != nil {
		t.Fatal(err)
	}
}
