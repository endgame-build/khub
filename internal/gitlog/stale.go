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
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/workspace"
)

// StaleEntry is gitlog.StaleEntry: one entity past the threshold or its
// authored expiry, with the date the age rule judged and where that date came
// from.
type StaleEntry struct {
	Type          string
	Slug          string
	EffectiveDate *time.Time // nil only for an expired entity with no judgeable date
	Age           *int       // days since the effective date; nil alongside EffectiveDate
	Source        string     // "updated" | "git log" | "created" | "none"
	Reason        string     // ReasonExpired when stale_after has passed, else ReasonAge
}

// The two reasons an entity is stale. Expired wins when both hold: the
// authored statement is the more specific one, and the row still carries the
// age the other rule judged.
const (
	ReasonAge     = "age"
	ReasonExpired = "expired"
)

// StaleReport is gitlog.StaleReport: the stale set plus whether git was
// available to backfill missing dates.
type StaleReport struct {
	Entries      []StaleEntry
	GitAvailable bool
}

// Stale is gitlog.stale: entities whose effective date is more than `days` old,
// or whose authored `stale_after` has passed, oldest first.
//
// days is Python's `days: int | None` — nil resolves to the workspace's
// stale_days, the single threshold source shared with status/query. now is a
// civil date at UTC midnight (the CLI's date.today()), so the day delta is
// exact. An entity with no judgeable date is skipped unless it has expired;
// such a row sorts last.
func Stale(root string, days *int, now time.Time) (*StaleReport, error) {
	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		return nil, err
	}
	scanned, err := index.Build(root, resolved)
	if err != nil {
		return nil, err
	}
	valid := index.Filter(scanned, index.StrayNodes(scanned)) // strays are not entities
	gitOK, err := HasGitHistory(root)
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

	nodes := sortedNodes(valid)
	paths := map[index.Node]string{}
	var requests []DateRequest
	if gitOK {
		for _, node := range nodes {
			rtype, _ := resolved.Types.Get(node.Type)
			// Collection commit dates never describe individual rows.
			if !absent(valid.Meta[node], "updated") || rtype.Storage.Layout == schema.LayoutCollection {
				continue
			}
			rel, err := filepath.Rel(root, entity.EntityPath(root, valid, node))
			if err != nil {
				return nil, err
			}
			paths[node] = filepath.ToSlash(rel)
			requests = append(requests, DateRequest{Path: paths[node], Last: true})
		}
	}
	dates, err := CommitDates(root, requests)
	if err != nil {
		return nil, err
	}
	entries := []StaleEntry{}
	for _, node := range nodes {
		meta := valid.Meta[node]
		var gitDate *time.Time
		if d := dates[paths[node]].Last; !d.IsZero() {
			gitDate = &d
		}
		eff, source := project.EffectiveDate(meta, gitDate)
		rtype, _ := resolved.Types.Get(node.Type)
		expired := project.IsExpired(rtype, meta, now)
		var age *int
		if eff != nil {
			a := daysBetween(*eff, now)
			age = &a
		}
		if !expired && (age == nil || *age <= threshold) {
			continue
		}
		reason := ReasonAge
		if expired {
			reason = ReasonExpired
		}
		entries = append(entries, StaleEntry{
			Type: node.Type, Slug: node.Slug, EffectiveDate: eff, Age: age, Source: source, Reason: reason,
		})
	}
	// list.sort(key=(age, type, slug), reverse=True): a reverse comparison on the
	// whole key, not a reversal of the ascending order.
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if ka, kb := sortAge(a.Age), sortAge(b.Age); ka != kb {
			return ka > kb
		}
		if a.Type != b.Type {
			return a.Type > b.Type
		}
		return a.Slug > b.Slug
	})
	return &StaleReport{Entries: entries, GitAvailable: gitOK}, nil
}

// sortAge places an undated row after every dated one.
func sortAge(age *int) int {
	if age == nil {
		return -1
	}
	return *age
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
