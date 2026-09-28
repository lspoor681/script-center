// Run is the local runner: it starts scripts in a pseudo-terminal and streams
// their output to the window as events.
//
// The design deliberately keeps the process management small. A run is a
// pty.Session, which already owns the terminal, the exit code and the
// cancellation path, so all this layer adds is an id, an event emitter and the
// argv selection. Keeping the process under a pseudo-terminal is also what the
// remote-execution design (docs/TODO.md) reuses, so a run is shaped the same
// way on both sides of the fence.
package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"

	"github.com/lspoor/script-center/internal/detect"
	"github.com/lspoor/script-center/internal/pty"
	"github.com/lspoor/script-center/internal/scan"
)

// ErrNoRun reports that a request named a run that is not active anymore. It is
// the normal answer to stopping a run that has already ended, not a bug.
var ErrNoRun = errors.New("app: that run is not active")

// ErrUnrunnable reports that a script's language cannot be run from the app.
var ErrUnrunnable = errors.New("app: this script cannot be run from the app")

// ErrActiveRun reports that a run was refused because another one is already
// going. The runner streams one run at a time on purpose, and the panel holds
// one, so a second start would orphan a process the user could not see.
var ErrActiveRun = errors.New("app: another run is already active")

// runSeq numbers runs, so ids are unique for the life of the application.
var runSeq atomic.Int64

// Runner starts scripts and streams their output as events.
//
// Only one run is active at a time: the runner is small on purpose, and
// concurrent runs are on the roadmap rather than a supported thing.
type Runner struct {
	emit func(name string, data any)

	mu       sync.Mutex
	sessions map[string]*run
}

// run is one live run.
type run struct {
	session *pty.Session
	// stopped records that the user asked for the run to end, which the exit
	// event needs to distinguish from the script ending on its own.
	stopped bool
}

// NewRunner returns a Runner with no runs.
func NewRunner() *Runner {
	return &Runner{sessions: map[string]*run{}}
}

// SetEmitter installs the callback that delivers events.
//
// The App calls this once at startup with a function that emits on the window;
// a test installs one that appends to a slice. A nil emitter is allowed and
// drops events, which keeps a Service usable before startup.
func (r *Runner) SetEmitter(emit func(name string, data any)) {
	r.emit = emit
}

// Active reports whether any run is still streaming, which decides whether the
// Run button stays enabled.
func (r *Runner) Active() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.sessions) > 0
}

// Start launches argv in dir and returns the id its events will be tagged
// with. Output arrives as run:output events and the end as one run:exit event.
//
// Only one run is active at a time. The check is not atomic against another
// concurrent Start, which are only ever single-user clicks anyway; the guard is
// for the common mistake of starting a second run while the first streams.
func (r *Runner) Start(argv []string, dir string) (string, error) {
	if r.Active() {
		return "", ErrActiveRun
	}

	session, err := pty.Start(pty.Config{Argv: argv, Dir: dir, Env: runEnv()})
	if err != nil {
		return "", err
	}

	id := fmt.Sprintf("run-%d", runSeq.Add(1))
	r.mu.Lock()
	r.sessions[id] = &run{session: session}
	r.mu.Unlock()

	go r.pump(session, id)
	return id, nil
}

// Stop ends a run the way a user stopping it would: the child is terminated and
// the exit event reports the run as stopped.
func (r *Runner) Stop(id string) error {
	r.mu.Lock()
	entry, ok := r.sessions[id]
	if ok {
		entry.stopped = true
	}
	r.mu.Unlock()

	if !ok {
		return fmt.Errorf("%w: %s", ErrNoRun, id)
	}
	return entry.session.Close()
}

// emitEvent delivers an event when an emitter is installed. A nil emitter drops
// events, so a Service used before startup or in a test without one does not
// panic when a run ends.
func (r *Runner) emitEvent(name string, data any) {
	if r.emit != nil {
		r.emit(name, data)
	}
}

// pump reads a run's output until the session closes, forwarding each chunk as
// an event, and closes with the run:exit event.
func (r *Runner) pump(session *pty.Session, id string) {
	// A process exiting does not necessarily mean its output is drained, so
	// the loop reads until io.EOF rather than stopping at the first error.
	buf := make([]byte, 8192)
	for {
		n, err := session.Read(buf)
		if n > 0 {
			r.emitEvent("run:output", RunOutput{ID: id, Chunk: string(buf[:n])})
		}
		if err != nil {
			break
		}
	}

	code := session.Wait()
	r.mu.Lock()
	entry, ok := r.sessions[id]
	delete(r.sessions, id)
	r.mu.Unlock()
	if !ok {
		// The exit event still needs an answer even if the entry is gone.
		entry = &run{}
	}
	_ = session.Close()
	r.emitEvent("run:exit", RunExit{ID: id, Code: code, Stopped: entry.stopped})
}

// runEnv is the environment a run gets. Interaction is turned off because the
// output lands in a plain text block: the user wants to read what the script
// printed, not reconstruct it from escape codes.
func runEnv() []string {
	env := append([]string(nil), os.Environ()...)
	return append(env, "NO_COLOR=1", "TERM=dumb")
}

