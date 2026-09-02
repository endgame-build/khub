// Package template ports src/khub/core/template.py — body templates (TPL-001)
// — and, since the kb 0.14.0 port, kb's body-content layer over them. A
// template is a per-type YAML file at .khub/templates/<type>.yaml; its
// presence makes add/init seed new bodies from it and makes validate require
// its section headings in every instance body as an ordered subsequence
// (prefix-match, numbering stripped, extra headings allowed). A section may
// also carry rules about what belongs under it (word_count, required_text,
// forbidden_text, code_blocks) and be declared optional; the file may carry a
// top-level hint and a lens registry (lenses.go). Body scanning is body.go.
//
// Declared, not inferred. The file states the headings, so one file is both
// the scaffold and the contract: an author edits the sections and both
// change together.
package template

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/omap"
)

// TemplatesDir is the workspace-relative template directory (TEMPLATES_DIR).
const TemplatesDir = ".khub/templates"

var (
	entryKeys = map[string]bool{
		"heading": true, "hint": true, "text": true, "optional": true,
		"word_count": true, "required_text": true, "forbidden_text": true,
		"code_blocks": true,
	}
	// Reserved for later and rejected today with a clear error rather than
	// silently ignored — a template that half-works is worse than one that
	// says no.
	reservedKeys = map[string]bool{
		"repeat": true, "pattern": true, "min_tokens": true, "max_tokens": true, "budget": true,
	}
	topKeys = map[string]bool{"title": true, "hint": true, "sections": true, "lenses": true}

	// Leading list numbering on a heading ("1.", "3)", "2.1)"), stripped
	// before matching, so numbering a body's sections never breaks its
	// contract (kb NUMBERING_RE). The trailing separator is REQUIRED: with it
	// optional, `\d+\s+` swallowed the leading number of any heading that
	// legitimately starts with one — "2026 goals" became "goals", so a
	// template declaring that heading could never be satisfied by a body
	// containing it verbatim. Go's \d and \s are ASCII where Python's are
	// Unicode-wide — an authored-markdown non-difference, ported as-is.
	numberingRE = regexp.MustCompile(`^\d+([.)]\d*)*[.)]\s+`)
)

// TextRule is one `required_text` / `forbidden_text` entry: a literal, or a
// regex when Regex is non-nil (kb TextRule). Both forms are case-insensitive —
// a rule about what an author must say is about the words, not their
// capitalisation at the start of a sentence.
type TextRule struct {
	Source string
	Regex  *regexp.Regexp
}

// FoundIn reports whether the rule matches text (kb TextRule.found_in).
func (r TextRule) FoundIn(text string) bool {
	if r.Regex != nil {
		return r.Regex.MatchString(text)
	}
	return strings.Contains(strings.ToLower(text), strings.ToLower(r.Source))
}

// String renders the rule the way a complaint names it: /src/ for a regex,
// 'src' for a literal (kb TextRule.__str__).
func (r TextRule) String() string {
	if r.Regex != nil {
		return "/" + r.Source + "/"
	}
	return "'" + r.Source + "'"
}

// CodeRule is one `code_blocks` entry: fenced blocks of Lang ("" = any
// language), bounded below and/or above; a nil bound is unset (kb CodeRule).
type CodeRule struct {
	Lang string
	Min  *int
	Max  *int
}

// String names the rule's subject in a complaint (kb CodeRule.__str__):
// "<lang> block(s)", or "code block(s)" when the rule is language-agnostic.
func (r CodeRule) String() string {
	if r.Lang != "" {
		return r.Lang + " block(s)"
	}
	return "code block(s)"
}

// Section is one template entry: the heading plus its scaffold content and
// its body-content rules. Hint and Text hold "" where Python holds None —
// render() treats them identically. An Optional section is scaffolded but
// never required; a nil word bound is unset.
type Section struct {
	Heading       string
	Hint          string
	Text          string
	Optional      bool
	MinWords      *int
	MaxWords      *int
	RequiredText  []TextRule
	ForbiddenText []TextRule
	CodeBlocks    []CodeRule
}

// HasRules reports whether the section declares any body-content rule.
// Nil checks, not zero checks: `word_count: {max: 0}` is a real declaration
// — "this section holds a diagram and nothing else" — and a zero that reads
// as absent would parse, validate, and then never be enforced.
func (s *Section) HasRules() bool {
	return s.MinWords != nil || s.MaxWords != nil ||
		len(s.RequiredText) > 0 || len(s.ForbiddenText) > 0 || len(s.CodeBlocks) > 0
}

