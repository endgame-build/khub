package integrity

import (
	"reflect"
	"testing"

	"github.com/endgame-build/khub/internal/entity"
	"github.com/endgame-build/khub/internal/index"
)

// termsFor builds the lookup mentionSuggestions builds, from the names given
// per node, in (type, slug) node order.
func termsFor(pairs map[index.Node][]string) map[string][]mentionTerm {
	nodes := make([]index.Node, 0, len(pairs))
	for n := range pairs {
		nodes = append(nodes, n)
	}
	index.SortNodes(nodes)
	out := map[string][]mentionTerm{}
	for _, n := range nodes {
		for _, term := range pairs[n] {
			k := firstWord(term)
			out[k] = append(out[k], mentionTerm{text: term, target: n})
		}
	}
	return out
}

func TestFindMentionsLongestWinsOnWordBoundaries(t *testing.T) {
	client := index.Node{Type: "client", Slug: "initech"}
	opp := index.Node{Type: "opportunity", Slug: "initech-pov"}
	dana := index.Node{Type: "person", Slug: "dana-lee"}
	terms := termsFor(map[index.Node][]string{
		client: {"Initech"},
		opp:    {"Initech POV", "initech-pov"},
		dana:   {"Dana Lee", "dana-lee"},
	})
	body := "The initech pov is led by DANA LEE; Initech-wide rollout later. See [[dana-lee]], Initech."
	var got []string
	for _, m := range findMentions(body, terms) {
		got = append(got, m.target.ID()+"="+body[m.start:m.end])
	}
	want := []string{
		"opportunity/initech-pov=initech pov",
		"person/dana-lee=DANA LEE",
		"person/dana-lee=dana-lee",
		"client/initech=Initech",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFindMentionsSharedNameMatchesEveryOwner(t *testing.T) {
	a := index.Node{Type: "client", Slug: "initech"}
	b := index.Node{Type: "opportunity", Slug: "initech-pov"}
	terms := termsFor(map[index.Node][]string{a: {"Initech"}, b: {"Initech"}})
	got := findMentions("about Initech", terms)
	if len(got) != 2 || got[0].target != a || got[1].target != b {
		t.Fatalf("got %+v", got)
	}
}

func TestMentionTermsDropShortAndLetterlessNames(t *testing.T) {
	meta := fields("title", "QA", "name", "  (Quality) ", "aliases", "")
	meta.Set("aliases", []any{"quality", "2024", "Q.A."})
	got := mentionTermsOf(index.Node{Type: "team", Slug: "qa"}, meta)
	want := []string{"Quality", "Q.A"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestCheckMentionsSuggestsUnlinkedNamesOnly(t *testing.T) {
	root := freshWS(t)
	create(t, root, "client", "initech-corp", "name", "Initech Corp", "aliases", "Initech")
	create(t, root, "person", "dana-lee", "name", "Dana Lee")
	create(t, root, "person", "sam-roe", "name", "Sam Roe")
	if _, err := entity.Create(root, "opportunity", entity.CreateOpts{
		Fields: fields("name", "Initech POV", "stage", "prospect", "client", "initech-corp", "owner", "sam-roe"),
		ID:     "initech-pov",
		Body:   "Initech POV: Sam Roe owns it, Dana Lee reviews it, Initech pays.\n",
	}); err != nil {
		t.Fatal(err)
	}

	plain := mustCheck(t, root, false)
	if plain.MentionsRun || plain.Mentions != nil {
		t.Fatalf("mentions ran without being asked: %+v", plain.Mentions)
	}

	report, err := CheckWith(root, CheckOptions{Mentions: true})
	if err != nil {
		t.Fatal(err)
	}
	want := []Mention{{
		Type: "opportunity", Slug: "initech-pov", Target: "person/dana-lee", Text: "Dana Lee",
		// owner is single-valued and already set, so only the `to: any` ones.
		Predicates: []string{"related", "sources", "references", "depends_on"},
	}}
	if !reflect.DeepEqual(report.Mentions, want) {
		t.Fatalf("got %+v, want %+v", report.Mentions, want)
	}
	if report.Passed() != plain.Passed() {
		t.Fatalf("mentions moved the gate: %v vs %v", report.Passed(), plain.Passed())
	}

	link(t, root, "opportunity/initech-pov", "related", "dana-lee")
	report, err = CheckWith(root, CheckOptions{Mentions: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Mentions) != 0 {
		t.Fatalf("a linked mention is still suggested: %+v", report.Mentions)
	}
}

func TestCheckMentionsAnInboundEdgeCounts(t *testing.T) {
	root := freshWS(t)
	create(t, root, "person", "dana-lee", "name", "Dana Lee")
	if _, err := entity.Create(root, "client", entity.CreateOpts{
		Fields: fields("name", "Initech Corp"),
		ID:     "initech-corp",
		Body:   "Our contact is Dana Lee.\n",
	}); err != nil {
		t.Fatal(err)
	}
	report, err := CheckWith(root, CheckOptions{Mentions: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Mentions) != 1 {
		t.Fatalf("want one suggestion, got %+v", report.Mentions)
	}
	link(t, root, "person/dana-lee", "related", "initech-corp")
	report, err = CheckWith(root, CheckOptions{Mentions: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Mentions) != 0 {
		t.Fatalf("an edge in the other direction still suggests one: %+v", report.Mentions)
	}
}

func spansOf(body string, ms []mentionSpan) []string {
	var out []string
	for _, m := range ms {
		out = append(out, m.target.ID()+"="+body[m.start:m.end])
	}
	return out
}

func TestFindMentionsEmphasisWrapAndFold(t *testing.T) {
	sam := index.Node{Type: "person", Slug: "sam-roe"}
	city := index.Node{Type: "site", Slug: "ecole"}
	terms := termsFor(map[index.Node][]string{sam: {"Sam Roe"}, city: {"École"}})
	body := "Ask __Sam\n  Roe__ at the _ÉCOLE_ or the école."
	got := spansOf(body, findMentions(body, terms))
	want := []string{"person/sam-roe=Sam\n  Roe", "site/ecole=ÉCOLE", "site/ecole=école"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFindMentionsLeftmostThenLongest(t *testing.T) {
	big := index.Node{Type: "client", Slug: "big-initech"}
	opp := index.Node{Type: "opportunity", Slug: "initech-pov"}
	terms := termsFor(map[index.Node][]string{big: {"Big Initech"}, opp: {"Initech POV"}})
	body := "We met Big Initech POV Holdings."
	got := spansOf(body, findMentions(body, terms))
	if want := []string{"client/big-initech=Big Initech"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}
