package main

// Differential (property) mode — go-port-plan M7.
//
// The golden fixtures are single-shot: fresh workspace, a handful of commands,
// compare. Real use is a LONG SEQUENCE against an ACCUMULATING workspace, and
// that is where the bugs this port actually shipped were hiding — a stray file
// that only matters once something links to it, an edit whose damage only shows
// on the next read, a lock that only matters on the second write.
//
// This mode generates seeded random verb sequences over the real schema, runs
// each step through BOTH binaries against identical workspaces, and compares
// stdout, stderr, exit code AND the full file tree AFTER EVERY STEP. It stops
// at the first divergence and prints the minimal reproducing prefix, which is
// what a new golden case gets built from.

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type schemaView struct {
	Types []struct {
		Name   string `json:"name"`
		Layout string `json:"layout"`
		Fields []struct {
			Name     string   `json:"name"`
			Type     string   `json:"type"`
			Required bool     `json:"required"`
			Enum     []string `json:"enum"`
			Pattern  *string  `json:"pattern"`
		} `json:"fields"`
		Relations []struct {
			Predicate string   `json:"predicate"`
			To        []string `json:"to"`
			Many      bool     `json:"many"`
		} `json:"relations"`
	} `json:"types"`
}

// adversarial values are the shapes that actually broke this port: CRLF,
// comments, control characters past the fold column, casefold-sensitive
// slugs, YAML-ambiguous scalars, and quoting hazards.
var adversarial = []string{
	"plain value",
	"café — ß straße",
	"İstanbul",
	"yes",
	"017",
	"2026-01-15",
	"a: colon and # hash",
	"'quoted'",
	`say "hi"`,
	"trailing space   ",
	strings.Repeat("word ", 30),
	"line1\nline2",
	"tab\there",
	"—em—dash—",
	"",
}

type fuzzState struct {
	schema   schemaView
	created  map[string][]string // type -> slugs this run created
	rng      *rand.Rand
	queue    [][]string // remaining steps of the workflow in flight
	flowName string
	flowN    int
	// Which arcs actually ran. An arc whose placeholders never bind is dropped
	// silently by nextWorkflowStep, which is how `collection-churn` sat in the
	// firm-ops list — referencing a type that preset does not have — and
	// reported nothing for entire campaigns. Counting both makes a dead arc
	// visible in the summary instead of looking like coverage.
	fired   map[string]int
	skipped map[string]int
}

func (s *fuzzState) pick(ss []string) string { return ss[s.rng.Intn(len(ss))] }

// knownSlug returns a qualified id the workspace plausibly has.
func (s *fuzzState) knownSlug() (string, bool) {
	var types []string
	for t, sl := range s.created {
		if len(sl) > 0 {
			types = append(types, t)
		}
	}
	if len(types) == 0 {
		return "", false
	}
	sort.Strings(types)
	t := s.pick(types)
	return t + "/" + s.pick(s.created[t]), true
}

func (s *fuzzState) fieldValue(f struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Required bool     `json:"required"`
	Enum     []string `json:"enum"`
	Pattern  *string  `json:"pattern"`
}) string {
	if len(f.Enum) > 0 {
		return s.pick(f.Enum)
	}
	if f.Pattern != nil {
		return "org/name" // most shipped patterns accept this; a miss tests the error path
	}
	switch f.Type {
	case "date":
		return s.pick([]string{"2026-01-15", "2020-06-01", "not-a-date"})
	case "bool":
		return s.pick([]string{"true", "false", "yes", "nonsense"})
	case "number":
		return s.pick([]string{"0.8", "42", "1e5", "abc"})
	case "list":
		return "alpha, beta"
	default:
		return s.pick(adversarial)
	}
}

