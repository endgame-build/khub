// footprint — how many bytes each read puts in front of an agent, pinned.
//
// An agent reads khub's JSON into its context, so the size of every answer is
// a cost khub charges on each call, and nothing measured it. This runs the
// read surface (parity/scale/corpus.Reads, minus the process floor) against a
// generated corpus on a pipe, from inside the corpus, with the clock pinned,
// and records per command the exit code, stdout and stderr bytes, the row
// count of an array answer, and a hash of stdout. The bytes are
// deterministic — byte-identical across runs, workspace paths and a fresh
// regeneration — so the record is an exact pin, like a parity fixture: a
// change is re-recorded deliberately, with the delta table this prints in the
// commit that grows an answer.
//
//	footprint -bin ./khub -work <corpus> -record > parity/scale/footprint.json
//	footprint -bin ./khub -work <corpus> -check parity/scale/footprint.json
//
// The corpus is fingerprinted (every file but .khub/generated and the
// manifest), so a generator or preset change reads as "the corpus moved,
// re-record" (exit 2) and never as "khub's output grew" (exit 1). A record
// made at another scale than the corpus given is reported and skipped (exit
// 0): parity/scale/smoke.sh calls this at whatever scale it ran.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	// A dev tool, not a khub output path — see parity/scale/corpus/corpus.go.
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/endgame-build/khub/parity/scale/corpus"
)

// Record is the file. Case order is corpus.Reads order.
type Record struct {
	Corpus Fingerprint `json:"corpus"`
	Env    []string    `json:"env"`
	Cases  []Entry     `json:"cases"`
}

type Fingerprint struct {
	corpus.Generated
	Total      int    `json:"total"`
	TreeSHA256 string `json:"tree_sha256"`
}

// Entry is one command. Exit, StdoutBytes, StderrBytes and Rows are gated;
// SHA256 is shown so a same-size content change is visible to the reviewer
// without being a second byte contract beside parity.
type Entry struct {
	Label       string   `json:"label"`
	Argv        []string `json:"argv"`
	Exit        int      `json:"exit"`
	StdoutBytes int      `json:"stdout_bytes"`
	StderrBytes int      `json:"stderr_bytes"`
	Rows        int      `json:"rows"`
	SHA256      string   `json:"sha256"`
}

// What the agent's pipe sees: no terminal, 80 columns for anything that
// still asks, the corpus's own day in UTC (OKF stamps are UTC instants).
var pinned = []string{"COLUMNS=80", "TTY_COMPATIBLE=0", "NO_COLOR=1", "TZ=UTC"}

func main() {
	bin := flag.String("bin", "./khub", "the khub binary to measure")
	work := flag.String("work", "", "a generated corpus (required)")
	check := flag.String("check", "", "compare against this record")
	record := flag.Bool("record", false, "print a fresh record to stdout")
	flag.Parse()
	if *work == "" || (*check == "") == !*record {
		fmt.Fprintln(os.Stderr, "usage: footprint -bin <khub> -work <corpus> (-record | -check <file>)")
		os.Exit(2)
	}
	binPath, err := filepath.Abs(*bin)
	if err != nil {
		fail(err)
	}
	root, err := filepath.Abs(*work)
	if err != nil {
		fail(err)
	}
	m, err := corpus.Read(root)
	if err != nil {
		fail(fmt.Errorf("no corpus at %s: %w", root, err))
	}

	var want Record
	if *check != "" {
		raw, err := os.ReadFile(*check)
		if err != nil {
			fail(err)
		}
		if err := json.Unmarshal(raw, &want); err != nil {
			fail(fmt.Errorf("%s: %w", *check, err))
		}
		if want.Corpus.Scale != m.Generated.Scale {
			fmt.Printf("footprint: %s was recorded at scale %d and this corpus is %d; nothing to compare\n",
				*check, want.Corpus.Scale, m.Generated.Scale)
			return
		}
	}

	got := Record{
		Corpus: Fingerprint{Generated: m.Generated, Total: m.Total, TreeSHA256: fingerprint(root)},
		Env:    append([]string{"KHUB_PARITY_NOW=" + m.Generated.Today}, pinned...),
	}
	env := append(os.Environ(), got.Env...)
	subs := corpus.Handles(m)
	for _, c := range corpus.Reads {
		if c.Label == corpus.Floor {
			continue
		}
		argv := corpus.Fill(c.Argv, subs)
		code, stdout, stderr, err := run(binPath, root, argv, env)
		if err != nil {
			fail(err)
		}
		if code != c.Exit {
			fail(fmt.Errorf("khub %s exited %d, wanted %d:\n%s%s", strings.Join(argv, " "), code, c.Exit, stdout, stderr))
		}
		sum := sha256.Sum256(stdout)
		got.Cases = append(got.Cases, Entry{
			Label: c.Label, Argv: argv, Exit: code,
			StdoutBytes: len(stdout), StderrBytes: len(stderr), Rows: rows(stdout),
			SHA256: hex.EncodeToString(sum[:]),
		})
	}

	if *record {
		out, err := json.MarshalIndent(got, "", "  ")
		if err != nil {
			fail(err)
		}
		fmt.Println(string(out))
		if *check != "" {
			os.Exit(compare(want, got, os.Stderr))
		}
		return
	}
	os.Exit(compare(want, got, os.Stdout))
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "footprint: %v\n", err)
	os.Exit(1)
}

