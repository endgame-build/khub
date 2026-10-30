package canon

// Issue #80: a long frontmatter value must survive emit → load in both
// profiles. Three shapes broke it: a double-quoted fold right after an
// escaped backslash (written without the `\` continuation, so the value
// gained a space), a plain scalar's continuation line with trailing white
// space (goccy read the space twice), and a plain scalar folded inside a run
// of two spaces (the fold discarded one). The first and third are ruamel's
// own bytes, so these tests pin where khub departs from them.

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/omap"
)

// The three values from the issue, verbatim.
const (
	issue80Double = "`A` declares five raw `<Reference><HintPath>..\\..\\..\\..\\vendor\\Example\\...\\bin\\Debug\\netstandard2.0\\*.dll</HintPath>` items that duplicate its package references and point at another repository's"
	issue80Plain  = "The default build target runs `dotnet test Example.Tests.csproj --filter \"TestCategory = Smoke Tests\"`, and any failure fails the build. The other UI fixtures and the whole API project are built but never run by the checked-in build."
	issue80Spaces = "it's  C:\\one\\two\\three\\four\\five\\six\\seven\\eight\\nine\\ten\\eleven\\twelve\\thirteen\\fourteen\\fifteen\\sixteen\\file.txt"
)

// assertRoundTrip emits {description: v} in both profiles and checks that
// each loads back as v at both altitudes and emits again byte-identically.
func assertRoundTrip(t *testing.T, label, v string) {
	t.Helper()
	doc := omap.New()
	doc.Set("description", v)
	for _, p := range profiles {
		first, err := p.fn(doc)
		if err != nil {
			t.Fatalf("%s %s: emit: %v", label, p.name, err)
		}
		for _, mode := range []ResolveMode{Mode12, Mode11} {
			back, _, err := LoadDocMap(first, mode)
			if err != nil {
				t.Fatalf("%s %s mode %d: load: %v\n%s", label, p.name, mode, err, first)
			}
			got, _ := back.Get("description")
			if got != v {
				t.Fatalf("%s %s mode %d: value moved\nwant %q\ngot  %q\nemitted:\n%s", label, p.name, mode, v, got, first)
			}
		}
		back, _, _ := LoadDocMap(first, Mode12)
		second, err := p.fn(back)
		if err != nil {
			t.Fatalf("%s %s: second emit: %v", label, p.name, err)
		}
		if second != first {
			t.Fatalf("%s %s: emit is not idempotent\n--- first\n%s--- second\n%s", label, p.name, first, second)
		}
	}
}

func TestIssue80ValuesRoundTrip(t *testing.T) {
	for _, c := range []struct{ label, value string }{
		{"double", issue80Double}, {"plain", issue80Plain}, {"spaces", issue80Spaces},
	} {
		assertRoundTrip(t, c.label, c.value)
	}
}

// TestIssue80Bytes pins the round-trip profile's bytes for the three values:
// the two departures from ruamel (the escaped break after `\\`, the quoted
// style for a space run at the fold) and the one shape ruamel gets right
// (trailing space before a plain fold, which the loader now folds per spec).
func TestIssue80Bytes(t *testing.T) {
	cases := []struct{ label, value, want string }{
		{"double", issue80Double,
			"description: \"`A` declares five raw `<Reference><HintPath>..\\\\..\\\\..\\\\..\\\\vendor\\\\\\\n" +
				"  Example\\\\...\\\\bin\\\\Debug\\\\netstandard2.0\\\\*.dll</HintPath>` items that duplicate\n" +
				"  its package references and point at another repository's\"\n"},
		{"plain", issue80Plain,
			"description: The default build target runs `dotnet test Example.Tests.csproj \n" +
				"  --filter \"TestCategory = Smoke Tests\"`, and any failure fails the build. The \n" +
				"  other UI fixtures and the whole API project are built but never run by the \n" +
				"  checked-in build.\n"},
		{"spaces", issue80Spaces,
			"description: \"it's  C:\\\\one\\\\two\\\\three\\\\four\\\\five\\\\six\\\\seven\\\\eight\\\\nine\\\\ten\\\\\\\n" +
				"  eleven\\\\twelve\\\\thirteen\\\\fourteen\\\\fifteen\\\\sixteen\\\\file.txt\"\n"},
	}
	for _, c := range cases {
		doc := omap.New()
		doc.Set("description", c.value)
		got, err := DumpRT(doc)
		if err != nil {
			t.Fatal(err)
		}
		if got != c.want {
			t.Errorf("%s: bytes moved\n--- want\n%s--- got\n%s", c.label, c.want, got)
		}
	}
}

