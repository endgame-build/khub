// layout.go is `khub layout`: where each type's bytes land, and `khub layout
// migrate`, the verb that moves a type's entities to another layout, format
// or path. A thin adapter over internal/layout, with zero per-type knowledge:
// every row and every refusal comes from the resolved schema.

package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/endgame-build/khub/internal/layout"
	"github.com/endgame-build/khub/internal/omap"
)

func registerLayout(root *cobra.Command) {
	var groupFormat string
	layoutCmd := newCmd("layout", "Show where each type's entities land: khub layout [migrate].",
		"COMMAND [ARGS]...", func(cmd *cobra.Command, args []string) error {
			return Guard(groupFormat, func() error { return layoutShow(groupFormat) })
		})
	layoutCmd.Args = clickNoSuchCommand
	layoutCmd.Flags().StringVar(&groupFormat, "format", "text", formatHelp)

	var toLayout, toFormat, toPath, format string
	var execute bool
	migrateCmd := newCmd("migrate",
		"Move a type's entities to another layout, format or path: khub layout migrate repo --layout collection --to-format jsonl.",
		"{type}", func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return missingArgument(cmd.CommandPath(), "{type}", "type")
			}
			return Guard(format, func() error {
				return layoutMigrate(args[0], layout.Target{Layout: toLayout, Format: toFormat, Path: toPath}, execute, format)
			})
		})
	migrateCmd.Args = clickArity(1)
	migrateCmd.Long = migrateCmd.Short + "\n\nA dry run by default: the plan is printed and nothing is written. " +
		"--execute performs it under the workspace lock, journaled like ``khub upgrade``, then " +
		"regenerates index.md. The plan refuses a destination that exists, a storage tree holding " +
		"files that are not the type's entities, and a move the schema would not resolve."
	migrateCmd.Flags().StringVar(&toLayout, "layout", "", "Destination layout: file, folder or collection (default: unchanged).")
	migrateCmd.Flags().StringVar(&toFormat, "to-format", "", "Destination storage format: md, json, yaml, or jsonl for a collection (default: unchanged).")
	migrateCmd.Flags().StringVar(&toPath, "path", "", "Destination directory or collection file (default: the current path, with the extension a collection needs).")
	migrateCmd.Flags().BoolVar(&execute, "execute", false, "Perform the migration (default: dry run).")
	migrateCmd.Flags().StringVar(&format, "format", "text", "text (default) or json (emits the plan and what ran).")

	layoutCmd.AddCommand(migrateCmd)
	root.AddCommand(layoutCmd)
}

func layoutShow(format string) error {
	ws, err := resolveRoot()
	if err != nil {
		return err
	}
	rows, err := layout.Show(ws)
	if err != nil {
		return err
	}
	payload := make([]any, 0, len(rows))
	table := make([][]string, 0, len(rows))
	for _, r := range rows {
		rec := omap.New()
		rec.Set("type", r.Type)
		rec.Set("layout", r.Layout)
		rec.Set("format", r.Format)
		rec.Set("path", r.Path)
		payload = append(payload, rec)
		table = append(table, []string{r.Type, r.Layout, r.Format, r.Path})
	}
	return Emit(payload, format, func() {
		printTable("layout", []string{"type", "layout", "format", "path"}, table)
	})
}

func layoutMigrate(typeName string, target layout.Target, execute bool, format string) error {
	ws, err := resolveRoot()
	if err != nil {
		return err
	}
	var indexStep tail[string]
	opt := layout.Options{Execute: execute}
	if execute {
		// Held: the library runs the tail inside its own lock after the
		// commit, so it must not lock again (fsio.Locked is not re-entrant).
		opt.Tails = func(root string) { indexStep = indexTailHeld(root) }
	}
	plan, err := layout.Migrate(ws, typeName, target, opt)
	if err != nil {
		return err
	}
	payload := omap.New()
	payload.Set("type", plan.Type)
	payload.Set("from", storageRecord(plan.From))
	payload.Set("to", storageRecord(plan.To))
	payload.Set("entities", plan.Entities)
	payload.Set("create", strList(plan.Create))
	payload.Set("rewrite", strList(plan.Rewrite))
	payload.Set("remove", strList(plan.Remove))
	payload.Set("rewritten", strList(plan.Rewritten))
	payload.Set("stale_reset", plan.StaleReset)
	payload.Set("storage_yaml", plan.StorageYAML)
	payload.Set("dry_run", !plan.Executed)
	indexStep.set(payload, "index", func(action *string) any { return *action }, true)
	return Emit(payload, format, func() {
		fmt.Printf("%s: %s/%s at %s -> %s/%s at %s (%d entities)\n", plan.Type,
			plan.From.Layout, plan.From.Format, plan.From.Path,
			plan.To.Layout, plan.To.Format, plan.To.Path, plan.Entities)
		for _, p := range plan.Create {
			fmt.Printf("create %s\n", p)
		}
		for _, p := range plan.Rewrite {
			fmt.Printf("rewrite %s\n", p)
		}
		for _, p := range plan.Remove {
			fmt.Printf("remove %s\n", p)
		}
		fmt.Printf("rewrite %s (storage.%s)\n", layout.StorageYAML, plan.Type)
		for _, p := range plan.Rewritten {
			fmt.Printf("rewritten %s (comments, quoting or layout in this file will not round-trip)\n", p)
		}
		if plan.StaleReset > 0 {
			fmt.Printf("stale: %d undated entities will take the migration commit's date\n", plan.StaleReset)
		}
		if !plan.Executed {
			fmt.Println("dry run — nothing written; pass --execute to migrate")
			return
		}
		fmt.Printf("Migrated %d %s entities to %s\n", plan.Entities, plan.Type, plan.To.Path)
		if indexStep.Err == "" && indexStep.Result != nil {
			fmt.Printf("index.md %s\n", *indexStep.Result)
		}
		if indexStep.Err != "" {
			fmt.Fprintf(os.Stderr, "index skipped: %s\n", indexStep.Err)
		}
	})
}

// storageRecord is one side of the plan's `from`/`to`, in the schema's own
// key order: layout, format, path.
func storageRecord(s layout.Storage) *omap.Map {
	rec := omap.New()
	rec.Set("layout", s.Layout)
	rec.Set("format", s.Format)
	rec.Set("path", s.Path)
	return rec
}
