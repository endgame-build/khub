package entity

// --dry-run on the write verbs: every refusal the write raises, and no byte
// written — not an entity, not a collection, not the workspace lock.

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// snapshot maps every file under root to its bytes.
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		if d.IsDir() {
			out[rel+"/"] = ""
			return nil
		}
		raw, rerr := os.ReadFile(p)
		out[rel] = string(raw)
		return rerr
	})
	requireNoError(t, err)
	return out
}

func requireSameTree(t *testing.T, before, after map[string]string) {
	t.Helper()
	for p, b := range before {
		if a, ok := after[p]; !ok || a != b {
			t.Fatalf("dry run changed %s", p)
		}
	}
	for p := range after {
		if _, ok := before[p]; !ok {
			t.Fatalf("dry run created %s", p)
		}
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	ws := collectionWS(t)
	referencedClient(t, ws)
	seed(t, ws, "identity/team/noor.md", kv("type", "person", "name", "Noor"))
	_, err := Create(ws, "repo", CreateOpts{Fields: fields("repo", "acme/one"), ID: "one", UseTemplate: true})
	requireNoError(t, err)
	requireNoError(t, os.RemoveAll(filepath.Join(ws, ".khub", "generated")))
	before := snapshot(t, ws)

	created, err := Create(ws, "opportunity", CreateOpts{
		Fields: fields("name", "Deal", "client", "initech"), UseTemplate: true, DryRun: true})
	requireNoError(t, err)
	if created.Slug != "deal" || relSlash(ws, created.Path) != "opportunities/deal/_index.md" {
		t.Fatalf("preview = %+v", created)
	}
	row, err := Create(ws, "repo", CreateOpts{Fields: fields("repo", "acme/two"), ID: "two", UseTemplate: true, DryRun: true})
	requireNoError(t, err)
	if row.Locator != "repo.jsonl#two" {
		t.Fatalf("row preview = %+v", row)
	}

	edited, err := Update(ws, "o1", UpdateOpts{Fields: fields("stage", "won"), DryRun: true})
	requireNoError(t, err)
	if !edited.Changed {
		t.Fatal("an edit that moves a value reported no change")
	}
	rowEdit, err := Update(ws, "one", UpdateOpts{Fields: fields("status", "archived"), DryRun: true})
	requireNoError(t, err)
	if !rowEdit.Changed {
		t.Fatal("a row edit that moves a value reported no change")
	}

	linked, err := Link(ws, "o1", "owner", "noor", true)
	requireNoError(t, err)
	if !linked.Changed {
		t.Fatal("a new edge reported no change")
	}
	present, err := Link(ws, "o1", "client", "initech", true)
	requireNoError(t, err)
	if present.Changed {
		t.Fatal("an existing edge reported a change")
	}
	unlinked, err := Unlink(ws, "o1", "client", "initech", true)
	requireNoError(t, err)
	if !unlinked.Changed {
		t.Fatal("removing an edge reported no change")
	}

	refused, err := Delete(ws, "initech", false, true)
	requireNoError(t, err)
	if refused.Removed || len(refused.Inbound) != 3 {
		t.Fatalf("guard: removed=%v inbound=%d", refused.Removed, len(refused.Inbound))
	}
	for _, id := range []string{"initech", "o2", "one"} {
		removed, derr := Delete(ws, id, true, true)
		requireNoError(t, derr)
		if !removed.Removed {
			t.Fatalf("%s: forced preview not removed", id)
		}
	}

	requireSameTree(t, before, snapshot(t, ws))
}

// A dry run refuses exactly where the write does, with the same code; the
// write then runs as if the dry run had never happened.
func TestDryRunRefusesAsTheWriteDoes(t *testing.T) {
	ws := newWS(t, "build-hub")
	_, err := Create(ws, "prd", CreateOpts{UseTemplate: true})
	requireNoError(t, err)
	_, err = Create(ws, "requirement", CreateOpts{Fields: fields("title", "T", "kind", "functional"), UseTemplate: true})
	requireNoError(t, err)

	cases := []struct {
		name string
		code string
		run  func(dry bool) error
	}{
		{"singleton exists", "singleton_exists", func(dry bool) error {
			_, err := Create(ws, "prd", CreateOpts{UseTemplate: true, DryRun: dry})
			return err
		}},
		{"slug taken", "slug_taken", func(dry bool) error {
			_, err := Create(ws, "requirement", CreateOpts{Fields: fields("title", "T"), UseTemplate: true, DryRun: dry})
			return err
		}},
		{"enum", "enum_violation", func(dry bool) error {
			_, err := Update(ws, "req-t", UpdateOpts{Fields: fields("kind", "bogus"), DryRun: dry})
			return err
		}},
		{"dangling target", "referential_integrity", func(dry bool) error {
			_, err := Link(ws, "req-t", "realized_in", "ghost", dry)
			return err
		}},
		{"self link", "self_link", func(dry bool) error {
			_, err := Link(ws, "req-t", "related", "req-t", dry)
			return err
		}},
		{"illegal predicate", "illegal_predicate", func(dry bool) error {
			_, err := Unlink(ws, "req-t", "nope", "x", dry)
			return err
		}},
		{"unknown id", "lookup_error", func(dry bool) error {
			_, err := Delete(ws, "ghost", true, dry)
			return err
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_ = requireCode(t, c.run(true), c.code)
			_ = requireCode(t, c.run(false), c.code)
		})
	}
}

// Changed is false only when the stored bytes would not move.
func TestDryRunEditReportsNoChange(t *testing.T) {
	ws := newWS(t, "build-hub")
	t.Setenv("KHUB_PARITY_NOW", "2026-01-15")
	_, err := Create(ws, "requirement", CreateOpts{Fields: fields("title", "T", "kind", "functional"), UseTemplate: true})
	requireNoError(t, err)

	same, err := Update(ws, "req-t", UpdateOpts{Fields: fields("kind", "functional"), DryRun: true})
	requireNoError(t, err)
	if same.Changed {
		t.Fatal("an edit to the stored value reported a change")
	}
	t.Setenv("KHUB_PARITY_NOW", "2026-01-16")
	bumped, err := Update(ws, "req-t", UpdateOpts{Fields: fields("kind", "functional"), DryRun: true})
	requireNoError(t, err)
	if !bumped.Changed {
		t.Fatal("a new day's `updated` bump reported no change")
	}
}

// Filesystem refusals khub reaches before publishing — the lock directory,
// a file where an entity's folder must go — fail the dry run with the
// write's own error, byte for byte.
func TestDryRunFailsWithTheWritesPathError(t *testing.T) {
	setups := map[string]func(t *testing.T, ws string){
		"lock directory is a file": func(t *testing.T, ws string) {
			writeFile(t, filepath.Join(ws, ".khub", "generated"), "x")
		},
		"entity folder is a file": func(t *testing.T, ws string) {
			mkdirAll(t, filepath.Join(ws, "opportunities"))
			writeFile(t, filepath.Join(ws, "opportunities", "deal"), "x")
		},
	}
	for name, setup := range setups {
		t.Run(name, func(t *testing.T) {
			ws := newWS(t, "firm-ops")
			seed(t, ws, "clients/initech.md", kv("type", "client", "name", "Initech"))
			setup(t, ws)
			add := func(dry bool) error {
				_, err := Create(ws, "opportunity", CreateOpts{
					Fields: fields("name", "Deal", "client", "initech"), UseTemplate: true, DryRun: dry})
				return err
			}
			before := snapshot(t, ws)
			dry := add(true)
			requireSameTree(t, before, snapshot(t, ws))
			real := add(false)
			if dry == nil || real == nil || dry.Error() != real.Error() {
				t.Fatalf("dry = %v, real = %v", dry, real)
			}
		})
	}
}
