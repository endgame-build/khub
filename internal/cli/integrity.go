// integrity.go ports cli/integrity_cmd.py: the v1 integrity gate. Both verbs
// follow the read-command output contract, and both GATE — the report is
// emitted first, then a non-empty finding set exits 1.
package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/endgame-build/khub/internal/integrity"
	"github.com/endgame-build/khub/internal/omap"
)

func registerValidate(root *cobra.Command) {
	var format string
	var strict bool
	cmd := newCmd("validate [TARGET]", "Validate entities: khub validate [TARGET] [--strict].",
		"[TARGET]", func(cmd *cobra.Command, args []string) error {
			return Guard(format, func() error {
				ws, err := resolveRoot()
				if err != nil {
					return err
				}
				var target *string
				if len(args) > 0 {
					target = &args[0]
				}
				report, err := integrity.Validate(ws, target, strict)
				if err != nil {
					return err
				}
				payload := omap.New()
				payload.Set("count", report.Count)
				errorList := make([]any, 0, len(report.Errors))
				for _, e := range report.Errors {
					record := omap.New()
					record.Set("id", e.ID())
					record.Set("type", e.Type)
					record.Set("slug", e.Slug)
					record.Set("field", e.Field)
					record.Set("reason", e.Reason)
					errorList = append(errorList, record)
				}
				payload.Set("errors", errorList)
				if eerr := Emit(payload, format, func() {
					if report.OK() {
						fmt.Printf("Validated %d entities; 0 errors\n", report.Count)
						return
					}
					for _, e := range report.Errors {
						fmt.Printf("%s: %s: %s\n", e.ID(), e.Field, e.Reason)
					}
					fmt.Printf("Validated %d entities; %d errors\n", report.Count, len(report.Errors))
				}); eerr != nil {
					return eerr
				}
				if !report.OK() {
					return &ExitError{Code: 1}
				}
				return nil
			})
		})
	cmd.Args = clickArity(1)
	cmd.Flags().BoolVar(&strict, "strict", false, "Close the schema: reject undeclared keys.")
	cmd.Flags().StringVar(&format, "format", "text", "text (Rich on a TTY) or json.")
	root.AddCommand(cmd)
}

func registerCheck(root *cobra.Command) {
	var format string
	var strict bool
	cmd := newCmd("check", "Check the active graph: completeness, orphans, dangling edges, strays, cycles.",
		"", func(cmd *cobra.Command, args []string) error {
			return Guard(format, func() error {
				ws, err := resolveRoot()
				if err != nil {
					return err
				}
				report, err := integrity.Check(ws, strict)
				if err != nil {
					return err
				}
				if eerr := Emit(checkPayload(report), format, func() { checkHuman(report) }); eerr != nil {
					return eerr
				}
				if !report.Passed() {
					return &ExitError{Code: 1}
				}
				return nil
			})
		})
	cmd.Args = clickArity(0)
	cmd.Flags().BoolVar(&strict, "strict", false, "Fail the gate on orphans too (default: informational).")
	cmd.Flags().StringVar(&format, "format", "text", "text (Rich on a TTY) or json.")
	root.AddCommand(cmd)
}

