// khub — schema-bound, agent-facing context management.
package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/endgame-build/khub/internal/cli"
)

func main() {
	// Go's runtime raises SIGPIPE and terminates when stdout/stderr hit EPIPE.
	// khub must instead see the write error and treat it as a non-failure
	// (`khub schema | head` is not an error — cli/_render.py re-raises
	// BrokenPipeError rather than rendering it). Ignoring the signal turns the
	// kill into a returned EPIPE that Guard can recognise.
	signal.Ignore(syscall.SIGPIPE)
	os.Exit(cli.Execute(os.Args[1:]))
}
