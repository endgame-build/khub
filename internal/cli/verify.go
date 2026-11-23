// verify.go is `khub verify`: record a review of an entity as an OKF
// `verified` event, optionally proven by a signed commit.

package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/endgame-build/khub/internal/entity"
	"github.com/endgame-build/khub/internal/gitlog"
	"github.com/endgame-build/khub/internal/omap"
)

func registerVerify(root *cobra.Command) {
	var format, by, at string
	var commit bool
	cmd := newCmd("verify [ID]", "Record a review: khub verify ad-2026-01-15-use-jwt --by human:ilya.",
		"[ID]", func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return missingArgument(cmd.CommandPath(), "[ID]", "ID")
			}
			// No default actor: a khub-stamped review would mint a tier nobody asserted.
			if by == "" {
				return clickUsageErr(cmd.CommandPath(), "[ID]", "Missing option '--by'.")
			}
			return Guard(format, func() error {
				ws, err := resolveRoot()
				if err != nil {
					return err
				}
				// --commit is all or nothing: everything git needs is checked before
				// the write, and the commit runs under the workspace lock, which
				// restores the file if the commit still fails.
				var sha any
				var publish func(path string) error
				if commit {
					view, err := entity.Get(ws, args[0], false)
					if err != nil {
						return err
					}
					if err := gitlog.CommitPreflight(ws, relPath(ws, view.Path), view.Locator != ""); err != nil {
						return err
					}
					publish = func(path string) error {
						s, err := gitlog.CommitSigned(ws, relPath(ws, path),
							fmt.Sprintf("verify %s/%s by %s", view.Type, view.Slug, by))
						sha = s
						return err
					}
				}
				res, err := entity.Verify(ws, args[0], by, at, publish)
				if err != nil {
					return err
				}
				record := omap.New()
				record.Set("id", res.Type+"/"+res.Slug)
				record.Set("type", res.Type)
				record.Set("slug", res.Slug)
				record.Set("path", relPath(ws, res.Path))
				if res.Locator != "" {
					record.Set("locator", res.Locator)
				}
				record.Set("by", res.By)
				record.Set("at", res.At)
				record.Set("rev", res.Rev)
				record.Set("commit", sha)
				return Emit(record, format, func() {
					fmt.Printf("Verified %s/%s by %s at %s (rev %s)\n", res.Type, res.Slug, res.By, res.At, res.Rev)
					if sha != nil {
						fmt.Printf("Committed %s, signed\n", sha)
					}
				})
			})
		})
	cmd.Args = clickArity(1)
	cmd.Flags().StringVar(&by, "by", "", "Who reviewed it (required): human:<name>, process:<name>, or <producer>[/<version>].")
	cmd.Flags().StringVar(&at, "at", "", "When, as ISO 8601 with a T and an offset; default now.")
	cmd.Flags().BoolVar(&commit, "commit", false, "Also make a signed git commit of the entity's file; refused when signing is not configured.")
	cmd.Flags().StringVar(&format, "format", "text", "text or json (emits the recorded event).")
	root.AddCommand(cmd)
}
