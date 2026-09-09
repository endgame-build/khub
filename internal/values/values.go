// Package values ports core/values.py: the shared value predicates the write
// gate and the integrity gate both use, so a value the write path accepts is
// exactly a value validate accepts.
package values

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/omap"
)

var boolish = map[string]bool{
	"true": true, "false": true, "yes": true, "no": true,
	"1": true, "0": true, "on": true, "off": true,
}
var trueish = map[string]bool{"true": true, "yes": true, "1": true, "on": true}

// Present reports whether a value counts as set: nil, blank strings and empty
// lists do not.
func Present(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(x) != ""
	case []any:
		return len(x) > 0
	default:
		return true
	}
}

// IsBool reports whether v is a bool or one of the YAML 1.1 boolean words.
func IsBool(v any) bool {
	switch x := v.(type) {
	case bool:
		return true
	case string:
		return boolish[strings.ToLower(strings.TrimSpace(x))]
	default:
		return false
	}
}

// AsBool coerces v to a bool the way the Python build did: true-ish words,
// non-zero numbers and non-empty lists are true.
func AsBool(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return trueish[strings.ToLower(strings.TrimSpace(x))]
	case nil:
		return false
	case int64:
		return x != 0
	case float64:
		return x != 0
	case []any:
		return len(x) > 0
	case *omap.Map:
		return x.Len() > 0
	case map[string]any:
		return len(x) > 0
	default:
		return true
	}
}

// IsNumber mirrors is_number: Python float() acceptance (underscores,
// whitespace, inf/nan spellings) then a finiteness gate.
func IsNumber(v any) bool {
	switch x := v.(type) {
	case bool:
		return false
	case int, int64:
		return true
	case canon.BigInt:
		return true
	case float64:
		return !math.IsInf(x, 0) && !math.IsNaN(x)
	case string:
		f, ok := pyParseFloat(x)
		return ok && !math.IsInf(f, 0) && !math.IsNaN(f)
	default:
		return false
	}
}

// pyParseFloat follows Python float(str): strip whitespace, allow single
// underscores between digits, accept inf/infinity/nan spellings.
func pyParseFloat(s string) (float64, bool) {
	t := strings.TrimSpace(s)
	if t == "" {
		return 0, false
	}
	low := strings.ToLower(strings.TrimLeft(t, "+-"))
	if low == "inf" || low == "infinity" || low == "nan" {
		sign := 1.0
		if strings.HasPrefix(t, "-") {
			sign = -1
		}
		if low == "nan" {
			return math.NaN(), true
		}
		return sign * math.Inf(1), true
	}
	// underscores: only between digits (Python grammar); reject otherwise
	if strings.Contains(t, "_") {
		var b strings.Builder
		runes := []rune(t)
		for i, ch := range runes {
			if ch == '_' {
				if i == 0 || i == len(runes)-1 || !isDigit(runes[i-1]) || !isDigit(runes[i+1]) {
					return 0, false
				}
				continue
			}
			b.WriteRune(ch)
		}
		t = b.String()
	}
	// Go ParseFloat accepts hex floats and "Inf"; Python float() does not accept hex
	if strings.ContainsAny(t, "xX") {
		return 0, false
	}
	f, err := strconv.ParseFloat(t, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

func isDigit(ch rune) bool { return ch >= '0' && ch <= '9' }

// IsDateish mirrors is_dateish: Python 3.11 date/datetime.fromisoformat over
// str(value). Dates and datetimes pass as-is.
func IsDateish(v any) bool {
	switch v.(type) {
	case canon.Date, canon.DateTime:
		return true
	}
	return isoParses(Str(v))
}

// Str mirrors Python str() for the value shapes khub holds.
func Str(v any) string {
	switch x := v.(type) {
	case nil:
		return "None"
	case bool:
		if x {
			return "True"
		}
		return "False"
	case string:
		return x
	case int64:
		return strconv.FormatInt(x, 10)
	case canon.BigInt:
		return x.Literal
	case float64:
		return canon.PyFloatRepr(x)
	case canon.Date:
		return x.ISO
	case canon.DateTime:
		return x.ISO
	case int:
		return strconv.Itoa(x)
	case []any:
		return pyListRepr(x)
	case *omap.Map:
		return pyDictRepr(x)
	default:
		// CLAUDE.md: no silent fallbacks. Returning "" here rendered a
		// mapping-valued relation as an empty target in `check --format json`
		// where Python printed its repr — a wrong value that looked like a
		// legitimately absent one.
		return fmt.Sprintf("%v", v)
	}
}

// pyListRepr / pyDictRepr mirror Python's str() for containers, which is what
// khub prints when a relation value is not the scalar the schema expects.
func pyListRepr(items []any) string {
	parts := make([]string, len(items))
	for i, it := range items {
		parts[i] = pyReprElem(it)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func pyDictRepr(m *omap.Map) string {
	parts := make([]string, 0, m.Len())
	for _, k := range m.Keys() {
		v, _ := m.Get(k)
		parts = append(parts, "'"+k+"': "+pyReprElem(v))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// pyReprElem is repr() for an element inside a container: strings are quoted.
func pyReprElem(v any) string {
	if s, ok := v.(string); ok {
		return "'" + s + "'"
	}
	return Str(v)
}

var isoLayouts = []string{
	"2006-01-02",
	"20060102",
	"2006-01-02T15:04",
	"2006-01-02T15:04:05",
	"2006-01-02T15:04:05.999999999",
	"2006-01-02 15:04",
	"2006-01-02 15:04:05",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02T15:04:05Z07:00",
	"2006-01-02T15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05Z07:00",
	"2006-01-02T15:04Z07:00",
	"2006-01-02T15",
	"2006-01-02 15",
}

var weekDate = regexp.MustCompile(`^(\d{4})-W(\d{2})(?:-(\d))?$`)

func isoParses(s string) bool {
	for _, layout := range isoLayouts {
		if _, err := time.Parse(layout, s); err == nil {
			return true
		}
	}
	// Python 3.11 fromisoformat accepts ISO week dates (2026-W03-1)
	if m := weekDate.FindStringSubmatch(s); m != nil {
		w := (int(m[2][0]-'0'))*10 + int(m[2][1]-'0')
		if w >= 1 && w <= 53 {
			if m[3] == "" {
				return true
			}
			d := int(m[3][0] - '0')
			return d >= 1 && d <= 7
		}
	}
	return false
}

// Truthy is Python's bool(v) over the value shapes khub loads from YAML/JSON.
//
// Distinct from AsBool, which reads a bool-ish FIELD (`draft: "no"`); this is
// the plain truthiness test Python applies to a value's presence. It lived as
// eight near-copies across the port (`truthy`, `pyTruthy`, `pyFalsy`) that had
// already drifted apart — graph's copy answered `true` for an empty mapping
// where project's answered `false`, on the same frontmatter.
func Truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case int:
		return x != 0
	case int64:
		return x != 0
	case float64:
		return x != 0
	case canon.BigInt:
		return x.Literal != "0"
	case []any:
		return len(x) > 0
	case *omap.Map:
		return x.Len() > 0
	case map[string]any:
		return len(x) > 0
	default:
		return true
	}
}
