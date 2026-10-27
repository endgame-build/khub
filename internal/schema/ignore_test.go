package schema

import (
	"strings"
	"testing"
)

func TestIgnoredMatches(t *testing.T) {
	l, err := ParseIgnore("# examples\ndocs/examples/\n/notes.md\nfixtures/\nplatform/**/phase8/\n*.draft.md\n\n")
	if err != nil {
		t.Fatal(err)
	}
	for rel, want := range map[string]bool{
		"docs/examples/hubs/dns/spokes/dns-core/spec.md": true, // below a matched directory
		"docs/examples.md":                      false, // not that directory
		"notes.md":                              true,
		"sub/notes.md":                          false, // anchored at the root
		"a/b/fixtures/x.md":                     true,  // a bare name, any depth
		"a/b/fixtures":                          false, // dirOnly, and fixtures here is a file
		"platform/jira-sync/phase8/create/x.md": true,  // ** spans directories
		"platform/phase8/x.md":                  true,  // ** spans none
		"work/ep-1/plan.draft.md":               true,
		"work/ep-1/plan.md":                     false,
	} {
		if got := l.Ignored(rel, false); got != want {
			t.Errorf("Ignored(%q) = %v, want %v", rel, got, want)
		}
	}
	if !l.Ignored("a/fixtures", true) {
		t.Error("a directory named fixtures must be ignored")
	}
	var none *IgnoreList
	if none.Ignored("anything.md", false) {
		t.Error("a nil list ignores nothing")
	}
}

func TestParseIgnoreRefusals(t *testing.T) {
	for line, want := range map[string]string{
		"!keep.md": "negation is not supported",
		"/":        "names nothing",
		"a/../b":   "'..' segment",
		"a//b":     "empty",
		"docs/[x/": "malformed glob",
	} {
		if _, err := ParseIgnore(line + "\n"); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ParseIgnore(%q) = %v, want an error containing %q", line, err, want)
		}
	}
}
