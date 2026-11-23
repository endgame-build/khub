package search

import (
	"os"
	"testing"
)

// PROF_WS=/tmp/bench10k/ws go test ./internal/search -bench=Search -cpuprofile=/tmp/search.out
//
// The term comes from PROF_QUERY, so a corpus can be searched for something it
// holds: parity/scale's generator plants one needle (`search.needle` in its
// manifest) and a tag with a hundred hits. A fixed word the corpus does not
// contain would time only the miss path, which is what the old default did.
func BenchmarkSearchLargeCorpus(b *testing.B) {
	ws := os.Getenv("PROF_WS")
	if ws == "" {
		b.Skip("set PROF_WS")
	}
	term := os.Getenv("PROF_QUERY")
	if term == "" {
		term = "modernization"
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Search(ws, term, Options{Limit: 20}); err != nil {
			b.Fatal(err)
		}
	}
}
