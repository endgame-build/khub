// gate — the performance rule, against a recorded baseline.
//
// bench measures; this decides. It reads bench's JSON (the `-profile gate`
// shape: two scales, eleven interleaved repeats, both clocks) and compares
// three statistics per command with parity/scale/baseline.json:
//
//	cpu_e   growth exponent of cpu min between the smallest and largest scale
//	cpu_r   cpu min at the largest scale over the control's (`schema show`)
//	wall_r  wall min at the largest scale over the process floor's (`--version`)
//
// Why those: wall-time medians under a shared runner swung up to 3.5× between
// unchanged runs and missed an injected +80% scan, while the child's CPU time,
// min of eleven, varies 2–8% and caught it; and `schema show` normalises CPU
// better than `--version` because it runs the same kind of work. The rule:
//
//	FAIL     cpu_e > ceiling (1.15), or > baseline + delta (0.10)
//	FAIL     cpu_r > baseline × cpu_ratio (1.35)
//	FAIL     wall_r > baseline × wall_ratio (2.0) — a backstop for lost parallelism
//	INVALID  the control grew (cpu_e outside [-0.10, 0.15]) or the floor is
//	         3× the baseline's: the run measured the machine, not khub (exit 2)
//
// Write rows gate cpu_e and cpu_r only: write wall time is fsync-bound and
// showed 0.7–8.8s outliers for a 30–100ms operation. A baseline recorded on
// another machine (goos, cpu model or core count differ) keeps only the
// exponent ceiling blocking and downgrades the per-command delta and both
// ratios to notes: the first run on a GitHub runner against a container
// baseline put every exponent a uniform 0.06–0.12 higher, the machine's
// process floor diluting the small scale differently, so only the absolute
// cliff detector travels. Tolerances live in the baseline, so a change to
// them is reviewed like any other number.
//
//	go run ./parity/scale/gate -baseline parity/scale/baseline.json bench.json
//	go run ./parity/scale/gate -baseline parity/scale/baseline.json -format md bench.json
//	go run ./parity/scale/gate -record bench1.json bench2.json bench3.json > parity/scale/baseline.json
//
// Exit codes follow cli.md: 0 pass, 1 the code regressed, 2 the call or the
// measurement was wrong. With GITHUB_OUTPUT set, `verdict=pass|fail|invalid`
// is appended for the workflow.
package main

import (
	// A dev tool, not a khub output path — see parity/scale/corpus/corpus.go.
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/endgame-build/khub/parity/scale/corpus"
)

// Bench is the slice of bench's JSON this reads.
type Bench struct {
	Khub       string                    `json:"khub"`
	Commit     string                    `json:"commit"`
	Go         string                    `json:"go"`
	GOOS       string                    `json:"goos"`
	NProc      int                       `json:"nproc"`
	CPUModel   string                    `json:"cpu_model"`
	LoadBefore []float64                 `json:"load_before"`
	Scales     []int                     `json:"scales"`
	Entities   map[string]int            `json:"entities"`
	Repeat     int                       `json:"repeat"`
	Reads      map[string]map[string]Row `json:"reads"`
	Writes     map[string]map[string]Row `json:"writes"`
}

type Row struct {
	Wall Stat `json:"wall"`
	CPU  Stat `json:"cpu"`
}

type Stat struct {
	Median float64 `json:"median"`
	Min    float64 `json:"min"`
}

// Baseline is parity/scale/baseline.json.
type Baseline struct {
	Recorded  Recorded         `json:"recorded"`
	Profile   Profile          `json:"profile"`
	Tolerance Tolerance        `json:"tolerance"`
	Control   string           `json:"control"`
	Floor     string           `json:"floor"`
	FloorWall float64          `json:"floor_wall_ms"`
	Reads     map[string]Marks `json:"reads"`
	Writes    map[string]Marks `json:"writes"`
}

type Recorded struct {
	Khub     string `json:"khub"`
	Commit   string `json:"commit"`
	Go       string `json:"go"`
	GOOS     string `json:"goos"`
	Runner   string `json:"runner"`
	NProc    int    `json:"nproc"`
	CPUModel string `json:"cpu_model"`
	Runs     int    `json:"runs"`
}

type Profile struct {
	Scales    []int  `json:"scales"`
	Repeat    int    `json:"repeat"`
	Statistic string `json:"statistic"`
}

type Tolerance struct {
	ExponentCeiling float64 `json:"exponent_ceiling"`
	ExponentDelta   float64 `json:"exponent_delta"`
	CPURatio        float64 `json:"cpu_ratio"`
	WallRatio       float64 `json:"wall_ratio"`
}

