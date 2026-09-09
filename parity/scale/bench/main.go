// bench — where khub's time goes, and how it grows with the corpus.
//
// smoke.sh prints a wall clock next to each case, which is enough to notice a
// regression and not enough to answer the question underneath it: is a
// command's cost linear in the corpus, or quadratic and merely still small.
// So this runs the read surface over corpora of several sizes and reports,
// per command, the median milliseconds at each size and the fitted exponent
// between the smallest and the largest — n^1.0 is linear, n^2.0 is the wall.
//
// Out of process, on purpose: khub is one static binary and the number a user
// feels at the shell is the only one there is. `--version` is the process
// floor and `schema show <type>` the control that reads the ontology and not
// the corpus; everything else should sit above them by about what its scan
// costs.
//
//	go run ./parity/scale/bench -bin ./khub                       # 100/550/2000, corpora cached
//	go run ./parity/scale/bench -bin ./khub -scales 100,550       # fewer
//	go run ./parity/scale/bench -bin ./khub -format json
//	go run ./parity/scale/bench -bin ./khub -regen                # rebuild the corpora first
//
// Not a gate. Nothing here fails on a threshold: the milliseconds depend on
// the machine, the filesystem and what else is running. The exponent is the
// part that travels.
package main

import (
	"bytes"
	// A dev tool, not a khub output path — see parity/scale/corpus/corpus.go.
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/endgame-build/khub/parity/scale/corpus"
)

// readCase is one timed command. `{…}` placeholders are filled from the
// corpus's own manifest, so nothing here spells an id, a title or an id
// scheme — the rule smoke.sh follows. `expect` is enforced once, on the
// warm-up run: a command that is fast because it errored out is the one
// measurement mistake this file cannot afford.
type readCase struct {
	label  string
	argv   []string
	expect int
}

var reads = []readCase{
	{"status", []string{"status"}, 0},
	{"check", []string{"check"}, 0},
	{"check --strict", []string{"check", "--strict"}, 1},
	{"validate (whole corpus)", []string{"validate"}, 0},
	{"validate <one entity>", []string{"validate", "requirement/{needle_id}"}, 0},
	{"query --orphan", []string{"query", "--orphan"}, 0},
	{"query --tag", []string{"query", "--tag", "{tag}"}, 0},
	{"search (one hit)", []string{"search", "{needle}"}, 0},
	{"search (no hit)", []string{"search", "quxzzytl"}, 0},
	{"get", []string{"get", "{needle_id}"}, 0},
	{"get --edges", []string{"get", "{held}", "--edges"}, 0},
	{"neighbors", []string{"neighbors", "{neighbors_id}"}, 0},
	{"neighbors --depth 12", []string{"neighbors", "{neighbors_id}", "--depth", "12"}, 0},
	{"impact", []string{"impact", "{deepest_id}"}, 0},
	{"impact --reverse", []string{"impact", "{hub_id}", "--reverse"}, 0},
	{"history", []string{"history", "{history_id}"}, 0},
	{"stale --days 90", []string{"stale", "--days", "90"}, 0},
	{"reindex --dry-run", []string{"reindex", "--dry-run"}, 0},
	// Reads the ontology and nothing else — the one command whose cost should
	// not move with the corpus at all, and so the control for everything above.
	{"schema show <type>", []string{"schema", "show", "requirement"}, 0},
	// No workspace at all: what a process costs before khub does anything.
	{"--version (process floor)", []string{"--version"}, 0},
}

