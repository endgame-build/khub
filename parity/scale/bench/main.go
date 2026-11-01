// bench — where khub's time goes, and how it grows with the corpus.
//
// smoke.sh prints a wall clock next to each case, which is enough to notice a
// regression and not enough to answer the question underneath it: is a
// command's cost linear in the corpus, or quadratic and merely still small.
// So this runs the read surface, one write cycle and one OKF export and
// import over corpora of several sizes and reports, per command, the cost at
// each size and the fitted exponent between the smallest and the largest —
// n^1.0 is linear, n^2.0 is the wall.
//
// Out of process, on purpose: khub is one static binary and the number a user
// feels at the shell is the only one there is. Two clocks per sample: wall
// time, which is what the user feels, and the child's CPU time (user + sys),
// which ignores the scheduler contention of a shared runner and is the
// statistic parity/scale/gate compares, as the min over the repeats — under
// contention noise only ever adds time. Runs are interleaved across scales
// and cases, so a slow stretch hits every row alike instead of one scale.
//
// `--version` is the process floor and `schema show <type>` the control that
// reads the ontology and not the corpus; everything else should sit above
// them by about what its scan costs.
//
//	go run ./parity/scale/bench -bin ./khub                       # 100/550/2000, corpora cached
//	go run ./parity/scale/bench -bin ./khub -scales 100,550       # fewer
//	go run ./parity/scale/bench -bin ./khub -profile gate         # 100 and 2000, 11 repeats, what the gate reads
//	go run ./parity/scale/bench -bin ./khub -format json          # every sample, for the gate
//	go run ./parity/scale/bench -bin ./khub -regen                # rebuild the corpora first
//
// Not a gate by itself. Nothing here fails on a threshold: parity/scale/gate
// holds the rule, against a recorded baseline. What does fail here is a
// command that exits wrong on its warm-up, a no-op link that reports a
// change, and a write cycle that leaves the corpus different from the cached
// original — a write-path oracle at n that no fixture can be.
package main

import (
	"bytes"
	"crypto/sha256"
	// A dev tool, not a khub output path — see parity/scale/corpus/corpus.go.
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/endgame-build/khub/parity/scale/corpus"
)

// Stat summarises one clock over the repeats. Samples are kept: the gate
// computes, the bench measures.
type Stat struct {
	Median  float64   `json:"median"`
	Min     float64   `json:"min"`
	Max     float64   `json:"max"`
	Samples []float64 `json:"samples"`
}

// Row is one case at one scale, both clocks, milliseconds.
type Row struct {
	Wall Stat `json:"wall"`
	CPU  Stat `json:"cpu"`
}

// Corpus is the fingerprint of one corpus the run measured.
type Corpus struct {
	corpus.Generated
	Total int `json:"total"`
}

// Report is the JSON document. The header says what machine measured, so a
// baseline recorded elsewhere can be told apart from one recorded here.
type Report struct {
	Khub       string                      `json:"khub"`
	Commit     string                      `json:"commit"`
	Go         string                      `json:"go"`
	GOOS       string                      `json:"goos"`
	GOARCH     string                      `json:"goarch"`
	NProc      int                         `json:"nproc"`
	GOMAXPROCS int                         `json:"gomaxprocs"`
	CPUModel   string                      `json:"cpu_model"`
	LoadBefore []float64                   `json:"load_before"`
	LoadAfter  []float64                   `json:"load_after"`
	Scales     []int                       `json:"scales"`
	Entities   map[string]int              `json:"entities"`
	Repeat     int                         `json:"repeat"`
	Corpus     map[string]Corpus           `json:"corpus"`
	Reads      map[string]map[string]Row   `json:"reads"`
	Writes     map[string]map[string]Row   `json:"writes"`
	OKF        map[string]map[string]Row   `json:"okf"`
	WriteCheck map[string]string           `json:"write_check"`
	labels     map[string][]string         // declaration order per section
	sizes      map[int]int                 // scale → entities
	samples    map[string]map[int][]sample // label → scale → samples
}

