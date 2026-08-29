// Library-level assertions ported from tests/test_wire.py (the core.wire half;
// the CLI rows are covered by parity fixtures), plus byte-level golden checks
// against parity/cases/init-wire-skills/wire-targets — the recorded block
// bytes and dry-run preview ARE the spec.
package wire

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/workspace"
)

// freshWS is the conftest fixture: a scaffolded workspace with an empty entity
// tree. firm-ops mints no singletons, so the tree really is empty.
func freshWS(t *testing.T, preset string) string {
	t.Helper()
	t.Setenv("KHUB_PARITY_NOW", "2026-01-15")
	ws := filepath.Join(t.TempDir(), "ws")
	if err := os.MkdirAll(ws, 0o777); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Init(preset, ws, workspace.InitOptions{}); err != nil {
		t.Fatal(err)
	}
	return ws
}

func mustWire(t *testing.T, root string, opt Options) *Result {
	t.Helper()
	res, err := Wire(root, opt)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func read(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func actions(res *Result) map[string]string {
	out := map[string]string{}
	for _, o := range res.Outcomes {
		out[filepath.Base(o.Path)] = o.Action
	}
	return out
}

func actionList(res *Result) []string {
	out := make([]string, len(res.Outcomes))
	for i, o := range res.Outcomes {
		out[i] = o.Action
	}
	return out
}

// --- targeting -------------------------------------------------------------------

func TestWireCreatesClaudeMD(t *testing.T) {
	ws := freshWS(t, "firm-ops")
	res := mustWire(t, ws, Options{Claude: true})
	text := read(t, filepath.Join(ws, "CLAUDE.md"))
	for _, want := range []string{
		Begin, End,
		"@.khub/schema.yaml", // the Claude import — reason without the CLI
		"firm-ops",           // the active preset
		"`client`",           // a declared type, read from the live schema
	} {
		if !strings.Contains(text, want) {
			t.Errorf("CLAUDE.md lacks %q", want)
		}
	}
	if !reflect.DeepEqual(actionList(res), []string{"created"}) {
		t.Errorf("actions = %v", actionList(res))
	}
	if exists(filepath.Join(ws, "AGENTS.md")) {
		t.Error("Claude:true wrote AGENTS.md")
	}
}

func TestWireAgentsPointerNotImport(t *testing.T) {
	ws := freshWS(t, "firm-ops")
	res := mustWire(t, ws, Options{Agents: true})
	text := read(t, filepath.Join(ws, "AGENTS.md"))
	if !strings.Contains(text, Begin) || !strings.Contains(text, End) {
		t.Error("AGENTS.md has no managed block")
	}
	// AGENTS.md has no import directive — but it points at the schema file.
	if strings.Contains(text, "@.khub/schema.yaml") {
		t.Error("AGENTS.md carries the Claude import")
	}
	if !strings.Contains(text, ".khub/schema.yaml") || !strings.Contains(text, "`client`") {
		t.Error("AGENTS.md lacks the schema pointer or the types")
	}
	if !reflect.DeepEqual(actionList(res), []string{"created"}) {
		t.Errorf("actions = %v", actionList(res))
	}
	if exists(filepath.Join(ws, "CLAUDE.md")) {
		t.Error("Agents:true wrote CLAUDE.md")
	}
}

func TestWireBareCreatesTheFileThatIsMissing(t *testing.T) {
	// A repo carrying only one context file was wired only for the agents that
	// read that one; the other stayed blind to a workspace sitting right there.
	ws := freshWS(t, "firm-ops")
	mustWire(t, ws, Options{Claude: true}) // seed CLAUDE.md
	res := mustWire(t, ws, Options{})      // bare: updates CLAUDE.md, creates AGENTS.md
	want := map[string]string{"CLAUDE.md": "unchanged", "AGENTS.md": "created"}
	if !reflect.DeepEqual(actions(res), want) {
		t.Errorf("actions = %v", actions(res))
	}
	if !exists(filepath.Join(ws, "AGENTS.md")) {
		t.Error("AGENTS.md was not created")
	}
}

func TestWireBareSeedsBothWhenNeitherExists(t *testing.T) {
	ws := freshWS(t, "firm-ops")
	res := mustWire(t, ws, Options{})
	if !reflect.DeepEqual(actionList(res), []string{"created", "created"}) {
		t.Errorf("actions = %v", actionList(res))
	}
	if !exists(filepath.Join(ws, "CLAUDE.md")) || !exists(filepath.Join(ws, "AGENTS.md")) {
		t.Error("bare wire did not seed both files")
	}
}

func TestWireBareUpdatesBothWhenBothExist(t *testing.T) {
	ws := freshWS(t, "firm-ops")
	mustWire(t, ws, Options{Claude: true, Agents: true})
	res := mustWire(t, ws, Options{})
	names := []string{filepath.Base(res.Outcomes[0].Path), filepath.Base(res.Outcomes[1].Path)}
	if !reflect.DeepEqual(names, []string{"CLAUDE.md", "AGENTS.md"}) {
		t.Errorf("targets = %v", names)
	}
}

// --- idempotency and minimal diff ---------------------------------------------------

func TestWireIsIdempotent(t *testing.T) {
	ws := freshWS(t, "firm-ops")
	mustWire(t, ws, Options{})
	before := read(t, filepath.Join(ws, "CLAUDE.md"))
	res := mustWire(t, ws, Options{})
	if read(t, filepath.Join(ws, "CLAUDE.md")) != before {
		t.Error("a re-wire changed the file")
	}
	if !reflect.DeepEqual(actionList(res), []string{"unchanged", "unchanged"}) {
		t.Errorf("actions = %v", actionList(res))
	}
}

func TestWireReplacesBlockPreservingSurroundings(t *testing.T) {
	ws := freshWS(t, "firm-ops")
	claude := filepath.Join(ws, "CLAUDE.md")
	seed := "# Project\n\nHouse rules.\n\n" + Begin + "\nstale khub block\n" + End + "\n\nMore rules.\n"
	if err := os.WriteFile(claude, []byte(seed), 0o666); err != nil {
		t.Fatal(err)
	}
	res := mustWire(t, ws, Options{}) // CLAUDE.md exists → bare wire updates it in place
	text := read(t, claude)
	for _, want := range []string{"# Project", "House rules.", "More rules.", "@.khub/schema.yaml"} {
		if !strings.Contains(text, want) {
			t.Errorf("CLAUDE.md lost %q", want)
		}
	}
	if strings.Contains(text, "stale khub block") {
		t.Error("the stale block survived")
	}
	if strings.Count(text, Begin) != 1 || strings.Count(text, End) != 1 {
		t.Error("the markers were duplicated")
	}
	want := map[string]string{"CLAUDE.md": "updated", "AGENTS.md": "created"}
	if !reflect.DeepEqual(actions(res), want) {
		t.Errorf("actions = %v", actions(res))
	}
}

func TestWireAppendsToAMarkerlessFile(t *testing.T) {
	// No markers and non-blank content: the block is appended after one blank
	// line, and the result always ends with a newline.
	ws := freshWS(t, "firm-ops")
	claude := filepath.Join(ws, "CLAUDE.md")
	if err := os.WriteFile(claude, []byte("# Project\n\nHouse rules."), 0o666); err != nil {
		t.Fatal(err)
	}
	mustWire(t, ws, Options{Claude: true})
	text := read(t, claude)
	if !strings.HasPrefix(text, "# Project\n\nHouse rules.\n\n"+Begin) {
		t.Errorf("append shape = %q", text[:80])
	}
	if !strings.HasSuffix(text, End+"\n") {
		t.Error("no trailing newline after the block")
	}
}

func TestWireDryRunWritesNothing(t *testing.T) {
	ws := freshWS(t, "firm-ops")
	res := mustWire(t, ws, Options{Claude: true, DryRun: true})
	if exists(filepath.Join(ws, "CLAUDE.md")) {
		t.Error("a dry run wrote CLAUDE.md")
	}
	if !strings.Contains(res.Preview, Begin) || !strings.Contains(res.Preview, "@.khub/schema.yaml") {
		t.Error("the preview lacks the block")
	}
	if !reflect.DeepEqual(actionList(res), []string{"created"}) {
		t.Errorf("a dry run must still report the action it would take: %v", actionList(res))
	}
}

// --- the block itself ----------------------------------------------------------------

func TestSingletonCuesCarryTheirFileLink(t *testing.T) {
	// The block tells an agent to edit the existing document — and says which.
	// build-lite's two singletons are the case; a non-singleton is written by
	// `khub add`, so its cue names no path.
	ws := freshWS(t, "build-lite")
	mustWire(t, ws, Options{Claude: true, Agents: true})
	for _, name := range []string{"CLAUDE.md", "AGENTS.md"} {
		text := read(t, filepath.Join(ws, name))
		for _, want := range []string{
			"[knowledge/prd.md](knowledge/prd.md)",
			"[knowledge/arc42.md](knowledge/arc42.md)",
		} {
			if !strings.Contains(text, want) {
				t.Errorf("%s lacks %q", name, want)
			}
		}
		found := false
		for _, line := range strings.Split(text, "\n") {
			if strings.HasPrefix(line, "- `adr`") {
				found = true
				if strings.Contains(line, "](") {
					t.Errorf("%s: the adr cue carries a link: %s", name, line)
				}
			}
		}
		if !found {
			t.Errorf("%s has no adr cue", name)
		}
	}
}

func TestBuildBlockStampAndTypeListFallbacks(t *testing.T) {
	// stamp = "preset@version", or the bare preset without a version, or
	// "custom" without either; an empty type list reads "none declared yet".
	for _, tc := range []struct {
		preset, version, want string
	}{
		{"firm-ops", "0.1.0", "Preset: `firm-ops@0.1.0`."},
		{"firm-ops", "", "Preset: `firm-ops`."},
		{"", "", "Preset: `custom`."},
	} {
		block := BuildBlock(tc.preset, tc.version, []string{"a"}, true, nil)
		if !strings.Contains(block, tc.want) {
			t.Errorf("BuildBlock(%q,%q) lacks %q", tc.preset, tc.version, tc.want)
		}
	}
	empty := BuildBlock("p", "1", nil, true, nil)
	if !strings.Contains(empty, "Entity types: none declared yet.") {
		t.Error("an empty type list has no fallback")
	}
	// No `when` declarations means no capture-trigger section at all.
	if strings.Contains(empty, "Record as you go") {
		t.Error("the trigger section rendered without any when")
	}
	withWhen := BuildBlock("p", "1", []string{"a"}, true, []When{{Type: "a", Text: "it happens"}})
	if !strings.Contains(withWhen, "- `a` — it happens") {
		t.Error("the when line is missing")
	}
}

func TestBuildBlockMarkersAndNoTrailingNewline(t *testing.T) {
	block := BuildBlock("p", "1", []string{"a"}, false, nil)
	if !strings.HasPrefix(block, Begin) || !strings.HasSuffix(block, End) {
		t.Error("the block is not marker-delimited end to end")
	}
	if strings.Contains(block, "@.khub/schema.yaml") {
		t.Error("importSupported=false emitted the import")
	}
}

// --- golden bytes: parity/cases/init-wire-skills/wire-targets -------------------------

func TestGoldenWiredFiles(t *testing.T) {
	// The recorded manifest pins both wired files byte for byte, and step 02's
	// stdout pins the dry-run preview.
	ws := freshWS(t, "build-lite")
	mustWire(t, ws, Options{})
	want := map[string]string{
		"CLAUDE.md": "a23f3c96dc82eb67661a82acb26792dc26ac3357e8696e00f24ddae124f44521",
		"AGENTS.md": "15596cc4273c20515c0226770cdf734dee5884d7b8f255542494f4bc99211edb",
	}
	for name, digest := range want {
		sum := sha256.Sum256([]byte(read(t, filepath.Join(ws, name))))
		if hex.EncodeToString(sum[:]) != digest {
			t.Errorf("%s bytes drifted from the recorded fixture", name)
		}
	}

	res := mustWire(t, ws, Options{DryRun: true})
	fixture := filepath.Join("..", "..", "parity", "cases", "init-wire-skills",
		"wire-targets", "expected", "steps", "02.stdout")
	raw, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	// The CLI prints the preview with typer.echo, which appends the newline.
	if res.Preview+"\n" != string(raw) {
		t.Error("the dry-run preview drifted from the recorded fixture")
	}
}

func TestGoldenInitWireTailTree(t *testing.T) {
	// parity/cases/init-wire-skills/init-wire-tail: the whole scaffold plus the
	// wire tail init runs (claude AND agents, explicitly — not the bare form),
	// checked against the recorded manifest.
	t.Setenv("KHUB_PARITY_NOW", "2026-01-15")
	ws := filepath.Join(t.TempDir(), "ws")
	if err := os.MkdirAll(ws, 0o777); err != nil {
		t.Fatal(err)
	}
	res, err := workspace.Init("build-lite", ws, workspace.InitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	wired := mustWire(t, res.Path, Options{Claude: true, Agents: true})
	if !reflect.DeepEqual(actionList(wired), []string{"created", "created"}) {
		t.Errorf("wire tail actions = %v", actionList(wired))
	}
	assertManifest(t, ws, "init-wire-tail")
}

// --- --target -----------------------------------------------------------------------

func TestResolveTarget(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Options
	}{
		{"claude", Options{Claude: true}},
		{"agents", Options{Agents: true}},
		{"both", Options{Claude: true, Agents: true}},
	} {
		got, err := ResolveTarget(&tc.in)
		if err != nil {
			t.Fatalf("%s: %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("%s = %+v", tc.in, got)
		}
	}
	// The flag was not passed: neither selector, so wire updates what exists.
	if got, err := ResolveTarget(nil); err != nil || got != (Options{}) {
		t.Errorf("nil = %+v, %v", got, err)
	}
	bogus := "bogus"
	_, err := ResolveTarget(&bogus)
	l, ok := err.(*errs.Located)
	if !ok || l.Code != "bad_target" {
		t.Fatalf("err = %v", err)
	}
	if l.Message != "Unknown --target 'bogus'. Choose claude, agents, or both" {
		t.Fatalf("message = %q", l.Message)
	}
}

// assertManifest compares a wired tree against a recorded parity fixture, in
// the runner's own "sha256 exec-bit relpath" sorted format.
func assertManifest(t *testing.T, root, fixture string) {
	t.Helper()
	wantPath := filepath.Join("..", "..", "parity", "cases", "init-wire-skills",
		fixture, "expected", "tree.manifest")
	raw, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatal(err)
	}
	var rels []string
	byRel := map[string]string{}
	err = filepath.Walk(root, func(p string, info os.FileInfo, werr error) error {
		if werr != nil || info.IsDir() {
			return nil //nolint:nilerr // the runner ignores walk errors too
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, ".git/") || strings.Contains(rel, "/.git/") ||
			strings.Contains(rel, ".khub/generated/") {
			return nil
		}
		payload, _ := os.ReadFile(p)
		mode := "-"
		if info.Mode()&0o111 != 0 {
			mode = "x"
		}
		sum := sha256.Sum256(payload)
		rels = append(rels, rel)
		byRel[rel] = hex.EncodeToString(sum[:]) + " " + mode + " " + rel
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Sort by PATH, matching the runner: sorting the composed line ordered by
	// hash, so one changed byte reshuffled the whole manifest.
	sort.Strings(rels)
	lines := make([]string, len(rels))
	for i, rel := range rels {
		lines[i] = byRel[rel]
	}
	if got := strings.Join(lines, "\n") + "\n"; got != string(raw) {
		t.Errorf("tree.manifest mismatch for %s\n--- want ---\n%s\n--- got ---\n%s", fixture, raw, got)
	}
}
