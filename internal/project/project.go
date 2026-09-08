// Package project ports src/khub/core/project.py — the graph projection behind
// `khub status`, plus the single staleness definition status, query, and
// `khub stale` all share (INT-SHARED-004).
//
// A minimal, derived-at-runtime view of the entity tree: per-type counts, the
// draft/active split, orphan (zero edges in or out), stale (older than
// stale_days), and the OKF-conformance flag. Counts are never stored (WS-006);
// orphan and stale are core projection properties computed identically for
// status, query, and check (WS-008).
//
// project.stale_days is NOT here: it lives in internal/workspace as
// workspace.StaleDays, alongside its DEFAULT_STALE_DAYS anchor.
package project

import (
	"time"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/values"
)

// Projection is a derived summary of the workspace's entity tree. Counts is
// keyed by type name in SCHEMA DECLARATION order — the status JSON emits it
// verbatim, so the order is contract.
type Projection struct {
	Counts        *omap.Map // string -> int
	Total         int
	Draft         int
	Active        int
	Orphan        int
	Stale         int
	OKFConformant bool
	Stray         int
	Malformed     int
}

// Project scans the entity tree and derives counts and health flags.
//
// Entity identity is (type, slug): two entities of different types may share a
// slug without colliding. An edge connects two DISTINCT nodes, so a
// self-reference does not rescue an otherwise-isolated entity from orphanhood.
// OKF conformance requires typed/union relation targets to resolve to a node of
// a declared target type; universal (`to: any`) edges may point anywhere.
//
// Strays (a file whose internal `type` mismatches its layout) and malformed
// files are excluded from the entity counts — matching the integrity verbs — and
// reported as their own stray/malformed totals instead.
func Project(root string, staleDays int, now time.Time) (*Projection, error) {
	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		return nil, err
	}
	idx, err := index.Build(root, resolved)
	if err != nil {
		return nil, err
	}
	strays := index.StrayNodes(idx)
	valid := index.Filter(idx, strays) // strays are not entities of their layout

	counts := omap.New()
	for _, tname := range resolved.Types.Keys() {
		counts.Set(tname, 0)
	}
	for _, node := range valid.Order {
		prev, _ := counts.Get(node.Type)
		n, _ := prev.(int)
		counts.Set(node.Type, n+1)
	}

	draft, active, stale := 0, 0, 0
	brokenRef := false
	hasOut := map[index.Node]bool{}
	hasIn := map[index.Node]bool{}
	for _, node := range valid.Order {
		meta := valid.Meta[node]
		draftRaw, _ := meta.Get("draft")
		if values.AsBool(draftRaw) {
			draft++
		} else {
			active++
		}
		if IsStale(meta, now, staleDays, nil) {
			stale++
		}
		rtype, _ := resolved.Types.Get(node.Type)
		for _, predicate := range rtype.Relations.Keys() {
			rel, _ := rtype.Relations.Get(predicate)
			raw, _ := meta.Get(predicate)
			if !values.Truthy(raw) {
				continue
			}
			for _, target := range targetList(raw) {
				resolvedTo := valid.ResolveTarget(rel, target)
				others := 0
				for other := range resolvedTo {
					if other == node {
						continue
					}
					others++
					hasIn[other] = true
				}
				if others > 0 {
					hasOut[node] = true
				}
				if rel.Kind != schema.KindAny && len(resolvedTo) == 0 {
					brokenRef = true
				}
			}
		}
	}

	// The rule (including the `orphan: true` exemption) lives on ResolvedType so
	// this, query and viz cannot drift. Only the degrees are computed locally —
	// the traversal above derives them while also detecting broken references,
	// which is why this path does not build a graph to ask.
	orphan := 0
	for _, node := range valid.Order {
		rtype, _ := valid.Resolved.Types.Get(node.Type)
		if rtype.IsOrphan(hasOut[node], hasIn[node]) {
			orphan++
		}
	}

	total := 0
	for _, tname := range counts.Keys() {
		n, _ := counts.Get(tname)
		v, _ := n.(int)
		total += v
	}

	return &Projection{
		Counts: counts,
		Total:  total,
		Draft:  draft,
		Active: active,
		Orphan: orphan,
		Stale:  stale,
		// OKF conformance means the tree would export as a valid bundle: every
		// file in a layout is a typed entity (no strays, no malformed) and every
		// typed/union edge resolves.
		OKFConformant: !brokenRef && len(strays) == 0 && len(idx.Malformed) == 0,
		Stray:         len(strays),
		Malformed:     len(idx.Malformed),
	}, nil
}

