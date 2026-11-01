package cli

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/workspace"
)

// writeCmd is a fresh add or edit command, flags unset unless set is given.
func writeCmd(t *testing.T, name string, set map[string]string) *cobra.Command {
	t.Helper()
	cmd, _, err := newRoot().Find([]string{name})
	if err != nil || cmd.Name() != name {
		t.Fatalf("no %s command: %v", name, err)
	}
	for k, v := range set {
		if err := cmd.Flags().Set(k, v); err != nil {
			t.Fatal(err)
		}
	}
	return cmd
}

// No shipped preset declares a field add or edit would read as its own flag.
func TestShippedPresetsShadowNoFlag(t *testing.T) {
	for _, preset := range []string{"firm-ops", "build-hub"} {
		ws := filepath.Join(t.TempDir(), preset)
		if _, err := workspace.Init(preset, ws, workspace.InitOptions{}); err != nil {
			t.Fatal(err)
		}
		resolved, err := introspect.LoadSchema(ws)
		if err != nil {
			t.Fatal(err)
		}
		if got := shadowedFields(newRoot(), resolved); len(got) != 0 {
			t.Errorf("%s shadows %v", preset, got)
		}
	}
}

// One type's shadowed field blocks no other type's write, nor its own write
// without the flag; passing the ambiguous flag for that type refuses.
func TestShadowedFieldRefusesOnlyTheAmbiguousWrite(t *testing.T) {
	ws := filepath.Join(t.TempDir(), "ws")
	if _, err := workspace.Init("firm-ops", ws, workspace.InitOptions{}); err != nil {
		t.Fatal(err)
	}
	onto := filepath.Join(ws, ".khub", "ontology.yaml")
	raw, err := os.ReadFile(onto)
	if err != nil {
		t.Fatal(err)
	}
	extra := "    gadget:\n      attributes:\n        by: { type: text }\n"
	if err := os.WriteFile(onto, append(raw, []byte(extra)...), 0o644); err != nil {
		t.Fatal(err)
	}
	resolved, err := introspect.LoadSchema(ws)
	if err != nil {
		t.Fatal(err)
	}
	if got := shadowedFields(newRoot(), resolved); len(got) != 1 || got[0].Type != "gadget" ||
		got[0].Field != "by" || len(got[0].Commands) != 2 {
		t.Fatalf("shadowedFields = %+v", got)
	}

	by := map[string]string{"by": "human:ilya"}
	if err := refuseAmbiguousFlag(writeCmd(t, "add", by), ws, "person", ""); err != nil {
		t.Errorf("--by on another type refused: %v", err)
	}
	if err := refuseAmbiguousFlag(writeCmd(t, "add", nil), ws, "gadget", ""); err != nil {
		t.Errorf("a gadget write without --by refused: %v", err)
	}
	var le *errs.Located
	err = refuseAmbiguousFlag(writeCmd(t, "add", by), ws, "gadget", "")
	want := "--by is ambiguous for type 'gadget': it is both a `khub add` flag and the type's field 'by'. " +
		"Rename the field in the schema; until then, leave --by out (KHUB_ACTOR names the writer) " +
		"and set the field with `khub edit <id> by <value>`"
	if !errors.As(err, &le) || le.Code != "flag_shadowed" || le.Message != want {
		t.Fatalf("err = %v", err)
	}
}
