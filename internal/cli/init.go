// init.go ports cli/init_cmd.py: scaffold a workspace from a preset, then wire
// it into the agent context files. The wire tail is best-effort and skippable
// (--no-wire); it never unwinds a successful scaffold.
package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/omap"
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
				result, err := workspace.Init(args[0], target, workspace.InitOptions{
					PresetSource: presetSource, Name: name, Force: force,
				})
				if err != nil {
					return err
				}

				// Best-effort tail: a wire hiccup does not unwind the scaffold above.
				var wireResult *wire.Result
				wireError := ""
				if !noWire { // no selection to make without a wizard: seed both files
					res, werr := wire.Wire(result.Path, wire.Options{Claude: true, Agents: true})
					switch {
					case werr == nil:
						wireResult = res
					default:
						var located *errs.Located
						if errors.As(werr, &located) {
							wireError = located.Message
						} else {
							wireError = werr.Error()
						}
					}
				}

				payload := initPayload(result, wireResult, wireError)
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
						tail := ""
						if len(head) > 3 {
							head, tail = head[:3], " …"
						}
						fmt.Printf("preserved %d workspace-owned file(s): %s%s\n",
							len(result.Preserved), strings.Join(head, ", "), tail)
					}
					if wireResult != nil {
						for _, outcome := range wireResult.Outcomes {
							fmt.Printf("%s %s\n", outcome.Action, filepath.Base(outcome.Path))
						}
					}
					if wireError != "" {
						fmt.Fprintf(os.Stderr, "wire skipped: %s\n", wireError)
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

// initPayload is dataclasses.asdict(result) in declaration order, with the wire
// tail and the skill hint appended.
func initPayload(result *workspace.InitResult, wireResult *wire.Result, wireError string) *omap.Map {
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
	if wireResult != nil {
		outcomes := make([]any, 0, len(wireResult.Outcomes))
		for _, o := range wireResult.Outcomes {
			record := omap.New()
			record.Set("path", o.Path)
			record.Set("action", o.Action)
			outcomes = append(outcomes, record)
		}
		payload.Set("wire", outcomes)
	}
	if wireError != "" {
		payload.Set("wire_error", wireError)
	}
	payload.Set("skill_hint", skillHintFor(result.Path))
	return payload
}
