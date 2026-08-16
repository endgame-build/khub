// Package cli is the thin, schema-introspecting adapter (design-memo layer 5).
// render.go ports cli/_render.py: the single output gate and the single error
// boundary every command routes through.
package cli

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"unicode"
	"unicode/utf8"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/omap"
	"golang.org/x/term"
)

// IsTTY ports rich Console.is_terminal, the gate the whole suite drives:
// TTY_COMPATIBLE ("0"/"1") wins, then FORCE_COLOR (set-and-nonempty → true,
// set-and-empty → false), then a real isatty check on stdout.
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
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// WantJSON: --format json or any non-TTY stream — for reads and writes alike.
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

// ExitError carries the process exit code through the command return path.
type ExitError struct{ Code int }

func (e *ExitError) Error() string { return fmt.Sprintf("exit %d", e.Code) }

// Fail renders one failure in the shape the caller asked for: the JSON error
// envelope on stdout under the gate, prose on stderr otherwise. Exit 1.
func Fail(message, code, fmt_ string) error {
	if WantJSON(fmt_) {
		env := omap.New()
		inner := omap.New()
		inner.Set("code", code)
		inner.Set("message", message)
		env.Set("error", inner)
		doc, _ := canon.EncodeCLI(env)
		fmt.Println(doc)
	} else {
		fmt.Fprintln(os.Stderr, message)
	}
	return &ExitError{Code: 1}
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
		return Fail(located.Message, located.Code, fmt_)
	}
	var usage *errs.Usage
	if errors.As(err, &usage) {
		return err // root handler prints usage to stderr, exit 2
	}
	if isEPIPE(err) {
		return err // `khub schema | head` is not a failure; never dress it as one
	}
	if isOSError(err) {
		return Fail(osErrorMessage(err), "os_error", fmt_)
	}
	return err
}

func isEPIPE(err error) bool { return errors.Is(err, syscall.EPIPE) }

func isOSError(err error) bool {
	// *os.PathError, *os.LinkError, syscall errors — the OSError family
	switch err.(type) {
	case *os.PathError, *os.LinkError, *os.SyscallError:
		return true
	}
	return false
}

// osErrorMessage mirrors f"{type(err).__name__}: {err}". Python's OSError
// str is "[Errno N] message: 'path'"; the closest faithful Go rendering is
// pinned by fixtures — PermissionError/FileNotFoundError map from errno.
func osErrorMessage(err error) string {
	if pe, ok := err.(*os.PathError); ok {
		name := "OSError"
		switch {
		case os.IsNotExist(pe):
			name = "FileNotFoundError"
		case os.IsPermission(pe):
			name = "PermissionError"
		case os.IsExist(pe):
			name = "FileExistsError"
		}
		errno := errnoOf(pe.Err)
		return fmt.Sprintf("%s: [Errno %d] %s: '%s'", name, errno, strerror(pe.Err), pe.Path)
	}
	return fmt.Sprintf("OSError: %s", err.Error())
}

func errnoOf(err error) int {
	var se syscall.Errno
	if errors.As(err, &se) {
		return int(se)
	}
	return 0
}

// strerror is the C strerror(3) text Python's OSError carries, which is the
// syscall message with its first letter capitalized ("Permission denied", "No
// such file or directory"). Go's syscall table lowercases it, and the os_error
// envelope is byte-pinned, so restore the case here.
func strerror(err error) string {
	msg := err.Error()
	if msg == "" {
		return msg
	}
	first, size := utf8.DecodeRuneInString(msg)
	return string(unicode.ToUpper(first)) + msg[size:]
}
