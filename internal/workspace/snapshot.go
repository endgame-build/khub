package workspace

// The schema snapshot file: `.khub/schema.applied.yaml` records the resolved
// schema the corpus was last brought in line with, so `khub schema diff` can
// report what has changed since. It is tracked in git (only .khub/generated/
// is ignored) — the baseline has to travel with the corpus it describes.
//
// Only an explicit `khub schema snapshot` writes it. `upgrade` carries it over
// untouched, so a preset upgrade shows up as pending changes.

import (
	"errors"
	"io/fs"
	"path/filepath"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/fsio"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/omap"
)

// SnapshotFile is the snapshot's workspace-relative path.
const SnapshotFile = ".khub/schema.applied.yaml"

const snapshotHeader = "# khub schema snapshot: the resolved schema this corpus was last migrated to.\n" +
	"# Written by `khub schema snapshot`; `khub schema diff` compares against it.\n"

// SnapshotResult is what `schema snapshot` reports.
type SnapshotResult struct {
	Path  string
	Types int
}

// WriteSnapshot records the workspace's current resolved schema as the
// baseline, replacing any earlier one.
func WriteSnapshot(root string) (*SnapshotResult, error) {
	return fsio.Locked(root, func() (*SnapshotResult, error) {
		resolved, err := introspect.LoadSchema(root)
		if err != nil {
			return nil, err
		}
		text, err := canon.DumpWide(introspect.SnapshotView(resolved))
		if err != nil {
			return nil, err
		}
		path := filepath.Join(root, osPath(SnapshotFile))
		if err := fsio.AtomicWriteIn(root, path, []byte(snapshotHeader+text)); err != nil {
			return nil, err
		}
		return &SnapshotResult{Path: SnapshotFile, Types: resolved.Types.Len()}, nil
	})
}

// DiffSnapshot compares the current resolved schema against the recorded
// baseline. A workspace with no snapshot refuses, naming the call that
// records one.
func DiffSnapshot(root string) ([]any, error) {
	raw, err := fsio.ReadFile(root, filepath.Join(root, osPath(SnapshotFile)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, errs.New("no_schema_snapshot",
			"No schema snapshot at "+SnapshotFile+" — run `khub schema snapshot` to record the baseline")
	}
	if err != nil {
		return nil, err
	}
	old, err := loadSnapshot(string(raw))
	if err != nil {
		return nil, err
	}
	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		return nil, err
	}
	// Round-trip the current view through the same emitter and loader, so both
	// sides of the comparison carry the same scalar representation.
	text, err := canon.DumpWide(introspect.SnapshotView(resolved))
	if err != nil {
		return nil, err
	}
	cur, err := canon.LoadDocMode(text, canon.Mode12)
	if err != nil {
		return nil, err
	}
	return introspect.DiffSnapshot(old, cur.(*omap.Map)), nil
}

func loadSnapshot(text string) (*omap.Map, error) {
	v, err := canon.LoadDocMode(text, canon.Mode12)
	m, ok := v.(*omap.Map)
	if ok {
		types, _ := m.Get("types")
		_, ok = types.(*omap.Map)
	}
	if err != nil || !ok {
		return nil, errs.New("invalid_snapshot",
			SnapshotFile+" is not a schema snapshot — rerun `khub schema snapshot` to record the baseline again")
	}
	return m, nil
}
