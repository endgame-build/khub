// Value helpers the src/khub/core/entity.py port needs: Python truthiness over
// loaded YAML/JSON values, str.casefold matching, and the write-gate coercions
// _to_bool / _to_number / _as_dateobj.
package entity

import (
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/values"
)

var folder = cases.Fold()

// foldEqual is Python's `a.casefold() == b.casefold()` — full Unicode folding
// (ß → ss), which strings.EqualFold does not do (go-port-plan R6).
func foldEqual(a, b string) bool { return folder.String(a) == folder.String(b) }

// toLower is Python str.lower() — slugify's case step, deliberately NOT
// casefold.
// toLower is Python str.lower(): FULL Unicode case mapping, which is not what
// strings.ToLower does. 'İ' (U+0130) lowercases to two code points in Python
// (i + U+0307 COMBINING DOT ABOVE) and to one in strings.ToLower — and that
// difference is visible in minted slugs, because the combining mark becomes a
// separator (`İstanbul` -> `i-stanbul`, not `istanbul`). This is risk R6.
func toLower(s string) string { return unicodeLower.String(s) }

var unicodeLower = cases.Lower(language.Und)

// trimHyphens is Python str.strip("-").
func trimHyphens(s string) string { return strings.Trim(s, "-") }

// truthy is Python's bool(value) over the value shapes khub loads.
func truthy(v any) bool {
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
	default:
		return true
	}
}

// keysOf tolerates the nil field map a caller with no --field flags passes.
func keysOf(m *omap.Map) []string {
	if m == nil {
		return nil
	}
	return m.Keys()
}

// toBool coerces a bool-ish string the way validate reads it, or raises.
// Re-uses the shared predicates so the write gate accepts exactly the BOOLISH
// set validate accepts — 'banana' is rejected here, not silently stored as
// False.
func toBool(raw string) (bool, error) {
	if !values.IsBool(raw) {
		return false, errs.New("bool_violation", fmt.Sprintf(
			"'%s' is not a valid boolean (true/false, yes/no, 1/0, on/off)", raw))
	}
	return values.AsBool(raw), nil
}

// toNumber coerces raw to a finite int/float, or raises. The finiteness check is
// values.IsNumber (the same gate validate uses), so a non-numeric or non-finite
// string never lands on disk for a later validate to flag. Python tries int()
// first, so 12 stays an int and 0.8 becomes a float; an int beyond int64 keeps
// its literal (Python ints are unbounded).
func toNumber(raw, field string) (any, error) {
	if !values.IsNumber(raw) {
		return nil, errs.NumberViolation(raw, field)
	}
	if n, ok := pyInt(raw); ok {
		return n, nil
	}
	f, err := strconv.ParseFloat(stripUnderscores(strings.TrimSpace(raw)), 64)
	if err != nil {
		return nil, errs.NumberViolation(raw, field)
	}
	return f, nil
}

// pyInt is Python int(str): surrounding whitespace, an optional sign, and
// decimal digits with single underscores between them. Anything else (a dot, an
// exponent) is not an int.
func pyInt(raw string) (any, bool) {
	t := strings.TrimSpace(raw)
	if t == "" {
		return nil, false
	}
	sign := ""
	if t[0] == '+' || t[0] == '-' {
		if t[0] == '-' {
			sign = "-"
		}
		t = t[1:]
	}
	if !underscoresBetweenDigits(t) {
		return nil, false
	}
	digits := stripUnderscores(t)
	if !onlyDigits(digits) {
		return nil, false
	}
	if n, err := strconv.ParseInt(sign+digits, 10, 64); err == nil {
		return n, true
	}
	whole, ok := new(big.Int).SetString(sign+digits, 10)
	if !ok {
		return nil, false
	}
	return canon.BigInt{Literal: whole.String()}, true
}

func stripUnderscores(s string) string { return strings.ReplaceAll(s, "_", "") }

func onlyDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return len(s) > 0
}

func underscoresBetweenDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] != '_' {
			continue
		}
		if i == 0 || i == len(s)-1 || s[i-1] == '_' || s[i+1] == '_' {
			return false
		}
	}
	return true
}

