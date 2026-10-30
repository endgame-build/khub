package errs

import "testing"

// The message is printed verbatim, so its bytes are pinned here.
func TestPortNotPermittedMessage(t *testing.T) {
	e := PortNotPermitted(80)
	want := "Not permitted to bind port 80; it needs privileges, the system reserves it, " +
		"or another program holds it. Pass --port <n> to serve on another port"
	if e.Code != "port_not_permitted" || e.Error() != want {
		t.Fatalf("PortNotPermitted(80) = %q %q, want port_not_permitted %q", e.Code, e.Error(), want)
	}
}

// Nearest is the DTO's did-you-mean. A wrong guess is worse than silence, so
// the far misses below must stay empty.
func TestNearest(t *testing.T) {
	cases := []struct {
		want       string
		candidates []string
		out        string
	}{
		{"functionl", []string{"functional", "non-functional"}, "functional"},
		{"statsu", []string{"status", "stack"}, "status"},
		{"knd", []string{"type", "kind"}, "kind"},
		{"nope", []string{"type", "tags"}, ""},               // two edits in four is a different word
		{"Functional", []string{"functional"}, "functional"}, // case alone always qualifies
		{"ab", []string{"ac"}, ""},                           // under three characters, case only
		{"AB", []string{"ab"}, "ab"},
		{"kind", []string{"kind", "kinds"}, "kinds"}, // an exact match is never suggested back
		{"agent", []string{"claude", "agents", "both"}, "agents"},
		{"cat", []string{"bat", "cab"}, ""},    // two candidates equally near is a tie: silence
		{"cat", []string{"bat", "bat"}, "bat"}, // the same spelling twice is no tie
		{"x", nil, ""},
		{"knid", []string{"kind", "title"}, "kind"}, // a swap of neighbours is one edit
	}
	for _, c := range cases {
		if got := Nearest(c.want, c.candidates); got != c.out {
			t.Errorf("Nearest(%q, %v) = %q, want %q", c.want, c.candidates, got, c.out)
		}
	}
}

// The factories fill the DTO fields from what they already know; the message
// is pinned prose and never mentions the suggestion.
func TestEnumViolationDTO(t *testing.T) {
	e := EnumViolation("acepted", "status", []string{"proposed", "accepted"})
	if e.Field != "status" || e.Suggestion != "accepted" || len(e.Expected) != 2 {
		t.Fatalf("EnumViolation DTO = %q %v %q", e.Field, e.Expected, e.Suggestion)
	}
	if e.Error() != "'acepted' is not a valid status (proposed, accepted)" {
		t.Fatalf("EnumViolation message moved: %q", e.Error())
	}
	if l := LookupError("x").Near("y", nil); l.Suggestion != "y" || l.Expected != nil || l.Error() != "No entity 'x' found" {
		t.Fatalf("Near = %q %v %q", l.Suggestion, l.Expected, l.Error())
	}
	if l := LookupError("x").Near("", []string{"a/x1", "b/x1"}); l.Suggestion != "" || len(l.Expected) != 2 {
		t.Fatalf("Near tie = %q %v", l.Suggestion, l.Expected)
	}
	if e := StrictUnknownField("knid", []string{"title", "kind"}); e.Field != "knid" || e.Suggestion != "kind" {
		t.Fatalf("StrictUnknownField DTO = %q %q", e.Field, e.Suggestion)
	}
}

// NearestAll keeps every candidate at the best distance, in candidate order,
// so a caller matching spellings of different things can see the tie.
func TestNearestAll(t *testing.T) {
	got := NearestAll("cmp-serch", []string{"cmp-search", "cmp-search", "cmp-serch2", "cmp-api"})
	if len(got) != 3 || got[0] != 0 || got[1] != 1 || got[2] != 2 {
		t.Fatalf("NearestAll = %v, want [0 1 2]", got)
	}
	if got := NearestAll("zzzz", []string{"cmp-search"}); got != nil {
		t.Fatalf("NearestAll far miss = %v, want nil", got)
	}
}
