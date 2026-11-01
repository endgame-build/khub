package migrate_test

// FuzzMigrateIdempotent runs the per-document rewrite `khub upgrade` applies
// (exported here by export_test.go) in memory, against the schema in
// migrate_test.go. The legacy tests prove the rewrites on shapes somebody
// wrote down; this proves the properties every rewrite must keep on shapes
// nobody did. `go test` runs the seeds; `go test
// -fuzz=FuzzMigrateIdempotent ./internal/migrate` explores.

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/migrate"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/values"
)

// fuzzKinds are the types a fuzzed document is read as, one per legacy shape
// the ontology in migrate_test.go holds.
var fuzzKinds = []struct{ typ, format string }{
	{"note", "md"},  // base status, sources, references, stale_after
	{"adr", "md"},   // a decision enum that holds retired status values
	{"svc", "yaml"}, // a non-md document, its prose in `body`
	{"old", "md"},   // a redeclared status that cannot hold draft
}

// legacyKey matches a document that carries a key the migration rewrites.
var legacyKey = regexp.MustCompile(`(?m)^(draft|sources|status|stale_after):`)

// seedMigrate adds the legacy tests' documents, every corpus file that
// carries a key the migration rewrites, and hand-written comment-heavy and
// flow-style documents.
func seedMigrate(f *testing.F) {
	f.Helper()
	for _, s := range []struct {
		kind uint8
		doc  string
	}{
		{0, "---\ntype: note\ntitle: A   # hand comment\ndraft: true\nsources:\n- other\n- path/to/file.md\n---\nBody.\n"},
		{0, "---\n# header\ntype: note\ndraft: false   # retired\nsources:  # where from\n- other\n- resource: https://x.example\nreferences:\n- other\n- more\n---\n"},
		{1, "---\ntype: adr\nstatus: accepted  # the call\ndraft: true\n---\n"},
		{1, "---\ntype: adr\nstatus: proposed\n---\n"},
		{2, "type: svc\nstatus: active  # live\nbody: prose\n"},
		{2, "type: svc\nstatus: active  # live\ndraft: true\n"},
		{2, "type: svc\nstatus: deprecated\ndraft: true\n"},
		{0, "---\ntype: note\nstatus: stable\ndraft: true\nsources:\n- other\n---\n"},
		{3, "---\ntype: old\ndraft: true\n---\n"},
		{3, "---\ntype: old\nstatus: active\ndraft: false\n---\n"},
		{0, "---\ntype: note\ndraft: true\n----\nBody.\n"},
		{0, "---\ntype: note\nstale_after: 2026-12-31  # review by\n---\n"},
		{0, "---\ntype: note\nstale_after: '2026-12-31'\n---\n"},
		{0, "---\ntype: note\nstale_after: 2026-12-31T09:00:00+02:00\n---\n"},
		{0, "---\n# lead\ntype: note  # t\n# before draft\ndraft: true   # flag\n# before sources\nsources:   # s\n  - a   # first\n  # mid\n  - {resource: https://x.example}  # obj\n# tail\n---\nBody # not a comment\n"},
		{0, "---\n{type: note, draft: true, sources: [a, b], status: active}\n---\n"},
		{0, "---\ntype: note\nsources: [a, {resource: r}, b]  # flow\nreferences: [b, c]\ndraft: false\n---\n"},
		{0, "---\ntype: note\nsources: solo\nstale_after: \"2026-01-02\"\n---\n"},
		{0, "---\ntype: note\nsources: &s [a, b]\nreferences: *s\ndraft: true\n---\n"},
		{1, "---\ntype: adr\nstatus: rejected\ndecision: accepted\n---\n"},
		{2, "type: svc\n# c\nstatus: archived # was\ndraft: true\nsources: [x]\nbody: |\n  prose\n"},
	} {
		f.Add(s.kind, s.doc)
	}
	n := 0
	for _, dir := range []string{"../../parity/corpus/canonical", "../../parity/corpus/annotated"} {
		err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			raw, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			text := string(raw)
			if !legacyKey.MatchString(text) {
				return nil
			}
			switch filepath.Ext(p) {
			case ".md":
				f.Add(uint8(0), text)
				f.Add(uint8(1), text)
			case ".yaml":
				f.Add(uint8(2), text)
				f.Add(uint8(1), "---\n"+text+"---\n")
			default:
				return nil
			}
			n++
			return nil
		})
		if err != nil {
			f.Fatal(err)
		}
	}
	if n < 20 {
		f.Fatalf("suspiciously small corpus: %d files", n)
	}
}

