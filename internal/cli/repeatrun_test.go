package cli

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// asCLI makes the test binary run as khub when set: the repeat-run test
// re-executes itself, so every run is a fresh process with its own map seeds,
// and the release binary gains no test seam.
const asCLI = "KHUB_TEST_RUN_AS_CLI"

// TestMain holds the package's only TestMain slot: a later one in internal/cli
// must merge with this one, keeping the asCLI branch first.
func TestMain(m *testing.M) {
	if os.Getenv(asCLI) == "1" {
		os.Exit(Execute(os.Args[1:]))
	}
	os.Exit(m.Run())
}

// repeatRuns is how many separate processes run each read call. Go seeds map
// iteration per map, but a map of up to eight keys often keeps its insertion
// order: a two-key map ranged in output stays identical across five runs about
// half the time. So detection is probabilistic, and every tie the workspace
// builds holds nine entries or more, past one map group, where a ranged map
// has drifted in every measured case.
const repeatRuns = 5

// readCalls is every read verb, as the calls the test makes. writeVerbs are
// the commands it does not repeat. TestEveryCommandIsClassified holds the two
// lists to the command tree, so a new command needs a decision here.
var readCalls = map[string][][]string{
	"status": {{"status"}},
	"get": {
		{"get", "cmp-c04", "cmp-c06", "ad-2026-01-15-a2"},
		{"get", "cmp-c00", "--edges"},
		{"get", "cmp-c08", "--edges", "--format", "table"},
		{"get", "cmp-c04", "--format", "raw"},
		{"get", "api-api-one", "--edges"},
	},
	"query": {
		{"query"},
		{"query", "--format", "ids"},
		{"query", "--type", "component"},
		{"query", "--orphan"},
		{"query", "--stale"},
		{"query", "--draft"},
		{"query", "--active", "--has", "depends_on"},
		{"query", "--missing", "repo"},
		{"query", "--tag", "core"},
		{"query", "--type", "component", "--limit", "5"},
	},
	"search": {
		{"search", "ledger"},
		{"search", "ledger OR shard", "--limit", "5"},
		{"search", "shard", "--plain", "--format", "ids"},
		{"search", "ledger", "--type", "component"},
	},
	"neighbors": {
		{"neighbors", "cmp-c00"},
		{"neighbors", "cmp-c00", "--in", "--depth", "3"},
		{"neighbors", "cmp-c06", "--out"},
		{"neighbors", "cmp-c04", "--predicate", "consumes"},
	},
	"impact": {
		{"impact", "cmp-c00", "--reverse"},
		{"impact", "cmp-c07"},
		{"impact", "cmp-c09"},
		{"impact", "cmp-c08", "--reverse"},
		{"impact", "ad-2026-01-15-a1", "--predicate", "supersedes", "--reverse"},
	},
	"history": {
		{"history", "ad-2026-01-15-a4"},
		{"history", "ad-2026-01-15-a4", "--limit", "2"},
		{"history", "ad-2026-01-15-a3", "--predicate", "supersedes"},
	},
	"validate": {{"validate"}, {"validate", "--strict"}, {"validate", "component/cmp-dangle"}, {"validate", "component"}},
	"check":    {{"check"}, {"check", "--strict"}},
	"stale":    {{"stale"}, {"stale", "--days", "1"}},
	"reindex":  {{"reindex", "--dry-run"}},
	// viz writes its page outside the workspace; the test compares the page.
	"viz": {{"viz", "--out", "%VIZ%"}, {"viz", "--out", "%VIZ%", "--type", "component"}},
	"backfill": {
		{"backfill", "--dry-run"},
		{"backfill", "--type", "component", "--dry-run"},
		{"backfill", "--type", "requirement", "--dry-run"},
	},
	"wire": {{"wire", "--dry-run", "--target", "both"}},
	"upgrade": {
		{"upgrade", "--dry-run"},
		{"upgrade", "--dry-run", "--no-schema"},
		{"upgrade", "--dry-run", "--no-wire", "--format", "json"},
	},
	"schema":       {{"schema"}, {"schema", "--format", "json"}},
	"schema types": {{"schema", "types"}, {"schema", "types", "--format", "json"}},
	"schema show": {
		{"schema", "show", "component"}, {"schema", "show", "adr"},
		{"schema", "show", "component", "--format", "json"},
	},
	"schema base":  {{"schema", "base"}, {"schema", "base", "--format", "json"}},
	"schema edges": {{"schema", "edges"}, {"schema", "edges", "--format", "json"}},
	"schema diff":  {{"schema", "diff"}, {"schema", "diff", "--format", "json"}},
}

