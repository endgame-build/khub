//go:build unix

package workspace

import (
	"syscall"
	"testing"
)

// makeFifo creates a named pipe at path and reports that it did.
func makeFifo(t *testing.T, path string) bool {
	t.Helper()
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	return true
}
