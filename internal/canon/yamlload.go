package canon

// Read half of the YAML canon: goccy AST → *omap.Map with ruamel-compatible
// scalar resolution. goccy's own Unmarshal resolves YAML
// scalars by its own rules and loses key order; khub needs ruamel 1.2
// semantics (yes/on are strings, 2026-01-15 is a date, 017 is an int) and
// insertion order, so resolution happens here over plain-scalar tokens using
// the same resolver table the emitter quotes against.

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
	"github.com/goccy/go-yaml/token"

	"github.com/endgame-build/khub/internal/omap"
)

// ResolveMode selects the Python loader being mirrored: python-frontmatter's
// PyYAML SafeLoader (YAML 1.1 — md frontmatter) or ruamel (YAML 1.2 — yaml
// entities and the RT edit path). The two genuinely diverge (yes/on, 017,
// sexagesimals); parity/corpus/load-cases.jsonl records both.
type ResolveMode int

const (
	Mode12 ResolveMode = iota // ruamel YAML 1.2
	Mode11                    // PyYAML SafeLoader YAML 1.1
)

// LoadDocMap parses one YAML document into an ordered map. A nil/empty
// document returns an empty map; a non-mapping document returns the value
// itself via LoadDoc for the caller's malformed-entity error.
func LoadDocMap(text string, mode ResolveMode) (*omap.Map, any, error) {
	v, err := LoadDocMode(text, mode)
	if err != nil {
		return nil, nil, err
	}
	if v == nil {
		return omap.New(), nil, nil
	}
	if m, ok := v.(*omap.Map); ok {
		return m, nil, nil
	}
	return nil, v, nil
}

// LoadDoc parses one YAML document with ruamel 1.2 resolution.
func LoadDoc(text string) (any, error) { return LoadDocMode(text, Mode12) }

// LoadDocMode parses one YAML document, resolving plain scalars as YAML 1.1
// (Mode11, the scan altitude) or 1.2 (Mode12, the edit altitude).
func LoadDocMode(text string, mode ResolveMode) (any, error) {
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	// PyYAML's and ruamel's Reader reject non-printable characters outright,
	// so a control character anywhere in the document makes the file malformed
	// in Python. goccy accepts them, which silently turned a malformed file
	// into a live entity in Go — found by the differential fuzzer, as a
	// one-entity difference in `status` counts.
	if i, ch, bad := firstNonPrintable(text); bad {
		return nil, fmt.Errorf("unacceptable character #x%04x: control characters are not allowed\n"+
			"  in \"<unicode string>\", position %d", ch, i)
	}
	f, err := parser.ParseBytes([]byte(text), 0)
	if err != nil {
		return nil, err
	}
	if len(f.Docs) == 0 || f.Docs[0].Body == nil {
		return nil, nil
	}
	anchors := map[string]any{}
	r := &resolver{mode: mode}
	return r.node(f.Docs[0].Body, anchors)
}

type resolver struct{ mode ResolveMode }

func (r *resolver) node(n ast.Node, anchors map[string]any) (any, error) {
	switch x := n.(type) {
	case *ast.MappingNode:
		m := omap.New()
		for _, mv := range x.Values {
			if err := r.entry(mv, m, anchors); err != nil {
				return nil, err
			}
		}
		return m, nil
	case *ast.MappingValueNode: // single-pair mapping parses as this
		m := omap.New()
		if err := r.entry(x, m, anchors); err != nil {
			return nil, err
		}
		return m, nil
	case *ast.SequenceNode:
		items := []any{}
		for _, it := range x.Values {
			v, err := r.node(it, anchors)
			if err != nil {
				return nil, err
			}
			items = append(items, v)
		}
		return items, nil
	case *ast.AnchorNode:
		v, err := r.node(x.Value, anchors)
		if err != nil {
			return nil, err
		}
		anchors[x.Name.GetToken().Value] = v
		return v, nil
	case *ast.AliasNode:
		name := x.Value.GetToken().Value
		v, ok := anchors[name]
		if !ok {
			return nil, fmt.Errorf("unknown alias '%s'", name)
		}
		return v, nil
	case *ast.NullNode:
		return nil, nil
	case *ast.StringNode, *ast.IntegerNode, *ast.FloatNode, *ast.BoolNode,
		*ast.InfinityNode, *ast.NanNode, *ast.LiteralNode:
		return r.scalar(n)
	case *ast.TagNode:
		return r.node(x.Value, anchors)
	case *ast.MappingKeyNode: // explicit `? key` — what the emitter writes past 128 chars
		return r.node(x.Value, anchors)
	default:
		return nil, fmt.Errorf("unsupported YAML node %T", n)
	}
}

