package canon

// The comment-preserving write path — Render's and DumpCollection's twin for
// documents that ALREADY exist on disk.
//
// Python khub edits through ruamel's round-trip loader, so a hand-written
// comment survives an edit; formats.py promises exactly that ("md and yaml
// preserve key order and comments"), and docs/collections-design.md extends
// the promise to a yaml collection's header, inter-row and trailing comments.
// The Go port parses to *omap.Map, which has no comment channel, so a
// whole-document re-emit destroys every comment.
//
// The fix is the mechanism parity/yamlgate/REPORT.md proved on 133 real files
// and parity/DECISIONS.md D2 sanctions: goccy's LEXER token origins
// reconstruct a document byte-for-byte (T1c 133/133), so an edit becomes a
// surgical splice of one token's Origin and every other byte survives BY
// CONSTRUCTION (T2 99/99). The AST is used only to locate the pairs; it is
// never a writer.
//
// # Fallback policy (the load-bearing design decision)
//
// A splice can express three shapes: an in-place change to a scalar sitting on
// its key's own line, at any mapping depth; a brand-new key appended to the
// TOP-LEVEL mapping (where ruamel also appends it); and no change at all.
// Anything else — a deleted key, a reordered key, a new key inside a nested
// block, a value that is a block/sequence rather than a same-line scalar —
// falls back to the existing LoadDocMap + DumpRT whole-document re-emit.
//
// The fallback is byte-correct: the T3 corpus (1089 cases in canon_test.go)
// pins the emitter against ruamel, and internal/entity's differential tests
// pin the documents. It is only LOSSY FOR COMMENTS. So the trade is "comments
// are lost for the one document that needs an inexpressible change" instead of
// today's "comments are always lost".
//
// A comment-free document deliberately takes the fallback too: the emitter is
// the byte-pinned path, and splicing there would preserve hand-authored
// formatting (quote style, `key:` for a null, sequence indentation) that ruamel
// NORMALIZES on write — a parity regression bought for nothing. The splice
// only earns its keep when there is a comment to protect.

import (
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/lexer"
	"github.com/goccy/go-yaml/parser"
	"github.com/goccy/go-yaml/token"

	"github.com/endgame-build/khub/internal/omap"
)

// ErrNoSplice reports that a change cannot be expressed as an in-place token
// splice. Every caller answers it the same way: re-emit the whole document
// through Render / DumpCollection.
var ErrNoSplice = errors.New("canon: change is not expressible as an in-place splice")

// SpliceDoc is Render's comment-preserving twin: given a document's CURRENT
// text plus the metadata and body it must end up carrying, it returns the new
// text with every byte it need not touch — comments above all — intact.
//
// ErrNoSplice means "re-emit through Render instead"; it is an expected
// outcome, not a failure. mode must be the ResolveMode the caller loaded meta
// with, so an unchanged exotic scalar (`yes`, `017`) is not read as a change.
func SpliceDoc(old string, meta *omap.Map, body, fmt_ string, mode ResolveMode) (string, error) {
	switch fmt_ {
	case "md":
		// The edit-altitude split (the one read_doc uses), so the spliced YAML
		// reassembles through the same RenderMD the emitter path uses.
		yamlText, _, err := SplitFrontmatter(old)
		if err != nil {
			return "", ErrNoSplice
		}
		out, err := SpliceMapping([]byte(yamlText), meta, mode)
		if err != nil {
			return "", err
		}
		return RenderMD(string(out), body), nil
	case "yaml":
		work := meta
		if body != "" {
			work = cloneWith(meta, bodyKey, body)
		}
		out, err := SpliceMapping([]byte(old), work, mode)
		if err != nil {
			return "", err
		}
		return string(out), nil
	default:
		return "", ErrNoSplice // json admits no comments; nothing to preserve
	}
}

