package cli

import (
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/retrieve"
	"github.com/endgame-build/khub/internal/search"
)

// The note speaks only when a result should not be taken at face value:
// seeds the limit dropped, no hit, unparseable files, a cut plain query, and
// entities a cap trimmed — a trimmed seed above all.
func TestRetrieveNote(t *testing.T) {
	five := make([]search.Hit, 5)
	searched := func(hits []search.Hit, total int) *search.Result {
		return &search.Result{Hits: hits, Total: total, Searched: 40}
	}
	for _, tc := range []struct {
		name   string
		res    retrieve.Result
		plain  bool
		want   []string // substrings; nil means no note
		absent []string
	}{
		{"ids, nothing trimmed", retrieve.Result{}, false, nil, nil},
		{"search, all seeded", retrieve.Result{Search: searched(five, 5)}, false, nil, nil},
		{"limit dropped hits", retrieve.Result{Search: searched(five[:2], 5)}, false,
			[]string{"seeding from 2 of 5 hits; pass --limit 5 for all"}, nil},
		{"no raw hits suggests --id and --plain", retrieve.Result{Search: searched([]search.Hit{}, 0)}, false,
			[]string{"no hits in 40 entities searched", "seed by --id <id>", "rerun with --plain"}, nil},
		{"no plain hits does not suggest --plain", retrieve.Result{Search: searched([]search.Hit{}, 0)}, true,
			[]string{"no hits in 40 entities searched"}, []string{"--plain"}},
		{"malformed files", retrieve.Result{Search: &search.Result{Hits: five, Total: 5, Searched: 40, Malformed: 2}}, false,
			[]string{"2 file(s) in the workspace could not be parsed; run `khub validate`"}, nil},
		{"capped plain query", retrieve.Result{Search: &search.Result{Hits: five, Total: 5, Searched: 40, Capped: true}}, true,
			[]string{"cut to its first 64 distinct words"}, nil},
		{"ids the limit dropped", retrieve.Result{Seeds: 1, Omitted: retrieve.Omitted{Limit: 2}}, false,
			[]string{"2 of 3 ids dropped by --limit"}, []string{"hits"}},
		{"search drops are not id drops", retrieve.Result{Search: searched(five[:2], 5), Seeds: 2,
			Omitted: retrieve.Omitted{Limit: 3}}, false,
			[]string{"seeding from 2 of 5 hits"}, []string{"ids dropped"}},
		{"entity cap", retrieve.Result{Omitted: retrieve.Omitted{MaxEntities: 3}}, false,
			[]string{"3 entities trimmed by --max-entities"}, []string{"--max-bytes", "seed"}},
		{"byte cap", retrieve.Result{Omitted: retrieve.Omitted{MaxBytes: 1}}, false,
			[]string{"1 entities trimmed by --max-bytes"}, []string{"--max-entities", "seed"}},
		{"a seed went", retrieve.Result{Omitted: retrieve.Omitted{MaxBytes: 2, Insufficient: true}}, false,
			[]string{"2 entities trimmed by --max-bytes", "could not hold every seed", "--no-body"}, nil},
	} {
		got := retrieveNote(&tc.res, tc.plain)
		if tc.want == nil {
			if got != "" {
				t.Errorf("%s: want no note, got %q", tc.name, got)
			}
			continue
		}
		for _, w := range tc.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: want %q in %q", tc.name, w, got)
			}
		}
		for _, a := range tc.absent {
			if strings.Contains(got, a) {
				t.Errorf("%s: %q must not appear in %q", tc.name, a, got)
			}
		}
	}
}

// The --expand grammar: a bare predicate takes the default depth, `p:n` its
// own, and anything else is a usage error.
func TestParseExpand(t *testing.T) {
	got, err := parseExpand([]string{"depends_on", "affects:2", "related:0"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	want := []retrieve.Expansion{
		{Predicate: "depends_on", Depth: 1}, {Predicate: "affects", Depth: 2}, {Predicate: "related", Depth: 0},
	}
	if len(got) != len(want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("want %v, got %v", want, got)
		}
	}
	for _, bad := range []string{"", ":2", "depends_on:x", "depends_on:-1", "depends_on:1.5"} {
		if _, err := parseExpand([]string{bad}, 1); err == nil {
			t.Errorf("%q: want an error", bad)
		}
	}
}
