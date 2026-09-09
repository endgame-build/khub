// Body scanning — masking, headings, word counts and the per-section rules
// over an entity body (kb `_fence_spans`, `mask_fences`, `mask_comments`,
// `prose`, `_h2_spans`, `word_count`, `section_metrics`, `_align`,
// `body_rules`). Everything that reads a body goes through here, and agrees
// on one thing: a fenced block and an HTML comment are not prose.
//
// Masking is byte-length and newline preserving — each masked byte becomes a
// space, `\n` stays — so BodyRules and Metrics can slice the ORIGINAL body by
// the H2Span offsets found in the masked one.

package template

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/text/cases"
)

var (
	// An opening or closing code fence (kb FENCE_RE): up to three spaces of
	// indent, a run of at least three backticks or tildes, then the info
	// string. `.` excludes `\n` in both engines, so a `\r` left by a CRLF line
	// lands in the info group and TrimSpace removes it — as Python's strip().
	fenceRE = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})[ \\t]*(.*)$")
	// An HTML comment (kb COMMENT_RE, DOTALL). A scaffold hint is one, so
	// everything that measures or indexes prose takes them out first, or it
	// scores the instructions rather than the writing.
	commentRE = regexp.MustCompile(`(?s)<!--.*?-->`)
	// An H2 heading line (kb H2_RE): `##` indented up to three spaces, then
	// at least one space or tab, then the heading text.
	h2RE = regexp.MustCompile(`(?m)^ {0,3}##[ \t]+(.*)$`)

	// Python's str.casefold() for fence languages (ß → ss) — strings.ToLower
	// is not full case folding.
	langFold = cases.Fold()
)

// H2Span is one body H2 (numbering stripped) with the byte offsets of its
// section — from the end of the heading line's content to the start of the
// next H2's line, or the end of the body — in the original, unmasked body.
type H2Span struct {
	Heading string
	Start   int
	End     int
}

// SectionWords is one section's prose word count.
type SectionWords struct {
	Heading string
	Words   int
}

// SectionMetrics is the body's prose word count and one entry per section
// (kb section_metrics): validate emits it for a single target.
type SectionMetrics struct {
	Words    int
	Sections []SectionWords
}

// Complaint is one body_rule finding: the section heading and kb's verbatim
// reason text.
type Complaint struct {
	Heading string
	Reason  string
}

// fenceSpan is one fenced block: its first line, one past its last, and the
// info string (trimmed).
type fenceSpan struct {
	start int
	end   int
	info  string
}

// fenceSpans pairs fences the CommonMark way rather than by regex
// backreference (kb _fence_spans): a closing fence repeats the OPENING
// character at least as many times and carries no info string of its own; a
// backtick fence's info string may not itself contain a backtick; an
// unterminated fence runs to the end. The backreference this replaces closed
// a ````-fence on the first inner ```, letting the rest of the block —
// headings included — back into the document it was meant to hide from.
func fenceSpans(lines []string) []fenceSpan {
	var spans []fenceSpan
	opener := ""
	start, info := 0, ""
	for i, line := range lines {
		m := fenceRE.FindStringSubmatch(line)
		if opener == "" {
			if m != nil && (m[1][0] == '~' || !strings.Contains(m[2], "`")) {
				opener, start, info = m[1], i, strings.TrimSpace(m[2])
			}
			continue
		}
		if m != nil && m[1][0] == opener[0] && len(m[1]) >= len(opener) && strings.TrimSpace(m[2]) == "" {
			spans = append(spans, fenceSpan{start, i + 1, info})
			opener = ""
		}
	}
	if opener != "" {
		spans = append(spans, fenceSpan{start, len(lines), info})
	}
	return spans
}

// blank returns len(s) spaces — the masked form of one line.
func blank(s string) string { return strings.Repeat(" ", len(s)) }

// MaskFences blanks every byte inside a fenced code block (fences included),
// keeping length and newlines (kb mask_fences). Blanked and not deleted:
// Metrics slices the body at the offsets H2Spans reports, and cutting the
// fence out would shift every heading after it.
func MaskFences(body string) string {
	lines := strings.Split(body, "\n")
	for _, span := range fenceSpans(lines) {
		for i := span.start; i < span.end; i++ {
			lines[i] = blank(lines[i])
		}
	}
	return strings.Join(lines, "\n")
}

// FencedLangs lists the info-string language of every fenced block, in order
// — `mermaid`, `bash`, or "" for a bare fence (kb fenced_langs). Attributes
// after the language are dropped: only the first word counts.
func FencedLangs(body string) []string {
	spans := fenceSpans(strings.Split(body, "\n"))
	out := make([]string, 0, len(spans))
	for _, span := range spans {
		lang := ""
		if words := strings.Fields(span.info); len(words) > 0 {
			lang = words[0]
		}
		out = append(out, lang)
	}
	return out
}