// metaCalls are help and version, which every command answers apart from the
// workspace.
var metaCalls = [][]string{
	{"--version"}, {"--help"}, {"query", "--help"}, {"schema", "--help"},
	{"schema", "diff", "--help"}, {"neighbors", "--help"},
}

// refusalCalls must exit 2. The ones that list candidates (known types,
// declared predicates, an ambiguous slug's nine matches) print a set, which is
// where unsorted output would show.
var refusalCalls = [][]string{
	{"get", "nope"},
	{"get", "component/nope"},
	{"get", "twin"},
	{"query", "--type", "bogus"},
	{"neighbors", "cmp-c00", "--predicate", "bogus"},
	{"impact", "cmp-c00", "--predicate", "bogus"},
	{"history", "ad-2026-01-15-a4", "--predicate", "bogus"},
	{"backfill", "--type", "bogus", "--dry-run"},
	{"schema", "show", "bogus"},
	{"search", "AND ("},
	{"stale", "--days", "x"},
}

// writeVerbs change the workspace, or serve it, so repeating them proves
// nothing about ordering.
var writeVerbs = map[string]bool{
	"init": true, "add": true, "edit": true, "link": true, "unlink": true,
	"remove": true, "serve": true, "schema snapshot": true,
}

func TestEveryCommandIsClassified(t *testing.T) {
	var walk func(prefix string, cmd *cobra.Command)
	seen := map[string]bool{}
	walk = func(prefix string, cmd *cobra.Command) {
		for _, sub := range cmd.Commands() {
			if sub.Hidden || sub.Name() == "" {
				continue
			}
			name := strings.TrimSpace(prefix + " " + sub.Name())
			seen[name] = true
			_, read := readCalls[name]
			if !read && !writeVerbs[name] {
				t.Errorf("command %q is neither a read call nor a write verb in repeatrun_test.go", name)
			}
			walk(name, sub)
		}
	}
	walk("", newRoot())
	for name := range readCalls {
		if !seen[name] {
			t.Errorf("read call %q names no command", name)
		}
	}
	for name := range writeVerbs {
		if !seen[name] {
			t.Errorf("write verb %q names no command", name)
		}
	}
}

type runResult struct {
	stdout, stderr string
	code           int
}

func (r runResult) String() string {
	return fmt.Sprintf("exit %d\n--- stdout\n%s--- stderr\n%s", r.code, r.stdout, r.stderr)
}

// cliEnv pins every input the parity runner pins: the clock, the width, git's
// identity and config, and the output mode.
func cliEnv(human bool) []string {
	env := []string{asCLI + "=1",
		"KHUB_PARITY_NOW=2026-01-15", "COLUMNS=80", "NO_COLOR=1",
		"GIT_AUTHOR_NAME=Repeat", "GIT_AUTHOR_EMAIL=repeat@example.com",
		"GIT_COMMITTER_NAME=Repeat", "GIT_COMMITTER_EMAIL=repeat@example.com",
		"GIT_AUTHOR_DATE=2025-06-01T10:00:00Z", "GIT_COMMITTER_DATE=2025-06-01T10:00:00Z",
		"GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
	}
	if human {
		env = append(env, "TTY_COMPATIBLE=1")
	} else {
		env = append(env, "TTY_COMPATIBLE=0")
	}
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		switch strings.ToUpper(key) {
		case "PATH", "SYSTEMROOT", "TEMP", "TMP", "TMPDIR", "HOME", "USERPROFILE":
			env = append(env, kv)
		}
	}
	return env
}

func runCLI(t *testing.T, self, dir string, human bool, args ...string) runResult {
	t.Helper()
	cmd := exec.Command(self, args...)
	cmd.Dir = dir
	cmd.Env = cliEnv(human)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("khub %v: %v", args, err)
		}
		code = exitErr.ExitCode()
	}
	return runResult{stdout.String(), stderr.String(), code}
}

