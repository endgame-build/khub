package main

import (
	"fmt"
	"os"

	"github.com/goccy/go-yaml/lexer"
)

func main() {
	for _, f := range os.Args[1:] {
		raw, _ := os.ReadFile(f)
		tokens := lexer.Tokenize(string(raw))
		var sb []byte
		for _, t := range tokens {
			sb = append(sb, t.Origin...)
		}
		ok := string(sb) == string(raw)
		fmt.Printf("%s: concat==src %v (tokens %d)\n", f, ok, len(tokens))
		if !ok {
			a, b := string(raw), string(sb)
			for i := 0; i < len(a) && i < len(b); i++ {
				if a[i] != b[i] {
					lo := i - 20
					if lo < 0 {
						lo = 0
					}
					fmt.Printf("  first byte diff at %d: src=%q got=%q\n", i, a[lo:min(i+20, len(a))], b[lo:min(i+20, len(b))])
					break
				}
			}
			if len(a) != len(b) {
				fmt.Printf("  len src=%d got=%d\n", len(a), len(b))
			}
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