// Marks are the three statistics; WallR is nil for a write row.
type Marks struct {
	CPUE  float64  `json:"cpu_e"`
	CPUR  float64  `json:"cpu_r"`
	WallR *float64 `json:"wall_r"`
}

func main() {
	baselinePath := flag.String("baseline", "", "the baseline to compare with (or to show deltas against when recording)")
	record := flag.Bool("record", false, "write a baseline from the bench files given (median of each statistic across them)")
	runner := flag.String("runner", os.Getenv("RUNNER_OS")+os.Getenv("ImageOS"), "a name for the machine class a recording was made on")
	format := flag.String("format", "text", "text or md")
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: gate [-baseline file] [-record] [-format text|md] bench.json...")
		os.Exit(2)
	}
	var benches []Bench
	for _, path := range flag.Args() {
		var b Bench
		if err := load(path, &b); err != nil {
			fail(2, err)
		}
		if len(b.Scales) < 2 {
			fail(2, fmt.Errorf("%s: the gate needs at least two scales, got %v", path, b.Scales))
		}
		benches = append(benches, b)
	}
	var base *Baseline
	if *baselinePath != "" {
		base = &Baseline{}
		if err := load(*baselinePath, base); err != nil {
			fail(2, err)
		}
	}
	if *record {
		out := recordBaseline(benches, *runner)
		if base != nil {
			fmt.Fprintln(os.Stderr, deltas(*base, out))
		}
		raw, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			fail(2, err)
		}
		fmt.Println(string(raw))
		return
	}
	if base == nil {
		fail(2, fmt.Errorf("-baseline is required unless -record"))
	}
	if len(benches) != 1 {
		fail(2, fmt.Errorf("one bench file to gate, got %d", len(benches)))
	}
	verdict, report := gate(*base, benches[0], *runner, *format == "md")
	fmt.Print(report)
	if out := os.Getenv("GITHUB_OUTPUT"); out != "" {
		if f, err := os.OpenFile(out, os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
			fmt.Fprintf(f, "verdict=%s\n", verdict)
			f.Close()
		}
	}
	switch verdict {
	case "fail":
		os.Exit(1)
	case "invalid":
		os.Exit(2)
	}
}

func fail(code int, err error) {
	fmt.Fprintf(os.Stderr, "gate: %v\n", err)
	os.Exit(code)
}

