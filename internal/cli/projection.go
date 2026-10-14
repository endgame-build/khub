// projection.go ports cli/projection_cmd.py: the derived projection writers.
// Neither gates — both are no-op-safe reads that write a derived, disposable
// artifact, so an empty workspace is a success. Neither takes --format either:
// they print prose, and only the shared error boundary emits JSON.

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/endgame-build/khub/internal/reindex"
	"github.com/endgame-build/khub/internal/viz"
)

func registerReindex(root *cobra.Command) {
	var dryRun bool
	cmd := newCmd("reindex", "Regenerate the OKF index.md from the graph: khub reindex [--dry-run].",
		"", func(cmd *cobra.Command, args []string) error {
			return Guard("text", func() error {
				ws, err := resolveRoot()
				if err != nil {
					return err
				}
				result, err := reindex.Reindex(ws, dryRun)
				if err != nil {
					return err
				}
				if dryRun {
					// Name what the preview could not see. The diff below is
					// derived from an incomplete graph, so showing it without
					// this line would imply the index is fine to write — and a
					// real reindex will still refuse.
					if len(result.Malformed) > 0 {
						shown := result.Malformed
						more := ""
						if len(shown) > 3 {
							more = fmt.Sprintf(" (+%d more)", len(shown)-3)
							shown = shown[:3]
						}
						fmt.Fprintf(os.Stderr,
							"Previewing over %d unparseable file(s) — %s%s. "+
								"They contribute nothing to this diff, and `khub reindex` "+
								"will refuse until they parse. Run `khub check` for the list.\n",
							len(result.Malformed), strings.Join(shown, ", "), more)
					}
					// An empty diff means the index already matches; say so rather
					// than print nothing.
					if result.Diff != "" {
						_, werr := fmt.Fprint(os.Stdout, result.Diff)
						return werr
					}
					fmt.Println("index.md is up to date")
					return nil
				}
				if result.Count > 0 {
					fmt.Printf("Reindexed %d entities into index.md\n", result.Count)
				} else {
					fmt.Println("Reindexed 0 entities")
				}
				return nil
			})
		})
	cmd.Args = clickArity(0)
	cmd.Flags().BoolVar(&dryRun, "dry-run", false,
		"Print the diff against the current index.md and write nothing.")
	root.AddCommand(cmd)
}

func registerViz(root *cobra.Command) {
	var out, typeName string
	var open bool
	cmd := newCmd("viz",
		"Render the typed graph to a self-contained Cytoscape HTML: khub viz [--out F] [--open] [--type T].",
		"", func(cmd *cobra.Command, args []string) error {
			return Guard("text", func() error {
				ws, err := resolveRoot()
				if err != nil {
					return err
				}
				result, err := viz.Viz(ws, out, optString(cmd, "type", typeName))
				if err != nil {
					return err
				}
				fmt.Printf("Wrote %s (%d nodes, %d edges)\n", filepath.ToSlash(out), result.Nodes, result.Edges)
				if open {
					// A file:// URI, not a bare path — the openers treat a
					// scheme-less path as relative/invalid.
					openBrowser(fileURI(result.Path))
				}
				return nil
			})
		})
	cmd.Args = clickArity(0)
	cmd.Flags().StringVar(&out, "out", viz.DefaultOut, "Output path for the HTML (default viz.html).")
	cmd.Flags().BoolVar(&open, "open", false, "Open the written file in the default browser.")
	cmd.Flags().StringVar(&typeName, "type", "", "Render only that type and its incident edges.")
	root.AddCommand(cmd)
}

func fileURI(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	return slashedFileURI(filepath.ToSlash(abs), runtime.GOOS)
}

// slashedFileURI prefixes the file scheme. A Windows drive path (`C:/x`) has
// no leading slash, and the URI needs one before the drive for an empty
// authority: file:///C:/x. A UNC path (`//host/share`) already starts with a
// slash and is left as browsers accept it.
func slashedFileURI(slashed, goos string) string {
	if goos == "windows" && !strings.HasPrefix(slashed, "/") {
		slashed = "/" + slashed
	}
	return "file://" + slashed
}

// openBrowser is webbrowser.open: best effort, and never a failure of the
// render that already succeeded.
func openBrowser(uri string) {
	switch runtime.GOOS {
	case "darwin":
		_ = exec.Command("open", uri).Start()
	case "windows":
		// The URL handler start and webbrowser.open use, without cmd.exe's
		// quoting rules between khub and the path.
		_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", uri).Start()
	default:
		_ = exec.Command("xdg-open", uri).Start()
	}
}
