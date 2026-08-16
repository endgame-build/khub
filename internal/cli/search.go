// search.go ports cli/search_cmd.py: full-text over title and body. Raw FTS5
// MATCH syntax passes through ("quoted phrases", OR, NEAR, prefix*).
package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/search"
)

func registerSearch(root *cobra.Command) {
	var typeName, format string
	var limit int
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
				hits, err := search.Search(ws, args[0], optString(cmd, "type", typeName), limit)
				if err != nil {
					return err
				}
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
					record.Set("score", h.Score)
					record.Set("snippet", h.Snippet)
					record.Set("path", h.Path)
					if h.Locator != "" {
						record.Set("locator", h.Locator)
					}
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
	cmd.Flags().StringVar(&format, "format", "text", "text (Rich table on a TTY), json, or ids.")
	root.AddCommand(cmd)
}
