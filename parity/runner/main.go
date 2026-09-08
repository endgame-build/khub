// parity-run — the golden-fixture harness (go-port-plan M1).
//
//	parity-run -bin "<cmd…>" -record -cases parity/cases [-only fam[/case]]
//	parity-run -bin "<cmd…>" -cases parity/cases [-only …]     # verify
//
// A case is a directory holding case.yaml:
//
//	mode: pipe | human | pty        # default pipe
//	env:  {K: V}                    # optional extras
//	steps:
//	  - sh: "git init -q ."         # deterministic setup, must exit 0
//	  - argv: [init, build-hub, ., --no-wire]
//	    stdin: "optional bytes"
//
// Every argv step records stdout, stderr, and exit code separately under
// expected/steps/NN.{stdout,stderr,exit}; after the last step the workspace
// tree (minus .git/** and .khub/generated/**) digests into
// expected/tree.manifest. Verification is byte-exact except two named
// normalizers (cycles-canonical, tty-layout-free) declared per case.
package main

import (
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
)

type step struct {
	Sh    string   `yaml:"sh"`
	Argv  []string `yaml:"argv"`
	Stdin *string  `yaml:"stdin"`
}

type caseSpec struct {
	Mode      string            `yaml:"mode"`
	Env       map[string]string `yaml:"env"`
	Normalize []string          `yaml:"normalize"`
	Steps     []step            `yaml:"steps"`
	// Divergence records a KNOWN, accepted difference from Python for this
	// case (see parity/DECISIONS.md). The case still runs and still reports,
	// but a mismatch counts as XFAIL rather than FAIL. It is deliberately
	// per-case and noisy: the reason is printed on every run so an accepted
	// divergence can never quietly become invisible. An unexpected PASS is
	// reported too, so a stale entry gets noticed.
	Divergence string `yaml:"divergence"`
}

var (
	binFlag      = flag.String("bin", "", "command to run (space-split; argv appended)")
	record       = flag.Bool("record", false, "record expectations instead of verifying")
	casesDir     = flag.String("cases", "parity/cases", "cases root")
	only         = flag.String("only", "", "family or family/case filter")
	coverage     = flag.String("coverage", "", "coverage.yaml path: report fixture gaps instead of running")
	subsetBin    = flag.String("subset-of", "", "with -coverage: assert this binary's command set is a subset of coverage.yaml's (catches commands the port ADDS)")
	differ       = flag.Bool("differential", false, "property mode: random verb sequences through two binaries, compared after every step")
	binB         = flag.String("bin-b", "", "second binary for -differential")
	seedWS       = flag.String("seed-ws", "", "workspace to clone for each -differential seed")
	seeds        = flag.Int("seeds", 20, "-differential: number of seeded sequences")
	seqLen       = flag.Int("len", 40, "-differential: steps per sequence")
	verboseSteps = flag.Bool("v", false, "-differential: print every command as it runs")
	workflowMode = flag.Bool("workflows", false, "-differential: emit realistic workflow arcs instead of uniform random verbs")
	setupBin     = flag.String("setup-bin", "", "run steps whose verb is not yet ported with this binary (dual-ship bridge)")
	portedCSV    = flag.String("ported", "", "comma-separated verbs -bin implements; others use -setup-bin")
	repoRoot     = flag.String("repo", ".", "repo root (for tool paths)")
	showDiffN    = flag.Int("diffs", 6, "max diffs to print per case")
)

