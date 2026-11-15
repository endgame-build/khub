// Command presetdocs regenerates the derived tables in docs/<preset>-preset.md
// from what the binary resolves, the way gen_cli_reference.sh regenerates
// docs/cli-reference.md from what it prints.
//
// For every preset under presets/ it seeds a scratch workspace with
// `khub init <preset>`, reads `khub schema --format json` and
// `khub schema base --format json` there, and rewrites the regions between
// `<!-- BEGIN GENERATED <region> -->` and `<!-- END GENERATED <region> -->`
// markers. Prose outside the markers is the author's and is never touched.
//
//	go run ./parity/tools/presetdocs              # build khub fresh, rewrite the docs
//	go run ./parity/tools/presetdocs -check       # fail if one is stale
//	go run ./parity/tools/presetdocs -bin ./khub  # use a binary already built
//
// The presets are embedded in the binary, so a khub built before a preset edit
// still resolves the old schema and reports a stale doc as current. Without
// -bin the tool therefore builds ./cmd/khub from the checkout into a temporary
// directory and uses that fresh build; pass -bin only with a binary built from
// this checkout (CI does, right after its build step).
//
// A preset with no doc, a doc missing a required region, or a type with no
// generated section fails either way: a new preset or type cannot pass by
// omission. CI runs it after the build and then `git diff --exit-code docs/`.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/endgame-build/khub/internal/presets"
)

func main() {
	bin := flag.String("bin", "", "a khub binary built from this checkout (default: build one fresh)")
	repo := flag.String("repo", ".", "the repository root (holds presets/ and docs/)")
	check := flag.Bool("check", false, "write nothing; exit 1 when a doc is stale")
	flag.Parse()
	os.Exit(run(*bin, *repo, *check))
}

func run(bin, repo string, check bool) int {
	if bin == "" {
		built, cleanup, err := build(repo)
		if err != nil {
			fmt.Fprintln(os.Stderr, "presetdocs:", err)
			return 2
		}
		defer cleanup()
		bin = built
	}
	bin, err := filepath.Abs(bin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "presetdocs:", err)
		return 2
	}
	names := presets.Known(presets.DirSource(filepath.Join(repo, "presets")))
	if len(names) == 0 {
		fmt.Fprintln(os.Stderr, "presetdocs: no presets under", filepath.Join(repo, "presets"))
		return 2
	}
	failed := false
	for _, name := range names {
		doc := filepath.Join(repo, "docs", name+"-preset.md")
		problems, stale, err := one(bin, name, doc, check)
		if err != nil {
			fmt.Fprintf(os.Stderr, "presetdocs: %s: %v\n", name, err)
			return 2
		}
		for _, p := range problems {
			fmt.Fprintf(os.Stderr, "%s: %s\n", filepath.ToSlash(doc), p)
		}
		switch {
		case len(problems) > 0:
			failed = true
		case stale && check:
			fmt.Fprintf(os.Stderr, "%s: stale; run go run ./parity/tools/presetdocs\n", filepath.ToSlash(doc))
			failed = true
		case stale:
			fmt.Printf("wrote %s\n", filepath.ToSlash(doc))
		default:
			fmt.Printf("%s is current\n", filepath.ToSlash(doc))
		}
	}
	if failed {
		return 1
	}
	return 0
}

// build compiles ./cmd/khub from the checkout at repo into a temporary
// directory, so the schemas read are the presets as they are on disk now.
func build(repo string) (string, func(), error) {
	dir, err := os.MkdirTemp("", "presetdocs-bin-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	out := filepath.Join(dir, "khub")
	if runtime.GOOS == "windows" {
		out += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", out, "./cmd/khub")
	cmd.Dir = repo
	if msg, err := cmd.CombinedOutput(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("go build ./cmd/khub: %w\n%s", err, msg)
	}
	return out, cleanup, nil
}

// one regenerates one preset's doc. It reports the doc's problems, whether its
// generated regions moved, and an error when khub itself could not be read.
func one(bin, name, doc string, check bool) ([]string, bool, error) {
	raw, err := os.ReadFile(doc)
	if os.IsNotExist(err) {
		return []string{fmt.Sprintf("missing: every preset needs a reference doc with generated regions (preset %s)", name)}, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	s, base, err := resolve(bin, name)
	if err != nil {
		return nil, false, err
	}
	if s.Provenance.Preset != name {
		return nil, false, fmt.Errorf("khub resolved preset %q, want %q", s.Provenance.Preset, name)
	}
	if problems := validate(s); len(problems) > 0 {
		return problems, false, nil
	}
	// A Windows checkout may carry CRLF; compare and write LF, as git stores it.
	current := strings.ReplaceAll(string(raw), "\r\n", "\n")
	next, problems := splice(current, s, base)
	if len(problems) > 0 {
		return problems, false, nil
	}
	if next == current {
		return nil, false, nil
	}
	if !check {
		if err := os.WriteFile(doc, []byte(next), 0o644); err != nil {
			return nil, false, err
		}
	}
	return nil, true, nil
}

func resolve(bin, name string) (*schemaDoc, *baseDoc, error) {
	tmp, err := os.MkdirTemp("", "presetdocs-")
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	ws := filepath.Join(tmp, "ws")
	if _, err := khub(bin, tmp, "init", name, ws); err != nil {
		return nil, nil, err
	}
	out, err := khub(bin, ws, "schema", "--format", "json")
	if err != nil {
		return nil, nil, err
	}
	var s schemaDoc
	if err := decode(out, &s); err != nil {
		return nil, nil, fmt.Errorf("khub schema: %w", err)
	}
	out, err = khub(bin, ws, "schema", "base", "--format", "json")
	if err != nil {
		return nil, nil, err
	}
	var base baseDoc
	if err := decode(out, &base); err != nil {
		return nil, nil, fmt.Errorf("khub schema base: %w", err)
	}
	return &s, &base, nil
}

func khub(bin, dir string, args ...string) ([]byte, error) {
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "TTY_COMPATIBLE=0", "NO_COLOR=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("khub %s: %w\n%s%s", strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
	return stdout.Bytes(), nil
}
