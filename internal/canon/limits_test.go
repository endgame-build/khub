package canon

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/omap"
)

// A billion-laughs document is refused at load, as frontmatter and as a yaml
// entity, before anything renders it.
func TestAliasBombIsRefused(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "alias-bomb.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDocMode(string(raw), Mode12); err == nil || !strings.Contains(err.Error(), "aliases expand past") {
		t.Fatalf("LoadDocMode = %v", err)
	}
	for _, doc := range []struct{ text, fmt string }{
		{"---\ntype: note\n" + string(raw) + "---\n", "md"},
		{"type: note\n" + string(raw), "yaml"},
	} {
		var le *errs.Located
		if _, _, err := Parse(doc.text, doc.fmt); !errors.As(err, &le) || le.Code != "malformed_entity" {
			t.Errorf("Parse(%s) = %v, want malformed_entity", doc.fmt, err)
		}
	}
}

// Ordinary aliasing loads: a few repeated values stay far under the budget.
func TestModestAliasesLoad(t *testing.T) {
	doc := "base: &b {owner: noor, tier: gold}\ny: *b\nz: [*b, *b]\n"
	if _, err := LoadDocMode(doc, Mode12); err != nil {
		t.Fatal(err)
	}
}

// The budget counts expanded nodes, so one alias to a large anchor counts the
// anchor's size, and a flat document of any size never trips it.
func TestAliasBudgetCountsExpansion(t *testing.T) {
	big := "a: &a [" + strings.Repeat("x, ", MaxAliasNodes/2) + "x]\n"
	if _, err := LoadDocMode(big+"b: *a\n", Mode12); err != nil {
		t.Fatalf("one alias under the budget: %v", err)
	}
	if _, err := LoadDocMode(big+"b: *a\nc: *a\nd: *a\n", Mode12); err == nil {
		t.Fatal("three aliases past the budget loaded")
	}
}

// A text document past MaxTextBytes is refused by every entry point, with the
// path when the caller has one.
func TestOversizedTextIsRefused(t *testing.T) {
	text := "---\ntype: note\n---\n" + strings.Repeat("x", MaxTextBytes)
	var le *errs.Located
	if _, _, err := Parse(text, "md"); !errors.As(err, &le) || le.Code != "malformed_entity" {
		t.Errorf("Parse = %v", err)
	}
	if _, err := LoadCollection(strings.Repeat(" ", MaxTextBytes+1), "yaml"); !errors.As(err, &le) {
		t.Errorf("LoadCollection = %v", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "big.md")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadText(path); !errors.As(err, &le) || !strings.Contains(le.Message, "big.md is ") {
		t.Errorf("ReadText = %v", err)
	}
	if _, err := ReadTextIn(dir, path); !errors.As(err, &le) || le.Code != "malformed_entity" {
		t.Errorf("ReadTextIn = %v", err)
	}
	if _, _, err := Parse(text[:MaxTextBytes], "md"); err != nil {
		t.Errorf("a document at the limit was refused: %v", err)
	}
}

// The emitter never breaks a plain scalar after a run of two or more spaces:
// reading back folds the break and the spaces before it into one space. ruamel
// breaks there and changes the value; khub runs the line on. Both profiles.
func TestPlainScalarKeepsASpaceRun(t *testing.T) {
	for _, c := range []struct {
		name  string
		dump  func(any) (string, error)
		value string
	}{
		{"round-trip", DumpRT, strings.Repeat("x", 69) + "  tail"},
		{"round-trip", DumpRT, strings.Repeat("word ", 14) + "   gap   " + strings.Repeat("word ", 14)},
		{"wide", DumpWide, strings.Repeat("x", 4085) + "  tail"},
	} {
		doc := omap.New()
		doc.Set("name", c.value)
		text, err := c.dump(doc)
		if err != nil {
			t.Fatal(err)
		}
		back, _, err := LoadDocMap(text, Mode12)
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := back.Get("name"); got != c.value {
			t.Errorf("%s: read back differs\n%q", c.name, text)
		}
	}
}

// A value holding NEL, LS or PS round-trips in both profiles: it is
// double-quoted with the break escaped, never single-quoted with it raw.
func TestExoticBreaksRoundTrip(t *testing.T) {
	for _, value := range []string{"\u0085", "a\u0085b", "\u0085a", "a\u0085", " ", "x y", "a\u0085\u0085b"} {
		for name, dump := range map[string]func(any) (string, error){"round-trip": DumpRT, "wide": DumpWide} {
			doc := omap.New()
			doc.Set("k", value)
			first, err := dump(doc)
			if err != nil {
				t.Fatal(err)
			}
			back, _, err := LoadDocMap(first, Mode12)
			if err != nil {
				t.Fatal(err)
			}
			got, _ := back.Get("k")
			again, _ := dump(back)
			if got != value || again != first {
				t.Errorf("%s %q: read back %q, re-emitted %q from %q", name, value, got, again, first)
			}
		}
	}
}
