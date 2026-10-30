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
		got := searchNote(tc.res, tc.plain, nil)
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

// Several texts: bare words the shell split are named with the quoted
// spelling that searches them as one text, and a plain text cut at the word
// cap is named by its position.
func TestSearchNoteSeveralTexts(t *testing.T) {
	one := []search.Hit{{}}
	for _, tc := range []struct {
		name  string
		res   search.Result
		texts []string
		want  string
	}{
		{"bare words", search.Result{Hits: one, Total: 1}, []string{"auth", "flow"},
			`searched 2 texts as separate groups and fused them; quote "auth flow" to search it as one text`},
		{"one bare word", search.Result{Hits: one, Total: 1}, []string{"auth"}, ""},
		{"a phrase among them", search.Result{Hits: one, Total: 1}, []string{"auth", `"ForbiddenError"`}, ""},
		{"a spaced text among them", search.Result{Hits: one, Total: 1}, []string{"auth flow", "token"}, ""},
		{"a prefix among them", search.Result{Hits: one, Total: 1}, []string{"auth", "tok*"}, ""},
		{"one text cut", search.Result{Hits: one, Total: 1, CappedGroups: []int{2}}, []string{"a b", "c d"},
			"text 2 was cut to its first 64 distinct words"},
		{"two texts cut", search.Result{Hits: one, Total: 1, CappedGroups: []int{1, 3}}, []string{"a b", "c d", "e f"},
			"texts 1, 3 were cut to their first 64 distinct words"},
	} {
		if got := searchNote(tc.res, true, tc.texts); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}
