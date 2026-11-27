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
// The fix is the mechanism the yamlgate corpus proves: goccy's LEXER token
// origins reconstruct a document byte-for-byte, so an edit becomes a
// surgical splice of one token's Origin and every other byte survives BY
// CONSTRUCTION (T2 99/99). The AST is used only to locate the pairs; it is
// never a writer.
//
// # Fallback policy (the load-bearing design decision)
//
// A splice expresses these shapes, each touching only the lines it must:
//
//   - a changed scalar sitting on its key's own line, rewritten in place, at
//     any mapping depth;
//   - any other changed value of a block-mapping key (a block list or
//     mapping, a literal, an implicit null, a bare mapping becoming a list):
//     that pair's owned lines rewritten by the emitter;
//   - new items appended to a block sequence;
//   - a new key appended to a nested block mapping, or to the root;
//   - a deleted key in a block mapping;
//   - a renamed key, asked for explicitly through Rename;
//   - no change at all.
//
// Anything else — a reordered key, a new key inside a flow or empty mapping,
// a change inside a flow mapping that is not a scalar in place — falls back to
// the existing LoadDocMap + DumpRT whole-document re-emit.
//
// Line ownership decides where bytes go. A pair owns whole lines: from its
// key's line through the line holding the last token of its value, so a block
// value, the comments inside it and a trailing comment on its last line belong
// to it. A standalone comment line above the key or below the value's last
// line does not. So a deleted pair takes exactly its owned lines; a rewritten
// value replaces them, keeping only the comment on the key's own line; a key
// appended to a nested block, or an item appended to a list, lands right after
// the owned lines of the block's last pair or of the list's key, before any
// standalone comment that follows (a comment between two rows stays between
// them). A key appended to the root goes after the document's last line,
// trailing comments included. A list that is not a block sequence (`[a, b]`)
// grows by having its pair rewritten, in block style.
//
// The fallback is byte-correct: the T3 corpus (1089 cases in canon_test.go)
// pins the emitter against ruamel, and internal/entity's differential tests
// pin the documents. It is only LOSSY FOR COMMENTS. So the trade is "comments
// are lost for the one document that needs an inexpressible change" instead of
// today's "comments are always lost".
//
// Every document takes the splice path, comment-bearing or not: re-emitting a
// comment-free document looked harmless but silently rewrote VALUES ruamel
// round-trips verbatim (`017` → `17`, `.inf` → `+.Inf`). See the policy note
// inside SpliceMapping.

