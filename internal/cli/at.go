// at.go is the adapter half of the point-in-time reads: the global `--at
// <ref|date>` runs a read command against the workspace as a git commit holds
// it, and `--as-of <date>` pins the date the stale flag is judged against.
// The snapshot itself is internal/gitlog's; this file scopes it to one
// invocation, keeps the writes away from it, removes it however the run ends,
// and keeps its scratch path out of everything khub prints.

package cli

import (
	"context"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/gitlog"
)

// snapshotPrefix names every scratch directory, so a sweep can find the ones
// a killed run left behind.
const snapshotPrefix = "khub-at-"

// snapshotSweepAge is how old an orphaned scratch directory must be before a
// later --at run removes it: far past any run still reading its own.
const snapshotSweepAge = 24 * time.Hour

var (
	// snapMu guards scratch and snapshot against the signal handler: it is
	// held while the snapshot is written, so the handler removes the
	// directory only once nothing is writing into it.
	snapMu  sync.Mutex
	scratch string // the scratch directory, from creation until removal
	// scrubbed is the scratch directory's path for scrubSnapshot. It outlives
	// the directory, so an error raised after removal is still rewritten.
	scrubbed string
	snapshot *gitlog.Snapshot
	sigStop  func()
)

// readsAt lists the commands that run under --at: every one that only reads
// the workspace. Anything else writes (add, edit, reindex, schema snapshot …),
// serves the live tree (serve), or writes a file the scratch tree would
// swallow (viz).
var readsAt = map[string]bool{
	"khub status":       true,
	"khub get":          true,
	"khub query":        true,
	"khub search":       true,
	"khub neighbors":    true,
	"khub impact":       true,
	"khub history":      true,
	"khub validate":     true,
	"khub check":        true,
	"khub stale":        true,
	"khub schema":       true,
	"khub schema types": true,
	"khub schema show":  true,
	"khub schema base":  true,
	"khub schema edges": true,
	"khub schema diff":  true,
}

// refuseWritesAt is the root's PersistentPreRunE: under --at, a command
// outside readsAt refuses with read_only_at before anything runs.
func refuseWritesAt(cmd *cobra.Command, _ []string) error {
	if state.At == "" || readsAt[cmd.CommandPath()] {
		return nil
	}
	format := ""
	if f := cmd.Flags().Lookup("format"); f != nil {
		format = f.Value.String()
	}
	e := errs.ReadOnlyAt(cmd.CommandPath())
	return Fail(e.Message, e.Code, format)
}

// snapshotRoot materializes the --at commit once per invocation. The live
// workspace is found exactly as without --at (-C and the walk up to .khub/),
// and the snapshot takes the same directory within the repository.
func snapshotRoot() (string, error) {
	if snapshot != nil {
		return snapshot.Root, nil
	}
	live, err := liveRoot()
	if err != nil {
		return "", err
	}
	sweepSnapshots()
	dir, err := os.MkdirTemp("", snapshotPrefix)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithCancel(context.Background())
	snapMu.Lock()
	scratch, scrubbed = dir, dir
	watchSignals(cancel)
	snap, err := gitlog.Materialize(ctx, live, state.At, dir)
	if err == nil {
		snapshot = snap
	}
	snapMu.Unlock()
	if err != nil {
		closeSnapshot()
		return "", err
	}
	return snap.Root, nil
}

// watchSignals removes the scratch directory on SIGINT or SIGTERM and exits
// the way the signal would have: 130 or 143. The deferred cleanup in Execute
// never runs on a signal, so without this a killed --at run leaves its copy
// behind. Called with snapMu held.
func watchSignals(cancel context.CancelFunc) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})
	sigStop = func() {
		signal.Stop(ch)
		close(done)
	}
	go func() {
		select {
		case sig := <-ch:
			cancel() // stop the snapshot's git calls; Materialize then returns
			snapMu.Lock()
			if scratch != "" {
				_ = os.RemoveAll(scratch)
			}
			code := 130
			if sig == syscall.SIGTERM {
				code = 143
			}
			os.Exit(code)
		case <-done:
		}
	}()
}

// closeSnapshot removes the scratch tree, if one was made.
func closeSnapshot() {
	snapMu.Lock()
	defer snapMu.Unlock()
	if scratch != "" {
		_ = os.RemoveAll(scratch)
	}
	scratch, snapshot = "", nil
	if sigStop != nil {
		sigStop()
		sigStop = nil
	}
}

// sweepSnapshots removes scratch directories a killed run (SIGKILL, a power
// loss) left in the temp directory, once they are a day old.
func sweepSnapshots() {
	tmp := os.TempDir()
	entries, err := os.ReadDir(tmp)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), snapshotPrefix) {
			continue
		}
		info, err := e.Info()
		if err != nil || time.Since(info.ModTime()) < snapshotSweepAge {
			continue
		}
		_ = os.RemoveAll(filepath.Join(tmp, e.Name()))
	}
}

// history is where `stale` reads commit dates: the live HEAD, or under --at
// the live repository up to the snapshot's commit.
func history(ws string) gitlog.History {
	if snapshot != nil {
		return snapshot.History()
	}
	return gitlog.Head(ws)
}

// scrubSnapshot rewrites the scratch directory out of text khub prints: a
// path inside it reads `<ref>:<workspace-relative path>`, the way git names a
// file in a commit. Every spelling of the directory is replaced: as made, with
// symlinks resolved (macOS's /var is /private/var), with forward slashes, and
// JSON-escaped.
func scrubSnapshot(text string) string { return scrubAs(text, state.At+":") }

// scrubSnapshotJSON is scrubSnapshot over an encoded JSON document: the ref
// goes in JSON-escaped, so a quote or backslash in it cannot break the
// document.
func scrubSnapshotJSON(doc string) string {
	ref, err := canon.EncodeCLI(state.At + ":")
	if err != nil || len(ref) < 2 {
		return doc
	}
	return scrubAs(doc, ref[1:len(ref)-1])
}

func scrubAs(text, ref string) string {
	if scrubbed == "" || text == "" {
		return text
	}
	forms := []string{scrubbed}
	if real, err := filepath.EvalSymlinks(filepath.Dir(scrubbed)); err == nil {
		if real = filepath.Join(real, filepath.Base(scrubbed)); real != scrubbed {
			forms = append(forms, real)
		}
	}
	for _, f := range forms {
		for _, spelling := range []string{f, filepath.ToSlash(f), strings.ReplaceAll(f, `\`, `\\`)} {
			for _, sep := range []string{"/", `\`, `\\`} {
				text = strings.ReplaceAll(text, spelling+sep, ref)
			}
			text = strings.ReplaceAll(text, spelling, ref+".")
		}
	}
	return text
}