// SpliceCollection is DumpCollection's comment-preserving twin. Only the yaml
// shape carries comments; json and jsonl re-emit as before.
func SpliceCollection(old string, rows *omap.Map, fmt_ string) (string, error) {
	if fmt_ != "yaml" {
		return "", ErrNoSplice
	}
	// LoadCollection reads a yaml collection under ruamel 1.2 resolution.
	out, err := SpliceMapping([]byte(old), rows, Mode12)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// SpliceMapping rewrites the YAML text src so its mapping reads back as want,
// touching only the bytes it must. Returns ErrNoSplice when the change is not
// expressible (see the package-level fallback policy).
func SpliceMapping(src []byte, want *omap.Map, mode ResolveMode) ([]byte, error) {
	text := string(src)
	toks := lexer.Tokenize(text)
	// Previously a comment-free document fell back to the emitter, on the
	// reasoning that splicing would preserve hand-authored formatting ruamel
	// normalizes. That reasoning was wrong for VALUES: ruamel round-trips
	// `017` and `.inf` verbatim, so re-emitting rewrote them to `17` and
	// `+.Inf` — silent corruption of fields the user never touched, on any
	// edit. Splicing preserves the value exactly; what it also preserves is
	// quote style and null spelling, which ruamel does normalize. That is
	// cosmetic and already recorded as D13, and a cosmetic divergence is a far
	// better trade than a changed value.
	if reconcat(toks, text) != text {
		// T1c identity is the whole safety argument. Without it the untouched
		// bytes are not guaranteed to survive, so refuse rather than guess.
		return nil, ErrNoSplice
	}
	have, nonMap, err := LoadDocMap(text, mode)
	if err != nil || nonMap != nil {
		return nil, ErrNoSplice
	}
	edits, adds, err := diffMapping(have, want, nil, true)
	if err != nil {
		return nil, err
	}
	if len(edits) > 0 {
		targets, terr := scalarTargets(src)
		if terr != nil {
			return nil, ErrNoSplice
		}
		byOffset := make(map[int]int, len(toks))
		for i, tok := range toks {
			byOffset[tok.Position.Offset] = i
		}
		for _, e := range edits {
			tgt, ok := targets[pathKey(e.path)]
			if !ok {
				return nil, ErrNoSplice // block, sequence or absent value
			}
			vi, ok := byOffset[tgt.valOffset]
			if !ok || !spliceValue(toks, vi, tgt, e.value) {
				return nil, ErrNoSplice
			}
		}
	}
	out := reconcat(toks, text)
	for _, a := range adds {
		line, derr := DumpRT(oneKey(a.key, a.value))
		if derr != nil {
			return nil, ErrNoSplice
		}
		if out != "" && !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		out += line
	}
	// Belt and braces: the spliced text must read back as exactly what the
	// caller asked for. A splice that changed the wrong bytes never ships.
	check, checkNonMap, cerr := LoadDocMap(out, mode)
	if cerr != nil || checkNonMap != nil || !equalValue(check, want) {
		return nil, ErrNoSplice
	}
	return []byte(out), nil
}

// --- change detection --------------------------------------------------------

// scalarEdit is one same-line scalar to overwrite, addressed by mapping path.
type scalarEdit struct {
	path  []string
	value any
}

// keyAppend is one brand-new top-level key to append after the last line.
type keyAppend struct {
	key   string
	value any
}

// diffMapping walks have against want and reports the edits and appends a
// splice would need. top marks the root mapping, the only depth where a new
// key is expressible (ruamel appends it after the last line; inside a nested
// block ruamel's placement follows comment attachment, which a splice cannot
// reproduce — see the fallback policy).
func diffMapping(have, want *omap.Map, path []string, top bool) ([]scalarEdit, []keyAppend, error) {
	haveKeys, wantKeys := have.Keys(), want.Keys()
	if len(wantKeys) < len(haveKeys) {
		return nil, nil, ErrNoSplice // a key was deleted
	}
	if !top && len(wantKeys) != len(haveKeys) {
		return nil, nil, ErrNoSplice // a key was added below the root
	}
	var edits []scalarEdit
	var adds []keyAppend
	for i, k := range haveKeys {
		if wantKeys[i] != k {
			return nil, nil, ErrNoSplice // a key was deleted or reordered
		}
		hv, _ := have.Get(k)
		wv, _ := want.Get(k)
		if equalValue(hv, wv) {
			continue
		}
		here := append(append([]string{}, path...), k)
		hm, hIsMap := hv.(*omap.Map)
		wm, wIsMap := wv.(*omap.Map)
		if hIsMap && wIsMap {
			subEdits, subAdds, err := diffMapping(hm, wm, here, false)
			if err != nil {
				return nil, nil, err
			}
			edits = append(edits, subEdits...)
			adds = append(adds, subAdds...)
			continue
		}
		edits = append(edits, scalarEdit{path: here, value: wv})
	}
	for _, k := range wantKeys[len(haveKeys):] {
		v, _ := want.Get(k)
		adds = append(adds, keyAppend{key: k, value: v})
	}
	return edits, adds, nil
}

// equalValue is deep equality over the canon value set (nil, bool, string,
// int64, float64, BigInt, Date, DateTime, []any, *omap.Map). Key ORDER counts:
// a reordered mapping is a change a splice must refuse.
func equalValue(a, b any) bool {
	switch x := a.(type) {
	case nil:
		return b == nil
	case *omap.Map:
		y, ok := b.(*omap.Map)
		if !ok || x.Len() != y.Len() {
			return false
		}
		yKeys := y.Keys()
		for i, k := range x.Keys() {
			if yKeys[i] != k {
				return false
			}
			xv, _ := x.Get(k)
			yv, _ := y.Get(k)
			if !equalValue(xv, yv) {
				return false
			}
		}
		return true
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !equalValue(x[i], y[i]) {
				return false
			}
		}
		return true
	default:
		return a == b // the remaining canon scalars are all comparable
	}
}