// blankBytes replaces every byte of buf[from:to] except `\n` with a space.
func blankBytes(buf []byte, from, to int) {
	for i := from; i < to; i++ {
		if buf[i] != '\n' {
			buf[i] = ' '
		}
	}
}

// MaskComments blanks every byte inside an HTML comment, keeping length and
// newlines (kb mask_comments); an unterminated `<!--` masks to the end of the
// document, the way every Markdown renderer treats it — leaving the rest
// visible would mean reading headings a reader cannot see. Newline-preserving
// on top of length-preserving: a comment spanning three lines leaves three
// lines behind, or the line-anchored heading pattern would find headings on
// lines that never existed.
func MaskComments(text string) string {
	buf := []byte(text)
	end := 0
	for _, m := range commentRE.FindAllStringIndex(text, -1) {
		blankBytes(buf, m[0], m[1])
		end = m[1]
	}
	if opened := strings.Index(text[end:], "<!--"); opened != -1 {
		blankBytes(buf, end+opened, len(buf))
	}
	return string(buf)
}

// StripComments removes every terminated HTML comment, each replaced by one
// space (kb `COMMENT_RE.sub(" ", body)`); an unterminated tail is kept. This
// is the search index's reading: removed rather than blanked, because the
// text is what a snippet renders back and nothing slices it at an offset —
// blanking would trade a scoring bug for a display one.
func StripComments(text string) string {
	return commentRE.ReplaceAllLiteralString(text, " ")
}

// Prose is the body with fenced code and HTML comments blanked — what a
// human wrote (kb prose). Blanked, not deleted: every caller either counts
// words (blanks are free) or slices the ORIGINAL text at offsets taken from
// this one.
func Prose(text string) string {
	return MaskComments(MaskFences(text))
}

// H2Spans lists the body's H2 headings, each with the half-open byte offsets
// of the prose beneath it (kb _h2_spans). Headings are found in Prose, not in
// the raw body: a `##` inside a fence is obviously not a heading, and one
// inside an HTML comment is the same claim with worse consequences in both
// directions — a commented-out heading would satisfy body_shape for a section
// the document no longer has, and a stray one inside a scaffold hint would
// steal the span of the real heading above it.
func H2Spans(body string) []H2Span {
	// The heading text is read from the masked prose too (kb takes
	// `m.group(1)` of `prose(body)`): a comment on the heading line —
	// `## Context <!-- one paragraph -->` — is not part of the heading, and
	// slicing the raw body there would make `Context` fail to align. Masking
	// is length-preserving, so the offsets are the same in both.
	masked := Prose(body)
	hits := h2RE.FindAllStringSubmatchIndex(masked, -1)
	out := make([]H2Span, 0, len(hits))
	for i, m := range hits {
		end := len(body)
		if i+1 < len(hits) {
			end = hits[i+1][0]
		}
		text := strings.TrimSpace(masked[m[2]:m[3]])
		out = append(out, H2Span{
			Heading: numberingRE.ReplaceAllString(text, ""),
			Start:   m[1],
			End:     end,
		})
	}
	return out
}

// BodyH2s is kb body_h2s: the body's H2 headings in order, numbering
// stripped, fenced code and comments ignored.
func BodyH2s(body string) []string {
	spans := H2Spans(body)
	out := make([]string, 0, len(spans))
	for _, span := range spans {
		out = append(out, span.Heading)
	}
	return out
}

// isCJK reports the ranges wide enough to matter (kb CJK_RE): kana, the two
// common Han blocks, the compatibility ideographs, and halfwidth kana.
func isCJK(r rune) bool {
	switch {
	case r >= 0x3040 && r <= 0x30FF,
		r >= 0x3400 && r <= 0x4DBF,
		r >= 0x4E00 && r <= 0x9FFF,
		r >= 0xF900 && r <= 0xFAFF,
		r >= 0xFF66 && r <= 0xFF9F:
		return true
	}
	return false
}

// WordCount counts whitespace-separated words, each CJK character counting
// as one (kb word_count). Splitting on whitespace alone scores a Chinese or
// Japanese paragraph at roughly one word per line, which would make any
// number read off these metrics nonsense in exactly the corpora least placed
// to argue about it.
func WordCount(text string) int {
	cjk := 0
	rest := strings.Map(func(r rune) rune {
		if isCJK(r) {
			cjk++
			return ' '
		}
		return r
	}, text)
	return cjk + len(strings.Fields(rest))
}

// Metrics is kb section_metrics: prose word counts for the body and each of
// its sections. Heading text is stripped from the total too, so Words is the
// sum of the sections plus whatever prose sits above the first one, and a
// reader can add the numbers up and get the number they were given.
func Metrics(body string) SectionMetrics {
	spans := H2Spans(body)
	sections := make([]SectionWords, 0, len(spans))
	for _, span := range spans {
		sections = append(sections, SectionWords{
			Heading: span.Heading,
			Words:   WordCount(Prose(body[span.Start:span.End])),
		})
	}
	return SectionMetrics{
		Words:    WordCount(Prose(h2RE.ReplaceAllString(body, ""))),
		Sections: sections,
	}
}

