// yamlgate — the YAML round-trip gate.
//
// For every YAML-bearing file in the corpus it runs:
//
//	T1a  convenience round-trip: MapSlice decode → Marshal → diff
//	T1b  AST round-trip: parser.ParseBytes(ParseComments) → String() → diff
//	T2   surgical edit: set the first `updated:` value via the AST → diff must
//	     touch only that line
//
// Zero product code; CI asserts its counts.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/lexer"
	"github.com/goccy/go-yaml/parser"
	"github.com/goccy/go-yaml/token"
)

type result struct {
	file              string
	t1a, t1b, t1c, t2 string // "ok" | "diff" | "error: …" | "skip"
	t1aDiff, t1bDiff  string
	t1cDiff, t2Diff   string
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
	counts := map[string]map[string]int{"t1a": {}, "t1b": {}, "t1c": {}, "t2": {}}
	for _, r := range results {
		counts["t1a"][kind(r.t1a)]++
		counts["t1b"][kind(r.t1b)]++
		counts["t1c"][kind(r.t1c)]++
		counts["t2"][kind(r.t2)]++
	}
	fmt.Printf("files: %d\n", len(results))
	for _, t := range []string{"t1a", "t1b", "t1c", "t2"} {
		fmt.Printf("%s: ok=%d diff=%d error=%d skip=%d\n", t,
			counts[t]["ok"], counts[t]["diff"], counts[t]["error"], counts[t]["skip"])
	}
	fmt.Println()
	for _, r := range results {
		if kind(r.t1c) == "ok" && kind(r.t2) != "diff" {
			continue
		}
		fmt.Printf("%s\n  t1c=%s %s\n  t2=%s %s\n", r.file, r.t1c, r.t1cDiff, r.t2, r.t2Diff)
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
