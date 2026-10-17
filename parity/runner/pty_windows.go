//go:build windows

package main

// pty_windows.go runs `mode: pty` steps on a Windows pseudo console (ConPTY).
//
// It is not the unix pty.go with another device underneath. A ConPTY puts the
// child on a real console, so x/term's IsTerminal (GetConsoleMode) answers
// true for its stdout and khub's isatty rung takes the terminal branch, which
// is what these cases exist to drive. But conhost re-renders the console's
// screen buffer as VT sequences rather than passing the child's bytes through,
// and a console is one device, so stdout and stderr arrive merged. The bytes
// that come out are therefore not the contract, and runCase compares a pty
// step behaviourally here (ptyBehavioural, ptymode.go): exit code, and the
// output mode of what the screen shows.
//
// The child is started with CreateProcess directly, because os/exec has no way
// to attach a PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE. Its std handles are set to
// INVALID_HANDLE_VALUE under STARTF_USESTDHANDLES: left unset, a child whose
// parent has redirected stdio (every CI runner) can inherit the parent's
// handles and write past the pseudo console. Given invalid handles, the
// console subsystem hands it the pseudo console's own.

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ptyBehavioural: a ConPTY re-renders, so pty steps are checked by behaviour.
const ptyBehavioural = true

// ptyCols and ptyRows agree with the unix winsize and with COLUMNS=80.
const ptyCols, ptyRows = 80, 25

// ptyStepTimeout bounds one pty step. No fixture step runs longer than a
// second; a step that has not exited by then is hung (a serve step in a pty
// case would be), and the suite reports it instead of hanging with it.
const ptyStepTimeout = 2 * time.Minute

