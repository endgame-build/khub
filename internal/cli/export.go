// export.go is `khub export --okf <dir>`: write the workspace as an OKF 0.2
// bundle another tool can read.

package cli

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/endgame-build/khub/internal/export"
	"github.com/endgame-build/khub/internal/omap"
)

func registerExport(root *cobra.Command) {
	var format, okf string
	var force, dryRun bool
	cmd := newCmd("export", "Write the workspace as an OKF 0.2 bundle: khub export --okf DIR.",
		"", func(cmd *cobra.Command, args []string) error {
			if okf == "" {
				return clickUsageErr(cmd.CommandPath(), "", "Missing option '--okf'.")
			}
			return Guard(format, func() error {
				ws, err := resolveRoot()
				if err != nil {
					return err
				}
				res, err := export.Export(ws, okf, export.Options{Force: force, DryRun: dryRun})
				if err != nil {
					return err
				}
				target := filepath.ToSlash(okf)
				payload := omap.New()
				payload.Set("path", target)
				payload.Set("concepts", strList(res.Concepts))
				payload.Set("indexes", strList(res.Indexes))
				payload.Set("okf_version", export.OKFVersion)
				payload.Set("skipped_strays", strList(res.SkippedStrays))
				payload.Set("cleanup_left", strList(res.CleanupLeft))
				payload.Set("dry_run", dryRun)
				return Emit(payload, format, func() {
					if dryRun {
						fmt.Printf("dry run — would write %d concepts and %d index files to %s\n",
							len(res.Concepts), len(res.Indexes), target)
						for _, p := range append(res.Concepts, res.Indexes...) {
							fmt.Println("  " + p)
						}
						for _, p := range res.SkippedStrays {
							fmt.Printf("  skipped stray %s\n", p)
						}
						return
					}
					fmt.Printf("Exported %d concepts and %d index files to %s (OKF %s)\n",
						len(res.Concepts), len(res.Indexes), target, export.OKFVersion)
					for _, p := range res.SkippedStrays {
						fmt.Printf("  skipped stray %s (its type names another type; not an entity)\n", p)
					}
					for _, p := range res.CleanupLeft {
						fmt.Printf("  could not remove the replaced bundle %s; delete it by hand\n", p)
					}
				})
			})
		})
	cmd.Args = clickArity(0)
	cmd.Long = cmd.Short + "\n\nOne md concept per entity at a path mirroring the workspace; json, yaml and " +
		"collection rows are materialized as md. Declared relations become a `# Related` section of " +
		"bundle-absolute links. Every directory gets an index.md, and the root one declares okf_version. " +
		"Only entities are exported: .khub/, agent files and ignored paths stay out, and strays are listed. " +
		"The bundle is built beside the target and swapped in, so a failed export changes nothing. A " +
		".khub-export file at the bundle root lists the files the export wrote. The target must be empty " +
		"or absent, and must not overlap the workspace. --force replaces a target only when it holds that " +
		"file, holds no .khub/ and no .git/, and holds nothing the file does not list."
	cmd.Flags().StringVar(&okf, "okf", "", "The bundle directory to write (required).")
	cmd.Flags().BoolVar(&force, "force", false,
		"Replace a bundle khub exported before, when it holds only the files its .khub-export lists.")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "List the files the bundle would hold and write nothing.")
	cmd.Flags().StringVar(&format, "format", "text", "text or json (the written paths).")
	root.AddCommand(cmd)
}
