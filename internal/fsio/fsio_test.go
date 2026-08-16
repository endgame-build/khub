package fsio

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
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

func TestCollectionLockPathIsTheGeneratedSidecar(t *testing.T) {
	got := CollectionLockPath(filepath.FromSlash("/ws"), "repo")
	want := filepath.FromSlash("/ws/.khub/generated/locks/repo.lock")
	if got != want {
		t.Fatalf("lock path = %q, want %q", got, want)
	}
}

func TestWithLockCreatesTheDirAndSerializes(t *testing.T) {
	root := t.TempDir()
	lock := CollectionLockPath(root, "repo")
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
			errCh <- WithLock(lock, func() error {
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
			t.Fatal(err)
		}
	}
	if got := readAll(t, data); len(got) != workers+1 {
		t.Fatalf("counter = %q (%d writes landed), want %d", got, len(got)-1, workers)
	}
	if _, err := os.Stat(lock); err != nil {
		t.Fatalf("lock sidecar missing: %v", err)
	}
}

func TestWithLockReleasesOnError(t *testing.T) {
	root := t.TempDir()
	lock := CollectionLockPath(root, "repo")
	sentinel := errors.New("mutate failed")

	if err := WithLock(lock, func() error { return sentinel }); !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want the callback's error", err)
	}
	// A leaked lock would deadlock this second acquisition.
	if err := WithLock(lock, func() error { return nil }); err != nil {
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
