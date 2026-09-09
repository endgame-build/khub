// skill.go ports cli/skill_cmd.py: copy khub's agent skills into the local
// agent directories. The install is a file copy out of the binary — offline,
// silent, and safe to re-run.

package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/skill"
	"github.com/endgame-build/khub/internal/skills"
)

func registerInstallSkills(root *cobra.Command) {
	var targets, wanted []string
	var global, dryRun bool
	var format string
	cmd := newCmd("install-skills",
		"Install khub's agent skills: khub install-skills [--target agents] [--global].",
		"", func(cmd *cobra.Command, args []string) error {
			return Guard(format, func() error {
				// --global writes under $HOME and needs no workspace — which is the
				// point: a machine-wide install is what you run before any workspace
				// exists.
				ws := ""
				if !global {
					resolved, err := resolveRoot()
					if err != nil {
						return err
					}
					ws = resolved
				}
				report, err := skill.Install(skills.FS(), ws, skill.Options{
					Skills: emptyToNil(wanted), Targets: emptyToNil(targets),
					Global: global, DryRun: dryRun,
				})
				if err != nil {
					return err
				}
				record := omap.New()
				record.Set("scope", report.Scope)
				record.Set("skills", strList(report.Skills))
				record.Set("dry_run", report.DryRun)
				record.Set("writes", skillWrites(report))
				rows := [][]string{}
				changed := 0
				for _, w := range report.Writes {
					rows = append(rows, []string{w.Path, w.Action})
					if w.Action != "unchanged" {
						changed++
					}
				}
				return Emit(record, format, func() {
					if len(rows) == 0 {
						fmt.Println("No skills to install")
						return
					}
					printTable(fmt.Sprintf("install-skills (%s)", report.Scope),
						[]string{"path", "action"}, rows)
					verb := "wrote"
					if report.DryRun {
						verb = "would write"
					}
					fmt.Printf("%s %d of %d files (%s scope)\n",
						verb, changed, len(report.Writes), report.Scope)
				})
			})
		})
	cmd.Args = clickArity(0)
	cmd.Flags().StringArrayVar(&targets, "target", nil,
		"claude, agents, or opencode (repeatable). Default: all three.")
	cmd.Flags().StringArrayVar(&wanted, "skill", nil,
		"Which skill to install (repeatable). Default: all shipped.")
	cmd.Flags().BoolVar(&global, "global", false,
		"Install into the home directories instead of this workspace.")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Report what would be written, and write nothing.")
	cmd.Flags().StringVar(&format, "format", "text", "text (Rich table on a TTY) or json.")
	root.AddCommand(cmd)
}

// skillWrites renders an install's per-file writes as the `{path, action}`
// list install-skills carries under `writes` and upgrade under `skills`.
func skillWrites(report *skill.Report) []any {
	items := make([]pathAction, 0, len(report.Writes))
	for _, w := range report.Writes {
		items = append(items, pathAction{w.Path, w.Action})
	}
	return pathActionRecords(items)
}

// emptyToNil is Python's `value or None`: an unrepeated option means "all".
func emptyToNil(ss []string) []string {
	if len(ss) == 0 {
		return nil
	}
	return ss
}
