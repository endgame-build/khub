// importcmd.go is `khub import --okf <bundle>`: read an OKF bundle into the
// workspace as drafts. (The file is not import.go: `import` is a Go keyword
// and reads badly as a file name beside it.)

package cli

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/spf13/cobra"

	"github.com/endgame-build/khub/internal/okfimport"
	"github.com/endgame-build/khub/internal/omap"
)

func registerImport(root *cobra.Command) {
	var format, okf, typeMap, by string
	var dryRun bool
	cmd := newCmd("import", "Read an OKF bundle into the workspace as drafts: khub import --okf DIR.",
		"", func(cmd *cobra.Command, args []string) error {
			if okf == "" {
				return clickUsageErr(cmd.CommandPath(), "", "Missing option '--okf'.")
			}
			return Guard(format, func() error {
				ws, err := resolveRoot()
				if err != nil {
					return err
				}
				res, err := okfimport.Import(ws, okf, okfimport.Options{TypeMap: typeMap, Actor: by, DryRun: dryRun})
				if err != nil {
					return err
				}
				return Emit(importPayload(filepath.ToSlash(okf), res, dryRun), format, func() {
					verb := "Imported"
					if dryRun {
						fmt.Println("dry run — planned outcomes; workspace unchanged")
						verb = "Would import"
					}
					// Concept ids and parser messages come from the bundle, which
					// is untrusted: nothing in them reaches the terminal as a
					// control sequence.
					fmt.Printf("%s %d concepts from %s as drafts; skipped %d\n",
						verb, len(res.Imported), printable(filepath.ToSlash(okf)), len(res.Skipped))
					for _, s := range res.Skipped {
						fmt.Printf("  skipped %s (%s): %s\n", printable(s.Concept), s.Reason, printable(s.Message))
					}
					for _, u := range res.Unresolved {
						fmt.Printf("  unresolved %s %s: %s (kept in okf_unresolved)\n",
							printable(u.Concept), u.Field, printable(u.Value))
					}
					fmt.Printf("links: %d resolved, %d broken, %d external; %d references added\n",
						res.Links.Resolved, res.Links.Broken, res.Links.External, res.ReferencesAdded)
				})
			})
		})
	cmd.Args = clickArity(0)
	cmd.Long = cmd.Short + "\n\nEvery `.md` concept in the bundle except index.md and log.md lands as a draft in " +
		"the type its `type` names, matched by name or slug, or mapped with --type-map. Frontmatter is kept " +
		"as written, unknown keys included. The id is the type's scheme around the file's stem, as add mints " +
		"one, with the stem kept in `aliases` unless another entity already answers to it. The bundle's " +
		"`verified` is kept as `okf_verified` and never counted, and a relation value nothing answers to " +
		"is kept in `okf_unresolved`; a concept already carrying the key import writes is skipped. A link " +
		"to another imported concept becomes a `references` edge; export's `# Related` section is read back " +
		"into relations. A concept that cannot land is skipped and reported. The concepts that land are " +
		"written all or none, and nothing is written to the bundle."
	cmd.Flags().StringVar(&okf, "okf", "", "The bundle directory to read (required).")
	cmd.Flags().StringVar(&typeMap, "type-map", "", "A YAML file mapping OKF types to this workspace's types.")
	cmd.Flags().StringVar(&by, "by", "",
		"Who `generated.by` records on a concept that has no `generated`. Default: KHUB_ACTOR, then agent/claude-code under Claude Code, then khub.")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Plan the import and write nothing.")
	cmd.Flags().StringVar(&format, "format", "text", "text or json (every imported and skipped concept).")
	root.AddCommand(cmd)
}

// importPayload is the JSON contract: what landed, what did not and why, and
// what the links resolved to.
func importPayload(bundle string, res *okfimport.Result, dryRun bool) *omap.Map {
	payload := omap.New()
	payload.Set("bundle", bundle)
	imported := make([]any, 0, len(res.Imported))
	for _, c := range res.Imported {
		rec := omap.New()
		rec.Set("concept", c.Concept)
		rec.Set("id", c.ID)
		rec.Set("path", c.Path)
		rec.Set("okf_type", c.OKFType)
		rec.Set("alias", nullable(c.Alias))
		rec.Set("alias_dropped", nullable(c.AliasDropped))
		rec.Set("status_was", nullable(c.StatusWas))
		rec.Set("verified_quarantined", c.VerifiedQuarantined)
		imported = append(imported, rec)
	}
	payload.Set("imported", imported)
	skipped := make([]any, 0, len(res.Skipped))
	for _, s := range res.Skipped {
		rec := omap.New()
		rec.Set("concept", s.Concept)
		rec.Set("reason", s.Reason)
		rec.Set("message", s.Message)
		skipped = append(skipped, rec)
	}
	payload.Set("skipped", skipped)
	unresolved := make([]any, 0, len(res.Unresolved))
	for _, u := range res.Unresolved {
		rec := omap.New()
		rec.Set("concept", u.Concept)
		rec.Set("field", u.Field)
		rec.Set("value", u.Value)
		unresolved = append(unresolved, rec)
	}
	payload.Set("unresolved", unresolved)
	payload.Set("references_added", res.ReferencesAdded)
	links := omap.New()
	links.Set("resolved", res.Links.Resolved)
	links.Set("broken", res.Links.Broken)
	links.Set("external", res.Links.External)
	payload.Set("links", links)
	payload.Set("dry_run", dryRun)
	return payload
}

// printable drops control characters, so text from an untrusted bundle cannot
// steer the terminal.
func printable(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}
