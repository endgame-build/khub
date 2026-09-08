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
	"syscall"

	"github.com/spf13/cobra"

	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/serve"
)

func registerServe(root *cobra.Command) {
	var port int
	cmd := newCmd("serve",
		"Serve a read-only graph view on loopback: khub serve [--port 7777].",
		"", func(cmd *cobra.Command, args []string) error {
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
				fmt.Printf("Serving %s at http://127.0.0.1:%d (ctrl-c to stop)\n", ws, bound)

				ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
				defer stop()
				return serve.Serve(ctx, ln, ws)
			})
		})
	cmd.Args = clickArity(0)
	cmd.Flags().IntVar(&port, "port", serve.DefaultPort,
		"Port to bind on 127.0.0.1; 0 takes an ephemeral one.")
	root.AddCommand(cmd)
}
