package explore

import (
	"fmt"
	"strings"
	"time"

	"github.com/endgame-build/khub/internal/integrity"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
)

// findingLimit caps the findings one bucket lists; its total is always whole.
const findingLimit = 50

// Findings is what `khub check` fails on (/api/findings), each with the entity
// or file it is about and the reason, in the order check lists its buckets.
// Orphans, drafts and the rest check only reports are markers on the entities
// themselves (Overview), so they are not repeated here.
//
// shadows are the fields a command's own flag hides, which only the command
// line knows.
func Findings(root string, shadows func(*schema.ResolvedSchema) []integrity.FlagShadow, now time.Time) (*omap.Map, error) {
	report, err := integrity.Check(root, false, now)
	if err != nil {
		return nil, err
	}
	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		return nil, err
	}
	if shadows != nil {
		report.FlagShadowed = shadows(resolved)
	}
	buckets := []any{}
	add := func(name string, items []any) {
		if len(items) == 0 {
			return
		}
		b := omap.New()
		b.Set("bucket", name)
		b.Set("total", len(items))
		if len(items) > findingLimit {
			items = items[:findingLimit]
		}
		b.Set("items", items)
		buckets = append(buckets, b)
	}
	// id names an entity the page can open, about is anything else, and ids
	// are the entities a finding joins.
	of := func(id, about, reason string, ids ...string) any {
		m := omap.New()
		m.Set("id", id)
		m.Set("about", about)
		m.Set("reason", reason)
		m.Set("ids", strList(ids))
		return m
	}
	collect := func(name string, n int, at func(i int) any) {
		items := make([]any, 0, n)
		for i := range n {
			items = append(items, at(i))
		}
		add(name, items)
	}
	abouts := func(name string, list []string, reason string) {
		collect(name, len(list), func(i int) any { return of("", list[i], reason) })
	}

	abouts("malformed", report.Malformed, "The file does not parse")
	collect("incomplete", len(report.Incomplete), func(i int) any {
		f := report.Incomplete[i]
		return of(f.ID(), "", missing(f))
	})
	collect("dangling", len(report.Dangling), func(i int) any {
		f := report.Dangling[i]
		return of(f.ID(), "", f.Predicate+" names "+f.Target+", which no entity answers to")
	})
	collect("body_shape", len(report.BodyShape), func(i int) any {
		f := report.BodyShape[i]
		return of(f.ID(), "", f.Reason)
	})
	collect("cycles", len(report.Cycles), func(i int) any {
		return of("", "", "The relation must not lead back to its start", report.Cycles[i]...)
	})
	abouts("strays", report.Strays, "Its type does not match where it is stored")
	collect("misplaced", len(report.Misplaced), func(i int) any {
		f := report.Misplaced[i]
		return of("", f.Path, "Declares type "+f.Type+", stored under "+f.Expected)
	})
	collect("alias_conflicts", len(report.AliasConflicts), func(i int) any {
		f := report.AliasConflicts[i]
		return of("", f.Alias, "Names more than one entity", f.Claimants...)
	})
	abouts("missing_singletons", report.MissingSingletons, "The type is required and has no entity")
	abouts("draft_required_singletons", report.DraftRequiredSingletons, "The type is required and its entity is a draft")
	abouts("stray_templates", report.StrayTemplates, "No type uses this template")
	abouts("missing_templates", report.MissingTemplates, "The declared template has no file")
	collect("template_invalid", len(report.TemplateInvalid), func(i int) any {
		f := report.TemplateInvalid[i]
		return of("", f.Type, f.Reason)
	})
	collect("flag_shadowed", len(report.FlagShadowed), func(i int) any {
		f := report.FlagShadowed[i]
		return of("", f.Type+"."+f.Field, "A flag of "+strings.Join(f.Commands, ", ")+" hides the field")
	})
	collect("stale_waivers", len(report.StaleWaivers), func(i int) any {
		f := report.StaleWaivers[i]
		return of("", f.ID(), fmt.Sprintf("The waiver of its %s finding matches nothing", f.Finding))
	})

	out := omap.New()
	out.Set("malformed", strList(report.Malformed))
	out.Set("passed", report.Passed())
	out.Set("buckets", buckets)
	return out, nil
}
