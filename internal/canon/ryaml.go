package canon

// ruamel.yaml-emulating block-style YAML emitter — the write half of khub's
// serialization canon. Ported line-for-line
// from ruamel.yaml 0.18.x Emitter (write_plain / write_single_quoted /
// write_double_quoted / analyze_scalar / choose_scalar_style) for the shapes
// khub writes: block mappings and sequences, flow {} / [] for empty
// containers, plain/single/double scalars, allow_unicode on. Two profiles
// mirror the two Python instances:
//   DumpRT   — formats.py round-trip profile (best_width 80)
//   DumpWide — workspace.py init profile (width 4096, insertion order)

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/endgame-build/khub/internal/omap"
)

const bestIndent = 2

// DumpRT emits YAML in ruamel's round-trip profile at width 80 — entity
// frontmatter and collections, the human-edited surface.
func DumpRT(v any) (string, error) { return dump(v, 80) }

// DumpWide emits YAML in ruamel's safe profile at width 4096 — the schema
// layer files, where long scalars stay on one line.
func DumpWide(v any) (string, error) { return dump(v, 4096) }

func dump(v any, width int) (string, error) {
	e := &emitter{width: width}
	if err := e.emitRoot(v); err != nil {
		return "", err
	}
	return e.b.String(), nil
}

type emitter struct {
	b         strings.Builder
	width     int
	col       int
	indention bool // at line start (only breaks/indent written since)
}

func (e *emitter) write(s string) {
	e.b.WriteString(s)
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		e.col = len([]rune(s[i+1:]))
	} else {
		e.col += len([]rune(s))
	}
	if s != "" {
		e.indention = false
	}
}

// writeBreak writes a line break character (ruamel write_line_break): the
// char itself ends the line — exotic breaks (U+2028…) count as breaks too.
func (e *emitter) writeBreak(br string) {
	e.b.WriteString(br)
	e.col = 0
	e.indention = true
}

// writeIndent is ruamel write_indent: break the line unless a break was just
// written, then pad up to indent.
func (e *emitter) writeIndent(indent int) {
	if !e.indention || e.col > indent {
		e.writeBreak("\n")
	}
	if e.col < indent {
		e.b.WriteString(strings.Repeat(" ", indent-e.col))
		e.col = indent
	}
}

func (e *emitter) emitRoot(v any) error {
	switch x := v.(type) {
	case *omap.Map:
		if x.Len() == 0 {
			e.write("{}\n")
			return nil
		}
		return e.emitMap(x, 0)
	case []any:
		if len(x) == 0 {
			e.write("[]\n")
			return nil
		}
		return e.emitSeq(x, 0)
	default:
		return fmt.Errorf("root must be a mapping or sequence, got %T", v)
	}
}

func (e *emitter) emitMap(m *omap.Map, indent int) error {
	pad := strings.Repeat(" ", indent)
	for _, k := range m.Keys() {
		v, _ := m.Get(k)
		e.write(pad)
		if err := e.emitEntry(k, v, indent); err != nil {
			return err
		}
	}
	return nil
}

// emitMapInline: first entry continues the current line (seq item "- k: v").
func (e *emitter) emitMapInline(m *omap.Map, indent int) error {
	pad := strings.Repeat(" ", indent)
	for i, k := range m.Keys() {
		v, _ := m.Get(k)
		if i > 0 {
			e.write(pad)
		}
		if err := e.emitEntry(k, v, indent); err != nil {
			return err
		}
	}
	return nil
}

func (e *emitter) emitEntry(k string, v any, indent int) error {
	a := analyze(k)
	if a.multiline || len([]rune(k)) > 128 {
		e.write("? ")
		e.writeScalarStr(k, indent, true, false)
		e.write("\n" + strings.Repeat(" ", indent) + ":")
	} else {
		e.writeScalarStr(k, indent, false, true)
		e.write(":")
	}
	return e.emitValue(v, indent)
}

func (e *emitter) emitValue(v any, indent int) error {
	switch x := v.(type) {
	case *omap.Map:
		if x.Len() == 0 {
			e.write(" {}\n")
			return nil
		}
		e.write("\n")
		return e.emitMap(x, indent+bestIndent)
	case []any:
		if len(x) == 0 {
			e.write(" []\n")
			return nil
		}
		e.write("\n")
		return e.emitSeq(x, indent) // dash-offset-0: items at the key's indent
	case string:
		e.write(" ")
		e.writeScalarStr(x, indent, true, false)
		e.write("\n")
		return nil
	default:
		s, err := scalarString(v)
		if err != nil {
			return err
		}
		e.write(" ")
		e.writePlain(s, indent+bestIndent, true)
		e.write("\n")
		return nil
	}
}

