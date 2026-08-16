package main

// pty.go implements `mode: pty` — the third leg of the output gate.
//
// khub's gate (cli/_render.py `is_tty`, ported in internal/cli/render.go) is a
// ladder: TTY_COMPATIBLE → FORCE_COLOR → stdout.isatty(). `pipe` and `human`
// mode drive the first rung with env vars; only a REAL terminal exercises the
// last one, and until this file existed neither implementation had ever been
// run against one.
//
// Three deliberate choices, each of which is a contract in its own right:
//
//   - Two ptys, not one. A real terminal is a single device shared by stdout
//     and stderr, but the harness records the two channels separately, so they
//     get one pty pair each. Nothing khub inspects can tell the difference:
//     the gate reads isatty(stdout) only, and Rich's Console is built on
//     sys.stdout.
//
//   - No output post-processing. A terminal in its default (cooked) mode maps
//     "\n" to "\r\n" on the way out — a transformation the TERMINAL performs,
//     not khub. Recording it would bake a tty-driver artifact into the
//     contract, and stripping it afterwards would be a normalizer that hides
//     any "\r" khub genuinely emitted. So the slave's termios has OPOST
//     cleared before the child starts, and the captured bytes are exactly the
//     bytes the process wrote. No CR normalization exists anywhere in the
//     runner, so a real "\r" would still show up as a diff.
//
//   - Stdin stays off the pty. The gate keys on stdout; leaving stdin as an
//     ordinary reader keeps EOF working (closing a pty master yields EIO, not
//     a clean EOF) and keeps the terminal from echoing a step's stdin bytes
//     back into the recorded stdout. `khub add --body - < file` in a real
//     terminal is exactly this shape.

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"github.com/creack/pty"
	"golang.org/x/term"
)

// ptyWinsize pins the terminal geometry so `os.get_terminal_size` (Rich's
// width probe, tried before COLUMNS) cannot vary with the recording machine.
// It agrees with the COLUMNS=80 that baseEnv sets for every mode.
var ptyWinsize = &pty.Winsize{Rows: 25, Cols: 80}

// runOnPTY runs cmd with stdout and stderr each attached to their own real
// pseudo-terminal, and returns what the child wrote to each.
func runOnPTY(cmd *exec.Cmd) (stdout, stderr []byte, code int, err error) {
	outMaster, outSlave, err := openTTY()
	if err != nil {
		return nil, nil, 0, err
	}
	defer outMaster.Close()
	errMaster, errSlave, err := openTTY()
	if err != nil {
		outSlave.Close()
		return nil, nil, 0, err
	}
	defer errMaster.Close()

	cmd.Stdout = outSlave
	cmd.Stderr = errSlave
	// No Setsid/Setctty: a CONTROLLING terminal is a different thing from an
	// isatty-true stdout, and nothing on the gate path asks for one — neither
	// khub nor Rich opens /dev/tty. Claiming one would also mean picking which
	// of the two ptys is "the" terminal, a fiction the harness does not need.

	if startErr := cmd.Start(); startErr != nil {
		outSlave.Close()
		errSlave.Close()
		return nil, nil, 0, startErr
	}
	// The parent's copies of the slave fds must go, or the masters never see
	// end-of-stream and the drain below blocks forever.
	outSlave.Close()
	errSlave.Close()

	var outBuf, errBuf bytes.Buffer
	var outErr, errErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); outErr = drain(outMaster, &outBuf) }()
	go func() { defer wg.Done(); errErr = drain(errMaster, &errBuf) }()
	waitErr := cmd.Wait()
	wg.Wait()

	if readErr := firstErr(outErr, errErr); readErr != nil {
		return nil, nil, 0, fmt.Errorf("pty read: %w", readErr)
	}
	code = 0
	if waitErr != nil {
		var ee *exec.ExitError
		if errors.As(waitErr, &ee) {
			code = ee.ExitCode()
		} else {
			return nil, nil, 0, waitErr
		}
	}
	return outBuf.Bytes(), errBuf.Bytes(), code, nil
}

// openTTY allocates one pty pair, pins its geometry, and takes the slave out of
// cooked mode so the driver performs no output translation (see the file
// comment). MakeRaw is the portable way to clear OPOST; the other flags it
// clears are input-side and irrelevant to a pty nothing types into.
func openTTY() (master, slave *os.File, err error) {
	master, slave, err = pty.Open()
	if err != nil {
		return nil, nil, fmt.Errorf("pty open: %w", err)
	}
	if err := pty.Setsize(master, ptyWinsize); err != nil {
		master.Close()
		slave.Close()
		return nil, nil, fmt.Errorf("pty setsize: %w", err)
	}
	if _, err := term.MakeRaw(int(slave.Fd())); err != nil {
		master.Close()
		slave.Close()
		return nil, nil, fmt.Errorf("pty raw mode: %w", err)
	}
	return master, slave, nil
}

// drain reads one pty master to end-of-stream. A master whose last slave fd has
// closed reports EIO on Linux and EOF on the BSDs; both mean "the child is
// done". Any other error is real and is surfaced rather than truncating the
// capture silently.
func drain(f *os.File, into *bytes.Buffer) error {
	buf := make([]byte, 4096)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			into.Write(buf[:n])
		}
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, syscall.EIO) {
				return nil
			}
			return err
		}
	}
}
