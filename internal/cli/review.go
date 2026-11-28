// review.go is `khub review`: the prompt that asks every open lens of the
// entities a change touches, and `--record`, which appends the answers as lens
// verdicts. khub calls no model: an agent, a person or a script answers.

package cli

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/clock"
	"github.com/endgame-build/khub/internal/entity"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/integrity"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/trust"
)

const reviewArgSpec = "[PATH]..."

func registerReview(root *cobra.Command) {
	var format, record, by string
	var changed, dryRun bool
	cmd := newCmd("review [PATH]...",
		"Prompt for the open review lenses, or record the answers: khub review --changed <path>...",
		reviewArgSpec, func(cmd *cobra.Command, args []string) error {
			path := cmd.CommandPath()
			recording := cmd.Flags().Changed("record")
			switch {
			case recording && changed:
				return clickUsageErr(path, reviewArgSpec, "Pass --record or --changed, not both.")
			case recording && len(args) > 0:
				return clickUsageErr(path, reviewArgSpec, "--record takes no paths.")
			case recording && by == "":
				return clickUsageErr(path, reviewArgSpec, "Missing option '--by'.")
			case !recording && (by != "" || dryRun):
				return clickUsageErr(path, reviewArgSpec, "--by and --dry-run need --record.")
			case !changed && len(args) > 0:
				return clickUsageErr(path, reviewArgSpec,
					"Got unexpected extra argument(s) ("+strings.Join(args, " ")+"); pass paths after --changed.")
			case slices.Contains(args, ""):
				return clickUsageErr(path, reviewArgSpec, "An empty string is not a file path.")
			}
			// An explicit --format text prints the prompt on a pipe too, as
			// `get --format raw` prints the file: the prompt is what an agent
			// reads, and the default JSON carries no body.
			prompt := cmd.Flags().Changed("format") && format == "text"
			return Guard(format, func() error {
				ws, err := resolveRoot()
				if err != nil {
					return err
				}
				if recording {
					return reviewRecord(cmd, ws, record, by, dryRun, format)
				}
				var paths []string
				if changed {
					paths = append([]string{}, args...)
				}
				cwd, err := os.Getwd()
				if err != nil {
					return err
				}
				report, err := integrity.Review(ws, cwd, paths)
				if err != nil {
					return err
				}
				if prompt {
					printReviewPrompt(report)
					return nil
				}
				return Emit(reviewPayload(report), format, func() { printReviewPrompt(report) })
			})
		})
	cmd.Flags().BoolVar(&changed, "changed", false,
		"Review only the entities stored at the paths that follow, resolved as get --path does; none given reviews nothing.")
	cmd.Flags().StringVar(&record, "record", "",
		"Record a reply from this file (- for stdin): {\"verdicts\": [{id, lens, rev, verdict, note}]}.")
	cmd.Flags().StringVar(&by, "by", "", "Who answered (required with --record): human:<name>, process:<name>, or <producer>[/<version>].")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "With --record: check the reply and print what would be recorded; write nothing.")
	cmd.Flags().StringVar(&format, "format", "text", "text (the prompt) or json.")
	root.AddCommand(cmd)
}

// reviewPayload is the JSON read: every target with every applicable lens and
// its state, then the skipped paths.
func reviewPayload(report *integrity.ReviewReport) *omap.Map {
	entities := make([]any, 0, len(report.Targets))
	for _, t := range report.Targets {
		record := omap.New()
		record.Set("id", t.ID())
		record.Set("type", t.Type)
		record.Set("slug", t.Slug)
		record.Set("path", t.Path)
		record.Set("rev", t.Rev)
		lenses := make([]any, 0, len(t.Lenses))
		for _, l := range t.Lenses {
			lens := omap.New()
			lens.Set("code", l.Code)
			lens.Set("name", l.Name)
			if l.Section == "" {
				lens.Set("section", nil)
			} else {
				lens.Set("section", l.Section)
			}
			lens.Set("instruction", l.Instruction)
			lens.Set("q", l.Q)
			lens.Set("state", l.State)
			lenses = append(lenses, lens)
		}
		record.Set("lenses", lenses)
		entities = append(entities, record)
	}
	payload := omap.New()
	payload.Set("entities", entities)
	payload.Set("skipped", strList(report.Skipped))
	return payload
}

