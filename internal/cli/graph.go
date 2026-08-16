// graph.go ports cli/graph_cmd.py: the three read-only walks. neighbors and
// history render a table as their human view; impact's human view is a
// depth-marked text tree, and it keeps its own output gate so `--format tree`
// forces the tree even on a pipe.
package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/graph"
	"github.com/endgame-build/khub/internal/omap"
)

func registerNeighbors(root *cobra.Command) {
	var predicate, format string
	var inbound, outbound bool
	var depth int
	cmd := newCmd("neighbors {ID}", "Walk one-hop neighbors: khub neighbors initech-pov [--predicate client --in].",
		"{ID}", func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return missingArgument(cmd.CommandPath(), "{ID}", "ID")
			}
			return Guard(format, func() error {
				direction := graph.DirectionBoth
				if inbound {
					direction = graph.DirectionIn
				} else if outbound {
					direction = graph.DirectionOut
				}
				ws, err := resolveRoot()
				if err != nil {
					return err
				}
				result, err := graph.WalkNeighbors(ws, args[0],
					optString(cmd, "predicate", predicate), direction, depth)
				if err != nil {
					return err
				}
				records := make([]any, 0, len(result))
				rows := [][]string{}
				for _, n := range result {
					record := omap.New()
					record.Set("id", n.Type+"/"+n.Slug)
					record.Set("type", n.Type)
					record.Set("slug", n.Slug)
					record.Set("predicate", n.Predicate)
					record.Set("direction", n.Direction)
					record.Set("derived", n.Derived)
					record.Set("depth", n.Depth)
					records = append(records, record)
					rows = append(rows, []string{
						n.Type + "/" + n.Slug, n.Predicate, n.Direction, fmt.Sprint(n.Depth),
					})
				}
				return Emit(records, format, func() {
					if len(rows) == 0 {
						fmt.Println("No neighbors")
						return
					}
					printTable("neighbors", []string{"id", "predicate", "direction", "depth"}, rows)
				})
			})
		})
	cmd.Args = clickArity(1)
	cmd.Flags().StringVar(&predicate, "predicate", "", "Restrict adjacency to one predicate.")
	cmd.Flags().BoolVar(&inbound, "in", false, "Inbound edges only (incl. derived inverses).")
	cmd.Flags().BoolVar(&outbound, "out", false, "Outbound (stored) edges only.")
	cmd.Flags().IntVar(&depth, "depth", 1, "Bounded multi-hop adjacency over all predicates.")
	cmd.Flags().StringVar(&format, "format", "text", "text (Rich table on a TTY) or json.")
	root.AddCommand(cmd)
}

func registerImpact(root *cobra.Command) {
	var predicate, format string
	var reverse bool
	cmd := newCmd("impact {ID}", "Compute blast radius: khub impact node-a [--reverse] [--predicate <p>].",
		"{ID}", func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return missingArgument(cmd.CommandPath(), "{ID}", "ID")
			}
			return Guard(format, func() error {
				ws, err := resolveRoot()
				if err != nil {
					return err
				}
				result, err := graph.WalkImpact(ws, args[0], predicate, reverse)
				if err != nil {
					return err
				}
				// JSON on an explicit request or any pipe (agent contract); the depth
				// tree is the human view on a TTY, and `--format tree` forces it even
				// on a pipe.
				if format == "json" || (format != "tree" && !IsTTY()) {
					records := make([]any, 0, len(result))
					for _, n := range result {
						record := omap.New()
						record.Set("id", n.Type+"/"+n.Slug)
						record.Set("type", n.Type)
						record.Set("slug", n.Slug)
						record.Set("depth", n.Depth)
						records = append(records, record)
					}
					doc, derr := canon.EncodeCLI(records)
					if derr != nil {
						return derr
					}
					_, werr := fmt.Fprintln(os.Stdout, doc)
					return werr
				}
				if len(result) <= 1 { // only the source — nothing reachable on the predicate
					fmt.Println("No downstream impact")
					return nil
				}
				for _, n := range result {
					fmt.Println(strings.Repeat("  ", n.Depth) + n.Type + "/" + n.Slug)
				}
				return nil
			})
		})
	cmd.Args = clickArity(1)
	cmd.Flags().StringVar(&predicate, "predicate", "depends_on", "The edge to walk the closure over.")
	cmd.Flags().BoolVar(&reverse, "reverse", false, "Walk ancestors (what reaches this node).")
	cmd.Flags().StringVar(&format, "format", "text", "tree (depth-marked, the TTY default) or json.")
	root.AddCommand(cmd)
}

func registerHistory(root *cobra.Command) {
	var predicate, format string
	var limit int
	cmd := newCmd("history {ID}", "Trace supersession lineage: khub history decision-0012 [--limit 3].",
		"{ID}", func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return missingArgument(cmd.CommandPath(), "{ID}", "ID")
			}
			return Guard(format, func() error {
				ws, err := resolveRoot()
				if err != nil {
					return err
				}
				result, err := graph.WalkHistory(ws, args[0], predicate, optInt(cmd, "limit", limit))
				if err != nil {
					return err
				}
				records := make([]any, 0, len(result))
				rows := [][]string{}
				for _, link := range result {
					record := omap.New()
					record.Set("id", link.Type+"/"+link.Slug)
					record.Set("type", link.Type)
					record.Set("slug", link.Slug)
					record.Set("superseded_by", nullable(link.SupersededBy))
					records = append(records, record)
					by := link.SupersededBy
					if by == "" {
						by = "—"
					}
					rows = append(rows, []string{link.Type + "/" + link.Slug, by})
				}
				return Emit(records, format, func() {
					if len(rows) <= 1 { // supersedes nothing — only the source record
						fmt.Println("No supersession history")
						return
					}
					printTable("history", []string{"id", "superseded_by"}, rows)
				})
			})
		})
	cmd.Args = clickArity(1)
	cmd.Flags().StringVar(&predicate, "predicate", "supersedes", "The self-referential edge to follow.")
	cmd.Flags().IntVar(&limit, "limit", 0, "Cap to the N most recent links.")
	cmd.Flags().StringVar(&format, "format", "text", "text (Rich table on a TTY) or json.")
	root.AddCommand(cmd)
}
