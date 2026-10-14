package fsio

import (
	"errors"
	"io/fs"
	"os"
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