func load(path string, v any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// marks computes the three statistics of one bench for one row.
func marks(b Bench, section string, label string) (Marks, bool) {
	rows := b.Reads
	if section == "writes" {
		rows = b.Writes
	}
	per, ok := rows[label]
	if !ok {
		return Marks{}, false
	}
	lo, hi := strconv.Itoa(b.Scales[0]), strconv.Itoa(b.Scales[len(b.Scales)-1])
	nLo, nHi := b.Entities[lo], b.Entities[hi]
	m := Marks{CPUE: exponent(nLo, per[lo].CPU.Min, nHi, per[hi].CPU.Min)}
	if control, ok := b.Reads[corpus.Control]; ok && control[hi].CPU.Min > 0 {
		m.CPUR = per[hi].CPU.Min / control[hi].CPU.Min
	}
	if section == "reads" {
		if floor, ok := b.Reads[corpus.Floor]; ok && floor[hi].Wall.Min > 0 {
			r := per[hi].Wall.Min / floor[hi].Wall.Min
			m.WallR = &r
		}
	}
	return m, true
}

func exponent(smallN int, small float64, bigN int, big float64) float64 {
	if small <= 0 || big <= 0 || smallN == bigN {
		return 0
	}
	return math.Log(big/small) / math.Log(float64(bigN)/float64(smallN))
}

func round(v float64) float64 { return math.Round(v*100) / 100 }

// recordBaseline takes the median of every statistic across the benches.
func recordBaseline(benches []Bench, runner string) Baseline {
	first := benches[0]
	hi := strconv.Itoa(first.Scales[len(first.Scales)-1])
	out := Baseline{
		Recorded: Recorded{Khub: first.Khub, Commit: first.Commit, Go: first.Go, GOOS: first.GOOS, Runner: runner,
			NProc: first.NProc, CPUModel: first.CPUModel, Runs: len(benches)},
		Profile:   Profile{Scales: first.Scales, Repeat: first.Repeat, Statistic: "min"},
		Tolerance: Tolerance{ExponentCeiling: 1.15, ExponentDelta: 0.10, CPURatio: 1.35, WallRatio: 2.0},
		Control:   corpus.Control,
		Floor:     corpus.Floor,
		Reads:     map[string]Marks{},
		Writes:    map[string]Marks{},
	}
	var floors []float64
	for _, b := range benches {
		if f, ok := b.Reads[corpus.Floor]; ok {
			floors = append(floors, f[hi].Wall.Min)
		}
	}
	out.FloorWall = round(median(floors))
	for _, section := range []string{"reads", "writes"} {
		cases := corpus.Reads
		dst := out.Reads
		if section == "writes" {
			cases, dst = corpus.Writes, out.Writes
		}
		for _, c := range cases {
			var es, rs, ws []float64
			for _, b := range benches {
				m, ok := marks(b, section, c.Label)
				if !ok {
					continue
				}
				es, rs = append(es, m.CPUE), append(rs, m.CPUR)
				if m.WallR != nil {
					ws = append(ws, *m.WallR)
				}
			}
			if len(es) == 0 {
				continue
			}
			m := Marks{CPUE: round(median(es)), CPUR: round(median(rs))}
			if len(ws) > 0 {
				w := round(median(ws))
				m.WallR = &w
			}
			dst[c.Label] = m
		}
	}
	return out
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := slices.Clone(xs)
	slices.Sort(s)
	if len(s)%2 == 1 {
		return s[len(s)/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}

// deltas describes a new baseline against an old one, for the commit.
func deltas(old, next Baseline) string {
	var b strings.Builder
	fmt.Fprintf(&b, "baseline deltas, %s (%s) -> %s (%s):\n", old.Recorded.Khub, old.Recorded.Commit, next.Recorded.Khub, next.Recorded.Commit)
	for _, section := range []string{"reads", "writes"} {
		o, n := old.Reads, next.Reads
		if section == "writes" {
			o, n = old.Writes, next.Writes
		}
		for _, label := range sortedKeys(n) {
			om, had := o[label]
			nm := n[label]
			if !had {
				fmt.Fprintf(&b, "  %-28s new: cpu_e %.2f cpu_r %.2f\n", label, nm.CPUE, nm.CPUR)
				continue
			}
			fmt.Fprintf(&b, "  %-28s cpu_e %.2f -> %.2f   cpu_r %.2f -> %.2f\n", label, om.CPUE, nm.CPUE, om.CPUR, nm.CPUR)
		}
	}
	return b.String()
}

func sortedKeys(m map[string]Marks) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// gate applies the rule and renders the table. The verdict is pass, fail or
// invalid.
func gate(base Baseline, b Bench, runner string, md bool) (string, string) {
	var out strings.Builder
	hi := strconv.Itoa(b.Scales[len(b.Scales)-1])
	tol := base.Tolerance
	sameMachine := base.Recorded.GOOS == b.GOOS && base.Recorded.NProc == b.NProc && base.Recorded.CPUModel == b.CPUModel
	ratiosGate := sameMachine
	invalid := []string{}
	fails := []string{}
	notes := []string{}

	if !sameMachine {
		notes = append(notes, fmt.Sprintf("the baseline was recorded on %s/%s, %d cores (%s); this is %s, %d cores (%s): only the exponent ceiling gates, deltas and ratios are notes",
			base.Recorded.Runner, base.Recorded.GOOS, base.Recorded.NProc, base.Recorded.CPUModel, b.GOOS, b.NProc, b.CPUModel))
	}
	if control, ok := marks(b, "reads", corpus.Control); ok {
		if control.CPUE < -0.10 || control.CPUE > 0.15 {
			invalid = append(invalid, fmt.Sprintf("the control (%s) grew with the corpus: cpu_e %.2f outside [-0.10, 0.15]", corpus.Control, control.CPUE))
		}
	} else {
		invalid = append(invalid, "the bench has no control row")
	}
	if floor, ok := b.Reads[corpus.Floor]; ok && base.FloorWall > 0 && sameMachine {
		if floor[hi].Wall.Min > 3*base.FloorWall {
			invalid = append(invalid, fmt.Sprintf("the process floor is %.1f ms against %.1f ms recorded: the machine, not khub", floor[hi].Wall.Min, base.FloorWall))
		}
	}

	sep, head := "  ", "  %-28s %7s %7s %7s %7s %7s %7s  %s\n"
	if md {
		sep = "| "
		head = "| %-28s | %7s | %7s | %7s | %7s | %7s | %7s | %s |\n"
	}
	rowf := head
	for _, section := range []string{"reads", "writes"} {
		cases, marksBase := corpus.Reads, base.Reads
		if section == "writes" {
			cases, marksBase = corpus.Writes, base.Writes
		}
		if md {
			fmt.Fprintf(&out, "\n**%s** (n=%s, min of %d)\n\n", section, hi, b.Repeat)
		} else {
			fmt.Fprintf(&out, "\n%s (n=%s, min of %d)\n", section, hi, b.Repeat)
		}
		fmt.Fprintf(&out, head, "command", "cpu_e", "base", "cpu_r", "base", "wall_r", "base", "verdict")
		if md {
			fmt.Fprintf(&out, "|---|---|---|---|---|---|---|---|\n")
		}
		for _, c := range cases {
			if c.Label == corpus.Floor {
				continue
			}
			m, ok := marks(b, section, c.Label)
			if !ok {
				continue
			}
			bm, had := marksBase[c.Label]
			verdict := "ok"
			switch {
			case c.Label == corpus.Control:
				verdict = "control"
			case !had:
				verdict = "new, no baseline"
				notes = append(notes, c.Label+": no baseline row yet; record one")
			default:
				var why []string
				if m.CPUE > tol.ExponentCeiling {
					why = append(why, fmt.Sprintf("cpu_e %.2f > %.2f", m.CPUE, tol.ExponentCeiling))
				}
				if m.CPUE > bm.CPUE+tol.ExponentDelta {
					s := fmt.Sprintf("cpu_e %.2f > base %.2f + %.2f", m.CPUE, bm.CPUE, tol.ExponentDelta)
					if sameMachine {
						why = append(why, s)
					} else {
						notes = append(notes, c.Label+": "+s+" (not gated off the recording machine)")
					}
				}
				if bm.CPUR > 0 && m.CPUR > bm.CPUR*tol.CPURatio {
					s := fmt.Sprintf("cpu_r %.1f > base %.1f × %.2f", m.CPUR, bm.CPUR, tol.CPURatio)
					if ratiosGate {
						why = append(why, s)
					} else {
						notes = append(notes, c.Label+": "+s+" (not gated off the recording machine)")
					}
				}
				if m.WallR != nil && bm.WallR != nil && *bm.WallR > 0 && *m.WallR > *bm.WallR*tol.WallRatio {
					s := fmt.Sprintf("wall_r %.1f > base %.1f × %.1f", *m.WallR, *bm.WallR, tol.WallRatio)
					if ratiosGate {
						why = append(why, s)
					} else {
						notes = append(notes, c.Label+": "+s+" (not gated off the recording machine)")
					}
				}
				if bm.CPUR > 0 && m.CPUR < bm.CPUR*0.75 {
					notes = append(notes, fmt.Sprintf("%s: cpu_r %.1f is well under base %.1f; re-record to tighten", c.Label, m.CPUR, bm.CPUR))
				}
				if len(why) > 0 {
					verdict = "FAIL " + strings.Join(why, "; ")
					fails = append(fails, c.Label+": "+strings.Join(why, "; "))
				}
			}
			wr, bwr := "-", "-"
			if m.WallR != nil {
				wr = fmt.Sprintf("%.1f", *m.WallR)
			}
			if had && bm.WallR != nil {
				bwr = fmt.Sprintf("%.1f", *bm.WallR)
			}
			bce, bcr := "-", "-"
			if had {
				bce, bcr = fmt.Sprintf("%.2f", bm.CPUE), fmt.Sprintf("%.1f", bm.CPUR)
			}
			fmt.Fprintf(&out, rowf, c.Label, fmt.Sprintf("%.2f", m.CPUE), bce, fmt.Sprintf("%.1f", m.CPUR), bcr, wr, bwr, verdict)
		}
	}
	_ = sep
	fmt.Fprintln(&out)
	for _, n := range notes {
		fmt.Fprintf(&out, "note: %s\n", n)
	}
	verdict := "pass"
	switch {
	case len(invalid) > 0:
		verdict = "invalid"
		for _, i := range invalid {
			fmt.Fprintf(&out, "INVALID: %s\n", i)
		}
	case len(fails) > 0:
		verdict = "fail"
		for _, f := range fails {
			fmt.Fprintf(&out, "FAIL: %s\n", f)
		}
	default:
		fmt.Fprintf(&out, "gate: pass against %s (%s)\n", base.Recorded.Khub, base.Recorded.Commit)
	}
	if runner != "" && !sameMachine {
		fmt.Fprintf(&out, "gate: this machine class is %q\n", runner)
	}
	return verdict, out.String()
}
