//go:build !windows

package doctor

import (
	"io/fs"

	"golang.org/x/sys/unix"
)

// writableDir asks the kernel whether the effective user may create an
// entry in the directory abs (write and search), without writing anything.
// checked is always true here.
func writableDir(abs string) (checked bool, err error) {
	if err := unix.Faccessat(unix.AT_FDCWD, abs, unix.W_OK|unix.X_OK, unix.AT_EACCESS); err != nil {
		return true, &fs.PathError{Op: "access", Path: abs, Err: err}
	}
	return true, nil
}
