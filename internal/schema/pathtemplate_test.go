package schema

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestParseTemplateAppendsTheSlugLeaf(t *testing.T) {
	tp, err := ParseTemplate("work/{epic}", "md")
	if err != nil {
		t.Fatal(err)
	}
	if got := tp.String(); got != "work/{epic}/{slug}.md" {
		t.Fatalf("String() = %q", got)
	}
	if got := tp.Prefix(); got != "work" {
		t.Fatalf("Prefix() = %q", got)
	}
	if got := tp.Fields(); !reflect.DeepEqual(got, []string{"epic"}) {
		t.Fatalf("Fields() = %v", got)
	}
}

func TestParseTemplateRefusals(t *testing.T) {
	for raw, want := range map[string]string{
		"work/{epic}/x.md":      "must place {slug}",
		"{slug}/x.md":           "must start with a literal directory",
		"work/{slug}.md/x.md":   "only the file name carries an extension",
		"work/{slug}.json":      "only the file name carries an extension",
		"work/../{slug}.md":     "empty, dot or hidden segment",
		"work/.hidden/{slug}":   "empty, dot or hidden segment",
		"work/ep-{slug}/x.md":   "must be a whole {name}",
		"work/{Slug}/{slug}.md": "must be a whole {name}",
		"work/{slug}/spec.json": "names a file that is not .md",
		"/abs/{slug}":           "workspace-relative",
	} {
		if _, err := ParseTemplate(raw, "md"); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ParseTemplate(%q) = %v, want an error containing %q", raw, err, want)
		}
	}
}

func TestTemplateMatch(t *testing.T) {
	tp, _ := ParseTemplate("work/{slug}/{slug}.md", "md")
	if caps, ok := tp.Match("work/ep-1/ep-1.md"); !ok || caps["slug"] != "ep-1" {
		t.Fatalf("Match = %v, %v", caps, ok)
	}
	for _, rel := range []string{
		"work/ep-1/ep-2.md",         // a name placed twice captures one value
		"work/ep-1/ep-1.json",       // the leaf's extension
		"work/.ep-1/.ep-1.md",       // hidden names never match
		"work/node_modules/ep-1.md", // nor vendored ones
		"work/ep-1.md",              // depth
		"other/ep-1/ep-1.md",        // literal
	} {
		if _, ok := tp.Match(rel); ok {
			t.Errorf("Match(%q) matched", rel)
		}
	}
	spec, _ := ParseTemplate("spokes/{slug}/spec.md", "md")
	if caps, ok := spec.Match("spokes/a/spec.md"); !ok || caps["slug"] != "a" {
		t.Fatalf("spec Match = %v, %v", caps, ok)
	}
}

func TestTemplateFill(t *testing.T) {
	tp, _ := ParseTemplate("projects/{project}/work/{epic}/{slug}.md", "md")
	fields := map[string]string{"epic": "ep-1"}
	get := func(name string) (string, bool) { v, ok := fields[name]; return v, ok }
	if _, missing, err := tp.Fill("s", get); err != nil || !reflect.DeepEqual(missing, []string{"project"}) {
		t.Fatalf("missing = %v, err = %v", missing, err)
	}
	fields["project"] = "p1"
	if rel, missing, err := tp.Fill("s", get); err != nil || missing != nil || rel != "projects/p1/work/ep-1/s.md" {
		t.Fatalf("Fill = %q, %v, %v", rel, missing, err)
	}
	for _, bad := range []string{"..", ".git", "a/b", `a\b`, "con", "node_modules"} {
		fields["project"] = bad
		_, _, err := tp.Fill("s", get)
		var pv *PathValueError
		if !errors.As(err, &pv) || pv.Field != "project" || pv.Value != bad {
			t.Errorf("Fill with project %q = %v, want a PathValueError", bad, err)
		}
	}
}

func TestTemplateOverlap(t *testing.T) {
	story, _ := ParseTemplate("work/{epic}/{slug}.md", "md")
	for path, want := range map[string]bool{
		"work/ep-1/x.md": true,  // a singleton the template places
		"work/ep-1/x.ya": false, // another extension
		"work/x.md":      false, // another depth
		"spokes/a/x.md":  false,
	} {
		if got := story.intersects(literalTemplate(path)); got != want {
			t.Errorf("intersects(%q) = %v, want %v", path, got, want)
		}
	}
	flat, _ := ParseTemplate("work/{dir}/{slug}.md", "md")
	if !story.intersects(flat) {
		t.Error("two templates over one directory shape must intersect")
	}
}
