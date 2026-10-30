// retrieve.go is the thin adapter over internal/retrieve: one-call bounded
// context assembly — seeds (a search or ids), their neighbourhood over the
// derived graph, and the caps that keep the answer inside a budget. The
// expansion grammar and every cap are parsed here and decided in the core.

package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/endgame-build/khub/internal/graph"
	"github.com/endgame-build/khub/internal/retrieve"
	"github.com/endgame-build/khub/internal/search"
)

func registerRetrieve(root *cobra.Command) {
	var (
		typeName, format            string
		ids, expand                 []string
		limit, depth, maxEnt, maxBy int
		plain, inbound, outbound    bool
		noBody                      bool
	)
	const argSpec = "[TEXT]"
	cmd := newCmd("retrieve [TEXT]",
		"Assemble context in one call: khub retrieve \"auth token\" --expand depends_on:2 --max-bytes 20000.",
		argSpec, func(cmd *cobra.Command, args []string) error {
			text := ""
			if len(args) > 0 {
				text = args[0]
			}
			if text != "" && len(ids) > 0 {
				return badParameter(cmd.CommandPath(), argSpec, "Pass search text or --id, not both")
			}
			if len(ids) > 0 && (cmd.Flags().Changed("type") || plain) {
				return badParameter(cmd.CommandPath(), argSpec, "--type and --plain shape a search; they do not apply to --id seeds")
			}
			if depth < 0 {
				return badParameter(cmd.CommandPath(), argSpec, "--depth must be 0 or more")
			}
			expansions, perr := parseExpand(expand, depth)
			if perr != nil {
				return badParameter(cmd.CommandPath(), argSpec, perr.Error())
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
				res, err := retrieve.Retrieve(ws, retrieve.Options{
					Text: text, Plain: plain, Type: optString(cmd, "type", typeName), IDs: ids,
					Expand: expansions, Direction: direction, Depth: depth,
					Limit: optInt(cmd, "limit", limit), MaxEntities: maxEnt, MaxBytes: maxBy,
					NoBody: noBody, Now: today(),
				})
				if err != nil {
					return err
				}
				if note := retrieveNote(res, plain); note != "" {
					Note(note)
				}
				return Emit(retrieve.Render(res), format, func() { printRetrieveTable(res) })
			})
		})
	cmd.Args = clickArity(1)
	cmd.Flags().StringArrayVar(&ids, "id", nil, "Seed by id instead of a search (repeatable).")
	cmd.Flags().StringVar(&typeName, "type", "", "Restrict the search to one entity type.")
	cmd.Flags().BoolVar(&plain, "plain", false, "Treat TEXT as plain words: any may match, prefix-matched, punctuation literal.")
	cmd.Flags().StringArrayVar(&expand, "expand", nil,
		"Follow this predicate, to --depth hops or its own (depends_on:2); repeatable. Default: every predicate.")
	cmd.Flags().BoolVar(&inbound, "in", false, "Inbound edges only (incl. derived inverses).")
	cmd.Flags().BoolVar(&outbound, "out", false, "Outbound (stored) edges only.")
	cmd.Flags().IntVar(&depth, "depth", 1, "Hops to expand from each seed; 0 returns the seeds alone.")
	cmd.Flags().IntVar(&limit, "limit", 0, "Cap the seeds before expansion (default: 20 for a search, every --id).")
	cmd.Flags().IntVar(&maxEnt, "max-entities", 0, "Cap the entities after expansion, periphery first; 0 is unbounded.")
	cmd.Flags().IntVar(&maxBy, "max-bytes", 0, "Cap the bytes of the JSON document, trimming the same way; 0 is unbounded.")
	cmd.Flags().BoolVar(&noBody, "no-body", false, "Leave each entity's body out.")
	cmd.Flags().StringVar(&format, "format", "text", "text (Rich table on a TTY) or json.")
	root.AddCommand(cmd)
}

