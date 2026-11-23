// The opt-in mention audit behind `check --mentions`.
//
// A relation is the only thing that feeds the graph; a name written in body
// prose is navigational only. That rule stays. This pass makes the gap it
// leaves visible: an entity whose body names another entity, with no edge
// between the two in either direction, is reported as a suggestion — the
// predicates the mentioning type could use to link it. Nothing is written,
// and the suggestions never affect `passed`.

package integrity

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/endgame-build/khub/internal/graph"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/template"
	"github.com/endgame-build/khub/internal/values"
)

// Mention is one suggested edge: the body of Type/Slug names Target as
// Text, the two share no edge, and Predicates are the relations of the
// mentioning type that could carry it.
type Mention struct {
	Type       string
	Slug       string
	Target     string // qualified id
	Text       string // the words as the body spells them
	Predicates []string
}

// ID is the mentioning entity's qualified id.
func (m Mention) ID() string { return m.Type + "/" + m.Slug }

// minMentionRunes is the shortest name the audit matches. A one- or
// two-letter name ("QA", "UI") matches ordinary prose far more often than it
// names the entity.
const minMentionRunes = 3

// mentionTerm is one name an entity answers to, keyed for lookup by its
// first word, casefolded.
type mentionTerm struct {
	text   string
	target index.Node
}

// isWordRune is what a name is made of. A hyphen joins words, so a slug
// (`dana-lee`) is one word and a name never matches the middle of a
// hyphenated compound. An underscore does not: it is Markdown emphasis
// (`_Initech_`, `__Sam Roe__`) far more often than part of a name.
func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-'
}

// foldRune is r's case-insensitive key: the smallest rune of its simple
// case-folding orbit, so two runes strings.EqualFold calls equal share a key.
// Simple folding maps one rune to one rune; a full folding (`ß` to `ss`)
// and Unicode normalization (NFD vs NFC) are deliberately out of scope.
func foldRune(r rune) rune {
	lowest := r
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		lowest = min(lowest, f)
	}
	return lowest
}

// foldKey is s with every rune replaced by its foldRune.
func foldKey(s string) string {
	return strings.Map(foldRune, s)
}

// matchAt matches term against text at byte offset i: rune by rune under
// foldRune, a run of whitespace in the term against a run of at least one
// whitespace rune in the text, so a name the author wrapped across a line
// still matches. It returns the end offset in text.
func matchAt(text string, i int, term string) (int, bool) {
	ti := 0
	for ti < len(term) {
		tr, tn := utf8.DecodeRuneInString(term[ti:])
		if unicode.IsSpace(tr) {
			for ti < len(term) {
				r, n := utf8.DecodeRuneInString(term[ti:])
				if !unicode.IsSpace(r) {
					break
				}
				ti += n
			}
			start := i
			for i < len(text) {
				r, n := utf8.DecodeRuneInString(text[i:])
				if !unicode.IsSpace(r) {
					break
				}
				i += n
			}
			if i == start {
				return 0, false
			}
			continue
		}
		if i >= len(text) {
			return 0, false
		}
		br, bn := utf8.DecodeRuneInString(text[i:])
		if br != tr && foldRune(br) != foldRune(tr) {
			return 0, false
		}
		ti += tn
		i += bn
	}
	return i, true
}

// mentionTermsOf is every name the node answers to: `title`, `name`, each
// alias and the slug, trimmed of surrounding punctuation, deduplicated
// case-insensitively, in that order. A name shorter than minMentionRunes, or
// holding no letter, is dropped.
func mentionTermsOf(node index.Node, meta *omap.Map) []string {
	var raw []string
	for _, key := range []string{"title", "name"} {
		if v, _ := meta.Get(key); values.Truthy(v) {
			raw = append(raw, values.Str(v))
		}
	}
	raw = append(raw, index.AliasesOf(meta)...)
	raw = append(raw, node.Slug)
	seen := map[string]bool{}
	var out []string
	for _, t := range raw {
		t = strings.TrimFunc(t, func(r rune) bool { return !isWordRune(r) })
		if utf8.RuneCountInString(t) < minMentionRunes || !strings.ContainsFunc(t, unicode.IsLetter) {
			continue
		}
		k := foldKey(t)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, t)
	}
	return out
}

// firstWord is the leading run of word runes, under foldKey.
func firstWord(s string) string {
	end := strings.IndexFunc(s, func(r rune) bool { return !isWordRune(r) })
	if end < 0 {
		end = len(s)
	}
	return foldKey(s[:end])
}

