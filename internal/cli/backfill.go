// backfill.go ports cli/backfill_cmd.py: additive frontmatter/date writes for
// cutover. It does not gate — an already-valid tree and a non-git workspace are
// both success.
package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/endgame-build/khub/internal/backfill"
)

func registerBackfill(root *cobra.Command) {
	var typeName string
	var dryRun bool
	cmd := newCmd("backfill", "Backfill missing dates and frontmatter: khub backfill [--type T] [--dry-run].",
		"", func(cmd *cobra.Command, args []string) error {
			return Guard("text", func() error {
				ws, err := resolveRoot()
				if err != nil {
					return err
				}
				report, err := backfill.Backfill(ws, optString(cmd, "type", typeName), dryRun)
				if err != nil {
					return err
				}
				backfillEmit(report)
				return nil
			})
		})
	cmd.Args = clickArity(0)
	cmd.Flags().StringVar(&typeName, "type", "",
		"Add missing per-type frontmatter scaffolding for that type.")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false,
		"List the entities and fields that would change; write nothing.")
	root.AddCommand(cmd)
}

// backfillEmit is backfill_cmd._emit: a write command, so it prints text — no
// JSON contract (unlike the read commands).
func backfillEmit(report *backfill.BackfillReport) {
	if report.DryRun {
		for _, group := range byEntity(report) {
			fmt.Printf("%s: %s\n", group.id, strings.Join(group.fields, ", "))
		}
		if len(report.Changes) == 0 {
			fmt.Println("No changes")
		}
		emitSkipped(report) // a preview that omits the skip is not a preview
		return
	}
	if !report.GitAvailable {
		fmt.Println("No git history; dates not backfilled")
	} else {
		fmt.Printf("Backfilled dates on %d entities\n", report.DatedEntities())
	}
	// A --type run writes scaffolding even with no git; surface it so the "not
	// backfilled" line above is never the whole story when files did change.
	if n := report.ScaffoldedEntities(); n > 0 {
		fmt.Printf("Scaffolded frontmatter on %d entities\n", n)
	}
	emitSkipped(report)
}

// emitSkipped is the collection-skip line — printed by both the real run and
// the dry-run.
func emitSkipped(report *backfill.BackfillReport) {
	if len(report.SkippedCollections) == 0 {
		return
	}
	fmt.Printf("Skipped collection types (%s): row-level git dates land with row-diff attribution\n",
		strings.Join(report.SkippedCollections, ", "))
}

type entityChanges struct {
	id     string
	fields []string
}

// byEntity groups changes per entity id, preserving first-seen order — the
// dry-run list.
func byEntity(report *backfill.BackfillReport) []entityChanges {
	index := map[string]int{}
	groups := []entityChanges{}
	for _, c := range report.Changes {
		id := c.ID()
		if at, seen := index[id]; seen {
			groups[at].fields = append(groups[at].fields, c.Field)
			continue
		}
		index[id] = len(groups)
		groups = append(groups, entityChanges{id: id, fields: []string{c.Field}})
	}
	return groups
}
