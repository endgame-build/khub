// Package template ports src/khub/core/template.py — body templates (TPL-001).
// A template is a per-type YAML file at .khub/templates/<type>.yaml; its
// presence makes add/init seed new bodies from it and makes validate require
// its section headings in every instance body as an ordered subsequence
// (prefix-match, numbering stripped, extra headings allowed).
//
// The Python _FENCE regex uses a backreference (template.py:45), which RE2
// cannot compile; fenceMask below is the equivalent hand-rolled line scanner.
package template

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/omap"
)

// TemplatesDir is the workspace-relative template directory (TEMPLATES_DIR).
const TemplatesDir = ".khub/templates"

var (
	entryKeys    = map[string]bool{"heading": true, "hint": true, "text": true}
	reservedKeys = map[string]bool{
		"optional": true, "repeat": true, "pattern": true,
		"min_tokens": true, "max_tokens": true, "budget": true,
	}
	topKeys = map[string]bool{"title": true, "sections": true}

	// Leading list numbering on a heading ("1.", "3)", "2.1"), stripped before
	// matching (_NUMBERING; RE2-safe, ported as-is — Go \d/\s are ASCII where
	// Python's are Unicode-wide, an authored-markdown non-difference).
	numberingRE = regexp.MustCompile(`^\d+([.)]\d*)*[.)]?\s+`)
)

// Section is one template entry: the required heading plus its scaffold
// content. Hint and Text hold "" where Python holds None or any other falsy
// value — render() treats them identically.
type Section struct {
	Heading string
	Hint    string
	Text    string
}

// BodyTemplate is a parsed body template: scaffold source and section
// contract in one. Title is "" when the template declares none.
type BodyTemplate struct {
	Type     string
	Title    string
	Sections []Section
}

// Render is BodyTemplate.render: the scaffolded body — each heading, then its
// hint comment / text, blocks separated by blank lines.
func (t *BodyTemplate) Render() string {
	var parts []string
	for _, s := range t.Sections {
		parts = append(parts, "## "+s.Heading+"\n")
		if s.Hint != "" {
			parts = append(parts, "<!-- "+s.Hint+" -->\n")
		}
		if s.Text != "" {
			parts = append(parts, strings.TrimRight(s.Text, "\n")+"\n")
		}
	}
	return strings.Join(parts, "\n")
}

// RequiredHeadings lists the section headings in template order.
func (t *BodyTemplate) RequiredHeadings() []string {
	out := make([]string, len(t.Sections))
	for i, s := range t.Sections {
		out[i] = s.Heading
	}
	return out
}

// TemplatePath is template.template_path.
func TemplatePath(root, typeName string) string {
	return filepath.Join(root, filepath.FromSlash(TemplatesDir), typeName+".yaml")
}

// LoadTemplate is template.load_template: the type's template, or (nil, nil)
// when there is no body contract (no template file; a file declaring
// `sections: []` still returns a template so add/init keep seeding). A
// missing sections key is an error: the file declares nothing coherent.
func LoadTemplate(root, typeName string) (*BodyTemplate, error) {
	p := TemplatePath(root, typeName)
	fi, err := os.Stat(p)
	if err != nil || !fi.Mode().IsRegular() {
		return nil, nil
	}
	// canon.ReadText, not os.ReadFile: a template authored with CRLF would
	// otherwise carry \r into every body rendered from it, exactly as
	// --body-file did. Python read templates with Path.read_text(), which
	// normalizes.
	raw, err := canon.ReadText(p)
	if err != nil {
		return nil, err
	}
	v, err := canon.LoadDoc(raw)
	if err != nil {
		return nil, err // a raw YAML parse error, like Python's — validate collects it
	}
	data, ok := topLevelMap(v)
	if !ok {
		return nil, invalid(typeName, "top level must be a mapping with a 'sections' list")
	}
	if unknown := keysOutside(data, topKeys); len(unknown) > 0 {
		return nil, invalid(typeName, "unknown top-level keys: "+strings.Join(unknown, ", "))
	}
	rawSections, present := data.Get("sections")
	if !present {
		return nil, invalid(typeName, "'sections' is required (use `sections: []` for no body contract)")
	}
	list, isList := rawSections.([]any)
	if !isList {
		return nil, invalid(typeName, "'sections' must be a list")
	}
	titleAny, _ := data.Get("title")
	title := scalarText(titleAny)
	if len(list) == 0 {
		// Declared, deliberately empty: no headings required — but still a
		// template, so add seeds the title and init still creates the singleton.
		return &BodyTemplate{Type: typeName, Title: title}, nil
	}
	sections := make([]Section, 0, len(list))
	for i, entryAny := range list {
		entry, isMap := entryAny.(*omap.Map)
		if !isMap {
			return nil, invalid(typeName, fmt.Sprintf("sections[%d] must be a mapping with a 'heading'", i))
		}
		if reserved := keysInside(entry, reservedKeys); len(reserved) > 0 {
			return nil, invalid(typeName, fmt.Sprintf(
				"sections[%d] uses reserved key(s) %s — planned for a later version, not supported yet",
				i, strings.Join(reserved, ", ")))
		}
		if unknown := keysOutside(entry, entryKeys); len(unknown) > 0 {
			return nil, invalid(typeName, fmt.Sprintf(
				"sections[%d] unknown key(s): %s", i, strings.Join(unknown, ", ")))
		}
		headingAny, _ := entry.Get("heading")
		heading, isStr := headingAny.(string)
		if !isStr || heading == "" {
			return nil, invalid(typeName, fmt.Sprintf("sections[%d] needs a non-empty string 'heading'", i))
		}
		hintAny, _ := entry.Get("hint")
		textAny, _ := entry.Get("text")
		sections = append(sections, Section{
			Heading: strings.TrimSpace(heading),
			Hint:    scalarText(hintAny),
			Text:    scalarText(textAny),
		})
	}
	return &BodyTemplate{Type: typeName, Title: title, Sections: sections}, nil
}

