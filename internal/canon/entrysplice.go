package canon

// SpliceEntry is SpliceMapping's twin for a schema layer file: it replaces one
// nested MAPPING entry — `storage.<type>` when `khub layout migrate` rewrites
// a type's storage — and leaves every other byte of the document alone.
//
// SpliceMapping edits same-line scalars at any depth and appends a key at the
// root only; a key added inside a nested block (`format: json` arriving on a
// type that never declared one) is not expressible there and would fall back
// to a whole-document re-emit, which drops the layer's provenance header and
// every comment beside the other types. A storage entry is small and its own
// comments are the only ones at stake, so this splice works at the granularity
// of the entry: the lines the entry occupies are replaced by a fresh rendering
// of it, and the rest of the file survives by construction.
//
// An entry authored in flow style (`{ layout: file, path: x }`) stays flow
// when every value it carries is a scalar; otherwise, and for an entry
// authored in block style, it is re-emitted in block style at its own
// indent. A key the parent mapping lacks is appended after the parent's last
// entry; a parent that is missing altogether is appended at the end of the
// document. ErrNoSplice reports a document the splice cannot address (a flow
// parent, an empty parent, a document the lexer does not reconstruct
// byte-for-byte); the caller re-emits instead.

import (
	"strings"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/lexer"
	"github.com/goccy/go-yaml/parser"

	"github.com/endgame-build/khub/internal/omap"
)

