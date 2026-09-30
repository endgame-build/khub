// schema.go ports cli/schema_cmd.py: the four introspection views over the
// active schema. Zero per-type knowledge — every row derives from the resolved
// contract at runtime.

package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/workspace"
)

// formatHelp is the one --format help string the schema sub-app shares; Typer
// declares it once as a module-level Option and reuses it per subcommand.
const formatHelp = "text (Rich table on a TTY) or json."

func registerSchema(root *cobra.Command) {
	// Each subcommand declares its OWN --format, exactly as schema_cmd.py binds
	// FormatOpt per command. A single shared (persistent) flag would let
	// `khub schema --format json types` push the group's choice into `types`,
	// which Click never does.
	var groupFormat, typesFormat, showFormat, edgesFormat, baseFormat string
	schemaCmd := newCmd("schema", "Introspect the active schema.", "COMMAND [ARGS]...",
		func(cmd *cobra.Command, args []string) error {
			return Guard(groupFormat, func() error { return schemaFull(groupFormat) })
		})
	// A group's leftover token is an unknown subcommand, not a stray argument.
	schemaCmd.Args = clickNoSuchCommand
	schemaCmd.Flags().StringVar(&groupFormat, "format", "text", formatHelp)

	typesCmd := newCmd("types", "List the declared type names.", "",
		func(cmd *cobra.Command, args []string) error {
			return Guard(typesFormat, func() error { return schemaTypes(typesFormat) })
		})
	typesCmd.Args = clickArity(0)
	typesCmd.Flags().StringVar(&typesFormat, "format", "text", formatHelp)

	showCmd := newCmd("show", "Detail one type: fields, enums, required flags, relations, layout.",
		"{type}", func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				// Typer declares TYPE with `...`, so Click renders the full boxed
				// usage error rather than a bare line.
				return missingArgument(cmd.CommandPath(), "{type}", "type")
			}
			return Guard(showFormat, func() error { return schemaShow(args[0], showFormat) })
		})
	showCmd.Args = clickArity(1)
	showCmd.Flags().StringVar(&showFormat, "format", "text", formatHelp)

	baseCmd := newCmd("base", "Show the effective base block every type inherits.", "",
		func(cmd *cobra.Command, args []string) error {
			return Guard(baseFormat, func() error { return schemaBase(baseFormat) })
		})
	baseCmd.Args = clickArity(0)
	baseCmd.Flags().StringVar(&baseFormat, "format", "text", formatHelp)

	edgesCmd := newCmd("edges", "List the relation vocabulary by predicate.", "",
		func(cmd *cobra.Command, args []string) error {
			return Guard(edgesFormat, func() error { return schemaEdges(edgesFormat) })
		})
	edgesCmd.Args = clickArity(0)
	edgesCmd.Flags().StringVar(&edgesFormat, "format", "text", formatHelp)

	var snapshotFormat, diffFormat string
	snapshotCmd := newCmd("snapshot",
		"Record the current resolved schema as the baseline in .khub/schema.applied.yaml.", "",
		func(cmd *cobra.Command, args []string) error {
			return Guard(snapshotFormat, func() error { return schemaSnapshot(snapshotFormat) })
		})
	snapshotCmd.Args = clickArity(0)
	snapshotCmd.Flags().StringVar(&snapshotFormat, "format", "text", formatHelp)

	diffCmd := newCmd("diff", "List schema changes since the last `khub schema snapshot`.", "",
		func(cmd *cobra.Command, args []string) error {
			return Guard(diffFormat, func() error { return schemaDiff(diffFormat) })
		})
	diffCmd.Args = clickArity(0)
	diffCmd.Flags().StringVar(&diffFormat, "format", "text", formatHelp)

	schemaCmd.AddCommand(typesCmd, showCmd, edgesCmd, baseCmd, snapshotCmd, diffCmd)
	root.AddCommand(schemaCmd)
}

func schemaSnapshot(format string) error {
	root, err := resolveRoot()
	if err != nil {
		return err
	}
	res, err := workspace.WriteSnapshot(root)
	if err != nil {
		return err
	}
	payload := omap.New()
	payload.Set("path", res.Path)
	payload.Set("types", res.Types)
	return Emit(payload, format, func() {
		fmt.Printf("Recorded the schema snapshot (%d types) at %s\n", res.Types, res.Path)
	})
}

