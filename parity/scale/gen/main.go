// gen writes a build-hub workspace at scale through the real khub binary and
// the manifest smoke.sh asserts it against.
//
//	go run ./parity/scale/gen -bin ./khub -work /tmp/khub-scale/work -scale 550 -seed 1 -today 2026-01-15
//
// Same -seed, -scale and -today: byte-identical files. See parity/scale/README.md.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/endgame-build/khub/parity/scale/corpus"
)

func main() {
	bin := flag.String("bin", "./khub", "the khub binary to drive")
	work := flag.String("work", "", "workspace to write (required; replaced if it holds a manifest or is empty)")
	scale := flag.Int("scale", 550, "total entities, singletons included")
	seed := flag.Uint64("seed", 1, "PRNG seed")
	todayFlag := flag.String("today", "", "YYYY-MM-DD; pins every date and the clock khub sees (default: today)")
	quiet := flag.Bool("quiet", false, "print nothing on success")
	flag.Parse()

	if *work == "" {
		fmt.Fprintln(os.Stderr, "gen: -work is required")
		os.Exit(2)
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	if *todayFlag != "" {
		t, err := time.Parse("2006-01-02", *todayFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gen: -today: %v\n", err)
			os.Exit(2)
		}
		today = t
	}
	say := func(format string, args ...any) { fmt.Printf(format+"\n", args...) }
	if *quiet {
		say = nil
	}
	if _, err := corpus.Build(corpus.Options{
		Bin: *bin, Work: *work, Scale: *scale, Seed: *seed, Today: today, Log: say,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "gen: %v\n", err)
		os.Exit(1)
	}
}
