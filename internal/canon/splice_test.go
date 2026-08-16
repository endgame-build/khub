package canon

// Unit tests for the comment-preserving write path. Every "want" string here
// is the literal output Python khub 0.18 (ruamel round-trip) wrote for the same
// document and the same edit, recorded through parity/tools/pykhub.sh.

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/omap"
)

// m builds an ordered map from alternating key/value pairs.
func m(pairs ...any) *omap.Map {
	out := omap.New()
	for i := 0; i+1 < len(pairs); i += 2 {
		out.Set(pairs[i].(string), pairs[i+1])
	}
	return out
}

func splice(t *testing.T, src string, want *omap.Map) string {
	t.Helper()
	out, err := SpliceMapping([]byte(src), want, Mode12)
	if err != nil {
		t.Fatalf("SpliceMapping: %v", err)
	}
	return string(out)
}

func requireNoSplice(t *testing.T, src string, want *omap.Map) {
	t.Helper()
	out, err := SpliceMapping([]byte(src), want, Mode12)
	if !errors.Is(err, ErrNoSplice) {
		t.Fatalf("want ErrNoSplice, got (%q, %v)", out, err)
	}
}

// --- the shapes a splice expresses -------------------------------------------

// A comment on the edited key's OWN line is re-seated at the column ruamel
// recorded for it: pad back to that column, never fewer than one space.
func TestSpliceKeepsCommentOnTheEditedLine(t *testing.T) {
	src := "type: opportunity\nstage: prospect  # current pipeline stage\nowner: noor\n"
	want := "type: opportunity\nstage: won       # current pipeline stage\nowner: noor\n"
	got := splice(t, src, m("type", "opportunity", "stage", "won", "owner", "noor"))
	if got != want {
		t.Fatalf("\n got: %q\nwant: %q", got, want)
	}
}

// A value that overruns the comment's recorded column falls back to one space.
func TestSpliceCommentFallsBackToOneSpace(t *testing.T) {
	src := "stage: prospect # one space\nowner: noor\n"
	want := "stage: prospecting # one space\nowner: noor\n"
	got := splice(t, src, m("stage", "prospecting", "owner", "noor"))
	if got != want {
		t.Fatalf("\n got: %q\nwant: %q", got, want)
	}
}

// The run of spaces after the colon collapses to one, exactly as ruamel
// re-emits a changed pair, while the comment keeps its column.
func TestSpliceCollapsesTheGapAfterTheColon(t *testing.T) {
	src := "name: Gap Co\nstage:     prospect  # gapped\ntier: a\n"
	want := "name: Gap Co\nstage: won           # gapped\ntier: a\n"
	got := splice(t, src, m("name", "Gap Co", "stage", "won", "tier", "a"))
	if got != want {
		t.Fatalf("\n got: %q\nwant: %q", got, want)
	}
}

// A comment on an untouched SIBLING line survives byte-for-byte.
func TestSpliceKeepsCommentOnAnUntouchedLine(t *testing.T) {
	src := "type: client\nname: Comment Co   # keep this comment\ndescription: before\n"
	want := "type: client\nname: Comment Co   # keep this comment\ndescription: after\n"
	got := splice(t, src, m("type", "client", "name", "Comment Co", "description", "after"))
	if got != want {
		t.Fatalf("\n got: %q\nwant: %q", got, want)
	}
}

// A document header comment and a comment between two keys both survive.
func TestSpliceKeepsHeaderAndInterKeyComments(t *testing.T) {
	src := "# document header comment\ntype: client\n" +
		"# a comment between keys\nstage: prospect\n# trailing note\n"
	want := "# document header comment\ntype: client\n" +
		"# a comment between keys\nstage: won\n# trailing note\n"
	got := splice(t, src, m("type", "client", "stage", "won"))
	if got != want {
		t.Fatalf("\n got: %q\nwant: %q", got, want)
	}
}

// A new TOP-LEVEL key appends after the last line — where ruamel puts it too,
// after any trailing comment.
func TestSpliceAppendsANewTopLevelKey(t *testing.T) {
	src := "type: client\nname: Grow Co   # keep this comment\n# trailing note\n"
	want := "type: client\nname: Grow Co   # keep this comment\n# trailing note\ndescription: brand new\n"
	got := splice(t, src, m("type", "client", "name", "Grow Co", "description", "brand new"))
	if got != want {
		t.Fatalf("\n got: %q\nwant: %q", got, want)
	}
}