// schemaDiff reports the changes since the snapshot. Pending changes are a
// report, not a failed gate: it exits 0 either way and `pending` says which.
func schemaDiff(format string) error {
	root, err := resolveRoot()
	if err != nil {
		return err
	}
	changes, err := workspace.DiffSnapshot(root)
	if err != nil {
		return err
	}
	payload := omap.New()
	payload.Set("pending", len(changes) > 0)
	payload.Set("changes", changes)
	return Emit(payload, format, func() {
		if len(changes) == 0 {
			fmt.Println("No pending schema changes")
			return
		}
		for _, c := range changes {
			cm := c.(*omap.Map)
			op, _ := cm.Get("op")
			path, _ := cm.Get("path")
			from, _ := cm.Get("from")
			to, _ := cm.Get("to")
			switch op {
			case "added":
				fmt.Printf("+ %v\n", path)
			case "removed":
				fmt.Printf("- %v\n", path)
			default:
				fmt.Printf("~ %v: %s → %s\n", path, diffValue(from), diffValue(to))
			}
		}
	})
}

func loadResolved() (string, *schema.ResolvedSchema, error) {
	root, err := resolveRoot()
	if err != nil {
		return "", nil, err
	}
	resolved, err := introspect.LoadSchema(root)
	return root, resolved, err
}

func schemaFull(format string) error {
	root, resolved, err := loadResolved()
	if err != nil {
		return err
	}
	prov, err := workspace.Provenance(root)
	if err != nil {
		return err
	}
	view := introspect.SchemaView(resolved, prov)
	return Emit(view, format, func() {
		preset, _ := prov.Get("preset")
		ver, _ := prov.Get("version")
		rows := [][]string{}
		types, _ := view.Get("types")
		for _, t := range types.([]any) {
			tv := t.(*omap.Map)
			name, _ := tv.Get("name")
			fields, _ := tv.Get("fields")
			rels, _ := tv.Get("relations")
			layout, _ := tv.Get("layout")
			rows = append(rows, []string{
				fmt.Sprint(name),
				fmt.Sprint(len(fields.([]any))),
				fmt.Sprint(len(rels.([]any))),
				fmt.Sprint(layout),
			})
		}
		printTable(fmt.Sprintf("schema: %v@%v", preset, ver),
			[]string{"type", "fields", "relations", "layout"}, rows)
	})
}

// schemaBase prints the effective base block. Since the ontology/policy/
// storage split the base is embedded in the binary and no workspace file
// carries (or may declare) it, so this is the readable surface for it.
func schemaBase(format string) error {
	_, resolved, err := loadResolved()
	if err != nil {
		return err
	}
	view := introspect.BaseView(resolved)
	return Emit(view, format, func() {
		rows := appendRows([][]string{}, view, "fields", fieldRow)
		printTable("base fields", []string{"field", "type", "required", "enum", "notes"}, rows)
		relRows := appendRows([][]string{}, view, "relations", relationRow)
		printTable("base relations", []string{"predicate", "to", "required", "kind", "notes"}, relRows)
	})
}

func schemaTypes(format string) error {
	_, resolved, err := loadResolved()
	if err != nil {
		return err
	}
	names := introspect.TypesList(resolved)
	payload := []any{}
	rows := [][]string{}
	for _, n := range names {
		payload = append(payload, n)
		rows = append(rows, []string{n})
	}
	return Emit(payload, format, func() { printTable("types", []string{"type"}, rows) })
}

func schemaShow(name, format string) error {
	root, resolved, err := loadResolved()
	if err != nil {
		return err
	}
	preset := "the"
	if prov, perr := workspace.Provenance(root); perr == nil {
		if p, ok := prov.Get("preset"); ok {
			if s, isStr := p.(string); isStr && s != "" {
				preset = s
			}
		}
	}
	view, err := introspect.TypeView(resolved, name, preset)
	if err != nil {
		return err
	}
	return Emit(view, format, func() {
		layout, _ := view.Get("layout")
		rows := appendRows([][]string{}, view, "fields", fieldRow)
		rows = appendRows(rows, view, "relations", relationRow)
		// A minting type shows the id it will mint beside its layout, so the
		// title and the `bad_id` finding name the same shape.
		title := fmt.Sprintf("%s (%v)", name, layout)
		if shape, _ := view.Get("id_shape"); shape != nil {
			title = fmt.Sprintf("%s (%v · ids %v)", name, layout, shape)
		}
		printTable(title, []string{"field", "type", "required", "enum", "notes"}, rows)
	})
}

