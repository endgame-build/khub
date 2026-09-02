// Body-scanning rows ported from kb 0.14.0 tests/test_kb.py: heading
// discovery over comment- and fence-masked prose, CommonMark fence pairing,
// numbering, the alignment contract, the section rules, word counts, and
// the search index's reading of comments.
package template

import (
	"reflect"
	"strings"
	"testing"
)

// rules is kb's `rules(yaml, body)` helper: the complaints a one-off template
// makes about a body, as `heading: complaint`.
func rules(t *testing.T, yaml, body string) []string {
	t.Helper()
	root := t.TempDir()
	writeTemplate(t, root, "adr", yaml)
	tpl, err := LoadTemplate(root, "adr", nil)
	if err != nil {
		t.Fatal(err)
	}
	if tpl == nil {
		t.Fatal("no template loaded")
	}
	out := []string{}
	for _, c := range BodyRules(tpl, body) {
		out = append(out, c.Heading+": "+c.Reason)
	}
	return out
}

func wantRules(t *testing.T, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rules = %q, want %q", got, want)
	}
}

var adrSections = []Section{{Heading: "Context"}, {Heading: "Decision"}, {Heading: "Consequences"}}

// --- headings --------------------------------------------------------------------

func TestCommentedOutHeadingIsNotAHeading(t *testing.T) {
	// Heading discovery reads prose like every other reader. Otherwise a `##`
	// inside a scaffold hint invents a section, and commenting a real heading
	// out satisfies body_shape for a section the document no longer has.
	body := "## Context\n\n<!-- and then a hint that says:\n## Decision\ngoes here -->\n\nthe actual context\n"
	if got := BodyH2s(body); !reflect.DeepEqual(got, []string{"Context"}) {
		t.Errorf("BodyH2s = %v", got)
	}
	// The span of Context runs to the end of the body, not to the fake
	// heading — so its prose is measured, not the four words above the comment.
	if got := Metrics(body).Sections; !reflect.DeepEqual(got, []SectionWords{{"Context", 3}}) {
		t.Errorf("Metrics sections = %+v", got)
	}

	// The mirror case, and the worse one.
	tpl := &BodyTemplate{Type: "adr", Sections: []Section{{Heading: "Decision"}}}
	hidden := "<!--\n## Decision\n-->\n\nWe are using Postgres for the whole of it.\n"
	if got := BodyH2s(hidden); len(got) != 0 {
		t.Errorf("BodyH2s = %v, want none", got)
	}
	if missing, ok := MissingHeading(tpl, hidden); !ok || missing != "Decision" {
		t.Errorf("MissingHeading = (%q, %v)", missing, ok)
	}
}

func TestHeadingLineCommentIsNotPartOfTheHeading(t *testing.T) {
	// The heading text comes from the masked prose, as kb's `_h2_spans` takes
	// `m.group(1)` of `prose(body)`. Read from the raw body instead, a hint
	// left on the heading line becomes part of the heading and `Context` no
	// longer aligns with `## Context <!-- one paragraph -->`.
	tpl := &BodyTemplate{Type: "adr", Sections: []Section{{Heading: "Context"}}}
	for _, body := range []string{
		"## Context <!-- one paragraph -->\n\nthe forces\n",
		"## <!-- x --> Context\n\nthe forces\n",
	} {
		if got := BodyH2s(body); !reflect.DeepEqual(got, []string{"Context"}) {
			t.Errorf("BodyH2s(%q) = %v, want [Context]", body, got)
		}
		if missing, ok := MissingHeading(tpl, body); ok {
			t.Errorf("MissingHeading(%q) = %q, want none", body, missing)
		}
		// The span still starts after the whole heading line, comment
		// included, so the comment is not counted as the section's prose.
		if got := Metrics(body).Sections; !reflect.DeepEqual(got, []SectionWords{{"Context", 2}}) {
			t.Errorf("Metrics(%q).Sections = %+v", body, got)
		}
	}
}

func TestUnterminatedCommentRunsToEndOfDocument(t *testing.T) {
	// What fenceSpans already does for an unterminated fence, and what every
	// Markdown renderer does. Leaving the rest visible would mean reading
	// headings a reader cannot see — check passing a document that renders
	// as nothing.
	body := "## Context\n\nWe needed a store.\n\n<!-- forgot to close\n\n## Decision\n\nPostgres.\n"
	if got := BodyH2s(body); !reflect.DeepEqual(got, []string{"Context"}) {
		t.Errorf("BodyH2s = %v", got)
	}
	if got := Metrics(body).Sections; !reflect.DeepEqual(got, []SectionWords{{"Context", 4}}) {
		t.Errorf("Metrics sections = %+v", got)
	}
}