// Untouched blocks, sequences and quoting styles pass through untouched — the
// splice only rewrites the values that moved.
func TestSpliceLeavesUnrelatedStructureAlone(t *testing.T) {
	src := "type: client\n" +
		"tags:\n  # inside a sequence\n  - alpha\n  - beta   # trailing on a seq item\n" +
		"notes: 'single quoted'\n" +
		"nested:\n  a: 1\n  b: 2\n" +
		"stage: prospect\n"
	want := strings.Replace(src, "stage: prospect", "stage: won", 1)
	got := splice(t, src, m(
		"type", "client",
		"tags", []any{"alpha", "beta"},
		"notes", "single quoted",
		"nested", m("a", int64(1), "b", int64(2)),
		"stage", "won"))
	if got != want {
		t.Fatalf("\n got: %q\nwant: %q", got, want)
	}
}

// A scalar nested inside a block mapping is addressable by path.
func TestSpliceEditsANestedScalar(t *testing.T) {
	src := "# header\nalpha:\n  type: repo\n  status: active   # watch this\n"
	want := "# header\nalpha:\n  type: repo\n  status: archived # watch this\n"
	got := splice(t, src, m("alpha", m("type", "repo", "status", "archived")))
	if got != want {
		t.Fatalf("\n got: %q\nwant: %q", got, want)
	}
}

// The yaml-collection promise of docs/collections-design.md: a header comment,
// per-row inline comments, an inter-row comment and a trailing note all survive
// an edit to one row's field.
func TestSpliceCollectionKeepsHeaderAndRowComments(t *testing.T) {
	src := "# repos.yaml — hand-maintained inventory\n" +
		"# second header line\n" +
		"alpha:\n  type: repo\n  repo: acme/alpha   # the main service\n  status: active\n" +
		"# between rows\n" +
		"beta:\n  type: repo\n  repo: acme/beta\n  status: active   # keep an eye on this\n" +
		"# trailing note\n"
	want := strings.Replace(src, "  status: active\n", "  status: archived\n", 1)

	rows := m(
		"alpha", m("type", "repo", "repo", "acme/alpha", "status", "archived"),
		"beta", m("type", "repo", "repo", "acme/beta", "status", "active"))
	got, err := SpliceCollection(src, rows, "yaml")
	if err != nil {
		t.Fatalf("SpliceCollection: %v", err)
	}
	if got != want {
		t.Fatalf("\n got: %q\nwant: %q", got, want)
	}
}

// --- the shapes it refuses ---------------------------------------------------

// Policy: a comment-free document takes the byte-pinned emitter, not the
// splice. Nothing to protect, and splicing would freeze hand-authored
// formatting that ruamel normalizes.
// A comment-free document is spliced too. It used to fall back to the emitter,
// which re-emitted every line and silently rewrote UNTOUCHED non-canonical
// scalars — `017` became `17`, `.inf` became `+.Inf` — on any edit. ruamel
// round-trips those verbatim, so re-emitting was the divergence, not the
// splice. See D13 for the cosmetic difference splicing keeps instead.
func TestSpliceHandlesACommentFreeDocument(t *testing.T) {
	got, err := SpliceMapping([]byte("type: client\nstage: prospect\n"),
		m("type", "client", "stage", "won"), Mode12)
	if err != nil {
		t.Fatalf("splice: %v", err)
	}
	if string(got) != "type: client\nstage: won\n" {
		t.Errorf("got %q", got)
	}
}

// The value that exposed it: editing one field must not rewrite another.
func TestSplicePreservesUntouchedNonCanonicalScalars(t *testing.T) {
	src := "type: client\nd: 017\ng: .inf\nstage: prospect\n"
	got, err := SpliceMapping([]byte(src),
		m("type", "client", "d", int64(17), "g", math.Inf(1), "stage", "won"), Mode12)
	if err != nil {
		t.Fatalf("splice: %v", err)
	}
	if want := "type: client\nd: 017\ng: .inf\nstage: won\n"; string(got) != want {
		t.Errorf("untouched scalars rewritten\n got %q\nwant %q", got, want)
	}
}

// A deleted key cannot be spliced.
func TestSpliceRefusesADeletedKey(t *testing.T) {
	requireNoSplice(t, "type: client  # keep\nbody: prose\nstage: prospect\n",
		m("type", "client", "stage", "prospect"))
}

// A reordered mapping cannot be spliced.
func TestSpliceRefusesAReorderedMapping(t *testing.T) {
	requireNoSplice(t, "type: client  # keep\nstage: prospect\n",
		m("stage", "prospect", "type", "client"))
}

