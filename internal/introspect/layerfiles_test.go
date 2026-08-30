package introspect

// LayerFiles is the one discovery walk LoadSchemaLayers resolves from, so what
// it reports as "absent" decides which error a reader is handed. Absence and
// failure must not collapse into the same answer.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/errs"
)

func TestLayerFilesReportsAbsenceAsEmpty(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".khub"), 0o777); err != nil {
		t.Fatal(err)
	}
	layers, err := LayerFiles(root)
	if err != nil {
		t.Fatalf("an empty .khub is absence, not failure: %v", err)
	}
	if len(layers) != 0 {
		t.Fatalf("layers = %v, want none", layers)
	}
}

func TestLayerFilesReportsAStatFailureAsItself(t *testing.T) {
	// .khub is a FILE, so stat on .khub/ontology.yaml fails with ENOTDIR —
	// not ErrNotExist. Read as absence, this surfaced as "file not found",
	// which names the wrong cause and sends a reader looking for a file that
	// is not the problem. (ENOTDIR rather than chmod: uid 0 ignores 0o000.)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".khub"), []byte("not a directory"), 0o666); err != nil {
		t.Fatal(err)
	}
	_, err := LayerFiles(root)
	if err == nil {
		t.Fatal("a stat failure was reported as absence")
	}
	var located *errs.Located
	if !errors.As(err, &located) {
		t.Fatalf("err is %T, want *errs.Located: %v", err, err)
	}
	if located.Code != "schema_error" {
		t.Errorf("code = %q, want schema_error", located.Code)
	}
	if strings.Contains(located.Error(), "file not found") {
		t.Errorf("the message still claims absence: %s", located.Error())
	}
	if !strings.Contains(located.Error(), "ontology.yaml") {
		t.Errorf("the message does not name the layer: %s", located.Error())
	}
}
