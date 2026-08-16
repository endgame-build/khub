package search

import (
	"os"
	"testing"
)

// PROF_WS=/tmp/bench10k/ws go test ./internal/search -bench=Search -cpuprofile=/tmp/search.out
func BenchmarkSearchLargeCorpus(b *testing.B) {
	ws := os.Getenv("PROF_WS")
	if ws == "" {
		b.Skip("set PROF_WS")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Search(ws, "modernization", nil, 20); err != nil {
			b.Fatal(err)
		}
	}
}