// nextStep produces one plausible command. Invalid ones are deliberately
// reachable: error parity is contract too.
func (s *fuzzState) nextStep() []string {
	verbs := []string{
		"add", "add", "add", "edit", "edit", "link", "unlink", "remove",
		"get", "query", "query", "search", "neighbors", "impact", "history",
		"validate", "check", "status", "stale", "reindex", "backfill",
	}
	switch v := s.pick(verbs); v {
	case "add":
		t := s.schema.Types[s.rng.Intn(len(s.schema.Types))]
		if t.Layout == "singleton" {
			return []string{"get", t.Name + "/" + t.Name, "--format", "json"}
		}
		argv := []string{"add", t.Name, "--format", "json"}
		for _, f := range t.Fields {
			switch f.Name {
			case "type", "created", "updated", "draft":
				continue
			}
			if !f.Required && s.rng.Intn(3) != 0 {
				continue
			}
			argv = append(argv, "--"+strings.ReplaceAll(f.Name, "_", "-"), s.fieldValue(f))
		}
		if s.rng.Intn(4) == 0 {
			argv = append(argv, "--draft")
		}
		if s.rng.Intn(5) == 0 {
			argv = append(argv, "--body", s.pick(adversarial))
		}
		return argv
	case "edit":
		id, ok := s.knownSlug()
		if !ok {
			return []string{"status", "--format", "json"}
		}
		return []string{"edit", id, "description", s.pick(adversarial), "--format", "json"}
	case "link", "unlink":
		a, ok1 := s.knownSlug()
		b, ok2 := s.knownSlug()
		if !ok1 || !ok2 {
			return []string{"check", "--format", "json"}
		}
		return []string{v, a, "related", b, "--format", "json"}
	case "remove":
		id, ok := s.knownSlug()
		if !ok {
			return []string{"validate", "--format", "json"}
		}
		argv := []string{"remove", id, "--format", "json"}
		if s.rng.Intn(2) == 0 {
			argv = append(argv, "--force")
		}
		return argv
	case "get", "neighbors", "impact", "history":
		id, ok := s.knownSlug()
		if !ok {
			return []string{"status", "--format", "json"}
		}
		return []string{v, id, "--format", "json"}
	case "query":
		argv := []string{"query", "--format", "json"}
		switch s.rng.Intn(4) {
		case 0:
			argv = append(argv, "--type", s.schema.Types[s.rng.Intn(len(s.schema.Types))].Name)
		case 1:
			argv = append(argv, "--draft")
		case 2:
			argv = append(argv, "--orphan")
		}
		return argv
	case "search":
		return []string{"search", s.pick([]string{"modernization", "café", "\"a b\"", "prefix*", "((("}), "--format", "json"}
	case "reindex", "backfill":
		argv := []string{v}
		if s.rng.Intn(2) == 0 {
			argv = append(argv, "--dry-run")
		}
		return argv
	default:
		return []string{v, "--format", "json"}
	}
}

