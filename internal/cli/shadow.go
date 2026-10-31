// shadow.go keeps a schema field and a write verb's own flag from sharing a
// name unnoticed. add and edit read their flags before any --<field> pair, so
// `--<field> <value>` never reaches such a field and a value meant for it goes
// to the flag instead. Only the flag form is ambiguous: `khub edit <id>
// <field> <value>` still sets the field.

package cli

import (
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/endgame-build/khub/internal/entity"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/integrity"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/schema"
)

// writeVerbs read their own flags before any --<field> pair.
var writeVerbs = []string{"add", "edit"}

// shadowedFields lists every schema field a write verb's flag shadows, with
// the verbs, read from the cobra commands themselves.
func shadowedFields(root *cobra.Command, resolved *schema.ResolvedSchema) []integrity.FlagShadow {
	verbsOf := map[string][]string{}
	for _, verb := range writeVerbs {
		cmd, _, err := root.Find([]string{verb})
		if err != nil || cmd.Name() != verb {
			continue
		}
		cmd.Flags().VisitAll(func(f *pflag.Flag) { verbsOf[f.Name] = append(verbsOf[f.Name], verb) })
	}
	out := []integrity.FlagShadow{}
	for _, name := range resolved.Types.Keys() {
		rtype, _ := resolved.Types.Get(name)
		for _, field := range rtype.FieldNames() {
			if verbs := verbsOf[field]; len(verbs) > 0 {
				out = append(out, integrity.FlagShadow{Type: name, Field: field, Commands: verbs})
			}
		}
	}
	return out
}

// refuseAmbiguousFlag refuses a write that passes a flag the type being
// written also declares as a field: the value would silently go to the flag.
// typeName names the type for add, id the entity for edit. A write that passes
// no such flag proceeds; check and validate report the collision itself.
func refuseAmbiguousFlag(cmd *cobra.Command, ws, typeName, id string) error {
	var passed []string
	cmd.Flags().Visit(func(f *pflag.Flag) { passed = append(passed, f.Name) })
	if len(passed) == 0 || (typeName == "" && id == "") {
		return nil
	}
	resolved, err := introspect.LoadSchema(ws)
	if err != nil {
		return err
	}
	declared := false
	for _, name := range resolved.Types.Keys() {
		rtype, _ := resolved.Types.Get(name)
		for _, flag := range passed {
			declared = declared || rtype.Declares(flag)
		}
	}
	if !declared {
		return nil // no type declares a passed flag: nothing to resolve
	}
	if typeName == "" {
		view, err := entity.Get(ws, id, false)
		if err != nil {
			return err
		}
		typeName = view.Type
	}
	rtype, ok := resolved.Types.Get(typeName)
	if !ok {
		return nil // the write itself reports the unknown type
	}
	for _, flag := range passed {
		if rtype.Declares(flag) {
			hint := "the flag keeps its own meaning"
			if flag == "by" {
				hint = "KHUB_ACTOR names the writer"
			}
			return errs.FieldShadowsFlag(typeName, flag, cmd.Name(), hint)
		}
	}
	return nil
}
