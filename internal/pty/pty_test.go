package pty

import (
	"bytes"
	"fmt"
	"runtime"
	"testing"
	"time"
)

// The marker below is deliberately built by the child from two separate
// fragments. A pseudo-terminal echoes typed input back to the reader, so a
// test that searched for a token appearing verbatim in the command would pass
// on the echo alone and prove nothing. Splitting the marker means the echoed
// command text can never contain the string we wait for, so a match is real
// evidence that the child actually executed it.
const (
	markerPrefix = "SCRIPT_CENTER_"
	markerSuffix = "PONG"
	marker       = markerPrefix + markerSuffix
)

// readTimeout bounds every read in this file. Generous, because CI runners are
// slow, but short enough that a genuine hang fails the suite rather than
// stalling it.
const readTimeout = 30 * time.Second

// TestPTYRoundTrip is the guard against a silent loss of pseudo-terminal
// support on a platform. It proves three things together: a pseudo-terminal can
// be allocated, a child process can be attached to it, and input written by
// the parent is received and acted on by the child.
//
// A library with no implementation for the current platform may still compile
// and may still report no error at construction time, in which case this test
// is the only thing standing between that and a release where no interactive
// prompt works. It must therefore run on every supported platform.
func TestPTYRoundTrip(t *testing.T) {
	s := startShell(t)

	if _, err := fmt.Fprintf(s, "%s%s", echoMarker(), LineEnding()); err != nil {
		t.Fatalf("write to pty: %v", err)
	}

	out, err := readUntil(s, marker, readTimeout)
	if err != nil {
		t.Fatalf("child never produced %q: %v\ngot: %q", marker, err, out)
	}
}

// TestPTYIsRealTerminal checks that the child genuinely sees a terminal rather
// than a redirected pipe. Programs routinely call isatty and change or disable
// their behavior when it is false, so attaching a pipe would produce a runner
// that looks functional in tests and breaks on credential prompts.
func TestPTYIsRealTerminal(t *testing.T) {
	s := startShell(t)

	if _, err := fmt.Fprintf(s, "%s%s", isTTY(), LineEnding()); err != nil {
		t.Fatalf("write to pty: %v", err)
	}

	// Unix: test -t 0 && echo ISATTY || echo NOTATTY
	// Windows: [Console]::IsInputRedirected is True only when input is a pipe,
	// so a console-attached child prints False.
	want, unwanted := "ISATTY", "NOTATTY"
	if runtime.GOOS == "windows" {
		want, unwanted = "False", "True"
	}

	out, err := readUntil(s, want, readTimeout)
	if err != nil {
		if bytes.Contains([]byte(out), []byte(unwanted)) {
			t.Fatalf("child reports input is redirected (%s): "+
				"the pty degraded to a pipe, so prompts will not work", unwanted)
		}
		t.Fatalf("child never reported a real terminal (want %q): %v\ngot: %q", want, err, out)
	}
}

// TestPTYResize covers the resizing the console pane depends on. On Unix this
// delivers SIGWINCH and on Windows it forces a full screen resend, so it is a
// real cross-platform code path rather than a no-op.
func TestPTYResize(t *testing.T) {
	s := startShell(t)

	for _, sz := range []struct{ rows, cols uint16 }{
		{40, 120},
		{10, 40},
		{24, 80},
	} {
		size := DefaultSize
		size.Rows, size.Cols = sz.rows, sz.cols
		if err := s.Resize(size); err != nil {
			t.Fatalf("resize to %dx%d: %v", sz.cols, sz.rows, err)
		}
	}
}

// TestPTYExitCode covers the status reporting every run result depends on. A
// child that exits 7 must surface as 7, not as a generic success.
func TestPTYExitCode(t *testing.T) {
	const want = 7
	s := mustStart(t, Config{Argv: exitCodeArgv(want)})

	got := s.Wait()
	if got != want {
		t.Fatalf("Wait() = %d, want %d", got, want)
	}
}