// dateLayouts is Python 3.11 date.fromisoformat's calendar forms; whichever
// matches, the value stores as a plain YYYY-MM-DD (ruamel emits a date
// unquoted).
var dateLayouts = []string{"2006-01-02", "20060102"}

// dateTimeLayouts is Python 3.11 datetime.fromisoformat narrowed to the shapes
// khub sees; offset-bearing forms come first so a trailing Z/±HH:MM is consumed
// rather than left over.
var dateTimeLayouts = []struct {
	layout    string
	hasOffset bool
}{
	{"2006-01-02T15:04:05.999999999Z07:00", true},
	{"2006-01-02T15:04:05Z07:00", true},
	{"2006-01-02T15:04Z07:00", true},
	{"2006-01-02 15:04:05.999999999Z07:00", true},
	{"2006-01-02 15:04:05Z07:00", true},
	{"2006-01-02 15:04Z07:00", true},
	{"2006-01-02T15:04:05.999999999", false},
	{"2006-01-02T15:04:05", false},
	{"2006-01-02T15:04", false},
	{"2006-01-02 15:04:05.999999999", false},
	{"2006-01-02 15:04:05", false},
	{"2006-01-02 15:04", false},
}

var isoWeek = regexp.MustCompile(`^(\d{4})-W(\d{2})(?:-(\d))?$`)

// asDateObj is an ISO string as a date/datetime value, so YAML stores it
// unquoted. `add` writes its created/updated defaults as date objects
// (created: 2026-07-06); a user-supplied string kept as a str would serialize
// quoted (updated: '2026-01-01') — same value, noisier diff.
//
// Python holds one datetime object and lets each serializer pick its form:
// ruamel writes isoformat(' ') and json.dumps writes isoformat() (a 'T').
// canon.DateTime carries a single string, so the separator is chosen here from
// the type's storage format — the value is written immediately and never
// crosses formats.
func asDateObj(raw, storageFmt string) any {
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return canon.Date{ISO: t.Format("2006-01-02")}
		}
	}
	if d, ok := parseISOWeek(raw); ok {
		return d
	}
	sep := " "
	if storageFmt == "json" || storageFmt == "jsonl" {
		sep = "T"
	}
	for _, l := range dateTimeLayouts {
		if t, err := time.Parse(l.layout, raw); err == nil {
			return canon.DateTime{ISO: pyDateTimeISO(t, l.hasOffset, sep)}
		}
	}
	return raw // unreachable behind values.IsDateish; keep the value, never crash
}

// pyDateTimeISO is datetime.isoformat(sep): always seconds, microseconds only
// when non-zero, offset only when the value is aware.
func pyDateTimeISO(t time.Time, hasOffset bool, sep string) string {
	out := t.Format("2006-01-02") + sep + t.Format("15:04:05")
	if micro := t.Nanosecond() / 1000; micro != 0 {
		out += fmt.Sprintf(".%06d", micro)
	}
	if hasOffset {
		out += t.Format("-07:00")
	}
	return out
}

// parseISOWeek covers the ISO week dates Python 3.11's date.fromisoformat
// accepts (and values.IsDateish therefore lets through the gate).
func parseISOWeek(raw string) (canon.Date, bool) {
	m := isoWeek.FindStringSubmatch(raw)
	if m == nil {
		return canon.Date{}, false
	}
	year, _ := strconv.Atoi(m[1])
	week, _ := strconv.Atoi(m[2])
	if week < 1 || week > 53 {
		return canon.Date{}, false
	}
	day := 1
	if m[3] != "" {
		day, _ = strconv.Atoi(m[3])
		if day < 1 || day > 7 {
			return canon.Date{}, false
		}
	}
	// ISO week 1 is the week holding January 4th.
	jan4 := time.Date(year, time.January, 4, 0, 0, 0, 0, time.UTC)
	weekday := int(jan4.Weekday())
	if weekday == 0 {
		weekday = 7 // Sunday is ISO day 7
	}
	monday := jan4.AddDate(0, 0, 1-weekday)
	return canon.Date{ISO: monday.AddDate(0, 0, (week-1)*7+day-1).Format("2006-01-02")}, true
}
