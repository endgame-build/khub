// Package cli is the thin, schema-introspecting adapter (design-memo layer 5).
// render.go ports cli/_render.py: the single output gate and the single error
// boundary every command routes through.
package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unicode"
	"unicode/utf8"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/omap"
)

// IsTTY ports rich Console.is_terminal, the gate the whole suite drives:
// TTY_COMPATIBLE ("0"/"1") wins, then FORCE_COLOR (set-and-nonempty → true,
// set-and-empty → false), then a real isatty check on stdout (isTerminal,
// which on Windows also recognises a Cygwin/MSYS2 pty such as mintty's).
func IsTTY() bool {
	switch os.Getenv("TTY_COMPATIBLE") {
	case "0":
		return false
	case "1":
		return true
	}
	if fc, set := os.LookupEnv("FORCE_COLOR"); set {
		return fc != ""
	}
	return isTerminal(os.Stdout)
}

// WantJSON is true for --format json or any non-TTY stream — for reads and writes alike.
func WantJSON(fmt_ string) bool { return fmt_ == "json" || !IsTTY() }

// Emit prints one JSON document (json format or non-TTY) or runs the human view.
func Emit(data any, fmt_ string, human func()) error {
	if WantJSON(fmt_) {
		doc, err := canon.EncodeCLI(data)
		if err != nil {
			return err
		}
		// The write error matters: EPIPE must reach Guard so `khub schema | head`
		// stays a non-error, exactly as _render.py re-raises BrokenPipeError.
		_, werr := fmt.Fprintln(os.Stdout, doc)
		return werr
	}
	human()
	return nil
}

// Note prints one advisory line to stderr, in JSON and human modes alike. A
// command calls it before Emit, so the line reaches the caller even when
// stdout is piped through `head`.
func Note(message string) { fmt.Fprintln(os.Stderr, "note: "+message) }

// ExitError carries the process exit code through the command return path.
type ExitError struct{ Code int }

// Error names the exit code; nothing prints it, the code is the message.
func (e *ExitError) Error() string { return fmt.Sprintf("exit %d", e.Code) }

// Fail renders one failure in the shape the caller asked for: the JSON error
// envelope on stdout under the gate, prose on stderr otherwise. Exit 2.
//
// Every Located failure is a REFUSAL — the call was malformed, or would have
// written something the schema forbids — and nothing was written, which is
// what an agent needs to know: correct the call and retry. Exit 1 is reserved
// for a gate that ran and failed (`validate`, `check`): the workspace is what
// is wrong, not the call. Usage errors share 2 because they are the same
// answer to the same question.
func Fail(message, code, fmt_ string) error {
	return FailLocated(&errs.Located{Code: code, Message: message}, fmt_)
}

// FailLocated is Fail for a Located error, carrying its machine fields into
// the envelope: `{"error": {"code", "message", "field"?, "expected"?,
// "suggestion"?}}`, in that order. A field the error does not set is absent,
// never null, so a refusal with nothing to add is the bare {code, message}
// pair. Prose mode prints the message alone; the extras are for machines.
func FailLocated(e *errs.Located, fmt_ string) error {
	if WantJSON(fmt_) {
		env := omap.New()
		inner := omap.New()
		inner.Set("code", e.Code)
		inner.Set("message", e.Message)
		if e.Field != "" {
			inner.Set("field", e.Field)
		}
		if len(e.Expected) > 0 {
			expected := make([]any, len(e.Expected))
			for i, v := range e.Expected {
				expected[i] = v
			}
			inner.Set("expected", expected)
		}
		if e.Suggestion != "" {
			inner.Set("suggestion", e.Suggestion)
		}
		env.Set("error", inner)
		doc, _ := canon.EncodeCLI(env)
		fmt.Println(doc)
	} else {
		fmt.Fprintln(os.Stderr, e.Message)
	}
	return &ExitError{Code: 2}
}

// Guard is the single error boundary: a Located error renders via Fail; an
// OS error renders as os_error with Python's "{TypeName}: {err}" shape;
// EPIPE passes through untouched (khub schema | head is not an error).
func Guard(fmt_ string, fn func() error) error {
	err := fn()
	if err == nil {
		return nil
	}
	var exitErr *ExitError
	if errors.As(err, &exitErr) {
		return exitErr
	}
	var located *errs.Located
	if errors.As(err, &located) {
		return FailLocated(located, fmt_)
	}
	var usage *errs.Usage
	if errors.As(err, &usage) {
		return err // root handler prints usage to stderr, exit 2
	}
	if isEPIPE(err) {
		return err // `khub schema | head` is not a failure; never dress it as one
	}
	if isPathConstructionBug(err) {
		// NOT dressed as os_error. The catch below exists so a read-only
		// directory or a vanished file reads as one clean line — but the bug
		// that motivated it was itself an OSError: delete() built
		// `knowledge/product/prd.md/prd.md` for a singleton and raised
		// ENOTDIR, and it was the loud failure that made the bad path obvious.
		// Folding that into the same tidy envelope hides khub's own defects in
		// the channel meant for the user's environment. ENOTDIR, EISDIR and
		// ENAMETOOLONG cannot be caused by a well-formed path, so they are
		// reported as what they are.
		return Fail(pathBugMessage(err), "internal_path_error", fmt_)
	}
	if isOSError(err) {
		return Fail(osErrorMessage(err), "os_error", fmt_)
	}
	return err
}

