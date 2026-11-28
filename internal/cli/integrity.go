// integrity.go ports cli/integrity_cmd.py: the v1 integrity gate. Both verbs
// follow the read-command output contract, and both GATE — the report is
// emitted first, then a non-empty finding set exits 1.

package cli

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/endgame-build/khub/internal/clock"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/integrity"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/trust"
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
				if err := addShadowErrors(cmd.Root(), ws, report, target); err != nil {
					return err
				}
				if eerr := Emit(validatePayload(report), format, func() { validateHuman(report) }); eerr != nil {
					return eerr
				}
				// Errors alone gate; a gap is reported, never exit 1.
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
	var strict, stale, requireVerified, mentions, requireLenses, changed bool
	var tier string
	cmd := newCmd("check", "Check the active graph: completeness, orphans, dangling edges, strays, cycles, bodies.",
		"", func(cmd *cobra.Command, args []string) error {
			switch {
			case changed && !requireLenses:
				return clickUsageErr(cmd.CommandPath(), "", "--changed needs --require-lenses.")
			case !changed && len(args) > 0:
				return clickUsageErr(cmd.CommandPath(), "",
					"Got unexpected extra argument(s) ("+strings.Join(args, " ")+")")
			case slices.Contains(args, ""):
				return clickUsageErr(cmd.CommandPath(), "", "An empty string is not a file path.")
			}
			return Guard(format, func() error {
				ws, err := resolveRoot()
				if err != nil {
					return err
				}
				opts := integrity.CheckOptions{Strict: strict, Mentions: mentions, Lenses: requireLenses}
				if changed {
					// Paths read from the working directory, as get --path reads them.
					if opts.Base, err = os.Getwd(); err != nil {
						return err
					}
					opts.Changed = append([]string{}, args...)
				}
				report, err := integrity.CheckWith(ws, opts, clock.Now())
				if err != nil {
					return err
				}
				report.StaleGate = stale
				if requireVerified {
					if tier != trust.Machine && tier != trust.Human {
						return errs.UnknownTier(tier, []string{trust.Machine, trust.Human})
					}
					report.TierGate = tier
				}
				resolved, err := introspect.LoadSchema(ws)
				if err != nil {
					return err
				}
				report.FlagShadowed = shadowedFields(cmd.Root(), resolved)
				if eerr := Emit(checkPayload(report), format, func() { checkHuman(report) }); eerr != nil {
					return eerr
				}
				if !report.Passed() {
					return &ExitError{Code: 1}
				}
				return nil
			})
		})
	cmd.Flags().BoolVar(&strict, "strict", false, "Fail the gate on orphans too (default: informational).")
	cmd.Flags().BoolVar(&stale, "stale", false, "Fail the gate on active entities whose stale_after has passed.")
	cmd.Flags().BoolVar(&requireVerified, "require-verified", false, "Fail the gate on active entities below --tier.")
	cmd.Flags().StringVar(&tier, "tier", trust.Machine, "The review tier --require-verified asks for: machine or human.")
	cmd.Flags().BoolVar(&requireLenses, "require-lenses", false,
		"Fail the gate on applicable review lenses of active entities not in state pass (see khub review).")
	cmd.Flags().BoolVar(&changed, "changed", false,
		"With --require-lenses: judge only the entities stored at the paths that follow.")
	cmd.Flags().BoolVar(&mentions, "mentions", false,
		"Also suggest edges for entities a body names but no relation links (informational; never written).")
	cmd.Flags().StringVar(&format, "format", "text", "text (Rich on a TTY) or json.")
	root.AddCommand(cmd)
}

// findingRecords renders one finding bucket's rows: `{id, type, slug, field,
// reason}` for validate, whose rows span fields; without `field` for check's
// body buckets, where the bucket implies it (`template_invalid` is always the
// type's template, `body_shape` and `thin` are always the body).
func findingRecords(errs []integrity.FieldError, withField bool) []any {
	out := make([]any, 0, len(errs))
	for _, e := range errs {
		record := omap.New()
		record.Set("id", e.ID())
		record.Set("type", e.Type)
		record.Set("slug", e.Slug)
		if withField {
			record.Set("field", e.Field)
		}
		record.Set("reason", e.Reason)
		out = append(out, record)
	}
	return out
}

