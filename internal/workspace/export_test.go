package workspace

import "testing"

// Test-only exports for golden_test.go (package workspace_test). That file
// sits outside the package because it runs reindex after Init, and
// reindex → entity → workspace would cycle from an internal test file.

func InDir(t *testing.T, dir string, fn func()) {
	t.Helper()
	inDir(t, dir, fn)
}

func AssertManifest(t *testing.T, root, fixture string) {
	t.Helper()
	assertManifest(t, root, fixture)
}

func PresetVersion(preset string) string { return presetVersion(preset) }

// IndexName exposes the package's private copy of reindex.IndexName so the
// golden test can pin the two together.
const IndexName = indexName

// LockHeld reports whether another handle holds the workspace lock, for the
// external tests that run a verb's tails (migrate_lock_test.go).
func LockHeld(t *testing.T, root string) bool {
	t.Helper()
	return lockHeld(t, root)
}