type sample struct{ wall, cpu float64 }

func main() {
	bin := flag.String("bin", "./khub", "the khub binary to time")
	scalesFlag := flag.String("scales", "100,550,2000", "corpus sizes, comma separated")
	cache := flag.String("cache", filepath.Join(os.TempDir(), "khub-scale"), "where corpora are cached, one directory per scale")
	work := flag.String("work", filepath.Join(os.TempDir(), "khub-scale-bench"), "where the write cycle's corpus copies go")
	repeat := flag.Int("repeat", 5, "timed runs per command, after one warm-up")
	seed := flag.Uint64("seed", 1, "PRNG seed for the corpora")
	todayFlag := flag.String("today", "2026-01-15", "YYYY-MM-DD pinned into every corpus and the clock khub sees")
	regen := flag.Bool("regen", false, "rebuild the corpora first")
	writes := flag.Bool("writes", true, "run the write cycle too, on a copy of each corpus")
	okf := flag.Bool("okf", true, "time export --okf and import --okf of each corpus, into fresh directories")
	profile := flag.String("profile", "", "`gate` sets -scales 100,2000 -repeat 11: what parity/scale/gate reads")
	format := flag.String("format", "text", "text or json")
	flag.Parse()

	if *profile == "gate" {
		*scalesFlag, *repeat = "100,2000", 11
	} else if *profile != "" {
		fail(fmt.Errorf("-profile: unknown profile %q", *profile))
	}
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
	chatty := *format == "text"
	if chatty {
		fmt.Printf("\033[1mkhub bench\033[0m — scales %s, %d interleaved runs after a warm-up, out of process\n", joinInts(scales), *repeat)
	}

	r := &Report{
		Khub: version(binPath), Commit: commit(), Go: runtime.Version(), GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
		NProc: runtime.NumCPU(), GOMAXPROCS: runtime.GOMAXPROCS(0), CPUModel: cpuModel(), LoadBefore: loadavg(),
		Scales: scales, Entities: map[string]int{}, Repeat: *repeat, Corpus: map[string]Corpus{},
		Reads: map[string]map[string]Row{}, Writes: map[string]map[string]Row{}, OKF: map[string]map[string]Row{},
		WriteCheck: map[string]string{}, labels: map[string][]string{}, sizes: map[int]int{},
		samples: map[string]map[int][]sample{},
	}

	roots := map[int]string{}
	copies := map[int]string{}
	subs := map[int]map[string]string{}
	manifests := map[int]*corpus.Manifest{}
	for _, scale := range scales {
		root := filepath.Join(*cache, strconv.Itoa(scale))
		m, err := corpusFor(binPath, root, scale, *seed, today, *regen, chatty)
		if err != nil {
			fail(err)
		}
		key := strconv.Itoa(scale)
		roots[scale], r.sizes[scale], r.Entities[key] = root, m.Total, m.Total
		r.Corpus[key] = Corpus{Generated: m.Generated, Total: m.Total}
		subs[scale] = corpus.Handles(m)
		manifests[scale] = m
		if *writes {
			dst := filepath.Join(*work, key)
			if err := copyCorpus(root, dst); err != nil {
				fail(err)
			}
			copies[scale] = dst
		}
	}

	env := append(os.Environ(), "KHUB_PARITY_NOW="+today.Format("2006-01-02"), "KHUB_ACTOR=khub", "TZ=UTC")
	for _, c := range corpus.Reads {
		r.labels["reads"] = append(r.labels["reads"], c.Label)
	}
	if *writes {
		for _, c := range corpus.Writes {
			r.labels["writes"] = append(r.labels["writes"], c.Label)
		}
	}
	if *okf {
		r.labels["okf"] = okfLabels
	}
	okfScratch := func(scale int) string { return filepath.Join(*work, strconv.Itoa(scale)+"-okf") }

	// The warm-up pays for the page cache and checks every exit code once.
	for _, scale := range scales {
		for _, c := range corpus.Reads {
			argv := corpus.Fill(c.Argv, subs[scale])
			if _, code, err := invoke(binPath, roots[scale], argv, env); err != nil {
				fail(err)
			} else if code != c.Exit {
				fail(fmt.Errorf("khub %s at n=%d exited %d, wanted %d — the measurement would be of the error path",
					strings.Join(argv, " "), r.sizes[scale], code, c.Exit))
			}
		}
		if *writes {
			if _, err := writeCycle(binPath, copies[scale], subs[scale], env, 0); err != nil {
				fail(err)
			}
		}
		if *okf {
			if _, err := okfCycle(binPath, roots[scale], okfScratch(scale), manifests[scale], env); err != nil {
				fail(err)
			}
		}
	}

	// Interleaved: every repeat visits every scale and every case before the
	// next repeat starts, so a slow stretch on the machine hits all rows.
	for i := 1; i <= *repeat; i++ {
		for _, scale := range scales {
			for _, c := range corpus.Reads {
				argv := corpus.Fill(c.Argv, subs[scale])
				s, _, err := invoke(binPath, roots[scale], argv, env)
				if err != nil {
					fail(err)
				}
				r.record(c.Label, scale, s)
			}
			if *writes {
				got, err := writeCycle(binPath, copies[scale], subs[scale], env, i)
				if err != nil {
					fail(err)
				}
				for j, c := range corpus.Writes {
					r.record(c.Label, scale, got[j])
				}
			}
			if *okf {
				got, err := okfCycle(binPath, roots[scale], okfScratch(scale), manifests[scale], env)
				if err != nil {
					fail(err)
				}
				for j, label := range okfLabels {
					r.record(label, scale, got[j])
				}
			}
		}
	}
	r.LoadAfter = loadavg()

	// The write cycle must have left every copy exactly as the cache has it.
	if *writes {
		for _, scale := range scales {
			key := strconv.Itoa(scale)
			if diff := treeDiff(roots[scale], copies[scale]); diff != "" {
				r.WriteCheck[key] = diff
				if chatty {
					fmt.Fprintf(os.Stderr, "bench: the write cycle changed the corpus at n=%d:\n%s", r.sizes[scale], diff)
				}
			} else {
				r.WriteCheck[key] = "identical"
			}
		}
	}

	r.fold()
	if *format == "json" {
		out, err := json.MarshalIndent(r, "", "  ")
		if err != nil {
			fail(err)
		}
		fmt.Println(string(out))
	} else {
		r.table()
	}
	for _, v := range r.WriteCheck {
		if v != "identical" {
			os.Exit(1)
		}
	}
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "bench: %v\n", err)
	os.Exit(1)
}