// validatePayload is kb `cmd_validate`'s payload: `count`, the gating
// `errors`, the informational `gaps`, then the single-target `body` metrics
// (`null` for a type or whole-workspace run) and applicable `lenses` (`[]`).
func validatePayload(report *integrity.ValidateReport) *omap.Map {
	payload := omap.New()
	payload.Set("count", report.Count)
	payload.Set("errors", findingRecords(report.Errors, true))
	payload.Set("gaps", findingRecords(report.Gaps, true))
	if report.Body == nil {
		payload.Set("body", nil)
	} else {
		body := omap.New()
		body.Set("words", report.Body.Words)
		sections := make([]any, 0, len(report.Body.Sections))
		for _, s := range report.Body.Sections {
			record := omap.New()
			record.Set("heading", s.Heading)
			record.Set("words", s.Words)
			sections = append(sections, record)
		}
		body.Set("sections", sections)
		payload.Set("body", body)
	}
	lenses := make([]any, 0, len(report.Lenses))
	for _, l := range report.Lenses {
		record := omap.New()
		record.Set("code", l.Code)
		record.Set("name", l.Name)
		if l.Section == "" {
			record.Set("section", nil)
		} else {
			record.Set("section", l.Section)
		}
		record.Set("instruction", l.Instruction)
		lenses = append(lenses, record)
	}
	payload.Set("lenses", lenses)
	return payload
}

// validateHuman is kb `cmd_validate`'s text: errors, then gaps with a `-`
// marker, the summary line, the body metrics, then each lens with its
// instruction indented beneath it.
func validateHuman(report *integrity.ValidateReport) {
	for _, e := range report.Errors {
		fmt.Printf("%s: %s: %s\n", e.ID(), e.Field, e.Reason)
	}
	for _, g := range report.Gaps {
		fmt.Printf("%s: - %s: %s\n", g.ID(), g.Field, g.Reason)
	}
	fmt.Printf("Validated %d entities; %d errors, %d gaps\n",
		report.Count, len(report.Errors), len(report.Gaps))
	if report.Body != nil {
		counts := make([]string, 0, len(report.Body.Sections))
		for _, s := range report.Body.Sections {
			counts = append(counts, fmt.Sprintf("%s %d", s.Heading, s.Words))
		}
		line := fmt.Sprintf("Body: %d words", report.Body.Words)
		if len(counts) > 0 {
			line += " (" + strings.Join(counts, ", ") + ")"
		}
		fmt.Println(line)
	}
	for _, l := range report.Lenses {
		where := ""
		if l.Section != "" {
			where = " [## " + l.Section + "]"
		}
		fmt.Printf("Lens %s%s: %s\n", l.Code, where, l.Name)
		for line := range strings.SplitSeq(l.Instruction, "\n") {
			fmt.Printf("  %s\n", line)
		}
	}
}