// SpliceEntry rewrites the text src so the mapping at path (a chain of keys
// from the root) reads back as entry, touching only the lines that entry
// occupies. mode is the resolution the caller reads the document under.
func SpliceEntry(src []byte, path []string, entry *omap.Map, mode ResolveMode) ([]byte, error) {
	if len(path) == 0 || entry == nil {
		return nil, ErrNoSplice
	}
	text := string(src)
	if reconcat(lexer.Tokenize(text), text) != text {
		return nil, ErrNoSplice
	}
	have, nonMap, err := LoadDocMap(text, mode)
	if err != nil || nonMap != nil {
		return nil, ErrNoSplice
	}
	var body ast.Node
	if strings.TrimSpace(text) != "" {
		f, err := parser.ParseBytes(src, 0)
		if err != nil || len(f.Docs) == 0 || f.Docs[0].Body == nil {
			return nil, ErrNoSplice
		}
		body = f.Docs[0].Body
	}
	lines := strings.SplitAfter(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	out, err := spliceEntryLines(lines, body, path, entry)
	if err != nil {
		return nil, err
	}
	// The spliced text must read back as the whole document with just this
	// entry replaced; anything else never ships.
	want := setPath(have, path, entry)
	check, checkNonMap, cerr := LoadDocMap(out, mode)
	if cerr != nil || checkNonMap != nil || !want || !equalValue(check, have) {
		return nil, ErrNoSplice
	}
	return []byte(out), nil
}

// setPath stores entry at path inside m, creating the mappings between; it
// reports false when a key on the way holds a non-mapping value.
func setPath(m *omap.Map, path []string, entry *omap.Map) bool {
	cur := m
	for _, k := range path[:len(path)-1] {
		v, has := cur.Get(k)
		if !has || v == nil {
			next := omap.New()
			cur.Set(k, next)
			cur = next
			continue
		}
		next, ok := v.(*omap.Map)
		if !ok {
			return false
		}
		cur = next
	}
	cur.Set(path[len(path)-1], entry)
	return true
}

// spliceEntryLines locates the entry and rebuilds the document line by line.
func spliceEntryLines(lines []string, body ast.Node, path []string, entry *omap.Map) (string, error) {
	parent, ok := mappingAt(body, path[:len(path)-1])
	key := path[len(path)-1]
	if !ok {
		if len(path) > 1 {
			if _, present := findPair(body, path[0]); present {
				return "", ErrNoSplice // the parent exists but is not a block mapping
			}
		}
		// No parent at all: append the whole chain at the end of the document.
		nested := entry
		for i := len(path) - 1; i >= 0; i-- {
			nested = oneKey(path[i], nested)
		}
		block, err := DumpWide(nested)
		if err != nil {
			return "", ErrNoSplice
		}
		return endedLine(joinLines(lines)) + block, nil
	}
	if parent.Start != nil && parent.Start.Value == "{" {
		return "", ErrNoSplice // a flow parent has no lines of its own to splice
	}
	if len(parent.Values) == 0 {
		return "", ErrNoSplice
	}
	var pair *ast.MappingValueNode
	for _, mv := range parent.Values {
		if keyText(mv) == key {
			pair = mv
			break
		}
	}
	if pair == nil {
		// Append after the parent's last entry, at the children's indent.
		last := parent.Values[len(parent.Values)-1]
		keyTok := last.Key.GetToken()
		if keyTok == nil || keyTok.Position == nil {
			return "", ErrNoSplice
		}
		end := lastLine(last)
		if end > len(lines) {
			return "", ErrNoSplice
		}
		block, err := renderBlockEntry(key, entry, keyTok.Position.Column-1)
		if err != nil {
			return "", err
		}
		return endedLine(joinLines(lines[:end])) + block + joinLines(lines[end:]), nil
	}
	keyTok := pair.Key.GetToken()
	if keyTok == nil || keyTok.Position == nil {
		return "", ErrNoSplice
	}
	first := keyTok.Position.Line
	end := lastLine(pair)
	if first < 1 || end > len(lines) || first > end {
		return "", ErrNoSplice
	}
	// The key must open its line: an entry inside a flow mapping or a sequence
	// item shares the line with something the splice cannot keep.
	if strings.TrimSpace(lines[first-1][:keyTok.Position.Column-1]) != "" {
		return "", ErrNoSplice
	}
	indent := keyTok.Position.Column - 1
	var rendered string
	var err error
	if m, isMap := pair.Value.(*ast.MappingNode); isMap && m.Start != nil && m.Start.Value == "{" {
		rendered, err = renderFlowEntry(key, entry, indent)
	}
	if rendered == "" && err == nil {
		rendered, err = renderBlockEntry(key, entry, indent)
	}
	if err != nil {
		return "", err
	}
	return joinLines(lines[:first-1]) + rendered + joinLines(lines[end:]), nil
}

// mappingAt walks path from body and returns the block or flow mapping it
// names; the empty path is the root mapping.
func mappingAt(body ast.Node, path []string) (*ast.MappingNode, bool) {
	cur := body
	for _, k := range path {
		mv, ok := findPair(cur, k)
		if !ok {
			return nil, false
		}
		cur = mv.Value
	}
	switch x := cur.(type) {
	case *ast.MappingNode:
		return x, true
	case *ast.MappingValueNode:
		return &ast.MappingNode{Values: []*ast.MappingValueNode{x}}, true
	}
	return nil, false
}

// findPair returns the pair of a mapping node keyed k.
func findPair(n ast.Node, k string) (*ast.MappingValueNode, bool) {
	switch x := n.(type) {
	case *ast.MappingNode:
		for _, mv := range x.Values {
			if keyText(mv) == k {
				return mv, true
			}
		}
	case *ast.MappingValueNode:
		if keyText(x) == k {
			return x, true
		}
	}
	return nil, false
}

func keyText(mv *ast.MappingValueNode) string {
	if mv == nil || mv.Key == nil {
		return ""
	}
	tok := mv.Key.GetToken()
	if tok == nil {
		return ""
	}
	return tok.Value
}

// lastLine is the last document line a node's tokens reach: a token on its
// own line, plus the lines a block scalar's origin spans.
func lastLine(n ast.Node) int {
	last := 0
	var walk func(ast.Node)
	walk = func(n ast.Node) {
		if n == nil {
			return
		}
		if tok := n.GetToken(); tok != nil && tok.Position != nil {
			origin := strings.TrimRight(strings.TrimLeft(tok.Origin, "\n"), " \t\n")
			if end := tok.Position.Line + strings.Count(origin, "\n"); end > last {
				last = end
			}
		}
		switch x := n.(type) {
		case *ast.MappingNode:
			for _, mv := range x.Values {
				walk(mv)
			}
			if x.End != nil && x.End.Position != nil && x.End.Position.Line > last {
				last = x.End.Position.Line
			}
		case *ast.MappingValueNode:
			walk(x.Key)
			walk(x.Value)
		case *ast.SequenceNode:
			for _, v := range x.Values {
				walk(v)
			}
			if x.End != nil && x.End.Position != nil && x.End.Position.Line > last {
				last = x.End.Position.Line
			}
		}
	}
	walk(n)
	return last
}

// renderBlockEntry renders `key:` and its block body, every line indented.
func renderBlockEntry(key string, entry *omap.Map, indent int) (string, error) {
	text, err := DumpWide(oneKey(key, entry))
	if err != nil {
		return "", ErrNoSplice
	}
	pad := strings.Repeat(" ", indent)
	var b strings.Builder
	for _, line := range strings.SplitAfter(strings.TrimSuffix(text, "\n"), "\n") {
		b.WriteString(pad + line)
	}
	b.WriteString("\n")
	return b.String(), nil
}

// renderFlowEntry renders `key: { k: v, k: v }` on one line, or "" when a
// value is not a scalar (the caller falls back to block style).
func renderFlowEntry(key string, entry *omap.Map, indent int) (string, error) {
	parts := make([]string, 0, entry.Len())
	for _, k := range entry.Keys() {
		v, _ := entry.Get(k)
		switch v.(type) {
		case *omap.Map, []any:
			return "", nil
		}
		s, err := inlineScalar(v)
		if err != nil {
			return "", ErrNoSplice
		}
		ks, err := inlineScalar(k)
		if err != nil {
			return "", ErrNoSplice
		}
		parts = append(parts, ks+": "+s)
	}
	body := "{}"
	if len(parts) > 0 {
		body = "{ " + strings.Join(parts, ", ") + " }"
	}
	return strings.Repeat(" ", indent) + key + ": " + body + "\n", nil
}

// inlineScalar renders one scalar the way the wide emitter would on a key's
// line, without the leading space or the line break.
func inlineScalar(v any) (string, error) {
	if s, ok := v.(string); ok {
		e := &emitter{width: 4096}
		e.writeScalarStr(s, 0, false, false)
		return e.b.String(), nil
	}
	return scalarString(v)
}

func joinLines(lines []string) string {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l)
	}
	return b.String()
}

// endedLine closes an unterminated last line so an appended block starts on
// a line of its own.
func endedLine(s string) string {
	if s != "" && !strings.HasSuffix(s, "\n") {
		return s + "\n"
	}
	return s
}
