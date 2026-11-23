package workspace

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/omap"
)

// carryEdits keeps a key the shipped file lacks at any version, keeps a
// changed value only at the same version, and never carries a type the
// shipped ontology dropped.
func TestCarryEditsRule(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "storage.yaml")
	ws := "storage:\n  note: {layout: file, path: notes, format: yaml, id_prefix: nt}\n  gone: {layout: file, path: gone}\n"
	if err := os.WriteFile(path, []byte(ws), 0o644); err != nil {
		t.Fatal(err)
	}
	shippedDoc, err := canon.LoadDoc("storage:\n  note: {layout: file, path: notes, format: md}\n")
	if err != nil {
		t.Fatal(err)
	}
	shipped := shippedDoc.(*omap.Map)
	declared := map[string]bool{"note": true}
	keys := func(cs []Carried) []string {
		var out []string
		for _, c := range cs {
			out = append(out, c.Type+"."+c.Key)
		}
		return out
	}

	doc, carried, err := carryEdits(dir, path, "storage", shipped, declared, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := keys(carried); !slices.Equal(got, []string{"note.id_prefix"}) {
		t.Errorf("across versions carried %v, want only the added key", got)
	}
	note, _ := mapIn(doc, "storage")
	nv, _ := note.Get("note")
	if f, _ := nv.(*omap.Map).Get("format"); f != "md" {
		t.Errorf("across versions format = %v, want the shipped md", f)
	}

	_, carried, err = carryEdits(dir, path, "storage", shipped, declared, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := keys(carried); !slices.Equal(got, []string{"note.format", "note.id_prefix"}) {
		t.Errorf("at the same version carried %v", got)
	}
}

// A carried key the new schema rejects never blocks the upgrade: it is
// dropped with the resolver's reason, and the keys that resolve are kept.
func TestSettleCarriedDropsWhatDoesNotResolve(t *testing.T) {
	root := t.TempDir()
	khub := filepath.Join(root, ".khub")
	if err := os.MkdirAll(khub, 0o755); err != nil {
		t.Fatal(err)
	}
	ws := "policy:\n  note:\n    orphan: true\n    retired_flag: true\n"
	if err := os.WriteFile(filepath.Join(khub, "policy.yaml"), []byte(ws), 0o644); err != nil {
		t.Fatal(err)
	}
	load := func(s string) *omap.Map {
		v, err := canon.LoadDoc(s)
		if err != nil {
			t.Fatal(err)
		}
		return v.(*omap.Map)
	}
	merged := &flattened{
		Ontology: load("ontology:\n  entities:\n    note: {}\n"),
		Policy:   load("policy: {}\n"),
		Storage:  load("storage:\n  note: {layout: file, path: notes}\n"),
	}
	_, carried, dropped, err := settleCarried(root, khub, merged, map[string]bool{"note": true}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(carried) != 1 || carried[0].Key != "orphan" {
		t.Errorf("carried = %+v", carried)
	}
	if len(dropped) != 1 || dropped[0].Key != "retired_flag" || !strings.Contains(dropped[0].Reason, "retired_flag") {
		t.Errorf("dropped = %+v", dropped)
	}
}