// TestIssue80PrefixSweep varies the opening word's length 1..40 as the issue
// did, so the fold lands on every position of each hazard.
func TestIssue80PrefixSweep(t *testing.T) {
	for n := 1; n <= 40; n++ {
		pad := strings.Repeat("A", n)
		assertRoundTrip(t, "double/"+pad, "`"+pad+issue80Double[4:])
		assertRoundTrip(t, "plain/"+pad, pad+" "+issue80Plain)
		assertRoundTrip(t, "spaces/"+pad, "it's"+pad+issue80Spaces[4:])
		assertRoundTrip(t, "spaces-nested/"+pad, "word  "+pad+"  "+strings.Repeat("x", 70)+"  tail")
		assertRoundTrip(t, "double-space-run/"+pad, pad+"'s "+strings.Repeat("w ", 30)+"  "+strings.Repeat("z\\y", 20))
	}
}

// TestFoldGenerated draws values from an alphabet of the characters that steer
// style choice and folding, and asserts every one survives both profiles at
// both altitudes. Seeded, so a failure names a reproducible value.
func TestFoldGenerated(t *testing.T) {
	alphabet := []rune("abcdefgh xyz  \\\\'\"`:.-#!")
	rng := rand.New(rand.NewSource(80))
	for i := 0; i < 3000; i++ {
		n := 60 + rng.Intn(160)
		rs := make([]rune, n)
		for j := range rs {
			rs[j] = alphabet[rng.Intn(len(alphabet))]
		}
		v := string(rs)
		if strings.Contains(v, "<<") {
			continue // known goccy gap, see FuzzEmitRoundTrip
		}
		assertRoundTrip(t, "gen", v)
	}
}

// TestFoldPlainLegacyBytes reads files khub 0.30.0 wrote. The trailing space
// before a plain fold now reads per spec (the value is intact); the two
// shapes where the bytes themselves lost the value read as the bytes say.
func TestFoldPlainLegacyBytes(t *testing.T) {
	cases := []struct{ label, doc, want string }{
		{"plain trailing space before fold, continuation lines",
			"description: The default build target runs `dotnet test Example.Tests.csproj \n  --filter \"TestCategory = Smoke Tests\"`, and any failure fails the build. The \n  other UI fixtures and the whole API project are built but never run by the \n  checked-in build.\n",
			issue80Plain},
		{"plain space run split by a fold", "description: it's  \n  C:\\one\\two\n", "it's C:\\one\\two"},
		{"double fold after escaped backslash without continuation", "description: \"..\\\\vendor\\\\\n  Example\\\\x\"\n", "..\\vendor\\ Example\\x"},
		{"plain tab and spaces before fold", "d: one two \t \n  three\n", "one two three"},
		{"plain empty lines fold to breaks", "d: one two \n\n  three  \n\n\n  four\n", "one two\nthree\n\nfour"},
		{"plain crlf", "d: one two \r\n  three\r\n", "one two three"},
		{"plain value on the next line", "d:\n  one two \n  three\n", "one two three"},
		{"double escaped break keeps trailing space", "d: \"foo \\\n  \\ bar\"\n", "foo  bar"},
		{"double escaped break joins", "d: \"abc\\\n  def\"\n", "abcdef"},
		{"double plain fold", "d: \"abc \n  def\"\n", "abc def"},
		{"single fold", "d: 'abc \n  def'\n", "abc def"},
	}
	for _, c := range cases {
		for _, mode := range []ResolveMode{Mode12, Mode11} {
			m, _, err := LoadDocMap(c.doc, mode)
			if err != nil {
				t.Fatalf("%s mode %d: %v", c.label, mode, err)
			}
			got, _ := m.Get(m.Keys()[0])
			if got != c.want {
				t.Errorf("%s mode %d:\nwant %q\ngot  %q", c.label, mode, c.want, got)
			}
		}
	}
}

func TestFoldPlain(t *testing.T) {
	cases := []struct{ in, want string }{
		{" a b \n  c", "a b c"},
		{"\n  a\n  b", "a b"},
		{" a\n\n  b", "a\nb"},
		{" a  \n\n\n  b\n", "a\n\nb"},
		{" a\t\n  \tb", "a b"},
		{" one\n", "one"},
		{" a \r\n  b\r\n", "a b"},
		{" a\n   deeper\n  back", "a deeper back"},
		{" a\n  b\n\n", "a b"},
	}
	for _, c := range cases {
		if got := foldPlain(c.in); got != c.want {
			t.Errorf("foldPlain(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
