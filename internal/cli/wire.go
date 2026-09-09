// wire.go ports cli/wire_cmd.py: link the workspace into the agent context
// files. CLAUDE.md gets `@.khub/*.yaml` schema imports; AGENTS.md gets schema
// pointer. Bare `wire` updates whichever already exist.

package cli

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/endgame-build/khub/internal/wire"
)

func registerWire(root *cobra.Command) {
	var target string
	var dryRun bool
	cmd := newCmd("wire",
		"Wire the workspace into agent context files (CLAUDE.md gets ``@`` imports of the\n"+
			"schema layer files; AGENTS.md gets a schema pointer). Bare ``wire`` updates whichever already exist.",
		"", func(cmd *cobra.Command, args []string) error {
			return Guard("text", func() error {
				ws, err := resolveRoot()
				if err != nil {
					return err
				}
				opt, err := wire.ResolveTarget(optString(cmd, "target", target))
				if err != nil {
					return err
				}
				opt.DryRun = dryRun
				result, err := wire.Wire(ws, opt)
				if err != nil {
					return err
				}
				if dryRun {
					fmt.Println(result.Preview)
					return nil
				}
				for _, outcome := range result.Outcomes {
					fmt.Printf("%s %s\n", outcome.Action, filepath.Base(outcome.Path))
				}
				return nil
			})
		})
	cmd.Args = clickArity(0)
	cmd.Flags().StringVar(&target, "target", "",
		"Create and wire a specific file: claude, agents, or both. "+
			"Omit to update the agent files that already exist.")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print the block(s); write nothing.")
	root.AddCommand(cmd)
}
