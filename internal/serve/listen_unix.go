//go:build unix

package serve

// Off Windows the POSIX errnos Listen tests are the whole answer.
func isWinsockAddrInUse(error) bool { return false }

func isWinsockAccess(error) bool { return false }
