package cli

import (
	"errors"
	"io/fs"
	"syscall"
	"testing"
)

// The Windows mapping borrows darwin's errno numbers and messages, because
// those are what the os_error fixtures pin. Hold every value it can produce
// against darwin's own syscall table, and the name each prints against the
// classification a darwin build makes.
func TestWindowsMappingAgreesWithDarwin(t *testing.T) {
	seen := map[posixErrno]bool{}
	for _, p := range windowsToPOSIX {
		seen[p] = true
	}
	for p := range seen {
		e := syscall.Errno(p.num)
		if e.Error() != p.text {
			t.Errorf("errno %d: table says %q, darwin says %q", p.num, p.text, e.Error())
		}
		want := "OSError"
		switch {
		case errors.Is(e, fs.ErrNotExist):
			want = "FileNotFoundError"
		case errors.Is(e, fs.ErrPermission):
			want = "PermissionError"
		case errors.Is(e, fs.ErrExist):
			want = "FileExistsError"
		}
		if got := errnoClassName(p.num); got != want {
			t.Errorf("errno %d: errnoClassName = %q, darwin prints %q", p.num, got, want)
		}
		if got := errorClass(e); got != want {
			t.Errorf("errno %d: errorClass = %q, darwin prints %q", p.num, got, want)
		}
	}
	for want, p := range map[syscall.Errno]posixErrno{
		syscall.ENOTDIR: posixENOTDIR, syscall.EISDIR: posixEISDIR,
		syscall.ENAMETOOLONG: posixENAMETOOLONG,
	} {
		if p.num != int(want) {
			t.Errorf("path-construction errno %v is %d in the table, %d on darwin", want, p.num, int(want))
		}
	}
}
