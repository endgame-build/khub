package template

import "testing"

func TestMaskTemplateHeadingsBlanksOnlyTheDeclaredPrefix(t *testing.T) {
	tpl := &BodyTemplate{Sections: []Section{{Heading: "Goals"}, {Heading: "Context"}}}
	body := "## Goals for Initech\nGoals stay.\n\n## 2. Context\n## Other\n"
	got := MaskTemplateHeadings(tpl, body)
	want := "         for Initech\nGoals stay.\n\n             \n## Other\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if len(got) != len(body) {
		t.Fatalf("length moved: %d vs %d", len(got), len(body))
	}
	if MaskTemplateHeadings(nil, body) != body {
		t.Fatal("a nil template masked something")
	}
}