// repeatWorkspace builds a graph where ordering has room to vary: ties at
// every depth, a diamond, a three-node cycle, a dangling edge, orphans,
// drafts, stale entities with tied dates, a supersession chain, derived
// inverses, and several types sharing predicates. Each tie a call depends on
// holds nine entries or more (see repeatRuns): twelve components depend on
// cmp-c00, eleven consume api-api-one, nine ADRs affect cmp-c00, eleven
// components are stale on one date and lack a required kind, one slug lives
// in nine types, and the schema diff adds nine attributes.
func repeatWorkspace(t *testing.T, self string) string {
	t.Helper()
	ws := filepath.Join(t.TempDir(), "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	steps := [][]string{
		{"init", "build-hub", ".", "--no-wire"},
		{"add", "system", "--title", "Core"},
		{"add", "system", "--title", "Edge"},
		{"add", "repo", "--title", "Mono", "--repo", "acme/mono", "--status", "active"},
		{"add", "capability", "--title", "Billing"},
		{"add", "capability", "--title", "Search"},
		{"add", "actor", "--title", "Admin"},
		{"add", "actor", "--title", "Lonely"},
	}
	for i := range 20 {
		kind := "service"
		if i%3 == 1 {
			kind = "library"
		}
		system := "sys-core"
		if i%2 == 1 {
			system = "sys-edge"
		}
		add := []string{"add", "component", "--title", fmt.Sprintf("C%02d", i), "--kind", kind,
			"--system", system, "--tags", "core,shared",
			"--body", fmt.Sprintf("Component %d keeps the ledger and the shard map.", i)}
		if i < 8 {
			add = append(add, "--repo", "rp-mono")
		}
		steps = append(steps, add)
	}
	steps = append(steps,
		[]string{"add", "api", "--title", "Api One", "--kind", "rest", "--status", "active", "--provider", "cmp-c00"},
		[]string{"add", "api", "--title", "Api Two", "--kind", "grpc", "--status", "active", "--provider", "cmp-c01"},
		[]string{"add", "use-case", "--title", "Pay", "--trigger", "human", "--actor", "act-admin",
			"--capability", "cap-billing", "--served_by", "cmp-c04,cmp-c05"},
		[]string{"add", "use-case", "--title", "Find", "--trigger", "scheduled",
			"--capability", "cap-search", "--served_by", "cmp-c06,cmp-c05"},
		[]string{"add", "requirement", "--title", "R1", "--kind", "functional",
			"--realized_in", "cmp-c06,cmp-c04,cmp-c05", "--capabilities", "cap-billing,cap-search",
			"--use_cases", "uc-pay,uc-find", "--body", "The ledger must balance."},
		[]string{"add", "requirement", "--title", "R2", "--kind", "constraint", "--draft"},
		[]string{"add", "requirement", "--title", "R3", "--kind", "business-rule", "--body", "A shard stays small."},
		[]string{"add", "adr", "--title", "A1", "--status", "accepted", "--affects", "cmp-c00,cmp-c01"},
		[]string{"add", "adr", "--title", "A2", "--status", "accepted", "--supersedes", "ad-2026-01-15-a1",
			"--affects", "cmp-c01,cmp-c00"},
		[]string{"add", "adr", "--title", "A3", "--status", "accepted", "--supersedes", "ad-2026-01-15-a2",
			"--affects", "cmp-c08"},
		[]string{"add", "adr", "--title", "A4", "--status", "proposed", "--supersedes", "ad-2026-01-15-a3"},
	)
	for i := 1; i <= 7; i++ {
		steps = append(steps, []string{"add", "adr", "--title", fmt.Sprintf("B%d", i),
			"--status", "accepted", "--affects", "cmp-c00"})
	}
	// depends_on: fan-in ties on c00 (c01–c03, c11, c12–c19), a diamond
	// through c04/c05, a chain to c07, the cycle c08 → c09 → c10 → c08, and
	// c11 bridging both halves.
	edges := [][2]string{
		{"c01", "c00"}, {"c02", "c00"}, {"c03", "c00"},
		{"c04", "c01"}, {"c04", "c02"}, {"c05", "c03"}, {"c05", "c02"},
		{"c06", "c05"}, {"c06", "c04"}, {"c07", "c06"},
		{"c08", "c09"}, {"c09", "c10"}, {"c10", "c08"},
		{"c11", "c08"}, {"c11", "c00"},
	}
	consumes := [][2]string{
		{"c04", "api-api-one"}, {"c05", "api-api-one"}, {"c06", "api-api-two"},
		{"c07", "api-api-two"}, {"c07", "api-api-one"},
	}
	for i := 12; i < 20; i++ {
		c := fmt.Sprintf("c%02d", i)
		edges = append(edges, [2]string{c, "c00"})
		consumes = append(consumes, [2]string{c, "api-api-one"})
	}
	for _, edge := range edges {
		steps = append(steps, []string{"link", "cmp-" + edge[0], "depends_on", "cmp-" + edge[1]})
	}
	for _, edge := range consumes {
		steps = append(steps, []string{"link", "cmp-" + edge[0], "consumes", edge[1]})
	}
	steps = append(steps,
		[]string{"link", "cmp-c03", "related", "cmp-c09"},
		[]string{"link", "cmp-c09", "related", "cmp-c03"},
	)
	steps = append(steps, []string{"schema", "snapshot"})
	for _, step := range steps {
		if r := runCLI(t, self, ws, false, step...); r.code != 0 {
			t.Fatalf("setup khub %v:\n%s", step, r)
		}
	}
	// A schema change after the snapshot, so `schema diff` has rows to order.
	ontology := filepath.Join(ws, ".khub", "ontology.yaml")
	text, err := os.ReadFile(ontology)
	if err != nil {
		t.Fatal(err)
	}
	const tier = "        tier:\n          enum:\n          - tier-1\n"
	if !bytes.Contains(text, []byte(tier)) {
		t.Fatal("the build-hub ontology no longer declares component.tier as expected")
	}
	added := ""
	for _, name := range []string{"cost", "region", "zone", "sla", "pager", "budget", "vendor", "license", "runtime"} {
		added += "        " + name + ":\n          type: text\n"
	}
	text = bytes.Replace(text, []byte(tier), []byte(added+"        tier:\n          enum:\n          - tier-0\n"), 1)
	if err := os.WriteFile(ontology, text, 0o644); err != nil {
		t.Fatal(err)
	}
	// Written by hand: an edge no verb would write, eleven components stale on
	// the same day and missing their required kind (cmp-old has it), and the
	// slug "twin" in every file type, each missing what its type requires
	// beyond a title.
	hand := map[string]string{
		"knowledge/components/cmp-dangle.md": "---\ntype: component\ncreated: 2024-01-01\nupdated: 2024-01-01\n" +
			"draft: false\ntitle: Dangle\nkind: external\ndepends_on:\n- cmp-ghost\n- cmp-c00\n---\nThe ledger edge.\n",
		"knowledge/components/cmp-old.md": "---\ntype: component\ncreated: 2024-01-01\nupdated: 2024-01-01\n" +
			"draft: false\ntitle: Old\nkind: library\n---\n",
		"knowledge/components/cmp-older.md": "---\ntype: component\ncreated: 2024-01-01\n" +
			"draft: false\ntitle: Older\nkind: library\n---\n",
	}
	for i := 1; i <= 9; i++ {
		hand[fmt.Sprintf("knowledge/components/cmp-stale-%d.md", i)] = "---\ntype: component\n" +
			"created: 2024-01-01\nupdated: 2024-01-01\ndraft: false\n" + fmt.Sprintf("title: Stale %d\n", i) + "---\n"
	}
	for _, typ := range [][2]string{
		{"capability", "capabilities"}, {"actor", "actors"}, {"use-case", "use-cases"},
		{"requirement", "requirements"}, {"adr", "decisions"}, {"system", "systems"},
		{"component", "components"}, {"api", "apis"}, {"repo", "repos"},
	} {
		hand["knowledge/"+typ[1]+"/twin.md"] = "---\ntype: " + typ[0] + "\ncreated: 2024-01-01\n" +
			"draft: false\ntitle: Twin\n---\n"
	}
	for name, text := range hand {
		if err := os.WriteFile(filepath.Join(ws, filepath.FromSlash(name)), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A git history, so stale and backfill read commit dates.
	for _, args := range [][]string{
		{"init", "-q", "."}, {"add", "-A"}, {"commit", "-qm", "seed"},
	} {
		git := exec.Command("git", args...)
		git.Dir = ws
		git.Env = cliEnv(false)
		if out, err := git.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return ws
}

// treeDigest hashes every file under root except git's own store, so the test
// can prove the read calls wrote nothing.
func treeDigest(t *testing.T, root string) map[string][32]byte {
	t.Helper()
	files := map[string][32]byte{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() {
			if rel == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = sha256.Sum256(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// TestReadVerbsRepeatByteIdentical runs every read call in separate processes,
// in both output modes, and holds stdout, stderr and the exit code to the
// first run's bytes. Determinism is contract (.claude/rules/go.md): output
// that ranges a map or a set can differ between runs, and over ties of nine
// or more it has in every measured case (see repeatRuns).
func TestReadVerbsRepeatByteIdentical(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns several hundred processes; skipped under -short")
	}
	if raceEnabled() {
		t.Skip("each child process is race-instrumented, about 40x slower; plain go test runs it")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on the PATH")
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ws := repeatWorkspace(t, self)
	before := treeDigest(t, ws)

	names := make([]string, 0, len(readCalls))
	for name := range readCalls {
		names = append(names, name)
	}
	sort.Strings(names)
	// The calls only read, so they run in parallel; the group returns once
	// every one has, and the tree digest below proves none of them wrote.
	t.Run("calls", func(t *testing.T) {
		for _, name := range names {
			for _, call := range readCalls[name] {
				for _, human := range []bool{false, true} {
					label := strings.Join(call, " ")
					if human {
						label += " (human)"
					}
					t.Run(label, func(t *testing.T) {
						t.Parallel()
						repeatCall(t, self, ws, name == "viz", false, human, call)
					})
				}
			}
		}
		for _, group := range []struct {
			calls   [][]string
			refused bool
		}{{metaCalls, false}, {refusalCalls, true}} {
			for _, call := range group.calls {
				for _, human := range []bool{false, true} {
					label := strings.Join(call, " ")
					if human {
						label += " (human)"
					}
					t.Run(label, func(t *testing.T) {
						t.Parallel()
						repeatCall(t, self, ws, false, group.refused, human, call)
					})
				}
			}
		}
	})

	after := treeDigest(t, ws)
	for path, sum := range after {
		if prev, ok := before[path]; !ok || prev != sum {
			t.Errorf("a read call wrote %s", path)
		}
	}
	for path := range before {
		if _, ok := after[path]; !ok {
			t.Errorf("a read call removed %s", path)
		}
	}
}

// raceEnabled reports whether this test binary was built with -race.
func raceEnabled() bool {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return false
	}
	for _, s := range info.Settings {
		if s.Key == "-race" && s.Value == "true" {
			return true
		}
	}
	return false
}

// repeatCall runs one call repeatRuns times and fails on the first run whose
// bytes differ from the first. A viz call also compares the page it wrote. A
// call that must be refused fails unless it exits 2; any other call fails if
// it does, since a refusal there means the call is wrong.
func repeatCall(t *testing.T, self, ws string, writesPage, refused, human bool, call []string) {
	vizOut := filepath.Join(t.TempDir(), "viz.html")
	args := make([]string, len(call))
	for i, a := range call {
		args[i] = strings.ReplaceAll(a, "%VIZ%", vizOut)
	}
	var first runResult
	var firstPage []byte
	for run := range repeatRuns {
		got := runCLI(t, self, ws, human, args...)
		if refused && got.code != 2 {
			t.Fatalf("expected a refusal (exit 2):\n%s", got)
		}
		if !refused && got.code == 2 {
			t.Fatalf("refused; the call is wrong:\n%s", got)
		}
		var page []byte
		if writesPage {
			var err error
			if page, err = os.ReadFile(vizOut); err != nil {
				t.Fatal(err)
			}
		}
		if run == 0 {
			first, firstPage = got, page
			continue
		}
		if got != first {
			t.Fatalf("run %d differs from run 1\n=== run 1\n%s=== run %d\n%s", run+1, first, run+1, got)
		}
		if !bytes.Equal(page, firstPage) {
			t.Fatalf("run %d wrote a different page from run 1", run+1)
		}
	}
}