// TestPTYCloseTerminatesChild covers cancellation: closing the session must
// stop the child rather than leaving it orphaned. Without this, canceling a
// run would leak a process for every interrupted script.
//
// It checks whether the child is still running rather than what it exited with,
// because the exit code is not a sound proxy for whether it was stopped. On
// Unix a killed child reports a signal and the code is meaningful. On Windows
// crosspty first closes the input pipe to ask the child to exit on its own, and
// only kills the job object if the child has not gone within its kill delay.
// The graceful path yields the control-c exit code and the forced path yields
// KillExitCode, which defaults to 0. A test asserting a non-zero code therefore
// passed or failed depending on which teardown path won the race, which is how
// this test came to fail on roughly half of all Windows CI runs while passing
// every time on Unix.
func TestPTYCloseTerminatesChild(t *testing.T) {
	s := mustStart(t, Config{Argv: longRunningArgv()})

	pid := s.Pid()
	if pid <= 0 {
		t.Fatalf("Pid() = %d, want a positive process id", pid)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Wait blocks until the child has been reaped, so once it returns the
	// process is definitively gone and there is no need to poll for it. A
	// Close that failed to stop the child would leave this blocking, and the
	// 10 minute go test timeout would report the hang.
	s.Wait()

	if processAlive(pid) {
		t.Fatalf("process %d is still running after Close, so canceling a run would leak it", pid)
	}
}

// TestProcessAliveTellsLiveAndDeadApart is the control for
// TestPTYCloseTerminatesChild. That test only ever asks about a process that
// has already gone, so a processAlive that was simply always false would make it
// pass without proving anything, on every platform, forever. This one asks both
// ways about the same child: it must report a running child as alive, so a false
// negative is caught here rather than silently hollowing out the test above.
func TestProcessAliveTellsLiveAndDeadApart(t *testing.T) {
	s := mustStart(t, Config{Argv: longRunningArgv()})

	pid := s.Pid()
	if pid <= 0 {
		t.Fatalf("Pid() = %d, want a positive process id", pid)
	}

	if !processAlive(pid) {
		t.Fatalf("process %d is running but processAlive said it was gone", pid)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	s.Wait()

	if processAlive(pid) {
		t.Fatalf("process %d has exited but processAlive said it was running", pid)
	}
}

func TestStartRejectsEmptyArgv(t *testing.T) {
	if _, err := Start(Config{}); err == nil {
		t.Fatal("Start with empty Argv returned no error")
	}
}

// startShell launches a bare interactive interpreter and returns a session
// registered for cleanup.
func startShell(t *testing.T) *Session {
	t.Helper()
	return mustStart(t, Config{Argv: Shell()})
}

func mustStart(t *testing.T, cfg Config) *Session {
	t.Helper()
	s, err := Start(cfg)
	if err != nil {
		t.Fatalf("Start(%v): %v", cfg.Argv, err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// exitCodeArgv returns an argv that exits immediately with the given code.
func exitCodeArgv(code int) []string {
	if runtime.GOOS == "windows" {
		return []string{"cmd.exe", "/C", fmt.Sprintf("exit %d", code)}
	}
	return []string{"/bin/sh", "-c", fmt.Sprintf("exit %d", code)}
}

// longRunningArgv returns an argv that stays alive until it is signaled.
func longRunningArgv() []string {
	if runtime.GOOS == "windows" {
		return []string{"cmd.exe", "/C", "ping -n 120 127.0.0.1 >NUL"}
	}
	return []string{"/bin/sh", "-c", "while :; do sleep 1; done"}
}

// readUntil reads from s until want appears in the accumulated output, and
// returns everything read. It fails the test if that does not happen in time or
// the pty reports an error first.
func readUntil(s *Session, want string, timeout time.Duration) (string, error) {
	type read struct {
		data []byte
		err  error
	}
	// crosspty exposes no read deadline, so the read runs on its own goroutine
	// and the timeout is enforced by selecting. Close interrupts an in-flight
	// read, which is what unblocks this goroutine on the timeout path.
	ch := make(chan read, 1)
	go func() {
		var all []byte
		buf := make([]byte, 4096)
		for {
			n, err := s.Read(buf)
			all = append(all, buf[:n]...)
			if bytes.Contains(all, []byte(want)) {
				ch <- read{data: all}
				return
			}
			if err != nil {
				ch <- read{data: all, err: err}
				return
			}
		}
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case r := <-ch:
		return string(r.data), r.err
	case <-timer.C:
		// Closing is safe here and is what releases the reader goroutine.
		_ = s.Close()
		return "", fmt.Errorf("timed out after %s waiting for %q", timeout, want)
	}
}

// echoMarker is the command that makes a child print marker. The marker is
// assembled from two fragments so that the command's own echoed text cannot
// contain it.
func echoMarker() string {
	if runtime.GOOS == "windows" {
		return fmt.Sprintf("Write-Output ('%s' + '%s')", markerPrefix, markerSuffix)
	}
	return fmt.Sprintf("printf '%s%%s\\n' '%s'", markerPrefix, markerSuffix)
}

// isTTY is the command that reports whether the child sees a real terminal.
func isTTY() string {
	if runtime.GOOS == "windows" {
		return "Write-Output ([Console]::IsInputRedirected)"
	}
	return "test -t 0 && echo ISATTY || echo NOTATTY"
}