import (
	"errors"
	"math"
	"slices"
	"sort"
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

// Rename asks a splice to rename one mapping key in place: only the key's
// bytes change, so its value, its comments and its position survive. Path
// addresses the key as it stands in the CURRENT text (a frontmatter key is
// one element, a collection row's field is [slug, field]); To is the new
// name. A rename cannot be inferred from the wanted mapping alone — a dropped
// key and a new one read the same as a renamed key — so the caller says so.
// The wanted mapping must carry To where the old key stood.
type Rename struct {
	Path []string
	To   string
}

// SpliceDoc is Render's comment-preserving twin: given a document's CURRENT
// text plus the metadata and body it must end up carrying, it returns the new
// text with every byte it need not touch — comments above all — intact.
//
// ErrNoSplice means "re-emit through Render instead"; it is an expected
// outcome, not a failure. mode must be the ResolveMode the caller loaded meta
// with, so an unchanged exotic scalar (`yes`, `017`) is not read as a change.
func SpliceDoc(old string, meta *omap.Map, body, fmt_ string, mode ResolveMode, renames ...Rename) (string, error) {
	switch fmt_ {
	case "md":
		// The edit-altitude split (the one read_doc uses), so the spliced YAML
		// reassembles through the same RenderMD the emitter path uses.
		yamlText, _, err := SplitFrontmatter(old)
		if err != nil {
			return "", ErrNoSplice
		}
		out, err := SpliceMapping([]byte(yamlText), meta, mode, renames...)
		if err != nil {
			return "", err
		}
		return RenderMD(string(out), body), nil
	case "yaml":
		work := meta
		if body != "" {
			work = cloneWith(meta, bodyKey, body)
		}
		out, err := SpliceMapping([]byte(old), work, mode, renames...)
		if err != nil {
			return "", err
		}
		return string(out), nil
	default:
		return "", ErrNoSplice // json admits no comments; nothing to preserve
	}
}

// SpliceCollection is DumpCollection's comment-preserving twin. Only the yaml
// shape carries comments; json and jsonl re-emit as before, and the re-emit
// keeps a renamed field where rows put it.
func SpliceCollection(old string, rows *omap.Map, fmt_ string, renames ...Rename) (string, error) {
	if fmt_ != "yaml" {
		return "", ErrNoSplice
	}
	// LoadCollection reads a yaml collection under ruamel 1.2 resolution.
	out, err := SpliceMapping([]byte(old), rows, Mode12, renames...)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// SpliceMapping rewrites the YAML text src so its mapping reads back as want,
// touching only the bytes it must. Renames run first, then deletions (keys of
// have that want lacks), then the scalar edits and top-level appends. Returns
// ErrNoSplice when the change is not expressible (see the package-level
// fallback policy).
func SpliceMapping(src []byte, want *omap.Map, mode ResolveMode, renames ...Rename) ([]byte, error) {
	text := string(src)
	toks, have, err := spliceable(text, mode)
	if err != nil {
		return nil, err
	}
	if len(renames) > 0 {
		if text, err = renameKeys(text, toks, have, renames); err != nil {
			return nil, err
		}
		if toks, have, err = spliceable(text, mode); err != nil {
			return nil, err
		}
	}
	if dels := deletedKeys(have, want, nil); len(dels) > 0 {
		if want.Len() == 0 {
			return nil, ErrNoSplice // the emitter writes an emptied document as `{}`
		}
		if text, err = deleteKeys(text, toks, dels); err != nil {
			return nil, err
		}
		if toks, have, err = spliceable(text, mode); err != nil {
			return nil, err
		}
	}
	if adds := nestedAdds(have, want, nil, true); len(adds) > 0 {
		if text, err = insertKeys(text, toks, have, adds); err != nil {
			return nil, err
		}
		if toks, have, err = spliceable(text, mode); err != nil {
			return nil, err
		}
	}
	if apps := seqAppends(have, want, nil); len(apps) > 0 {
		if text, err = appendItems(text, toks, apps); err != nil {
			return nil, err
		}
		if toks, have, err = spliceable(text, mode); err != nil {
			return nil, err
		}
	}
	edits, adds, err := diffMapping(have, want, nil, true)
	if err != nil {
		return nil, err
	}
	// A key a `<<` merge supplies has no line of its own. An edit of it is
	// written as an explicit key in that mapping, which outranks the merge and
	// leaves the `<<` line and its anchor as written.
	if merged := mergedEdits(text, have, edits); len(merged) > 0 {
		if text, err = insertKeys(text, toks, have, merged); err != nil {
			return nil, err
		}
		if toks, have, err = spliceable(text, mode); err != nil {
			return nil, err
		}
		if edits, adds, err = diffMapping(have, want, nil, true); err != nil {
			return nil, err
		}
	}
	var replaces []scalarEdit
	if len(edits) > 0 {
		targets, terr := scalarTargets([]byte(text))
		if terr != nil {
			return nil, ErrNoSplice
		}
		byOffset := tokenIndex(toks)
		for _, e := range edits {
			// A value that is not a same-line scalar, before or after, has its
			// pair's lines rewritten instead.
			tgt, ok := targets[pathKey(e.path)]
			vi, found := byOffset[tgt.valOffset]
			if !ok || !found || !spliceValue(toks, vi, tgt, e.value) {
				replaces = append(replaces, e)
			}
		}
	}
	out := reconcat(toks, text)
	if len(replaces) > 0 {
		if toks, _, err = spliceable(out, mode); err != nil {
			return nil, err
		}
		if out, err = replaceValues(out, toks, replaces); err != nil {
			return nil, err
		}
	}
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

// spliceable tokenizes text and loads its mapping, refusing a document whose
// untouched bytes a splice could not guarantee to keep.
func spliceable(text string, mode ResolveMode) (token.Tokens, *omap.Map, error) {
	toks := lexer.Tokenize(text)
	// Every document splices, comment-free or not. The emitter would rewrite
	// untouched values ruamel keeps verbatim (`017` → `17`, `.inf` → `+.Inf`);
	// the splice keeps them, along with quote style and null spelling, which
	// ruamel would normalize. A cosmetic divergence beats a changed value.
	if reconcat(toks, text) != text && !restoreFoldSpaces(toks, text) {
		// T1c identity is the whole safety argument. Without it the untouched
		// bytes are not guaranteed to survive, so refuse rather than guess.
		return nil, nil, ErrNoSplice
	}
	for _, tk := range toks {
		if tk.Type == token.MappingKeyType {
			// An explicit `? key` loads fine but its value sits on the line
			// after the key, which the target collector does not model: it
			// would read the key as "?" and append the real key as new. The
			// emitter re-renders such a document correctly, so refuse here.
			return nil, nil, ErrNoSplice
		}
	}
	have, nonMap, err := LoadDocMap(text, mode)
	if err != nil || nonMap != nil {
		return nil, nil, ErrNoSplice
	}
	return toks, have, nil
}

// restoreFoldSpaces puts back the spaces and tabs goccy drops before a line
// break inside a multi-line scalar's Origin. The emitter writes `word \n` when
// it folds a long plain scalar, as ruamel does, so without this every folded
// value made its whole document unspliceable. Only whitespace directly before
// a line break is restored, and it reports false unless reconcat then
// rebuilds text byte for byte.
func restoreFoldSpaces(toks token.Tokens, text string) bool {
	p := 0
	for _, tk := range toks {
		start := p
		for i := 0; i < len(tk.Origin); {
			switch {
			case p < len(text) && text[p] == tk.Origin[i]:
				p++
				i++
			case p < len(text) && (text[p] == ' ' || text[p] == '\t') && tk.Origin[i] == '\n' &&
				spacesToBreak(text[p:]) > 0:
				p += spacesToBreak(text[p:])
			default:
				return false
			}
		}
		tk.Origin = text[start:p]
	}
	return reconcat(toks, text) == text
}

// spacesToBreak is the length of the run of spaces and tabs that opens s and
// ends at a line break, or 0 when the run ends anywhere else.
func spacesToBreak(s string) int {
	n := len(s) - len(strings.TrimLeft(s, " \t"))
	if n == 0 || n == len(s) || s[n] != '\n' {
		return 0
	}
	return n
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
// splice would need. top marks the root mapping, where a new key is appended
// after the last line; a new key below the root is the insertion pass's, so
// one left here means want reordered the block.
func diffMapping(have, want *omap.Map, path []string, top bool) ([]scalarEdit, []keyAppend, error) {
	haveKeys, wantKeys := have.Keys(), want.Keys()
	if len(wantKeys) < len(haveKeys) {
		return nil, nil, ErrNoSplice // a key the deletion pass did not remove
	}
	if !top && len(wantKeys) != len(haveKeys) {
		return nil, nil, ErrNoSplice // a nested key the insertion pass did not place
	}
	var edits []scalarEdit
	var adds []keyAppend
	for i, k := range haveKeys {
		if wantKeys[i] != k {
			return nil, nil, ErrNoSplice // a key was reordered
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

// nestedAdd is the run of new keys want appends to one nested mapping.
type nestedAdd struct {
	parent []string
	keys   *omap.Map
}

// nestedAdds lists, per nested mapping both sides hold, the keys want has and
// have lacks. Root keys are diffMapping's appends.
func nestedAdds(have, want *omap.Map, path []string, top bool) []nestedAdd {
	var out []nestedAdd
	fresh := omap.New()
	for _, k := range want.Keys() {
		wv, _ := want.Get(k)
		hv, ok := have.Get(k)
		if !ok {
			fresh.Set(k, wv)
			continue
		}
		hm, hIsMap := hv.(*omap.Map)
		wm, wIsMap := wv.(*omap.Map)
		if hIsMap && wIsMap {
			out = append(out, nestedAdds(hm, wm, append(slices.Clone(path), k), false)...)
		}
	}
	if !top && fresh.Len() > 0 {
		out = append(out, nestedAdd{path, fresh})
	}
	return out
}

// mergedEdits turns each edit of a key that is not written in the text, one
// a `<<` merge supplies, into an insertion into its mapping. The keys
// inserted are the edited one and every merged key between the mapping's last
// written key and it, in the loaded order, at their current values: under
// ruamel's order (explicit keys first) that keeps an edit of a later merged
// key from jumping ahead of an earlier one; under PyYAML's (merged first)
// the run is empty. A top-level key is never merged-only in a loaded entity,
// so only nested paths qualify.
func mergedEdits(text string, have *omap.Map, edits []scalarEdit) []nestedAdd {
	locs, err := keyLocs(text)
	if err != nil {
		return nil
	}
	written := func(path []string) bool { _, ok := locs[pathKey(path)]; return ok }
	values := map[string]any{} // pathKey → edited value
	var parents [][]string
	for _, e := range edits {
		if written(e.path) || len(e.path) < 2 {
			continue
		}
		parent := e.path[:len(e.path)-1]
		if !slices.ContainsFunc(parents, func(p []string) bool { return pathKey(p) == pathKey(parent) }) {
			parents = append(parents, parent)
		}
		values[pathKey(e.path)] = e.value
	}
	var out []nestedAdd
	for _, parent := range parents {
		m, ok := mapAt(have, parent)
		if !ok {
			return nil
		}
		keys := m.Keys()
		last := -1 // index of the mapping's last written key
		upto := -1 // index of its last edited merged key
		for i, k := range keys {
			path := append(slices.Clone(parent), k)
			if written(path) {
				last = i
			}
			if _, edited := values[pathKey(path)]; edited {
				upto = i
			}
		}
		add := omap.New()
		for i := last + 1; i <= upto; i++ {
			path := append(slices.Clone(parent), keys[i])
			if written(path) {
				continue
			}
			v, edited := values[pathKey(path)]
			if !edited {
				v, _ = m.Get(keys[i])
			}
			add.Set(keys[i], v)
		}
		for _, k := range keys[:last+1] {
			// A merged key before the last written one (PyYAML's order) is
			// written alone; its position is already its first appearance.
			if v, edited := values[pathKey(append(slices.Clone(parent), k))]; edited {
				add.Set(k, v)
			}
		}
		out = append(out, nestedAdd{parent, add})
	}
	return out
}

// insertKeys writes each run of new keys into its block mapping, rendered as
// the emitter renders an entry at the block's indent, right after the lines
// the block's last pair owns. A flow mapping, or an empty one, has no such
// line and is refused.
func insertKeys(text string, toks token.Tokens, have *omap.Map, adds []nestedAdd) (string, error) {
	locs, err := keyLocs(text)
	if err != nil {
		return "", ErrNoSplice
	}
	byOffset := tokenIndex(toks)
	starts := tokenStarts(toks)
	ins := make([]lineInsert, 0, len(adds))
	for _, a := range adds {
		parent, ok := mapAt(have, a.parent)
		if !ok || parent.Len() == 0 {
			return "", ErrNoSplice
		}
		// The block's last written key: a key a `<<` merge supplies has no line.
		var loc keyLoc
		ok = false
		for i := parent.Len() - 1; i >= 0 && !ok; i-- {
			loc, ok = locs[pathKey(append(slices.Clone(a.parent), parent.Keys()[i]))]
		}
		if !ok || !loc.block {
			return "", ErrNoSplice
		}
		ki, ok := byOffset[loc.offset]
		if !ok {
			return "", ErrNoSplice
		}
		_, to, ok := pairLines(text, toks, starts, ki)
		if !ok {
			return "", ErrNoSplice
		}
		e := &emitter{width: 80}
		if err := e.emitMap(a.keys, toks[ki].Position.Column-1); err != nil {
			return "", ErrNoSplice
		}
		ins = append(ins, lineInsert{to, e.b.String()})
	}
	return insertLines(text, ins), nil
}

// seqAppend is the run of new items want adds to the end of one list.
type seqAppend struct {
	path  []string
	items []any
}

// seqAppends lists, per list both sides hold under one key, the items want
// adds after every item have already holds.
func seqAppends(have, want *omap.Map, path []string) []seqAppend {
	var out []seqAppend
	for _, k := range want.Keys() {
		wv, _ := want.Get(k)
		hv, ok := have.Get(k)
		if !ok {
			continue
		}
		here := append(slices.Clone(path), k)
		switch h := hv.(type) {
		case *omap.Map:
			if w, isMap := wv.(*omap.Map); isMap {
				out = append(out, seqAppends(h, w, here)...)
			}
		case []any:
			if w, isList := wv.([]any); isList && len(h) > 0 && len(w) > len(h) && equalValue(h, w[:len(h)]) {
				out = append(out, seqAppend{here, w[len(h):]})
			}
		}
	}
	return out
}

// appendItems writes each run of new items into its block sequence, rendered
// as the emitter renders items at the sequence's dash indent, right after the
// lines the list's key owns (so after the last item and its comments). A list
// that is not a block sequence under a block key is left for replaceValues.
func appendItems(text string, toks token.Tokens, apps []seqAppend) (string, error) {
	locs, err := keyLocs(text)
	if err != nil {
		return "", ErrNoSplice
	}
	byOffset := tokenIndex(toks)
	starts := tokenStarts(toks)
	var ins []lineInsert
	for _, a := range apps {
		loc, ok := locs[pathKey(a.path)]
		if !ok || !loc.block {
			continue
		}
		ki, ok := byOffset[loc.offset]
		if !ok {
			continue
		}
		dash := firstValueToken(toks, ki)
		if dash == nil || dash.Type != token.SequenceEntryType || dash.Position.Line <= toks[ki].Position.Line {
			continue
		}
		_, to, ok := pairLines(text, toks, starts, ki)
		if !ok {
			return "", ErrNoSplice
		}
		e := &emitter{width: 80}
		if err := e.emitSeq(a.items, dash.Position.Column-1); err != nil {
			return "", ErrNoSplice
		}
		ins = append(ins, lineInsert{to, e.b.String()})
	}
	return insertLines(text, ins), nil
}

// firstValueToken is the first token after key's ':' that is not a comment.
func firstValueToken(toks token.Tokens, ki int) *token.Token {
	for i := ki + 1; i < len(toks); i++ {
		switch toks[i].Type {
		case token.MappingValueType:
			if i == ki+1 {
				continue
			}
		case token.CommentType:
			continue
		}
		return toks[i]
	}
	return nil
}

// replaceValues rewrites, for each edit, the whole lines its pair owns with
// the pair as the emitter renders it at the key's indent. The key keeps its
// source spelling, and a comment on the key's own line stays on it, re-seated
// at its column; comments on the value's other lines go with the old value.
func replaceValues(text string, toks token.Tokens, edits []scalarEdit) (string, error) {
	locs, err := keyLocs(text)
	if err != nil {
		return "", ErrNoSplice
	}
	byOffset := tokenIndex(toks)
	starts := tokenStarts(toks)
	type cut struct {
		from, to int
		text     string
	}
	cuts := make([]cut, 0, len(edits))
	for _, ed := range edits {
		loc, ok := locs[pathKey(ed.path)]
		if !ok || !loc.block {
			return "", ErrNoSplice
		}
		ki, ok := byOffset[loc.offset]
		if !ok {
			return "", ErrNoSplice
		}
		from, to, ok := pairLines(text, toks, starts, ki)
		if !ok {
			return "", ErrNoSplice
		}
		key := toks[ki]
		spelled := strings.TrimRight(key.Origin[leadLen(key.Origin):], spaceChars)
		rendered, ok := renderKey(ed.path[len(ed.path)-1])
		if !ok {
			return "", ErrNoSplice
		}
		pad := strings.Repeat(" ", key.Position.Column-1)
		e := &emitter{width: 80}
		if err := e.emitMap(oneKey(ed.path[len(ed.path)-1], ed.value), key.Position.Column-1); err != nil {
			return "", ErrNoSplice
		}
		body, found := strings.CutPrefix(e.b.String(), pad+rendered)
		if !found {
			return "", ErrNoSplice
		}
		pair := pad + spelled + body
		if c := keyLineComment(toks, ki); c != nil {
			first, rest, _ := strings.Cut(pair, "\n")
			gap := max(1, c.Position.Column-1-len([]rune(first)))
			pair = first + strings.Repeat(" ", gap) + strings.TrimSpace(c.Origin) + "\n" + rest
		}
		if strings.HasSuffix(text[:to], "\r\n") {
			pair = strings.ReplaceAll(pair, "\n", "\r\n")
		}
		cuts = append(cuts, cut{from, to, pair})
	}
	sort.Slice(cuts, func(i, j int) bool { return cuts[i].from > cuts[j].from })
	for i, c := range cuts {
		if i > 0 && c.to > cuts[i-1].from {
			return "", ErrNoSplice
		}
		if c.to == len(text) && !strings.HasSuffix(text, "\n") {
			c.text = strings.TrimSuffix(c.text, "\n")
		}
		text = text[:c.from] + c.text + text[c.to:]
	}
	return text, nil
}

// keyLineComment is the comment that ends the key's own line, or nil.
func keyLineComment(toks token.Tokens, ki int) *token.Token {
	line := toks[ki].Position.Line
	for i := ki + 1; i < len(toks) && toks[i].Position != nil && toks[i].Position.Line == line; i++ {
		if toks[i].Type == token.CommentType {
			return toks[i]
		}
	}
	return nil
}

// lineInsert is rendered lines to place at a byte offset that starts a line.
type lineInsert struct {
	at   int
	text string
}

// insertLines places each insert, matching the file's line ending at the
// insertion point and ending an unterminated last line first.
func insertLines(text string, ins []lineInsert) string {
	// Later offsets first, so each insert leaves the earlier ones in place.
	sort.SliceStable(ins, func(i, j int) bool { return ins[i].at > ins[j].at })
	for _, in := range ins {
		rendered := in.text
		if in.at == len(text) && text != "" && !strings.HasSuffix(text, "\n") {
			rendered = "\n" + rendered
		}
		if strings.HasSuffix(text[:in.at], "\r\n") {
			rendered = strings.ReplaceAll(rendered, "\n", "\r\n")
		}
		text = text[:in.at] + rendered + text[in.at:]
	}
	return text
}

// deletedKeys lists the paths of have's keys that want lacks, at every depth
// where both sides hold a mapping. A deleted key's own children are not listed.
func deletedKeys(have, want *omap.Map, path []string) [][]string {
	var out [][]string
	for _, k := range have.Keys() {
		here := append(append([]string{}, path...), k)
		wv, ok := want.Get(k)
		if !ok {
			out = append(out, here)
			continue
		}
		hv, _ := have.Get(k)
		hm, hIsMap := hv.(*omap.Map)
		wm, wIsMap := wv.(*omap.Map)
		if hIsMap && wIsMap {
			out = append(out, deletedKeys(hm, wm, here)...)
		}
	}
	return out
}

// EqualValues reports whether two loaded values are the same document value.
func EqualValues(a, b any) bool { return equalValue(a, b) }

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
	case float64:
		// NaN is the one value that is not equal to itself. A `.nan` scalar a
		// hand-written document carries must still read as unchanged, or every
		// edit of that document would re-emit the whole thing (and lose the
		// author's `017` and comments on the way).
		y, ok := b.(float64)
		return ok && (x == y || (math.IsNaN(x) && math.IsNaN(y)))
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

// --- renames and deletions --------------------------------------------------

// keyLoc addresses one mapping key in the token stream.
type keyLoc struct {
	offset int  // token.Position.Offset of the key token
	block  bool // the pair sits in a block mapping, one pair per line
}

// keyLocs walks the AST for every scalar mapping key, keyed by mapping path.
func keyLocs(text string) (map[string]keyLoc, error) {
	f, err := parser.ParseBytes([]byte(text), 0)
	if err != nil {
		return nil, err
	}
	out := map[string]keyLoc{}
	for _, doc := range f.Docs {
		if doc.Body != nil {
			collectKeys(doc.Body, nil, out)
		}
	}
	return out, nil
}

func collectKeys(n ast.Node, path []string, out map[string]keyLoc) {
	var pairs []*ast.MappingValueNode
	switch x := n.(type) {
	case *ast.MappingNode:
		pairs = x.Values
	case *ast.MappingValueNode:
		pairs = []*ast.MappingValueNode{x}
	default:
		return // sequence members have no key path
	}
	for _, mv := range pairs {
		if mv.Key == nil {
			continue
		}
		tok := mv.Key.GetToken()
		if tok == nil || tok.Position == nil {
			continue
		}
		here := append(append([]string{}, path...), tok.Value)
		switch mv.Key.(type) {
		case *ast.StringNode, *ast.IntegerNode, *ast.FloatNode, *ast.BoolNode,
			*ast.NullNode, *ast.InfinityNode, *ast.NanNode:
			out[pathKey(here)] = keyLoc{offset: tok.Position.Offset, block: !mv.IsFlowStyle}
		}
		// An anchored, tagged or merge key is not addressed: its token is not
		// the key text.
		if mv.Value != nil {
			collectKeys(mv.Value, here, out)
		}
	}
}

// renameKeys overwrites each renamed key's token with the new name, rendered
// as the emitter renders a key. The rest of the line, comment included, keeps
// its bytes.
func renameKeys(text string, toks token.Tokens, have *omap.Map, renames []Rename) (string, error) {
	locs, err := keyLocs(text)
	if err != nil {
		return "", ErrNoSplice
	}
	byOffset := tokenIndex(toks)
	// The names each mapping holds as the renames apply, so `status→decision`
	// then `draft→status` frees `status` before the second claims it.
	names := map[string]map[string]bool{}
	for _, r := range renames {
		if len(r.Path) == 0 {
			return "", ErrNoSplice
		}
		parentPath := r.Path[:len(r.Path)-1]
		held := names[pathKey(parentPath)]
		if held == nil {
			parent, ok := mapAt(have, parentPath)
			if !ok {
				return "", ErrNoSplice
			}
			held = map[string]bool{}
			for _, k := range parent.Keys() {
				held[k] = true
			}
			names[pathKey(parentPath)] = held
		}
		from := r.Path[len(r.Path)-1]
		if from == r.To {
			continue
		}
		if !held[from] || held[r.To] {
			return "", ErrNoSplice
		}
		delete(held, from)
		held[r.To] = true
		loc, ok := locs[pathKey(r.Path)]
		if !ok {
			return "", ErrNoSplice
		}
		ki, ok := byOffset[loc.offset]
		if !ok {
			return "", ErrNoSplice
		}
		rendered, ok := renderKey(r.To)
		if !ok {
			return "", ErrNoSplice
		}
		tok := toks[ki]
		lead := leadLen(tok.Origin)
		core := strings.TrimRight(tok.Origin[lead:], spaceChars)
		if core == "" || strings.ContainsAny(core, breakChars) {
			return "", ErrNoSplice
		}
		tok.Origin = tok.Origin[:lead] + rendered + tok.Origin[lead+len(core):]
	}
	return reconcat(toks, text), nil
}

// renderKey renders a mapping key the way emitEntry would. Reports false for a
// key the emitter writes in the explicit `? key` form.
func renderKey(k string) (string, bool) {
	if analyze(k).multiline || len([]rune(k)) > 128 {
		return "", false
	}
	e := &emitter{width: 80}
	e.writeScalarStr(k, 0, false, true)
	return e.b.String(), true
}

// deleteKeys cuts each deleted pair's whole lines out of text.
func deleteKeys(text string, toks token.Tokens, dels [][]string) (string, error) {
	locs, err := keyLocs(text)
	if err != nil {
		return "", ErrNoSplice
	}
	byOffset := tokenIndex(toks)
	starts := tokenStarts(toks)
	type cut struct{ from, to int }
	cuts := make([]cut, 0, len(dels))
	for _, p := range dels {
		loc, ok := locs[pathKey(p)]
		if !ok || !loc.block {
			return "", ErrNoSplice // a flow mapping shares its lines
		}
		ki, ok := byOffset[loc.offset]
		if !ok {
			return "", ErrNoSplice
		}
		from, to, ok := pairLines(text, toks, starts, ki)
		if !ok {
			return "", ErrNoSplice
		}
		cuts = append(cuts, cut{from, to})
	}
	sort.Slice(cuts, func(i, j int) bool { return cuts[i].from > cuts[j].from })
	for i, c := range cuts {
		if i > 0 && c.to > cuts[i-1].from {
			return "", ErrNoSplice
		}
		text = text[:c.from] + text[c.to:]
	}
	return text, nil
}

// pairLines returns the byte range of the whole lines a block pair occupies:
// from the start of its key's line through the end of the line holding the
// last token of its value. A token belongs to the value while it sits on the
// key's line, sits deeper than the key, is a `-` entry at the key's column, or
// is a literal/folded block's content. Comments neither extend nor end the
// range, so a standalone comment after the value's last line stays.
func pairLines(text string, toks token.Tokens, starts []int, ki int) (int, int, bool) {
	key := toks[ki]
	if key.Position == nil {
		return 0, 0, false
	}
	keyAt := starts[ki] + leadLen(key.Origin)
	from := strings.LastIndexByte(text[:keyAt], '\n') + 1
	if strings.Trim(text[from:keyAt], " ") != "" {
		return 0, 0, false // something precedes the key on its line
	}
	end := starts[ki] + len(strings.TrimRight(key.Origin, spaceChars))
	line, col := key.Position.Line, key.Position.Column
	for i := ki + 1; i < len(toks); i++ {
		tk := toks[i]
		if tk.Position == nil {
			return 0, 0, false
		}
		if tk.Type == token.CommentType {
			continue
		}
		blockBody := toks[i-1].Type == token.LiteralType || toks[i-1].Type == token.FoldedType
		inside := tk.Position.Line == line || tk.Position.Column > col || blockBody ||
			(tk.Position.Column == col && tk.Type == token.SequenceEntryType)
		if !inside {
			break
		}
		if e := starts[i] + len(strings.TrimRight(tk.Origin, spaceChars)); e > end {
			end = e
		}
	}
	to := len(text)
	if nl := strings.IndexByte(text[end:], '\n'); nl >= 0 {
		to = end + nl + 1
	}
	if rest := strings.TrimLeft(text[end:to], spaceChars); rest != "" && !strings.HasPrefix(rest, "#") {
		return 0, 0, false // another token shares the value's last line
	}
	return from, to, true
}

// mapAt walks m down path, reporting false when a step is not a mapping.
func mapAt(m *omap.Map, path []string) (*omap.Map, bool) {
	for _, k := range path {
		v, _ := m.Get(k)
		next, ok := v.(*omap.Map)
		if !ok {
			return nil, false
		}
		m = next
	}
	return m, true
}

// leadLen is the length of the run of spaces and breaks goccy puts in front
// of a token's text.
func leadLen(origin string) int {
	return len(origin) - len(strings.TrimLeft(origin, spaceChars))
}

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

// breakChars is the YAML line-break set the emitter also recognises.
const breakChars = "\n\r\u0085\u2028\u2029"

// spaceChars is the whitespace goccy folds into a token's Origin around its text.
const spaceChars = " \t\r\n"

// tokenStarts is each token's byte offset in the text reconcat rebuilds.
func tokenStarts(toks token.Tokens) []int {
	starts := make([]int, len(toks))
	for i, off := 0, 0; i < len(toks); i++ {
		starts[i] = off
		off += len(toks[i].Origin)
	}
	return starts
}

// tokenIndex maps each token's Position.Offset to its index in toks, the
// bridge from an AST node to the token reconcat writes.
func tokenIndex(toks token.Tokens) map[int]int {
	out := make(map[int]int, len(toks))
	for i, tok := range toks {
		out[tok.Position.Offset] = i
	}
	return out
}

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