func (r *Report) record(label string, scale int, s sample) {
	if r.samples[label] == nil {
		r.samples[label] = map[int][]sample{}
	}
	r.samples[label][scale] = append(r.samples[label][scale], s)
}

// fold turns the samples into the per-section rows the JSON carries.
func (r *Report) fold() {
	for section, labels := range r.labels {
		dst := r.section(section)
		for _, label := range labels {
			dst[label] = map[string]Row{}
			for scale, ss := range r.samples[label] {
				wall := make([]float64, len(ss))
				cpu := make([]float64, len(ss))
				for i, s := range ss {
					wall[i], cpu[i] = s.wall, s.cpu
				}
				dst[label][strconv.Itoa(scale)] = Row{Wall: summarise(wall), CPU: summarise(cpu)}
			}
		}
	}
}

// sections is the report's sections, in the order the tables print them.
var sections = []string{"reads", "writes", "okf"}

// section is the rows of one section of the report.
func (r *Report) section(name string) map[string]map[string]Row {
	switch name {
	case "reads":
		return r.Reads
	case "writes":
		return r.Writes
	case "okf":
		return r.OKF
	}
	panic("bench: no report section " + name)
}

func summarise(samples []float64) Stat {
	sorted := slices.Clone(samples)
	slices.Sort(sorted)
	median := sorted[len(sorted)/2]
	if len(sorted)%2 == 0 {
		median = (sorted[len(sorted)/2-1] + sorted[len(sorted)/2]) / 2
	}
	return Stat{Median: median, Min: sorted[0], Max: sorted[len(sorted)-1], Samples: samples}
}

