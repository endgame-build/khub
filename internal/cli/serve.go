// serve.go adapts `khub serve` — the live, read-only sibling of viz. It prints
// prose and takes no --format, like the other operator commands; only the
// shared error boundary emits JSON.

package cli

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/serve"
)

func registerServe(root *cobra.Command) {
	var port int
	var aspect string
	cmd := newCmd("serve",
		"Serve a read-only graph view on loopback: khub serve [--port 7777] [--aspect A].",
		"", func(cmd *cobra.Command, args []string) error {
			// A usage error, so it answers before the TTY gate: the call is
			// wrong whatever stdout is.
			if cmd.Flags().Changed("aspect") {
				if err := checkAspect(cmd, aspect); err != nil {
					return err
				}
			}
			return Guard("text", func() error {
				// serve blocks until interrupted. An agent that runs it in a
				// foreground call hangs until its own timeout, which is the
				// failure the no-prompts rule exists to prevent — so refuse
				// rather than hang, and name the escape.
				if !IsTTY() {
					return errs.ServeNeedsTTY()
				}
				ws, err := resolveRoot()
				if err != nil {
					return err
				}
				ln, err := serve.Listen(port)
				if err != nil {
					return err
				}
				// The bound port, not the requested one: --port 0 takes an
				// ephemeral port and the printed URL still has to work.
				bound := port
				if addr, ok := ln.Addr().(*net.TCPAddr); ok {
					bound = addr.Port
				}
				// The aspect rides on the URL rather than in server state: the
				// page reads it once at load, so the link the operator copies
				// or bookmarks reopens the same view, and the server stays a
				// function of the request alone.
				page := "/"
				if cmd.Flags().Changed("aspect") {
					page = "/?aspect=" + aspect
				}
				fmt.Printf("Serving %s at http://127.0.0.1:%d%s (ctrl-c to stop)\n", filepath.ToSlash(ws), bound, page)

				ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
				defer stop()
				return serve.Serve(ctx, ln, ws)
			})
		})
	cmd.Args = clickArity(0)
	cmd.Flags().IntVar(&port, "port", serve.DefaultPort,
		"Port to bind on 127.0.0.1; 0 takes an ephemeral one.")
	cmd.Flags().StringVar(&aspect, "aspect", "",
		"View to open on: instances, schema, or both. Default: the schema view.")
	root.AddCommand(cmd)
}
