package integrity

import (
	"os"
	"testing"
)

// PROF_WS=/tmp/bench10k/ws go test ./internal/integrity -bench=Check -cpuprofile=/tmp/check.out
func BenchmarkCheckLargeCorpus(b *testing.B) {
	ws := os.Getenv("PROF_WS")
	if ws == "" {
		b.Skip("set PROF_WS")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Check(ws, false); err != nil {
			b.Fatal(err)
		}
	}
}