func (r *resolver) entry(mv *ast.MappingValueNode, m *omap.Map, anchors map[string]any) error {
	keyAny, err := r.node(mv.Key, anchors)
	if err != nil {
		return err
	}
	key, ok := scalarKeyString(keyAny)
	if !ok {
		return fmt.Errorf("unsupported mapping key %v", keyAny)
	}
	if key == "<<" { // YAML merge: existing keys win, merges apply in order
		val, err := r.node(mv.Value, anchors)
		if err != nil {
			return err
		}
		merges := []any{val}
		if lst, isList := val.([]any); isList {
			merges = lst
		}
		for _, mg := range merges {
			if mm, isMap := mg.(*omap.Map); isMap {
				for _, k := range mm.Keys() {
					if _, exists := m.Get(k); !exists {
						v, _ := mm.Get(k)
						m.Set(k, v)
					}
				}
			}
		}
		return nil
	}
	if _, dup := m.Get(key); dup {
		return fmt.Errorf("duplicate key '%s'", key)
	}
	val, err := r.node(mv.Value, anchors)
	if err != nil {
		return err
	}
	m.Set(key, val)
	return nil
}

func scalarKeyString(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case int64:
		return strconv.FormatInt(x, 10), true
	case bool:
		if x {
			return "true", true
		}
		return "false", true
	case nil:
		return "", true
	case Date:
		return x.ISO, true
	default:
		return "", false
	}
}

// resolveScalar applies ruamel 1.2 resolution to one scalar node. Quoted and
// block scalars are strings regardless of content; plain scalars resolve via
// the resolver table shared with the emitter.
func (r *resolver) scalar(n ast.Node) (any, error) {
	tok := n.GetToken()
	switch tok.Type {
	case token.SingleQuoteType, token.DoubleQuoteType:
		return n.(interface{ GetValue() any }).(*ast.StringNode).Value, nil
	}
	if lit, ok := n.(*ast.LiteralNode); ok {
		return lit.Value.Value, nil
	}
	s := tok.Value
	return ResolvePlainScalarMode(s, r.mode), nil
}

// firstNonPrintable reports the first character PyYAML's Reader would reject.
// Its NON_PRINTABLE class allows tab, LF, CR, printable ASCII, NEL, and the
// usual Unicode ranges; everything else is an error.
func firstNonPrintable(s string) (pos int, ch rune, bad bool) {
	for i, r := range s {
		switch {
		case r == 0x09 || r == 0x0a || r == 0x0d:
		case r >= 0x20 && r <= 0x7e:
		case r == 0x85:
		case r >= 0xa0 && r <= 0xd7ff:
		case r >= 0xe000 && r <= 0xfffd:
		case r >= 0x10000 && r <= 0x10ffff:
		default:
			return i, r, true
		}
	}
	return 0, 0, false
}

// ResolvePlainScalar maps a plain YAML scalar to its ruamel-1.2 value.
func ResolvePlainScalar(s string) any { return ResolvePlainScalarMode(s, Mode12) }

var (
	pyBool11  = regexp.MustCompile(`^(?:yes|Yes|YES|no|No|NO|true|True|TRUE|false|False|FALSE|on|On|ON|off|Off|OFF)$`)
	pyInt11   = regexp.MustCompile(`^(?:[-+]?0b[0-1_]+|[-+]?0[0-7_]+|[-+]?(?:0|[1-9][0-9_]*)|[-+]?0x[0-9a-fA-F_]+|[-+]?[1-9][0-9_]*(?::[0-5]?[0-9])+)$`)
	pyFloat11 = regexp.MustCompile(`^(?:[-+]?(?:[0-9][0-9_]*)\.[0-9_]*(?:[eE][-+]?[0-9]+)?|\.[0-9_]+(?:[eE][-+]?[0-9]+)?|[-+]?[0-9][0-9_]*(?::[0-5]?[0-9])+\.[0-9_]*|[-+]?\.(?:inf|Inf|INF)|\.(?:nan|NaN|NAN))$`)
	pyTS11    = regexp.MustCompile(`^(?:[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]|[0-9][0-9][0-9][0-9]-[0-9][0-9]?-[0-9][0-9]?(?:[Tt]|[ 	]+)[0-9][0-9]?:[0-9][0-9]:[0-9][0-9](?:\.[0-9]*)?(?:[ 	]*(?:Z|[-+][0-9][0-9]?(?::[0-9][0-9])?))?)$`)
	sexa      = regexp.MustCompile(`^[-+]?[1-9][0-9_]*(?::[0-5]?[0-9])+$`)
)