func checkPayload(report *integrity.CheckReport) *omap.Map {
	payload := omap.New()
	payload.Set("passed", report.Passed())

	incomplete := make([]any, 0, len(report.Incomplete))
	for _, i := range report.Incomplete {
		record := omap.New()
		record.Set("id", i.ID())
		record.Set("type", i.Type)
		record.Set("slug", i.Slug)
		record.Set("missing_fields", strList(i.MissingFields))
		record.Set("missing_relations", strList(i.MissingRelations))
		incomplete = append(incomplete, record)
	}
	payload.Set("incomplete", incomplete)
	payload.Set("orphans", strList(report.Orphans))

	dangling := make([]any, 0, len(report.Dangling))
	for _, d := range report.Dangling {
		record := omap.New()
		record.Set("id", d.ID())
		record.Set("type", d.Type)
		record.Set("slug", d.Slug)
		record.Set("predicate", d.Predicate)
		record.Set("target", d.Target)
		dangling = append(dangling, record)
	}
	payload.Set("dangling", dangling)
	payload.Set("strays", strList(report.Strays))
	// Template files no type claims — usually a renamed template, which would
	// otherwise silently disable scaffolding and the body contract.
	payload.Set("stray_templates", strList(report.StrayTemplates))
	// The same hole from the claiming side: a declared `template:` name whose
	// file does not exist.
	payload.Set("missing_templates", strList(report.MissingTemplates))

	// Files claiming a known type from outside every layout: unscanned, so
	// invisible to every other finding here.
	misplaced := make([]any, 0, len(report.Misplaced))
	for _, m := range report.Misplaced {
		record := omap.New()
		record.Set("path", m.Path)
		record.Set("type", m.Type)
		record.Set("expected", m.Expected)
		misplaced = append(misplaced, record)
	}
	payload.Set("misplaced", misplaced)
	payload.Set("malformed", strList(report.Malformed))

	cycles := make([]any, 0, len(report.Cycles))
	for _, cycle := range report.Cycles {
		cycles = append(cycles, strList(cycle))
	}
	payload.Set("cycles", cycles)
	payload.Set("suppressed_dangling", report.SuppressedDangling)
	payload.Set("missing_singletons", strList(report.MissingSingletons))
	// Present but unpublished — for ANY singleton, not just required ones.
	payload.Set("draft_singletons", strList(report.DraftSingletons))
	// The subset that fails the gate, so a consumer can tell a finding from a note.
	payload.Set("draft_required_singletons", strList(report.DraftRequiredSingletons))
	// Say which gate ran: `orphans` populated with passed=true means default mode.
	payload.Set("strict", report.Strict)
	return payload
}

func checkHuman(report *integrity.CheckReport) {
	if report.Passed() {
		for _, o := range report.Orphans {
			fmt.Printf("orphan %s (informational)\n", o)
		}
		// Reported on BOTH paths: a drafted optional singleton does not fail the
		// gate, so on its own it lands here; the failing path prints it too.
		for _, name := range report.DraftSingletons {
			fmt.Printf("singleton %s is unpublished (draft: true) (informational)\n", name)
		}
		fmt.Println("Graph check passed")
		return
	}
	for _, inc := range report.Incomplete {
		gaps := strings.Join(append(append([]string{}, inc.MissingFields...), inc.MissingRelations...), ", ")
		fmt.Printf("active-but-incomplete %s: missing %s\n", inc.ID(), gaps)
	}
	for _, d := range report.Dangling {
		fmt.Printf("dangling edge %s: %s -> '%s' does not resolve\n", d.ID(), d.Predicate, d.Target)
	}
	for _, o := range report.Orphans {
		fmt.Printf("orphan %s\n", o)
	}
	for _, s := range report.Strays {
		fmt.Printf("stray file %s\n", s)
	}
	for _, s := range report.StrayTemplates {
		fmt.Printf("stray template %s: no type declares it and none is named for it\n", s)
	}
	for _, s := range report.MissingTemplates {
		fmt.Printf("declared template %s does not exist\n", s)
	}
	for _, m := range report.Misplaced {
		fmt.Printf("misplaced %s: declares type '%s' but sits outside %s — no command can see it\n",
			m.Path, m.Type, m.Expected)
	}
	for _, m := range report.Malformed {
		fmt.Printf("malformed file %s\n", m)
	}
	if report.SuppressedDangling > 0 {
		fmt.Printf("(%d dangling edges suppressed pending the malformed collection fix)\n",
			report.SuppressedDangling)
	}
	for _, cycle := range report.Cycles {
		fmt.Printf("cycle %s\n", strings.Join(cycle, " -> "))
	}
	for _, name := range report.MissingSingletons {
		fmt.Printf("required singleton %s is missing\n", name)
	}
	required := map[string]bool{}
	for _, name := range report.DraftRequiredSingletons {
		required[name] = true
	}
	for _, name := range report.DraftSingletons {
		// An optional drafted singleton is informational; a required one fails the gate.
		gate := "singleton"
		if required[name] {
			gate = "required singleton"
		}
		fmt.Printf("%s %s is unpublished (draft: true)\n", gate, name)
	}
}