// resolveInterpreter maps a language to its interpreter on PATH, or "" when none
// of the candidates exist.
//
// It is a variable rather than a function so tests can pin the outcome without
// depending on which interpreters happen to be installed on the machine running
// them.
var resolveInterpreter = func(language detect.Language) string {
	switch language {
	case detect.PowerShell:
		// PowerShell 7 is preferred for its consistent behavior, but a machine
		// without it can still run a script with the Windows PowerShell it does
		// have, so that is the fallback.
		return firstOnPath("pwsh", "powershell")
	case detect.Python:
		return firstOnPath("python", "python3")
	case detect.Bash:
		return firstOnPath("bash")
	case detect.Batch:
		return firstOnPath("cmd")
	case detect.Rust:
		return firstOnPath("cargo")
	}
	return ""
}

// firstOnPath returns the first candidate that exists on PATH, or "" if none
// do. The full path is returned rather than the bare name, so the resulting
// argv does not depend on a later change to PATH resolving the same name
// differently.
func firstOnPath(candidates ...string) string {
	for _, name := range candidates {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return ""
}

// commandFor builds the argv that runs a script and the directory it runs in.
//
// The working directory matters: scripts dot-source siblings and reference
// relative paths, so they run from their own directory the way they would if
// invoked by hand. A project runs from the project directory, where the tool
// expects to find its manifest.
func commandFor(script ScriptView, extra []string) (argv []string, dir string, err error) {
	if script.Kind == string(scan.KindProject) {
		if cargo := resolveInterpreter(detect.Rust); cargo != "" {
			return append([]string{cargo, "run"}, extra...), script.Path, nil
		}
		return nil, "", errMissing("Cargo")
	}

	argv, err = scriptCommand(script, extra)
	if err != nil {
		return nil, "", err
	}
	return argv, script.Dir, nil
}

// scriptCommand builds the argv that runs one script file: the interpreter
// found on PATH, any mandatory flags, the script's own path, then the extra
// arguments a user asked for.
func scriptCommand(script ScriptView, extra []string) ([]string, error) {
	switch script.detectedLanguage() {
	case detect.PowerShell:
		if exe := resolveInterpreter(detect.PowerShell); exe != "" {
			argv := []string{exe, "-NoLogo", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", script.Path}
			return append(argv, extra...), nil
		}
		return nil, errMissing("PowerShell")
	case detect.Python:
		if exe := resolveInterpreter(detect.Python); exe != "" {
			argv := []string{exe, script.Path}
			return append(argv, extra...), nil
		}
		return nil, errMissing("Python")
	case detect.Bash:
		if exe := resolveInterpreter(detect.Bash); exe != "" {
			argv := []string{exe, script.Path}
			return append(argv, extra...), nil
		}
		return nil, errMissing("Bash")
	case detect.Batch:
		if exe := resolveInterpreter(detect.Batch); exe != "" {
			argv := []string{exe, "/c", script.Path}
			return append(argv, extra...), nil
		}
		return nil, errMissing("Windows Command Processor")
	}
	return nil, fmt.Errorf("%w: %s has no run command", ErrUnrunnable, script.Name)
}

// errMissing reports that a language's interpreter is not on PATH, so its
// scripts cannot be run from the app.
func errMissing(runtime string) error {
	return fmt.Errorf("app: %s is not on PATH, so this cannot be run", runtime)
}

// RunScript starts a script in its own directory and streams its output to the
// frontend as events, returning the handle the events are tagged with.
//
// Extra is passed through to the script, so a user can run it with options
// without editing anything.
func (s *Service) RunScript(ctx context.Context, root, rel string, extra []string) (*RunView, error) {
	if s.runner == nil {
		s.runner = NewRunner()
	}
	view, err := s.cachedRoot(ctx, root)
	if err != nil {
		return nil, err
	}
	script, ok := findScript(view, rel)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNoScript, rel)
	}

	argv, dir, err := commandFor(*script, extra)
	if err != nil {
		return nil, err
	}

	var warning string
	if script.Metadata.Requirements.RunAsAdministrator {
		warning = "this script asks to run as administrator, but it will run with the app's own privileges"
	}

	id, err := s.runner.Start(argv, dir)
	if err != nil {
		return nil, fmt.Errorf("app: starting %q: %w", script.Name, err)
	}
	return &RunView{ID: id, Command: argv, Dir: dir, Warning: warning}, nil
}

// StopScript stops a running script.
func (s *Service) StopScript(id string) error {
	if s.runner == nil {
		return fmt.Errorf("%w: %s", ErrNoRun, id)
	}
	return s.runner.Stop(id)
}

// SetRunEmitter installs the window's event emitter on the runner. It is a
// Service-level method so tests can construct a Service without a webview and
// still receive run events.
func (s *Service) SetRunEmitter(emit func(name string, data any)) {
	if s.runner == nil {
		s.runner = NewRunner()
	}
	s.runner.SetEmitter(emit)
}
