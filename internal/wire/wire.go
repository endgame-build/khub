// Package wire ports src/khub/core/wire.py — `khub wire`, which links a khub
// workspace into agent context files (CLAUDE.md / AGENTS.md).
//
// It injects an idempotent, marker-delimited block that does two things:
//
//  1. Links the schema into the agent's context. CLAUDE.md gets a Claude Code
//     `@` import per existing schema layer file (loaded into context every
//     session); AGENTS.md (the cross-agent standard, which has no import
//     directive) gets a plain pointer to read the schema files. The block also
//     names the active preset and the declared types.
//  2. Documents the khub command surface, for when the CLI is available.
//
// Bare `wire` updates whichever context files already exist and creates either
// that is missing; Options.Claude/Agents target a specific file. Schema-generic:
// the block is built from the workspace at call time (the active preset from
// .khub/config.yaml and the declared types from the resolved schema), with no
// per-type code path. Re-running replaces the block in place, so the write
// stays a minimal, idempotent diff.
package wire

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/mdlink"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/workspace"
)

// Block markers. Everything between them is khub's to rewrite; everything
// outside is the file owner's.
const (
	Begin = "<!-- khub:begin -->"
	End   = "<!-- khub:end -->"
)

// Outcome is what happened to one wired file. Path is the joined path (the
// CLI prints its base name in prose and the whole string in JSON); Action is
// "created" | "updated" | "unchanged".
type Outcome struct {
	Path   string
	Action string
}

// Result is a preview of the target blocks and the per-file outcomes.
type Result struct {
	Preview  string
	Outcomes []Outcome
}

// When is one type's schema-declared capture trigger, in declaration order.
// EditPath carries a singleton's storage path so the rendered cue can link the
// one file the cue means — derived here at render time, so the authored cue
// stays pure domain language and a moved file cannot strand a stale link in
// the ontology.
type When struct {
	Type     string
	Text     string
	EditPath string // "" for non-singletons
}

// Options carries wire's keyword arguments. With neither Claude nor Agents
// set, both files are targets.
type Options struct {
	Claude bool
	Agents bool
	DryRun bool
}

// ResolveTarget is wire_cmd._resolve_target: map --target onto the two
// selectors. A nil target (the flag was not passed) selects neither, which
// updates whichever files already exist and creates the one that does not.
// Anything outside the vocabulary is the bad_target error.
func ResolveTarget(target *string) (Options, error) {
	if target == nil {
		return Options{}, nil
	}
	switch *target {
	case "claude":
		return Options{Claude: true}, nil
	case "agents":
		return Options{Agents: true}, nil
	case "both":
		return Options{Claude: true, Agents: true}, nil
	default:
		return Options{}, errs.BadTarget(*target)
	}
}

// whenToRecord renders the capture triggers from each type's schema-declared
// `when`.
//
// The block already said HOW to write. It never said WHEN, and a real-codebase
// eval showed that is where wired agents lose: most off-rails operations were
// no command at all, because a terse ask ("Note it.") read as conversation
// rather than work. These lines are per-domain and come verbatim from the
// schema, so a new type ships its own trigger and no surface code learns a
// type name.
func whenToRecord(whens []When) []string {
	if len(whens) == 0 {
		return nil
	}
	out := []string{
		"Record as you go — when one of these moments occurs, capture it:",
		"",
	}
	for _, w := range whens {
		line := "- `" + w.Type + "` — " + w.Text
		if w.EditPath != "" {
			line += " — edit [" + w.EditPath + "](" + mdlink.LinkDest(w.EditPath) + "), never add a second"
		}
		out = append(out, line)
	}
	out = append(out,
		"",
		"A stated fact about the system is a capture request, whatever the wording. "+
			`"note it", "write it down", "log it", "FYI", "heads up", "for the record" — and a `+
			"bare statement with no instruction at all — all mean record it. Do that, then say "+
			"what you recorded and its id. Answering \"Noted.\" without a record does not "+
			"complete the task, and neither does asking which file to write to: entities are "+
			"written with `khub add`, never by choosing a path.",
		"",
	)
	return out
}