func main() {
	flag.Parse()
	if *coverage != "" {
		os.Exit(runCoverage(*coverage))
	}
	if *differ {
		if *binFlag == "" || *binB == "" || *seedWS == "" {
			fmt.Fprintln(os.Stderr, "-differential needs -bin, -bin-b and -seed-ws")
			os.Exit(2)
		}
		os.Exit(runDifferential(*binFlag, *binB, *seedWS, *seeds, *seqLen))
	}
	if *binFlag == "" {
		fmt.Fprintln(os.Stderr, "need -bin")
		os.Exit(2)
	}
	var caseDirs []string
	_ = filepath.Walk(*casesDir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && info.Name() == "case.yaml" {
			rel, _ := filepath.Rel(*casesDir, filepath.Dir(p))
			if *only == "" || rel == *only || strings.HasPrefix(rel, *only+"/") {
				caseDirs = append(caseDirs, filepath.Dir(p))
			}
		}
		return nil
	})
	sort.Strings(caseDirs)
	if len(caseDirs) == 0 {
		fmt.Fprintln(os.Stderr, "no cases matched")
		os.Exit(2)
	}
	pass, fail, xfail := 0, 0, 0
	for _, dir := range caseDirs {
		ok, err := runCase(dir)
		rel, _ := filepath.Rel(*casesDir, dir)
		reason := divergenceOf(dir)
		switch {
		case err != nil:
			fmt.Printf("ERROR %s: %v\n", rel, err)
			fail++
		case ok && reason != "":
			fmt.Printf("XPASS %s: recorded divergence no longer reproduces — remove it: %s\n", rel, reason)
			pass++
		case ok:
			pass++
		case reason != "":
			fmt.Printf("XFAIL %s: %s\n", rel, reason)
			xfail++
		default:
			fail++
		}
	}
	verb := "verified"
	if *record {
		verb = "recorded"
	}
	fmt.Printf("\n%s %d cases: %d pass, %d fail, %d xfail (recorded divergences)\n",
		verb, len(caseDirs), pass, fail, xfail)
	if fail > 0 {
		os.Exit(1)
	}
}