// align matches each declared heading to the body prose beneath it (kb
// _align): spans is index-aligned with t.Sections, nil where the section is
// absent; missing names the first absent required heading (ok=true).
//
// The contract is an ordered subsequence and nothing more. Every declared
// heading must appear, in declared order, as a prefix of some body H2.
// Deliberately permissive everywhere else: extra H2s pass anywhere, anything
// deeper than H2 is invisible, a declared `Context` is satisfied by
// `## Context and scope` (not the reverse), leading numbering is stripped,
// and an H2 inside a fence or comment satisfies nothing and displaces
// nothing. A section declared optional that is absent fails nothing and has
// no span, so none of its rules are evaluated — "not written" never reads as
// "written badly". An unmatched heading does not advance the cursor, so one
// absent section cannot cascade into every section after it also reporting
// missing.
func align(t *BodyTemplate, body string) (spans []*H2Span, missing string, ok bool) {
	found := H2Spans(body)
	spans = make([]*H2Span, len(t.Sections))
	pos := 0
	for i, s := range t.Sections {
		k := slices.IndexFunc(found[pos:], func(h H2Span) bool {
			return strings.HasPrefix(h.Heading, s.Heading)
		})
		if k < 0 {
			if !s.Optional && !ok {
				missing, ok = s.Heading, true
			}
			continue
		}
		spans[i] = &found[pos+k]
		pos += k + 1
	}
	return spans, missing, ok
}

// MissingHeading is kb missing_heading: the first required template heading
// not found in order in body. ok=true carries the missing heading; ok=false
// is Python's None (the contract is satisfied).
func MissingHeading(t *BodyTemplate, body string) (string, bool) {
	_, missing, ok := align(t, body)
	return missing, ok
}

// BodyRules is kb body_rules: one Complaint per section rule body does not
// satisfy, in template order. All of these are gaps, never errors: a missing
// heading means the document does not have the shape it claims; thin prose
// under a heading that is there means it is unfinished, and unfinished is
// legal.
func BodyRules(t *BodyTemplate, body string) []Complaint {
	spans, _, _ := align(t, body)
	var out []Complaint
	for i := range t.Sections {
		s := &t.Sections[i]
		if !s.HasRules() || spans[i] == nil {
			continue
		}
		chunk := body[spans[i].Start:spans[i].End]
		// Rules read prose: an author's hint comment must not satisfy
		// required_text, and a forbidden_text hit inside a code sample is a
		// false positive.
		text := Prose(chunk)
		langs := FencedLangs(chunk)
		for j, lang := range langs {
			langs[j] = langFold.String(lang)
		}
		// An empty section is unwritten, not badly written, and every section
		// of every freshly scaffolded document is empty. Firing here would
		// mean `add` hands back a document that is already failing. A diagram
		// with no sentence around it is still written, though — a section
		// holding nothing but a fenced block is judged, not skipped.
		if strings.TrimSpace(text) == "" && len(langs) == 0 {
			continue
		}
		words := WordCount(text)
		if s.MinWords != nil && words < *s.MinWords {
			out = append(out, Complaint{s.Heading,
				fmt.Sprintf("%d words of prose, at least %d asked for", words, *s.MinWords)})
		}
		if s.MaxWords != nil && words > *s.MaxWords {
			out = append(out, Complaint{s.Heading,
				fmt.Sprintf("%d words of prose, at most %d asked for", words, *s.MaxWords)})
		}
		for _, rule := range s.RequiredText {
			if !rule.FoundIn(text) {
				out = append(out, Complaint{s.Heading, fmt.Sprintf("says nothing matching %s", rule)})
			}
		}
		for _, rule := range s.ForbiddenText {
			if rule.FoundIn(text) {
				out = append(out, Complaint{s.Heading, fmt.Sprintf("uses %s", rule)})
			}
		}
		for _, rule := range s.CodeBlocks {
			n := len(langs)
			if rule.Lang != "" {
				want := langFold.String(rule.Lang)
				n = 0
				for _, lang := range langs {
					if lang == want {
						n++
					}
				}
			}
			if rule.Min != nil && n < *rule.Min {
				out = append(out, Complaint{s.Heading,
					fmt.Sprintf("%d %s, at least %d asked for", n, rule, *rule.Min)})
			} else if rule.Max != nil && n > *rule.Max {
				out = append(out, Complaint{s.Heading,
					fmt.Sprintf("%d %s, at most %d asked for", n, rule, *rule.Max)})
			}
		}
	}
	return out
}
