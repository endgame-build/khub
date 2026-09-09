// entity.go ports cli/entity_cmd.py: the authoring verbs (add/get/edit/link/
// unlink/remove) as thin adapters over internal/entity.
//
// add, edit and query take arbitrary per-type --<field> value pairs, so they
// parse their own flags (see splitDynamic) instead of letting cobra reject the
// unknown ones. A missing required argument is a usage error (exit 2) with a
// hand-written line — these commands opened a wizard for it until 0.9.0; khub
// no longer prompts anywhere.

package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/endgame-build/khub/internal/entity"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/omap"
)

func registerAdd(root *cobra.Command) {
	var (
		id, bodyText, bodyFile, format string
		draft, strict, noTemplate      bool
	)
	cmd := newCmd("add [TYPE]",
		"Create an entity: khub add opportunity --client initech --owner noor --stage prospect.",
		"[TYPE]", nil)
	// The second docstring paragraph, which Typer prints under the usage line.
	cmd.Long = cmd.Short + "\n\nA missing TYPE is a usage error; every field is a flag."
	cmd.DisableFlagParsing = true
	cmd.Flags().StringVar(&id, "id", "", "Explicit slug (slugified). Without it the id is minted from name, then title, in the type's scheme (`khub schema show TYPE` names it); a type with neither refuses, so pass --id for a type you do not title.")
	cmd.Flags().BoolVar(&draft, "draft", false, "Mark the entity unpublished (default: active).")
	cmd.Flags().BoolVar(&strict, "strict", false, "Reject fields the schema does not declare.")
	cmd.Flags().StringVar(&bodyText, "body", "", "Body prose as a string.")
	cmd.Flags().StringVar(&bodyFile, "body-file", "", "Read the body from a file ('-' for stdin).")
	cmd.Flags().BoolVar(&noTemplate, "no-template", false,
		"Start with an empty body. Refused on a type whose template has a required heading, "+
			"since the result would fail validate; use --body to supply your own sections.")
	cmd.Flags().StringVar(&format, "format", "text", "text or json (emits the written record).")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		positional, extra, err := splitDynamic(cmd, args, 1)
		if err != nil {
			return clickUsageErr(cmd.CommandPath(), "[TYPE]", clickFlagMessage(cmd, err))
		}
		if helpRequested(cmd) {
			return cmd.Help()
		}
		return Guard(format, func() error {
			ws, rerr := resolveRoot()
			if rerr != nil {
				return rerr
			}
			fields, ferr := fieldsOrUsage(cmd, extra, "[TYPE]")
			if ferr != nil {
				return ferr
			}
			body, berr := pickBody(cmd, bodyText, bodyFile)
			if berr != nil {
				if errors.Is(berr, errBothBodies) {
					return badParameter(cmd.CommandPath(), "[TYPE]", berr.Error())
				}
				return berr
			}
			text := ""
			if body != nil {
				text = *body
			}
			typeName := arg(positional, 0)
			if typeName == "" {
				fmt.Fprintln(os.Stderr, "Missing argument 'TYPE' (e.g. project).")
				return &ExitError{Code: 2}
			}
			result, cerr := entity.Create(ws, typeName, entity.CreateOpts{
				Fields: fields, ID: explicitID(cmd, id), Draft: draft, Body: text,
				Strict: strict, UseTemplate: !noTemplate,
			})
			if cerr != nil {
				return emptyIDError(cmd, id, cerr)
			}
			return Emit(refRecord(ws, result.Type, result.Slug, result.Path, result.Draft, result.Locator),
				format, func() {
					fmt.Println(relPath(ws, result.Path))
					state := "active"
					if result.Draft {
						state = "draft"
					}
					fmt.Printf("Created %s '%s' (%s)\n", result.Type, result.Slug, state)
				})
		})
	}
	root.AddCommand(cmd)
}

