package canon

// Issue #80: a long string the emitter wraps must read back unchanged. Three
// shapes broke it — a double-quoted fold right after an escape sequence, a
// plain fold after a run of spaces, and goccy's fold of a plain scalar once a
// continuation line starts with `-`.

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/endgame-build/khub/internal/omap"
)

// The three values from the issue.
var issue80 = []string{
	"`A` declares five raw `<Reference><HintPath>..\\..\\..\\..\\vendor\\Example\\...\\bin\\Debug\\netstandard2.0\\*.dll</HintPath>` items that duplicate its package references and point at another repository's",
	"The default build target runs `dotnet test Example.Tests.csproj --filter \"TestCategory = Smoke Tests\"`, and any failure fails the build. The other UI fixtures and the whole API project are built but never run by the checked-in build.",
	"it's  C:\\one\\two\\three\\four\\five\\six\\seven\\eight\\nine\\ten\\eleven\\twelve\\thirteen\\fourteen\\fifteen\\sixteen\\file.txt",
}

// roundTrip emits {key: s} in both profiles and reads it back in both modes.
func roundTrip(t *testing.T, key, s string) {
	t.Helper()
	m := omap.New()
	m.Set(key, s)
	for _, p := range profiles {
		out, err := p.fn(m)
		if err != nil {
			t.Fatalf("%s: %v", p.name, err)
		}
		for _, mode := range []ResolveMode{Mode12, Mode11} {
			back, _, err := LoadDocMap(out, mode)
			if err != nil {
				t.Fatalf("%s: does not load: %v\n%s", p.name, err, out)
			}
			if got, _ := back.Get(key); got != s {
				t.Fatalf("%s mode %d: %q read back as %#v\n%s", p.name, mode, s, got, out)
			}
		}
	}
}

func TestIssue80Values(t *testing.T) {
	for _, s := range issue80 {
		roundTrip(t, "description", s)
	}
}

// TestIssue80PrefixLengths varies the first word's length, as the issue did,
// so the fold lands on every position of the space-free run in turn.
func TestIssue80PrefixLengths(t *testing.T) {
	for _, base := range issue80 {
		for n := 1; n <= 60; n++ {
			roundTrip(t, "description", strings.Repeat("A", n)+" "+base)
			roundTrip(t, "d", strings.Repeat("A", n)+"  "+base)
		}
	}
}

// TestWrapRoundTripGrid sweeps the separators that sit at a fold — one or
// two spaces, an escaped backslash, a quote, a dash — across every column.
func TestWrapRoundTripGrid(t *testing.T) {
	words := []string{`a\b`, `x\\`, `it's`, `"q"`, `--flag`, `-x`, `word`, `C:\one\two`, `#h`, `ü`}
	seps := []string{" ", "  ", "   ", `\`, " - "}
	for lead := 0; lead < 40; lead++ {
		for _, w := range words {
			for _, sep := range seps {
				var b strings.Builder
				b.WriteString(strings.Repeat("p", lead+1))
				for i := 0; b.Len() < 220; i++ {
					b.WriteString(sep)
					b.WriteString(w)
					fmt.Fprintf(&b, "%d", i)
				}
				roundTrip(t, "k", b.String())
			}
		}
	}
}

// TestFoldPlainSpec pins the reader's plain-scalar fold on hand-written
// sources, including the goccy shapes that kept white space before a break.
func TestFoldPlainSpec(t *testing.T) {
	cases := []struct{ src, want string }{
		{"d: a \n  -x b \n  c\n", "a -x b c"},
		{"d: a\n  -x \n  c \n  e\n", "a -x c e"},
		{"d: a\n  -x \n\n  c\n", "a -x\nc"},
		{"d: a\n\n\n  b\n", "a\n\nb"},
		{"d: a\n  -x\n  c   # note\ne: 1\n", "a -x c"},
		{"d: a\r\n  -b \r\n  c\r\n", "a -b c"},
		{"d:\n  a\n  b\n", "a b"},
		{"d: a  b\n", "a  b"},
		{"d: a#b\n  c\n", "a#b c"},
		// Text after a comment line is malformed (PyYAML rejects it); the fold
		// must not drop `e`, so goccy's value stands.
		{"d: a\n  -b \n  c # d\n  e\n", "a -b  c # d e"},
		{"- a \n  -b \n  c\n", ""},
	}
	for _, c := range cases {
		v, err := LoadDoc(c.src)
		if err != nil {
			t.Fatalf("%q: %v", c.src, err)
		}
		var got any
		switch x := v.(type) {
		case *omap.Map:
			got, _ = x.Get("d")
		case []any:
			got, c.want = x[0], "a -b c"
		}
		if got != c.want {
			t.Errorf("%q read as %q, want %q", c.src, got, c.want)
		}
	}
}

// FuzzStringRoundTrip: any string the emitter writes as a mapping value reads
// back as the same string, in both profiles and both resolve modes. Strings
// that resolve as another type are the emitter's to quote, so they count too.
func FuzzStringRoundTrip(f *testing.F) {
	for _, s := range issue80 {
		f.Add(s)
	}
	f.Add("x  " + strings.Repeat("y", 90))
	f.Add(strings.Repeat(`a\\ `, 30))
	f.Fuzz(func(t *testing.T, s string) {
		// Not text, or one of the two loader gaps FuzzEmitRoundTrip records;
		// none of them is a fold.
		if !utf8.ValidString(s) || strings.ContainsAny(s, "\u2028\u2029\u0085") || strings.Contains(s, "<<") {
			t.Skip()
		}
		if got, isStr := ResolvePlainScalarMode(s, Mode11).(string); !isStr || got != s {
			t.Skip() // the emitter quotes by 1.2; the 1.1 split (1:0, yes) is by design
		}
		if _, _, bad := firstNonPrintable(s); bad {
			t.Skip() // escaped by the emitter, but rejected by the reader's Python twin
		}
		roundTrip(t, "k", s)
	})
}