// ResolvePlainScalarMode types a plain scalar as YAML 1.1 or 1.2 would:
// the two differ on yes/no/on/off, octals and sexagesimals.
func ResolvePlainScalarMode(s string, mode ResolveMode) any {
	if mode == Mode11 {
		switch s {
		case "~", "null", "Null", "NULL", "":
			return nil
		}
		if pyBool11.MatchString(s) {
			low := strings.ToLower(s)
			return low == "yes" || low == "true" || low == "on"
		}
		if pyInt11.MatchString(s) {
			if sexa.MatchString(s) {
				return parseSexagesimal(s)
			}
			return parseYAMLInt11(s)
		}
		if pyFloat11.MatchString(s) {
			return parseYAMLFloat(s)
		}
		if pyTS11.MatchString(s) {
			if len(s) == 10 && !strings.ContainsAny(s, "Tt :") {
				return Date{ISO: s}
			}
			return DateTime{ISO: s}
		}
		return s
	}
	switch s {
	case "~", "null", "Null", "NULL", "":
		return nil
	case "true", "True", "TRUE":
		return true
	case "false", "False", "FALSE":
		return false
	}
	if resolvers[2].MatchString(s) { // int
		return parseYAMLInt(s)
	}
	if resolvers[1].MatchString(s) { // float
		return parseYAMLFloat(s)
	}
	if resolvers[5].MatchString(s) { // timestamp
		if len(s) == 10 {
			return Date{ISO: s}
		}
		return DateTime{ISO: s}
	}
	return s
}

// parseYAMLInt11: PyYAML 1.1 — a leading 0 means octal (017 == 15).
func parseYAMLInt11(s string) any {
	t := strings.ReplaceAll(s, "_", "")
	neg := strings.HasPrefix(t, "-")
	t = strings.TrimLeft(t, "+-")
	base := 10
	switch {
	case strings.HasPrefix(t, "0b"):
		t, base = t[2:], 2
	case strings.HasPrefix(t, "0x"):
		t, base = t[2:], 16
	case len(t) > 1 && strings.HasPrefix(t, "0"):
		t, base = t[1:], 8
	}
	if n, err := strconv.ParseInt(t, base, 64); err == nil {
		if neg {
			n = -n
		}
		return n
	}
	if neg {
		t = "-" + t
	}
	return BigInt{Literal: t}
}

func parseSexagesimal(s string) any {
	t := strings.ReplaceAll(s, "_", "")
	neg := strings.HasPrefix(t, "-")
	t = strings.TrimLeft(t, "+-")
	var total int64
	for _, part := range strings.Split(t, ":") {
		n, _ := strconv.ParseInt(part, 10, 64)
		total = total*60 + n
	}
	if neg {
		total = -total
	}
	return total
}

func parseYAMLInt(s string) any {
	t := strings.ReplaceAll(s, "_", "")
	neg := strings.HasPrefix(t, "-")
	t = strings.TrimLeft(t, "+-")
	base := 10
	switch {
	case strings.HasPrefix(t, "0b"):
		t, base = t[2:], 2
	case strings.HasPrefix(t, "0o"):
		t, base = t[2:], 8
	case strings.HasPrefix(t, "0x"):
		t, base = t[2:], 16
	}
	if t == "" {
		// The 1.2 int pattern admits `_` and `0x_`: no digit at all once the
		// separators go. ruamel raises on those; khub cannot raise from a
		// resolver, and an empty integer literal would emit as nothing and load
		// back as null (found by FuzzEmitRoundTrip). It stays the string it is.
		return s
	}
	if n, err := strconv.ParseInt(t, base, 64); err == nil {
		if neg {
			n = -n
		}
		return n
	}
	if neg {
		t = "-" + t
	}
	return BigInt{Literal: t} // decimal big ints only; radix big ints are unreachable in khub data
}

func parseYAMLFloat(s string) float64 {
	t := strings.ReplaceAll(s, "_", "")
	low := strings.ToLower(strings.TrimLeft(t, "+-"))
	switch low {
	case ".inf":
		if strings.HasPrefix(t, "-") {
			f, _ := strconv.ParseFloat("-Inf", 64)
			return f
		}
		f, _ := strconv.ParseFloat("+Inf", 64)
		return f
	case ".nan":
		f, _ := strconv.ParseFloat("NaN", 64)
		return f
	}
	f, _ := strconv.ParseFloat(t, 64)
	return f
}
