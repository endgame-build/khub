// query.go ports cli/query_cmd.py: filter entities by frontmatter and derived
// edges. Like add/edit it accepts arbitrary --<field> value filters, so it
// parses its own flags; the declared ones (--type, --tag, --has, --missing,
// --orphan, --stale, --draft/--active, --limit, --format, --save-as,
// --replace) still bind. `khub run <name>` replays a saved filter set through
// the same query and the same output.

package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/query"
	"github.com/endgame-build/khub/internal/savedquery"
)

func registerQuery(root *cobra.Command) {
	var (
		typeName, tag, has, missing, format, saveAs string
		orphan, stale, draft, active, replace       bool
		limit                                       int
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
	cmd.Flags().StringVar(&saveAs, "save-as", "", "Also save these filters under a name for `khub run <name>`.")
	cmd.Flags().BoolVar(&replace, "replace", false, "Let --save-as overwrite a different saved query of that name.")
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
			if replace && !cmd.Flags().Changed("save-as") {
				return clickUsageErr(cmd.CommandPath(), "", "--replace needs --save-as <name>")
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
			// The query runs first, so a filter set it refuses is never saved.
			matches, qerr := query.Query(ws, filters, today())
			if qerr != nil {
				return qerr
			}
			if cmd.Flags().Changed("save-as") {
				outcome, serr := savedquery.Save(ws, saveAs, savedquery.Definition(filters), replace)
				if serr != nil {
					return serr
				}
				Note(saveNote(outcome, saveAs))
			}
			return emitMatches(matches, format)
		})
	}
	root.AddCommand(cmd)
}

// emitMatches renders query rows: bare slugs under --format ids, records on
// the JSON gate, the table on a TTY. `query` and `run` share it, so a saved
// query answers exactly as the query it was saved from.
func emitMatches(matches []query.Match, format string) error {
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
	return Emit(records, format, func() {
		if len(rows) == 0 {
			fmt.Println("No entities match")
			return
		}
		printTable("query", []string{"id", "type", "title", "draft", "orphan", "stale"}, rows)
	})
}

// saveNote is the stderr line a save leaves, whatever the format: the rows on
// stdout stay exactly what `khub run <name>` prints.
func saveNote(outcome savedquery.Outcome, name string) string {
	switch outcome {
	case savedquery.Replaced:
		return fmt.Sprintf("replaced saved query '%s' in %s", name, savedquery.File)
	case savedquery.Unchanged:
		return fmt.Sprintf("saved query '%s' already in %s, unchanged", name, savedquery.File)
	default:
		return fmt.Sprintf("saved query '%s' in %s; replay it with `khub run %s`", name, savedquery.File, name)
	}
}
