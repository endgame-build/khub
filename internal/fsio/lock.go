// Port of the locking half of core/entity.py `_mutate_collection`.
package fsio

import (
	"os"
	"path/filepath"

	"github.com/gofrs/flock"
)

// CollectionLockPath is the sidecar every collection mutation serializes on:
// `.khub/generated/locks/<type>.lock`. Under generated/ so it inherits the
// gitignore and the deletable-anytime contract — never the data file itself,
// since the atomic swap replaces that inode and a lock held on it would guard a
// dead file after the first writer's rename.
func CollectionLockPath(root, typeName string) string {
	return filepath.Join(root, ".khub", "generated", "locks", typeName+".lock")
}

// WithLock runs fn holding an exclusive advisory lock on lockPath, creating the
// lock directory first. gofrs/flock is flock(2) on Unix — the same lock table
// Python's fcntl.flock uses, so a Go and a Python khub interoperate during
// dual-ship (go-port-plan R9).
func WithLock(lockPath string, fn func() error) (err error) {
	if mkErr := os.MkdirAll(filepath.Dir(lockPath), 0o777); mkErr != nil {
		return mkErr
	}
	// 0644 matches the sidecar Python creates; gofrs/flock defaults to 0600,
	// which would break the Py<->Go interop this lock exists to preserve when
	// the two run as different users.
	lk := flock.New(lockPath, flock.SetPermissions(0o644))
	if lockErr := lk.Lock(); lockErr != nil {
		return lockErr
	}
	defer func() {
		// Python unlocks in a `finally` and lets a failure raise; keep the
		// failure visible rather than dropping it, but never mask fn's error.
		if unlockErr := lk.Unlock(); unlockErr != nil && err == nil {
			err = unlockErr
		}
	}()
	return fn()
}