// printReviewPrompt is the text read: a prompt for whoever answers. A header
// that says what to do and the exact reply shape, then each target with an
// open lens: its id, path and rev, the lenses to answer, and its body
// verbatim between markers.
func printReviewPrompt(report *integrity.ReviewReport) {
	var open []integrity.LensTarget
	count := 0
	for _, t := range report.Targets {
		if n := len(t.Open()); n > 0 {
			open = append(open, t)
			count += n
		}
	}
	if len(open) == 0 {
		if len(report.Targets) == 0 {
			fmt.Println("Nothing to review: no entity with a review lens.")
		} else {
			fmt.Printf("Nothing to review: every applicable lens of %d entities passes.\n", len(report.Targets))
		}
		for _, p := range report.Skipped {
			fmt.Printf("skipped %s: no entity with a review lens\n", p)
		}
		return
	}
	fmt.Printf("Review %d lens(es) on %d entities.\n", count, len(open))
	fmt.Println("For each lens below, judge the entity's body between its BEGIN and END lines against the instruction.")
	fmt.Println("Answer only from that body. Reply with one JSON document in exactly this shape, one verdict per lens:")
	fmt.Println()
	fmt.Println(`{"verdicts": [{"id": "<id>", "lens": "<code>", "rev": "<rev>", "verdict": "pass", "note": "<optional>"},`)
	fmt.Println(`              {"id": "<id>", "lens": "<code>", "rev": "<rev>", "verdict": "fail", "note": "<what to change>"}]}`)
	fmt.Println()
	fmt.Println("Copy id and rev from the entity's line. A fail needs a note. Then record the reply:")
	fmt.Println("khub review --record <file|-> --by <actor>")
	for _, t := range open {
		fmt.Println()
		fmt.Printf("== %s  path %s  rev %s\n", t.ID(), t.Path, t.Rev)
		for _, l := range t.Open() {
			where := ""
			if l.Section != "" {
				where = " [## " + l.Section + "]"
			}
			fmt.Printf("Lens %s%s: %s (%s)\n", l.Code, where, l.Name, l.State)
			for line := range strings.SplitSeq(l.Instruction, "\n") {
				fmt.Printf("  %s\n", line)
			}
		}
		fmt.Printf("----- BEGIN %s rev %s -----\n", t.ID(), t.Rev)
		fmt.Print(t.Body)
		if t.Body != "" && !strings.HasSuffix(t.Body, "\n") {
			fmt.Println()
		}
		fmt.Printf("----- END %s rev %s -----\n", t.ID(), t.Rev)
	}
	for _, p := range report.Skipped {
		fmt.Printf("skipped %s: no entity with a review lens\n", p)
	}
}

// readReply reads the reply file, or stdin for "-".
func readReply(source string) ([]byte, error) {
	if source == "-" {
		return io.ReadAll(os.Stdin)
	}
	text, err := canon.ReadText(source)
	if err != nil {
		return nil, err
	}
	return []byte(text), nil
}