func (e *emitter) emitSeq(items []any, indent int) error {
	pad := strings.Repeat(" ", indent)
	for _, it := range items {
		e.write(pad + "-")
		if err := e.emitSeqItem(it, indent); err != nil {
			return err
		}
	}
	return nil
}

func (e *emitter) emitSeqItem(it any, indent int) error {
	switch x := it.(type) {
	case *omap.Map:
		if x.Len() == 0 {
			e.write(" {}\n")
			return nil
		}
		e.write(" ")
		return e.emitMapInline(x, indent+bestIndent)
	case []any:
		if len(x) == 0 {
			e.write(" []\n")
			return nil
		}
		e.write(" ")
		pad := strings.Repeat(" ", indent+bestIndent)
		for i, sub := range x {
			if i > 0 {
				e.write(pad)
			}
			e.write("-")
			if err := e.emitSeqItem(sub, indent+bestIndent); err != nil {
				return err
			}
		}
		return nil
	case string:
		e.write(" ")
		e.writeScalarStr(x, indent, true, false)
		e.write("\n")
		return nil
	default:
		s, err := scalarString(it)
		if err != nil {
			return err
		}
		e.write(" ")
		e.writePlain(s, indent+bestIndent, true)
		e.write("\n")
		return nil
	}
}

// --- scalar analysis (ruamel analyze_scalar, allow_unicode=True) -------------

type analysis struct {
	empty           bool
	multiline       bool
	allowBlockPlain bool
	allowSingle     bool
}

func isBreak(ch rune) bool { return ch == '\n' || ch == 0x85 || ch == 0x2028 || ch == 0x2029 }
func isZWS(ch rune) bool {
	return ch == 0 || ch == ' ' || ch == '\t' || ch == '\r' || isBreak(ch)
}

func analyze(s string) analysis {
	if s == "" {
		return analysis{empty: true, allowBlockPlain: true, allowSingle: true}
	}
	runes := []rune(s)

	blockIndicators := false
	lineBreaks := false
	special := false
	leadingSpace, leadingBreak := false, false
	trailingSpace, trailingBreak := false, false
	breakSpace, spaceBreak := false, false

	if strings.HasPrefix(s, "---") || strings.HasPrefix(s, "...") {
		blockIndicators = true
	}
	preceededByWS := true
	followedByWS := len(runes) == 1 || isZWS(runes[1])
	prevSpace, prevBreak := false, false

	for index := 0; index < len(runes); index++ {
		ch := runes[index]
		if index == 0 {
			if strings.ContainsRune("#,[]{}&*!|>'\"%@`", ch) {
				blockIndicators = true
			}
			if (ch == '?' || ch == ':') && followedByWS {
				blockIndicators = true
			}
			if ch == '-' && followedByWS {
				blockIndicators = true
			}
		} else {
			if ch == ':' && followedByWS {
				blockIndicators = true
			}
			if ch == '#' && preceededByWS {
				blockIndicators = true
			}
		}
		if isBreak(ch) {
			lineBreaks = true
		}
		if ch != '\n' && (ch < 0x20 || ch > 0x7e) {
			allowed := (ch == 0x85 || (ch >= 0xa0 && ch <= 0xd7ff) ||
				(ch >= 0xe000 && ch <= 0xfffd) ||
				(ch >= 0x10000 && ch <= 0x10ffff)) && ch != 0xfeff
			if !allowed {
				special = true
			}
		}
		if ch == ' ' {
			if index == 0 {
				leadingSpace = true
			}
			if index == len(runes)-1 {
				trailingSpace = true
			}
			if prevBreak {
				breakSpace = true
			}
			prevSpace, prevBreak = true, false
		} else if isBreak(ch) {
			if index == 0 {
				leadingBreak = true
			}
			if index == len(runes)-1 {
				trailingBreak = true
			}
			if prevSpace {
				spaceBreak = true
			}
			prevSpace, prevBreak = false, true
		} else {
			prevSpace, prevBreak = false, false
		}
		preceededByWS = isZWS(ch)
		followedByWS = index+2 >= len(runes) || isZWS(runes[index+2])
	}

	a := analysis{multiline: lineBreaks, allowBlockPlain: true, allowSingle: true}
	if leadingSpace || leadingBreak || trailingSpace || trailingBreak {
		a.allowBlockPlain = false
	}
	if breakSpace {
		a.allowBlockPlain, a.allowSingle = false, false
	}
	if special {
		a.allowBlockPlain, a.allowSingle = false, false
	} else if spaceBreak {
		a.allowBlockPlain, a.allowSingle = false, false
	}
	if lineBreaks {
		a.allowBlockPlain = false
	}
	if blockIndicators {
		a.allowBlockPlain = false
	}
	return a
}

// --- implicit resolvers (ruamel resolver.py, version 1.2 set) ----------------

