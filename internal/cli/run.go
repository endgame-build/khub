// run.go: `khub run <name>` replays a query saved with `query --save-as`.
// The definition reads back into the same filter set, runs through the same
// query and renders through the same rows, so a saved query answers exactly
// as `khub query` with its flags would today.

package cli

import (
	"github.com/spf13/cobra"

	"github.com/endgame-build/khub/internal/query"
	"github.com/endgame-build/khub/internal/savedquery"
)

func registerRun(root *cobra.Command) {
	var format string
	cmd := newCmd("run {name}", "Run a saved query: khub run open-prospects --format json.",
		"{name}", func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return missingArgument(cmd.CommandPath(), "{name}", "name")
			}
			return Guard(format, func() error {
				ws, err := resolveRoot()
				if err != nil {
					return err
				}
				def, err := savedquery.Get(ws, args[0])
				if err != nil {
					return err
				}
				filters, err := savedquery.Filters(args[0], def)
				if err != nil {
					return err
				}
				matches, err := query.Query(ws, filters, today())
				if err != nil {
					return savedquery.Outdated(args[0], err)
				}
				return emitMatches(matches, format)
			})
		})
	cmd.Args = clickArity(1)
	cmd.Flags().StringVar(&format, "format", "text", "text (Rich table on a TTY), json, or ids.")
	root.AddCommand(cmd)
}
