package canon

// Unit tests for the two structural splice kinds: deleting a pair and renaming
// a key. Neither has a ruamel counterpart to record, so each want is the source
// with exactly the bytes the ownership rule assigns to the change removed or
// replaced; every other byte must survive.

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/omap"
)

func spliceOps(t *testing.T, src string, want *omap.Map, renames ...Rename) string {
	t.Helper()
	out, err := SpliceMapping([]byte(src), want, Mode12, renames...)
	if err != nil {
		t.Fatalf("SpliceMapping: %v", err)
	}
	// A splice reaches a fixed point: the same want against its own output
	// changes nothing.
	again, err := SpliceMapping(out, want, Mode12)
	if err != nil || string(again) != string(out) {
		t.Fatalf("second splice moved bytes (%v)\n first: %q\nsecond: %q", err, out, again)
	}
	return string(out)
}

func requireNoSpliceOps(t *testing.T, src string, want *omap.Map, renames ...Rename) {
	t.Helper()
	out, err := SpliceMapping([]byte(src), want, Mode12, renames...)
	if !errors.Is(err, ErrNoSplice) {
		t.Fatalf("want ErrNoSplice, got (%q, %v)", out, err)
	}
}

func rename(to string, path ...string) Rename { return Rename{Path: path, To: to} }

// --- delete ------------------------------------------------------------------

