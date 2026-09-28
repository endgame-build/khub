// search.go ports cli/search_cmd.py: full-text over title and body. Raw FTS5
// MATCH syntax passes through ("quoted phrases", OR, NEAR, prefix*); --plain
// takes plain words instead.

package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/search"
)

func registerSearch(root *cobra.Command) {
	var typeName, format string
	var limit int
	var plain bool
	cmd := newCmd("search {text}", "Full-text search: khub search modernization --type transcript --format json.",
		"{text}", func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return missingArgument(cmd.CommandPath(), "{text}", "text")
			}
			return Guard(format, func() error {
				ws, err := resolveRoot()
				if err != nil {
					return err
				}
				res, err := search.Search(ws, args[0], search.Options{
					Type: optString(cmd, "type", typeName), Limit: limit, Plain: plain, Now: today()})
				if err != nil {
					return err
				}
				if note := searchNote(res, plain); note != "" {
					Note(note)
				}
				hits := res.Hits
				if format == "ids" { // bare slugs for piping — independent of TTY detection
					for _, h := range hits {
						fmt.Println(h.Slug)
					}
					return nil
				}
				records := make([]any, 0, len(hits))
				rows := [][]string{}
				for _, h := range hits {
					record := omap.New()
					record.Set("id", h.Type+"/"+h.Slug)
					record.Set("type", h.Type)
					record.Set("slug", h.Slug)
					record.Set("title", h.Title)
					record.Set("draft", h.Draft)
					record.Set("orphan", h.Orphan)
					record.Set("stale", h.Stale)
					record.Set("score", h.Score)
					if plain {
						record.Set("match", h.Match)
						record.Set("title_match", h.TitleMatch)
					}
					record.Set("snippet", h.Snippet)
					record.Set("path", h.Path)
					if h.Locator != "" {
						record.Set("locator", h.Locator)
					}
					edges := omap.New()
					edges.Set("out", edgeCounts(h.Out))
					edges.Set("in", edgeCounts(h.In))
					record.Set("edges", edges)
					records = append(records, record)
					rows = append(rows, []string{h.Type + "/" + h.Slug, h.Title, h.Snippet})
				}
				return Emit(records, format, func() {
					if len(rows) == 0 {
						fmt.Println("No entities match")
						return
					}
					printTable("search", []string{"id", "title", "snippet"}, rows)
				})
			})
		})
	cmd.Args = clickArity(1)
	cmd.Flags().StringVar(&typeName, "type", "", "Restrict to one entity type.")
	cmd.Flags().IntVar(&limit, "limit", 20, "Cap the returned set.")
	cmd.Flags().BoolVar(&plain, "plain", false, "Treat text as plain words: any may match, prefix-matched, punctuation literal.")
	cmd.Flags().StringVar(&format, "format", "text", "text (Rich table on a TTY), json, or ids.")
	root.AddCommand(cmd)
}

// searchNote is the advisory line for a result an agent should not take at
// face value: rows the limit dropped, no match at all, files the scan could
// not parse, or a plain query cut short. "" when there is nothing to say.
// One or two hits is often an exact answer, so only zero counts as thin;
// match and title_match judge a weak hit under --plain.
func searchNote(res search.Result, plain bool) string {
	var parts []string
	if dropped := res.Total - len(res.Hits); dropped > 0 {
		parts = append(parts, fmt.Sprintf("showing %d of %d hits; pass --limit %d for all",
			len(res.Hits), res.Total, res.Total))
	}
	if res.Total == 0 {
		thin := fmt.Sprintf("no hits in %d entities searched; for structure try "+
			"`khub query --type <type>`, `khub neighbors <id>` or `khub get <id>`", res.Searched)
		if !plain {
			thin += ", or rerun with --plain"
		}
		parts = append(parts, thin)
	}
	if res.Malformed > 0 {
		parts = append(parts, fmt.Sprintf("%d file(s) in the workspace could not be parsed; "+
			"run `khub validate`", res.Malformed))
	}
	if res.Capped {
		parts = append(parts, fmt.Sprintf("the query was cut to its first %d distinct words", search.MaxPlainTerms))
	}
	return strings.Join(parts, "; ")
}

// edgeCounts renders a hit's per-predicate edge counts as one JSON object.
func edgeCounts(counts []search.EdgeCount) *omap.Map {
	m := omap.New()
	for _, c := range counts {
		m.Set(c.Key, c.N)
	}
	return m
}