func TestUnterminatedFenceRunsToEOF(t *testing.T) {
	body := "## A\n\nsome words\n\n```\n## B\n\ntext\n"
	if got := BodyH2s(body); !reflect.DeepEqual(got, []string{"A"}) {
		t.Errorf("BodyH2s = %v", got)
	}
	if got := Metrics(body).Sections; !reflect.DeepEqual(got, []SectionWords{{"A", 2}}) {
		t.Errorf("Metrics sections = %+v", got)
	}
	if got := FencedLangs("```py\nx\n"); !reflect.DeepEqual(got, []string{"py"}) {
		t.Errorf("FencedLangs = %v", got)
	}
}

func TestFencePairingIsCommonMark(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []string
	}{
		{"longer closer closes", "```\ncode\n````\n## T\n", []string{"T"}},
		{"shorter closer does not", "````\ncode\n```\n## T\n", []string{}},
		{"tilde is not closed by backticks", "~~~\ncode\n```\n## T\n", []string{}},
		{"backticks are not closed by tildes", "```\ncode\n~~~\n## T\n", []string{}},
		{"closer with an info string is not a closer", "```\ncode\n``` info\n## T\n", []string{}},
		{"three-space indent on both", "   ```\n## H\n   ```\n## T\n", []string{"T"}},
		{"four-space indent is not a fence", "    ```\n## H\n", []string{"H"}},
		{"backtick in a backtick info string is not an opener", "``` a`b\n## H\n", []string{"H"}},
		{"backtick in a tilde info string is fine", "~~~ a`b\n## H\n~~~\n## T\n", []string{"T"}},
		{"opener with trailing whitespace", "```   \n## H\n```\n## T\n", []string{"T"}},
		{"info string language", "```mermaid title=x\n## H\n```\n## T\n", []string{"T"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := BodyH2s(tc.body); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("BodyH2s(%q) = %v, want %v", tc.body, got, tc.want)
			}
		})
	}
	if got := FencedLangs("```mermaid title=x\n```\n~~~\n~~~\n```   Bash \nls\n```\n"); !reflect.DeepEqual(got, []string{"mermaid", "", "Bash"}) {
		t.Errorf("FencedLangs = %v", got)
	}
}

func TestMasksPreserveLengthAndNewlines(t *testing.T) {
	// Byte-length and newline preserving, multi-byte content included, so
	// offsets found in the masked text address the original body.
	body := "## Ünïcode\n\n<!-- héllo\nwörld -->\n\n```\n日本語\n```\n\ntext 終\n<!-- open"
	for name, masked := range map[string]string{
		"MaskFences":   MaskFences(body),
		"MaskComments": MaskComments(body),
		"Prose":        Prose(body),
	} {
		if len(masked) != len(body) {
			t.Errorf("%s: len %d, want %d", name, len(masked), len(body))
		}
		if strings.Count(masked, "\n") != strings.Count(body, "\n") {
			t.Errorf("%s: newline count moved", name)
		}
		for i := 0; i < len(body); i++ {
			if (body[i] == '\n') != (masked[i] == '\n') {
				t.Errorf("%s: newline at byte %d moved", name, i)
			}
		}
	}
	prose := Prose(body)
	for _, gone := range []string{"héllo", "wörld", "日本語", "```", "open", "<!--"} {
		if strings.Contains(prose, gone) {
			t.Errorf("Prose still holds %q", gone)
		}
	}
	for _, kept := range []string{"## Ünïcode", "text 終"} {
		if !strings.Contains(prose, kept) {
			t.Errorf("Prose lost %q", kept)
		}
	}
	// The H2 span's offsets slice the original body, comment and fence included.
	spans := H2Spans(body)
	if len(spans) != 1 || spans[0].Heading != "Ünïcode" {
		t.Fatalf("H2Spans = %+v", spans)
	}
	if got := body[spans[0].Start:spans[0].End]; !strings.HasPrefix(got, "\n\n<!-- héllo") || !strings.HasSuffix(got, "<!-- open") {
		t.Errorf("sliced section = %q", got)
	}
	if got := Metrics(body); got.Words != 2 || !reflect.DeepEqual(got.Sections, []SectionWords{{"Ünïcode", 2}}) {
		t.Errorf("Metrics = %+v", got)
	}
}

