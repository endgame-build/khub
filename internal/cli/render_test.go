package cli

// The error-classification helpers, matched through errors.As rather than a
// bare type assertion. Every case here wraps, because unwrapped errors are
// what the parity fixtures already cover and what an assertion handles fine —
// the wrap is the whole of what errors.As buys, and the whole of what these
// tests exist to pin.

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/endgame-build/khub/internal/errs"
)

func pathErr(op, path string, errno syscall.Errno) *os.PathError {
	return &os.PathError{Op: op, Path: path, Err: errno}
}

func TestOSErrorClassificationSeesThroughAWrap(t *testing.T) {
	bare := pathErr("open", "/x/y.md", syscall.ENOENT)
	wrapped := fmt.Errorf("reading the entity: %w", bare)

	if !isOSError(wrapped) {
		t.Error("a wrapped *os.PathError is not recognised as an OS error")
	}
	// The point: the detailed envelope, not the "OSError: %s" fallback.
	got := osErrorMessage(wrapped)
	want := "FileNotFoundError: [Errno 2] No such file or directory: '/x/y.md'"
	if got != want {
		t.Errorf("osErrorMessage(wrapped) = %q, want %q", got, want)
	}
	if osErrorMessage(bare) != want {
		t.Errorf("the unwrapped rendering changed: %q", osErrorMessage(bare))
	}
}

func TestPathConstructionBugRendersThroughAWrapWithoutPanicking(t *testing.T) {
	// pathBugMessage used to assert unchecked, so it was safe only because
	// Guard gates on isPathConstructionBug first. Once the guard matches
	// through a wrap, a renderer that does not would panic instead of print.
	wrapped := fmt.Errorf("deleting: %w", pathErr("remove", "/a/b.md/b.md", syscall.ENOTDIR))

	if !isPathConstructionBug(wrapped) {
		t.Fatal("a wrapped ENOTDIR is not recognised as a path-construction bug")
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("pathBugMessage panicked on a wrapped error: %v", r)
		}
	}()
	msg := pathBugMessage(wrapped)
	for _, want := range []string{"/a/b.md/b.md", "Not a directory", "That path cannot exist as addressed"} {
		if !strings.Contains(msg, want) {
			t.Errorf("pathBugMessage lacks %q: %s", want, msg)
		}
	}
}

func TestPathBugMessageDoesNotPanicOnANonPathError(t *testing.T) {
	// Unreachable through Guard, which gates on isPathConstructionBug — the
	// helper pair exists so this stays a message rather than a panic if that
	// order ever changes.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked instead of rendering: %v", r)
		}
	}()
	if got := pathBugMessage(errors.New("plain")); !strings.Contains(got, "plain") {
		t.Errorf("message lost the cause: %q", got)
	}
}

func TestNonPathOSErrorsKeepThePlainEnvelope(t *testing.T) {
	// isOSError matches three types; osErrorMessage renders detail for
	// *os.PathError only. That asymmetry is deliberate and pinned here.
	link := &os.LinkError{Op: "symlink", Old: "/a", New: "/b", Err: syscall.EEXIST}
	if !isOSError(fmt.Errorf("wrapped: %w", link)) {
		t.Error("a wrapped *os.LinkError is not recognised as an OS error")
	}
	if got := osErrorMessage(link); !strings.HasPrefix(got, "OSError: ") {
		t.Errorf("a LinkError took the detailed branch: %q", got)
	}
}

func TestLocatedIsFoundThroughAWrap(t *testing.T) {
	// Guard matches *errs.Located with errors.As; the hand-rolled asLocated
	// walkers this replaced did the same walk by hand, in three copies.
	located := errs.New("schema_error", "Cannot read schema: file not found")
	var got *errs.Located
	if !errors.As(fmt.Errorf("loading: %w", located), &got) {
		t.Fatal("a wrapped *errs.Located was not found")
	}
	if got.Code != "schema_error" || got.Message != located.Message {
		t.Errorf("got %+v", got)
	}
}