// parseReply checks a reply's shape and each verdict's own fields; the checks
// that need the workspace run under the lock.
func parseReply(data []byte) ([]integrity.LensVerdict, error) {
	doc, err := canon.DecodeOrderedJSONStrict(data)
	if err != nil {
		return nil, errs.InvalidReply("it does not parse as JSON")
	}
	top, isMap := doc.(*omap.Map)
	if !isMap {
		return nil, errs.InvalidReply("the document is not an object")
	}
	raw, has := top.Get("verdicts")
	list, isList := raw.([]any)
	if !has || !isList {
		return nil, errs.InvalidReply("'verdicts' is missing or not a list")
	}
	out := make([]integrity.LensVerdict, 0, len(list))
	for i, item := range list {
		m, isMap := item.(*omap.Map)
		if !isMap {
			return nil, errs.InvalidReply(fmt.Sprintf("verdicts[%d] is not an object", i))
		}
		v := integrity.LensVerdict{Index: i}
		for _, f := range []struct {
			key string
			dst *string
		}{{"id", &v.ID}, {"lens", &v.Lens}, {"rev", &v.Rev}, {"verdict", &v.Verdict}} {
			s, isStr := get(m, f.key).(string)
			if !isStr || strings.TrimSpace(s) == "" {
				return nil, errs.InvalidReply(fmt.Sprintf("verdicts[%d].%s is missing or not a non-empty string", i, f.key))
			}
			*f.dst = s
		}
		if note, present := m.Get("note"); present && note != nil {
			s, isStr := note.(string)
			if !isStr {
				return nil, errs.InvalidReply(fmt.Sprintf("verdicts[%d].note is not a string", i))
			}
			v.Note, v.HasNote = s, strings.TrimSpace(s) != ""
		}
		if v.Verdict != integrity.VerdictPass && v.Verdict != integrity.VerdictFail {
			return nil, errs.InvalidVerdict(i, v.Verdict)
		}
		if v.Verdict == integrity.VerdictFail && !v.HasNote {
			return nil, errs.NoteRequired(i, v.ID, v.Lens)
		}
		out = append(out, v)
	}
	return out, nil
}

func get(m *omap.Map, k string) any {
	v, _ := m.Get(k)
	return v
}

// reviewRecord is `khub review --record`: check the whole reply, then append
// one lens event per verdict, all or none.
func reviewRecord(cmd *cobra.Command, ws, source, by string, dryRun bool, format string) error {
	if !trust.ValidActor(by) {
		return errs.InvalidActor(by)
	}
	data, err := readReply(source)
	if err != nil {
		return err
	}
	verdicts, err := parseReply(data)
	if err != nil {
		return err
	}
	at := clock.Stamp()
	appended, err := entity.AppendLensEvents(ws, dryRun,
		func(resolved *schema.ResolvedSchema, idx *index.Index) ([]entity.LensEvent, error) {
			if err := integrity.PlanLensVerdicts(ws, resolved, idx, verdicts); err != nil {
				return nil, err
			}
			events := make([]entity.LensEvent, 0, len(verdicts))
			for _, v := range verdicts {
				e := omap.New()
				e.Set("by", by)
				e.Set("at", canon.DateTime{ISO: at})
				e.Set("rev", v.Rev)
				e.Set("lens", v.Lens)
				e.Set("verdict", v.Verdict)
				e.Set("q", v.Q)
				if v.HasNote {
					e.Set("note", v.Note)
				}
				events = append(events, entity.LensEvent{Node: v.Node, Event: e})
			}
			return events, nil
		})
	if err != nil {
		return err
	}
	if !dryRun && len(appended) > 0 {
		warnAfterWrite(cmd, ws)
	}
	recorded := make([]any, 0, len(appended))
	for _, a := range appended {
		r := omap.New()
		r.Set("id", a.Node.Type+"/"+a.Node.Slug)
		r.Set("type", a.Node.Type)
		r.Set("slug", a.Node.Slug)
		r.Set("path", relPath(ws, a.Path))
		for _, k := range []string{"lens", "verdict", "rev", "q", "by", "at"} {
			v, _ := a.Event.Get(k)
			r.Set(k, v)
		}
		recorded = append(recorded, r)
	}
	payload := omap.New()
	payload.Set("recorded", recorded)
	if dryRun {
		payload.Set("changed", len(appended) > 0)
		payload.Set("dry_run", true)
	}
	return Emit(payload, format, func() {
		verb := "Recorded"
		if dryRun {
			verb = "Would record"
		}
		for _, a := range appended {
			lens, _ := a.Event.Get("lens")
			verdict, _ := a.Event.Get("verdict")
			fmt.Printf("%s %s on %s/%s lens %s (rev %s)\n", verb, verdict, a.Node.Type, a.Node.Slug, lens,
				get(a.Event, "rev"))
		}
		if len(appended) == 0 {
			fmt.Println("No verdicts to record")
		}
	})
}
