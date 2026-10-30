// extract.go wires the ingestion half of schema-constrained extraction under
// the schema group: `schema scaffold` emits the extraction contract, `schema
// conform` scores an extractor's envelope against it. Both are thin over
// internal/extract — zero per-type code, every row walked from the resolved
// schema. The text form of the scaffold is a Markdown prompt an agent pastes;
// conform's text form follows validate's line shape and gates the same way:
// exit 1 when an entity does not conform, 2 when the envelope cannot be read.

package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/extract"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/workspace"
)

func registerExtract(schemaCmd *cobra.Command) {
	var scaffoldFormat, conformFormat string
	var only []string
	scaffoldCmd := newCmd("scaffold",
		"Emit the extraction contract: every type as an extractor fills it, and the envelope to return.", "",
		func(cmd *cobra.Command, args []string) error {
			return Guard(scaffoldFormat, func() error { return schemaScaffold(only, scaffoldFormat) })
		})
	scaffoldCmd.Args = clickArity(0)
	scaffoldCmd.Flags().StringArrayVar(&only, "type", nil, "Limit the contract to this type (repeatable).")
	scaffoldCmd.Flags().StringVar(&scaffoldFormat, "format", "text", "text (a Markdown prompt) or json.")

	conformCmd := newCmd("conform",
		"Score an extraction envelope against the schema and the workspace; writes nothing.",
		"{file}", func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return missingArgument(cmd.CommandPath(), "{file}", "file")
			}
			return Guard(conformFormat, func() error { return schemaConform(args[0], conformFormat) })
		})
	conformCmd.Args = clickArity(1)
	conformCmd.Flags().StringVar(&conformFormat, "format", "text", "text (Rich on a TTY) or json.")

	schemaCmd.AddCommand(scaffoldCmd, conformCmd)
}

func schemaScaffold(only []string, format string) error {
	root, resolved, err := loadResolved()
	if err != nil {
		return err
	}
	prov, err := workspace.Provenance(root)
	if err != nil {
		return err
	}
	view, err := extract.Scaffold(root, resolved, prov, only)
	if err != nil {
		return err
	}
	return Emit(view, format, func() { fmt.Print(scaffoldMarkdown(view)) })
}

// scaffoldMarkdown renders the contract as the prompt an agent pastes: the
// rules, the envelope, then one section per type with its field and relation
// tables and body sections. Every string comes from the view.
func scaffoldMarkdown(view *omap.Map) string {
	var b strings.Builder
	prov := mapAt(view, "provenance")
	fmt.Fprintf(&b, "# khub extraction contract: %v@%v\n\n", get(prov, "preset"), get(prov, "version"))
	b.WriteString("Extract every record the source supports into the envelope below.\n\n")
	envelope := mapAt(view, "envelope")
	for _, rule := range listAt(envelope, "rules") {
		fmt.Fprintf(&b, "- %v\n", rule)
	}
	fmt.Fprintf(&b, "\nSupplied by khub, never filled: %s.\n", joinAny(get(envelope, "supplied")))
	shape, _ := canon.EncodeCLI(get(envelope, "shape"))
	fmt.Fprintf(&b, "\n## Envelope\n\n```json\n%s\n```\n", shape)
	for _, t := range listAt(view, "types") {
		tv := t.(*omap.Map)
		// One blank line between blocks whatever the previous one ended with.
		text := strings.TrimRight(b.String(), "\n")
		b.Reset()
		b.WriteString(text)
		fmt.Fprintf(&b, "\n\n## %v\n\n", get(tv, "name"))
		if when := get(tv, "when"); when != nil {
			fmt.Fprintf(&b, "When: %v\n\n", when)
		}
		if shape := get(tv, "id_shape"); shape != nil {
			fmt.Fprintf(&b, "Id: minted by khub as `%v`; never set it.\n\n", shape)
		} else {
			b.WriteString("Id: a singleton; the workspace holds at most one.\n\n")
		}
		fields := listAt(tv, "fields")
		if len(fields) > 0 {
			b.WriteString("| field | kind | required | allowed |\n|---|---|---|---|\n")
			for _, f := range fields {
				fv := f.(*omap.Map)
				var allowed []string
				if e := get(fv, "enum"); e != nil {
					allowed = append(allowed, "one of "+joinAny(e))
				}
				if p := get(fv, "pattern"); p != nil {
					allowed = append(allowed, fmt.Sprintf("matches `%v`", p))
				}
				if d := get(fv, "default"); d != nil {
					allowed = append(allowed, fmt.Sprintf("default %s", pyValue(d)))
				}
				fmt.Fprintf(&b, "| %v | %v | %s | %s |\n", get(fv, "name"), get(fv, "kind"),
					checkMark(get(fv, "required")), strings.Join(allowed, "; "))
			}
			b.WriteString("\n")
		}
		rels := listAt(tv, "relations")
		if len(rels) > 0 {
			b.WriteString("| relation | to | many | required |\n|---|---|---|---|\n")
			for _, r := range rels {
				rv := r.(*omap.Map)
				fmt.Fprintf(&b, "| %v | %s | %s | %s |\n", get(rv, "name"), joinAny(get(rv, "to")),
					checkMark(get(rv, "many")), checkMark(get(rv, "required")))
			}
			b.WriteString("\n")
		}
		if body, ok := get(tv, "body").(*omap.Map); ok {
			sections, _ := get(body, "sections").([]any)
			switch {
			case get(body, "sections") == nil:
				fmt.Fprintf(&b, "Body: template %v does not parse; see `khub validate`.\n", get(body, "template"))
			case len(sections) == 0:
				fmt.Fprintf(&b, "Body: prose, no headings (template %v).\n", get(body, "template"))
			default:
				fmt.Fprintf(&b, "Body sections (template %v), in order:\n\n", get(body, "template"))
				for _, s := range sections {
					sv := s.(*omap.Map)
					line := fmt.Sprintf("- ## %v", get(sv, "heading"))
					if b, _ := get(sv, "optional").(bool); b {
						line += " (optional)"
					}
					if hint := get(sv, "hint"); hint != nil {
						line += fmt.Sprintf(": %v", hint)
					}
					b.WriteString(line + "\n")
				}
			}
		}
	}
	return b.String()
}

