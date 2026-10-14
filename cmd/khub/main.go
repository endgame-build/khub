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
	// BrokenPipeError rather than rendering it). Notify turns the kill into a
	// returned EPIPE that Guard can recognise.
	//
	// Notify, not Ignore: Ignore sets SIG_IGN, which survives exec, so spawned
	// children (`git` in gitlog, the viz opener) would inherit an ignored
	// SIGPIPE. A Go handler is reset to SIG_DFL on exec — same EPIPE behavior
	// for khub itself, clean signal disposition for children.
	//
	// Windows has no SIGPIPE: the runtime never raises it there and this
	// Notify is inert. A write to a closed pipe already returns an error
	// (ERROR_NO_DATA or ERROR_BROKEN_PIPE), which Guard treats as EPIPE.
	signal.Notify(make(chan os.Signal, 1), syscall.SIGPIPE)
	// Windows consoles render the human view's ANSI colours only once asked
	// to; a no-op elsewhere.
	cli.PrepareConsole()
	os.Exit(cli.Execute(os.Args[1:]))
}
