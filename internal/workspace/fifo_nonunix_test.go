//go:build !unix

package workspace

import "testing"

// makeFifo reports false: this platform has no named pipe in the filesystem.
func makeFifo(*testing.T, string) bool { return false }