func runCase(dir string) (bool, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "case.yaml"))
	if err != nil {
		return false, err
	}
	var spec caseSpec
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		return false, err
	}
	if spec.Mode == "" {
		spec.Mode = "pipe"
	}
	work, err := os.MkdirTemp("", "parity-*")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(work)
	home := filepath.Join(work, ".home")
	ws := filepath.Join(work, "ws")
	_ = os.MkdirAll(home, 0o755)
	_ = os.MkdirAll(ws, 0o755)

	env := baseEnv(home, spec.Mode)
	for k, v := range spec.Env {
		env = append(env, k+"="+v)
	}

	expDir := filepath.Join(dir, "expected")
	stepsDir := filepath.Join(expDir, "steps")
	if *record {
		_ = os.RemoveAll(expDir)
		_ = os.MkdirAll(stepsDir, 0o755)
	}

	norm := newPathNormalizer(ws, home)
	ok := true
	// A `divergence:` line is case-scoped, so record WHICH steps actually
	// mismatched: the reason names one behaviour, and a case with many steps
	// must not let an unrelated regression in a different step ride along
	// silently under the same exemption.
	divergedSteps := map[int]bool{}
	argvIdx := 0
	for _, st := range spec.Steps {
		if st.Sh != "" {
			cmd := exec.Command("sh", "-c", st.Sh)
			cmd.Dir = ws
			cmd.Env = env
			out, err := cmd.CombinedOutput()
			if err != nil {
				return false, fmt.Errorf("sh step failed: %s: %v\n%s", st.Sh, err, out)
			}
			continue
		}
		argvIdx++
		bin := *binFlag
		if *setupBin != "" && !isPorted(st.Argv) {
			bin = *setupBin
		}
		stdout, stderr, code, err := runStepWith(bin, ws, env, st, spec.Mode)
		if err != nil {
			return false, err
		}
		stdout = norm.apply(stdout)
		stderr = norm.apply(stderr)
		base := filepath.Join(stepsDir, fmt.Sprintf("%02d", argvIdx))
		if *record {
			if err := os.WriteFile(base+".stdout", stdout, 0o644); err != nil {
				return false, err
			}
			if err := os.WriteFile(base+".stderr", stderr, 0o644); err != nil {
				return false, err
			}
			if err := os.WriteFile(base+".exit", []byte(strconv.Itoa(code)+"\n"), 0o644); err != nil {
				return false, err
			}
			continue
		}
		wantOut, oerr := os.ReadFile(base + ".stdout")
		wantErr, eerr := os.ReadFile(base + ".stderr")
		wantCodeRaw, cerr := os.ReadFile(base + ".exit")
		if oerr != nil || eerr != nil || cerr != nil {
			return false, fmt.Errorf("missing expectation for step %d (re-record with parity/tools/record.sh): %v",
				argvIdx, firstErr(oerr, eerr, cerr))
		}
		wantCode, aerr := strconv.Atoi(strings.TrimSpace(string(wantCodeRaw)))
		if aerr != nil {
			return false, fmt.Errorf("unreadable exit expectation for step %d: %w", argvIdx, aerr)
		}
		rel, _ := filepath.Rel(*casesDir, dir)
		label := fmt.Sprintf("%s step %d [%s]", rel, argvIdx, strings.Join(st.Argv, " "))
		if code != wantCode {
			fmt.Printf("FAIL %s: exit %d want %d\n", label, code, wantCode)
			ok = false
			divergedSteps[argvIdx] = true
		}
		if !bytesEqualNorm(stdout, wantOut, spec, st, "stdout") {
			fmt.Printf("FAIL %s stdout:\n%s", label, firstDiff(wantOut, stdout))
			ok = false
			divergedSteps[argvIdx] = true
		}
		if !bytesEqualNorm(stderr, wantErr, spec, st, "stderr") {
			fmt.Printf("FAIL %s stderr:\n%s", label, firstDiff(wantErr, stderr))
			ok = false
			divergedSteps[argvIdx] = true
		}
	}

	if !ok && spec.Divergence != "" {
		steps := make([]int, 0, len(divergedSteps))
		for s := range divergedSteps {
			steps = append(steps, s)
		}
		sort.Ints(steps)
		fmt.Printf("      diverging steps: %v (reason is case-scoped — confirm every one is the recorded cause)\n", steps)
	}

	manifest := treeManifest(ws, norm)
	manPath := filepath.Join(expDir, "tree.manifest")
	if *record {
		return true, os.WriteFile(manPath, []byte(manifest), 0o644)
	}
	wantMan, _ := os.ReadFile(manPath)
	if manifest != string(wantMan) {
		rel, _ := filepath.Rel(*casesDir, dir)
		fmt.Printf("FAIL %s tree.manifest:\n%s", rel, firstDiff(wantMan, []byte(manifest)))
		ok = false
	}
	return ok, nil
}

// extraCommands reports commands the binary offers that the Python surface
// does not declare.
func extraCommands(spec coverageSpec) []string {
	if *subsetBin == "" {
		return nil
	}
	declared := map[string]bool{}
	for _, c := range spec.Commands {
		declared[strings.Split(c.Name, " ")[0]] = true
	}
	parts := strings.Fields(*subsetBin)
	out, err := exec.Command(parts[0], append(parts[1:], "--help")...).CombinedOutput()
	if err != nil && len(out) == 0 {
		return []string{"subset check could not run " + *subsetBin + ": " + err.Error()}
	}
	var extra []string
	inCommands := false
	sawPanel := false
	for _, line := range strings.Split(string(out), "\n") {
		// Two help renderings: cobra's plain "Available Commands:" list and the
		// Rich panel "╭─ Commands ─…╮" the port now emits. Scanning for only
		// the first made this gate silently find nothing.
		if strings.HasPrefix(line, "Available Commands:") || strings.HasPrefix(line, "Commands:") ||
			strings.HasPrefix(line, "╭─ Commands ") {
			inCommands, sawPanel = true, true
			continue
		}
		if !inCommands {
			continue
		}
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "Flags:") || strings.HasPrefix(line, "╰") {
			inCommands = false
			continue
		}
		// A Rich panel row is "│ name        description"; a wrapped
		// description continues as "│             more text". Only the former
		// names a command, so require a single space after the bar.
		body, isRow := strings.CutPrefix(line, "│")
		if !isRow {
			body = line
		}
		if len(body) < 2 || body[0] != ' ' || body[1] == ' ' {
			continue
		}
		fields := strings.Fields(body)
		if len(fields) == 0 {
			continue
		}
		name := fields[0]
		if !declared[name] {
			extra = append(extra, "command NOT in the Python surface: "+name)
		}
	}
	if !sawPanel {
		// Never pass by finding nothing: if the command list could not be
		// located at all, the check proved nothing.
		return []string{"subset check could not find the command list in " + *subsetBin + " --help"}
	}
	return extra
}