// appendRows renders one row per element of the view's `key` list. The two
// schema tables differ in where the rows land and under which header — `base`
// builds two tables, `show` one — never in how a row is built, so this is the
// whole of what they share (fieldRow and relationRow were already common).
func appendRows(dst [][]string, view *omap.Map, key string, row func(*omap.Map) []string) [][]string {
	v, _ := view.Get(key)
	for _, e := range v.([]any) {
		dst = append(dst, row(e.(*omap.Map)))
	}
	return dst
}

func fieldRow(fv *omap.Map) []string {
	name, _ := fv.Get("name")
	typ, _ := fv.Get("type")
	req, _ := fv.Get("required")
	check := ""
	if b, _ := req.(bool); b {
		check = "✓"
	}
	enumTxt := ""
	if e, _ := fv.Get("enum"); e != nil {
		var parts []string
		for _, x := range e.([]any) {
			parts = append(parts, fmt.Sprint(x))
		}
		enumTxt = strings.Join(parts, ", ")
	}
	var notes []string
	if p, _ := fv.Get("pattern"); p != nil {
		notes = append(notes, fmt.Sprintf("pattern %v", p))
	}
	if d, _ := fv.Get("default"); d != nil {
		notes = append(notes, fmt.Sprintf("default %v", pyValue(d)))
	}
	return []string{fmt.Sprint(name), fmt.Sprint(typ), check, enumTxt, strings.Join(notes, "; ")}
}

func relationRow(rv *omap.Map) []string {
	pred, _ := rv.Get("predicate")
	to, _ := rv.Get("to")
	var targets []string
	for _, t := range to.([]any) {
		targets = append(targets, fmt.Sprint(t))
	}
	kind, _ := rv.Get("kind")
	var notes []string
	if inv, _ := rv.Get("inverse"); inv != nil {
		notes = append(notes, fmt.Sprintf("inverse %v", inv))
	}
	if ac, _ := rv.Get("acyclic"); ac == true {
		notes = append(notes, "acyclic")
	}
	req, _ := rv.Get("required")
	return []string{fmt.Sprint(pred), "→ " + strings.Join(targets, ", "), checkMark(req),
		fmt.Sprint(kind), strings.Join(notes, "; ")}
}

// checkMark is the `"✓" if x else ""` the schema tables print for a required
// column — never Python's True/False.
func checkMark(v any) string {
	if b, _ := v.(bool); b {
		return "✓"
	}
	return ""
}

func schemaEdges(format string) error {
	_, resolved, err := loadResolved()
	if err != nil {
		return err
	}
	edges := introspect.EdgesView(resolved)
	return Emit(edges, format, func() {
		rows := [][]string{}
		for _, e := range edges {
			ev := e.(*omap.Map)
			pred, _ := ev.Get("predicate")
			from, _ := ev.Get("from")
			to, _ := ev.Get("to")
			many, _ := ev.Get("many")
			req, _ := ev.Get("required")
			card := "one"
			if b, _ := many.(bool); b {
				card = "many"
			}
			var notes []string
			if inv, _ := ev.Get("inverse"); inv != nil {
				notes = append(notes, fmt.Sprintf("inverse %v", inv))
			}
			if ac, _ := ev.Get("acyclic"); ac == true {
				notes = append(notes, "acyclic")
			}
			rows = append(rows, []string{
				fmt.Sprint(pred), joinAny(from), joinAny(to), card,
				checkMark(req), strings.Join(notes, "; "),
			})
		}
		printTable("edges", []string{"predicate", "from", "to", "card", "required", "notes"}, rows)
	})
}

func joinAny(v any) string {
	var parts []string
	for _, x := range v.([]any) {
		parts = append(parts, fmt.Sprint(x))
	}
	return strings.Join(parts, ", ")
}

// pyBool renders a Go bool the way Python str() does (True/False) — the human
// tables print Python bools verbatim.
func pyBool(v any) string {
	if b, ok := v.(bool); ok {
		if b {
			return "True"
		}
		return "False"
	}
	return fmt.Sprint(v)
}

// diffValue renders one side of a changed snapshot leaf on one line; a list
// or mapping renders as JSON.
func diffValue(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case []any, *omap.Map:
		// Loaded YAML always encodes; a failure here is a bug, not input.
		s, err := canon.EncodeCLI(x)
		if err != nil {
			panic(err)
		}
		return s
	}
	return pyValue(v)
}

func pyValue(v any) string {
	if b, ok := v.(bool); ok {
		return pyBool(b)
	}
	return fmt.Sprint(v)
}
