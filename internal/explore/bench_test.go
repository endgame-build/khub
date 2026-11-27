package explore

import (
	"os"
	"testing"
)

// BenchmarkRows2000 times one list load — one scan, one graph, one
// completeness pass — on a large workspace. Every fixture elsewhere holds a dozen entities; the cost
// that matters (a 2,000-entity list should appear in well under a second) only
// shows at size. The corpus comes from the scale generator:
//
//	go run ./parity/scale/gen -bin ./khub -work /tmp/khub-2000 -scale 2000
//	KHUB_BENCH_CORPUS=/tmp/khub-2000 go test -bench Rows2000 ./internal/explore/
//
// Without KHUB_BENCH_CORPUS it skips: generating 2,000 entities through the
// CLI is the generator's job, not a benchmark's setup.
func BenchmarkRows2000(b *testing.B) {
	root := os.Getenv("KHUB_BENCH_CORPUS")
	if root == "" {
		b.Skip("set KHUB_BENCH_CORPUS to a workspace from parity/scale/gen")
	}
	for _, typ := range []string{"requirement", "component"} {
		b.Run(typ, func(b *testing.B) {
			for b.Loop() {
				if _, err := Rows(root, typ, now); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkPage2000 times one entity page on the same corpus.
func BenchmarkPage2000(b *testing.B) {
	root := os.Getenv("KHUB_BENCH_CORPUS")
	if root == "" {
		b.Skip("set KHUB_BENCH_CORPUS to a workspace from parity/scale/gen")
	}
	set, err := Rows(root, "component", now)
	if err != nil {
		b.Fatal(err)
	}
	rows, _ := set.Get("rows")
	list := rows.([]any)
	if len(list) == 0 {
		b.Skip("corpus has no components")
	}
	id, _ := list[0].(interface{ Get(string) (any, bool) }).Get("id")
	for b.Loop() {
		if _, err := Page(root, id.(string), now); err != nil {
			b.Fatal(err)
		}
	}
}
