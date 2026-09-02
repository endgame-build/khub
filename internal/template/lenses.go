// Lenses — per-template review prompts that apply when a frontmatter
// predicate holds (kb `_load_lenses`, `_lens_when`, `_lens_after`,
// `_order_lenses`, `Lens.applies`).
//
// khub validates, filters and orders the registry and prints it beside the
// body metrics; it never executes an `instruction` and never turns one into a
// finding. A lens is a prompt for whoever is reviewing the body, human or
// agent — the judgement it asks for is the part a reader has to be able to
// disagree with.
package template

import (
	"fmt"
	"math/big"
	"slices"
	"strings"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/omap"
)

var lensKeys = map[string]bool{
	"code": true, "name": true, "instruction": true,
	"when": true, "after": true, "section": true,
}

// WhenClause is one `when` predicate: the frontmatter field, and the values
// any one of which makes the clause hold — the template's loaded values as
// typed (bool, int64, float64, canon.BigInt, canon.Date, canon.DateTime,
// string, nil), compared by sameScalar.
type WhenClause struct {
	Field  string
	Values []any
}

// Lens is one declared lens. After names the codes this lens is ordered
// behind; Section is the declared heading it targets, "" for the whole body.
type Lens struct {
	Code        string
	Name        string
	Instruction string
	When        []WhenClause
	After       []string
	Section     string
}

// LensView is the applicable-lens record validate emits, fields in emitted
// key order.
type LensView struct {
	Code        string
	Name        string
	Section     string
	Instruction string
}

// Applies reports whether this lens applies to an entity with front as its
// frontmatter (kb Lens.applies). front is expected to carry schema defaults
// already (integrity's lensFront). Values meet typed, through sameScalar: a
// field neither written nor defaulted is nil, which only a `when: null`
// equals, so the lens simply does not apply.
func (l *Lens) Applies(front *omap.Map) bool {
	for _, clause := range l.When {
		v, _ := front.Get(clause.Field)
		if !slices.ContainsFunc(clause.Values, func(want any) bool { return sameScalar(v, want) }) {
			return false
		}
	}
	return true
}

// ApplicableLenses lists the lenses whose every When clause holds against
// front, in the template's (`after`-resolved) order — kb `_lens_report`.
func (t *BodyTemplate) ApplicableLenses(front *omap.Map) []LensView {
	out := []LensView{}
	for i := range t.Lenses {
		lens := &t.Lenses[i]
		if lens.Applies(front) {
			out = append(out, LensView{
				Code: lens.Code, Name: lens.Name, Section: lens.Section, Instruction: lens.Instruction,
			})
		}
	}
	return out
}

