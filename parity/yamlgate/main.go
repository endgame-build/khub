// yamlgate — the YAML round-trip gate.
//
// For every YAML-bearing file in the corpus it runs:
//
//	T1a  convenience round-trip: MapSlice decode → Marshal → diff
//	T1b  AST round-trip: parser.ParseBytes(ParseComments) → String() → diff
//	T2   surgical edit: set the first `updated:` value via the AST → diff must
//	     touch only that line
//	T4   canon delete splice: drop each top-level key in turn → the output is
//	     the input with one contiguous run of whole lines removed
//	T5   canon rename splice: rename each top-level key in turn → exactly one
//	     line changes, and only in its key
//	T6   canon nested append: add a key to each nested mapping in turn → the
//	     output is the input with exactly that one line inserted
//	T7   canon list append: add an item to each list under a key in turn → a
//	     block sequence gains exactly that one `- item` line
//
// T1–T2 use no product code; T4–T7 drive canon's splice writer. CI asserts
// the counts.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/lexer"
	"github.com/goccy/go-yaml/parser"
	"github.com/goccy/go-yaml/token"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/omap"
)

type result struct {
	file                              string
	t1a, t1b, t1c, t2, t4, t5, t6, t7 string // "ok" | "diff" | "error: …" | "skip"
	t1aDiff, t1bDiff                  string
	t1cDiff, t2Diff                   string
	t4Diff, t5Diff, t6Diff, t7Diff    string
	t4n, t5n, t6n, t7n                int // splices performed
}

func main() {
	root := "parity/corpus"
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	var files []string
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.Contains(p, "/probe/") {
			return nil
		}
		switch filepath.Ext(p) {
		case ".yaml", ".yml", ".md":
			files = append(files, p)
		}
		return nil
	})
	sort.Strings(files)

	var results []result
	for _, f := range files {
		results = append(results, gate(f))
	}
	report(results)
}

func gate(path string) result {
	r := result{file: path}
	raw, err := os.ReadFile(path)
	if err != nil {
		r.t1a = "error: " + err.Error()
		return r
	}
	src := string(raw)
	prefix, yamlText, suffix := "", src, ""
	if filepath.Ext(path) == ".md" {
		var perr error
		prefix, yamlText, suffix, perr = splitFrontmatter(src)
		if perr != nil {
			r.t1a, r.t1b, r.t2 = "skip", "skip", "skip"
			return r
		}
	}

	// T1a — convenience API round-trip
	func() {
		defer func() {
			if p := recover(); p != nil {
				r.t1a = fmt.Sprintf("error: panic %v", p)
			}
		}()
		var ms yaml.MapSlice
		if err := yaml.Unmarshal([]byte(yamlText), &ms); err != nil {
			r.t1a = "error: " + short(err)
			return
		}
		out, err := yaml.Marshal(ms)
		if err != nil {
			r.t1a = "error: " + short(err)
			return
		}
		r.t1a, r.t1aDiff = diff(yamlText, string(out))
	}()

	// T1b — AST round-trip
	fileAST, err := parser.ParseBytes([]byte(yamlText), parser.ParseComments)
	if err != nil {
		r.t1b = "error: " + short(err)
		r.t2 = "skip"
		return r
	}
	rendered := fileAST.String()
	full := prefix + rendered + suffix
	if filepath.Ext(path) != ".md" {
		full = rendered
	}
	r.t1b, r.t1bDiff = diff(src, full)

	// T1c — lexer token-origin reconstruction (the splice substrate)
	r.t1c, r.t1cDiff = diff(yamlText, reconcat(yamlText))

	// T4, T5 — canon's structural splices over every top-level key
	r.t4, r.t4Diff, r.t4n = opsGate(yamlText, deleteCheck)
	r.t5, r.t5Diff, r.t5n = opsGate(yamlText, renameCheck)
	r.t6, r.t6Diff, r.t6n = appendGate(yamlText)
	r.t7, r.t7Diff, r.t7n = listGate(yamlText)

	// T2 — surgical edit of the first `updated:` scalar via token splice
	edited, found := spliceUpdated(yamlText)
	if !found {
		r.t2 = "skip"
		return r
	}
	editedFull := prefix + edited + suffix
	if filepath.Ext(path) != ".md" {
		editedFull = edited
	}
	r.t2, r.t2Diff = surgicalCheck(src, editedFull)
	return r
}

// reconcat rebuilds source from lexer token origins (plus the final newline
// the last token drops). This byte-identity is what makes splice edits safe.
func reconcat(src string) string {
	tokens := lexer.Tokenize(src)
	var b strings.Builder
	for _, tok := range tokens {
		b.WriteString(tok.Origin)
	}
	out := b.String()
	if strings.HasSuffix(src, "\n") && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out
}

