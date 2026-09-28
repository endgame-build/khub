package cli

import (
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/search"
)

// The note speaks only when a result should not be taken at face value. A
// result is thin only with no match at all, counted before the limit.
func TestSearchNote(t *testing.T) {
	five := make([]search.Hit, 5)
	for _, tc := range []struct {
		name   string
		res    search.Result
		plain  bool
		want   []string // substrings; nil means no note
		absent []string
	}{
		{"plenty, all shown", search.Result{Hits: five, Total: 5, Searched: 40}, false, nil, nil},
		{"limit dropped rows", search.Result{Hits: five[:2], Total: 5, Searched: 40}, false,
			[]string{"showing 2 of 5 hits; pass --limit 5 for all"}, nil},
		{"--limit 0 is not a thin result", search.Result{Hits: []search.Hit{}, Total: 5, Searched: 40}, false,
			[]string{"showing 0 of 5"}, []string{"no hits"}},
		{"one exact hit says nothing", search.Result{Hits: five[:1], Total: 1, Searched: 40}, false, nil, nil},
		{"no raw hits suggests --plain", search.Result{Hits: []search.Hit{}, Total: 0, Searched: 40}, false,
			[]string{"no hits in 40 entities searched", "khub query --type", "rerun with --plain"}, nil},
		{"no plain hits does not", search.Result{Hits: []search.Hit{}, Total: 0, Searched: 40}, true,
			[]string{"no hits in 40 entities searched"}, []string{"--plain"}},
		{"malformed files", search.Result{Hits: five, Total: 5, Searched: 40, Malformed: 2}, false,
			[]string{"2 file(s) in the workspace could not be parsed; run `khub validate`"}, nil},
		{"capped plain query", search.Result{Hits: five, Total: 5, Searched: 40, Capped: true}, true,
			[]string{"cut to its first 64 distinct words"}, nil},
	} {
		got := searchNote(tc.res, tc.plain)
		if tc.want == nil {
			if got != "" {
				t.Errorf("%s: want no note, got %q", tc.name, got)
			}
			continue
		}
		for _, w := range tc.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: note %q lacks %q", tc.name, got, w)
			}
		}
		for _, a := range tc.absent {
			if strings.Contains(got, a) {
				t.Errorf("%s: note %q should not contain %q", tc.name, got, a)
			}
		}
	}
}
