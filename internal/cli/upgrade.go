// upgrade.go is `khub upgrade`: bring an existing workspace up to the khub
// binary now on PATH. The library call replaces .khub/ from the shipped preset
// and provisions what the ontology gained; two tails follow — wire, index —
// each non-fatal for the same reason init's wire tail is: by the time they run
// the workspace is already upgraded, and aborting there would leave it
// half-done with its context files never re-wired.

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/endgame-build/khub/internal/migrate"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/wire"
	"github.com/endgame-build/khub/internal/workspace"
)

func registerUpgrade(root *cobra.Command) {
	var noSchema, noWire, dryRun bool
	var format string
	cmd := newCmd("upgrade",
		"Refresh an existing workspace: the shipped schema and templates, new scaffolds, the wire block.",
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
				var wireStep tail[wire.Result]
				var indexStep tail[string]
				// held: the library runs the tails inside its own lock after the
				// core commit, so they must not lock again (fsio.Locked is not
				// re-entrant). A dry run previews against a temporary candidate
				// with no lock held, and the DryRun flags below already skip it.
				runTails := func(target string, held bool) {
					wireFn, indexFn := wire.Wire, indexTail
					if held {
						wireFn, indexFn = wire.WireHeld, indexTailHeld
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
						// A message may carry the candidate in either form: an OS
						// error the native one, khub's own prose the slash one.
						for _, from := range []string{target, filepath.ToSlash(target)} {
							wireStep.Err = strings.ReplaceAll(wireStep.Err, from, filepath.ToSlash(ws))
							indexStep.Err = strings.ReplaceAll(indexStep.Err, from, filepath.ToSlash(ws))
						}
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

				payload := upgradePayload(result, wireStep, indexStep)
				return Emit(payload, format, func() {
					if dryRun {
						fmt.Println("dry run — planned outcomes; workspace unchanged")
					}
					wireChanges := 0
					if wireStep.Result != nil {
						for _, o := range wireStep.Result.Outcomes {
							if o.Action != "unchanged" {
								wireChanges++
							}
						}
					}
					idle := len(result.Config) == 0 && len(result.Carried) == 0 && len(result.Dropped) == 0 && len(result.SingletonsCreated) == 0 &&
						len(result.Migrated) == 0 && len(result.MigrationConflicts) == 0 &&
						wireChanges == 0 &&
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
						for _, c := range result.Carried {
							fmt.Printf("kept %s %s.%s from your workspace\n", c.File, c.Type, c.Key)
						}
						for _, d := range result.Dropped {
							fmt.Printf("dropped %s %s.%s from your workspace: %s\n", d.File, d.Type, d.Key, d.Reason)
						}
						if len(result.SingletonsCreated) > 0 {
							fmt.Println("created singletons: " + strings.Join(result.SingletonsCreated, ", "))
						}
						for _, e := range result.Migrated {
							fmt.Println(migrationLine(e))
						}
						for _, c := range result.MigrationConflicts {
							fmt.Println(conflictLine(c))
						}
						if wireStep.Result != nil {
							for _, outcome := range wireStep.Result.Outcomes {
								// An untouched file is not news, and `idle` already
								// counts it as nothing.
								if outcome.Action != "unchanged" {
									fmt.Printf("%s %s\n", outcome.Action, filepath.Base(outcome.Path))
								}
							}
						}
						if indexStep.Err == "" {
							fmt.Printf("index.md %s\n", *indexStep.Result)
						}
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
		"migrates legacy entity fields (`draft`, bare `sources` entries, a retired `status`) in place; " +
		"preflights the candidate schema and scaffolds before publishing, then re-wires the agent " +
		"files and regenerates index.md. " +
		"Refuses outside a workspace: ``khub init`` scaffolds, ``khub upgrade`` refreshes."
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview core and tail outcomes without changing the workspace.")
	cmd.Flags().BoolVar(&noSchema, "no-schema", false,
		"Keep this workspace's .khub/ files — schema and templates — as they are; "+
			"report what the shipped ontology has that they do not.")
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
	result *workspace.UpgradeResult, wired tail[wire.Result], indexed tail[string],
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
	carried := make([]any, 0, len(result.Carried))
	for _, c := range result.Carried {
		record := omap.New()
		record.Set("file", c.File)
		record.Set("type", c.Type)
		record.Set("key", c.Key)
		carried = append(carried, record)
	}
	payload.Set("carried", carried)
	dropped := make([]any, 0, len(result.Dropped))
	for _, d := range result.Dropped {
		record := omap.New()
		record.Set("file", d.File)
		record.Set("type", d.Type)
		record.Set("key", d.Key)
		record.Set("reason", d.Reason)
		dropped = append(dropped, record)
	}
	payload.Set("dropped", dropped)
	payload.Set("singletons_created", strList(result.SingletonsCreated))
	payload.Set("schema_drift", strList(result.SchemaDrift))
	wired.set(payload, "wire", func(r *wire.Result) any { return wireOutcomes(r) }, true)
	indexed.set(payload, "index", func(action *string) any { return *action }, true)
	payload.Set("dry_run", result.DryRun)
	payload.Set("removed_types", strList(result.RemovedTypes))
	migrated := make([]any, 0, len(result.Migrated))
	for _, e := range result.Migrated {
		changes := make([]any, 0, len(e.Changes))
		for _, c := range e.Changes {
			change := omap.New()
			change.Set("op", string(c.Op))
			change.Set("field", c.Field)
			change.Set("from", c.From)
			change.Set("to", c.To)
			changes = append(changes, change)
		}
		record := omap.New()
		record.Set("id", e.ID)
		record.Set("path", e.Path)
		record.Set("changes", changes)
		record.Set("reemitted", e.Reemitted)
		migrated = append(migrated, record)
	}
	payload.Set("migrated", migrated)
	conflicts := make([]any, 0, len(result.MigrationConflicts))
	for _, c := range result.MigrationConflicts {
		record := omap.New()
		record.Set("id", nullable(c.ID))
		record.Set("path", c.Path)
		record.Set("field", nullable(c.Field))
		record.Set("reason", c.Reason)
		conflicts = append(conflicts, record)
	}
	payload.Set("migration_conflicts", conflicts)
	return payload
}

// conflictLine is one entity or file the migration left as written.
func conflictLine(c migrate.Conflict) string {
	if c.ID == "" {
		return fmt.Sprintf("not migrated %s: %s", c.Path, c.Reason)
	}
	return fmt.Sprintf("not migrated %s (%s): %s", c.ID, c.Path, c.Reason)
}

// migrationLine is one migrated entity in the text report:
// `migrated note/a (notes/a.md): draft → status, status true → draft`.
func migrationLine(e migrate.Entity) string {
	parts := make([]string, 0, len(e.Changes))
	for _, c := range e.Changes {
		switch c.Op {
		case migrate.OpRenameKey:
			parts = append(parts, fmt.Sprintf("%v → %v", c.From, c.To))
		case migrate.OpMapValue:
			parts = append(parts, fmt.Sprintf("%s %v → %v", c.Field, c.From, c.To))
		case migrate.OpDeleteKey:
			parts = append(parts, "dropped "+c.Field)
		case migrate.OpMoveEntries:
			parts = append(parts, fmt.Sprintf("%s → %v", c.Field, c.To))
		}
	}
	line := fmt.Sprintf("migrated %s (%s): %s", e.ID, e.Path, strings.Join(parts, ", "))
	if e.Reemitted {
		line += " — rewritten whole, comments not kept"
	}
	return line
}
