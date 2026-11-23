// query.go ports cli/query_cmd.py: filter entities by frontmatter and derived
// edges. Like add/edit it accepts arbitrary --<field> value filters, so it
// parses its own flags; the declared ones (--type, --tag, --has, --missing,
// --count, --in-count, --out-count, --orphan, --stale, --draft/--active,
// --under/--via, --trust, --limit, --format, --receipt, --save-as, --replace)
// still bind. `khub run <name>` replays a saved filter set through the same
// query and the same output.

package cli

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/endgame-build/khub/internal/clock"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/query"
	"github.com/endgame-build/khub/internal/savedquery"
)

func registerQuery(root *cobra.Command) {
	var (
		typeName, tag, has, missing, format, trustTier string
		under, via, saveAs                             string
		orphan, stale, draft, active, rcpt, replace    bool
		limit                                          int
		counts, inCounts, outCounts                    []string
	)
	cmd := newCmd("query", "Filter entities: khub query --type opportunity --stage prospect --format json.",
		"", nil)
	cmd.DisableFlagParsing = true
	cmd.Flags().StringVar(&typeName, "type", "", "Restrict to one entity type.")
	cmd.Flags().StringVar(&tag, "tag", "", "Keep entities carrying this tag.")
	cmd.Flags().StringVar(&has, "has", "", "Keep entities with a resolvable edge for the predicate.")
	cmd.Flags().StringVar(&missing, "missing", "", "Keep entities lacking a resolvable edge (gap finder).")
	cmd.Flags().StringVar(&under, "under", "", "Keep entities that reach this entity over --via, any number of hops.")
	cmd.Flags().StringVar(&via, "via", "", "The relation --under follows up to its parent, or its declared inverse.")
	cmd.Flags().StringArrayVar(&counts, "count", nil, "Bound the edge count for a relation or inverse: 'depends_on>=2' (repeatable).")
	cmd.Flags().StringArrayVar(&inCounts, "in-count", nil, "Bound the inbound edge count: '>=5', or 0 for none (repeatable).")
	cmd.Flags().StringArrayVar(&outCounts, "out-count", nil, "Bound the outbound edge count: 0 finds leaves (repeatable).")
	cmd.Flags().BoolVar(&orphan, "orphan", false, "Keep only orphan (edge-less) entities.")
	cmd.Flags().BoolVar(&stale, "stale", false, "Keep only stale entities.")
	cmd.Flags().BoolVar(&draft, "draft", false, "Isolate drafts.")
	cmd.Flags().BoolVar(&active, "active", false, "Exclude drafts.")
	cmd.Flags().IntVar(&limit, "limit", 0, "Cap the returned set.")
	cmd.Flags().StringVar(&trustTier, "trust", "", "Keep entities at this review tier: unverified, machine, or human.")
	cmd.Flags().StringVar(&format, "format", "text", "text (Rich table on a TTY), json, or ids.")
	cmd.Flags().BoolVar(&rcpt, "receipt", false, "Wrap JSON output with the applied filters, matched and returned counts, and truncation.")
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
			underID, viaPred := optString(cmd, "under", under), optString(cmd, "via", via)
			if underID != nil && viaPred == nil {
				return badParameter(cmd.CommandPath(), "", "--under needs --via <predicate>, the relation an entity follows up to its parent")
			}
			if viaPred != nil && underID == nil {
				return badParameter(cmd.CommandPath(), "", "--via needs --under <id>, the entity to scope to")
			}
			countFilters, inFilters, outFilters, cerr := parseCounts(cmd, counts, inCounts, outCounts)
			if cerr != nil {
				return cerr
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
				Trust:      optString(cmd, "trust", trustTier),
				Counts:     countFilters,
				InCount:    inFilters,
				OutCount:   outFilters,
				Orphan:     orphan,
				Stale:      stale,
				DraftOnly:  draft,
				ActiveOnly: active,
				Limit:      optInt(cmd, "limit", limit),
				Under:      underID,
				Via:        viaPred,
			}
			// The query runs first, so a filter set it refuses is never saved.
			matches, matched, qerr := query.QueryCounted(ws, filters, clock.Now())
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
			return emitMatches(filters, matches, matched, rcpt, format)
		})
	}
	root.AddCommand(cmd)
}

// emitMatches renders query rows: bare slugs under --format ids, records (in
// the --receipt envelope when asked) on the JSON gate, the table on a TTY.
// `query` and `run` share it, so a saved query answers exactly as the query
// it was saved from.
func emitMatches(filters query.Filters, matches []query.Match, matched int, rcpt bool, format string) error {
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
		record.Set("trust", m.Trust)
		records = append(records, record)
		rows = append(rows, []string{
			m.Type + "/" + m.Slug, m.Type, m.Title,
			pyBool(m.Draft), pyBool(m.Orphan), pyBool(m.Stale), m.Trust,
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
		printTable("query", []string{"id", "type", "title", "draft", "orphan", "stale", "trust"}, rows)
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
	applied.Set("under", orNull(f.Under))
	applied.Set("via", orNull(f.Via))
	counts := []any{}
	for _, c := range f.Counts {
		counts = append(counts, c.Name+c.Cmp.Op+strconv.Itoa(c.Cmp.N))
	}
	applied.Set("count", counts)
	applied.Set("in_count", bounds(f.InCount))
	applied.Set("out_count", bounds(f.OutCount))
	applied.Set("orphan", f.Orphan)
	applied.Set("stale", f.Stale)
	applied.Set("draft", f.DraftOnly)
	applied.Set("active", f.ActiveOnly)
	applied.Set("trust", orNull(f.Trust))
	applied.Set("limit", orNull(f.Limit))
	return applied
}

// bounds echoes degree comparisons as `<op><n>`, a bare n as `=n`.
func bounds(cs []query.Comparison) []any {
	out := []any{}
	for _, c := range cs {
		out = append(out, c.Op+strconv.Itoa(c.N))
	}
	return out
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
