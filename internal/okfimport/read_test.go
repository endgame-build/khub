package okfimport

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// A concept file swapped for a link between the check and the open is
// refused, not followed; one left alone reads normally.
func TestReadConceptRefusesAFileSwappedForALink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	dir := t.TempDir()
	secret := filepath.Join(dir, "secret.md")
	file := filepath.Join(dir, "a.md")
	for _, p := range []string{secret, file} {
		if err := os.WriteFile(p, []byte("---\ntype: note\n---\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := readConcept(file); err != nil {
		t.Fatalf("plain read: %v", err)
	}
	t.Cleanup(func() { openFile = os.Open })
	openFile = func(p string) (*os.File, error) {
		if err := os.Remove(p); err != nil {
			return nil, err
		}
		if err := os.Symlink(secret, p); err != nil {
			return nil, err
		}
		return os.Open(p)
	}
	if _, err := readConcept(file); !errors.Is(err, errSymlink) {
		t.Fatalf("got %v", err)
	}
}