// parseExpand reads the `<predicate>[:<depth>]` entries; one without a depth
// takes the default. The predicates are checked against the schema in the
// core, where the refusal can list the declared ones.
func parseExpand(entries []string, depth int) ([]retrieve.Expansion, error) {
	out := make([]retrieve.Expansion, 0, len(entries))
	for _, entry := range entries {
		predicate, suffix, hasDepth := strings.Cut(entry, ":")
		if predicate == "" {
			return nil, fmt.Errorf("--expand needs a predicate, got '%s'", entry)
		}
		d := depth
		if hasDepth {
			n, err := strconv.Atoi(suffix)
			if err != nil || n < 0 {
				return nil, fmt.Errorf("--expand %s: the depth must be a whole number, 0 or more", entry)
			}
			d = n
		}
		out = append(out, retrieve.Expansion{Predicate: predicate, Depth: d})
	}
	return out, nil
}

// retrieveNote is the advisory line for a result an agent should not take at
// face value: seeds the limit dropped, a search with no hit, files the scan
// could not parse, a plain query cut short, and entities the caps trimmed —
// above all a seed, which means the budget could not hold the evidence asked
// for. "" when there is nothing to say.
func retrieveNote(res *retrieve.Result, plain bool) string {
	var parts []string
	if s := res.Search; s != nil {
		if dropped := s.Total - len(s.Hits); dropped > 0 {
			parts = append(parts, fmt.Sprintf("seeding from %d of %d hits; pass --limit %d for all",
				len(s.Hits), s.Total, s.Total))
		}
		if s.Total == 0 {
			thin := fmt.Sprintf("no hits in %d entities searched; seed by --id <id>, "+
				"or for structure try `khub query --type <type>`", s.Searched)
			if !plain {
				thin += ", or rerun with --plain"
			}
			parts = append(parts, thin)
		}
		if s.Malformed > 0 {
			parts = append(parts, fmt.Sprintf("%d file(s) in the workspace could not be parsed; "+
				"run `khub validate`", s.Malformed))
		}
		if s.Capped {
			parts = append(parts, fmt.Sprintf("the query was cut to its first %d distinct words", search.MaxPlainTerms))
		}
	}
	o := res.Omitted
	if res.Search == nil && o.Limit > 0 {
		parts = append(parts, fmt.Sprintf("%d of %d ids dropped by --limit", o.Limit, res.Seeds+o.Limit))
	}
	if o.MaxEntities > 0 {
		parts = append(parts, fmt.Sprintf("%d entities trimmed by --max-entities", o.MaxEntities))
	}
	if o.MaxBytes > 0 {
		parts = append(parts, fmt.Sprintf("%d entities trimmed by --max-bytes", o.MaxBytes))
	}
	if o.Insufficient {
		parts = append(parts, "the caps could not hold every seed; raise --max-entities or --max-bytes, "+
			"lower --limit, or pass --no-body")
	}
	return strings.Join(parts, "; ")
}

// printRetrieveTable is the human view: one row per entity with how it got
// here, then what the caps left out.
func printRetrieveTable(res *retrieve.Result) {
	if len(res.Records) == 0 {
		fmt.Println("No entities retrieved")
	} else {
		rows := [][]string{}
		for _, r := range res.Records {
			via := "seed"
			if r.Reason.Kind == retrieve.ReasonExpanded {
				via = r.Reason.Direction + " " + r.Reason.Predicate + " from " + r.Reason.From
			}
			rows = append(rows, []string{r.Node.ID(), fmt.Sprint(r.Reason.Depth), via, r.Title})
		}
		printTable("retrieve", []string{"id", "depth", "via", "title"}, rows)
	}
	o := res.Omitted
	if o.Truncated() {
		fmt.Printf("Omitted: %d by --limit, %d by --max-entities, %d by --max-bytes\n",
			o.Limit, o.MaxEntities, o.MaxBytes)
	}
	if o.Insufficient {
		fmt.Println("Insufficient: the caps could not hold every seed")
	}
}