// spliceUpdated replaces the scalar after the first `updated:` key by editing
// that token's origin only, then reconcatenating the untouched stream.
func spliceUpdated(src string) (string, bool) {
	tokens := lexer.Tokenize(src)
	for i := 0; i+2 < len(tokens); i++ {
		if tokens[i].Value == "updated" && tokens[i+1].Type == token.MappingValueType {
			val := tokens[i+2]
			// only a scalar on the key's own line is the entity-meta shape;
			// `updated:` opening a nested block (schema attr decls) is not a target
			if val.Position == nil || tokens[i].Position == nil ||
				val.Position.Line != tokens[i].Position.Line ||
				!strings.Contains(val.Origin, val.Value) {
				continue
			}
			val.Origin = strings.Replace(val.Origin, val.Value, "2026-02-02", 1)
			var b strings.Builder
			for _, tok := range tokens {
				b.WriteString(tok.Origin)
			}
			out := b.String()
			if strings.HasSuffix(src, "\n") && !strings.HasSuffix(out, "\n") {
				out += "\n"
			}
			return out, true
		}
	}
	return "", false
}

// splice is one structural splice of key in src, checked for surgery. It
// returns spliced=false when canon declines (ErrNoSplice), which is policy.
type splice func(src string, have *omap.Map, key string) (spliced bool, diff string)

// opsGate runs op on every top-level key: "ok" when every splice canon made
// was surgical, "skip" when it made none, "diff" on the first that was not.
func opsGate(src string, op splice) (string, string, int) {
	have, nonMap, err := canon.LoadDocMap(src, canon.Mode12)
	if err != nil || nonMap != nil || have == nil || have.Len() == 0 {
		return "skip", "", 0
	}
	n := 0
	for _, key := range have.Keys() {
		spliced, d := op(src, have, key)
		if d != "" {
			return "diff", fmt.Sprintf("key %q: %s", key, d), n
		}
		if spliced {
			n++
		}
	}
	if n == 0 {
		return "skip", "", 0
	}
	return "ok", "", n
}

func copyWithout(m *omap.Map, drop, rename, to string) *omap.Map {
	out := omap.New()
	for _, k := range m.Keys() {
		v, _ := m.Get(k)
		switch k {
		case drop:
			continue
		case rename:
			k = to
		}
		out.Set(k, v)
	}
	return out
}

// deleteCheck drops key: the output must be the input with one contiguous run
// of whole lines removed, the first of them holding the key.
func deleteCheck(src string, have *omap.Map, key string) (bool, string) {
	out, err := canon.SpliceMapping([]byte(src), copyWithout(have, key, "", ""), canon.Mode12)
	if errors.Is(err, canon.ErrNoSplice) {
		return false, ""
	}
	if err != nil {
		return false, err.Error()
	}
	o, e := strings.Split(src, "\n"), strings.Split(string(out), "\n")
	cut := len(o) - len(e)
	if cut <= 0 {
		return true, "no line removed"
	}
	p := 0
	for p < len(e) && o[p] == e[p] {
		p++
	}
	if !slices.Equal(o[p+cut:], e[p:]) {
		return true, fmt.Sprintf("not one contiguous run of lines from line %d", p+1)
	}
	if !strings.Contains(strings.Join(o[p:p+cut], "\n"), key) {
		return true, fmt.Sprintf("removed lines %d-%d do not hold the key", p+1, p+cut)
	}
	return true, ""
}

// renameCheck renames key to a fresh name: exactly one line may change, and
// only its key — the rest of the line keeps its bytes.
func renameCheck(src string, have *omap.Map, key string) (bool, string) {
	fresh := "renamed_key"
	for _, taken := have.Get(fresh); taken; _, taken = have.Get(fresh) {
		fresh += "_"
	}
	out, err := canon.SpliceMapping([]byte(src), copyWithout(have, "", key, fresh), canon.Mode12,
		canon.Rename{Path: []string{key}, To: fresh})
	if errors.Is(err, canon.ErrNoSplice) {
		return false, ""
	}
	if err != nil {
		return false, err.Error()
	}
	o, e := strings.Split(src, "\n"), strings.Split(string(out), "\n")
	if len(o) != len(e) {
		return true, fmt.Sprintf("line count changed: %d -> %d", len(o), len(e))
	}
	changed := -1
	for i := range o {
		if o[i] != e[i] {
			if changed >= 0 {
				return true, fmt.Sprintf("lines %d and %d both changed", changed+1, i+1)
			}
			changed = i
		}
	}
	if changed < 0 {
		return true, "nothing changed"
	}
	ol, el := o[changed], e[changed]
	indent := el[:len(el)-len(strings.TrimLeft(el, " "))]
	tail, ok := strings.CutPrefix(el, indent+fresh)
	if !ok || !strings.HasSuffix(ol, tail) || !strings.Contains(ol[:len(ol)-len(tail)], key) {
		return true, fmt.Sprintf("line %d changed beyond its key: %q -> %q", changed+1, ol, el)
	}
	return true, ""
}