// divergenceOf reads a case's recorded-divergence reason, if any.
func divergenceOf(dir string) string {
	raw, err := os.ReadFile(filepath.Join(dir, "case.yaml"))
	if err != nil {
		return ""
	}
	var cs caseSpec
	if yaml.Unmarshal(raw, &cs) != nil {
		return ""
	}
	return cs.Divergence
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

func baseEnv(home, mode string) []string {
	env := []string{
		"HOME=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"TZ=UTC", "LANG=C.UTF-8", "LC_ALL=C.UTF-8",
		"COLUMNS=80",
		"KHUB_PARITY_NOW=2026-01-15",
		"GIT_AUTHOR_NAME=Parity", "GIT_AUTHOR_EMAIL=parity@example.com",
		"GIT_COMMITTER_NAME=Parity", "GIT_COMMITTER_EMAIL=parity@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"PATH=" + os.Getenv("PATH"),
	}
	// The recorder side needs the pyshim on PYTHONPATH; harmless for Go.
	abs, _ := filepath.Abs(*repoRoot)
	env = append(env, "PYTHONPATH="+filepath.Join(abs, "parity", "tools", "pyshim"))
	switch mode {
	case "human":
		env = append(env, "TTY_COMPATIBLE=1", "NO_COLOR=1")
	case "pipe":
		env = append(env, "TTY_COMPATIBLE=0")
	case "pty":
		// Deliberately NO TTY_COMPATIBLE and NO FORCE_COLOR: the whole point of
		// this mode is that the third rung of the gate — stdout.isatty() — is
		// the only thing left to decide. See pty.go.
		//
		// TERM=dumb is the one hint that IS set. Rich reads TERM: any ordinary
		// value gives it a colour system and it emits SGR into the table, while
		// the port emits none. Recording that and leaning on tty-layout-free to
		// strip it would certify a colour divergence as a pass. `dumb` puts
		// Rich on the port's footing (Console._detect_color_system returns None
		// for a dumb terminal) so these fixtures compare real bytes. The
		// residual gap — nobody compares COLOURED output — is named in
		// parity/DECISIONS.md, not hidden here.
		env = append(env, "TERM=dumb")
	}
	return env
}

// isPorted reports whether -bin implements this step's verb; -ported lists the
// verbs already landed, so a partially-ported binary is still fixture-checkable
// (setup steps run through the Python bridge).
func isPorted(argv []string) bool {
	if *portedCSV == "" {
		return true
	}
	verb := ""
	for _, a := range argv {
		if !strings.HasPrefix(a, "-") {
			verb = a
			break
		}
	}
	for _, p := range strings.Split(*portedCSV, ",") {
		if strings.TrimSpace(p) == verb {
			return true
		}
	}
	return false
}

func runStepWith(bin, ws string, env []string, st step, mode string) (stdout, stderr []byte, code int, err error) {
	parts := strings.Fields(bin)
	absRepo, _ := filepath.Abs(*repoRoot)
	stepArgv := make([]string, len(st.Argv))
	for i, a := range st.Argv {
		stepArgv[i] = strings.ReplaceAll(a, "PRESETSRC", filepath.Join(absRepo, "parity", "corpus"))
	}
	argv := append(append([]string{}, parts[1:]...), stepArgv...)
	cmd := exec.Command(parts[0], argv...)
	cmd.Dir = ws
	cmd.Env = env
	if st.Stdin != nil {
		cmd.Stdin = strings.NewReader(*st.Stdin)
	}
	if mode == "pty" {
		return runOnPTY(cmd)
	}
	var outB, errB strings.Builder
	cmd.Stdout = &outB
	cmd.Stderr = &errB
	runErr := cmd.Run()
	code = 0
	if runErr != nil {
		var ee *exec.ExitError
		if errors.As(runErr, &ee) {
			code = ee.ExitCode()
		} else {
			return nil, nil, 0, runErr
		}
	}
	return []byte(outB.String()), []byte(errB.String()), code, nil
}

func treeManifest(root string, norm *pathNormalizer) string {
	type entry struct{ rel, line string }
	var entries []entry
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, ".git/") || strings.Contains(rel, "/.git/") ||
			strings.Contains(rel, ".khub/generated/") {
			return nil
		}
		raw, _ := os.ReadFile(p)
		// Hash NORMALIZED content: a scaffolded config.yaml records the
		// absolute --preset-source path, so the raw bytes differ per checkout
		// while the file is semantically identical.
		if norm != nil {
			raw = norm.apply(raw)
		}
		mode := "-"
		if info.Mode()&0o111 != 0 {
			mode = "x"
		}
		entries = append(entries, entry{rel: rel,
			line: fmt.Sprintf("%x %s %s", sha256.Sum256(raw), mode, rel)})
		return nil
	})
	// Sort by PATH. Sorting the composed line ordered by hash, so one changed
	// byte reshuffled the whole manifest and the diff pointed at the wrong file.
	sort.Slice(entries, func(i, j int) bool { return entries[i].rel < entries[j].rel })
	lines := make([]string, len(entries))
	for i, e := range entries {
		lines[i] = e.line
	}
	return strings.Join(lines, "\n") + "\n"
}

