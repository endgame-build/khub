// upgrade.go is `khub upgrade`: bring an existing workspace up to the khub
// binary now on PATH. The library call replaces .khub/ from the shipped preset
// and provisions what the ontology gained; three tails follow — skills, wire,
// index — each non-fatal for the same reason init's wire tail is: by the time
// they run the workspace is already upgraded, and aborting there would leave
// it half-done with its context files never re-wired.

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/skill"
	"github.com/endgame-build/khub/internal/skills"
	"github.com/endgame-build/khub/internal/wire"
	"github.com/endgame-build/khub/internal/workspace"
)

func registerUpgrade(root *cobra.Command) {
	var noSchema, noSkill, noWire, dryRun bool
	var format string
	cmd := newCmd("upgrade",
		"Refresh an existing workspace: the shipped schema and templates, new scaffolds, the skills, the wire block.",
		"", func(cmd *cobra.Command, args []string) error {
			return Guard(format, func() error {
				// resolveRoot, not the cwd: an upgrade in an empty directory
				// must refuse (no_workspace), never quietly scaffold one there
				// and call it an upgrade.
				root, err := resolveRoot()
				if err != nil {
					return err
				}
				// The resolved root is absolute; every path from here on is
				// reported the way init reports the target it was given, so
				// hand the library and the tails the same relative form init
				// hands them ("." at the root).
				ws := displayRoot(root)
				var skillsStep tail[skill.Report]
				var wireStep tail[wire.Result]
				var indexStep tail[string]
				// held: the library runs the tails inside its own lock after the
				// core commit, so they must not lock again (fsio.Locked is not
				// re-entrant). A dry run previews against a temporary candidate
				// with no lock held, and the DryRun flags below already skip it.
				runTails := func(target string, held bool) {
					installFn, wireFn, indexFn := skill.Install, wire.Wire, indexTail
					if held {
						installFn, wireFn, indexFn = skill.InstallHeld, wire.WireHeld, indexTailHeld
					}
					if !noSkill {
						skillsStep = tailOf(installFn(skills.FS(), target, skill.Options{DryRun: dryRun}))
					}
					if !noWire {
						wireStep = tailOf(wireFn(target, wire.Options{Claude: true, Agents: true, DryRun: dryRun}))
					}
					indexStep = indexFn(target)
					if dryRun {
						if wireStep.Result != nil {
							for i := range wireStep.Result.Outcomes {
								outcome := &wireStep.Result.Outcomes[i]
								if rel, err := filepath.Rel(target, outcome.Path); err == nil {
									outcome.Path = filepath.Join(ws, rel)
								}
							}
						}
						skillsStep.Err = strings.ReplaceAll(skillsStep.Err, target, ws)
						wireStep.Err = strings.ReplaceAll(wireStep.Err, target, ws)
						indexStep.Err = strings.ReplaceAll(indexStep.Err, target, ws)
					}
				}
				opt := workspace.UpgradeOptions{NoSchema: noSchema, DryRun: dryRun}
				if dryRun {
					opt.Preview = func(candidate string) error { runTails(candidate, false); return nil }
				} else {
					opt.Tails = func(root string) { runTails(root, true) }
				}
				result, err := workspace.Upgrade(ws, opt)
				if err != nil {
					return err
				}

				payload := upgradePayload(result, skillsStep, wireStep, indexStep)
				return Emit(payload, format, func() {
					if dryRun {
						fmt.Println("dry run — planned outcomes; workspace unchanged")
					}
					skillChanges := 0
					if skillsStep.Result != nil {
						for _, w := range skillsStep.Result.Writes {
							if w.Action != "unchanged" {
								skillChanges++
							}
						}
					}
					wireChanges := 0
					if wireStep.Result != nil {
						for _, o := range wireStep.Result.Outcomes {
							if o.Action != "unchanged" {
								wireChanges++
							}
						}
					}
					idle := len(result.Config) == 0 && len(result.SingletonsCreated) == 0 &&
						skillChanges == 0 && wireChanges == 0 &&
						indexStep.Err == "" && *indexStep.Result == "unchanged"
					if idle {
						fmt.Println("nothing to do — already up to date")
					} else {
						for _, c := range result.Config {
							fmt.Printf("%s %s\n", c.Action, c.Name)
							if c.Backup != "" {
								fmt.Printf("  your edits are in %s — khub replaced a file you had changed\n",
									c.Backup)
							}
						}
						if len(result.SingletonsCreated) > 0 {
							fmt.Println("created singletons: " + strings.Join(result.SingletonsCreated, ", "))
						}
						if skillsStep.Result != nil {
							for _, w := range skillsStep.Result.Writes {
								if w.Action != "unchanged" {
									fmt.Printf("%s %s\n", w.Action, w.Path)
								}
							}
						}
						if wireStep.Result != nil {
							for _, outcome := range wireStep.Result.Outcomes {
								// As with the skills above: an untouched file is
								// not news, and `idle` already counts it as nothing.
								if outcome.Action != "unchanged" {
									fmt.Printf("%s %s\n", outcome.Action, filepath.Base(outcome.Path))
								}
							}
						}
						if indexStep.Err == "" {
							fmt.Printf("index.md %s\n", *indexStep.Result)
						}
					}
					if skillsStep.Err != "" {
						fmt.Fprintf(os.Stderr, "skills skipped: %s\n", skillsStep.Err)
					}
					if wireStep.Err != "" {
						fmt.Fprintf(os.Stderr, "wire skipped: %s\n", wireStep.Err)
					}
					if indexStep.Err != "" {
						fmt.Fprintf(os.Stderr, "index skipped: %s\n", indexStep.Err)
					}
					if len(result.RemovedTypes) > 0 {
						fmt.Println("removed types (entity files retained): " + strings.Join(result.RemovedTypes, ", "))
					}
					if len(result.SchemaDrift) > 0 {
						fmt.Printf("\nthe shipped ontology declares types your .khub/ontology.yaml does not: %s\n",
							strings.Join(result.SchemaDrift, ", "))
						fmt.Println("drop --no-schema to take them, or merge by hand")
					}
				})
			})
		})
	cmd.Args = clickArity(0)
	cmd.Long = cmd.Short + "\n\nReplaces .khub/{ontology,policy,storage}.yaml and .khub/templates/*.yaml from " +
		"the preset recorded in .khub/config.yaml, copying an edited file to <name>.bak first; " +
		"preflights the candidate schema and scaffolds before publishing, then re-installs the agent " +
		"skills, re-wires the agent files, and regenerates index.md. " +
		"Refuses outside a workspace: ``khub init`` scaffolds, ``khub upgrade`` refreshes."
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview core and tail outcomes without changing the workspace.")
	cmd.Flags().BoolVar(&noSchema, "no-schema", false,
		"Keep this workspace's .khub/ files — schema and templates — as they are; "+
			"report what the shipped ontology has that they do not.")
	cmd.Flags().BoolVar(&noSkill, "no-skill", false,
		"Skip refreshing the agent skills; scaffolds, wire and index only.")
	cmd.Flags().BoolVar(&noWire, "no-wire", false, "Skip re-wiring CLAUDE.md and AGENTS.md.")
	cmd.Flags().StringVar(&format, "format", "text",
		"text confirmation (default); json emits every step's outcome.")
	root.AddCommand(cmd)
}