// loadLenses is kb `_load_lenses`: the `lenses:` registry, validated and
// ordered by its own `after` edges. headings is the set of declared section
// headings a `section` may name; fields is what the schema declares for the
// owning type (attributes and relations), the set a `when` field must be in.
func loadLenses(stem string, v any, headings map[string]bool, fields []string) ([]Lens, error) {
	if v == nil {
		return nil, nil
	}
	list, isList := v.([]any)
	if !isList {
		return nil, invalid(stem, "'lenses' must be a list")
	}
	lenses := make([]Lens, 0, len(list))
	seen := map[string]bool{}
	for i, entryAny := range list {
		spot := fmt.Sprintf("lenses[%d]", i)
		entry, isMap := entryAny.(*omap.Map)
		if !isMap {
			return nil, invalid(stem, spot+" must be a mapping with 'code' and 'instruction'")
		}
		if unknown := keysOutside(entry, lensKeys); len(unknown) > 0 {
			return nil, invalid(stem, spot+" unknown key(s): "+strings.Join(unknown, ", "))
		}
		codeAny, _ := entry.Get("code")
		code, isStr := codeAny.(string)
		if !isStr || strings.TrimSpace(code) == "" {
			return nil, invalid(stem, spot+" needs a non-empty string 'code'")
		}
		code = strings.TrimSpace(code)
		if seen[code] {
			return nil, invalid(stem, spot+" repeats the lens code "+pyRepr(code))
		}
		seen[code] = true
		instructionAny, _ := entry.Get("instruction")
		instruction, isStr := instructionAny.(string)
		if !isStr || strings.TrimSpace(instruction) == "" {
			return nil, invalid(stem, spot+" needs a non-empty string 'instruction'")
		}
		// `entry.get("name", code)`: an absent key means the code; a present
		// non-string (null included) is refused.
		name := code
		if nameAny, has := entry.Get("name"); has {
			s, ok := nameAny.(string)
			if !ok {
				return nil, invalid(stem, spot+" 'name' must be text")
			}
			if name = strings.TrimSpace(s); name == "" {
				name = code
			}
		}
		section := ""
		if sectionAny, _ := entry.Get("section"); sectionAny != nil {
			s, ok := sectionAny.(string)
			if !ok {
				return nil, invalid(stem, spot+" 'section' must be text")
			}
			// A lens pinned to a heading no template declares can never be
			// shown, and the typo that caused it is invisible at every later
			// stage.
			section = strings.TrimSpace(s)
			if !headings[section] {
				return nil, invalid(stem, fmt.Sprintf(
					"%s 'section' names %s, which this template does not declare", spot, pyRepr(section)))
			}
		}
		whenAny, _ := entry.Get("when")
		when, err := lensWhen(stem, spot, whenAny, fields)
		if err != nil {
			return nil, err
		}
		afterAny, _ := entry.Get("after")
		after, err := lensAfter(stem, spot, afterAny)
		if err != nil {
			return nil, err
		}
		lenses = append(lenses, Lens{
			Code:        code,
			Name:        name,
			Instruction: strings.Trim(instruction, "\n"),
			When:        when,
			After:       after,
			Section:     section,
		})
	}
	return orderLenses(stem, lenses)
}

// lensWhen is kb `_lens_when`: `when: {kind: non-functional}` — the
// frontmatter this lens applies to. Values are kept as the loader typed them
// and compared typed (sameScalar), so `draft: false` in a lens matches
// `draft: false` in a body, and does not match a body whose file spells the
// quoted string "false".
//
// A field name is checked against the schema for the same reason `section` is
// checked against the declared headings: `when: {stat: proposed}` reads an
// absent field, which is nil, which no declared value equals — so the lens
// applies to nobody for the life of the template, and no later stage has
// anything to report. A nil fields set is a type declaring nothing, and
// refuses every `when` field the same way. Clauses come out sorted by field,
// as kb's do.
func lensWhen(stem, spot string, v any, fields []string) ([]WhenClause, error) {
	if v == nil {
		return nil, nil
	}
	m, isMap := v.(*omap.Map)
	if !isMap || m.Len() == 0 {
		return nil, invalid(stem, spot+" 'when' must be a non-empty field: value mapping")
	}
	keys := slices.Sorted(slices.Values(m.Keys()))
	out := make([]WhenClause, 0, len(keys))
	for _, field := range keys {
		if !slices.Contains(fields, field) {
			return nil, invalid(stem, fmt.Sprintf(
				"%s 'when' reads %s, which this type does not declare", spot, pyRepr(field)))
		}
		wanted, _ := m.Get(field)
		allowed, isList := wanted.([]any)
		if !isList {
			allowed = []any{wanted}
		}
		if len(allowed) == 0 {
			return nil, invalid(stem, fmt.Sprintf("%s 'when.%s' lists no value", spot, field))
		}
		out = append(out, WhenClause{Field: field, Values: slices.Clone(allowed)})
	}
	return out, nil
}

// lensAfter is kb `_lens_after`: one code or a list of them, each non-blank.
func lensAfter(stem, spot string, v any) ([]string, error) {
	if v == nil {
		return nil, nil
	}
	codes, isList := v.([]any)
	if !isList {
		codes = []any{v}
	}
	out := make([]string, 0, len(codes))
	for _, c := range codes {
		s, isStr := c.(string)
		if !isStr || strings.TrimSpace(s) == "" {
			return nil, invalid(stem, spot+" 'after' must name lens codes")
		}
		out = append(out, strings.TrimSpace(s))
	}
	return out, nil
}