// BodyTemplate is a parsed body template: scaffold source, section contract
// and lenses in one. Title and Hint are "" when the template declares none.
type BodyTemplate struct {
	Type     string
	Title    string
	Hint     string
	Sections []Section
	Lenses   []Lens
}

// Render is BodyTemplate.render: the scaffolded body — the top-level hint
// comment when set, then each heading with its hint comment / text, blocks
// separated by blank lines.
//
// Optional sections are scaffolded like any other — declaring one optional
// says it may be absent from a finished body, not that an author should have
// to remember it exists. The top-level hint leads, and is how a type with no
// heading contract at all still gets to say what belongs in its body.
func (t *BodyTemplate) Render() string {
	var parts []string
	if t.Hint != "" {
		parts = append(parts, "<!-- "+t.Hint+" -->\n")
	}
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

// RequiredHeadings lists the non-optional section headings in template order.
func (t *BodyTemplate) RequiredHeadings() []string {
	out := make([]string, 0, len(t.Sections))
	for _, s := range t.Sections {
		if !s.Optional {
			out = append(out, s.Heading)
		}
	}
	return out
}

// TemplatePath is template.template_path. stem is the template file's name
// without extension — a type's declared `template:` name, or by convention the
// type's own name (schema.ResolvedType.TemplateName resolves which).
func TemplatePath(root, stem string) string {
	return filepath.Join(root, filepath.FromSlash(TemplatesDir), stem+".yaml")
}

// LoadTemplate is template.load_template: the type's template, or (nil, nil)
// when there is no body contract (no template file; a file declaring
// `sections: []` still returns a template so add/init keep seeding). A
// missing sections key is an error: the file declares nothing coherent.
//
// fields is the owning type's declared field names
// (schema.ResolvedType.FieldNames), the set a lens `when` clause may name. It
// is always consulted: nil is a type declaring no fields, and a `when` naming
// any field is refused against it.
func LoadTemplate(root, stem string, fields []string) (*BodyTemplate, error) {
	p := TemplatePath(root, stem)
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
		return nil, invalid(stem, "top level must be a mapping with a 'sections' list")
	}
	if unknown := keysOutside(data, topKeys); len(unknown) > 0 {
		return nil, invalid(stem, "unknown top-level key(s): "+strings.Join(unknown, ", "))
	}
	rawSections, present := data.Get("sections")
	if !present {
		return nil, invalid(stem, "'sections' is required (use `sections: []` for no body contract)")
	}
	list, isList := rawSections.([]any)
	if !isList {
		return nil, invalid(stem, "'sections' must be a list")
	}
	sections := make([]Section, 0, len(list))
	for i, entryAny := range list {
		entry, isMap := entryAny.(*omap.Map)
		if !isMap {
			return nil, invalid(stem, fmt.Sprintf("sections[%d] must be a mapping with a 'heading'", i))
		}
		if reserved := keysInside(entry, reservedKeys); len(reserved) > 0 {
			return nil, invalid(stem, fmt.Sprintf(
				"sections[%d] uses reserved key(s) %s — planned for a later version, not supported yet",
				i, strings.Join(reserved, ", ")))
		}
		if unknown := keysOutside(entry, entryKeys); len(unknown) > 0 {
			return nil, invalid(stem, fmt.Sprintf(
				"sections[%d] unknown key(s): %s", i, strings.Join(unknown, ", ")))
		}
		headingAny, _ := entry.Get("heading")
		heading, isStr := headingAny.(string)
		if !isStr || heading == "" {
			return nil, invalid(stem, fmt.Sprintf("sections[%d] needs a non-empty string 'heading'", i))
		}
		spot := fmt.Sprintf("sections[%d]", i)
		// `hint` and `text` are rendered straight into a body, so a
		// non-string here would parse clean and then break inside Render —
		// out of `add`, long after the file that caused it went unreported.
		hint, err := textValue(stem, spot, entry, "hint")
		if err != nil {
			return nil, err
		}
		text, err := textValue(stem, spot, entry, "text")
		if err != nil {
			return nil, err
		}
		optional := false
		if optAny, has := entry.Get("optional"); has {
			b, isBool := optAny.(bool)
			if !isBool {
				return nil, invalid(stem, spot+" 'optional' must be true or false")
			}
			optional = b
		}
		wc, _ := entry.Get("word_count")
		low, high, err := wordBounds(stem, spot+" 'word_count'", wc)
		if err != nil {
			return nil, err
		}
		reqAny, _ := entry.Get("required_text")
		required, err := textRules(stem, spot+" 'required_text'", reqAny)
		if err != nil {
			return nil, err
		}
		forbAny, _ := entry.Get("forbidden_text")
		forbidden, err := textRules(stem, spot+" 'forbidden_text'", forbAny)
		if err != nil {
			return nil, err
		}
		cbAny, _ := entry.Get("code_blocks")
		code, err := codeRules(stem, spot+" 'code_blocks'", cbAny)
		if err != nil {
			return nil, err
		}
		sections = append(sections, Section{
			Heading:       strings.TrimSpace(heading),
			Hint:          hint,
			Text:          text,
			Optional:      optional,
			MinWords:      low,
			MaxWords:      high,
			RequiredText:  required,
			ForbiddenText: forbidden,
			CodeBlocks:    code,
		})
	}
	title, err := textValue(stem, "", data, "title")
	if err != nil {
		return nil, err
	}
	hint, err := textValue(stem, "", data, "hint")
	if err != nil {
		return nil, err
	}
	headings := make(map[string]bool, len(sections))
	for _, s := range sections {
		headings[s.Heading] = true
	}
	lensesAny, _ := data.Get("lenses")
	lenses, err := loadLenses(stem, lensesAny, headings, fields)
	if err != nil {
		return nil, err
	}
	return &BodyTemplate{Type: stem, Title: title, Hint: hint, Sections: sections, Lenses: lenses}, nil
}