// corpusFor returns the cached corpus under root, generating it when absent,
// when it was generated with another scale, seed or day than asked, or when
// its manifest has no trust counts (the review gates in corpus.Reads need a
// corpus seeded with reviews). A corpus an older khub wrote is kept — the version is in the manifest, and
// the report says so — because regeneration is the slow part by an order
// of magnitude and the bytes are deterministic.
func corpusFor(bin, root string, scale int, seed uint64, today time.Time, regen, chatty bool) (*corpus.Manifest, error) {
	day := today.Format("2006-01-02")
	if !regen {
		if m, err := corpus.Read(root); err == nil {
			g := m.Generated
			reviewed := m.OKF.Trust != nil
			if g.Scale == scale && g.Seed == seed && g.Today == day && reviewed {
				if chatty && g.KhubVersion != version(bin) {
					fmt.Printf("  note: the corpus at %s was written by khub %s; this binary is %s (pass -regen to rebuild)\n",
						root, g.KhubVersion, version(bin))
				}
				return m, nil
			}
			if chatty && !reviewed {
				fmt.Printf("  the corpus at %s carries no reviews; rebuilding\n", root)
			} else if chatty {
				fmt.Printf("  the corpus at %s was generated with scale %d, seed %d, today %s; rebuilding\n",
					root, g.Scale, g.Seed, g.Today)
			}
		}
	}
	if chatty {
		fmt.Printf("  generating %d entities into %s …\n", scale, root)
	}
	return corpus.Build(corpus.Options{Bin: bin, Work: root, Scale: scale, Seed: seed, Today: today})
}

// copyCorpus replaces dst with a copy of src. The cache is never mutated:
// a crash mid-cycle would otherwise corrupt the corpus every later run
// silently reuses.
func copyCorpus(src, dst string) error {
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.CopyFS(dst, os.DirFS(src))
}

// writeCycle runs corpus.Writes once on a probe titled for this repeat and
// returns one sample per verb, in order. The slug comes out of add's JSON;
// the no-op link must say `changed: false`.
func writeCycle(bin, root string, subs map[string]string, env []string, repeat int) ([]sample, error) {
	local := map[string]string{}
	for k, v := range subs {
		local[k] = v
	}
	local["probe_title"] = fmt.Sprintf("Bench probe %d", repeat)
	out := make([]sample, 0, len(corpus.Writes))
	for _, c := range corpus.Writes {
		argv := corpus.Fill(c.Argv, local)
		s, code, stdout, err := invokeOut(bin, root, argv, env)
		if err != nil {
			return nil, err
		}
		if code != c.Exit {
			return nil, fmt.Errorf("khub %s exited %d, wanted %d:\n%s", strings.Join(argv, " "), code, c.Exit, stdout)
		}
		var rec struct {
			Slug    string `json:"slug"`
			Changed *bool  `json:"changed"`
		}
		if err := json.Unmarshal(stdout, &rec); err != nil {
			return nil, fmt.Errorf("khub %s: %w\n%s", strings.Join(argv, " "), err, stdout)
		}
		switch c.Label {
		case "add":
			if rec.Slug == "" {
				return nil, fmt.Errorf("add minted no slug:\n%s", stdout)
			}
			local["probe"] = rec.Slug
		case "link":
			if rec.Changed == nil || !*rec.Changed {
				return nil, fmt.Errorf("the first link reported no change:\n%s", stdout)
			}
		case "link (no-op)":
			if rec.Changed == nil || *rec.Changed {
				return nil, fmt.Errorf("the no-op link reported a change — the measurement would be of a real write:\n%s", stdout)
			}
		}
		out = append(out, s)
	}
	return out, nil
}