// recordCreated tracks slugs so later steps can reference real entities.
func (s *fuzzState) recordCreated(argv []string, stdout string) {
	if len(argv) == 0 || argv[0] != "add" {
		return
	}
	var rec struct {
		Type string `json:"type"`
		Slug string `json:"slug"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(stdout)), &rec) != nil || rec.Slug == "" {
		return
	}
	s.created[rec.Type] = append(s.created[rec.Type], rec.Slug)
}

func runDifferential(binA, binB, seedWS string, seeds, length int) int {
	raw, err := exec.Command(strings.Fields(binA)[0],
		append(strings.Fields(binA)[1:], "-C", seedWS, "schema", "--format", "json")...).Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot read schema from %s: %v\n", seedWS, err)
		return 2
	}
	var sv schemaView
	if err := json.Unmarshal(raw, &sv); err != nil {
		fmt.Fprintf(os.Stderr, "schema parse: %v\n", err)
		return 2
	}

	divergences, ulps := 0, 0
	totalFired, totalSkipped := map[string]int{}, map[string]int{}
	for seed := 0; seed < seeds; seed++ {
		work, err := os.MkdirTemp("", "fuzz-*")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		wsA, wsB := filepath.Join(work, "a"), filepath.Join(work, "b")
		if err := copyTree(seedWS, wsA); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.RemoveAll(work)
			return 2
		}
		if err := copyTree(seedWS, wsB); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.RemoveAll(work)
			return 2
		}
		home := filepath.Join(work, "home")
		_ = os.MkdirAll(home, 0o755)
		env := baseEnv(home, "pipe")

		st := &fuzzState{
			schema: sv, created: map[string][]string{},
			rng:     rand.New(rand.NewSource(int64(seed))),
			fired:   map[string]int{},
			skipped: map[string]int{},
		}
		flows := workflowsFor(presetOf(seedWS))
		flowCounter := seed * 1000
		var history [][]string
		normA := newPathNormalizer(wsA, home)
		normB := newPathNormalizer(wsB, home)

		for stepNo := 1; stepNo <= length; stepNo++ {
			var argv []string
			if *workflowMode {
				argv = st.nextWorkflowStep(flows, &flowCounter)
			} else {
				argv = st.nextStep()
			}
			history = append(history, argv)

			if *verboseSteps {
				label := ""
				if *workflowMode {
					label = "[" + st.flowName + "] "
				}
				fmt.Printf("  s%-2d #%-3d %skhub %s\n", seed, stepNo, label, strings.Join(argv, " "))
			}
			outA, errA, codeA, e1 := runStepWith(binA, wsA, env, step{Argv: argv}, "pipe")
			outB, errB, codeB, e2 := runStepWith(binB, wsB, env, step{Argv: argv}, "pipe")
			if e1 != nil || e2 != nil {
				fmt.Printf("seed %d step %d: exec error %v %v\n", seed, stepNo, e1, e2)
				break
			}
			outA, errA = normA.apply(outA), normA.apply(errA)
			outB, errB = normB.apply(outB), normB.apply(errB)
			st.recordCreated(argv, string(outA))

			treeA := treeManifest(wsA, normA)
			treeB := treeManifest(wsB, normB)

			// D12: bm25 scores differ by ~1 ULP between SQLite builds. Round
			// ONLY the score field and retry; if that alone reconciles them,
			// count it and continue. Anything else about the result — hits,
			// order, snippets — still fails, so this cannot mask a real
			// search regression.
			if string(outA) != string(outB) && roundScores(string(outA)) == roundScores(string(outB)) {
				ulps++
				outB = outA
			}
			if string(outA) != string(outB) || string(errA) != string(errB) || codeA != codeB || treeA != treeB {
				divergences++
				fmt.Printf("\nDIVERGENCE seed=%d step=%d\n", seed, stepNo)
				fmt.Printf("  reproducing sequence (%d steps):\n", len(history))
				for i, h := range history {
					fmt.Printf("    %2d. khub %s\n", i+1, strings.Join(h, " "))
				}
				if codeA != codeB {
					fmt.Printf("  exit: A=%d B=%d\n", codeA, codeB)
				}
				if string(outA) != string(outB) {
					fmt.Printf("  stdout:\n%s", firstDiff(outA, outB))
				}
				if string(errA) != string(errB) {
					fmt.Printf("  stderr:\n%s", firstDiff(errA, errB))
				}
				if treeA != treeB {
					fmt.Printf("  file tree:\n%s", firstDiff([]byte(treeA), []byte(treeB)))
				}
				break
			}
		}
		os.RemoveAll(work)
		fmt.Printf("seed %d: %d steps\n", seed, len(history))
		for name, n := range st.fired {
			totalFired[name] += n
		}
		for name, n := range st.skipped {
			totalSkipped[name] += n
		}
	}

	fmt.Printf("\ndifferential: %d seeds x %d steps, %d divergences", seeds, length, divergences)
	if ulps > 0 {
		fmt.Printf(" (+%d bm25 score ULP differences allowed, D12)", ulps)
	}
	fmt.Println()
	if *workflowMode {
		// An arc that never fires is not coverage. Printing the zero is the
		// whole point: a template referencing a type the preset lacks reads as
		// "ran fine" otherwise.
		fmt.Println("arcs:")
		for _, name := range arcNamesInOrder(flowNames(presetOf(seedWS)), totalFired, totalSkipped) {
			note := ""
			if totalFired[name] == 0 {
				note = "   <- NEVER FIRED"
			}
			fmt.Printf("  %-24s fired=%-4d skipped=%-4d%s\n",
				name, totalFired[name], totalSkipped[name], note)
		}
	}
	if divergences > 0 {
		return 1
	}
	return 0
}

// presetOf reads the workspace's configured preset so the right workflow set
// is chosen.
func presetOf(ws string) string {
	raw, err := os.ReadFile(filepath.Join(ws, ".khub", "config.yaml"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "preset:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "preset:"))
		}
	}
	return ""
}

var scoreRe = regexp.MustCompile(`"score": (-?[0-9.]+(?:e-?[0-9]+)?)`)

// roundScores truncates bm25 scores to 12 significant digits so a last-bit
// difference between SQLite builds compares equal. Nothing else is touched.
func roundScores(s string) string {
	return scoreRe.ReplaceAllStringFunc(s, func(m string) string {
		var f float64
		if _, err := fmt.Sscanf(m, `"score": %g`, &f); err != nil {
			return m
		}
		return fmt.Sprintf(`"score": %.12g`, f)
	})
}

func copyTree(src, dst string) error {
	return exec.Command("cp", "-R", src, dst).Run()
}