// textValue reads an optional text key: absent or null reads as "", a string
// as itself, anything else is refused as `'<key>' must be text, got <type>`
// (Python's type name). spot prefixes the message for a section entry and is
// "" at the top level.
func textValue(stem, spot string, m *omap.Map, key string) (string, error) {
	v, _ := m.Get(key)
	switch x := v.(type) {
	case nil:
		return "", nil
	case string:
		return x, nil
	}
	where := fmt.Sprintf("'%s' must be text, got %s", key, canon.PyTypeName(v))
	if spot != "" {
		where = spot + " " + where
	}
	return "", invalid(stem, where)
}

// bound is kb _bound: a non-negative integer bound, or nil when unset. A
// bool is not an int here (Python's isinstance(True, int) is true, so a bare
// `min: true` would otherwise sail through as the bound 1 and quietly
// enforce a rule nobody wrote).
func bound(stem, where string, v any) (*int, error) {
	var n int
	switch x := v.(type) {
	case nil:
		return nil, nil
	case int64:
		n = int(x)
	case int:
		n = x
	default:
		return nil, invalid(stem, where+" must be a non-negative whole number")
	}
	if n < 0 {
		return nil, invalid(stem, where+" must be a non-negative whole number")
	}
	return &n, nil
}

// wordBounds is kb _word_bounds: `word_count: {min, max}`, either key alone
// or both.
func wordBounds(stem, where string, v any) (low, high *int, err error) {
	if v == nil {
		return nil, nil, nil
	}
	m, isMap := v.(*omap.Map)
	if !isMap {
		return nil, nil, invalid(stem, where+" must be a mapping with 'min' and/or 'max'")
	}
	if unknown := keysOutside(m, map[string]bool{"min": true, "max": true}); len(unknown) > 0 {
		return nil, nil, invalid(stem, where+" unknown key(s): "+strings.Join(unknown, ", "))
	}
	minAny, _ := m.Get("min")
	if low, err = bound(stem, where+" 'min'", minAny); err != nil {
		return nil, nil, err
	}
	maxAny, _ := m.Get("max")
	if high, err = bound(stem, where+" 'max'", maxAny); err != nil {
		return nil, nil, err
	}
	if low != nil && high != nil && *high < *low {
		return nil, nil, invalid(stem, where+" 'max' is below 'min'")
	}
	return low, high, nil
}