// appendGate adds a fresh key to every nested mapping in turn: "ok" when every
// splice canon made inserted exactly that one line, "skip" when it made none.
func appendGate(src string) (string, string, int) {
	have, nonMap, err := canon.LoadDocMap(src, canon.Mode12)
	if err != nil || nonMap != nil || have == nil {
		return "skip", "", 0
	}
	n := 0
	for _, path := range nestedMaps(have, nil) {
		out, err := canon.SpliceMapping([]byte(src), grownAt(have, path), canon.Mode12)
		if errors.Is(err, canon.ErrNoSplice) {
			continue
		}
		if err != nil {
			return "diff", err.Error(), n
		}
		n++
		o, e := strings.Split(src, "\n"), strings.Split(string(out), "\n")
		if len(e) != len(o)+1 {
			return "diff", fmt.Sprintf("path %q: line count %d -> %d", path, len(o), len(e)), n
		}
		p := 0
		for p < len(o) && o[p] == e[p] {
			p++
		}
		if strings.TrimLeft(e[p], " ") != "added_key: x" || !slices.Equal(o[p:], e[p+1:]) {
			return "diff", fmt.Sprintf("path %q: not one inserted line at line %d", path, p+1), n
		}
	}
	if n == 0 {
		return "skip", "", 0
	}
	return "ok", "", n
}

// listGate appends an item to every list under a key in turn. A block
// sequence must gain exactly one `- added_item` line; a flow list is rewritten
// whole by design and is not counted.
func listGate(src string) (string, string, int) {
	have, nonMap, err := canon.LoadDocMap(src, canon.Mode12)
	if err != nil || nonMap != nil || have == nil {
		return "skip", "", 0
	}
	n := 0
	for _, path := range keyedLists(have, nil) {
		out, err := canon.SpliceMapping([]byte(src), withItem(have, path), canon.Mode12)
		if errors.Is(err, canon.ErrNoSplice) {
			continue
		}
		if err != nil {
			return "diff", err.Error(), n
		}
		o, e := strings.Split(src, "\n"), strings.Split(string(out), "\n")
		if len(e) != len(o)+1 {
			// Only a flow list may differ by more than the item: its pair is
			// rewritten in block style.
			key := path[len(path)-1]
			if !slices.ContainsFunc(o, func(l string) bool { return strings.Contains(l, key) && strings.Contains(l, "[") }) {
				return "diff", fmt.Sprintf("path %q: block list gained %d lines", path, len(e)-len(o)), n
			}
			continue
		}
		n++
		p := 0
		for p < len(o) && o[p] == e[p] {
			p++
		}
		if strings.TrimLeft(e[p], " ") != "- added_item" || !slices.Equal(o[p:], e[p+1:]) {
			return "diff", fmt.Sprintf("path %q: not one inserted item line at line %d", path, p+1), n
		}
	}
	if n == 0 {
		return "skip", "", 0
	}
	return "ok", "", n
}

// keyedLists lists the path of every non-empty list held under a mapping key.
func keyedLists(m *omap.Map, path []string) [][]string {
	var out [][]string
	for _, k := range m.Keys() {
		v, _ := m.Get(k)
		here := append(slices.Clone(path), k)
		switch x := v.(type) {
		case *omap.Map:
			out = append(out, keyedLists(x, here)...)
		case []any:
			if len(x) > 0 {
				out = append(out, here)
			}
		}
	}
	return out
}

// withItem copies m with `added_item` appended to the list at path.
func withItem(m *omap.Map, path []string) *omap.Map {
	out := copyWithout(m, "", "", "")
	v, _ := m.Get(path[0])
	if len(path) == 1 {
		out.Set(path[0], append(slices.Clone(v.([]any)), "added_item"))
		return out
	}
	out.Set(path[0], withItem(v.(*omap.Map), path[1:]))
	return out
}

// nestedMaps lists the path of every mapping below the root.
func nestedMaps(m *omap.Map, path []string) [][]string {
	var out [][]string
	for _, k := range m.Keys() {
		v, _ := m.Get(k)
		if sub, ok := v.(*omap.Map); ok {
			here := append(slices.Clone(path), k)
			out = append(out, here)
			out = append(out, nestedMaps(sub, here)...)
		}
	}
	return out
}

// grownAt copies m with `added_key: x` appended to the mapping at path.
func grownAt(m *omap.Map, path []string) *omap.Map {
	out := copyWithout(m, "", "", "")
	if len(path) == 0 {
		out.Set("added_key", "x")
		return out
	}
	v, _ := m.Get(path[0])
	out.Set(path[0], grownAt(v.(*omap.Map), path[1:]))
	return out
}

