package cli

import (
	"errors"
	"io/fs"
	"regexp"
	"strings"
	"syscall"
)

// posixErrno is one errno as khub prints it in the os_error envelope: the
// number and syscall's lowercase message, which strerror capitalizes.
type posixErrno struct {
	num  int
	text string
}

// The darwin numbers and messages, which are what the os_error fixtures pin.
// A Windows build has no errno of its own to print — its syscall.Errno is a
// Win32 code with FormatMessage text — so it borrows these and prints the same
// line a Mac does. oserrno_darwin_test.go holds every value here against
// darwin's syscall table.
var (
	posixENOENT       = posixErrno{2, "no such file or directory"}
	posixEBADF        = posixErrno{9, "bad file descriptor"}
	posixENOMEM       = posixErrno{12, "cannot allocate memory"}
	posixEACCES       = posixErrno{13, "permission denied"}
	posixEBUSY        = posixErrno{16, "resource busy"}
	posixEEXIST       = posixErrno{17, "file exists"}
	posixEXDEV        = posixErrno{18, "cross-device link"}
	posixENOTDIR      = posixErrno{20, "not a directory"}
	posixEISDIR       = posixErrno{21, "is a directory"}
	posixEINVAL       = posixErrno{22, "invalid argument"}
	posixEMFILE       = posixErrno{24, "too many open files"}
	posixENOSPC       = posixErrno{28, "no space left on device"}
	posixEROFS        = posixErrno{30, "read-only file system"}
	posixEPIPE        = posixErrno{32, "broken pipe"}
	posixENAMETOOLONG = posixErrno{63, "file name too long"}
	posixENOTEMPTY    = posixErrno{66, "directory not empty"}
)

// windowsToPOSIX maps a Win32 error code onto the errno khub prints. Codes
// are literals because golang.org/x/sys/windows does not build off Windows and
// the table is tested on darwin; the name of each is beside it. The grouping
// follows CPython's winerror_to_errno, except ERROR_FILENAME_EXCED_RANGE,
// which is ENAMETOOLONG here so it reads as the path-construction fault it is.
// A code not listed prints as Windows reports it.
var windowsToPOSIX = map[uint32]posixErrno{
	2:   posixENOENT,       // ERROR_FILE_NOT_FOUND
	3:   posixENOENT,       // ERROR_PATH_NOT_FOUND
	4:   posixEMFILE,       // ERROR_TOO_MANY_OPEN_FILES
	5:   posixEACCES,       // ERROR_ACCESS_DENIED
	6:   posixEBADF,        // ERROR_INVALID_HANDLE
	8:   posixENOMEM,       // ERROR_NOT_ENOUGH_MEMORY
	14:  posixENOMEM,       // ERROR_OUTOFMEMORY
	15:  posixENOENT,       // ERROR_INVALID_DRIVE
	16:  posixEACCES,       // ERROR_CURRENT_DIRECTORY
	17:  posixEXDEV,        // ERROR_NOT_SAME_DEVICE
	19:  posixEROFS,        // ERROR_WRITE_PROTECT
	32:  posixEACCES,       // ERROR_SHARING_VIOLATION
	33:  posixEACCES,       // ERROR_LOCK_VIOLATION
	39:  posixENOSPC,       // ERROR_HANDLE_DISK_FULL
	53:  posixENOENT,       // ERROR_BAD_NETPATH
	65:  posixEACCES,       // ERROR_NETWORK_ACCESS_DENIED
	67:  posixENOENT,       // ERROR_BAD_NET_NAME
	80:  posixEEXIST,       // ERROR_FILE_EXISTS
	87:  posixEINVAL,       // ERROR_INVALID_PARAMETER
	109: posixEPIPE,        // ERROR_BROKEN_PIPE
	112: posixENOSPC,       // ERROR_DISK_FULL
	145: posixENOTEMPTY,    // ERROR_DIR_NOT_EMPTY
	161: posixENOENT,       // ERROR_BAD_PATHNAME
	170: posixEBUSY,        // ERROR_BUSY
	183: posixEEXIST,       // ERROR_ALREADY_EXISTS
	206: posixENAMETOOLONG, // ERROR_FILENAME_EXCED_RANGE
	232: posixEPIPE,        // ERROR_NO_DATA ("The pipe is being closed")
	267: posixENOTDIR,      // ERROR_DIRECTORY
}

// errorClass is the exception name khub prints beside err. A Win32 code
// osErrno maps is classed by the darwin errno it prints (errnoClassName), so
// ERROR_SHARING_VIOLATION reads PermissionError beside its 13. Anything else
// is classed as it reports itself, which on darwin is the same answer.
func errorClass(err error) string {
	var se syscall.Errno
	if errors.As(err, &se) {
		if n, _, mapped := osErrno(se); mapped {
			return errnoClassName(n)
		}
	}
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "FileNotFoundError"
	case errors.Is(err, fs.ErrPermission):
		return "PermissionError"
	case errors.Is(err, fs.ErrExist):
		return "FileExistsError"
	}
	return "OSError"
}

// errnoClassName is the exception name a darwin build prints beside errno n:
// the fs.ErrNotExist / ErrPermission / ErrExist classes Go's unix
// syscall.Errno.Is reports, restricted to the errnos windowsToPOSIX yields.
func errnoClassName(n int) string {
	switch n {
	case posixENOENT.num:
		return "FileNotFoundError"
	case posixEACCES.num:
		return "PermissionError"
	case posixEEXIST.num, posixENOTEMPTY.num:
		return "FileExistsError"
	}
	return "OSError"
}

// cygwinPtyName matches the pipe name a Cygwin or MSYS2 pty presents in place
// of a console: `\msys-dd50a72ab4668b33-pty0-to-master`, from the installation
// key, the pty number and the direction. mintty (the standalone Git Bash
// window) gives its programs such a pipe, so a real isatty answers false
// there. The kernel may report the name with or without the
// `\Device\NamedPipe` prefix.
var cygwinPtyName = regexp.MustCompile(`^\\(?:cygwin|msys)-[0-9a-fA-F]+-pty[0-9]+-(?:from|to)-master`)

// isCygwinPtyName is the check mattn/go-isatty's IsCygwinTerminal makes,
// tightened to the documented shape and kept here as a pure function so it is
// tested off Windows.
func isCygwinPtyName(name string) bool {
	return cygwinPtyName.MatchString(strings.TrimPrefix(name, `\Device\NamedPipe`))
}