// pathNormalizer replaces the run's own temp roots (and their /private
// realpath variants on macOS) with stable placeholders, so error messages and
// --global paths that embed absolute paths stay comparable across runs.
type pathNormalizer struct{ pairs [][2]string }

func newPathNormalizer(ws, home string) *pathNormalizer {
	n := &pathNormalizer{}
	add := func(p, placeholder string) {
		if p == "" {
			return
		}
		real, err := filepath.EvalSymlinks(p)
		if err == nil && real != p {
			n.pairs = append(n.pairs, [2]string{real, placeholder})
		}
		n.pairs = append(n.pairs, [2]string{p, placeholder})
	}
	add(ws, "%WS%")
	add(home, "%HOME%")
	// The repo root leaks into recorded bytes through --preset-source (init
	// echoes `source`, and writes it into .khub/config.yaml), so a fixture
	// recorded in one checkout could never verify in another. That made the
	// suite machine-specific rather than portable.
	if abs, err := filepath.Abs(*repoRoot); err == nil {
		add(abs, "%REPO%")
	}
	return n
}

func (n *pathNormalizer) apply(b []byte) []byte {
	s := string(b)
	for _, pr := range n.pairs {
		s = strings.ReplaceAll(s, pr[0], pr[1])
		s = strings.ReplaceAll(s, strings.ReplaceAll(pr[0], "/", `\/`), pr[1]) // JSON-escaped form
	}
	return []byte(s)
}

// --- normalizers -------------------------------------------------------------

var ansiRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