func TestNumberingStripsOnlyWithTrailingSeparator(t *testing.T) {
	// `\d+\s+` with an optional separator swallowed the leading number of any
	// heading that legitimately starts with one, so a declared `2026 goals`
	// could never be satisfied by a body containing it verbatim.
	cases := map[string]string{
		"## 1. Context\n":         "Context",
		"## 2.1) Context\n":       "Context",
		"## 3) Context\n":         "Context",
		"## 2026 goals\n":         "2026 goals",
		"## 3 open questions\n":   "3 open questions",
		"## 10.2.3 Deep\n":        "10.2.3 Deep",
		"## 10.2.3. Deep\n":       "Deep",
		"## 1.\tTabbed\n":         "Tabbed",
		"## 1.Nospace\n":          "1.Nospace",
		"##   2)   padded   \n":   "padded",
		"## 12345678901234) X\n":  "X",
		"## 4.) Odd but strips\n": "Odd but strips",
	}
	for body, want := range cases {
		if got := BodyH2s(body); !reflect.DeepEqual(got, []string{want}) {
			t.Errorf("BodyH2s(%q) = %v, want [%q]", body, got, want)
		}
	}
	numeric := &BodyTemplate{Type: "prd", Sections: []Section{{Heading: "2026 goals"}}}
	if missing, ok := MissingHeading(numeric, "## 2026 goals\n"); ok {
		t.Errorf("MissingHeading = %q, want none", missing)
	}
}

func TestH2IndentedUpToThreeSpaces(t *testing.T) {
	body := " ## A\n  ## B\n   ## C\n    ## D\n##\tTab\n\t## Not\n"
	if got := BodyH2s(body); !reflect.DeepEqual(got, []string{"A", "B", "C", "Tab"}) {
		t.Errorf("BodyH2s = %v", got)
	}
	tpl := &BodyTemplate{Type: "t", Sections: []Section{{Heading: "C"}}}
	if missing, ok := MissingHeading(tpl, "   ## C\n"); ok {
		t.Errorf("MissingHeading = %q, want none", missing)
	}
}

// --- the alignment contract ------------------------------------------------------

func TestBodyContractIsPermissive(t *testing.T) {
	// Presence and order of the declared headings is the WHOLE contract.
	// Everything else an author reaches for has to pass, or the gate becomes
	// something people route around instead of fixing.
	tpl := &BodyTemplate{Type: "adr", Sections: adrSections}
	allowed := map[string]string{
		"exactly the template":               "## Context\n## Decision\n## Consequences\n",
		"extra before the first":             "## Notes\n## Context\n## Decision\n## Consequences\n",
		"extra between two":                  "## Context\n## Notes\n## Decision\n## Consequences\n",
		"extra after the last":               "## Context\n## Decision\n## Consequences\n## Notes\n",
		"H3 nested underneath":               "## Context\n### Detail\n## Decision\n## Consequences\n",
		"numbered":                           "## 1. Context\n## 2. Decision\n## 3. Consequences\n",
		"dotted numbering":                   "## 2.1) Context\n## 2.2) Decision\n## 2.3) Consequences\n",
		"heading extends the required one":   "## Context and forces\n## Decision\n## Consequences\n",
		"prose and a fence between sections": "## Context\n\ntext\n\n```py\nx = 1\n```\n\n## Decision\n## Consequences\n",
	}
	for label, body := range allowed {
		if missing, ok := MissingHeading(tpl, body); ok {
			t.Errorf("%s: MissingHeading = %q, want none", label, missing)
		}
	}
}

