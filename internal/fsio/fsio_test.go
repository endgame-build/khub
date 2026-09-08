package fsio

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
)

func TestAtomicWriteCreatesAndReplaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "repo.jsonl")

	if err := AtomicWrite(path, []byte("first\n")); err != nil {
		t.Fatal(err)
	}
	if got := readAll(t, path); got != "first\n" {
		t.Fatalf("content = %q", got)
	}
	if err := AtomicWrite(path, []byte("second\n")); err != nil {
		t.Fatal(err)
	}
	if got := readAll(t, path); got != "second\n" {
		t.Fatalf("content = %q", got)
	}
	// The temp sibling is Python's `<name>.tmp`, and the rename consumes it.
	if _, err := os.Stat(path + ".tmp"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("temp sibling survived: %v", err)
	}
}

func TestWriteNewRefusesAnExistingPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "acme.md")

	if err := WriteNew(path, []byte("one\n")); err != nil {
		t.Fatal(err)
	}
	err := WriteNew(path, []byte("two\n"))
	if !errors.Is(err, fs.ErrExist) {
		t.Fatalf("second WriteNew err = %v, want fs.ErrExist", err)
	}
	if got := readAll(t, path); got != "one\n" {
		t.Fatalf("the loser clobbered the winner: %q", got)
	}
}

func TestWorkspaceLockPathIsTheGeneratedSidecar(t *testing.T) {
	got := WorkspaceLockPath(filepath.FromSlash("/ws"))
	want := filepath.FromSlash("/ws/.khub/generated/locks/workspace.lock")
	if got != want {
		t.Fatalf("lock path = %q, want %q", got, want)
	}
}

func TestWithLockCreatesTheDirAndSerializes(t *testing.T) {
	// Every round starts from a root with no lock directory, so eight workers
	// race its creation from scratch each time. One round reproduced darwin's
	// transient ENOENT on the freshly created directory in roughly two runs
	// out of five; twenty rounds turn that into a reliable detector.
	for round := 0; round < 20; round++ {
		root := t.TempDir()
		lock := WorkspaceLockPath(root)
		data := filepath.Join(root, "counter")
		if err := os.WriteFile(data, []byte("0"), 0o666); err != nil {
			t.Fatal(err)
		}

		// Each worker does the whole read-modify-write inside the lock, exactly as
		// _mutate_collection does; a lost update would show as a short final count.
		const workers = 8
		var wg sync.WaitGroup
		errCh := make(chan error, workers)
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				errCh <- withTestLock(root, func() error {
					raw, err := os.ReadFile(data)
					if err != nil {
						return err
					}
					return AtomicWrite(data, append(raw, 'x'))
				})
			}()
		}
		wg.Wait()
		close(errCh)
		for err := range errCh {
			if err != nil {
				t.Fatalf("round %d: %v", round, err)
			}
		}
		if got := readAll(t, data); len(got) != workers+1 {
			t.Fatalf("round %d: counter = %q (%d writes landed), want %d", round, got, len(got)-1, workers)
		}
		if _, err := os.Stat(lock); err != nil {
			t.Fatalf("round %d: lock sidecar missing: %v", round, err)
		}
	}
}

func TestWithLockReleasesOnError(t *testing.T) {
	root := t.TempDir()
	sentinel := errors.New("mutate failed")

	if err := withTestLock(root, func() error { return sentinel }); !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want the callback's error", err)
	}
	// A leaked lock would deadlock this second acquisition.
	if err := withTestLock(root, func() error { return nil }); err != nil {
		t.Fatalf("re-acquire failed: %v", err)
	}
}

func readAll(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestAtomicWritePreservesPermissionsAndUnrelatedTemp(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "data")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(outside, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path+".tmp"); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWriteIn(root, path, []byte("replacement")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("permissions=%o", info.Mode().Perm())
	}
	if got := readAll(t, outside); got != "keep" {
		t.Fatalf("temp symlink clobbered victim: %q", got)
	}
	if _, err := os.Lstat(path + ".tmp"); err != nil {
		t.Fatal("unrelated temp was removed")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWriteIn(root, path, []byte("bad")); err == nil {
		t.Fatal("target symlink accepted")
	}
}

func TestFailedPublicationDoesNotExposeTemporaryNameOrModifyOriginal(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires unprivileged user")
	}
	root := t.TempDir()
	path := filepath.Join(root, "data")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(root, 0o755)
	err := AtomicWriteIn(root, path, []byte("replacement"))
	var pe *fs.PathError
	if !errors.As(err, &pe) || pe.Path != path {
		t.Fatalf("error exposes temporary path: %v", err)
	}
	if got := readAll(t, path); got != "original" {
		t.Fatalf("failed write changed file: %q", got)
	}
}

func TestConfinedOperationsRejectEscapes(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(outside, "file"), filepath.Join(root, "link", "file")} {
		if err := AtomicWriteIn(root, path, []byte("bad")); err == nil {
			t.Fatalf("escape accepted: %s", path)
		}
		if err := MkdirAll(root, filepath.Join(path, "nested")); err == nil {
			t.Fatalf("mkdir escape accepted: %s", path)
		}
	}
}

func withTestLock(root string, fn func() error) error {
	_, err := Locked(root, func() (struct{}, error) { return struct{}{}, fn() })
	return err
}

func TestReadDirFileErrorIsPlatformStable(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	_, err := ReadDir(root, path)
	var pe *os.PathError
	if !errors.As(err, &pe) || pe.Op != "readdir" || pe.Path != path {
		t.Fatalf("ReadDir error = %v", err)
	}
}

// Filesystems without hard links (exFAT, some SMB and FUSE mounts) still get an
// exclusive publish: re-check the target, then rename (review of PR #143, finding 3).
func TestWriteNewFallsBackWhenHardLinksAreUnsupported(t *testing.T) {
	original := linkFile
	t.Cleanup(func() { linkFile = original })
	noTemp := func(t *testing.T, dir string) {
		t.Helper()
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".khub-write-") {
				t.Fatalf("temporary left behind: %s", e.Name())
			}
		}
	}
	t.Run("publishes", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "acme.md")
		linkFile = func(*os.Root, string, string) error { return syscall.ENOTSUP }
		if err := WriteNewIn(dir, path, []byte("one\n")); err != nil {
			t.Fatal(err)
		}
		if got := readAll(t, path); got != "one\n" {
			t.Fatalf("content = %q", got)
		}
		noTemp(t, dir)
	})
	t.Run("refuses a winner", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "acme.md")
		linkFile = func(*os.Root, string, string) error {
			if err := os.WriteFile(path, []byte("winner\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			return syscall.ENOTSUP
		}
		err := WriteNewIn(dir, path, []byte("loser\n"))
		if !errors.Is(err, fs.ErrExist) {
			t.Fatalf("err = %v, want fs.ErrExist", err)
		}
		if got := readAll(t, path); got != "winner\n" {
			t.Fatalf("the loser clobbered the winner: %q", got)
		}
		noTemp(t, dir)
	})
}