var resolvers = []*regexp.Regexp{
	regexp.MustCompile(`^(?:true|True|TRUE|false|False|FALSE)$`),
	regexp.MustCompile(`^(?:[-+]?(?:[0-9][0-9_]*)\.[0-9_]*(?:[eE][-+]?[0-9]+)?|[-+]?(?:[0-9][0-9_]*)(?:[eE][-+]?[0-9]+)|[-+]?\.[0-9_]+(?:[eE][-+][0-9]+)?|[-+]?\.(?:inf|Inf|INF)|\.(?:nan|NaN|NAN))$`),
	regexp.MustCompile(`^(?:[-+]?0b[0-1_]+|[-+]?0o?[0-7_]+|[-+]?[0-9_]+|[-+]?0x[0-9a-fA-F_]+)$`),
	regexp.MustCompile(`^(?:<<)$`),
	regexp.MustCompile(`^(?:~|null|Null|NULL|)$`),
	regexp.MustCompile(`^(?:[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]|[0-9][0-9][0-9][0-9]-[0-9][0-9]?-[0-9][0-9]?(?:[Tt]|[ \t]+)[0-9][0-9]?:[0-9][0-9]:[0-9][0-9](?:\.[0-9]*)?(?:[ \t]*(?:Z|[-+][0-9][0-9]?(?::[0-9][0-9])?))?)$`),
	regexp.MustCompile(`^(?:=)$`),
}

