package canon

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/omap"
)

// YAML merge keys load as ruamel (Mode12) and PyYAML (Mode11) load them: an
// explicit key wins wherever it sits, and the key order is each loader's.
// The expected values were read from ruamel.yaml 0.19.1 (typ='rt') and
// PyYAML's safe_load.
func TestMergeKeys(t *testing.T) {
	cases := []struct {
		name, doc string
		want12    string // keys=values of x, in order
		want11    string
	}{
		{"block", "base: &b\n  owner: noor\n  tier: gold\nx:\n  <<: *b\n  name: x\n",
			"name=x owner=noor tier=gold", "owner=noor tier=gold name=x"},
		{"flow", "base: &b {owner: noor, tier: gold}\nx: {<<: *b, name: x}\n",
			"name=x owner=noor tier=gold", "owner=noor tier=gold name=x"},
		{"list", "a: &a {k: 1, p: a}\nb: &b {k: 2, q: b}\nx:\n  <<: [*a, *b]\n  z: 3\n",
			"z=3 k=1 p=a q=b", "k=1 q=b p=a z=3"},
		{"explicit before", "base: &b {owner: noor, tier: gold}\nx:\n  tier: silver\n  <<: *b\n",
			"tier=silver owner=noor", "owner=noor tier=silver"},
		{"explicit after", "base: &b {owner: noor, tier: gold}\nx:\n  <<: *b\n  tier: silver\n",
			"tier=silver owner=noor", "owner=noor tier=silver"},
	}
	render := func(m *omap.Map) string {
		var parts []string
		for _, k := range m.Keys() {
			v, _ := m.Get(k)
			parts = append(parts, k+"="+fmt.Sprint(v))
		}
		return strings.Join(parts, " ")
	}
	for _, c := range cases {
		for mode, want := range map[ResolveMode]string{Mode12: c.want12, Mode11: c.want11} {
			doc, _, err := LoadDocMap(c.doc, mode)
			if err != nil {
				t.Errorf("%s mode %d: %v", c.name, mode, err)
				continue
			}
			x, _ := doc.Get("x")
			if got := render(x.(*omap.Map)); got != want {
				t.Errorf("%s mode %d: x = %s, want %s", c.name, mode, got, want)
			}
		}
	}
	// A quoted "<<" is an ordinary key, and a merge of a scalar is an error.
	doc, _, err := LoadDocMap("x: {\"<<\": 1}\n", Mode12)
	if err != nil {
		t.Fatal(err)
	}
	if x, _ := doc.Get("x"); render(x.(*omap.Map)) != "<<=1" {
		t.Errorf("a quoted << merged: %v", x)
	}
	if _, err := LoadDocMode("x: {<<: 1}\n", Mode12); err == nil {
		t.Error("a merge of a scalar loaded")
	}
	// One `<<` per mapping: goccy refuses a second, so a list is the only way
	// to merge several.
	if _, err := LoadDocMode("a: &a {k: 1}\nx:\n  <<: *a\n  <<: *a\n", Mode12); err == nil {
		t.Error("a second << loaded")
	}
}

// A merge of an alias counts toward the alias budget like any alias.
func TestMergeCountsTowardTheAliasBudget(t *testing.T) {
	var b strings.Builder
	b.WriteString("a: &a\n")
	for i := range MaxAliasNodes / 2 {
		b.WriteString("  k" + strconv.Itoa(i) + ": 1\n")
	}
	doc := b.String() + "x: {<<: *a}\ny: {<<: *a}\nz: {<<: *a}\n"
	if _, err := LoadDocMode(doc, Mode12); err == nil || !strings.Contains(err.Error(), "aliases expand past") {
		t.Fatalf("merges past the budget: %v", err)
	}
}