func TestSpliceDeletesAPair(t *testing.T) {
	for _, tc := range []struct {
		name, src, want string
		keep            *omap.Map
	}{
		{"scalar", "type: adr\ndraft: true\ntitle: X\n", "type: adr\ntitle: X\n",
			m("type", "adr", "title", "X")},
		{"flow map", "a: 1\nmeta: {x: 1, y: 2}\nb: 2\n", "a: 1\nb: 2\n",
			m("a", int64(1), "b", int64(2))},
		{"block map", "a: 1\nmeta:\n  x: 1\n  # inside the block\n  y: 2\nb: 2\n", "a: 1\nb: 2\n",
			m("a", int64(1), "b", int64(2))},
		{"block list at dash offset 0", "a: 1\ntags:\n- x\n- y   # on the last item\nb: 2\n", "a: 1\nb: 2\n",
			m("a", int64(1), "b", int64(2))},
		{"indented block list", "a: 1\ntags:\n  - x\n  - y\nb: 2\n", "a: 1\nb: 2\n",
			m("a", int64(1), "b", int64(2))},
		{"literal block", "a: 1\ntext: |\n  line one\n\n  line two\nb: 2\n", "a: 1\nb: 2\n",
			m("a", int64(1), "b", int64(2))},
		{"folded plain scalar", "a: 1\ndesc: the quick brown\n  fox jumps\nb: 2\n", "a: 1\nb: 2\n",
			m("a", int64(1), "b", int64(2))},
		{"first key", "# header\na: 1\nb: 2\n", "# header\nb: 2\n",
			m("b", int64(2))},
		{"last key", "a: 1\nb: 2\n", "a: 1\n",
			m("a", int64(1))},
		{"last key before a trailing comment", "a: 1\nb: 2\n# trailing note\n", "a: 1\n# trailing note\n",
			m("a", int64(1))},
		{"last key, no final newline", "a: 1\nb: 2", "a: 1\n",
			m("a", int64(1))},
		{"key with a trailing comment", "a: 1\ndraft: true  # publish flag\nb: 2\n", "a: 1\nb: 2\n",
			m("a", int64(1), "b", int64(2))},
		{"key preceded by a comment", "a: 1\n# about draft\ndraft: true\nb: 2\n", "a: 1\n# about draft\nb: 2\n",
			m("a", int64(1), "b", int64(2))},
		{"comment after a block value", "a: 1\ntags:\n- x\n# about b\nb: 2\n", "a: 1\n# about b\nb: 2\n",
			m("a", int64(1), "b", int64(2))},
		{"two keys", "a: 1\nx: 0\nb: 2\ny: 0  # y\nc: 3\n", "a: 1\nb: 2\nc: 3\n",
			m("a", int64(1), "b", int64(2), "c", int64(3))},
		{"nested key", "alpha:\n  type: repo\n  draft: false  # d\n  status: active\n",
			"alpha:\n  type: repo\n  status: active\n",
			m("alpha", m("type", "repo", "status", "active"))},
		{"crlf", "# h\r\na: 1\r\ndraft: true\r\nb: 2\r\n", "# h\r\na: 1\r\nb: 2\r\n",
			m("a", int64(1), "b", int64(2))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := spliceOps(t, tc.src, tc.keep); got != tc.want {
				t.Fatalf("\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}

// A pair inside a flow mapping shares its line with its siblings.
func TestSpliceRefusesADeleteItCannotCut(t *testing.T) {
	requireNoSpliceOps(t, "meta: {x: 1, y: 2}\n", m("meta", m("x", int64(1))))
	// An emptied document re-emits as `{}`, the bytes ruamel writes.
	requireNoSpliceOps(t, "# h\na: 1\n", omap.New())
}

// Deleting one key and moving another is still a reorder.
func TestSpliceRefusesADeleteThatReorders(t *testing.T) {
	requireNoSpliceOps(t, "a: 1\nx: 0\nb: 2\nc: 3\n", m("a", int64(1), "c", int64(3), "b", int64(2)))
}

// --- rename ------------------------------------------------------------------

func TestSpliceRenamesAKey(t *testing.T) {
	for _, tc := range []struct {
		name, src, want string
		keep            *omap.Map
		r               Rename
	}{
		{"plain", "type: adr\nstatus: accepted  # decided\ntitle: X\n",
			"type: adr\ndecision: accepted  # decided\ntitle: X\n",
			m("type", "adr", "decision", "accepted", "title", "X"), rename("decision", "status")},
		{"quoted key", "type: adr\n\"status\": accepted\n",
			"type: adr\ndecision: accepted\n",
			m("type", "adr", "decision", "accepted"), rename("decision", "status")},
		{"to a key the emitter quotes", "status: x\n",
			"'true': x\n",
			m("true", "x"), rename("true", "status")},
		{"flow value", "meta: {a: 1}  # c\nb: 2\n",
			"info: {a: 1}  # c\nb: 2\n",
			m("info", m("a", int64(1)), "b", int64(2)), rename("info", "meta")},
		{"block value", "status:\n- a\n- b   # last\nnext: 1\n",
			"decision:\n- a\n- b   # last\nnext: 1\n",
			m("decision", []any{"a", "b"}, "next", int64(1)), rename("decision", "status")},
		{"nested key", "alpha:\n  type: adr\n  status: accepted\n",
			"alpha:\n  type: adr\n  decision: accepted\n",
			m("alpha", m("type", "adr", "decision", "accepted")), rename("decision", "alpha", "status")},
		{"renamed and changed", "status: accepted  # c\n",
			"decision: rejected  # c\n",
			m("decision", "rejected"), rename("decision", "status")},
		{"to itself", "status: x  # c\n",
			"status: x  # c\n",
			m("status", "x"), rename("status", "status")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := spliceOps(t, tc.src, tc.keep, tc.r); got != tc.want {
				t.Fatalf("\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}

func TestSpliceRefusesARenameItCannotPlace(t *testing.T) {
	// The old key is absent.
	requireNoSpliceOps(t, "a: 1\n", m("b", int64(1)), rename("b", "missing"))
	// The new name is already taken.
	requireNoSpliceOps(t, "a: 1\nb: 2\n", m("b", int64(1)), rename("b", "a"))
	// want moved the renamed key: a rename keeps the key where it stood.
	requireNoSpliceOps(t, "a: 1\nb: 2\n", m("b", int64(2), "z", int64(1)), rename("z", "a"))
	// A key the emitter writes in the `? key` form.
	requireNoSpliceOps(t, "a: 1\n", m("x\ny", int64(1)), rename("x\ny", "a"))
	// No path at all.
	requireNoSpliceOps(t, "a: 1\n", m("a", int64(1)), Rename{To: "b"})
}

// --- combined ----------------------------------------------------------------

// The migration's shape: rename status→decision, delete draft, change a
// scalar, append the new status, all in one splice that keeps every comment.
func TestSpliceCombinesRenameDeleteEditAndAppend(t *testing.T) {
	src := "# ADR header\ntype: adr\nstatus: accepted   # the decision\n" +
		"draft: true  # publish flag\ntitle: Old title\ntags:\n- a\n# trailing note\n"
	want := "# ADR header\ntype: adr\ndecision: accepted   # the decision\n" +
		"title: New title\ntags:\n- a\n# trailing note\nstatus: draft\n"
	got := spliceOps(t, src,
		m("type", "adr", "decision", "accepted", "title", "New title", "tags", []any{"a"}, "status", "draft"),
		rename("decision", "status"))
	if got != want {
		t.Fatalf("\n got: %q\nwant: %q", got, want)
	}
}

// A block emptied of its last pair is rewritten as `{}`, the way the emitter
// writes an empty mapping, rather than read back as null.
func TestSpliceEmptiesABlockToFlow(t *testing.T) {
	if got, want := spliceOps(t, "meta:\n  x: 1\nb: 2\n", m("meta", omap.New(), "b", int64(2))),
		"meta: {}\nb: 2\n"; got != want {
		t.Fatalf("\n got: %q\nwant: %q", got, want)
	}
}

// A rename may claim a name an earlier rename in the same splice freed.
func TestSpliceChainsRenamesThroughAFreedName(t *testing.T) {
	src := "type: adr\nstatus: accepted  # the call\ndraft: true  # flag\n"
	want := "type: adr\ndecision: accepted  # the call\nstatus: draft # flag\n"
	got := spliceOps(t, src, m("type", "adr", "decision", "accepted", "status", "draft"),
		rename("decision", "status"), rename("status", "draft"))
	if got != want {
		t.Fatalf("\n got: %q\nwant: %q", got, want)
	}
	// The other order claims a name still held.
	requireNoSpliceOps(t, src, m("type", "adr", "decision", "accepted", "status", "draft"),
		rename("status", "draft"), rename("decision", "status"))
}

func TestSpliceDocCombinesOpsInFrontmatter(t *testing.T) {
	old := "---\ntype: adr\nstatus: rejected  # why\ndraft: false\n---\n\nBody.\n"
	want := "---\ntype: adr\ndecision: rejected  # why\nstatus: stable\n---\n\nBody.\n"
	got, err := SpliceDoc(old, m("type", "adr", "decision", "rejected", "status", "stable"),
		"\nBody.\n", "md", Mode12, rename("decision", "status"))
	if err != nil {
		t.Fatalf("SpliceDoc: %v", err)
	}
	if got != want {
		t.Fatalf("\n got: %q\nwant: %q", got, want)
	}
}

func TestSpliceCombinesOpsUnderCRLF(t *testing.T) {
	src := "# h\r\na: 1\r\ndraft: true\r\nstatus: x  # c\r\n"
	want := "# h\r\na: 1\r\ndecision: x  # c\r\n"
	if got := spliceOps(t, src, m("a", int64(1), "decision", "x"), rename("decision", "status")); got != want {
		t.Fatalf("\n got: %q\nwant: %q", got, want)
	}
}

// --- collections -------------------------------------------------------------

// A yaml collection: each row renames and drops a field, and the header, row
// and inter-row comments all survive.
func TestSpliceCollectionRenamesAndDeletesRowFields(t *testing.T) {
	src := "# repos.yaml\n" +
		"alpha:\n  type: adr\n  status: accepted   # alpha's call\n  draft: true\n" +
		"# between rows\n" +
		"beta:\n  type: adr\n  draft: false  # published\n  status: rejected\n" +
		"# trailing note\n"
	want := "# repos.yaml\n" +
		"alpha:\n  type: adr\n  decision: accepted   # alpha's call\n" +
		"# between rows\n" +
		"beta:\n  type: adr\n  decision: rejected\n" +
		"# trailing note\n"
	rows := m(
		"alpha", m("type", "adr", "decision", "accepted"),
		"beta", m("type", "adr", "decision", "rejected"))
	got, err := SpliceCollection(src, rows, "yaml",
		rename("decision", "alpha", "status"), rename("decision", "beta", "status"))
	if err != nil {
		t.Fatalf("SpliceCollection: %v", err)
	}
	if got != want {
		t.Fatalf("\n got: %q\nwant: %q", got, want)
	}
}

// json and jsonl carry no comments, so they keep re-emitting; the re-emit puts
// a renamed field where rows hold it, which is where the old field stood.
func TestSpliceCollectionLeavesJSONToTheReEmit(t *testing.T) {
	rows := m("alpha", m("type", "adr", "decision", "accepted", "title", "A"))
	for _, f := range []string{"json", "jsonl"} {
		old, err := DumpCollection(m("alpha", m("type", "adr", "status", "accepted", "title", "A")), f)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := SpliceCollection(old, rows, f, rename("decision", "alpha", "status")); !errors.Is(err, ErrNoSplice) {
			t.Fatalf("%s: want ErrNoSplice, got %v", f, err)
		}
		out, err := DumpCollection(rows, f)
		if err != nil {
			t.Fatal(err)
		}
		back, err := LoadCollection(out, f)
		if err != nil {
			t.Fatal(err)
		}
		row, _ := back.Get("alpha")
		if keys := row.(*omap.Map).Keys(); len(keys) != 3 || keys[1] != "decision" {
			t.Fatalf("%s: renamed field moved: %v", f, keys)
		}
	}
}

// The corpus file built for these splices, through the whole migration shape:
// every line the ops do not own survives byte for byte.
func TestSpliceOpsOverTheCorpusFile(t *testing.T) {
	raw, err := os.ReadFile("../../parity/corpus/annotated/ops-shapes.yaml")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	have, _, err := LoadDocMap(src, Mode12)
	if err != nil {
		t.Fatal(err)
	}
	want := omap.New()
	for _, k := range have.Keys() {
		v, _ := have.Get(k)
		switch k {
		case "status":
			want.Set("decision", v)
		case "draft":
		case "rows":
			rows := v.(*omap.Map)
			alpha, _ := rows.Get("alpha")
			alpha.(*omap.Map).Delete("draft")
			want.Set(k, rows)
		case "updated":
			want.Set(k, Date{ISO: "2026-02-02"})
		default:
			want.Set(k, v)
		}
	}
	want.Set("status", "draft")

	wantText := strings.NewReplacer(
		"status: accepted   # renamed", "decision: accepted   # renamed",
		"draft: true  # trailing comment on the deleted pair\n", "",
		"    draft: false   # nested pair\n", "",
		"updated: 2026-01-15", "updated: 2026-02-02",
	).Replace(src) + "status: draft\n"
	if got := spliceOps(t, src, want, rename("decision", "status")); got != wantText {
		t.Fatalf("\n got: %q\nwant: %q", got, wantText)
	}
}

// --- nested append -----------------------------------------------------------

// A new key in a nested block lands right after the lines the block's last
// pair owns, at the block's indent: before a standalone comment that follows
// (it belongs to whatever comes next), after the last pair's own comment.
func TestSpliceAppendsANestedKey(t *testing.T) {
	d := Date{ISO: "2026-08-15"}
	for _, tc := range []struct {
		name, src, want string
		keep            *omap.Map
	}{
		{"row gains a key",
			"# header\nalpha:\n  type: repo\n  status: active   # watch\nbeta:\n  type: repo\n",
			"# header\nalpha:\n  type: repo\n  status: active   # watch\n  updated: 2026-08-15\nbeta:\n  type: repo\n",
			m("alpha", m("type", "repo", "status", "active", "updated", d), "beta", m("type", "repo"))},
		{"row followed by a comment",
			"alpha:\n  type: repo\n# between rows\nbeta:\n  type: repo\n",
			"alpha:\n  type: repo\n  updated: 2026-08-15\n# between rows\nbeta:\n  type: repo\n",
			m("alpha", m("type", "repo", "updated", d), "beta", m("type", "repo"))},
		{"last row before a trailing comment",
			"alpha:\n  type: repo\nbeta:\n  type: repo\n# trailing note\n",
			"alpha:\n  type: repo\nbeta:\n  type: repo\n  updated: 2026-08-15\n# trailing note\n",
			m("alpha", m("type", "repo"), "beta", m("type", "repo", "updated", d))},
		{"last row, no final newline",
			"alpha:\n  type: repo",
			"alpha:\n  type: repo\n  updated: 2026-08-15\n",
			m("alpha", m("type", "repo", "updated", d))},
		{"block whose last pair is a list",
			"alpha:\n  tags:\n  - a\n  - b   # last\nz: 1\n",
			"alpha:\n  tags:\n  - a\n  - b   # last\n  type: repo\nz: 1\n",
			m("alpha", m("tags", []any{"a", "b"}, "type", "repo"), "z", int64(1))},
		{"two levels down",
			"type: note\ngenerated:\n  by: khub  # writer\n",
			"type: note\ngenerated:\n  by: khub  # writer\n  at: 2026-08-15\n",
			m("type", "note", "generated", m("by", "khub", "at", d))},
		{"a nested mapping value",
			"alpha:\n  type: repo\n",
			"alpha:\n  type: repo\n  generated:\n    by: khub\n    at: x\n",
			m("alpha", m("type", "repo", "generated", m("by", "khub", "at", "x")))},
		{"two rows and a root key at once",
			"# h\nalpha:\n  type: repo\nbeta:\n  type: repo\n",
			"# h\nalpha:\n  type: repo\n  updated: 2026-08-15\nbeta:\n  type: repo\n  updated: 2026-08-15\nz: 1\n",
			m("alpha", m("type", "repo", "updated", d), "beta", m("type", "repo", "updated", d), "z", int64(1))},
		{"crlf",
			"alpha:\r\n  type: repo\r\nbeta:\r\n  type: repo\r\n",
			"alpha:\r\n  type: repo\r\n  updated: 2026-08-15\r\nbeta:\r\n  type: repo\r\n",
			m("alpha", m("type", "repo", "updated", d), "beta", m("type", "repo"))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := spliceOps(t, tc.src, tc.keep); got != tc.want {
				t.Fatalf("\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}

// The Phase 3 write shape on a yaml collection row: edit a field, delete one,
// and stamp a `generated` block the row did not have, comments intact.
func TestSpliceCollectionRowGainsAStampedBlock(t *testing.T) {
	src := "# vendors\nacme:\n  type: vendor\n  title: Acme   # inline\n  body: prose\n# between rows\nglobex:\n  type: vendor\n"
	want := "# vendors\nacme:\n  type: vendor\n  title: Acme Co # inline\n" +
		"  generated:\n    by: khub\n    at: 2026-08-15T00:00:00Z\n# between rows\nglobex:\n  type: vendor\n"
	rows := m(
		"acme", m("type", "vendor", "title", "Acme Co", "generated", m("by", "khub", "at", DateTime{ISO: "2026-08-15T00:00:00Z"})),
		"globex", m("type", "vendor"))
	got, err := SpliceCollection(src, rows, "yaml")
	if err != nil {
		t.Fatalf("SpliceCollection: %v", err)
	}
	if got != want {
		t.Fatalf("\n got: %q\nwant: %q", got, want)
	}
}

// A scalar inside a one-line flow mapping splices in place.
func TestSpliceEditsAScalarInAFlowMapping(t *testing.T) {
	src := "type: note\ngenerated: { by: x, at: y }  # stamp\n"
	want := "type: note\ngenerated: { by: x, at: z }  # stamp\n"
	if got := spliceOps(t, src, m("type", "note", "generated", m("by", "x", "at", "z"))); got != want {
		t.Fatalf("\n got: %q\nwant: %q", got, want)
	}
}

// A new key in a block emptied to `{}` or a scalar has no last pair to follow.
func TestSpliceRefusesANestedAppendWithNoBlock(t *testing.T) {
	requireNoSpliceOps(t, "alpha: {}\n", m("alpha", m("type", "repo")))
	requireNoSpliceOps(t, "alpha:\n  type: repo\n", m("alpha", m("updated", "x", "type", "repo")))
}

// --- list append -------------------------------------------------------------

// grownList is the document at src with items appended to the list at path.
func grownList(t *testing.T, src string, path []string, items ...any) *omap.Map {
	t.Helper()
	have, _, err := LoadDocMap(src, Mode12)
	if err != nil {
		t.Fatal(err)
	}
	m := have
	for _, k := range path[:len(path)-1] {
		v, _ := m.Get(k)
		m = v.(*omap.Map)
	}
	v, _ := m.Get(path[len(path)-1])
	m.Set(path[len(path)-1], append(v.([]any), items...))
	return have
}

// A new item in a block sequence lands after the lines the list's key owns,
// at the sequence's dash indent; every existing item keeps its style and its
// comments.
func TestSpliceAppendsToABlockSequence(t *testing.T) {
	stamp := m("by", "human:b", "at", DateTime{ISO: "2026-02-01T00:00:00Z"}, "rev", "r2")
	for _, tc := range []struct {
		name, src, want string
		path            []string
		item            any
	}{
		{"verify again",
			"title: Cm  # title note\nverified:\n- {by: human:a, at: 2026-01-01T00:00:00Z, rev: r1}  # first\n",
			"title: Cm  # title note\nverified:\n- {by: human:a, at: 2026-01-01T00:00:00Z, rev: r1}  # first\n" +
				"- by: human:b\n  at: 2026-02-01T00:00:00Z\n  rev: r2\n",
			[]string{"verified"}, stamp},
		{"indented sequence before a sibling and a comment",
			"tags:\n  - a\n  - b   # last\n# about z\nz: 1\n",
			"tags:\n  - a\n  - b   # last\n  - c\n# about z\nz: 1\n",
			[]string{"tags"}, "c"},
		{"inside a collection row",
			"# rows\nacme:\n  tags:\n  - a\n# between\nglobex:\n  tags: []\n",
			"# rows\nacme:\n  tags:\n  - a\n  - b\n# between\nglobex:\n  tags: []\n",
			[]string{"acme", "tags"}, "b"},
		{"no final newline",
			"tags:\n- a",
			"tags:\n- a\n- b\n",
			[]string{"tags"}, "b"},
		{"crlf",
			"tags:\r\n- a\r\nz: 1\r\n",
			"tags:\r\n- a\r\n- b\r\nz: 1\r\n",
			[]string{"tags"}, "b"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := spliceOps(t, tc.src, grownList(t, tc.src, tc.path, tc.item)); got != tc.want {
				t.Fatalf("\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}