func checkPayload(report *integrity.CheckReport) *omap.Map {
	payload := omap.New()
	payload.Set("passed", report.Passed())

	incomplete := make([]any, 0, len(report.Incomplete))
	for _, i := range report.Incomplete {
		incomplete = append(incomplete, incompleteRecord(i))
	}
	payload.Set("incomplete", incomplete)
	payload.Set("orphans", strList(report.Orphans))

	dangling := make([]any, 0, len(report.Dangling))
	for _, d := range report.Dangling {
		dangling = append(dangling, danglingRecord(d))
	}
	payload.Set("dangling", dangling)
	payload.Set("strays", strList(report.Strays))
	// Template files no type claims — usually a renamed template, which would
	// otherwise silently disable scaffolding and the body contract.
	payload.Set("stray_templates", strList(report.StrayTemplates))
	// The same hole from the claiming side: a declared `template:` name whose
	// file does not exist.
	payload.Set("missing_templates", strList(report.MissingTemplates))
	// A template that exists but does not parse: one row per TYPE
	// (`<type>/*`), and that type's bodies go unjudged.
	payload.Set("template_invalid", findingRecords(report.TemplateInvalid, false))
	// A body missing a required heading (or holding it out of order).
	payload.Set("body_shape", findingRecords(report.BodyShape, false))

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
	// Aliases naming more than one entity: an ID lookup through one is
	// ambiguous, or lands on another entity's slug.
	conflicts := make([]any, 0, len(report.AliasConflicts))
	for _, c := range report.AliasConflicts {
		record := omap.New()
		record.Set("alias", c.Alias)
		record.Set("claimants", strList(c.Claimants))
		conflicts = append(conflicts, record)
	}
	payload.Set("alias_conflicts", conflicts)
	// Only under --stale, so a default check keeps its shape.
	if report.StaleGate {
		payload.Set("expired", strList(report.Expired))
	}
	// Only under --require-verified: active entities below the asked tier.
	if report.TierGate != "" {
		below := make([]any, 0)
		for _, f := range report.BelowGate() {
			record := omap.New()
			record.Set("id", f.ID())
			record.Set("type", f.Type)
			record.Set("slug", f.Slug)
			record.Set("trust", f.Trust)
			below = append(below, record)
		}
		payload.Set("below_tier", below)
	}
	// Only under --require-lenses: applicable lenses not in state pass.
	if report.LensGate {
		lenses := make([]any, 0, len(report.Lenses))
		for _, f := range report.Lenses {
			record := omap.New()
			record.Set("id", f.ID())
			record.Set("type", f.Type)
			record.Set("slug", f.Slug)
			record.Set("lens", f.Lens)
			record.Set("state", f.State)
			lenses = append(lenses, record)
		}
		payload.Set("lenses", lenses)
	}
	// A schema field a write verb's flag shadows: that verb can never set it.
	shadowed := make([]any, 0, len(report.FlagShadowed))
	for _, f := range report.FlagShadowed {
		record := omap.New()
		record.Set("id", f.Type+"/*")
		record.Set("type", f.Type)
		record.Set("field", f.Field)
		record.Set("flag", "--"+f.Field)
		record.Set("commands", strList(f.Commands))
		shadowed = append(shadowed, record)
	}
	payload.Set("flag_shadowed", shadowed)
	// Policy waivers that matched no finding: they fail the gate, so a waiver
	// cannot outlive the finding it was written for.
	stale := make([]any, 0, len(report.StaleWaivers))
	for _, w := range report.StaleWaivers {
		record := omap.New()
		record.Set("id", w.ID())
		record.Set("type", w.Type)
		record.Set("slug", w.Slug)
		record.Set("finding", w.Finding)
		record.Set("reason", w.Reason)
		record.Set("cause", w.Cause)
		stale = append(stale, record)
	}
	payload.Set("stale_waivers", stale)
	// Say which gate ran: `orphans` populated with passed=true means default mode.
	payload.Set("strict", report.Strict)
	// Last, after `strict`: the buckets that never affect `passed`. A body
	// whose prose does not satisfy a section rule is that document, unfinished.
	payload.Set("thin", findingRecords(report.Thin, false))
	// Informational too: a review of a version that has since changed.
	superseded := make([]any, 0, len(report.Superseded))
	for _, s := range report.Superseded {
		record := omap.New()
		record.Set("id", s.ID())
		record.Set("type", s.Type)
		record.Set("slug", s.Slug)
		record.Set("event", s.Event)
		record.Set("by", s.By)
		record.Set("rev", s.Rev)
		superseded = append(superseded, record)
	}
	payload.Set("superseded_verifications", superseded)
	// Entities past their type's policy `retention` rule: candidates for
	// removal, which khub reports and never acts on.
	retention := make([]any, 0, len(report.Retention))
	for _, e := range report.Retention {
		record := omap.New()
		record.Set("id", e.ID())
		record.Set("type", e.Type)
		record.Set("slug", e.Slug)
		record.Set("date", e.Date)
		record.Set("age_days", e.AgeDays)
		retention = append(retention, record)
	}
	payload.Set("retention", retention)
	// Findings a policy waiver accepts, each with the bucket it left, the
	// waiver's reason, and the finding as that bucket would have held it.
	waived := make([]any, 0, len(report.Waived))
	for _, w := range report.Waived {
		record := omap.New()
		record.Set("id", w.ID())
		record.Set("type", w.Type)
		record.Set("slug", w.Slug)
		record.Set("finding", w.Finding)
		record.Set("reason", w.Reason)
		record.Set("record", waivedDetail(w.Detail))
		waived = append(waived, record)
	}
	payload.Set("waived", waived)
	// Only when asked for: suggestions, not findings, so they never affect
	// `passed` and stay out of the default payload.
	if report.MentionsRun {
		mentions := make([]any, 0, len(report.Mentions))
		for _, m := range report.Mentions {
			record := omap.New()
			record.Set("id", m.ID())
			record.Set("type", m.Type)
			record.Set("slug", m.Slug)
			record.Set("target", m.Target)
			record.Set("text", m.Text)
			record.Set("predicates", strList(m.Predicates))
			mentions = append(mentions, record)
		}
		payload.Set("mentions", mentions)
	}
	return payload
}

