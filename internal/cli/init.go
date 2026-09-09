// init.go ports cli/init_cmd.py: scaffold a workspace from a preset, then wire
// it into the agent context files and write the first index.md. Both tails
// are best-effort (the wire one skippable with --no-wire); neither unwinds a
// successful scaffold.

package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/reindex"
	"github.com/endgame-build/khub/internal/wire"
	"github.com/endgame-build/khub/internal/workspace"
)

// skillHint is printed after a scaffold and carried as `skill_hint` in the JSON
// payload, so an agent driving `init --format json` learns the follow-up
// without parsing prose.
const skillHint = "khub install-skills"

// skillHintFor aims the follow-up at the workspace that was just scaffolded.
// install-skills resolves its root by walking up from the working directory, so
// a bare hint after `khub init firm-ops ./my-hub` would either find no
// workspace or — worse — find an unrelated one above cwd.
func skillHintFor(scaffolded string) string {
	if resolvePath(scaffolded) == resolvePath(".") {
		return skillHint
	}
	return "khub -C " + scaffolded + " install-skills"
}

// resolvePath is Path.resolve(): absolute, symlinks followed when they exist.
func resolvePath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	if real, rerr := filepath.EvalSymlinks(abs); rerr == nil {
		return real
	}
	return abs
}

func registerInit(root *cobra.Command) {
	var presetSource, name, format string
	var force, noWire bool
	cmd := newCmd("init [preset] [path]",
		"Scaffold a workspace from a preset and wire it into the agent context files.",
		"[preset] [path]", func(cmd *cobra.Command, args []string) error {
			return Guard(format, func() error {
				if len(args) == 0 {
					fmt.Fprintln(os.Stderr, "Missing argument 'PRESET' (e.g. firm-ops).")
					return &ExitError{Code: 2}
				}
				target := "."
				if len(args) > 1 {
					target = args[1]
				}
				// Best-effort tails, run by the library under its own lock: a wire
				// hiccup does not unwind the scaffold, and no other writer can
				// slip in between the scaffold and the index it lists.
				var wireStep tail[wire.Result]
				var indexStep tail[string]
				result, err := workspace.Init(args[0], target, workspace.InitOptions{
					PresetSource: presetSource, Name: name, Force: force,
					Tails: func(root string) {
						if !noWire { // no selection to make without a wizard: seed both files
							wireStep = tailOf(wire.WireHeld(root, wire.Options{Claude: true, Agents: true}))
						}
						// The index is the cheapest read of the whole corpus, and a
						// workspace that has never run `reindex` simply has none — so
						// an agent's first look finds nothing. Written after the
						// singletons so it lists them.
						indexStep = indexTailHeld(root)
					},
				})
				if err != nil {
					return err
				}

				payload := initPayload(result, wireStep, indexStep)
				return Emit(payload, format, func() {
					if result.SeededOverCorpus {
						fmt.Printf("Initialized %s workspace; %d entity files modified\n",
							result.Preset, result.EntityFilesModified)
					} else {
						fmt.Printf("Initialized %s workspace at %s\n", result.Preset, result.Path)
					}
					if len(result.SingletonsCreated) > 0 {
						fmt.Println("created singletons: " + strings.Join(result.SingletonsCreated, ", "))
					}
					if len(result.Preserved) > 0 {
						// Say it out loud: a re-init leaves the workspace's own schema
						// and templates in place.
						head := result.Preserved
						more := ""
						if len(head) > 3 {
							head, more = head[:3], " …"
						}
						fmt.Printf("preserved %d workspace-owned file(s): %s%s\n",
							len(result.Preserved), strings.Join(head, ", "), more)
					}
					if wireStep.Result != nil {
						for _, outcome := range wireStep.Result.Outcomes {
							fmt.Printf("%s %s\n", outcome.Action, filepath.Base(outcome.Path))
						}
					}
					if wireStep.Err != "" {
						fmt.Fprintf(os.Stderr, "wire skipped: %s\n", wireStep.Err)
					}
					if indexStep.Err != "" {
						fmt.Fprintf(os.Stderr, "index skipped: %s\n", indexStep.Err)
					} else {
						fmt.Printf("index.md %s\n", *indexStep.Result)
					}
					fmt.Printf("\nAgent skill not installed. To install:\n  %s\n",
						skillHintFor(result.Path))
				})
			})
		})
	cmd.Args = clickArity(2)
	cmd.Long = cmd.Short + "\n\nA missing PRESET is a usage error; PATH defaults to the working directory." +
		"\nInstalling the agent skill is a separate step: ``khub install-skills``."
	cmd.Flags().StringVar(&presetSource, "preset-source", "",
		"Where to resolve the preset if not packaged with khub.")
	cmd.Flags().StringVar(&name, "name", "", "Workspace name (default: the target dir name).")
	cmd.Flags().BoolVar(&force, "force", false, "Scaffold into a non-empty target.")
	cmd.Flags().BoolVar(&noWire, "no-wire", false, "Skip wiring the schema into the agent files.")
	cmd.Flags().StringVar(&format, "format", "text",
		"text confirmation (default); json emits resolved provenance.")
	root.AddCommand(cmd)
}

// initPayload is dataclasses.asdict(result) in declaration order, then the
// tails — `wire` only when it ran (a --no-wire run carries no wire key at
// all), `index` always, each with its *_error right after it on failure —
// and the skill hint.
func initPayload(result *workspace.InitResult, wired tail[wire.Result], indexed tail[string]) *omap.Map {
	payload := omap.New()
	payload.Set("path", result.Path)
	payload.Set("preset", result.Preset)
	payload.Set("version", result.Version)
	payload.Set("name", result.Name)
	payload.Set("source", nullable(result.Source))
	payload.Set("entity_files_modified", result.EntityFilesModified)
	payload.Set("seeded_over_corpus", result.SeededOverCorpus)
	payload.Set("singletons_created", strList(result.SingletonsCreated))
	payload.Set("preserved", strList(result.Preserved))
	wired.set(payload, "wire", func(r *wire.Result) any { return wireOutcomes(r) }, false)
	indexed.set(payload, "index", func(action *string) any { return *action }, true)
	payload.Set("skill_hint", skillHintFor(result.Path))
	return payload
}

// wireOutcomes renders a wire result's per-file outcomes as the `{path,
// action}` list init and upgrade both carry under `wire`.
func wireOutcomes(result *wire.Result) []any {
	items := make([]pathAction, 0, len(result.Outcomes))
	for _, o := range result.Outcomes {
		items = append(items, pathAction{o.Path, o.Action})
	}
	return pathActionRecords(items)
}

// indexTail regenerates index.md at root after a scaffold, shared by init and
// upgrade. Never fatal — a malformed file makes reindex refuse, and the
// scaffold above stands either way — so it reports the action taken
// ("created" | "updated" | "unchanged"), or the refusal's message.
func indexTail(root string) tail[string] { return indexTailWith(root, reindex.Reindex) }

// indexTailHeld is indexTail for a caller already inside the workspace lock.
func indexTailHeld(root string) tail[string] { return indexTailWith(root, reindex.ReindexHeld) }

func indexTailWith(root string, run func(string, bool) (*reindex.Result, error)) tail[string] {
	before, rerr := os.ReadFile(filepath.Join(root, reindex.IndexName))
	if rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
		return tail[string]{Err: tailError(rerr)}
	}
	result, err := run(root, false)
	if err != nil {
		return tail[string]{Err: tailError(err)}
	}
	action := "updated"
	switch {
	case rerr != nil:
		action = "created"
	case string(before) == result.Content:
		action = "unchanged"
	}
	return tail[string]{Result: &action}
}
