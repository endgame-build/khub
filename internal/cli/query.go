// query.go ports cli/query_cmd.py: filter entities by frontmatter and derived
// edges. Like add/edit it accepts arbitrary --<field> value filters, so it
// parses its own flags; the declared ones (--type, --tag, --has, --missing,
// --orphan, --stale, --draft/--active, --limit, --format) still bind.

package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/query"
)

func registerQuery(root *cobra.Command) {
	var (
		typeName, tag, has, missing, format string
		orphan, stale, draft, active, rcpt  bool
		limit                               int
	)
	cmd := newCmd("query", "Filter entities: khub query --type opportunity --stage prospect --format json.",
		"", nil)
	cmd.DisableFlagParsing = true
	cmd.Flags().StringVar(&typeName, "type", "", "Restrict to one entity type.")
	cmd.Flags().StringVar(&tag, "tag", "", "Keep entities carrying this tag.")
	cmd.Flags().StringVar(&has, "has", "", "Keep entities with a resolvable edge for the predicate.")
	cmd.Flags().StringVar(&missing, "missing", "", "Keep entities lacking a resolvable edge (gap finder).")
	cmd.Flags().BoolVar(&orphan, "orphan", false, "Keep only orphan (edge-less) entities.")
	cmd.Flags().BoolVar(&stale, "stale", false, "Keep only stale entities.")
	cmd.Flags().BoolVar(&draft, "draft", false, "Isolate drafts.")
	cmd.Flags().BoolVar(&active, "active", false, "Exclude drafts.")
	cmd.Flags().IntVar(&limit, "limit", 0, "Cap the returned set.")
	cmd.Flags().StringVar(&format, "format", "text", "text (Rich table on a TTY), json, or ids.")
	cmd.Flags().BoolVar(&rcpt, "receipt", false, "Wrap JSON output with the applied filters, matched and returned counts, and truncation.")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		_, extra, err := splitDynamic(cmd, args, 0)
		if err != nil {
			return clickUsageErr(cmd.CommandPath(), "", clickFlagMessage(cmd, err))
		}
		if helpRequested(cmd) {
			return cmd.Help()
		}
		return Guard(format, func() error {
			fields, ferr := fieldsOrUsage(cmd, extra, "")
			if ferr != nil {
				return ferr
			}
			ws, rerr := resolveRoot()
			if rerr != nil {
				return rerr
			}
			filters := query.Filters{
				Type:       optString(cmd, "type", typeName),
				Fields:     fields,
				Tag:        optString(cmd, "tag", tag),
				Has:        optString(cmd, "has", has),
				Missing:    optString(cmd, "missing", missing),
				Orphan:     orphan,
				Stale:      stale,
				DraftOnly:  draft,
				ActiveOnly: active,
				Limit:      optInt(cmd, "limit", limit),
			}
			matches, matched, qerr := query.QueryCounted(ws, filters, today())
			if qerr != nil {
				return qerr
			}
			if format == "ids" { // bare ids for piping — independent of TTY detection
				for _, m := range matches {
					fmt.Println(m.Slug)
				}
				return nil
			}
			records := make([]any, 0, len(matches))
			rows := [][]string{}
			for _, m := range matches {
				record := omap.New()
				record.Set("id", m.Type+"/"+m.Slug)
				record.Set("type", m.Type)
				record.Set("slug", m.Slug)
				record.Set("title", m.Title)
				record.Set("draft", m.Draft)
				record.Set("orphan", m.Orphan)
				record.Set("stale", m.Stale)
				records = append(records, record)
				rows = append(rows, []string{
					m.Type + "/" + m.Slug, m.Type, m.Title,
					pyBool(m.Draft), pyBool(m.Orphan), pyBool(m.Stale),
				})
			}
			var doc any = records
			if rcpt {
				doc = withReceipt(queryApplied(filters), matched, records)
			}
			return Emit(doc, format, func() {
				if len(rows) == 0 {
					fmt.Println("No entities match")
					return
				}
				printTable("query", []string{"id", "type", "title", "draft", "orphan", "stale"}, rows)
			})
		})
	}
	root.AddCommand(cmd)
}

// queryApplied echoes the filter set a query ran, every key present: an
// option the caller left out is null (false for a switch), and fields holds
// the --<field> filters in input order.
func queryApplied(f query.Filters) *omap.Map {
	fields := f.Fields
	if fields == nil {
		fields = omap.New()
	}
	applied := omap.New()
	applied.Set("type", orNull(f.Type))
	applied.Set("fields", fields)
	applied.Set("tag", orNull(f.Tag))
	applied.Set("has", orNull(f.Has))
	applied.Set("missing", orNull(f.Missing))
	applied.Set("orphan", f.Orphan)
	applied.Set("stale", f.Stale)
	applied.Set("draft", f.DraftOnly)
	applied.Set("active", f.ActiveOnly)
	applied.Set("limit", orNull(f.Limit))
	return applied
}