func resolvesAsOtherType(s string) bool {
	for _, re := range resolvers {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

// --- style choice (ruamel choose_scalar_style, event.style == None) ----------

func (e *emitter) writeScalarStr(s string, indent int, split, keyContext bool) {
	a := analyze(s)
	contIndent := indent + bestIndent
	if keyContext {
		split = false
	}
	style := chooseStyle(s, a, keyContext)
	switch style {
	case "plain":
		e.writePlain(s, contIndent, split)
	case "single":
		e.writeSingle(s, contIndent, split)
	default:
		e.writeDouble(s, contIndent, split)
	}
}

func chooseStyle(s string, a analysis, keyContext bool) string {
	implicitStr := !a.empty && !resolvesAsOtherType(s)
	if implicitStr {
		if (!keyContext || (!a.empty && !a.multiline)) && a.allowBlockPlain {
			return "plain"
		}
	}
	if strings.ContainsRune(s, '\'') || strings.ContainsRune(s, '\n') {
		return "double"
	}
	if a.allowSingle && (!keyContext || !a.multiline) {
		return "single"
	}
	return "double"
}

// --- writers (verbatim ports) ------------------------------------------------

func (e *emitter) writePlain(s string, contIndent int, split bool) {
	runes := []rune(s)
	spaces, breaks := false, false
	start := 0
	for end := 0; end <= len(runes); end++ {
		var ch rune
		hasCh := end < len(runes)
		if hasCh {
			ch = runes[end]
		}
		if spaces {
			if !hasCh || ch != ' ' {
				if start+1 == end && e.col >= e.width && split {
					e.writeIndent(contIndent)
				} else {
					e.write(string(runes[start:end]))
				}
				start = end
			}
		} else if breaks {
			if !hasCh || !isBreak(ch) {
				if runes[start] == '\n' {
					e.writeBreak("\n")
				}
				for _, br := range runes[start:end] {
					if br == '\n' {
						e.writeBreak("\n")
					} else {
						e.writeBreak(string(br))
					}
				}
				e.writeIndent(contIndent)
				start = end
			}
		} else {
			if !hasCh || ch == ' ' || isBreak(ch) {
				data := runes[start:end]
				if len(data)+e.col > e.width && e.col > contIndent {
					e.writeIndent(contIndent)
				}
				e.write(string(data))
				start = end
			}
		}
		if hasCh {
			spaces = ch == ' '
			breaks = isBreak(ch)
		}
	}
}

func (e *emitter) writeSingle(s string, contIndent int, split bool) {
	e.write("'")
	runes := []rune(s)
	spaces, breaks := false, false
	start := 0
	for end := 0; end <= len(runes); end++ {
		var ch rune
		hasCh := end < len(runes)
		if hasCh {
			ch = runes[end]
		}
		if spaces {
			if !hasCh || ch != ' ' {
				if start+1 == end && e.col > e.width && split && start != 0 && end != len(runes) {
					e.writeIndent(contIndent)
				} else {
					e.write(string(runes[start:end]))
				}
				start = end
			}
		} else if breaks {
			if !hasCh || !isBreak(ch) {
				if runes[start] == '\n' {
					e.writeBreak("\n")
				}
				for _, br := range runes[start:end] {
					if br == '\n' {
						e.writeBreak("\n")
					} else {
						e.writeBreak(string(br))
					}
				}
				e.writeIndent(contIndent)
				start = end
			}
		} else {
			if !hasCh || ch == ' ' || isBreak(ch) || ch == '\'' {
				if start < end {
					e.write(string(runes[start:end]))
					start = end
				}
			}
		}
		if hasCh && ch == '\'' {
			e.write("''")
			start = end + 1
		}
		if hasCh {
			spaces = ch == ' '
			breaks = isBreak(ch)
		}
	}
	e.write("'")
}

var escapeReplacements = map[rune]string{
	0x00: "0", 0x07: "a", 0x08: "b", 0x09: "t", 0x0a: "n", 0x0b: "v",
	0x0c: "f", 0x0d: "r", 0x1b: "e", '"': "\"", '\\': "\\",
	0x85: "N", 0xa0: "_", 0x2028: "L", 0x2029: "P",
}

func doubleAllowedLiteral(ch rune) bool {
	if ch >= 0x20 && ch <= 0x7e {
		return true
	}
	// allow_unicode ranges, minus BOM
	if ((ch >= 0xa0 && ch <= 0xd7ff) || (ch >= 0xe000 && ch <= 0xfffd) ||
		(ch >= 0x10000 && ch <= 0x10ffff)) && ch != 0xfeff {
		return true
	}
	return false
}

func mustEscapeDouble(ch rune) bool {
	if ch == '"' || ch == '\\' || ch == 0x85 || ch == 0x2028 || ch == 0x2029 || ch == 0xfeff {
		return true
	}
	return !doubleAllowedLiteral(ch)
}

func (e *emitter) writeDouble(s string, contIndent int, split bool) {
	e.write("\"")
	runes := []rune(s)
	start := 0
	for end := 0; end <= len(runes); end++ {
		var ch rune
		hasCh := end < len(runes)
		if hasCh {
			ch = runes[end]
		}
		if !hasCh || mustEscapeDouble(ch) {
			if start < end {
				e.write(string(runes[start:end]))
				start = end
			}
			if hasCh {
				var data string
				if esc, has := escapeReplacements[ch]; has {
					data = "\\" + esc
				} else if ch <= 0xff {
					data = fmt.Sprintf("\\x%02X", ch)
				} else if ch <= 0xffff {
					data = fmt.Sprintf("\\u%04X", ch)
				} else {
					data = fmt.Sprintf("\\U%08X", ch)
				}
				e.write(data)
				start = end + 1
			}
		}
		if 0 < end && end < len(runes)-1 && (ch == ' ' || start >= end) &&
			e.col+(end-start) > e.width && split {
			needBackslash := true
			if end < len(runes) {
				if spacePos := indexRune(runes, ' ', end); spacePos >= 0 {
					if nlPos := indexRuneBefore(runes, '\n', end, spacePos); nlPos >= 0 {
						spacePos = nlPos
					}
					if spacePos+1 < len(runes) {
						if runes[spacePos] == '\n' && runes[spacePos+1] != ' ' {
							// keep backslash
						} else if !containsRuneRange(runes, '"', end, spacePos) &&
							!containsRuneRange(runes, '\'', end, spacePos) &&
							runes[spacePos+1] != ' ' && runes[spacePos+1] != '\n' &&
							pySlice(runes, end-1, end+1) != "  " &&
							start != end {
							needBackslash = false
						}
					}
				}
			}
			data := pySlice(runes, start, end)
			if needBackslash {
				data += "\\"
			}
			if start < end {
				start = end
			}
			e.write(data)
			e.writeIndent(contIndent)
			if start < len(runes) && runes[start] == ' ' {
				if !needBackslash {
					start++
				} else {
					e.write("\\")
				}
			}
		}
	}
	e.write("\"")
}

// pySlice reproduces Python's forgiving slice: an inverted or out-of-range
// range yields "" instead of panicking. The emitter's escape branch can leave
// start == end+1 within the same iteration, which Python renders as the empty
// string (ryaml would otherwise panic on any long scalar carrying a control
// character — the entity write path).
func pySlice(rs []rune, start, end int) string {
	if start < 0 {
		start = 0
	}
	if end > len(rs) {
		end = len(rs)
	}
	if start >= end {
		return ""
	}
	return string(rs[start:end])
}

func indexRune(rs []rune, ch rune, from int) int {
	for i := from; i < len(rs); i++ {
		if rs[i] == ch {
			return i
		}
	}
	return -1
}

func indexRuneBefore(rs []rune, ch rune, from, before int) int {
	for i := from; i < before && i < len(rs); i++ {
		if rs[i] == ch {
			return i
		}
	}
	return -1
}

func containsRuneRange(rs []rune, ch rune, from, to int) bool {
	for i := from; i < to && i < len(rs); i++ {
		if rs[i] == ch {
			return true
		}
	}
	return false
}