// schemaConform reads the envelope (a path, or `-` for stdin), scores it, and
// gates on the result: every entity conformant is exit 0, otherwise 1.
func schemaConform(file, format string) error {
	root, err := resolveRoot()
	if err != nil {
		return err
	}
	var raw []byte
	if file == "-" {
		raw, err = io.ReadAll(os.Stdin)
	} else {
		var text string
		text, err = canon.ReadText(file)
		raw = []byte(text)
	}
	if err != nil {
		return err
	}
	env, err := extract.ParseEnvelope(raw)
	if err != nil {
		return err
	}
	report, err := extract.Conform(root, env)
	if err != nil {
		return err
	}
	if eerr := Emit(conformPayload(report), format, func() { conformHuman(report) }); eerr != nil {
		return eerr
	}
	if !report.OK() {
		return &ExitError{Code: 1}
	}
	return nil
}

// conformPayload is the report's JSON: one record per entity, then the
// summary — `{entities, conformant, score}`.
func conformPayload(report *extract.Report) *omap.Map {
	entities := make([]any, 0, len(report.Entities))
	for _, e := range report.Entities {
		record := omap.New()
		record.Set("index", e.Index)
		record.Set("type", e.Type)
		record.Set("ok", e.OK)
		errs := make([]any, 0, len(e.Errors))
		for _, fe := range e.Errors {
			row := omap.New()
			row.Set("field", fe.Field)
			row.Set("reason", fe.Reason)
			errs = append(errs, row)
		}
		record.Set("errors", errs)
		record.Set("gaps", strList(e.Gaps))
		unresolved := make([]any, 0, len(e.Unresolved))
		for _, u := range e.Unresolved {
			row := omap.New()
			row.Set("predicate", u.Predicate)
			row.Set("value", u.Value)
			row.Set("reason", u.Reason)
			unresolved = append(unresolved, row)
		}
		record.Set("unresolved", unresolved)
		entities = append(entities, record)
	}
	payload := omap.New()
	payload.Set("entities", entities)
	summary := omap.New()
	summary.Set("entities", len(report.Entities))
	summary.Set("conformant", report.Conformant)
	summary.Set("score", report.Score)
	payload.Set("summary", summary)
	return payload
}

// conformHuman follows validate's text: one line per error, per unresolved
// target and per gap list, then the summary line.
func conformHuman(report *extract.Report) {
	for _, e := range report.Entities {
		for _, fe := range e.Errors {
			fmt.Printf("#%d %s: %s: %s\n", e.Index, e.Type, fe.Field, fe.Reason)
		}
		for _, u := range e.Unresolved {
			fmt.Printf("#%d %s: %s -> '%s' unresolved: %s\n", e.Index, e.Type, u.Predicate, u.Value, u.Reason)
		}
		if len(e.Gaps) > 0 {
			fmt.Printf("#%d %s: - missing %s\n", e.Index, e.Type, strings.Join(e.Gaps, ", "))
		}
	}
	fmt.Printf("Conformed %d entities; %d conformant, score %s\n",
		len(report.Entities), report.Conformant, report.ScoreText())
}

func get(m *omap.Map, key string) any {
	v, _ := m.Get(key)
	return v
}

func mapAt(m *omap.Map, key string) *omap.Map {
	if v, ok := get(m, key).(*omap.Map); ok {
		return v
	}
	return omap.New()
}

func listAt(m *omap.Map, key string) []any {
	v, _ := get(m, key).([]any)
	return v
}
