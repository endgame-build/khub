package canon

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/endgame-build/khub/internal/omap"
)

// A YAML `.inf`, `-.inf` or `.nan` in hand-written frontmatter reaches every
// output path as a float with no digits. CPython's repr spells them inf/nan,
// ruamel's representer writes the YAML literal back, and JSON has no token
// at all — so each dialect carries the YAML spelling as a string. Found by
// the adversarial corpus: the digit layout of PyFloatRepr printed `+.Inf`.
func TestDigitlessFloats(t *testing.T) {
	cases := []struct {
		f        float64
		py, yaml string
	}{
		{math.Inf(1), "inf", ".inf"},
		{math.Inf(-1), "-inf", "-.inf"},
		{math.NaN(), "nan", ".nan"},
	}
	for _, c := range cases {
		if got := PyFloatRepr(c.f); got != c.py {
			t.Errorf("PyFloatRepr(%v) = %q, want %q", c.f, got, c.py)
		}
		lit, ok := YAMLFloatLiteral(c.f)
		if !ok || lit != c.yaml {
			t.Errorf("YAMLFloatLiteral(%v) = %q, %v; want %q, true", c.f, lit, ok, c.yaml)
		}
		m := omap.New()
		m.Set("g", c.f)
		for _, d := range dialects {
			out, err := d.fn(m)
			if err != nil {
				t.Fatalf("%s: %v", d.name, err)
			}
			if !json.Valid([]byte(out)) {
				t.Errorf("%s produced invalid JSON for %v: %q", d.name, c.f, out)
			}
			var back map[string]any
			if err := json.Unmarshal([]byte(out), &back); err == nil && back["g"] != c.yaml {
				t.Errorf("%s carries %#v for %v, want %q", d.name, back["g"], c.f, c.yaml)
			}
		}
		row, err := EncodeJSONLRow("r", m)
		if err != nil || !json.Valid([]byte(row)) {
			t.Errorf("jsonl row for %v: %q (%v)", c.f, row, err)
		}
		for _, p := range profiles {
			out, err := p.fn(m)
			if err != nil {
				t.Fatalf("%s: %v", p.name, err)
			}
			if out != "g: "+c.yaml+"\n" {
				t.Errorf("%s emitted %q for %v, want %q", p.name, out, c.f, "g: "+c.yaml+"\n")
			}
		}
	}
	if _, ok := YAMLFloatLiteral(1.5); ok {
		t.Error("YAMLFloatLiteral names a finite float")
	}
}
