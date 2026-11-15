package fsio

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
)

// The Check* functions refuse where their writing twins refuse, and leave
// the directory exactly as they found it.
func TestChecksRefuseLikeTheWritesAndWriteNothing(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	existing := filepath.Join(root, "acme.md")
	if err := os.WriteFile(existing, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}

	// A new file where one exists, and replacing a directory.
	if err := CheckPublish(root, existing, true); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("exclusive publish over a file: %v", err)
	}
	if err := WriteNewIn(root, existing, []byte("x")); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("the write disagrees: %v", err)
	}
	if err := CheckPublish(root, filepath.Join(root, "dir"), false); !errors.Is(err, fs.ErrInvalid) {
		t.Fatalf("replacing a directory: %v", err)
	}
	// Fine: a replacement, and a new file under a directory MkdirAll would make.
	if err := CheckPublish(root, existing, false); err != nil {
		t.Fatal(err)
	}
	if err := CheckPublish(root, filepath.Join(root, "new", "deep", "x.md"), true); err != nil {
		t.Fatal(err)
	}
	if err := CheckMkdirAll(root, filepath.Join(root, "new", "deep")); err != nil {
		t.Fatal(err)
	}
	// A file where a directory must go.
	if err := CheckMkdirAll(root, filepath.Join(existing, "sub")); !errors.Is(err, syscall.ENOTDIR) {
		t.Fatalf("mkdir through a file: %v", err)
	}
	if err := CheckRemove(root, filepath.Join(root, "ghost.md")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("removing nothing: %v", err)
	}
	if err := CheckRemove(root, existing); err != nil {
		t.Fatal(err)
	}
	// Escapes refuse, as OpenPath refuses them for every write.
	if err := CheckPublish(root, filepath.Join(outside, "x"), false); err == nil {
		t.Fatal("escape accepted")
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		if runtime.GOOS != "windows" {
			t.Fatal(err)
		}
	} else {
		for _, check := range []func() error{
			func() error { return CheckPublish(root, filepath.Join(root, "link", "x"), true) },
			func() error { return CheckMkdirAll(root, filepath.Join(root, "link", "sub")) },
			func() error { return CheckRemove(root, filepath.Join(root, "link")) },
		} {
			if err := check(); err == nil {
				t.Fatal("a symlink accepted")
			}
		}
		if err := os.Remove(filepath.Join(root, "link")); err != nil {
			t.Fatal(err)
		}
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || readAll(t, existing) != "keep" {
		t.Fatalf("a check wrote: %v", entries)
	}
}

// CheckMkdirAll fails with MkdirAll's own error, byte for byte, wherever a
// file stands in the way: at the last component (mkdirat EEXIST) and in the
// middle (the walk's Stat, ENOTDIR on the component).
func TestCheckMkdirAllMatchesMkdirAll(t *testing.T) {
	for _, target := range []string{"a/f", "a/f/g", "a/f/g/h", "a/new/x", "a"} {
		t.Run(target, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, "a"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "a", "f"), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, filepath.FromSlash(target))
			dry := CheckMkdirAll(root, path)
			if _, err := os.Lstat(filepath.Join(root, "a", "new")); !errors.Is(err, fs.ErrNotExist) {
				t.Fatal("the check created a directory")
			}
			real := MkdirAll(root, path)
			if (dry == nil) != (real == nil) || (dry != nil && dry.Error() != real.Error()) {
				t.Fatalf("check = %v, mkdir = %v", dry, real)
			}
		})
	}
}

// CheckLock refuses where Locked does, before creating anything.
func TestCheckLockMatchesLocked(t *testing.T) {
	setups := map[string]func(t *testing.T, root string){
		"generated is a file": func(t *testing.T, root string) {
			if err := os.WriteFile(filepath.Join(root, ".khub", "generated"), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		},
		"generated is a symlink": func(t *testing.T, root string) {
			if err := os.Symlink(t.TempDir(), filepath.Join(root, ".khub", "generated")); err != nil {
				t.Skipf("no symlinks here: %v", err)
			}
		},
		"nothing there yet": func(*testing.T, string) {},
	}
	for name, setup := range setups {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Mkdir(filepath.Join(root, ".khub"), 0o755); err != nil {
				t.Fatal(err)
			}
			setup(t, root)
			dry := CheckLock(root)
			if _, err := os.Lstat(WorkspaceLockPath(root)); !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, syscall.ENOTDIR) {
				t.Fatalf("the check created the lock: %v", err)
			}
			_, real := Locked(root, func() (struct{}, error) { return struct{}{}, nil })
			if (dry == nil) != (real == nil) || (dry != nil && dry.Error() != real.Error()) {
				t.Fatalf("check = %v, locked = %v", dry, real)
			}
		})
	}
}