// isPathConstructionBug reports errno values that mean khub assembled a path
// that cannot exist, rather than the filesystem refusing a valid one.
//
// *os.PathError only, deliberately. os.Rename returns *os.LinkError and khub
// calls it once (internal/fsio's collection swap), but no errno in the set
// below can reach it: mutateCollection's in-lock ReadText catches EISDIR as a
// read, its MkdirAll catches ENOTDIR, and OpenFile on the temp sibling —
// four bytes longer than the destination — catches ENAMETOOLONG. EXDEV cannot
// happen between siblings. What a rename does fail with (ENOSPC, EDQUOT, EIO,
// EPERM) is the environment refusing a well-formed path, which is os_error and
// belongs there. Handling LinkError here would be a branch nothing can take.
func isPathConstructionBug(err error) bool {
	pe, ok := asPathError(err)
	if !ok {
		return false
	}
	switch errnoOf(pe.Err) {
	case errnoENOTDIR, errnoEISDIR, errnoENAMETOOLONG:
		return true
	}
	return false
}

// pathBugMessage names the path and both causes. The errno cannot tell them
// apart — a file sitting where a directory belongs produces exactly the same
// ENOTDIR as khub joining a filename onto a file — so claiming khub is at
// fault would be wrong about half the time. Naming the path is what diagnoses
// either one.
func pathBugMessage(err error) string {
	pe, ok := asPathError(err)
	if !ok {
		// Unreachable through Guard, which gates on isPathConstructionBug —
		// but the two match through one helper precisely so this stays a
		// message rather than a panic if that order ever changes.
		return fmt.Sprintf("OSError: %s", err.Error())
	}
	// The errno's own text carries the specific fault (Not a directory / Is a
	// directory / File name too long); the prose used to hardcode the ENOTDIR
	// case, which read as false for the other two this branch also matches.
	return fmt.Sprintf(
		"Cannot %s '%s': %s. That path cannot exist as addressed. "+
			"If you did not create it, khub built the path wrong — "+
			"please report it with this line.",
		pe.Op, filepath.ToSlash(pe.Path), strerror(pe.Err))
}

// isEPIPE is a write to a closed pipe: EPIPE, and on Windows the two Win32
// codes that mean the same (isBrokenPipe).
func isEPIPE(err error) bool { return isBrokenPipe(err) }

// asPathError is the one place that decides what counts as a *os.PathError.
// The guard (isPathConstructionBug) and the two renderers all go through it so
// they cannot disagree: a guard matching more broadly than its renderer used to
// mean a panic, not a misprint. errors.As rather than an assertion, per
// .claude/rules/go.md — an assertion stops matching the day anything wraps.
func asPathError(err error) (*os.PathError, bool) {
	var pe *os.PathError
	return pe, errors.As(err, &pe)
}

func isOSError(err error) bool {
	// *os.PathError, *os.LinkError, syscall errors — the OSError family.
	// osErrorMessage renders detail for the first only and falls through to
	// the plain envelope for the other two; that asymmetry is deliberate.
	var pe *os.PathError
	var le *os.LinkError
	var se *os.SyscallError
	return errors.As(err, &pe) || errors.As(err, &le) || errors.As(err, &se)
}

// osErrorMessage mirrors f"{type(err).__name__}: {err}". Python's OSError
// str is "[Errno N] message: 'path'"; the closest faithful Go rendering is
// pinned by fixtures — PermissionError/FileNotFoundError map from errno.
func osErrorMessage(err error) string {
	if pe, ok := asPathError(err); ok {
		return fmt.Sprintf("%s: [Errno %d] %s: '%s'", errorClass(pe.Err), errnoOf(pe.Err), strerror(pe.Err), filepath.ToSlash(pe.Path))
	}
	return fmt.Sprintf("OSError: %s", err.Error())
}

// errnoOf is the errno khub prints. On Windows a Win32 code is mapped onto the
// darwin errno the fixtures pin (osErrno); elsewhere it is the errno itself.
func errnoOf(err error) int {
	var se syscall.Errno
	if errors.As(err, &se) {
		n, _, _ := osErrno(se)
		return n
	}
	return 0
}

// strerror is the C strerror(3) text Python's OSError carries, which is the
// syscall message with its first letter capitalized ("Permission denied", "No
// such file or directory"). Go's syscall table lowercases it, and the os_error
// envelope is byte-pinned, so restore the case here. A Win32 code osErrno
// maps takes the darwin message in place of Windows' own text.
func strerror(err error) string {
	msg := err.Error()
	var se syscall.Errno
	if errors.As(err, &se) {
		if _, text, mapped := osErrno(se); mapped {
			msg = text
		}
	}
	if msg == "" {
		return msg
	}
	first, size := utf8.DecodeRuneInString(msg)
	return string(unicode.ToUpper(first)) + msg[size:]
}
