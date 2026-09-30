package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/presets"
)

// A snapshot taken and immediately diffed reports nothing, for every shipped
// preset and every parity-corpus preset that initializes. A view that does not
// emit and load back deterministically (map iteration order, a scalar that
// re-resolves differently) would surface here as a phantom change.
func TestSnapshotRoundTripsEveryPreset(t *testing.T) {
	type src struct{ name, dir string }
	var all []src
	for _, name := range presets.Known(presets.Embedded()) {
		all = append(all, src{name, ""})
	}
	corpus, err := filepath.Abs(filepath.Join("..", "..", "parity", "corpus"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"preset-bespoke", "preset-bykind", "preset-cells"} {
		all = append(all, src{name, corpus})
	}
	for _, p := range all {
		t.Run(p.name, func(t *testing.T) {
			ws := filepath.Join(t.TempDir(), "ws")
			mustInit(t, p.name, ws, InitOptions{PresetSource: p.dir})
			if _, err := WriteSnapshot(ws); err != nil {
				t.Fatal(err)
			}
			changes, err := DiffSnapshot(ws)
			if err != nil {
				t.Fatal(err)
			}
			if len(changes) != 0 {
				t.Fatalf("fresh snapshot diffs against itself: %v", changes)
			}
		})
	}
}

func TestSnapshotDiffRefusesWithoutABaseline(t *testing.T) {
	ws := filepath.Join(t.TempDir(), "ws")
	mustInit(t, "note", ws, InitOptions{PresetSource: presetSource(t)})
	_, err := DiffSnapshot(ws)
	var located *errs.Located
	if !errors.As(err, &located) || located.Code != "no_schema_snapshot" {
		t.Fatalf("want no_schema_snapshot, got %v", err)
	}
	// A list, and a mapping with no `types`, are not snapshots; diffing against
	// either would report every type as added.
	for _, text := range []string{"- not\n- a map\n", "other: 1\n"} {
		if err := os.WriteFile(filepath.Join(ws, SnapshotFile), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err = DiffSnapshot(ws)
		if !errors.As(err, &located) || located.Code != "invalid_snapshot" {
			t.Fatalf("%q: want invalid_snapshot, got %v", text, err)
		}
	}
}
