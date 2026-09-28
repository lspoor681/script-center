// Package elevate wraps the platform's way of starting a command with the
// user's administrator privileges.
//
// A script that asks to run elevated can stay inside the app's pseudo-terminal
// when the platform has a tool for doing that in place (sudo on Unix, Windows
// sudo in inline mode), so its output keeps streaming and its input stays
// reachable. Where no such tool is configured, the command is started elevated
// in a separate window instead: it runs, but nothing it prints reaches the app
// and nothing typed here reaches it.
package elevate

import (
	"errors"
	"fmt"
)

// ErrUnavailable reports that a command cannot be started elevated in a way
// that works on this machine. The wrapped message says what to install or
// enable.
var ErrUnavailable = errors.New("elevate: running with administrator privileges is not available")

// Status describes how an elevated command can be started on this machine.
type Status struct {
	// Available says a command can be started elevated at all.
	Available bool
	// Inline says the elevated command stays attached to the app's terminal.
	// When false and Available is true, the command runs in its own window,
	// out of the app's reach.
	Inline bool
	// Method names the tool that does the elevation, such as "sudo" or
	// "runas", for messages that mention it.
	Method string
	// Hint explains what to install or enable when Available or Inline is
	// false.
	Hint string

	// prefix is the argv to put before the command when Inline, such as
	// {"sudo"}.
	prefix []string
}

// detect is the platform probe, a variable so tests can pin the outcome
// without depending on the machine they run on.
var detect = platformProbe

// Detect reports how an elevated command can be started on this machine.
func Detect() Status { return detect() }

// Wrap puts the elevation tool in front of argv for an inline elevated run. It
// fails when Detect did not report an inline run, in which case the command
// has to start in its own window (StartElevated) instead.
func Wrap(argv []string) ([]string, error) {
	status := detect()
	if !status.Available {
		return nil, fmt.Errorf("%w: %s", ErrUnavailable, status.Hint)
	}
	if !status.Inline {
		return nil, fmt.Errorf("%w: %s can only run in its own window, outside this app", ErrUnavailable, status.Method)
	}
	wrapped := append([]string(nil), status.prefix...)
	return append(wrapped, argv...), nil
}

// Process is an elevated command running without the app's terminal, such as
// one launched in its own window. Only its exit code is available, and only
// once it has ended; there is no output to read and nothing to send it.
type Process struct {
	wait  func() int
	close func() error
}

// Wait blocks until the process ends and returns its exit code.
func (p *Process) Wait() int { return p.wait() }

// Close releases the process now that it has ended.
func (p *Process) Close() error { return p.close() }