func TestBodyContractEnforcesPresenceAndOrder(t *testing.T) {
	// The two things that do fail — and the prefix match runs one way only.
	tpl := &BodyTemplate{Type: "adr", Sections: adrSections}
	check := func(body, want string) {
		t.Helper()
		if missing, ok := MissingHeading(tpl, body); !ok || missing != want {
			t.Errorf("MissingHeading(%q) = (%q, %v), want %q", body, missing, ok, want)
		}
	}
	check("## Context\n## Decision\n", "Consequences")
	check("## Decision\n## Context\n## Consequences\n", "Decision")
	// A fenced heading is content, not structure: it can neither satisfy a
	// requirement nor displace one.
	check("## Context\n\n```md\n## Decision\n```\n\n## Consequences\n", "Decision")
	// The body extends the requirement, never the reverse.
	wide := &BodyTemplate{Type: "adr", Sections: []Section{{Heading: "Context and scope"}}}
	if missing, ok := MissingHeading(wide, "## Context\n"); !ok || missing != "Context and scope" {
		t.Errorf("MissingHeading = (%q, %v)", missing, ok)
	}
}

func TestOptionalSectionMayBeAbsent(t *testing.T) {
	// The key that makes every added section safe to ship: a corpus written
	// against an earlier release must not gain a body_shape error the day it
	// upgrades.
	tpl := &BodyTemplate{Type: "adr", Sections: []Section{
		{Heading: "Context"}, {Heading: "Alternatives", Optional: true}, {Heading: "Decision"}}}
	for _, body := range []string{"## Context\n## Decision\n", "## Context\n## Alternatives\n## Decision\n"} {
		if missing, ok := MissingHeading(tpl, body); ok {
			t.Errorf("MissingHeading(%q) = %q, want none", body, missing)
		}
	}
	// Absent still means absent — it must not swallow the section that follows it.
	if missing, ok := MissingHeading(tpl, "## Context\n"); !ok || missing != "Decision" {
		t.Errorf("MissingHeading = (%q, %v)", missing, ok)
	}
	// Order is part of the contract: sections written the wrong way round do
	// not both match, and the one that cannot be reached in order is reported.
	if missing, ok := MissingHeading(tpl, "## Decision\n## Context\n"); !ok || missing != "Decision" {
		t.Errorf("MissingHeading = (%q, %v)", missing, ok)
	}
}

func TestOptionalDeclaredEarlyCannotDisplaceRequired(t *testing.T) {
	// A match advances the cursor whether the section was optional or not,
	// so an optional heading declared BEFORE a required one, matching
	// something further down the document, carries the cursor past it: a
	// section present and correctly named reports missing. arc42 hit this —
	// `Deferred` declared before `Risks` matched the `## Deferred Decisions`
	// appendix at the foot of the document — and the fix was to declare the
	// appendices last.
	body := "## Context and Scope\n## Solution Strategy\n## Building Block View\n" +
		"## Crosscutting Concepts\n## Architecture Decisions\n" +
		"## Risks and Technical Debt\n## Glossary\n## Deferred Decisions\n"
	spine := &BodyTemplate{Type: "arc42", Sections: []Section{
		{Heading: "Introduction and Goals", Optional: true},
		{Heading: "Context and Scope"},
		{Heading: "Solution Strategy"},
		{Heading: "Building Block View"},
		{Heading: "Crosscutting Concepts"},
		{Heading: "Architecture Decisions"},
		{Heading: "Risks and Technical Debt"},
		{Heading: "Glossary", Optional: true},
		{Heading: "Deferred Decisions", Optional: true},
	}}
	if missing, ok := MissingHeading(spine, body); ok {
		t.Errorf("appendices last: MissingHeading = %q, want none", missing)
	}
	// The declaration order that broke it, kept so the hazard stays legible.
	hazard := &BodyTemplate{Type: "arc42", Sections: []Section{
		{Heading: "Deferred", Optional: true}, {Heading: "Risks and Technical Debt"}}}
	if missing, ok := MissingHeading(hazard, body); !ok || missing != "Risks and Technical Debt" {
		t.Errorf("hazard: MissingHeading = (%q, %v)", missing, ok)
	}
}

// --- section rules ---------------------------------------------------------------

func TestAbsentSectionDoesNotCascade(t *testing.T) {
	// A cursor that advanced on a miss would report every later section as
	// missing and judge none of them — one deleted optional heading
	// silencing the whole document.
	yaml := "sections:\n- heading: Context\n- heading: Alternatives\n  optional: true\n" +
		"- heading: Decision\n  word_count: {min: 20}\n"
	body := "## Context\n\nWe had to choose.\n\n## Decision\n\nPostgres.\n"
	wantRules(t, rules(t, yaml, body), []string{"Decision: 1 words of prose, at least 20 asked for"})
}