func registerGet(root *cobra.Command) {
	var format string
	var edges bool
	cmd := newCmd("get [ID]...", "Read entities' frontmatter and body, optionally with derived edges.",
		"[ID]...", func(cmd *cobra.Command, args []string) error {
			return Guard(format, func() error {
				ws, err := resolveRoot()
				if err != nil {
					return err
				}
				if len(args) == 0 {
					fmt.Fprintln(os.Stderr, "Missing argument 'ID'.")
					return &ExitError{Code: 2}
				}
				if len(args) > 1 && format == "raw" {
					return errs.New("batch_raw", "Raw output requires exactly one ID")
				}
				views, err := entity.GetMany(ws, args, edges)
				if err != nil {
					return err
				}
				if format == "raw" {
					_, werr := fmt.Fprint(os.Stdout, views[0].Raw)
					return werr
				}
				if len(views) == 1 {
					return Emit(getRecord(ws, views[0]), format, func() { printEntityTable(views[0]) })
				}
				records := make([]any, len(views))
				for i, view := range views {
					records[i] = getRecord(ws, view)
				}
				return Emit(records, format, func() {
					for _, view := range views {
						fmt.Println(view.Type + "/" + view.Slug)
						printEntityTable(view)
					}
				})
			})
		})
	cmd.Flags().BoolVar(&edges, "edges", false, "Include stored and derived edges.")
	cmd.Flags().StringVar(&format, "format", "text", "json, table, raw, or text (Rich on a TTY).")
	root.AddCommand(cmd)
}

func registerEdit(root *cobra.Command) {
	var bodyText, bodyFile, format string
	var strict bool
	cmd := newCmd("edit [ID]",
		"Edit an entity: khub edit initech-deal stage proposal-sent  (or --field value).",
		"[ID]", nil)
	cmd.Long = cmd.Short + "\n\nA missing ID is a usage error; the field and value are positional or flags."
	cmd.DisableFlagParsing = true
	cmd.Flags().BoolVar(&strict, "strict", false, "Reject fields the schema does not declare.")
	cmd.Flags().StringVar(&bodyText, "body", "", "Replace the body with this string ('' clears it).")
	cmd.Flags().StringVar(&bodyFile, "body-file", "", "Replace the body from a file ('-' for stdin).")
	cmd.Flags().StringVar(&format, "format", "text", "text or json (emits the updated record).")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		positional, extra, err := splitDynamic(cmd, args, 1)
		if err != nil {
			return clickUsageErr(cmd.CommandPath(), "[ID]", clickFlagMessage(cmd, err))
		}
		if helpRequested(cmd) {
			return cmd.Help()
		}
		return Guard(format, func() error {
			ws, rerr := resolveRoot()
			if rerr != nil {
				return rerr
			}
			fields, ferr := fieldsOrUsage(cmd, extra, "[ID]")
			if ferr != nil {
				return ferr
			}
			body, berr := pickBody(cmd, bodyText, bodyFile)
			if berr != nil {
				if errors.Is(berr, errBothBodies) {
					return badParameter(cmd.CommandPath(), "[ID]", berr.Error())
				}
				return berr
			}
			id := arg(positional, 0)
			if id == "" {
				fmt.Fprintln(os.Stderr, "Missing argument 'ID'.")
				return &ExitError{Code: 2}
			}
			result, uerr := entity.Update(ws, id, entity.UpdateOpts{
				Fields: fields, Body: body, Strict: strict,
			})
			if uerr != nil {
				return uerr
			}
			return Emit(refRecord(ws, result.Type, result.Slug, result.Path, result.Draft, result.Locator),
				format, func() { fmt.Printf("Updated %s '%s'\n", result.Type, result.Slug) })
		})
	}
	root.AddCommand(cmd)
}

