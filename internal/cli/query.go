// query.go ports cli/query_cmd.py: filter entities by frontmatter and derived
// edges. Like add/edit it accepts arbitrary --<field> value filters, so it
// parses its own flags; the declared ones (--type, --tag, --has, --missing,
// --count, --in-count, --out-count, --orphan, --stale, --draft/--active,
// --limit, --format) still bind.

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
		orphan, stale, draft, active        bool
		limit                               int
		counts, inCounts, outCounts         []string
	)
	cmd := newCmd("query", "Filter entities: khub query --type opportunity --stage prospect --format json.",
		"", nil)
	cmd.DisableFlagParsing = true
	cmd.Flags().StringVar(&typeName, "type", "", "Restrict to one entity type.")
	cmd.Flags().StringVar(&tag, "tag", "", "Keep entities carrying this tag.")
	cmd.Flags().StringVar(&has, "has", "", "Keep entities with a resolvable edge for the predicate.")
	cmd.Flags().StringVar(&missing, "missing", "", "Keep entities lacking a resolvable edge (gap finder).")
	cmd.Flags().StringArrayVar(&counts, "count", nil, "Bound the edge count for a relation or inverse: 'depends_on>=2' (repeatable).")
	cmd.Flags().StringArrayVar(&inCounts, "in-count", nil, "Bound the inbound edge count: '>=5', or 0 for none (repeatable).")
	cmd.Flags().StringArrayVar(&outCounts, "out-count", nil, "Bound the outbound edge count: 0 finds leaves (repeatable).")
	cmd.Flags().BoolVar(&orphan, "orphan", false, "Keep only orphan (edge-less) entities.")
	cmd.Flags().BoolVar(&stale, "stale", false, "Keep only stale entities.")
	cmd.Flags().BoolVar(&draft, "draft", false, "Isolate drafts.")
	cmd.Flags().BoolVar(&active, "active", false, "Exclude drafts.")
	cmd.Flags().IntVar(&limit, "limit", 0, "Cap the returned set.")
	cmd.Flags().StringVar(&format, "format", "text", "text (Rich table on a TTY), json, or ids.")
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
			countFilters, inFilters, outFilters, cerr := parseCounts(cmd, counts, inCounts, outCounts)
			if cerr != nil {
				return cerr
			}
			ws, rerr := resolveRoot()
			if rerr != nil {
				return rerr
			}
			matches, qerr := query.Query(ws, query.Filters{
				Type:       optString(cmd, "type", typeName),
				Fields:     fields,
				Tag:        optString(cmd, "tag", tag),
				Has:        optString(cmd, "has", has),
				Missing:    optString(cmd, "missing", missing),
				Counts:     countFilters,
				InCount:    inFilters,
				OutCount:   outFilters,
				Orphan:     orphan,
				Stale:      stale,
				DraftOnly:  draft,
				ActiveOnly: active,
				Limit:      optInt(cmd, "limit", limit),
			}, today())
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
			return Emit(records, format, func() {
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

// parseCounts reads the cardinality flags. A malformed expression is a usage
// error (exit 2) naming the flag and the accepted shape. Every example in it is
// shell-quoted: an agent copying `--count depends_on>=2` unquoted would have
// the shell write a file named `=2`.
func parseCounts(cmd *cobra.Command, counts, inCounts, outCounts []string) (
	[]query.Count, []query.Comparison, []query.Comparison, error) {
	const opsLine = " The op is one of =, !=, <, <=, >, >=; quote a bound that holds < or >."
	var parsed []query.Count
	for _, expr := range counts {
		c, ok := query.ParseCount(expr)
		if !ok {
			return nil, nil, nil, clickUsageErr(cmd.CommandPath(), "", fmt.Sprintf(
				"Invalid value for '--count': '%s' is not <relation><op><n>, as in depends_on=0 or "+
					"'depends_on>=2'.", expr)+opsLine+shadowedField(cmd, "count"))
		}
		parsed = append(parsed, c)
	}
	comparisons := func(flag string, exprs []string) ([]query.Comparison, error) {
		var out []query.Comparison
		for _, expr := range exprs {
			c, ok := query.ParseComparison(expr)
			if !ok {
				return nil, clickUsageErr(cmd.CommandPath(), "", fmt.Sprintf(
					"Invalid value for '--%s': '%s' is not [<op>]<n>, as in 0 or '>=5'.", flag, expr)+
					opsLine+shadowedField(cmd, flag))
			}
			out = append(out, c)
		}
		return out, nil
	}
	in, err := comparisons("in-count", inCounts)
	if err != nil {
		return nil, nil, nil, err
	}
	out, err := comparisons("out-count", outCounts)
	if err != nil {
		return nil, nil, nil, err
	}
	return parsed, in, out, nil
}

// shadowedField names the clash when the schema declares a field the flag's
// own name would otherwise filter (`--count 5` meant for an attribute `count`).
// A dashed flag leaves the underscore spelling free, so `--in_count 5` still
// filters a field in_count; `count` has no second spelling. Best effort: with
// no workspace or schema to read there is nothing to name.
func shadowedField(cmd *cobra.Command, flag string) string {
	field := fieldKey(flag)
	ws, err := resolveRoot()
	if err != nil || !query.DeclaresField(ws, field) {
		return ""
	}
	if field != flag {
		return fmt.Sprintf(" To filter the field '%s', write --%s.", field, field)
	}
	return fmt.Sprintf(" --%s is the edge-count filter, so query cannot filter the field '%s'.", flag, field)
}