// BlockSpec is everything the managed block renders from — all of it derived
// from one workspace read (Provenance plus LoadSchemaLayers), which is why it
// travels as one value rather than five parallel parameters.
type BlockSpec struct {
	// Preset and Version are the workspace's provenance stamp.
	Preset  string
	Version string
	// Types are the declared type names, in declaration order.
	Types []string
	// Whens are the capture cues, one per type that declares one.
	Whens []When
	// Layers are the workspace's EXISTING schema layer files
	// (workspace-relative, resolve order — introspect.LayerFiles).
	Layers []string
}

// BuildBlock is build_block: the managed context-file block (markers included,
// no trailing newline).
//
// Imports and links are rendered only for the layer files spec.Layers actually
// names, so a two-file workspace never advertises an import Claude Code would
// report as broken.
//
// With importSupported (CLAUDE.md), the schema is pulled in by Claude Code
// `@` imports — resolved relative to the file they sit in, loading the
// contract into context every session; each stays on its own line outside any
// code fence so the import fires. Without it (AGENTS.md and other agents,
// which have no import directive), the block points the agent at the schema
// files to read instead.
func BuildBlock(spec BlockSpec, importSupported bool) string {
	stamp := spec.Preset + "@" + spec.Version
	if spec.Version == "" {
		stamp = spec.Preset
		if spec.Preset == "" {
			stamp = "custom"
		}
	}
	typeList := "none declared yet"
	if len(spec.Types) > 0 {
		quoted := make([]string, len(spec.Types))
		for i, t := range spec.Types {
			quoted[i] = "`" + t + "`"
		}
		typeList = strings.Join(quoted, ", ")
	}
	var link []string
	if importSupported {
		link = []string{
			"The schema is imported below, so it loads into context even without " +
				"running the khub CLI:",
			"",
		}
		for _, l := range spec.Layers {
			link = append(link, "@"+l, "")
		}
	} else {
		link = []string{
			"The ontology is defined in the schema files below; read them to work in " +
				"this model, even without running the khub CLI:",
			"",
		}
	}
	fileRefs := make([]string, 0, len(spec.Layers))
	for _, l := range spec.Layers {
		ref := "[`" + l + "`](" + mdlink.LinkDest(l) + ")"
		if strings.HasSuffix(l, "/ontology.yaml") {
			ref += " (the domain model)"
		}
		fileRefs = append(fileRefs, ref)
	}

	lines := []string{
		Begin,
		"## khub workspace",
		"",
		"This repository is a [khub](https://github.com/endgame-build/khub) " +
			"workspace: its domain is modeled as typed entities and typed relations, and " +
			"the schema is the contract. Reason in that model.",
		"",
	}
	lines = append(lines, link...)
	lines = append(lines,
		"Schema files: "+strings.Join(fileRefs, ", ")+". "+
			"Preset: `"+stamp+"`. Entity types: "+typeList+".",
		"",
	)
	lines = append(lines, whenToRecord(spec.Whens)...)
	lines = append(lines,
		"Every write goes through the CLI — it is the only path that validates against "+
			"the schema and resolves relations. If you edit an entity file by hand anyway "+
			"(or a human did), run `khub validate` on it immediately: an unvalidated "+
			"hand-edit is how a workspace acquires a field no gate will ever report.",
		"",
		"Reading the graph is a khub operation too — query it, do not grep it:",
		"",
		"- Introspect: `khub schema`, `khub schema show <type>`, `khub status`.",
		"- Read: `khub query --type <t>`, `khub get <id> --edges`, "+
			"`khub neighbors <id>`, `khub impact <id>`, `khub history <id>`, "+
			"`khub search <text>`.",
		"- Write: `khub add <type> --<field> <v>`, `khub edit <id> <field> <v>`, "+
			"`khub link <id> <pred> <target>`, `khub unlink`, `khub remove <id>`. "+
			"Capture is never blocked; `--draft` marks an entity unpublished.",
		"- Every read and every write above takes `--format json`. Piped output is "+
			"JSON by default; a table is only for a TTY. (`reindex`, `viz`, `backfill` "+
			"and `wire` are operator commands and print prose.)",
		"",
		"Not installed? `npm install -D @endgame-build/khub`, "+
			"then `npx khub install-skills`.",
		End,
	)
	return strings.Join(lines, "\n")
}