func registerLink(root *cobra.Command) {
	var format string
	cmd := newCmd("link [ID] [PREDICATE] [TARGET]", "Add a relation: khub link initech-pov partner northwind.",
		"[ID] [PREDICATE] [TARGET]", func(cmd *cobra.Command, args []string) error {
			return Guard(format, func() error {
				ws, err := resolveRoot()
				if err != nil {
					return err
				}
				if len(args) < 3 {
					fmt.Fprintln(os.Stderr, "Provide ID PREDICATE TARGET.")
					return &ExitError{Code: 2}
				}
				result, err := entity.Link(ws, args[0], args[1], args[2])
				if err != nil {
					return err
				}
				return Emit(edgeRecord(result), format, func() {
					// An unchanged edge already existed — idempotent success (exit 0).
					if !result.Changed {
						fmt.Println("Edge already present")
						return
					}
					fmt.Printf("Linked %s --%s--> %s\n", result.Slug, result.Predicate, result.Target)
				})
			})
		})
	cmd.Args = clickArity(3)
	cmd.Flags().StringVar(&format, "format", "text", "text or json (emits the edge record).")
	root.AddCommand(cmd)
}

func registerUnlink(root *cobra.Command) {
	var format string
	cmd := newCmd("unlink [ID] [PREDICATE] [TARGET]", "Remove a relation: khub unlink initech-pov partner northwind.",
		"[ID] [PREDICATE] [TARGET]", func(cmd *cobra.Command, args []string) error {
			return Guard(format, func() error {
				ws, err := resolveRoot()
				if err != nil {
					return err
				}
				if len(args) < 3 {
					fmt.Fprintln(os.Stderr, "Provide ID PREDICATE TARGET.")
					return &ExitError{Code: 2}
				}
				result, err := entity.Unlink(ws, args[0], args[1], args[2])
				if err != nil {
					return err
				}
				return Emit(edgeRecord(result), format, func() {
					// No such edge — idempotent no-op success (exit 0).
					if !result.Changed {
						fmt.Printf("No edge %s -> %s on %s\n", result.Predicate, result.Target, result.Slug)
						return
					}
					fmt.Printf("Unlinked %s --%s--> %s\n", result.Slug, result.Predicate, result.Target)
				})
			})
		})
	cmd.Args = clickArity(3)
	cmd.Flags().StringVar(&format, "format", "text", "text or json (emits the edge record).")
	root.AddCommand(cmd)
}

func registerRemove(root *cobra.Command) {
	var format string
	var force bool
	cmd := newCmd("remove [ID]", "Remove an entity, guarded by inbound edges: khub remove old-fragment [--force].",
		"[ID]", func(cmd *cobra.Command, args []string) error {
			return Guard(format, func() error {
				ws, err := resolveRoot()
				if err != nil {
					return err
				}
				if len(args) == 0 {
					fmt.Fprintln(os.Stderr, "Missing argument 'ID'.")
					return &ExitError{Code: 2}
				}
				result, err := entity.Delete(ws, args[0], force)
				if err != nil {
					return err
				}
				if result.Removed {
					record := omap.New()
					record.Set("id", result.Type+"/"+result.Slug)
					record.Set("type", result.Type)
					record.Set("slug", result.Slug)
					record.Set("removed", true)
					return Emit(record, format, func() {
						fmt.Printf("Removed %s '%s'\n", result.Type, result.Slug)
					})
				}
				refusal := errs.InboundEdgeRefusal(result.Type, result.Slug, len(result.Inbound))
				if WantJSON(format) {
					// A refusal is a failure: let Guard render the shared {"error": …}
					// envelope rather than a bespoke payload no other command emits.
					return refusal
				}
				fmt.Fprintln(os.Stderr, refusal.Message)
				for _, edge := range result.Inbound {
					fmt.Fprintf(os.Stderr, "  %s/%s --%s-->\n",
						edge.SourceType, edge.SourceSlug, edge.Predicate)
				}
				return &ExitError{Code: 2} // a refusal: nothing was removed
			})
		})
	cmd.Args = clickArity(1)
	cmd.Flags().BoolVar(&force, "force", false, "Delete despite inbound edges (leaves them dangling).")
	cmd.Flags().StringVar(&format, "format", "text", "text or json (emits the removed record).")
	root.AddCommand(cmd)
}