func incompleteRecord(i integrity.Incomplete) *omap.Map {
	record := omap.New()
	record.Set("id", i.ID())
	record.Set("type", i.Type)
	record.Set("slug", i.Slug)
	record.Set("missing_fields", strList(i.MissingFields))
	record.Set("missing_relations", strList(i.MissingRelations))
	return record
}

func danglingRecord(d integrity.Dangling) *omap.Map {
	record := omap.New()
	record.Set("id", d.ID())
	record.Set("type", d.Type)
	record.Set("slug", d.Slug)
	record.Set("predicate", d.Predicate)
	record.Set("target", d.Target)
	return record
}

// waivedDetail renders a waived finding exactly as its own bucket would.
func waivedDetail(detail any) any {
	switch d := detail.(type) {
	case integrity.Incomplete:
		return incompleteRecord(d)
	case integrity.Dangling:
		return danglingRecord(d)
	case integrity.FieldError:
		return findingRecords([]integrity.FieldError{d}, false)[0]
	default:
		return detail // an orphan: its qualified id
	}
}

// waivedLine is a waived finding's text line: the bucket, what it says, and
// the waiver's reason.
func waivedLine(w integrity.Waived) string {
	what := ""
	switch d := w.Detail.(type) {
	case integrity.Incomplete:
		what = ": missing " + strings.Join(append(append([]string{}, d.MissingFields...), d.MissingRelations...), ", ")
	case integrity.Dangling:
		what = fmt.Sprintf(": %s -> '%s'", d.Predicate, d.Target)
	case integrity.FieldError:
		what = ": " + d.Reason
	}
	return fmt.Sprintf("waived %s %s%s (reason: %s)", w.Finding, w.ID(), what, w.Reason)
}

// shadowReason says why a shadowed field is a defect, for check and validate.
func shadowReason(f integrity.FlagShadow) string {
	verbs := make([]string, len(f.Commands))
	for i, v := range f.Commands {
		verbs[i] = "`khub " + v + "`"
	}
	return fmt.Sprintf("--%s is a flag of %s, so `--%s <value>` never sets field '%s' "+
		"(only `khub edit <id> %s <value>` does); rename the field in the schema",
		f.Field, strings.Join(verbs, " and "), f.Field, f.Field, f.Field)
}

// addShadowErrors adds each shadowed field to validate's errors, one row per
// field against its type (`<type>/*`), within the target's types.
func addShadowErrors(root *cobra.Command, ws string, report *integrity.ValidateReport, target *string) error {
	resolved, err := introspect.LoadSchema(ws)
	if err != nil {
		return err
	}
	for _, f := range shadowedFields(root, resolved) {
		if target != nil && strings.SplitN(*target, "/", 2)[0] != f.Type {
			continue
		}
		report.Errors = append(report.Errors, integrity.FieldError{
			Type: f.Type, Slug: "*", Field: f.Field, Reason: shadowReason(f),
		})
	}
	return nil
}