// upsert returns text with block inserted, or replacing an existing block.
//
// It replaces everything between the markers (keeping surrounding content), or
// appends the block when the file has no markers. The result always ends with
// a trailing newline.
func upsert(text, block string) string {
	var merged string
	switch {
	case strings.Contains(text, Begin) && strings.Contains(text, End):
		pre := text[:strings.Index(text, Begin)]
		post := text[strings.Index(text, End)+len(End):]
		merged = pre + block + post
	case strings.TrimSpace(text) != "":
		merged = strings.TrimRight(text, "\n") + "\n\n" + block
	default:
		merged = block
	}
	if strings.HasSuffix(merged, "\n") {
		return merged
	}
	return merged + "\n"
}

// Wire wires the workspace at root into agent context files.
//
// With Claude/Agents set, those files are the targets. With neither set, both
// are — and either that is missing is created, so a repo carrying only one
// context file (or neither) ends up wired for every agent that reads it rather
// than silently for some. Writes each target unless the content is unchanged
// (or DryRun is set). Idempotent.
func Wire(root string, opt Options) (*Result, error) {
	prov, err := workspace.Provenance(root)
	if err != nil {
		return nil, err
	}
	resolved, layers, err := introspect.LoadSchemaLayers(root)
	if err != nil {
		return nil, err
	}
	types := introspect.TypesList(resolved)
	var whens []When
	for _, name := range resolved.Types.Keys() {
		rtype, _ := resolved.Types.Get(name)
		if rtype.When != nil && *rtype.When != "" {
			w := When{Type: name, Text: *rtype.When}
			if rtype.Storage.Layout == schema.LayoutSingleton && rtype.Storage.Path != nil {
				w.EditPath = *rtype.Storage.Path
			}
			whens = append(whens, w)
		}
	}
	spec := BlockSpec{
		Preset:  provString(prov, "preset"),
		Version: provString(prov, "version"),
		Types:   types,
		Whens:   whens,
		Layers:  layers,
	}

	type candidate struct {
		path  string
		block string
	}
	// filepath.Join matches pathlib's `/` for the shapes root takes here (an
	// absolute resolved workspace root, or init's normalized target — "." joins
	// away in both).
	candidates := []candidate{
		{filepath.Join(root, "CLAUDE.md"), BuildBlock(spec, true)},
		{filepath.Join(root, "AGENTS.md"), BuildBlock(spec, false)},
	}
	targets := candidates
	if opt.Claude || opt.Agents {
		chosen := map[string]bool{"CLAUDE.md": opt.Claude, "AGENTS.md": opt.Agents}
		targets = nil
		for _, c := range candidates {
			if chosen[filepath.Base(c.path)] {
				targets = append(targets, c)
			}
		}
	}

	outcomes := []Outcome{}
	previews := make([]string, 0, len(targets))
	for _, c := range targets {
		previews = append(previews, "# "+filepath.Base(c.path)+"\n"+c.block)
		raw, readErr := os.ReadFile(c.path)
		exists := readErr == nil
		if readErr != nil && !errors.Is(readErr, fs.ErrNotExist) {
			return nil, readErr
		}
		old := ""
		if exists {
			old = string(raw)
		}
		updated := upsert(old, c.block)
		action := "updated"
		switch {
		case !exists:
			action = "created"
		case updated == old:
			action = "unchanged"
		}
		if !opt.DryRun && action != "unchanged" {
			if werr := os.WriteFile(c.path, []byte(updated), 0o666); werr != nil {
				return nil, werr
			}
		}
		outcomes = append(outcomes, Outcome{Path: c.path, Action: action})
	}
	return &Result{Preview: strings.Join(previews, "\n\n"), Outcomes: outcomes}, nil
}

// provString reads one key of the two-key provenance record as the str its
// annotation promises.
func provString(prov interface{ Get(string) (any, bool) }, key string) string {
	v, ok := prov.Get(key)
	if !ok || v == nil {
		return ""
	}
	s, _ := v.(string)
	return s
}