// FuzzMigrateIdempotent: for any document, the migration either refuses it
// and leaves its bytes alone, or rewrites it so that a second run changes
// nothing, every key it does not report removing survives with its value,
// every bare `sources` string ends up in `references`, and the output parses
// wherever the input did.
func FuzzMigrateIdempotent(f *testing.F) {
	seedMigrate(f)
	resolved, err := introspect.LoadSchema(newWS(f))
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, kind uint8, doc string) {
		k := fuzzKinds[int(kind)%len(fuzzKinds)]
		doc = canon.NormalizeNewlines(doc) // as upgrade reads every file
		rtype, ok := resolved.Types.Get(k.typ)
		if !ok {
			t.Fatalf("no type %s in the test schema", k.typ)
		}
		scan, _, err := canon.Parse(doc, k.format)
		if err != nil {
			return // the index reads it as malformed, so upgrade never migrates it
		}
		first, err := migrate.Doc(rtype, doc, k.format, scan)
		if err != nil {
			t.Fatalf("rewrite failed: %v\n%q", err, doc)
		}
		if first.Conflict != nil || len(first.Changes) == 0 {
			if first.Text != "" {
				t.Fatalf("unmigrated document rewritten (%+v):\n--- in\n%q", first, doc)
			}
			return
		}
		out := first.Text

		in, _, inErr := canon.ReadDoc(doc, k.format)
		if inErr != nil {
			t.Fatalf("migrated a document the edit altitude refuses (%v):\n%q", inErr, doc)
		}
		got, _, err := canon.ReadDoc(out, k.format)
		if err != nil {
			t.Fatalf("output does not parse: %v\n--- in\n%q\n--- out\n%q", err, doc, out)
		}
		outScan, _, err := canon.Parse(out, k.format)
		if err != nil {
			t.Fatalf("output does not scan: %v\n--- in\n%q\n--- out\n%q", err, doc, out)
		}
		keepsKeys(t, in, got, first.Changes, doc, out)
		movesBareSources(t, in, got, first.Changes, doc, out)

		second, err := migrate.Doc(rtype, out, k.format, outScan)
		if err != nil || second.Conflict != nil || len(second.Changes) != 0 {
			t.Fatalf("second run is not a no-op (err %v, %+v):\n--- in\n%q\n--- first\n%q", err, second, doc, out)
		}
	})
}

// keepsKeys requires every input key to survive into got unless a change
// removes it, and every key no change names to keep its value.
func keepsKeys(t *testing.T, in, got *omap.Map, changes []migrate.Change, doc, out string) {
	t.Helper()
	named := map[string]bool{}
	removed := map[string]bool{}
	for _, c := range changes {
		named[c.Field] = true
		switch c.Op {
		case migrate.OpRenameKey:
			named[c.To.(string)] = true
			removed[c.Field] = true
		case migrate.OpDeleteKey:
			removed[c.Field] = true
		case migrate.OpMoveEntries:
			named["references"] = true
			removed[c.Field] = true
		}
	}
	for _, key := range in.Keys() {
		before, _ := in.Get(key)
		after, has := got.Get(key)
		switch {
		case !has && !removed[key]:
			t.Fatalf("key %q lost, changes %+v:\n--- in\n%q\n--- out\n%q", key, changes, doc, out)
		case has && !named[key] && encode(t, before) != encode(t, after):
			t.Fatalf("key %q moved from %s to %s, changes %+v:\n--- in\n%q\n--- out\n%q",
				key, encode(t, before), encode(t, after), changes, doc, out)
		}
	}
}

// movesBareSources requires every bare `sources` string to land in
// `references` once a move is reported, and to stay somewhere otherwise.
func movesBareSources(t *testing.T, in, got *omap.Map, changes []migrate.Change, doc, out string) {
	t.Helper()
	src, _ := in.Get("sources")
	bare, _ := migrate.SplitSources(src)
	moved := slices.ContainsFunc(changes, func(c migrate.Change) bool { return c.Op == migrate.OpMoveEntries })
	outSrc, _ := got.Get("sources")
	outRefs, _ := got.Get("references")
	for _, b := range bare {
		inRefs := slices.Contains(values.Items(outRefs), b)
		if (moved && !inRefs) || (!inRefs && !slices.Contains(values.Items(outSrc), b)) {
			t.Fatalf("bare source %q lost, changes %+v:\n--- in\n%q\n--- out\n%q", b, changes, doc, out)
		}
	}
	if left, _ := migrate.SplitSources(outSrc); moved && len(left) > 0 {
		t.Fatalf("bare sources %q left behind:\n--- in\n%q\n--- out\n%q", left, doc, out)
	}
}

func encode(t *testing.T, v any) string {
	t.Helper()
	s, err := canon.EncodeCLI(v)
	if err != nil {
		t.Fatalf("encode %#v: %v", v, err)
	}
	return s
}