func bytesEqualNorm(got, want []byte, spec caseSpec, st step, channel string) bool {
	if string(got) == string(want) {
		return true
	}
	for _, n := range spec.Normalize {
		switch n {
		case "tty-layout-free":
			if channel == "stdout" && layoutFree(string(got)) == layoutFree(string(want)) {
				return true
			}
		case "cycles-canonical":
			if canonCycles(string(got)) == canonCycles(string(want)) {
				return true
			}
		}
	}
	return false
}

// layoutFree strips ANSI codes, box-drawing, and run-length whitespace so a
// human table compares on content, order, and literals only (go-port-plan:
// TTY output is content-pinned).
func layoutFree(s string) string {
	s = ansiRe.ReplaceAllString(s, "")
	var b strings.Builder
	for _, ch := range s {
		if ch >= 0x2500 && ch <= 0x257f { // box drawing
			b.WriteRune(' ')
			continue
		}
		b.WriteRune(ch)
	}
	lines := strings.Split(b.String(), "\n")
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		ln = strings.Join(strings.Fields(ln), " ")
		if ln != "" {
			out = append(out, ln)
		}
	}
	return strings.Join(out, "\n")
}

// canonCycles rotates each cycle to start at its smallest member — both the
// human "cycle {a -> b -> c}" lines and the JSON "cycles": [[…]] arrays —
// because Johnson enumeration rotation is implementation-defined (and
// observably unstable across CPython runs).
func canonCycles(s string) string {
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, "cycle {") && strings.HasSuffix(trimmed, "}") {
			inner := strings.TrimSuffix(strings.TrimPrefix(trimmed, "cycle {"), "}")
			parts := strings.Split(inner, " -> ")
			parts = rotateMin(parts)
			lines[i] = strings.Replace(ln, inner, strings.Join(parts, " -> "), 1)
		}
		if idx := strings.Index(ln, `"cycles": [[`); idx >= 0 {
			lines[i] = canonJSONCycles(ln, idx)
		}
	}
	return strings.Join(lines, "\n")
}

var cycleArrayRe = regexp.MustCompile(`\[("[^"\]]+"(?:, "[^"\]]+")*)\]`)

func canonJSONCycles(ln string, idx int) string {
	start := idx + len(`"cycles": [`)
	depth := 1
	end := start
	for ; end < len(ln) && depth > 0; end++ {
		switch ln[end] {
		case '[':
			depth++
		case ']':
			depth--
		}
	}
	segment := ln[start : end-1]
	var arrays []string
	for _, m := range cycleArrayRe.FindAllStringSubmatch(segment, -1) {
		parts := strings.Split(m[1], ", ")
		parts = rotateMin(parts)
		arrays = append(arrays, "["+strings.Join(parts, ", ")+"]")
	}
	sort.Strings(arrays)
	return ln[:start] + strings.Join(arrays, ", ") + ln[end-1:]
}

func rotateMin(parts []string) []string {
	if len(parts) == 0 {
		return parts
	}
	minI := 0
	for i, p := range parts {
		if p < parts[minI] {
			minI = i
		}
	}
	return append(append([]string{}, parts[minI:]...), parts[:minI]...)
}

func firstDiff(want, got []byte) string {
	w := strings.Split(string(want), "\n")
	g := strings.Split(string(got), "\n")
	var b strings.Builder
	shown := 0
	for i := 0; (i < len(w) || i < len(g)) && shown < *showDiffN; i++ {
		wl, gl := "", ""
		if i < len(w) {
			wl = w[i]
		}
		if i < len(g) {
			gl = g[i]
		}
		if wl != gl {
			fmt.Fprintf(&b, "  L%d want %q\n  L%d got  %q\n", i+1, wl, i+1, gl)
			shown++
		}
	}
	return b.String()
}

// --- coverage gate (go-port-plan A.5) ---------------------------------------

type coverageSpec struct {
	Commands []struct {
		Name   string   `yaml:"name"`
		Params []string `yaml:"params"`
	} `yaml:"commands"`
	ErrorCodes     []string `yaml:"error_codes"`
	FormatSpecials []string `yaml:"format_specials"`
	Modes          []string `yaml:"modes"`
	Unreachable    []string `yaml:"unreachable"`
}