// BodyH2s is template.body_h2s: the body's H2 headings, in order, numbering
// stripped; fenced code ignored.
func BodyH2s(body string) []string {
	lines := strings.Split(body, "\n")
	fenced := fenceMask(lines)
	out := []string{}
	for i, line := range lines {
		if fenced[i] {
			continue
		}
		h, ok := h2Heading(line)
		if !ok {
			continue
		}
		out = append(out, numberingRE.ReplaceAllString(strings.TrimSpace(h), ""))
	}
	return out
}

// MissingHeading is template.missing_heading: the first template heading not
// found in order in body. ok=true carries a missing heading; ok=false is
// Python's None (the contract is satisfied). Every template heading must
// appear, in template order, as a prefix of some body H2 (numbering
// stripped); extra body headings are allowed anywhere.
func MissingHeading(t *BodyTemplate, body string) (string, bool) {
	found := BodyH2s(body)
	pos := 0
	for _, required := range t.RequiredHeadings() {
		for pos < len(found) && !strings.HasPrefix(found[pos], required) {
			pos++
		}
		if pos == len(found) {
			return required, true
		}
		pos++
	}
	return "", false
}

// fenceMask replaces the Python _FENCE regex: it marks every line belonging
// to a closed fenced code block. A fence opens on a line starting with ```
// or ~~~ (info string allowed); it closes on the nearest later line that is
// exactly the same three-char marker followed only by non-newline whitespace.
// An opening with no matching close fences nothing — its lines stay content,
// and later markers are re-examined as openings, matching the regex's
// backtracking exactly (a ## line inside a closed fence is content, never
// structure).
func fenceMask(lines []string) []bool {
	mask := make([]bool, len(lines))
	i := 0
	for i < len(lines) {
		marker, open := fenceOpen(lines[i])
		if !open {
			i++
			continue
		}
		closedAt := -1
		for j := i + 1; j < len(lines); j++ {
			if fenceClose(lines[j], marker) {
				closedAt = j
				break
			}
		}
		if closedAt < 0 {
			i++
			continue
		}
		for k := i; k <= closedAt; k++ {
			mask[k] = true
		}
		i = closedAt + 1
	}
	return mask
}

func fenceOpen(line string) (string, bool) {
	if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
		return line[:3], true
	}
	return "", false
}

func fenceClose(line, marker string) bool {
	if !strings.HasPrefix(line, marker) {
		return false
	}
	for _, r := range line[len(marker):] {
		if !unicode.IsSpace(r) { // [^\S\n]* — \n cannot occur inside a split line
			return false
		}
	}
	return true
}

// h2Heading matches _H2 (`^##\s+(.*)$`) against one line: "##", at least one
// whitespace rune, then the captured remainder.
func h2Heading(line string) (string, bool) {
	if !strings.HasPrefix(line, "##") {
		return "", false
	}
	rest := line[2:]
	trimmed := strings.TrimLeftFunc(rest, unicode.IsSpace)
	if len(trimmed) == len(rest) { // no whitespace after "##": ###, ##Title, bare ##
		return "", false
	}
	return trimmed, true
}

// keysOutside returns the map's keys not in allowed, sorted.
func keysOutside(m *omap.Map, allowed map[string]bool) []string {
	var out []string
	for _, k := range m.Keys() {
		if !allowed[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// keysInside returns the map's keys that are in picked, sorted.
func keysInside(m *omap.Map, picked map[string]bool) []string {
	var out []string
	for _, k := range m.Keys() {
		if picked[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// topLevelMap applies load_yaml's `data or {}` then the isinstance(dict)
// gate: falsy documents collapse to an empty mapping (whose missing
// `sections` key errors downstream, matching Python); truthy non-mappings
// fail the gate.
func topLevelMap(v any) (*omap.Map, bool) {
	switch x := v.(type) {
	case nil:
		return omap.New(), true
	case *omap.Map:
		return x, true
	case string:
		if x == "" {
			return omap.New(), true
		}
	case bool:
		if !x {
			return omap.New(), true
		}
	case int64:
		if x == 0 {
			return omap.New(), true
		}
	case float64:
		if x == 0 {
			return omap.New(), true
		}
	case canon.BigInt:
		if x.Literal == "0" {
			return omap.New(), true
		}
	case []any:
		if len(x) == 0 {
			return omap.New(), true
		}
	}
	return nil, false
}

// scalarText renders an optional scalar (title/hint/text) the way Python's
// truthiness-then-str() usage does: falsy values (None, "", 0, false) read
// as "", anything else as its str() form.
func scalarText(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		if x {
			return "True"
		}
		return ""
	case int64:
		if x == 0 {
			return ""
		}
		return strconv.FormatInt(x, 10)
	case canon.BigInt:
		if x.Literal == "0" {
			return ""
		}
		return x.Literal
	case float64:
		if x == 0 {
			return ""
		}
		return canon.PyFloatRepr(x)
	case canon.Date:
		return x.ISO
	case canon.DateTime:
		return x.ISO
	default:
		return fmt.Sprintf("%v", v)
	}
}

func invalid(typeName, why string) *errs.Located {
	return errs.New(
		"template_invalid",
		fmt.Sprintf("Template for '%s' (%s/%s.yaml): %s", typeName, TemplatesDir, typeName, why),
	)
}