// mentionSpan is one match of a term in a body.
type mentionSpan struct {
	start, end int
	target     index.Node
}

// findMentions returns the matches in body, left to right. A match starts and
// ends on a word boundary and compares case-insensitively (see matchAt).
// Where matches overlap, the leftmost wins, and among those starting at the
// same word the longest: "Initech POV" names the opportunity, not the client
// called "Initech", and in "Big Initech POV" a name "Big Initech" wins over
// "Initech POV". A name two entities share matches both.
func findMentions(body string, terms map[string][]mentionTerm) []mentionSpan {
	var all []mentionSpan
	prevWord := false
	for i, r := range body {
		word := isWordRune(r)
		if word && !prevWord {
			for _, t := range terms[firstWord(body[i:])] {
				end, ok := matchAt(body, i, t.text)
				if !ok {
					continue
				}
				if next, _ := utf8.DecodeRuneInString(body[end:]); end < len(body) && isWordRune(next) {
					continue
				}
				all = append(all, mentionSpan{i, end, t.target})
			}
		}
		prevWord = word
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].start != all[j].start {
			return all[i].start < all[j].start
		}
		return all[i].end > all[j].end
	})
	var out []mentionSpan
	cursor := 0
	var last mentionSpan
	for _, m := range all {
		switch {
		case len(out) > 0 && m.start == last.start && m.end == last.end:
			out = append(out, m)
		case m.start >= cursor:
			out = append(out, m)
			last, cursor = m, m.end
		}
	}
	return out
}

// mentionPredicates is the relations of rtype that could link to the target
// type: those naming it first, then the `to: any` ones, each in declaration
// order. A single-valued relation the entity already fills is left out —
// linking it would replace an edge, not add one.
func mentionPredicates(rtype *schema.ResolvedType, meta *omap.Map, target string) []string {
	var named, anyKind []string
	for _, predicate := range rtype.Relations.Keys() {
		rel, _ := rtype.Relations.Get(predicate)
		if !rel.Many && present(metaGet(meta, predicate)) {
			continue
		}
		if rel.Kind == schema.KindAny {
			anyKind = append(anyKind, predicate)
			continue
		}
		for _, t := range rel.Targets {
			if t == target {
				named = append(named, predicate)
				break
			}
		}
	}
	return append(named, anyKind...)
}

// mentionSuggestions is the audit: for every entity with a body, each other
// entity the body names that shares no edge with it, and that some relation
// of the mentioning type could link. Rows are in (type, slug) order of the
// mentioning entity, then target id order; each pair is one row, carrying the
// first spelling the body used, its whitespace collapsed. idx must carry
// bodies; templates is the per-type contract bodyFindings loaded.
//
// Only prose is read: fenced code and HTML comments (a template's hint
// comments among them) are blanked, and so is each heading text the type's
// template declares — scaffolding `add` seeded, which names nothing. Drafts
// are read on both sides: linking to or from a draft is legal, and a draft is
// exactly the entity still being wired in.
func mentionSuggestions(
	resolved *schema.ResolvedSchema, idx *index.Index, g *graph.Graph, nodes []index.Node,
	templates map[string]*template.BodyTemplate,
) []Mention {
	terms := map[string][]mentionTerm{}
	for _, n := range nodes {
		for _, t := range mentionTermsOf(n, idx.Meta[n]) {
			k := firstWord(t)
			terms[k] = append(terms[k], mentionTerm{text: t, target: n})
		}
	}
	out := []Mention{}
	for _, source := range nodes {
		body := idx.Body[source]
		if body == "" {
			continue
		}
		rtype, _ := resolved.Types.Get(source.Type)
		prose := template.MaskTemplateHeadings(templates[source.Type], template.Prose(body))
		first := map[index.Node]string{}
		var targets []index.Node
		for _, m := range findMentions(prose, terms) {
			if m.target == source || g.Linked(source, m.target) {
				continue
			}
			if _, seen := first[m.target]; !seen {
				first[m.target] = strings.Join(strings.Fields(prose[m.start:m.end]), " ")
				targets = append(targets, m.target)
			}
		}
		index.SortNodes(targets)
		for _, t := range targets {
			predicates := mentionPredicates(rtype, idx.Meta[source], t.Type)
			if len(predicates) == 0 {
				continue
			}
			out = append(out, Mention{
				Type: source.Type, Slug: source.Slug,
				Target: t.ID(), Text: first[t], Predicates: predicates,
			})
		}
	}
	return out
}
