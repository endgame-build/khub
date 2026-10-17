package fsio

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// The classifier is wired in on Windows only; the errno values are Windows
// codes whatever they mean on the host running the test.
func TestSharingViolationClassifier(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"access denied", errorAccessDenied, true},
		{"sharing violation in a LinkError", &os.LinkError{Op: "renameat", Old: "a", New: "b", Err: errorSharingViolation}, true},
		{"lock violation in a PathError", &fs.PathError{Op: "removeat", Path: "a", Err: errorLockViolation}, true},
		{"file not found", &fs.PathError{Op: "removeat", Path: "a", Err: fs.ErrNotExist}, false},
		{"already exists", fs.ErrExist, false},
		{"another errno", &fs.PathError{Op: "renameat", Path: "a", Err: errorAccessDenied + 1}, false},
		{"not an errno", errors.New("boom"), false},
		{"nil", nil, false},
	}
	for _, c := range cases {
		if got := sharingViolation(c.err); got != c.want {
			t.Errorf("%s: sharingViolation(%v) = %v, want %v", c.name, c.err, got, c.want)
		}
	}
}

func TestRetry(t *testing.T) {
	transient := errors.New("transient")
	isTransient := func(err error) bool { return errors.Is(err, transient) }

	t.Run("succeeds after transient failures", func(t *testing.T) {
		var sleeps []time.Duration
		calls := 0
		err := retry(func() error {
			calls++
			if calls < 3 {
				return transient
			}
			return nil
		}, isTransient, func(d time.Duration) { sleeps = append(sleeps, d) }, retryBudget)
		if err != nil || calls != 3 {
			t.Fatalf("err = %v, calls = %d", err, calls)
		}
		if len(sleeps) != 2 || sleeps[0] != time.Millisecond || sleeps[1] != 2*time.Millisecond {
			t.Fatalf("sleeps = %v, want exponential backoff from 1ms", sleeps)
		}
	})

	t.Run("a permanent error returns at once", func(t *testing.T) {
		permanent := &fs.PathError{Op: "renameat", Path: "a", Err: fs.ErrPermission}
		calls := 0
		err := retry(func() error { calls++; return permanent }, isTransient,
			func(time.Duration) { t.Fatal("slept on a permanent error") }, retryBudget)
		if err != permanent || calls != 1 { //nolint:errorlint // identity is the point: retry returns the error untouched
			t.Fatalf("err = %v, calls = %d", err, calls)
		}
	})

	t.Run("the budget caps the wait and the last error surfaces unchanged", func(t *testing.T) {
		var slept time.Duration
		calls := 0
		var last error
		err := retry(func() error {
			calls++
			last = &fs.PathError{Op: "renameat", Path: "a", Err: transient}
			return last
		}, isTransient, func(d time.Duration) { slept += d }, retryBudget)
		if err != last { //nolint:errorlint // identity is the point: retry returns the error untouched
			t.Fatalf("err = %v, want the last attempt's error", err)
		}
		if slept != retryBudget {
			t.Fatalf("slept %v, want exactly the %v budget", slept, retryBudget)
		}
		if calls < 2 || calls > 20 {
			t.Fatalf("calls = %d", calls)
		}
	})
}

// Every reparse point is refused, a junction and a cloud placeholder alike:
// workspace storage holds plain files and directories only.
func TestReparseAttr(t *testing.T) {
	cases := []struct {
		name  string
		attrs uint32
		want  bool
	}{
		{"junction", fileAttributeReparsePoint | 0x10, true},
		{"cloud placeholder directory", fileAttributeReparsePoint | 0x10, true},
		{"cloud placeholder file", fileAttributeReparsePoint | 0x80, true},
		{"plain directory", 0x10, false},
		{"plain file", 0x80, false},
	}
	for _, c := range cases {
		if got := reparseAttr(c.attrs); got != c.want {
			t.Errorf("%s: reparseAttr(%#x) = %v, want %v", c.name, c.attrs, got, c.want)
		}
	}
}

// fileInPath's walk is platform-neutral. The not-exist errors are built by
// hand, as Windows reports a file in the path.
func TestFileInPathNamesTheFileInTheWay(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "dir", "file")
	if err := os.MkdirAll(filepath.Dir(file), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// The host's lstat, reporting a file in the path as Windows does.
	lstat := func(p string) (fs.FileInfo, error) {
		info, err := os.Lstat(p)
		if errors.Is(err, syscall.ENOTDIR) {
			err = &fs.PathError{Op: "lstat", Path: p, Err: fs.ErrNotExist}
		}
		return info, err
	}
	missing := func(path string) error { return &fs.PathError{Op: "statat", Path: path, Err: fs.ErrNotExist} }
	cases := []struct {
		name, root, path string
		notDir           bool
	}{
		{"below the file", root, filepath.Join(file, "a", "b"), true},
		{"the file itself", root, file, true},
		{"the root is the file", file, file, true},
		{"below a directory", root, filepath.Join(root, "dir", "gone"), false},
		{"nothing exists", root, filepath.Join(root, "gone", "deeper"), false},
		{"outside root", filepath.Join(root, "dir", "sub"), filepath.Join(file, "x"), false},
	}
	for _, c := range cases {
		in := missing(filepath.ToSlash(c.path))
		got := fileInPath(in, c.root, c.path, lstat)
		var pe *fs.PathError
		switch {
		case c.notDir && (!errors.As(got, &pe) || !errors.Is(pe.Err, NotADirectory) || pe.Op != "statat" || pe.Path != filepath.ToSlash(c.path)):
			t.Errorf("%s: got %v, want statat %s: NotADirectory", c.name, got, filepath.ToSlash(c.path))
		case !c.notDir && !errors.Is(got, in):
			t.Errorf("%s: got %v, want the error unchanged", c.name, got)
		}
	}
	for _, err := range []error{nil, fs.ErrNotExist, &fs.PathError{Op: "statat", Path: file, Err: fs.ErrPermission}} {
		if got := fileInPath(err, root, filepath.Join(file, "x"), lstat); !errors.Is(got, err) {
			t.Errorf("fileInPath(%v) = %v, want it unchanged", err, got)
		}
	}
}

// SyncedFolder compares Windows paths; they are plain strings here, so every
// platform runs the cases.
func TestSyncedFolderMatchesAtAComponentBoundary(t *testing.T) {
	env := map[string]string{
		"OneDrive":           `C:\Users\me\OneDrive`,
		"OneDriveCommercial": `D:\Work\OneDrive - Acme\`,
	}
	getenv := func(k string) string { return env[k] }
	cases := []struct {
		root, want string
		ok         bool
	}{
		{`C:\Users\me\OneDrive`, env["OneDrive"], true},
		{`C:\Users\me\OneDrive\notes\kb`, env["OneDrive"], true},
		{`c:/users/ME/onedrive/kb`, env["OneDrive"], true},
		{`D:\Work\OneDrive - Acme\kb`, env["OneDriveCommercial"], true},
		{`C:\Users\me\OneDriveX`, "", false},
		{`C:\Users\me\OneDrive2\kb`, "", false},
		{`C:\Users\me`, "", false},
		{`E:\kb`, "", false},
	}
	for _, c := range cases {
		got, ok := SyncedFolder(c.root, getenv)
		if got != c.want || ok != c.ok {
			t.Errorf("SyncedFolder(%q) = %q, %v; want %q, %v", c.root, got, ok, c.want, c.ok)
		}
	}
	if _, ok := SyncedFolder(`C:\kb`, func(string) string { return "" }); ok {
		t.Error("an unset variable matched")
	}
}