type stats struct {
	Median float64 `json:"median"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
}

func main() {
	bin := flag.String("bin", "./khub", "the khub binary to time")
	scalesFlag := flag.String("scales", "100,550,2000", "corpus sizes, comma separated")
	cache := flag.String("cache", filepath.Join(os.TempDir(), "khub-scale"), "where corpora are cached, one directory per scale")
	repeat := flag.Int("repeat", 5, "timed runs per command, after one warm-up")
	seed := flag.Uint64("seed", 1, "PRNG seed for the corpora")
	todayFlag := flag.String("today", "2026-01-15", "YYYY-MM-DD pinned into every corpus and the clock khub sees")
	regen := flag.Bool("regen", false, "rebuild the corpora first")
	format := flag.String("format", "text", "text or json")
	flag.Parse()

	today, err := time.Parse("2006-01-02", *todayFlag)
	if err != nil {
		fail(fmt.Errorf("-today: %w", err))
	}
	var scales []int
	for part := range strings.SplitSeq(*scalesFlag, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			fail(fmt.Errorf("-scales: %w", err))
		}
		scales = append(scales, n)
	}
	slices.Sort(scales)
	binPath, err := filepath.Abs(*bin)
	if err != nil {
		fail(err)
	}
	if *format == "text" {
		fmt.Printf("\033[1mkhub bench\033[0m — scales %s, median of %d runs, out of process\n", joinInts(scales), *repeat)
	}

	roots := map[int]string{}
	sizes := map[int]int{}
	manifests := map[int]*corpus.Manifest{}
	for _, scale := range scales {
		root := filepath.Join(*cache, strconv.Itoa(scale))
		m, err := corpusFor(binPath, root, scale, *seed, today, *regen, *format == "text")
		if err != nil {
			fail(err)
		}
		roots[scale], sizes[scale], manifests[scale] = root, m.Total, m
	}

	env := append(os.Environ(), "KHUB_PARITY_NOW="+today.Format("2006-01-02"))
	rows := map[string]map[int]stats{}
	for _, scale := range scales {
		subs := handles(manifests[scale])
		for _, c := range reads {
			argv := make([]string, len(c.argv))
			for i, a := range c.argv {
				argv[i] = fill(a, subs)
			}
			run := func() (int, error) { return invoke(binPath, roots[scale], argv, env) }
			code, err := run()
			if err != nil {
				fail(err)
			}
			if code != c.expect {
				fail(fmt.Errorf("khub %s at n=%d exited %d, wanted %d — the measurement would be of the error path",
					strings.Join(argv, " "), sizes[scale], code, c.expect))
			}
			if rows[c.label] == nil {
				rows[c.label] = map[int]stats{}
			}
			rows[c.label][scale] = timed(run, *repeat)
		}
	}

	if *format == "json" {
		payload := map[string]any{
			"scales":   scales,
			"entities": sizes,
			"repeat":   *repeat,
			"reads":    map[string]map[string]stats{},
		}
		reads := payload["reads"].(map[string]map[string]stats)
		for label, per := range rows {
			reads[label] = map[string]stats{}
			for scale, s := range per {
				reads[label][strconv.Itoa(scale)] = s
			}
		}
		out, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			fail(err)
		}
		fmt.Println(string(out))
		return
	}
	table(rows, scales, sizes)
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "bench: %v\n", err)
	os.Exit(1)
}

// corpusFor returns a cached corpus under root, generating it when absent.
// Generation is the slow part by an order of magnitude and the corpus is
// deterministic, so it is built once.
func corpusFor(bin, root string, scale int, seed uint64, today time.Time, regen, chatty bool) (*corpus.Manifest, error) {
	if !regen {
		if m, err := corpus.Read(root); err == nil {
			return m, nil
		}
	}
	if chatty {
		fmt.Printf("  generating %d entities into %s …\n", scale, root)
	}
	return corpus.Build(corpus.Options{Bin: bin, Work: root, Scale: scale, Seed: seed, Today: today})
}

// handles are the ids and terms the cases need, out of the manifest.
func handles(m *corpus.Manifest) map[string]string {
	topTag, topN := "", -1
	for _, tag := range corpus.Tags {
		if n := m.Tags[tag]; n > topN {
			topTag, topN = tag, n
		}
	}
	subs := map[string]string{
		"needle":       m.Search.Needle,
		"needle_id":    m.Search.ID,
		"tag":          topTag,
		"neighbors_id": m.Degrees[0].ID,
		"history_id":   m.History.Newest,
		"held":         m.Probes.HeldComponent,
	}
	for _, c := range m.Closures {
		switch c.Role {
		case "hub":
			subs["hub_id"] = c.ID
		case "deepest":
			subs["deepest_id"] = c.ID
		}
	}
	return subs
}

func fill(arg string, subs map[string]string) string {
	for key, value := range subs {
		arg = strings.ReplaceAll(arg, "{"+key+"}", value)
	}
	return arg
}

// invoke runs one khub command against root with its output discarded and
// returns the exit code. A version probe takes no workspace.
func invoke(bin, root string, argv []string, env []string) (int, error) {
	full := argv
	if argv[0] != "--version" {
		full = append([]string{"-C", root}, argv...)
	}
	cmd := exec.Command(bin, full...)
	cmd.Env = env
	var sink bytes.Buffer
	cmd.Stdout = &sink
	cmd.Stderr = &sink
	err := cmd.Run()
	if err == nil {
		return 0, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), nil
	}
	return -1, fmt.Errorf("%s %s: %w", bin, strings.Join(full, " "), err)
}

// timed is the median of repeat runs; the caller has already spent the
// warm-up, which pays for the page cache and is not what anyone is asking.
func timed(run func() (int, error), repeat int) stats {
	samples := make([]float64, 0, repeat)
	for range repeat {
		start := time.Now()
		if _, err := run(); err != nil {
			fail(err)
		}
		samples = append(samples, float64(time.Since(start).Microseconds())/1000)
	}
	slices.Sort(samples)
	median := samples[len(samples)/2]
	if len(samples)%2 == 0 {
		median = (samples[len(samples)/2-1] + samples[len(samples)/2]) / 2
	}
	return stats{Median: median, Min: samples[0], Max: samples[len(samples)-1]}
}

// exponent is the fitted b in t ∝ n^b across the two ends: 1.0 linear, 2.0
// quadratic, 0 constant. Two points, not a regression — the middle scales are
// there for a human to sanity-check the fit against.
func exponent(smallN int, smallMs float64, bigN int, bigMs float64) float64 {
	if smallMs <= 0 || bigMs <= 0 || smallN == bigN {
		return 0
	}
	return math.Log(bigMs/smallMs) / math.Log(float64(bigN)/float64(smallN))
}

// table prints one row per command in declaration order. `sizes` is what each
// corpus actually holds — an exponent fitted against the number asked for
// rather than the number written is wrong in exactly the direction that
// flatters the tool.
func table(rows map[string]map[int]stats, scales []int, sizes map[int]int) {
	grows := len(scales) > 1
	fmt.Printf("\n\033[1mreads — median ms, out of process\033[0m\n")
	var head strings.Builder
	for _, s := range scales {
		fmt.Fprintf(&head, "%11s", "n="+strconv.Itoa(sizes[s]))
	}
	tail := ""
	if grows {
		tail = fmt.Sprintf("%11s%10s", "µs/entity", "growth")
	}
	fmt.Printf("  %-26s%s%s\n", "command", head.String(), tail)
	width := 26 + 11*len(scales)
	if grows {
		width += 21
	}
	fmt.Printf("  %s\n", strings.Repeat("-", width))
	lo, hi := scales[0], scales[len(scales)-1]
	for _, c := range reads {
		per := rows[c.label]
		var cells strings.Builder
		for _, s := range scales {
			fmt.Fprintf(&cells, "%11.1f", per[s].Median)
		}
		if !grows {
			fmt.Printf("  %-26s%s\n", c.label, cells.String())
			continue
		}
		perEntity := per[hi].Median * 1000 / float64(sizes[hi])
		b := exponent(sizes[lo], per[lo].Median, sizes[hi], per[hi].Median)
		fmt.Printf("  %-26s%s%11.1f%10s\n", c.label, cells.String(), perEntity, fmt.Sprintf("n^%.2f", b))
	}
	fmt.Println("  the exponent is fitted between the smallest and the largest scale; n^1.0 is linear, n^2.0 the wall")
}

func joinInts(ns []int) string {
	parts := make([]string, len(ns))
	for i, n := range ns {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ", ")
}
