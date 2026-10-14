package serve

import (
	"errors"

	"golang.org/x/sys/windows"
)

// Winsock reports a bind failure with its own codes, which syscall.EADDRINUSE
// and syscall.EACCES (invented values on Windows) never match.
func isWinsockAddrInUse(err error) bool { return errors.Is(err, windows.WSAEADDRINUSE) }

// WSAEACCES on a bind is a port another process holds exclusively or one in
// a range the system reserves (Hyper-V and WinNAT exclusions).
func isWinsockAccess(err error) bool { return errors.Is(err, windows.WSAEACCES) }