func TestAbsentOptionalSectionIsNotThin(t *testing.T) {
	// Rules on a section that was never written must stay silent — otherwise
	// declaring one optional and then giving it a minimum contradicts itself.
	yaml := "sections:\n- heading: Context\n- heading: Alternatives\n  optional: true\n  word_count: {min: 20}\n"
	wantRules(t, rules(t, yaml, "## Context\n\nSomething.\n"), []string{})
	wantRules(t, rules(t, yaml, "## Context\n\nSomething.\n\n## Alternatives\n\nOne word.\n"),
		[]string{"Alternatives: 2 words of prose, at least 20 asked for"})
}

func TestEmptySectionIsUnwrittenNotBadlyWritten(t *testing.T) {
	// Every section of a freshly scaffolded document is empty. A minimum
	// firing there would mean `add` handed back a document already in
	// violation, and a rule with that cost is a rule nobody would declare.
	yaml := "sections:\n- heading: Context\n  hint: the forces\n  word_count: {min: 20}\n" +
		"  required_text: [because]\n  code_blocks: [{lang: mermaid, min: 1}]\n"
	wantRules(t, rules(t, yaml, "## Context\n\n<!-- the forces -->\n"), []string{})
	wantRules(t, rules(t, yaml, "## Context\n\nShort.\n"), []string{
		"Context: 1 words of prose, at least 20 asked for",
		"Context: says nothing matching 'because'",
		"Context: 0 mermaid block(s), at least 1 asked for",
	})
	// A diagram with no sentence around it is still written: a section holding
	// nothing but a fenced block is judged, not skipped.
	wantRules(t, rules(t, yaml, "## Context\n\n```py\nx\n```\n"), []string{
		"Context: 0 words of prose, at least 20 asked for",
		"Context: says nothing matching 'because'",
		"Context: 0 mermaid block(s), at least 1 asked for",
	})
}

func TestSectionRulesReadProseAndNothingElse(t *testing.T) {
	// A scaffold hint is the template's words, not the author's, and a phrase
	// inside a code sample is not a claim.
	yaml := "sections:\n- heading: Context\n  word_count: {max: 3}\n" +
		"  required_text: [postgres]\n  forbidden_text: [{pattern: 'robust'}]\n"
	hidden := "## Context\n\n<!-- postgres is robust, and here are many extra words -->\n" +
		"\n```sql\n-- postgres is robust\n```\n\nWe chose it.\n"
	wantRules(t, rules(t, yaml, hidden), []string{"Context: says nothing matching 'postgres'"})
}

func TestTextRulesAreCaseInsensitiveLiteralsOrPatterns(t *testing.T) {
	yaml := "sections:\n- heading: Context\n  required_text: ['We Chose']\n" +
		"  forbidden_text: [{pattern: '\\b(tbd|robust)\\b'}]\n"
	wantRules(t, rules(t, yaml, "## Context\n\nwe chose it.\n"), []string{})
	wantRules(t, rules(t, yaml, "## Context\n\nTBD.\n"), []string{
		"Context: says nothing matching 'We Chose'",
		`Context: uses /\b(tbd|robust)\b/`,
	})
	// A literal is a substring, not a word — `robustness` is not caught by /\brobust\b/.
	wantRules(t, rules(t, yaml, "## Context\n\nWe chose robustness.\n"), []string{})
}

func TestCodeBlockRulesCountByLanguage(t *testing.T) {
	yaml := "sections:\n- heading: Context\n  code_blocks:\n  - {lang: mermaid, min: 1}\n  - {max: 2}\n"
	one := "## Context\n\n```mermaid\ngraph TD;\n```\n"
	wantRules(t, rules(t, yaml, one), []string{})
	// `lang` omitted counts every block whatever its info string.
	wantRules(t, rules(t, yaml, one+"\n```py\nx = 1\n```\n\n```sh\nls\n```\n"),
		[]string{"Context: 3 code block(s), at most 2 asked for"})
	wantRules(t, rules(t, yaml, "## Context\n\n```\ngraph TD;\n```\n"),
		[]string{"Context: 0 mermaid block(s), at least 1 asked for"})
	// An info string carries attributes after the language; only the language
	// counts, case-folded.
	wantRules(t, rules(t, yaml, "## Context\n\n```MerMaid title=x\ngraph TD;\n```\n"), []string{})
}