func runCoverage(specPath string) int {
	raw, err := os.ReadFile(specPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	var spec coverageSpec
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	var argvBlob, outBlob strings.Builder
	var coverageErrs []string
	modes := map[string]bool{}
	_ = filepath.Walk(*casesDir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		switch {
		case info.Name() == "case.yaml":
			raw, rerr := os.ReadFile(p)
			if rerr != nil {
				coverageErrs = append(coverageErrs, p+": "+rerr.Error())
				return nil
			}
			var cs caseSpec
			if uerr := yaml.Unmarshal(raw, &cs); uerr != nil {
				// An unparseable case contributes nothing to coverage; failing
				// loudly beats a gate that silently under-counts.
				coverageErrs = append(coverageErrs, p+": "+uerr.Error())
				return nil
			}
			if cs.Mode == "" {
				cs.Mode = "pipe"
			}
			modes[cs.Mode] = true
			for _, st := range cs.Steps {
				argvBlob.WriteString(" §" + strings.Join(st.Argv, " §") + "\n")
			}
		case strings.HasSuffix(p, ".stdout") || strings.HasSuffix(p, ".stderr"):
			raw, _ := os.ReadFile(p)
			outBlob.Write(raw)
		}
		return nil
	})

	skip := map[string]bool{}
	for _, u := range spec.Unreachable {
		skip[strings.Fields(u)[0]] = true
		skip[u] = true
	}
	var gaps []string
	for _, c := range spec.Commands {
		if c.Name == "(root)" {
			continue
		}
		first := strings.Split(c.Name, " ")[0]
		if !strings.Contains(argvBlob.String(), " §"+first) {
			gaps = append(gaps, "command uncovered: "+c.Name)
			continue
		}
		for _, prm := range c.Params {
			if strings.HasPrefix(prm, "arg:") || prm == "--format" || prm == "--help" || skip[c.Name+" "+prm] {
				continue
			}
			if !strings.Contains(argvBlob.String(), "§"+prm+"\n") &&
				!strings.Contains(argvBlob.String(), "§"+prm+" ") &&
				!strings.Contains(argvBlob.String(), "§"+prm+"=") {
				gaps = append(gaps, "param uncovered: "+c.Name+" "+prm)
			}
		}
	}
	for _, code := range spec.ErrorCodes {
		if skip[code] {
			continue
		}
		// Only a real error envelope counts. A bare substring match anywhere in
		// recorded output would mark a code "covered" because some unrelated
		// prose happened to contain the word.
		if !strings.Contains(outBlob.String(), `"code": "`+code+`"`) {
			gaps = append(gaps, "error code uncovered: "+code)
		}
	}
	for _, f := range spec.FormatSpecials {
		// The value follows --format as its own argv token.
		if !strings.Contains(argvBlob.String(), "§"+f+"\n") &&
			!strings.Contains(argvBlob.String(), "§--format §"+f) {
			gaps = append(gaps, "format special uncovered: "+f)
		}
	}
	for _, m := range spec.Modes {
		if !modes[m] {
			gaps = append(gaps, "mode uncovered: "+m)
		}
	}
	// The gate is generated from Python's surface, so it proves every Python
	// command is covered — but it is blind to a command the port ADDS (cobra
	// ships `completion` by default, which Python rejects). Close that.
	gaps = append(gaps, extraCommands(spec)...)
	for _, e := range coverageErrs {
		gaps = append(gaps, "unreadable case: "+e)
	}
	sort.Strings(gaps)
	for _, g := range gaps {
		fmt.Println("GAP " + g)
	}
	fmt.Printf("coverage: %d gaps\n", len(gaps))
	if len(gaps) > 0 {
		return 1
	}
	return 0
}
