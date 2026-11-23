// Ports the `stale` half of src/khub/core/gitlog.py (gitlog.py:90-148).

package gitlog

import (
	"path/filepath"
	"sort"
	"time"

	"github.com/endgame-build/khub/internal/entity"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/project"
	"github.com/endgame-build/khub/internal/query"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/values"
	"github.com/endgame-build/khub/internal/workspace"
)

// Entry is gitlog.StaleEntry: one entity with the date it was judged against
// and where that date came from. `stale` and `recent` report the same shape.
type Entry struct {
	Type          string
	Slug          string
	Title         string // the title every read record carries (query.Title)
	Draft         bool
	EffectiveDate time.Time
	Age           int    // days since the effective date; drives both sorts
	Source        string // "updated" | "git log" | "created" | "none"
	// Unparseable: the source field holds no real date (Raw, as written; ""
	// when there is none). It has no age and sorts first, as the oldest.
	Unparseable bool
	Raw         string
	// Reason is "expired" when an authored `stale_after` has passed, else
	// "age": the entity is past the `updated` threshold.
	Reason string
}

// StaleReport is gitlog.StaleReport: the stale set plus whether git was
// available to backfill missing dates.
type StaleReport struct {
	Entries      []Entry
	GitAvailable bool
}

// Stale is gitlog.stale: entities whose effective date is more than `days` old,
// oldest first.
//
// days is Python's `days: int | None` — nil resolves to the workspace's
// stale_days, the single threshold source shared with status/query. now is the
// current instant: day deltas count from its UTC date, and `stale_after`
// compares against the instant itself. An entity whose `stale_after` has
// passed is listed whatever its age. An entity with no date is otherwise
// skipped; one whose date does not parse (2026-02-30) is reported, as
// project.IsStale judges it.
func Stale(root string, days *int, now time.Time) (*StaleReport, error) {
	s, err := scan(root)
	if err != nil {
		return nil, err
	}
	threshold := 0
	if days == nil {
		threshold, err = workspace.StaleDays(root)
		if err != nil {
			return nil, err
		}
	} else {
		threshold = *days
	}
	judged, err := s.judge(now)
	if err != nil {
		return nil, err
	}
	entries := []Entry{}
	for _, e := range judged {
		if e.Unparseable || e.Age > threshold || e.Reason == "expired" {
			entries = append(entries, e)
		}
	}
	// list.sort(key=(age, type, slug), reverse=True): a reverse comparison on the
	// whole key, not a reversal of the ascending order.
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.Unparseable != b.Unparseable {
			return a.Unparseable
		}
		if a.Age != b.Age {
			return a.Age > b.Age
		}
		if a.Type != b.Type {
			return a.Type > b.Type
		}
		return a.Slug > b.Slug
	})
	return &StaleReport{Entries: entries, GitAvailable: s.gitOK}, nil
}

// scope is one scan the git-derived reads judge: the schema, the valid
// entities (strays are not entities), and whether git has history to
// backfill a missing `updated` from.
type scope struct {
	root     string
	resolved *schema.ResolvedSchema
	valid    *index.Index
	gitOK    bool
}

func scan(root string) (*scope, error) {
	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		return nil, err
	}
	scanned, err := index.Build(root, resolved)
	if err != nil {
		return nil, err
	}
	valid := index.Filter(scanned, index.StrayNodes(scanned))
	gitOK, err := HasGitHistory(root)
	if err != nil {
		return nil, err
	}
	return &scope{root: root, resolved: resolved, valid: valid, gitOK: gitOK}, nil
}

// judge dates every entity in (type, slug) order: `updated`, else the file's
// last commit, else `created` (project.EffectiveDate, the definition status
// and query share). An entity with no date is skipped unless its
// `stale_after` has passed; one whose date does not parse is kept as
// Unparseable, with no age.
func (s *scope) judge(now time.Time) ([]Entry, error) {
	nodes := sortedNodes(s.valid)
	paths := map[index.Node]string{}
	var requests []DateRequest
	if s.gitOK {
		for _, node := range nodes {
			rtype, _ := s.resolved.Types.Get(node.Type)
			// Collection commit dates never describe individual rows.
			if !absent(s.valid.Meta[node], "updated") || rtype.Storage.Layout == schema.LayoutCollection {
				continue
			}
			rel, err := filepath.Rel(s.root, entity.EntityPath(s.root, s.valid, node))
			if err != nil {
				return nil, err
			}
			paths[node] = filepath.ToSlash(rel)
			requests = append(requests, DateRequest{Path: paths[node], Last: true})
		}
	}
	dates, err := CommitDates(s.root, requests)
	if err != nil {
		return nil, err
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	entries := []Entry{}
	for _, node := range nodes {
		meta := s.valid.Meta[node]
		rtype, _ := s.resolved.Types.Get(node.Type)
		var gitDate *time.Time
		if d := dates[paths[node]].Last; !d.IsZero() {
			gitDate = &d
		}
		reason := "age"
		if project.Expired(meta, now) {
			reason = "expired"
		}
		e := Entry{
			Type: node.Type, Slug: node.Slug, Title: query.Title(meta, node.Slug),
			Draft: rtype.IsDraft(meta), Reason: reason,
		}
		eff, source := project.EffectiveDate(meta, gitDate)
		e.Source = source
		if eff == nil {
			if source == "none" && reason != "expired" {
				continue
			}
			if source != "none" {
				v, _ := meta.Get(source)
				e.Raw = values.Str(v)
			}
			e.Unparseable = true
			entries = append(entries, e)
			continue
		}
		e.EffectiveDate, e.Age = *eff, daysBetween(*eff, today)
		entries = append(entries, e)
	}
	return entries, nil
}

// absent is `meta.get(key) is None`: a missing key and an explicit null read
// alike, and neither is a truthiness test (an empty string is PRESENT, so it
// does NOT fall through to the git date).
func absent(meta *omap.Map, key string) bool {
	v, _ := meta.Get(key)
	return v == nil
}

// sortedNodes is `sorted(valid.nodes)` — the (type, slug) tuple order.
func sortedNodes(idx *index.Index) []index.Node {
	out := make([]index.Node, 0, len(idx.Nodes))
	for n := range idx.Nodes {
		out = append(out, n)
	}
	index.SortNodes(out)
	return out
}

// daysBetween is (now - eff).days over two UTC-midnight civil dates, where the
// difference is always an exact multiple of 24h (so Go's truncation and
// Python's floor agree).
func daysBetween(eff, now time.Time) int {
	return int(now.Sub(eff) / (24 * time.Hour))
}
