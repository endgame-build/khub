// gitlog.go ports cli/gitlog_cmd.py: the git-derived read. It does not gate —
// `stale` reports a (possibly empty) set. The machine view carries the git
// fallback in each entry's `source`; the human view gets the notice, so the
// JSON payload stays a clean list.

package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/endgame-build/khub/internal/clock"
	"github.com/endgame-build/khub/internal/gitlog"
	"github.com/endgame-build/khub/internal/omap"
)

// nilBlank renders a missing value as an empty cell.
func nilBlank(v any) any {
	if v == nil {
		return ""
	}
	return v
}

func registerStale(root *cobra.Command) {
	var format string
	var days int
	cmd := newCmd("stale", "List entities past the `updated` threshold, oldest first: khub stale [--days N].",
		"", func(cmd *cobra.Command, args []string) error {
			return Guard(format, func() error {
				ws, err := resolveRoot()
				if err != nil {
					return err
				}
				// --days omitted → the workspace's configured stale_days.
				report, err := gitlog.Stale(ws, optInt(cmd, "days", days), clock.Now())
				if err != nil {
					return err
				}
				records := make([]any, 0, len(report.Entries))
				rows := [][]string{}
				for _, e := range report.Entries {
					// A date that does not parse prints as written, with no age.
					date, age, ageText := any(e.EffectiveDate.Format("2006-01-02")), any(e.Age), fmt.Sprint(e.Age)
					if e.Unparseable {
						date, age, ageText = e.Raw, nil, ""
						if e.Raw == "" {
							date = nil
						}
					}
					record := omap.New()
					record.Set("id", e.Type+"/"+e.Slug)
					record.Set("type", e.Type)
					record.Set("slug", e.Slug)
					record.Set("effective_date", date)
					record.Set("age", age)
					record.Set("source", e.Source)
					record.Set("reason", e.Reason)
					records = append(records, record)
					rows = append(rows, []string{e.Type + "/" + e.Slug, fmt.Sprint(nilBlank(date)), ageText, e.Source, e.Reason})
				}
				return Emit(records, format, func() {
					if !report.GitAvailable {
						fmt.Println("No git history; using updated field only")
					}
					if len(rows) == 0 {
						fmt.Println("No stale entities")
						return
					}
					printTable("stale", []string{"id", "effective_date", "age", "source", "reason"}, rows)
				})
			})
		})
	cmd.Args = clickArity(0)
	cmd.Flags().IntVar(&days, "days", 0, "Staleness threshold in days; default: the workspace's stale_days.")
	cmd.Flags().StringVar(&format, "format", "text", "text (Rich table on a TTY) or json.")
	root.AddCommand(cmd)
}