// emptyIDSentinel stands in for `--id ""`. Typer's Option(None) makes "an
// explicit empty --id" and "no --id at all" two different requests: the first
// is an invalid slug (or, on a singleton, an id that is not the type name), the
// second mints one. internal/entity spells "an id was given" as a non-empty
// string, so the CLI substitutes an all-symbol sentinel, which fails at exactly
// the gate Python's empty string does — after unknown_type, field validation
// and referential integrity, never before them — then restores ” in the
// message.
const emptyIDSentinel = "-"

// explicitID is Typer's Option(None) for --id.
func explicitID(cmd *cobra.Command, id string) string {
	if given := optString(cmd, "id", id); given != nil && *given == "" {
		return emptyIDSentinel
	}
	return id
}

// emptyIDError re-labels the sentinel's slug error as the one Python raises for
// an explicitly empty --id.
func emptyIDError(cmd *cobra.Command, id string, err error) error {
	if given := optString(cmd, "id", id); given == nil || *given != "" {
		return err
	}
	var located *errs.Located
	if errors.As(err, &located) && located.Code == "invalid_slug" {
		return errs.InvalidSlug("")
	}
	return err
}

// --- records / tables ---------------------------------------------------------

// refRecord is entity_cmd._ref_record: the type/slug reference add and edit
// emit. locator lands only on a collection row, so per-item records stay
// byte-identical.
func refRecord(ws, typeName, slug, path string, draft bool, locator string) *omap.Map {
	record := omap.New()
	record.Set("id", typeName+"/"+slug)
	record.Set("type", typeName)
	record.Set("slug", slug)
	record.Set("path", relPath(ws, path))
	record.Set("draft", draft)
	if locator != "" {
		record.Set("locator", locator)
	}
	return record
}

// edgeRecord is entity_cmd._edge_record. `changed` is the whole point: both
// verbs are idempotent and exit 0 either way, so prose alone left an agent
// unable to tell a created edge from one that was already there.
func edgeRecord(result *entity.LinkResult) *omap.Map {
	record := omap.New()
	record.Set("id", result.Type+"/"+result.Slug)
	record.Set("type", result.Type)
	record.Set("slug", result.Slug)
	record.Set("predicate", result.Predicate)
	record.Set("target", result.Target)
	record.Set("changed", result.Changed)
	return record
}

func getRecord(ws string, view *entity.EntityView) *omap.Map {
	record := omap.New()
	record.Set("id", view.Type+"/"+view.Slug)
	record.Set("type", view.Type)
	record.Set("slug", view.Slug)
	record.Set("path", relPath(ws, view.Path))
	record.Set("frontmatter", view.Meta)
	record.Set("body", view.Body)
	if view.Locator != "" {
		record.Set("locator", view.Locator)
	}
	if view.Edges != nil {
		edges := make([]any, 0, len(view.Edges))
		for _, e := range view.Edges {
			edge := omap.New()
			edge.Set("predicate", e.Predicate)
			edge.Set("target", e.Target)
			edge.Set("derived", e.Derived)
			edges = append(edges, edge)
		}
		record.Set("edges", edges)
	}
	return record
}

func printEntityTable(view *entity.EntityView) {
	rows := [][]string{}
	for _, key := range view.Meta.Keys() {
		value, _ := view.Meta.Get(key)
		rows = append(rows, []string{key, pyStr(value)})
	}
	for _, edge := range view.Edges {
		kind := "stored"
		if edge.Derived {
			kind = "derived"
		}
		rows = append(rows, []string{fmt.Sprintf("%s (%s)", edge.Predicate, kind), edge.Target})
	}
	if body := strings.TrimSpace(view.Body); body != "" {
		rows = append(rows, []string{"body", body})
	}
	printTable(view.Type+"/"+view.Slug, []string{"field", "value"}, rows)
}