// orderLenses is kb `_order_lenses`: declaration order, adjusted so every
// `after` edge is satisfied.
//
// A stable topological sort rather than a plain one: two lenses with no
// ordering between them come out in the order they were written, so editing a
// registry moves only what the edit meant to move. Kahn's algorithm, emitting
// ONE lens per round — the earliest-declared of those whose edges are
// satisfied. Draining the whole ready set each round would sort by depth
// instead: `a`, `b (after a)`, `c` would come out `a, c, b`, because `c` is
// ready in round one and `b` is not. Nothing asked for `c` to move.
func orderLenses(stem string, lenses []Lens) ([]Lens, error) {
	codes := map[string]bool{}
	for _, lens := range lenses {
		codes[lens.Code] = true
	}
	for _, lens := range lenses {
		for _, code := range lens.After {
			if !codes[code] {
				return nil, invalid(stem, fmt.Sprintf(
					"lens %s follows %s, which this template does not declare", pyRepr(lens.Code), pyRepr(code)))
			}
		}
	}
	pending := slices.Clone(lenses)
	done := map[string]bool{}
	out := make([]Lens, 0, len(lenses))
	for len(pending) > 0 {
		first := -1
		for i, lens := range pending { // pending keeps declaration order, so the first ready one is the earliest declared
			ready := true
			for _, code := range lens.After {
				if !done[code] {
					ready = false
					break
				}
			}
			if ready {
				first = i
				break
			}
		}
		if first < 0 {
			stuck := make([]string, 0, len(pending))
			for _, lens := range pending {
				stuck = append(stuck, lens.Code)
			}
			slices.Sort(stuck)
			return nil, invalid(stem, "lens 'after' edges form a cycle: "+strings.Join(stuck, ", "))
		}
		out = append(out, pending[first])
		done[pending[first].Code] = true
		pending = slices.Delete(pending, first, first+1)
	}
	return out, nil
}

// sameScalar is the comparison a `when` value and a frontmatter value meet
// under: typed, not through the text either is written to disk as. Both nil;
// both bool and equal; both numeric (int, int64, float64, canon.BigInt) and
// equal as numbers, so a schema default's Go int meets a document's int64
// and `12` meets `12.0`; a date or datetime by its ISO text, including
// against a string spelling the same ISO text; strings exact. Anything else
// — a list, a mapping, a bool against its spelling — is false, and a missing
// field (nil) never equals a value.
//
// kb compared the two sides as `emit_scalar` rendered them, which is what the
// bool and date rules above reproduce. The one divergence: kb's `when: {n: 12}`
// matched a body whose file carried the quoted string "12"; this does not,
// because a second copy of the emitter's quoting rules is not a comparison.
func sameScalar(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if x, ok := a.(bool); ok {
		y, ok := b.(bool)
		return ok && x == y
	}
	if x, ok := numberOf(a); ok {
		y, ok := numberOf(b)
		return ok && x.Cmp(y) == 0
	}
	if x, ok := textOf(a); ok {
		y, ok := textOf(b)
		return ok && x == y
	}
	return false
}

// numberOf is the exact rational a numeric scalar denotes, false for a
// non-number and for a float with no finite value.
func numberOf(v any) (*big.Rat, bool) {
	switch x := v.(type) {
	case int:
		// Schema defaults arrive as Go ints where a loaded document's arrive
		// as int64 (the same split `bound` handles).
		return new(big.Rat).SetInt64(int64(x)), true
	case int64:
		return new(big.Rat).SetInt64(x), true
	case float64:
		r := new(big.Rat).SetFloat64(x)
		return r, r != nil
	case canon.BigInt:
		return new(big.Rat).SetString(x.Literal)
	}
	return nil, false
}

// textOf is the text a string, date or datetime compares by: the string
// itself, or the ISO form.
func textOf(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case canon.Date:
		return x.ISO, true
	case canon.DateTime:
		return x.ISO, true
	}
	return "", false
}

// pyRepr is Python's repr() of a str as kb's `{code!r}` messages render it:
// single-quoted, double-quoted only when the text holds a single quote and no
// double quote. Backslashes and the quote in use are escaped.
func pyRepr(s string) string {
	q := "'"
	if strings.Contains(s, "'") && !strings.Contains(s, "\"") {
		q = "\""
	}
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, q, "\\"+q)
	return q + s + q
}