// textRules is kb _text_rules: `required_text` / `forbidden_text`, a list of
// literals and `{pattern: …}` maps. A pattern compiles case-insensitively
// through RE2 (`(?i)` prefixed): no backreferences or lookaround, unlike
// Python's re — a template leaning on either is refused as not compiling.
func textRules(stem, where string, v any) ([]TextRule, error) {
	if v == nil {
		return nil, nil
	}
	list, isList := v.([]any)
	if !isList {
		return nil, invalid(stem, where+" must be a list of literals or {pattern: …}")
	}
	rules := make([]TextRule, 0, len(list))
	for _, item := range list {
		if s, isStr := item.(string); isStr {
			if strings.TrimSpace(s) == "" {
				return nil, invalid(stem, where+" has an empty literal")
			}
			rules = append(rules, TextRule{Source: s})
			continue
		}
		m, isMap := item.(*omap.Map)
		if !isMap || !slices.Equal(m.Keys(), []string{"pattern"}) {
			return nil, invalid(stem, where+" entries are a literal or {pattern: …}")
		}
		srcAny, _ := m.Get("pattern")
		source, isStr := srcAny.(string)
		if !isStr || source == "" {
			return nil, invalid(stem, where+" 'pattern' must be a non-empty string")
		}
		re, err := regexp.Compile("(?i)" + source)
		if err != nil {
			return nil, invalid(stem, fmt.Sprintf("%s pattern %s does not compile: %s", where, pyRepr(source), err))
		}
		rules = append(rules, TextRule{Source: source, Regex: re})
	}
	return rules, nil
}

// codeRules is kb _code_rules: `code_blocks`, a list of `{lang?, min?, max?}`
// maps. A rule with neither bound is refused rather than accepted-and-inert,
// because the shorthand it looks like — `{lang: mermaid}` for "requires a
// diagram" — is exactly what someone means. Neither bound is implied:
// `{lang: bash, max: 2}` reads as "at most two", and defaulting min to 1
// would fire on a section that legitimately has none, and make `{max: 0}`
// — the way to say a section holds no code at all — unsatisfiable.
func codeRules(stem, where string, v any) ([]CodeRule, error) {
	if v == nil {
		return nil, nil
	}
	list, isList := v.([]any)
	if !isList {
		return nil, invalid(stem, where+" must be a list of {lang, min, max} mappings")
	}
	rules := make([]CodeRule, 0, len(list))
	for i, item := range list {
		spot := fmt.Sprintf("%s[%d]", where, i)
		m, isMap := item.(*omap.Map)
		if !isMap {
			return nil, invalid(stem, spot+" must be a mapping with 'lang', 'min' or 'max'")
		}
		if unknown := keysOutside(m, map[string]bool{"lang": true, "min": true, "max": true}); len(unknown) > 0 {
			return nil, invalid(stem, spot+" unknown key(s): "+strings.Join(unknown, ", "))
		}
		lang := ""
		if langAny, _ := m.Get("lang"); langAny != nil {
			s, isStr := langAny.(string)
			if !isStr || strings.TrimSpace(s) == "" {
				return nil, invalid(stem, spot+" 'lang' must be a non-empty string")
			}
			lang = strings.TrimSpace(s)
		}
		minAny, _ := m.Get("min")
		low, err := bound(stem, spot+" 'min'", minAny)
		if err != nil {
			return nil, err
		}
		maxAny, _ := m.Get("max")
		high, err := bound(stem, spot+" 'max'", maxAny)
		if err != nil {
			return nil, err
		}
		if low != nil && high != nil && *high < *low {
			return nil, invalid(stem, spot+" 'max' is below 'min'")
		}
		if low == nil && high == nil {
			return nil, invalid(stem, spot+" needs 'min' or 'max'")
		}
		rules = append(rules, CodeRule{Lang: lang, Min: low, Max: high})
	}
	return rules, nil
}

// keysOutside returns the map's keys not in allowed, sorted.
func keysOutside(m *omap.Map, allowed map[string]bool) []string {
	var out []string
	for _, k := range m.Keys() {
		if !allowed[k] {
			out = append(out, k)
		}
	}
	slices.Sort(out)
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
	slices.Sort(out)
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

func invalid(typeName, why string) *errs.Located {
	return errs.New(
		"template_invalid",
		fmt.Sprintf("Template for '%s' (%s/%s.yaml): %s", typeName, TemplatesDir, typeName, why),
	)
}