// A new key BELOW the root cannot be placed: ruamel puts it after whatever
// comment is attached to the block's last pair, which a splice cannot know.
func TestSpliceRefusesANewNestedKey(t *testing.T) {
	requireNoSplice(t, "# header\nalpha:\n  type: repo\n  status: active   # watch\n",
		m("alpha", m("type", "repo", "status", "active", "updated", Date{ISO: "2026-08-15"})))
}

// A value that is a block, not a same-line scalar, cannot be spliced.
func TestSpliceRefusesABlockValue(t *testing.T) {
	requireNoSplice(t, "# header\ntags:\n  - alpha\n  - beta\n",
		m("tags", []any{"alpha", "gamma"}))
	requireNoSplice(t, "# header\nnested:\n  a: 1\n",
		m("nested", "flattened"))
	requireNoSplice(t, "# header\ntext: |\n  block literal\n",
		m("text", "replaced"))
}

// An implicit null (`key:` with nothing after it) is not a same-line scalar:
// the splice must not overwrite the colon token.
func TestSpliceRefusesAnImplicitNull(t *testing.T) {
	requireNoSplice(t, "# header\nempty:\nstage: prospect\n",
		m("empty", "filled", "stage", "prospect"))
}

// A malformed document never reaches the splice.
func TestSpliceRefusesMalformedYAML(t *testing.T) {
	requireNoSplice(t, "# header\na: 1\n a: [\n", m("a", int64(2)))
}

// --- the document wrappers ---------------------------------------------------

func TestSpliceDocMD(t *testing.T) {
	old := "---\ntype: client\nname: Comment Co   # keep\nstage: prospect\n---\n\nBody prose.\n"
	want := "---\ntype: client\nname: Comment Co   # keep\nstage: won\n---\n\nBody prose.\n"
	got, err := SpliceDoc(old, m("type", "client", "name", "Comment Co", "stage", "won"),
		"\nBody prose.\n", "md", Mode12)
	if err != nil {
		t.Fatalf("SpliceDoc: %v", err)
	}
	if got != want {
		t.Fatalf("\n got: %q\nwant: %q", got, want)
	}
}

func TestSpliceDocYAMLCarriesTheBodyKey(t *testing.T) {
	old := "title: Contract  # keep\nkind: api\nbody: prose\n"
	want := "title: Contract  # keep\nkind: events\nbody: prose\n"
	got, err := SpliceDoc(old, m("title", "Contract", "kind", "events"), "prose", "yaml", Mode12)
	if err != nil {
		t.Fatalf("SpliceDoc: %v", err)
	}
	if got != want {
		t.Fatalf("\n got: %q\nwant: %q", got, want)
	}
}

// json admits no comments, so it never splices.
func TestSpliceDocRefusesJSON(t *testing.T) {
	if _, err := SpliceDoc("{\"a\": 1}\n", m("a", int64(2)), "", "json", Mode12); !errors.Is(err, ErrNoSplice) {
		t.Fatalf("want ErrNoSplice, got %v", err)
	}
	if _, err := SpliceCollection("{}\n", omap.New(), "jsonl"); !errors.Is(err, ErrNoSplice) {
		t.Fatalf("want ErrNoSplice, got %v", err)
	}
}

// A no-op edit returns the source byte-for-byte.
func TestSpliceNoChangeIsByteIdentical(t *testing.T) {
	src := "# header\ntype: client\nstage: prospect  # comment\n"
	got := splice(t, src, m("type", "client", "stage", "prospect"))
	if got != src {
		t.Fatalf("\n got: %q\nwant: %q", got, src)
	}
}

