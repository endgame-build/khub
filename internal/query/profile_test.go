package query

import (
	"os"
	"testing"
	"time"
)

// BenchmarkQueryLargeCorpus profiles the read path against a pre-seeded
// workspace: PROF_WS=/tmp/bench5k/ws go test ./internal/query -bench=. -cpuprofile=/tmp/cpu.out
func BenchmarkQueryLargeCorpus(b *testing.B) {
	ws := os.Getenv("PROF_WS")
	if ws == "" {
		b.Skip("set PROF_WS")
	}
	now := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Query(ws, Filters{}, now); err != nil {
			b.Fatal(err)
		}
	}
}