// EffectiveDate returns the date staleness is judged against, with its
// provenance — effective_date.
//
// Precedence: the `updated` field, then a git last-commit date (when supplied),
// then `created`. The single staleness definition shared by status/query (which
// pass no gitDate) and `khub stale` (which backfills git when `updated` is
// absent) — INT-SHARED-004. A present-but-unparseable value yields (nil,
// <source>) so the caller can still see where it came from.
//
// The presence test is `is not None`, not truthiness: `updated: null` falls
// through to gitDate, but `updated: ""` does not — it returns (nil, "updated").
func EffectiveDate(meta *omap.Map, gitDate *time.Time) (*time.Time, string) {
	if raw, ok := meta.Get("updated"); ok && raw != nil {
		return asDate(raw), "updated"
	}
	if gitDate != nil {
		d := *gitDate
		return &d, "git log"
	}
	if raw, ok := meta.Get("created"); ok && raw != nil {
		return asDate(raw), "created"
	}
	return nil, "none"
}

// IsStale is is_stale: no timestamp to judge against is never stale; a
// present-but-unparseable date is surfaced as stale, not silently dropped.
func IsStale(meta *omap.Map, now time.Time, staleDays int, gitDate *time.Time) bool {
	date, source := EffectiveDate(meta, gitDate)
	if source == "none" {
		return false
	}
	return date == nil || daysBetween(civil(now), *date) > staleDays
}

// civil normalizes a time to its calendar date at UTC midnight, so day
// differences are exact (Python compares date objects, never instants).
func civil(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func daysBetween(now, then time.Time) int {
	return int(now.Sub(then) / (24 * time.Hour))
}

// asDate is _as_date: a datetime yields its date, a date passes through, and
// anything else goes through date.fromisoformat(str(value)[:10]) with a
// ValueError -> None guard. The slice is 10 CHARACTERS, so it is taken over runes.
func asDate(value any) *time.Time {
	switch x := value.(type) {
	case nil:
		return nil
	case time.Time:
		d := civil(x)
		return &d
	case canon.DateTime:
		return parseISODate(truncateRunes(x.ISO, 10))
	case canon.Date:
		return parseISODate(truncateRunes(x.ISO, 10))
	}
	return parseISODate(truncateRunes(values.Str(value), 10))
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// parseISODate is CPython 3.11 date.fromisoformat: the extended form
// YYYY-MM-DD, the basic form YYYYMMDD, and ISO week dates (3.11 widened it).
func parseISODate(s string) *time.Time {
	for _, layout := range []string{"2006-01-02", "20060102"} {
		if t, err := time.Parse(layout, s); err == nil {
			d := civil(t)
			return &d
		}
	}
	if t, ok := parseISOWeekDate(s); ok {
		return &t
	}
	return nil
}

// parseISOWeekDate handles YYYY-Www-D and YYYYWwwD (day defaults to 1).
func parseISOWeekDate(s string) (time.Time, bool) {
	r := []rune(s)
	var year, week, day int
	var ok bool
	switch {
	case len(r) == 8 && r[4] == 'W': // YYYYWwwD
		year, week, day, ok = digits(r[0:4]), digits(r[5:7]), digits(r[7:8]), true
	case len(r) == 7 && r[4] == 'W': // YYYYWww
		year, week, day, ok = digits(r[0:4]), digits(r[5:7]), 1, true
	case len(r) == 10 && r[4] == '-' && r[5] == 'W' && r[8] == '-': // YYYY-Www-D
		year, week, day, ok = digits(r[0:4]), digits(r[6:8]), digits(r[9:10]), true
	case len(r) == 8 && r[4] == '-' && r[5] == 'W': // YYYY-Www
		year, week, day, ok = digits(r[0:4]), digits(r[6:8]), 1, true
	}
	if !ok || year < 1 || week < 1 || week > 53 || day < 1 || day > 7 {
		return time.Time{}, false
	}
	// ISO 8601: 4 January always falls in week 1.
	jan4 := time.Date(year, time.January, 4, 0, 0, 0, 0, time.UTC)
	weekday := int(jan4.Weekday())
	if weekday == 0 {
		weekday = 7 // Sunday
	}
	monday := jan4.AddDate(0, 0, 1-weekday)
	d := monday.AddDate(0, 0, (week-1)*7+(day-1))
	// A 53rd week that spilled into the next ISO year does not exist.
	if isoYear, _ := d.ISOWeek(); isoYear != year {
		return time.Time{}, false
	}
	return d, true
}

func digits(r []rune) int {
	n := 0
	for _, c := range r {
		if c < '0' || c > '9' {
			return -1
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// targetList is `value if isinstance(value, list) else [value]` plus str(target).
func targetList(v any) []string {
	if list, ok := v.([]any); ok {
		out := make([]string, 0, len(list))
		for _, item := range list {
			out = append(out, values.Str(item))
		}
		return out
	}
	return []string{values.Str(v)}
}