// displayRoot renders the resolved workspace relative to the working
// directory — "." when upgrade runs at the root, ".." from a subdirectory,
// the -C argument's shape otherwise — never the absolute form. init passes
// the target as given ("." for `init build-lite .`) to the library, wire and
// index tails alike, and every path in its payload comes out relative
// because of it; upgrade has no argument to pass, so this is its stand-in.
func displayRoot(ws string) string {
	rel, err := filepath.Rel(resolvePath("."), ws)
	if err != nil {
		return ws
	}
	return rel
}

// upgradePayload is the JSON contract, in order: the library result, then one
// key per tail — null where the tail was skipped or failed, with a *_error
// beside it on failure.
func upgradePayload(
	result *workspace.UpgradeResult,
	skilled tail[skill.Report], wired tail[wire.Result], indexed tail[string],
) *omap.Map {
	payload := omap.New()
	payload.Set("path", result.Path)
	payload.Set("preset", result.Preset)
	payload.Set("version_from", result.VersionFrom)
	payload.Set("version_to", result.VersionTo)
	config := make([]any, 0, len(result.Config))
	for _, c := range result.Config {
		record := omap.New()
		record.Set("name", c.Name)
		record.Set("action", c.Action)
		record.Set("backup", nullable(c.Backup))
		config = append(config, record)
	}
	payload.Set("config", config)
	payload.Set("singletons_created", strList(result.SingletonsCreated))
	payload.Set("schema_drift", strList(result.SchemaDrift))
	skilled.set(payload, "skills", func(r *skill.Report) any { return skillWrites(r) }, true)
	wired.set(payload, "wire", func(r *wire.Result) any { return wireOutcomes(r) }, true)
	indexed.set(payload, "index", func(action *string) any { return *action }, true)
	payload.Set("dry_run", result.DryRun)
	payload.Set("removed_types", strList(result.RemovedTypes))
	return payload
}
