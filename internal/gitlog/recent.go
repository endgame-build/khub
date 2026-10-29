// recent.go is `stale` turned around: the entities touched within a window,
// newest first. It judges the same effective date `stale` does.

package gitlog

import (
	"sort"
	"time"

	"github.com/endgame-build/khub/internal/query"
)

// RecentReport is the recent set, the total before --limit cut it, and
// whether git was available to backfill missing dates.
type RecentReport struct {
	Entries      []Entry
	Total        int
	GitAvailable bool
}

// Recent lists entities whose effective date is at most `days` old, newest
// first; ties break on (type, slug) ascending. limit is query's --limit: nil
// is no cap, otherwise query.Limit's slice (0 keeps nothing, -n drops the
// last n). now is a civil date at UTC midnight, as for Stale. A date after
// now (a negative age) counts as recent and sorts first.
func Recent(root string, days int, limit *int, now time.Time) (*RecentReport, error) {
	s, err := scan(root)
	if err != nil {
		return nil, err
	}
	dated, err := s.judge(now)
	if err != nil {
		return nil, err
	}
	entries := []Entry{}
	for _, e := range dated {
		if e.Age <= days {
			entries = append(entries, e)
		}
	}
	// judge yields (type, slug) order, so a stable sort on age alone keeps it
	// as the tiebreak.
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Age < entries[j].Age })
	total := len(entries)
	if limit != nil {
		entries = query.Limit(entries, *limit)
	}
	return &RecentReport{Entries: entries, Total: total, GitAvailable: s.gitOK}, nil
}