var okfLabels = []string{"export --okf", "import --okf"}

// okfCycle exports the corpus into a fresh directory under scratch, then
// imports that bundle into a fresh workspace of the corpus's preset, and
// returns one sample each. The init between them is not timed, and import
// must land the count the manifest expects. Both directories are removed
// first and last, so every repeat starts from nothing.
func okfCycle(bin, root, scratch string, m *corpus.Manifest, env []string) ([]sample, error) {
	bundle, fresh := filepath.Join(scratch, "bundle"), filepath.Join(scratch, "fresh")
	reset := func() error {
		if err := os.RemoveAll(scratch); err != nil {
			return err
		}
		return os.MkdirAll(scratch, 0o755)
	}
	if err := reset(); err != nil {
		return nil, err
	}
	steps := []struct {
		root  string
		argv  []string
		timed bool
	}{
		{root, []string{"export", "--okf", bundle, "--format", "json"}, true},
		{scratch, []string{"init", m.Generated.Preset, fresh, "--no-wire", "--format", "json"}, false},
		{fresh, []string{"import", "--okf", bundle, "--format", "json"}, true},
	}
	var out []sample
	var last []byte
	for _, step := range steps {
		s, code, stdout, err := invokeOut(bin, step.root, step.argv, env)
		if err != nil {
			return nil, err
		}
		if code != 0 {
			return nil, fmt.Errorf("khub %s exited %d, wanted 0:\n%s", strings.Join(step.argv, " "), code, stdout)
		}
		if step.timed {
			out = append(out, s)
		}
		last = stdout
	}
	var rec struct {
		Imported []json.RawMessage `json:"imported"`
	}
	if err := json.Unmarshal(last, &rec); err != nil {
		return nil, fmt.Errorf("khub import: %w\n%s", err, last)
	}
	if len(rec.Imported) != m.OKF.Import.Imported {
		return nil, fmt.Errorf("import landed %d concepts, the manifest expects %d", len(rec.Imported), m.OKF.Import.Imported)
	}
	return out, reset()
}

// treeDiff compares every regular file under a and b, ignoring
// .khub/generated (locks and journals), and names what differs.
func treeDiff(a, b string) string {
	hashes := func(root string) (map[string][32]byte, error) {
		out := map[string][32]byte{}
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			if d.IsDir() {
				if rel == filepath.Join(".khub", "generated") {
					return filepath.SkipDir
				}
				return nil
			}
			if !d.Type().IsRegular() {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			out[filepath.ToSlash(rel)] = sha256.Sum256(raw)
			return nil
		})
		return out, err
	}
	ha, err := hashes(a)
	if err != nil {
		return err.Error()
	}
	hb, err := hashes(b)
	if err != nil {
		return err.Error()
	}
	var lines []string
	for p, h := range ha {
		if hb2, ok := hb[p]; !ok {
			lines = append(lines, "  missing after the cycle: "+p)
		} else if hb2 != h {
			lines = append(lines, "  changed by the cycle: "+p)
		}
	}
	for p := range hb {
		if _, ok := ha[p]; !ok {
			lines = append(lines, "  left behind by the cycle: "+p)
		}
	}
	slices.Sort(lines)
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// invoke runs one khub command against root with its output discarded and
// returns the sample and exit code. A version probe takes no workspace.
func invoke(bin, root string, argv []string, env []string) (sample, int, error) {
	s, code, _, err := invokeOut(bin, root, argv, env)
	return s, code, err
}

func invokeOut(bin, root string, argv []string, env []string) (sample, int, []byte, error) {
	full := argv
	if argv[0] != "--version" {
		full = append([]string{"-C", root}, argv...)
	}
	cmd := exec.Command(bin, full...)
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	start := time.Now()
	err := cmd.Run()
	wall := float64(time.Since(start).Microseconds()) / 1000
	code := 0
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			return sample{}, -1, nil, fmt.Errorf("%s %s: %w", bin, strings.Join(full, " "), err)
		}
		code = exit.ExitCode()
	}
	ps := cmd.ProcessState
	cpu := float64((ps.UserTime() + ps.SystemTime()).Microseconds()) / 1000
	return sample{wall: wall, cpu: cpu}, code, stdout.Bytes(), nil
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

