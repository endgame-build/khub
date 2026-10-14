//go:build unix

package cli

import (
	"errors"
	"os"
	"syscall"

	"golang.org/x/term"
)

// The path-construction errnos, in this platform's own numbering.
var (
	errnoENOTDIR      = int(syscall.ENOTDIR)
	errnoEISDIR       = int(syscall.EISDIR)
	errnoENAMETOOLONG = int(syscall.ENAMETOOLONG)
)

// osErrno is the identity here: the platform's errno is what khub prints.
func osErrno(se syscall.Errno) (int, string, bool) { return int(se), "", false }

func isBrokenPipe(err error) bool { return errors.Is(err, syscall.EPIPE) }

// isTerminal is IsTTY's third rung, the real isatty.
func isTerminal(f *os.File) bool { return term.IsTerminal(int(f.Fd())) }

// PrepareConsole has nothing to do where terminals take ANSI natively.
func PrepareConsole() {}