func TestZeroBoundIsADeclarationNotAnAbsence(t *testing.T) {
	// `word_count: {max: 0}` says this section holds a diagram and nothing
	// else. Read as falsy it would parse, validate, and never be enforced.
	yaml := "sections:\n- heading: Diagram\n  word_count: {max: 0}\n"
	wantRules(t, rules(t, yaml, "## Diagram\n\n```mermaid\nA --> B\n```\n"), []string{})
	got := rules(t, yaml, "## Diagram\n\n```mermaid\nA --> B\n```\n\nand a note\n")
	if len(got) != 1 || !strings.Contains(got[0], "at most 0") {
		t.Errorf("rules = %q", got)
	}
}

func TestCodeBlockBoundIsNeverImplied(t *testing.T) {
	// `{lang: bash, max: 2}` reads as "at most two". Defaulting min to 1
	// turned it into "and at least one", firing on a section that
	// legitimately has none — and made `{max: 0}` unsatisfiable.
	yaml := "sections:\n- heading: Notes\n  code_blocks: [{lang: bash, max: 2}]\n"
	wantRules(t, rules(t, yaml, "## Notes\n\nJust prose, no command to run.\n"), []string{})
	wantRules(t, rules(t, yaml, "## Notes\n\n```bash\na\n```\n\n```bash\nb\n```\n"), []string{})
	got := rules(t, yaml, "## Notes\n\n```bash\na\n```\n\n```bash\nb\n```\n\n```bash\nc\n```\n")
	if len(got) != 1 || !strings.Contains(got[0], "at most 2") {
		t.Errorf("rules = %q", got)
	}
	noneAtAll := "sections:\n- heading: Notes\n  code_blocks: [{max: 0}]\n"
	wantRules(t, rules(t, noneAtAll, "## Notes\n\nProse only, which is the whole rule.\n"), []string{})
	got = rules(t, noneAtAll, "## Notes\n\n```\nx\n```\n")
	if len(got) != 1 || !strings.Contains(got[0], "at most 0") {
		t.Errorf("rules = %q", got)
	}
}

// --- word counts and the search reading ------------------------------------------

func TestWordCountCountsCJKByCharacter(t *testing.T) {
	// Splitting on whitespace scores a Japanese paragraph at about one word
	// per line, which would make every number reported nonsense in exactly
	// the corpora least placed to argue about it.
	cases := map[string]int{
		"日本語のテキスト":        8,
		"two words":       2,
		"mixed 日本 text":   4,
		"":                0,
		"  spaced\tout\n": 2,
		"ｶﾀｶﾅ half":       5, // halfwidth kana, one word each
	}
	for text, want := range cases {
		if got := WordCount(text); got != want {
			t.Errorf("WordCount(%q) = %d, want %d", text, got, want)
		}
	}
}

func TestMetricsTotalIsSectionsPlusPreamble(t *testing.T) {
	// Heading text is stripped from the total too, so a reader can add the
	// section numbers up and get the number they were given.
	body := "intro words\n\n## A heading\n\none two\n\n## B\n\nthree\n"
	got := Metrics(body)
	if got.Words != 5 || !reflect.DeepEqual(got.Sections, []SectionWords{{"A heading", 2}, {"B", 1}}) {
		t.Errorf("Metrics = %+v", got)
	}
	if empty := Metrics(""); empty.Words != 0 || len(empty.Sections) != 0 {
		t.Errorf("Metrics(\"\") = %+v", empty)
	}
}

func TestStripCommentsRemovesNotBlanks(t *testing.T) {
	// The search index's reading: a terminated comment becomes ONE space
	// (removed, not blanked — this text is what a snippet renders back and
	// nothing slices it at an offset), an unterminated tail stays.
	cases := map[string]string{
		"<!-- what it is -->\n\n```bash\ndocker compose up\n```\n": " \n\n```bash\ndocker compose up\n```\n",
		"a <!-- gone --> b":        "a   b",
		"x\n<!-- a\nb -->\ny":      "x\n \ny",
		"<!-- one --><!-- two -->": "  ",
		"kept <!-- open":           "kept <!-- open",
		"plain":                    "plain",
	}
	for in, want := range cases {
		if got := StripComments(in); got != want {
			t.Errorf("StripComments(%q) = %q, want %q", in, got, want)
		}
	}
}