// splitFrontmatter ports formats._split_frontmatter: returns the text before
// the YAML (fence + skipped blanks), the YAML text, and the body remainder
// (including the closing fence line) so the original reassembles by
// concatenation.
func splitFrontmatter(text string) (prefix, yamlText, suffix string, err error) {
	work := strings.TrimPrefix(text, "\uFEFF")
	lines := strings.Split(work, "\n")
	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	if start >= len(lines) || lines[start] != "---" {
		return "", "", "", fmt.Errorf("no frontmatter fence")
	}
	for i := start + 1; i < len(lines); i++ {
		if lines[i] == "---" {
			prefix = strings.Join(lines[:start+1], "\n") + "\n"
			yamlText = strings.Join(lines[start+1:i], "\n") + "\n"
			suffix = "---" + "\n" + strings.Join(lines[i+1:], "\n")
			if i == len(lines)-1 {
				suffix = "---"
			}
			return prefix, yamlText, suffix, nil
		}
	}
	return "", "", "", fmt.Errorf("unterminated frontmatter")
}

func diff(want, got string) (string, string) {
	if want == got {
		return "ok", ""
	}
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := 0; i < len(w) || i < len(g); i++ {
		wl, gl := "", ""
		if i < len(w) {
			wl = w[i]
		}
		if i < len(g) {
			gl = g[i]
		}
		if wl != gl {
			return "diff", fmt.Sprintf("line %d: want %q, got %q", i+1, wl, gl)
		}
	}
	return "diff", "length mismatch"
}

// surgicalCheck: exactly the lines mentioning the old/new updated value may
// change; everything else must be byte-identical.
func surgicalCheck(orig, edited string) (string, string) {
	o, e := strings.Split(orig, "\n"), strings.Split(edited, "\n")
	if len(o) != len(e) {
		return "diff", fmt.Sprintf("line count changed: %d -> %d", len(o), len(e))
	}
	changed := 0
	for i := range o {
		if o[i] == e[i] {
			continue
		}
		changed++
		if !strings.Contains(o[i], "updated") || !strings.Contains(e[i], "2026-02-02") {
			return "diff", fmt.Sprintf("non-surgical change at line %d: %q -> %q", i+1, o[i], e[i])
		}
	}
	if changed != 1 {
		return "diff", fmt.Sprintf("%d lines changed, want exactly 1", changed)
	}
	return "ok", ""
}

func short(err error) string {
	s := err.Error()
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

func report(results []result) {
	counts := map[string]map[string]int{"t1a": {}, "t1b": {}, "t1c": {}, "t2": {}, "t4": {}, "t5": {}, "t6": {}, "t7": {}}
	t4n, t5n, t6n, t7n := 0, 0, 0, 0
	for _, r := range results {
		counts["t1a"][kind(r.t1a)]++
		counts["t1b"][kind(r.t1b)]++
		counts["t1c"][kind(r.t1c)]++
		counts["t2"][kind(r.t2)]++
		counts["t4"][kind(r.t4)]++
		counts["t5"][kind(r.t5)]++
		counts["t6"][kind(r.t6)]++
		counts["t7"][kind(r.t7)]++
		t4n += r.t4n
		t5n += r.t5n
		t6n += r.t6n
		t7n += r.t7n
	}
	fmt.Printf("files: %d\n", len(results))
	for _, t := range []string{"t1a", "t1b", "t1c", "t2", "t4", "t5", "t6", "t7"} {
		fmt.Printf("%s: ok=%d diff=%d error=%d skip=%d\n", t,
			counts[t]["ok"], counts[t]["diff"], counts[t]["error"], counts[t]["skip"])
	}
	fmt.Printf("splices: t4=%d t5=%d t6=%d t7=%d\n", t4n, t5n, t6n, t7n)
	fmt.Println()
	for _, r := range results {
		if kind(r.t1c) == "ok" && kind(r.t2) != "diff" && kind(r.t4) != "diff" &&
			kind(r.t5) != "diff" && kind(r.t6) != "diff" && kind(r.t7) != "diff" {
			continue
		}
		fmt.Printf("%s\n  t1c=%s %s\n  t2=%s %s\n  t4=%s %s\n  t5=%s %s\n  t6=%s %s\n  t7=%s %s\n", r.file,
			r.t1c, r.t1cDiff, r.t2, r.t2Diff, r.t4, r.t4Diff, r.t5, r.t5Diff, r.t6, r.t6Diff, r.t7, r.t7Diff)
	}
}

func kind(s string) string {
	switch s {
	case "ok", "":
		return "ok"
	case "diff":
		return "diff"
	case "skip":
		return "skip"
	default:
		return "error"
	}
}
