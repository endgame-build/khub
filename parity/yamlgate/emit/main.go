// T3 — emit-from-scratch differential: parity/corpus/emit-cases.jsonl holds
// (value, ruamel RT dump, ruamel safe/4096 dump) recorded from Python; the
// canon emitter must reproduce both byte-for-byte.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/endgame-build/khub/internal/canon"
)

type row struct {
	Name  string          `json:"name"`
	Value json.RawMessage `json:"value"`
	RT    string          `json:"rt"`
	Safe  string          `json:"safe"`
}

func main() {
	path := "parity/corpus/emit-cases.jsonl"
	if len(os.Args) > 1 {
		path = os.Args[1]
	}
	f, err := os.Open(path)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)

	total, okRT, okSafe, shown := 0, 0, 0, 0
	maxShow := 12
	if v := os.Getenv("SHOW"); v != "" {
		_, _ = fmt.Sscanf(v, "%d", &maxShow) // unparsable SHOW keeps the default
	}
	for sc.Scan() {
		var r row
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			panic(err)
		}
		total++
		val, err := canon.DecodeOrderedJSON(r.Value)
		if err != nil {
			fmt.Printf("%s: decode error %v\n", r.Name, err)
			continue
		}
		gotRT, errRT := canon.DumpRT(val)
		gotSafe, errSafe := canon.DumpWide(val)
		if errRT == nil && gotRT == r.RT {
			okRT++
		} else if shown < maxShow {
			shown++
			fmt.Printf("== %s (rt) err=%v\n  want %s\n  got  %s\n", r.Name, errRT, firstDiff(r.RT, gotRT), firstDiffB(r.RT, gotRT))
		}
		if errSafe == nil && gotSafe == r.Safe {
			okSafe++
		} else if shown < maxShow {
			shown++
			fmt.Printf("== %s (safe) err=%v\n  want %s\n  got  %s\n", r.Name, errSafe, firstDiff(r.Safe, gotSafe), firstDiffB(r.Safe, gotSafe))
		}
	}
	fmt.Printf("\nT3: rt %d/%d  safe %d/%d\n", okRT, total, okSafe, total)
}

func firstDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := 0; i < len(w) || i < len(g); i++ {
		wl, gl := line(w, i), line(g, i)
		if wl != gl {
			return fmt.Sprintf("L%d %q", i+1, wl)
		}
	}
	return "(equal?)"
}

func firstDiffB(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := 0; i < len(w) || i < len(g); i++ {
		wl, gl := line(w, i), line(g, i)
		if wl != gl {
			return fmt.Sprintf("L%d %q", i+1, gl)
		}
	}
	return ""
}

func line(ls []string, i int) string {
	if i < len(ls) {
		return ls[i]
	}
	return "<missing>"
}