// --- locating the pairs ------------------------------------------------------

// spliceTarget addresses one same-line scalar value in the token stream.
type spliceTarget struct {
	valOffset int // token.Position.Offset of the value token
	colonLine int
	colonCol  int // 1-based column of the ':' — the column just past it
	keyIndent int // 0-based indent of the key, the emitter's indent for the value
}

// scalarTargets walks the AST for every mapping pair whose value is a scalar
// node, keyed by mapping path. The AST is the map, never the writer: the
// offsets it reports index the same token stream the lexer produced.
func scalarTargets(src []byte) (map[string]spliceTarget, error) {
	f, err := parser.ParseBytes(src, 0)
	if err != nil {
		return nil, err
	}
	out := map[string]spliceTarget{}
	for _, doc := range f.Docs {
		if doc.Body != nil {
			collectTargets(doc.Body, nil, out)
		}
	}
	return out, nil
}

func collectTargets(n ast.Node, path []string, out map[string]spliceTarget) {
	switch x := n.(type) {
	case *ast.MappingNode:
		for _, mv := range x.Values {
			collectPair(mv, path, out)
		}
	case *ast.MappingValueNode:
		collectPair(x, path, out)
	}
	// Sequence members are deliberately not addressed: a list element has no
	// key path, so a changed list falls back.
}

func collectPair(mv *ast.MappingValueNode, path []string, out map[string]spliceTarget) {
	if mv.Key == nil || mv.Value == nil || mv.Start == nil {
		return
	}
	keyTok := mv.Key.GetToken()
	if keyTok == nil || keyTok.Position == nil {
		return
	}
	here := append(append([]string{}, path...), keyTok.Value)
	switch mv.Value.(type) {
	case *ast.MappingNode, *ast.MappingValueNode:
		collectTargets(mv.Value, here, out)
		return
	case *ast.StringNode, *ast.IntegerNode, *ast.FloatNode, *ast.BoolNode,
		*ast.NullNode, *ast.InfinityNode, *ast.NanNode:
	default:
		return // sequence, literal/folded block, anchor, alias, tag — not spliceable
	}
	valTok := mv.Value.GetToken()
	if valTok == nil || valTok.Position == nil {
		return
	}
	out[pathKey(here)] = spliceTarget{
		valOffset: valTok.Position.Offset,
		colonLine: mv.Start.Position.Line,
		colonCol:  mv.Start.Position.Column,
		keyIndent: keyTok.Position.Column - 1,
	}
}

func pathKey(path []string) string { return strings.Join(path, "\x00") }

// --- the splice itself -------------------------------------------------------

// spliceableValue is the token set a same-line scalar can be. Everything else
// (MappingValue for an implicit null, Literal/Folded openers, anchors, flow
// indicators) means the value is not a plain scalar sitting after the colon.
var spliceableValue = map[token.Type]bool{
	token.StringType: true, token.SingleQuoteType: true, token.DoubleQuoteType: true,
	token.IntegerType: true, token.BinaryIntegerType: true, token.OctetIntegerType: true,
	token.HexIntegerType: true, token.FloatType: true, token.BoolType: true,
	token.NullType: true, token.InfinityType: true, token.NanType: true,
}

