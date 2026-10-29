// gitlog.go ports cli/gitlog_cmd.py: the git-derived reads. Neither gates —
// `stale` and `recent` report a (possibly empty) set. The machine view carries
// the git fallback in each entry's `source`; the human view gets the notice,
// so the JSON payload stays a clean list.

package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/endgame-build/khub/internal/gitlog"
	"github.com/endgame-build/khub/internal/omap"
)

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
				report, err := gitlog.Stale(ws, optInt(cmd, "days", days), today())
				if err != nil {
					return err
				}
				records := make([]any, 0, len(report.Entries))
				rows := [][]string{}
				for _, e := range report.Entries {
					record := omap.New()
					record.Set("id", e.Type+"/"+e.Slug)
					record.Set("type", e.Type)
					record.Set("slug", e.Slug)
					record.Set("effective_date", e.EffectiveDate.Format("2006-01-02"))
					record.Set("age", e.Age)
					record.Set("source", e.Source)
					records = append(records, record)
					rows = append(rows, []string{
						e.Type + "/" + e.Slug, e.EffectiveDate.Format("2006-01-02"),
						fmt.Sprint(e.Age), e.Source,
					})
				}
				return Emit(records, format, func() {
					if !report.GitAvailable {
						fmt.Println(noGitNotice)
					}
					if len(rows) == 0 {
						fmt.Println("No stale entities")
						return
					}
					printTable("stale", []string{"id", "effective_date", "age", "source"}, rows)
				})
			})
		})
	cmd.Args = clickArity(0)
	cmd.Flags().IntVar(&days, "days", 0, "Staleness threshold in days; default: the workspace's stale_days.")
	cmd.Flags().StringVar(&format, "format", "text", "text (Rich table on a TTY) or json.")
	root.AddCommand(cmd)
}

// noGitNotice is the human view's line when the workspace has no git history:
// an undated entity then falls back to `created`, not to a commit date.
const noGitNotice = "No git history; dates come from updated or created only"

// recentDays is `recent`'s default window: a week.
const recentDays = 7

func registerRecent(root *cobra.Command) {
	var format string
	var days, limit int
	cmd := newCmd("recent", "List entities touched within the window, newest first: khub recent [--days N].",
		"", func(cmd *cobra.Command, args []string) error {
			if days < 0 {
				return usageBox(cmd, fmt.Sprintf("Invalid value for '--days': %d is not in the range x>=0.", days))
			}
			return Guard(format, func() error {
				ws, err := resolveRoot()
				if err != nil {
					return err
				}
				report, err := gitlog.Recent(ws, days, optInt(cmd, "limit", limit), today())
				if err != nil {
					return err
				}
				if dropped := report.Total - len(report.Entries); dropped > 0 {
					Note(fmt.Sprintf("showing %d of %d recent entities; pass --limit %d for all",
						len(report.Entries), report.Total, report.Total))
				}
				records := make([]any, 0, len(report.Entries))
				rows := [][]string{}
				for _, e := range report.Entries {
					record := omap.New()
					record.Set("id", e.Type+"/"+e.Slug)
					record.Set("type", e.Type)
					record.Set("slug", e.Slug)
					record.Set("title", e.Title)
					record.Set("draft", e.Draft)
					record.Set("effective_date", e.EffectiveDate.Format("2006-01-02"))
					record.Set("age", e.Age)
					record.Set("source", e.Source)
					records = append(records, record)
					rows = append(rows, []string{
						e.Type + "/" + e.Slug, e.Title, e.EffectiveDate.Format("2006-01-02"),
						fmt.Sprint(e.Age), e.Source,
					})
				}
				return Emit(records, format, func() {
					if !report.GitAvailable {
						fmt.Println(noGitNotice)
					}
					if len(rows) == 0 {
						fmt.Println("No recent entities")
						return
					}
					printTable("recent", []string{"id", "title", "effective_date", "age", "source"}, rows)
				})
			})
		})
	cmd.Args = clickArity(0)
	cmd.Flags().IntVar(&days, "days", recentDays, "Window in days; an entity at most N days old is recent.")
	cmd.Flags().IntVar(&limit, "limit", 0, "Cap the returned set.")
	cmd.Flags().StringVar(&format, "format", "text", "text (Rich table on a TTY) or json.")
	root.AddCommand(cmd)
}
