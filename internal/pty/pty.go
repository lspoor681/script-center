// Package pty wraps cross-platform pseudo-terminal support.
//
// crosspty is used rather than creack/pty because creack/pty has no Windows
// implementation, and it fails silently there: it cross-compiles cleanly, so
// both the compiler and CI stay green, and then interactive prompts simply
// never work at runtime. The smoke tests in pty_test.go exist to catch that
// class of failure and must be run on Windows as well as Unix.
//
// A pseudo-terminal is what makes interactive prompts work at all: credential
// entry, "press Y/N", device-code sign-in, and Read-Host all require the child
// to believe it is attached to a console. Plain pipes are not enough, because
// many programs check isatty() and change or disable their behavior.
package pty

import (
	"errors"
	"runtime"

	"github.com/Kodecable/crosspty"
)

// DefaultSize is the initial terminal size reported to the child process.
var DefaultSize = crosspty.TermSize{Rows: 24, Cols: 80}

// Config describes a process to run attached to a pseudo-terminal.
type Config struct {
	// Argv is the command and its arguments, where Argv[0] is the executable.
	// An absolute path is recommended: a bare name containing no path
	// separators is resolved through exec.LookPath.
	Argv []string

	// Dir is the working directory. Empty means the current directory.
	Dir string

	// Env is the child environment. Nil means inherit the current environment;
	// an empty non-nil slice means the child gets no environment at all.
	Env []string

	// Size is the initial terminal size. The zero value means DefaultSize.
	Size crosspty.TermSize
}

// LineEnding returns the byte sequence required to submit a line to the child.
//
// On Windows a subprocess may have ENABLE_LINE_INPUT enabled, in which case a
// bare "\n" is not enough to make the child read the line: "\r\n" is required.
// On Unix "\n" is correct and "\r" would be sent to the program as data.
func LineEnding() string {
	if runtime.GOOS == "windows" {
		return "\r\n"
	}
	return "\n"
}

// Shell returns an argv that reads commands from standard input and evaluates
// them, for use when the application needs a bare interactive interpreter
// rather than a specific script.
func Shell() []string {
	if runtime.GOOS == "windows" {
		// powershell.exe is used rather than pwsh because it is present on
		// every supported Windows release, including those without pwsh.
		//
		// It is started interactively and not as "powershell.exe -Command -".
		// Reading commands from standard input is what that flag asks for, but
		// Windows PowerShell only honors it when standard input is a
		// redirected pipe, and under a pseudo-terminal it is a console. Asked
		// anyway, it prints its own help text and exits, so a session built on it
		// is dead on arrival: nothing is ever read and nothing is ever run.
		// Started interactively it reads from the console the pseudo-terminal
		// provides, which is the whole point of attaching one.
		return []string{"powershell.exe", "-NoLogo", "-NoProfile"}
	}
	return []string{"/bin/sh"}
}

// Session is a process attached to a pseudo-terminal.
//
// All methods are safe for concurrent use. Read may be called concurrently
// with Write, which is what allows output to stream while input is still being
// collected. Read and Write must not be called after Close.
type Session struct {
	pty crosspty.Pty
}

// Start launches cfg under a new pseudo-terminal.
//
// Start does not modify cfg.Argv. The underlying terminal library resolves a
// bare argv[0] to a full path in place, which would otherwise write back into
// the caller's slice and into any view built from it.
func Start(cfg Config) (*Session, error) {
	if len(cfg.Argv) == 0 {
		return nil, errors.New("pty: Argv is required")
	}
	if cfg.Size == (crosspty.TermSize{}) {
		cfg.Size = DefaultSize
	}
	argv := append([]string(nil), cfg.Argv...)
	p, err := crosspty.Start(crosspty.CommandConfig{
		Argv: argv,
		Dir:  cfg.Dir,
		Env:  cfg.Env,
		Size: cfg.Size,
	})
	if err != nil {
		return nil, err
	}
	return &Session{pty: p}, nil
}

// Read reads terminal output. It returns io.EOF once no more output can be
// read. Process exit and end-of-file are distinct: a process exit does not
// necessarily mean the output has been fully drained, so keep reading until
// io.EOF rather than stopping at exit.
func (s *Session) Read(p []byte) (int, error) { return s.pty.Read(p) }

// Write sends input to the child. Callers writing a line should append
// LineEnding.
func (s *Session) Write(p []byte) (int, error) { return s.pty.Write(p) }

// Resize reports a new terminal size to the child, which is what drives
// reflow and full-screen redraw.
func (s *Session) Resize(sz crosspty.TermSize) error { return s.pty.Resize(sz) }

// Pid returns the process identifier of the child, or -1 if unavailable.
func (s *Session) Pid() int { return s.pty.Pid() }

// Wait blocks until the child exits and returns its exit code. It returns -1
// when the exit code is genuinely unavailable, such as when the child was
// force-killed. Close must still be called afterwards to release resources.
func (s *Session) Wait() int { return s.pty.Wait() }

// Close terminates the child, escalating from a graceful signal to a force
// kill after a delay, and releases the pseudo-terminal. It may interrupt an
// in-flight Read or Write, which is what makes it usable for cancellation.
func (s *Session) Close() error { return s.pty.Close() }
