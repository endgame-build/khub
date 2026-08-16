// status.go ports cli/status_cmd.py: per-type counts, the draft/active split,
// orphan/stale counts, and the OKF flag — every number derived from the graph
// projection, none stored.
package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/project"
	"github.com/endgame-build/khub/internal/workspace"
)

func registerStatus(root *cobra.Command) {
	var format string
	cmd := newCmd("status", "Summarize the workspace: counts, draft/active, orphan/stale, OKF conformance.",
		"", func(cmd *cobra.Command, args []string) error {
			return Guard(format, func() error { return statusRun(format) })
		})
	cmd.Args = clickArity(0)
	cmd.Flags().StringVar(&format, "format", "text", "text (Rich table on a TTY) or json.")
	root.AddCommand(cmd)
}

func statusRun(format string) error {
	ws, err := resolveRoot()
	if err != nil {
		return err
	}
	days, err := workspace.StaleDays(ws)
	if err != nil {
		return err
	}
	proj, err := project.Project(ws, days, today())
	if err != nil {
		return err
	}

	data := omap.New()
	data.Set("counts", proj.Counts)
	data.Set("total", proj.Total)
	data.Set("draft", proj.Draft)
	data.Set("active", proj.Active)
	data.Set("orphan", proj.Orphan)
	data.Set("stale", proj.Stale)
	data.Set("okf_conformant", proj.OKFConformant)
	// Python guards these with getattr; the Go projection always carries them.
	data.Set("stray", proj.Stray)
	data.Set("malformed", proj.Malformed)

	return Emit(data, format, func() {
		if proj.Total == 0 {
			fmt.Println("Workspace initialized; no entities yet")
			return
		}
		rows := [][]string{}
		for _, tname := range proj.Counts.Keys() {
			count, _ := proj.Counts.Get(tname)
			rows = append(rows, []string{tname, fmt.Sprint(count)})
		}
		rows = append(rows,
			[]string{"draft / active", fmt.Sprintf("%d / %d", proj.Draft, proj.Active)},
			[]string{"orphan", fmt.Sprint(proj.Orphan)},
			[]string{"stale", fmt.Sprint(proj.Stale)},
			[]string{"stray", fmt.Sprint(proj.Stray)},
			[]string{"malformed", fmt.Sprint(proj.Malformed)},
		)
		conformant := "no"
		if proj.OKFConformant {
			conformant = "yes"
		}
		rows = append(rows, []string{"OKF-conformant", conformant})
		printTable("status", []string{"type", "count"}, rows)
	})
}