// spliceValue overwrites one value token's Origin in place, preserving the
// whitespace that follows it and re-seating any trailing comment at the column
// ruamel would put it.
func spliceValue(toks token.Tokens, vi int, tgt spliceTarget, v any) bool {
	tok := toks[vi]
	if tok.Position == nil || !spliceableValue[tok.Type] {
		return false
	}
	if tok.Position.Line != tgt.colonLine || tok.Position.Column <= tgt.colonCol {
		return false // a block, a sequence, or an implicit null on the next line
	}
	origin := tok.Origin
	trimmed := strings.TrimRight(origin, " \t\r\n")
	if trimmed == "" || !strings.HasPrefix(trimmed, " ") {
		return false // no ` ` separator after the colon: not the shape we splice
	}
	tail := origin[len(trimmed):]

	// The emitter renders the replacement exactly as a whole-document re-emit
	// would, starting from the column just past the ':' — so a long value folds
	// the same way and a quoted style is chosen the same way.
	rendered, ok := renderInline(v, tgt.colonCol, tgt.keyIndent)
	if !ok {
		return false
	}
	if c := inlineComment(toks, vi, tail); c != nil {
		// ruamel Emitter.write_comment: pad back to the comment's recorded
		// column, but never fewer than one space when the value overruns it.
		// The gap lives on this token, so the comment's own leading run goes.
		gap := (c.Position.Column - 1) - endColumn(rendered, tgt.colonCol)
		if gap < 1 {
			gap = 1
		}
		tail = strings.Repeat(" ", gap)
		c.Origin = strings.TrimLeft(c.Origin, " \t")
	}
	tok.Origin = rendered + tail // reconcat reads Origin alone
	return true
}

// inlineComment returns the comment token sharing the value's last line, or
// nil. goccy splits the gap between a value and its comment unpredictably —
// sometimes onto the value's Origin, sometimes onto the comment's — so the test
// is positional: no line break in the value's trailing run, and none opening
// the comment.
func inlineComment(toks token.Tokens, vi int, tail string) *token.Token {
	next := vi + 1
	if next >= len(toks) || toks[next].Type != token.CommentType || toks[next].Position == nil {
		return nil
	}
	if strings.ContainsAny(tail, breakChars) || startsWithBreak(toks[next].Origin) {
		return nil
	}
	return toks[next]
}

// renderInline renders `<space><scalar>` the way emitEntry would, given the
// column just past the ':' and the key's indent. Reports false for a value the
// emitter opens on the next line (a non-empty mapping or sequence).
func renderInline(v any, colonCol, keyIndent int) (string, bool) {
	e := &emitter{width: 80}
	e.col = colonCol
	if err := e.emitValue(v, keyIndent); err != nil {
		return "", false
	}
	s := strings.TrimSuffix(e.b.String(), "\n")
	if s == "" || !strings.HasPrefix(s, " ") {
		return "", false
	}
	return s, true
}

// endColumn is the emitter's column after writing rendered from colonCol. A
// value the emitter folded resets the column to its last line.
func endColumn(rendered string, colonCol int) int {
	if i := strings.LastIndexAny(rendered, breakChars); i >= 0 {
		_, size := utf8.DecodeRuneInString(rendered[i:])
		return len([]rune(rendered[i+size:]))
	}
	return colonCol + len([]rune(rendered))
}

// --- token-stream helpers ----------------------------------------------------

// reconcat rebuilds the document from token origins, restoring the final
// newline the last token drops. This byte identity (yamlgate T1c, 133/133) is
// what makes an untouched line survive by construction.
func reconcat(toks token.Tokens, src string) string {
	var b strings.Builder
	for _, tok := range toks {
		b.WriteString(tok.Origin)
	}
	out := b.String()
	if strings.HasSuffix(src, "\n") && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out
}

func carriesComment(toks token.Tokens) bool {
	for _, tok := range toks {
		if tok.Type == token.CommentType {
			return true
		}
	}
	return false
}

// breakChars is the YAML line-break set the emitter also recognises.
const breakChars = "\n\r\u0085\u2028\u2029"

// startsWithBreak reports whether a comment token opens its own line: goccy
// hands the preceding line break to the comment token, not to the value token
// before it, so a comment whose origin starts with a break is NOT inline.
func startsWithBreak(origin string) bool {
	if origin == "" {
		return false
	}
	r, _ := utf8.DecodeRuneInString(origin)
	return strings.ContainsRune(breakChars, r)
}

func oneKey(k string, v any) *omap.Map {
	m := omap.New()
	m.Set(k, v)
	return m
}