// runOnPTY runs cmd on a fresh pseudo console and returns what the console
// drew as stdout (stderr merged into it; the stderr return is always empty).
func runOnPTY(cmd *exec.Cmd) (stdout, stderr []byte, code int, err error) {
	if cmd.Err != nil {
		return nil, nil, 0, cmd.Err
	}
	if cmd.Stdin != nil {
		// Bytes written to a console's input are keystrokes: conhost would echo
		// them onto the screen. No pty case feeds stdin; refuse rather than
		// guess.
		return nil, nil, 0, errors.New("a mode: pty step with stdin cannot run on a Windows pseudo console")
	}

	var inRead, inWrite, outRead, outWrite windows.Handle
	if err := windows.CreatePipe(&inRead, &inWrite, nil, 0); err != nil {
		return nil, nil, 0, fmt.Errorf("conpty input pipe: %w", err)
	}
	defer func() { _ = windows.CloseHandle(inWrite) }()
	if err := windows.CreatePipe(&outRead, &outWrite, nil, 0); err != nil {
		_ = windows.CloseHandle(inRead)
		return nil, nil, 0, fmt.Errorf("conpty output pipe: %w", err)
	}
	defer func() { _ = windows.CloseHandle(outRead) }()

	var hpc windows.Handle
	perr := windows.CreatePseudoConsole(windows.Coord{X: ptyCols, Y: ptyRows}, inRead, outWrite, 0, &hpc)
	// The pseudo console holds its own duplicates of its ends of the pipes.
	_ = windows.CloseHandle(inRead)
	_ = windows.CloseHandle(outWrite)
	if perr != nil {
		return nil, nil, 0, fmt.Errorf("CreatePseudoConsole: %w", perr)
	}
	closed := false
	closePC := func() {
		if !closed {
			closed = true
			windows.ClosePseudoConsole(hpc)
		}
	}
	defer closePC()

	// Drain the output from the start: conhost blocks writing to a full pipe,
	// and ClosePseudoConsole waits for the pipe to be drained.
	var mu sync.Mutex
	var screen bytes.Buffer
	lastOutput := time.Now()
	drained := make(chan error, 1)
	go func() {
		buf := make([]byte, 4096)
		for {
			var n uint32
			rerr := windows.ReadFile(outRead, buf, &n, nil)
			if n > 0 {
				mu.Lock()
				screen.Write(buf[:n])
				lastOutput = time.Now()
				mu.Unlock()
			}
			if rerr != nil {
				if errors.Is(rerr, windows.ERROR_BROKEN_PIPE) || errors.Is(rerr, windows.ERROR_HANDLE_EOF) {
					drained <- nil
				} else {
					drained <- rerr
				}
				return
			}
		}
	}()

	proc, err := startOnConsole(cmd, hpc)
	if err != nil {
		return nil, nil, 0, err
	}
	defer func() { _ = windows.CloseHandle(proc) }()

	ev, err := windows.WaitForSingleObject(proc, uint32(ptyStepTimeout/time.Millisecond))
	if err != nil {
		return nil, nil, 0, fmt.Errorf("waiting for the pty child: %w", err)
	}
	if ev != windows.WAIT_OBJECT_0 {
		_ = windows.TerminateProcess(proc, 1)
		return nil, nil, 0, fmt.Errorf("pty child still running after %s; killed (a serve step in a pty case never exits)", ptyStepTimeout)
	}
	exited := time.Now()
	var exit uint32
	if err := windows.GetExitCodeProcess(proc, &exit); err != nil {
		return nil, nil, 0, fmt.Errorf("pty child exit code: %w", err)
	}

	// conhost paints asynchronously: the child can be gone before its last
	// write reaches the pipe. Wait for the output to go quiet before closing,
	// or the tail of the screen can be lost. Quiet counts from the later of
	// the last output and the exit, so a child that went quiet and then
	// exited still gets its final paint.
	settleDeadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(settleDeadline) {
		mu.Lock()
		quiet := time.Since(lastOutput) > 250*time.Millisecond && time.Since(exited) > 250*time.Millisecond
		mu.Unlock()
		if quiet {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	closePC()
	select {
	case derr := <-drained:
		if derr != nil {
			return nil, nil, 0, fmt.Errorf("conpty read: %w", derr)
		}
	case <-time.After(10 * time.Second):
		return nil, nil, 0, errors.New("conpty output never closed after ClosePseudoConsole")
	}
	mu.Lock()
	defer mu.Unlock()
	return append([]byte(nil), screen.Bytes()...), nil, int(exit), nil
}

// startOnConsole creates cmd's process attached to the pseudo console hpc
// and returns its process handle.
func startOnConsole(cmd *exec.Cmd, hpc windows.Handle) (windows.Handle, error) {
	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return 0, fmt.Errorf("attribute list: %w", err)
	}
	defer attrs.Delete()
	// The attribute's value IS the HPCON (not a pointer to it). Reading it
	// through a pointer keeps the conversion inside unsafe's rules.
	if err := attrs.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE,
		*(*unsafe.Pointer)(unsafe.Pointer(&hpc)), unsafe.Sizeof(hpc)); err != nil {
		return 0, fmt.Errorf("attach pseudo console: %w", err)
	}

	var si windows.StartupInfoEx
	si.Cb = uint32(unsafe.Sizeof(si))
	si.Flags = windows.STARTF_USESTDHANDLES
	si.StdInput = windows.InvalidHandle
	si.StdOutput = windows.InvalidHandle
	si.StdErr = windows.InvalidHandle
	si.ProcThreadAttributeList = attrs.List()

	app, err := windows.UTF16PtrFromString(cmd.Path)
	if err != nil {
		return 0, err
	}
	line, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(cmd.Args))
	if err != nil {
		return 0, err
	}
	var dir *uint16
	if cmd.Dir != "" {
		if dir, err = windows.UTF16PtrFromString(cmd.Dir); err != nil {
			return 0, err
		}
	}
	block := envBlock(cmd.Env)

	var pi windows.ProcessInformation
	if err := windows.CreateProcess(app, line, nil, nil, false,
		windows.EXTENDED_STARTUPINFO_PRESENT|windows.CREATE_UNICODE_ENVIRONMENT,
		&block[0], dir, &si.StartupInfo, &pi); err != nil {
		return 0, fmt.Errorf("start %s on a pseudo console: %w", cmd.Path, err)
	}
	_ = windows.CloseHandle(pi.Thread)
	return pi.Process, nil
}

// envBlock builds a CreateProcess environment block from KEY=VALUE pairs.
// Names compare case-insensitively on Windows and the last assignment wins,
// as os/exec does; SYSTEMROOT is carried over if the list lacks it, as
// os/exec also does, because Winsock and the crypto APIs need it.
func envBlock(env []string) []uint16 {
	var keys []string
	vals := map[string]string{}
	for _, kv := range env {
		k, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		up := strings.ToUpper(k)
		if _, seen := vals[up]; !seen {
			keys = append(keys, up)
		}
		vals[up] = kv
	}
	if _, ok := vals["SYSTEMROOT"]; !ok {
		if v, err := windows.GetSystemWindowsDirectory(); err == nil {
			keys = append(keys, "SYSTEMROOT")
			vals["SYSTEMROOT"] = "SYSTEMROOT=" + v
		}
	}
	var block []uint16
	for _, k := range keys {
		block = append(block, utf16.Encode([]rune(vals[k]))...)
		block = append(block, 0)
	}
	if len(block) == 0 {
		block = append(block, 0)
	}
	return append(block, 0)
}