// table prints one row per command in declaration order: wall median per
// scale, then CPU min per scale with its growth. `sizes` is what each corpus
// actually holds — an exponent fitted against the number asked for rather
// than the number written is wrong in exactly the direction that flatters
// the tool.
func (r *Report) table() {
	grows := len(r.Scales) > 1
	lo, hi := r.Scales[0], r.Scales[len(r.Scales)-1]
	for _, section := range sections {
		labels := r.labels[section]
		if len(labels) == 0 {
			continue
		}
		rows := r.section(section)
		fmt.Printf("\n\033[1m%s — wall median ms │ cpu min ms, out of process\033[0m\n", section)
		var head strings.Builder
		for _, s := range r.Scales {
			fmt.Fprintf(&head, "%11s", "n="+strconv.Itoa(r.sizes[s]))
		}
		tail := ""
		if grows {
			tail = fmt.Sprintf("%10s", "growth")
		}
		fmt.Printf("  %-28s%s │%s%s\n", "command", head.String(), head.String(), tail)
		fmt.Printf("  %s\n", strings.Repeat("-", 28+22*len(r.Scales)+2+len(tail)))
		for _, label := range labels {
			per := rows[label]
			var wall, cpu strings.Builder
			for _, s := range r.Scales {
				row := per[strconv.Itoa(s)]
				fmt.Fprintf(&wall, "%11.1f", row.Wall.Median)
				fmt.Fprintf(&cpu, "%11.1f", row.CPU.Min)
			}
			growth := ""
			if grows {
				b := exponent(r.sizes[lo], per[strconv.Itoa(lo)].CPU.Min, r.sizes[hi], per[strconv.Itoa(hi)].CPU.Min)
				growth = fmt.Sprintf("%10s", fmt.Sprintf("n^%.2f", b))
			}
			fmt.Printf("  %-28s%s │%s%s\n", label, wall.String(), cpu.String(), growth)
		}
	}
	if grows {
		fmt.Println("  growth is fitted on cpu min between the smallest and the largest scale; n^1.0 is linear, n^2.0 the wall")
	}
	for key, v := range r.WriteCheck {
		if v == "identical" {
			fmt.Printf("  write cycle at n=%s left the corpus byte-identical\n", key)
		}
	}
}

// --- the machine --------------------------------------------------------------

func version(bin string) string {
	out, err := exec.Command(bin, "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func commit() string {
	out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// cpuModel is best effort: /proc/cpuinfo on linux, sysctl on darwin, else
// the architecture. The gate only compares it for equality.
func cpuModel() string {
	switch runtime.GOOS {
	case "linux":
		raw, err := os.ReadFile("/proc/cpuinfo")
		if err == nil {
			for line := range strings.SplitSeq(string(raw), "\n") {
				if name, value, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(name) == "model name" {
					return strings.TrimSpace(value)
				}
			}
		}
	case "darwin":
		if out, err := exec.Command("sysctl", "-n", "machdep.cpu.brand_string").Output(); err == nil {
			return strings.TrimSpace(string(out))
		}
	}
	return runtime.GOARCH
}

func loadavg() []float64 {
	raw, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return nil
	}
	fields := strings.Fields(string(raw))
	if len(fields) < 3 {
		return nil
	}
	out := make([]float64, 0, 3)
	for _, f := range fields[:3] {
		v, err := strconv.ParseFloat(f, 64)
		if err != nil {
			return nil
		}
		out = append(out, v)
	}
	return out
}

func joinInts(ns []int) string {
	parts := make([]string, len(ns))
	for i, n := range ns {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ", ")
}
