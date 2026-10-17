package cli

import (
	"errors"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/term"
)

// The path-construction errnos, in the darwin numbering osErrno maps onto.
var (
	errnoENOTDIR      = posixENOTDIR.num
	errnoEISDIR       = posixEISDIR.num
	errnoENAMETOOLONG = posixENAMETOOLONG.num
)

// osErrno maps a Win32 code onto the errno and message a Mac prints, so the
// os_error envelope reads the same on every platform. An unmapped code prints
// as Windows reports it.
func osErrno(se syscall.Errno) (int, string, bool) {
	if p, ok := windowsToPOSIX[uint32(se)]; ok {
		return p.num, p.text, true
	}
	return int(se), "", false
}

// isBrokenPipe adds the two codes a write to a closed pipe returns on
// Windows: ERROR_BROKEN_PIPE, and ERROR_NO_DATA ("The pipe is being closed"),
// which is what `khub schema | head` meets once head exits.
func isBrokenPipe(err error) bool {
	return errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, windows.ERROR_BROKEN_PIPE) ||
		errors.Is(err, windows.ERROR_NO_DATA)
}

// isTerminal is IsTTY's third rung, the real isatty. A Windows console
// answers through term.IsTerminal. A Cygwin or MSYS2 pty (mintty, the
// standalone Git Bash window) is a named pipe to Windows, so it is recognised
// by the pipe's name — the same signal, read the only way that pty offers it.
func isTerminal(f *os.File) bool {
	fd := f.Fd()
	if term.IsTerminal(int(fd)) {
		return true
	}
	return isCygwinPty(windows.Handle(fd))
}

func isCygwinPty(h windows.Handle) bool {
	if ft, err := windows.GetFileType(h); err != nil || ft != windows.FILE_TYPE_PIPE {
		return false
	}
	// FILE_NAME_INFO: a uint32 byte length, then the UTF-16 name.
	var buf [4 + windows.MAX_PATH*2]byte
	if err := windows.GetFileInformationByHandleEx(h, windows.FileNameInfo, &buf[0], uint32(len(buf))); err != nil {
		return false
	}
	n := *(*uint32)(unsafe.Pointer(&buf[0])) / 2
	if n == 0 || int(n) > windows.MAX_PATH {
		return false
	}
	name := unsafe.Slice((*uint16)(unsafe.Pointer(&buf[4])), n)
	return isCygwinPtyName(windows.UTF16ToString(name))
}

// PrepareConsole turns on ANSI escape processing for stdout and stderr when
// they are a Windows console, so the human view's colours render rather than
// print as escape codes. A stream that is not a console, or a console that
// refuses, is left as it is: the bytes khub writes do not change either way.
// The console mode outlives khub, so restore puts back each mode it changed,
// last changed first: stdout and stderr can share one screen buffer, and the
// first saved mode is the one from before khub ran.
func PrepareConsole() (restore func()) {
	var undo []func()
	for _, f := range []*os.File{os.Stdout, os.Stderr} {
		h := windows.Handle(f.Fd())
		var mode uint32
		if windows.GetConsoleMode(h, &mode) != nil {
			continue
		}
		if windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING) == nil {
			undo = append(undo, func() { _ = windows.SetConsoleMode(h, mode) })
		}
	}
	return func() {
		for i := len(undo) - 1; i >= 0; i-- {
			undo[i]()
		}
	}
}