func checkHuman(report *integrity.CheckReport) {
	if report.Passed() {
		for _, o := range report.Orphans {
			fmt.Printf("orphan %s (informational)\n", o)
		}
		// Reported on BOTH paths: a drafted optional singleton does not fail the
		// gate, so on its own it lands here; the failing path prints it too.
		for _, name := range report.DraftSingletons {
			fmt.Printf("singleton %s is unpublished (status: draft) (informational)\n", name)
		}
		// Informational is not the same as invisible.
		for _, th := range report.Thin {
			fmt.Printf("thin %s: %s (informational)\n", th.ID(), th.Reason)
		}
		printSuperseded(report, " (informational)")
		printRetention(report)
		for _, w := range report.Waived {
			fmt.Println(waivedLine(w))
		}
		for _, m := range report.Mentions {
			fmt.Printf("%s (informational)\n", mentionLine(m))
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
	for _, ti := range report.TemplateInvalid {
		fmt.Printf("template_invalid %s: %s\n", ti.ID(), ti.Reason)
	}
	for _, bs := range report.BodyShape {
		fmt.Printf("body_shape %s: %s\n", bs.ID(), bs.Reason)
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
	for _, c := range report.AliasConflicts {
		fmt.Printf("alias '%s' names more than one entity: %s\n", c.Alias, strings.Join(c.Claimants, ", "))
	}
	for _, f := range report.FlagShadowed {
		fmt.Printf("flag_shadowed %s.%s: %s\n", f.Type, f.Field, shadowReason(f))
	}
	for _, w := range report.StaleWaivers {
		why := "matches no finding"
		if w.Cause == integrity.StaleOrphanExempt {
			why = fmt.Sprintf("type '%s' declares orphan: true, so it never has an orphan", w.Type)
		}
		fmt.Printf("stale waiver %s %s: %s — remove it from policy.yaml\n", w.Finding, w.ID(), why)
	}
	for _, f := range report.BelowGate() {
		fmt.Printf("below tier %s: trust %s, %s asked for\n", f.ID(), f.Trust, report.TierGate)
	}
	if report.LensGate {
		for _, f := range report.Lenses {
			fmt.Printf("lens %s %s: %s, pass asked for\n", f.ID(), f.Lens, f.State)
		}
	}
	if report.StaleGate {
		for _, id := range report.Expired {
			fmt.Printf("expired %s: its stale_after has passed\n", id)
		}
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
		fmt.Printf("%s %s is unpublished (status: draft)\n", gate, name)
	}
	// Last: the gate did not fail because of these.
	for _, th := range report.Thin {
		fmt.Printf("thin %s: %s\n", th.ID(), th.Reason)
	}
	printSuperseded(report, "")
	printRetention(report)
	for _, w := range report.Waived {
		fmt.Println(waivedLine(w))
	}
	for _, m := range report.Mentions {
		fmt.Println(mentionLine(m))
	}
}

// mentionLine is one mention suggestion as text: what the body says, the
// entity it names, and the predicates that could link it.
func mentionLine(m integrity.Mention) string {
	return fmt.Sprintf("mention %s: '%s' names %s with no edge (link as %s)",
		m.ID(), m.Text, m.Target, strings.Join(m.Predicates, ", "))
}

// printRetention prints the retention bucket, marked informational on both
// paths: the gate never fails because of it.
func printRetention(report *integrity.CheckReport) {
	for _, e := range report.Retention {
		fmt.Printf("retention %s: dated %s, %d days old, past its type's retention (informational)\n",
			e.ID(), e.Date, e.AgeDays)
	}
}

func printSuperseded(report *integrity.CheckReport, note string) {
	for _, s := range report.Superseded {
		fmt.Printf("superseded verification %s: verified[%d] by %s saw rev %s, and the content has changed since%s\n",
			s.ID(), s.Event, s.By, s.Rev, note)
	}
}