// Every scalar renders through the same emitter a whole-document re-emit uses,
// so a spliced value quotes and folds identically — and the comment holds its
// column (`#` at 0-based 12 in the seed) until the value overruns it.
// `yes` stays plain: ruamel resolves YAML 1.2, where yes is a string.
func TestSpliceRendersScalarsThroughTheEmitter(t *testing.T) {
	for _, tc := range []struct {
		name string
		v    any
		want string
	}{
		{"bool", false, "flag: false # c\n"},
		{"int", int64(7), "flag: 7     # c\n"},
		{"float", 1.5, "flag: 1.5   # c\n"},
		{"date", Date{ISO: "2026-08-15"}, "flag: 2026-08-15 # c\n"},
		{"null", nil, "flag: null  # c\n"},
		{"quoted", "yes", "flag: yes   # c\n"},
		{"double", "'quoted'", "flag: \"'quoted'\" # c\n"},
		{"empty", "", "flag: ''    # c\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := splice(t, "flag: seed  # c\n", m("flag", tc.v))
			if got != tc.want {
				t.Fatalf("\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}

// A replacement long enough to fold renders through the emitter from the same
// column ruamel starts at, so the fold lands on the same byte — with and
// without a comment to re-seat afterwards. Both wants are Python's output.
func TestSpliceFoldsLongValuesLikeRuamel(t *testing.T) {
	long := "the quick brown fox jumps over the lazy dog and keeps running far " +
		"past the horizon into another paragraph entirely"

	got := splice(t, "type: client\nname: Fold Co\ndescription: short\ntier: a  # tier note\n",
		m("type", "client", "name", "Fold Co", "description", long, "tier", "a"))
	want := "type: client\nname: Fold Co\n" +
		"description: the quick brown fox jumps over the lazy dog and keeps running far \n" +
		"  past the horizon into another paragraph entirely\n" +
		"tier: a  # tier note\n"
	if got != want {
		t.Fatalf("no inline comment\n got: %q\nwant: %q", got, want)
	}

	got = splice(t, "type: client\ndescription: short   # desc note\n",
		m("type", "client", "description", long))
	want = "type: client\n" +
		"description: the quick brown fox jumps over the lazy dog and keeps running far \n" +
		"  past the horizon into another paragraph entirely # desc note\n"
	if got != want {
		t.Fatalf("inline comment after a folded value\n got: %q\nwant: %q", got, want)
	}
}

// Recorded divergence. On a comment-bearing document ruamel re-emits every
// line, so it NORMALIZES hand-authored formatting Python-side: `'quoted'` loses
// its quotes, a valueless `empty:` becomes `empty: null`, and an indented
// sequence de-indents to dash-offset 0. The splice preserves the author's bytes
// instead. Values are identical either way; only the rendering of untouched
// lines differs, and only on documents that are BOTH commented and
// non-canonically formatted. Byte-parity with Python is claimed for
// comment-free documents, which take the emitter path.
func TestSplicePreservesAuthoredFormattingPythonWouldNormalize(t *testing.T) {
	src := "name: Norm Co   # keep me\nnotes: 'single quoted'\nempty:\n" +
		"tags:\n  - alpha\n  - beta\nstage: prospect\n"
	want := strings.Replace(src, "stage: prospect", "stage: won", 1)
	got := splice(t, src, m("name", "Norm Co", "notes", "single quoted", "empty", nil,
		"tags", []any{"alpha", "beta"}, "stage", "won"))
	if got != want {
		t.Fatalf("\n got: %q\nwant: %q", got, want)
	}
	// Python would have written this instead:
	//   notes: single quoted / empty: null / tags:\n- alpha\n- beta
}

// Anchors, aliases and merge keys are refused: khub never writes them, and
// ruamel's ignore_aliases would expand them on a re-emit anyway.
func TestSpliceRefusesAnchorsAndMerges(t *testing.T) {
	requireNoSplice(t, "# h\na: &x 1\nb: *x\n", m("a", int64(2), "b", int64(1)))
	requireNoSplice(t, "# h\nbase: &b\n  k: 1\nrow:\n  <<: *b\n  k2: 2\n",
		m("base", m("k", int64(1)), "row", m("k", int64(1), "k2", int64(3))))
	requireNoSplice(t, "# h\n? complex\n: 1\nb: 2\n", m("complex", int64(1), "b", int64(3)))
}

// A CRLF document keeps its line endings; an append to a file with no trailing
// newline adds one first.
func TestSpliceRespectsTheFilesLineEndings(t *testing.T) {
	got := splice(t, "# header\r\na: 1\r\nb: 2\r\n", m("a", int64(9), "b", int64(2)))
	if want := "# header\r\na: 9\r\nb: 2\r\n"; got != want {
		t.Fatalf("crlf\n got: %q\nwant: %q", got, want)
	}
	if got := splice(t, "# h\na: 1", m("a", int64(2))); got != "# h\na: 2" {
		t.Fatalf("no trailing newline: %q", got)
	}
	if got := splice(t, "# h\na: 1", m("a", int64(1), "b", int64(2))); got != "# h\na: 1\nb: 2\n" {
		t.Fatalf("append to a file with no trailing newline: %q", got)
	}
}