// compare prints the delta table and returns the exit code: 0 identical,
// 1 an answer moved, 2 the corpus moved.
func compare(want, got Record, w *os.File) int {
	if want.Corpus.TreeSHA256 != got.Corpus.TreeSHA256 {
		fmt.Fprintf(w, "footprint: the corpus moved (generator, preset or writer changed; scale %d, seed %d, today %s on both sides): re-record with -record\n",
			got.Corpus.Scale, got.Corpus.Seed, got.Corpus.Today)
		return 2
	}
	byLabel := map[string]Entry{}
	for _, e := range want.Cases {
		byLabel[e.Label] = e
	}
	moved := 0
	fmt.Fprintf(w, "  %-28s %5s %9s %9s %6s  %s\n", "command", "exit", "stdout B", "stderr B", "rows", "")
	for _, g := range got.Cases {
		e, ok := byLabel[g.Label]
		delete(byLabel, g.Label)
		note := ""
		switch {
		case !ok:
			note = "NEW, not in the record"
			moved++
		case e.Exit != g.Exit || e.StdoutBytes != g.StdoutBytes || e.StderrBytes != g.StderrBytes || e.Rows != g.Rows:
			note = fmt.Sprintf("MOVED from exit %d, %d B stdout, %d B stderr, %d rows (%+d B, %+.1f%%)",
				e.Exit, e.StdoutBytes, e.StderrBytes, e.Rows, g.StdoutBytes-e.StdoutBytes, pct(e.StdoutBytes, g.StdoutBytes))
			moved++
		case e.SHA256 != g.SHA256:
			note = "same size, content changed (not gated)"
		}
		fmt.Fprintf(w, "  %-28s %5d %9d %9d %6d  %s\n", g.Label, g.Exit, g.StdoutBytes, g.StderrBytes, g.Rows, note)
	}
	for label := range byLabel {
		fmt.Fprintf(w, "  %-28s  GONE from the read surface, still in the record\n", label)
		moved++
	}
	if moved > 0 {
		fmt.Fprintf(w, "footprint: %d answer(s) moved; if that is intended, re-record with -record and say why in the commit\n", moved)
		return 1
	}
	fmt.Fprintf(w, "footprint: %d answers match the record\n", len(got.Cases))
	return 0
}

func pct(from, to int) float64 {
	if from == 0 {
		return 0
	}
	return float64(to-from) * 100 / float64(from)
}

// rows is the length of an array answer, 0 for anything else.
func rows(stdout []byte) int {
	trimmed := bytes.TrimSpace(stdout)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return 0
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(trimmed, &arr); err != nil {
		return 0
	}
	return len(arr)
}

// fingerprint hashes every regular file under root but .khub/generated
// (locks, journals), the manifest (which names the writer's version) and
// .khub/config.yaml (which names the workspace after its directory), in
// path order — the same files the scale smoke's determinism pass compares.
func fingerprint(root string) string {
	type file struct {
		path string
		sum  [32]byte
	}
	var files []file
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
		if !d.Type().IsRegular() || rel == corpus.ManifestName || rel == filepath.Join(".khub", "config.yaml") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files = append(files, file{filepath.ToSlash(rel), sha256.Sum256(raw)})
		return nil
	})
	if err != nil {
		fail(err)
	}
	slices.SortFunc(files, func(a, b file) int { return strings.Compare(a.path, b.path) })
	h := sha256.New()
	for _, f := range files {
		h.Write([]byte(f.path))
		h.Write([]byte{0})
		h.Write(f.sum[:])
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func run(bin, root string, argv []string, env []string) (int, []byte, []byte, error) {
	// From inside the corpus, so an answer that spells the workspace path
	// (upgrade's) reads the same wherever the corpus lives.
	cmd := exec.Command(bin, append([]string{"-C", root}, argv...)...)
	cmd.Dir = root
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return 0, stdout.Bytes(), stderr.Bytes(), nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), stdout.Bytes(), stderr.Bytes(), nil
	}
	return -1, nil, nil, fmt.Errorf("%s %s: %w", bin, strings.Join(argv, " "), err)
}
