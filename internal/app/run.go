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
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/lspoor/script-center/internal/detect"
	"github.com/lspoor/script-center/internal/elevate"
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
	session runSession
	// blind is non-nil for a run that had to be started elevated in its own
	// window. It delivers no output and cannot be stopped, but its exit code
	// still arrives when it ends.
	blind blindProc
	// stopped records that the user asked for the run to end, which the exit
	// event needs to distinguish from the script ending on its own.
	stopped bool
}

// blindProc is everything the runner can do with an elevated run that lives in
// its own, separate window: wait for it to end and release it. There is no
// terminal to read or write.
type blindProc interface {
	Wait() int
	Close() error
}

// runSession is everything the runner needs from a pseudo-terminal session,
// so a test can fake a process start without launching one. pty.Session
// satisfies it.
type runSession interface {
	Read([]byte) (int, error)
	Write([]byte) (int, error)
	Wait() int
	Close() error
}

// The elevation mechanisms are variables so tests can pin them without
// actually elevating anything: a UAC prompt cannot appear in a test, and a
// handful of lines of test should not depend on whether sudo happens to be
// installed on the machine running them.
var (
	detectElevation = elevate.Detect
	elevateWrap     = elevate.Wrap
	// startElevated returns the runner's blindProc abstraction rather than
	// elevate.Process itself, so a test can stub it below the level where a
	// process is really started.
	startElevated = func(argv []string, dir string) (blindProc, error) {
		return elevate.StartElevated(argv, dir)
	}
	// startPty starts a process under a pseudo-terminal. It is a variable so a
	// test can stub the process start itself: a run test that pins elevation
	// must not depend on the interpreter named in the argv existing on the
	// machine running it.
	startPty = func(cfg pty.Config) (runSession, error) {
		return pty.Start(cfg)
	}
)

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

	session, err := startPty(pty.Config{Argv: argv, Dir: dir, Env: runEnv()})
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
	r.mu.Unlock()

	if !ok {
		return fmt.Errorf("%w: %s", ErrNoRun, id)
	}
	if entry.blind != nil {
		return errors.New("app: this run is in a separate elevated window and cannot be stopped from the app")
	}
	r.mu.Lock()
	entry.stopped = true
	r.mu.Unlock()
	return entry.session.Close()
}

// SendInput sends one line to a running script, answering whatever it is
// prompting for. The line ending is the platform's submission character, so
// the child reads it the way it would keyboard entry.
func (r *Runner) SendInput(id, text string) error {
	r.mu.Lock()
	entry, ok := r.sessions[id]
	r.mu.Unlock()

	if !ok {
		return fmt.Errorf("%w: %s", ErrNoRun, id)
	}
	if entry.blind != nil {
		return errors.New("app: this run is in a separate elevated window and cannot take input from the app")
	}
	_, err := entry.session.Write([]byte(text + pty.LineEnding()))
	return err
}

// StartBlind registers an already-started elevated run that lives in its own
// window and delivers only its exit code when it ends.
func (r *Runner) StartBlind(proc blindProc) (string, error) {
	if r.Active() {
		return "", ErrActiveRun
	}
	id := fmt.Sprintf("run-%d", runSeq.Add(1))
	r.mu.Lock()
	r.sessions[id] = &run{blind: proc}
	r.mu.Unlock()

	go r.pumpBlind(proc, id)
	return id, nil
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
func (r *Runner) pump(session runSession, id string) {
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

// pumpBlind waits for a run that cannot be streamed, reporting only its exit
// event: a separate elevated window has no output for the panel and cannot be
// stopped, so there is nothing else to say about it.
func (r *Runner) pumpBlind(proc blindProc, id string) {
	code := proc.Wait()
	r.mu.Lock()
	delete(r.sessions, id)
	r.mu.Unlock()
	_ = proc.Close()
	r.emitEvent("run:exit", RunExit{ID: id, Code: code})
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
	// The scan stores the directory relative to the root, so resolve a relative
	// one here: the process working directory must be absolute, or the OS looks
	// it up against this app's own directory and a nested script fails to start.
	dir = script.Dir
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(script.Root, dir)
	}
	return argv, dir, nil
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
// without editing anything. runAsAdmin is handed to the script's command even
// when the script did not ask for it; a script that declares it (via
// "#Requires -RunAsAdministrator") is always elevated regardless of the flag.
//
// Elevation runs wherever the platform has a way to keep the elevated command
// inside the app's terminal (sudo inline); otherwise the script starts
// elevated in a window of its own, which RunView.Blind tells the panel, and
// which gives up streaming output and stoppability. When elevation is
// impossible, the run is refused with a message that says how to make it
// possible rather than quietly running with the app's own privileges.
func (s *Service) RunScript(ctx context.Context, root, rel string, extra []string, runAsAdmin bool) (*RunView, error) {
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

	requiresAdmin := runAsAdmin || script.Metadata.Requirements.RunAsAdministrator
	if !requiresAdmin {
		id, err := s.runner.Start(argv, dir)
		if err != nil {
			return nil, fmt.Errorf("app: starting %q: %w", script.Name, err)
		}
		return &RunView{ID: id, Command: argv, Dir: dir}, nil
	}

	status := detectElevation()
	if !status.Available {
		return nil, fmt.Errorf("app: %s asks to run as administrator, which is not possible here: %s", script.Name, status.Hint)
	}

	// A script that declared the requirement is told the run is elevated; a
	// user who asked for it directly already knows.
	var warning string
	if script.Metadata.Requirements.RunAsAdministrator {
		warning = "this script asks to run as administrator; running with administrator privileges"
	}

	if status.Inline {
		argv, err = elevateWrap(argv)
		if err != nil {
			return nil, fmt.Errorf("app: %s: %w", script.Name, err)
		}
		id, err := s.runner.Start(argv, dir)
		if err != nil {
			return nil, fmt.Errorf("app: starting %q: %w", script.Name, err)
		}
		return &RunView{ID: id, Command: argv, Dir: dir, Warning: warning}, nil
	}

	proc, err := startElevated(argv, dir)
	if err != nil {
		return nil, fmt.Errorf("app: starting %q elevated: %w", script.Name, err)
	}
	id, err := s.runner.StartBlind(proc)
	if err != nil {
		_ = proc.Close()
		return nil, fmt.Errorf("app: starting %q: %w", script.Name, err)
	}
	if script.Metadata.Requirements.RunAsAdministrator {
		warning = "this script asks to run as administrator; it is running in its own elevated window, which this panel cannot see or stop"
	}
	return &RunView{ID: id, Command: argv, Dir: dir, Warning: warning, Blind: true}, nil
}

// StopScript stops a running script.
func (s *Service) StopScript(id string) error {
	if s.runner == nil {
		return fmt.Errorf("%w: %s", ErrNoRun, id)
	}
	return s.runner.Stop(id)
}

// SendInput answers a running script's prompt with one line. It fails when
// the run is gone, or when the run lives in its own elevated window where the
// app cannot reach its input.
func (s *Service) SendInput(id, text string) error {
	if s.runner == nil {
		return fmt.Errorf("%w: %s", ErrNoRun, id)
	}
	return s.runner.SendInput(id, text)
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

// SetStageEmitter installs the window's event emitter for read progress. It
// exists for the same reason as SetRunEmitter: a test can construct a Service
// without a webview and still collect the events a read produces.
func (s *Service) SetStageEmitter(emit func(name string, data any)) {
	s.stageMu.Lock()
	defer s.stageMu.Unlock()
	s.stageEmit = emit
}

// emitStage reports one step of a read to the window.
//
// A nil emitter drops the event, which keeps a Service usable before startup
// and in a test that wants no events, the same way Runner.emitEvent does for
// runs.
func (s *Service) emitStage(stage ReadStage) {
	s.stageMu.Lock()
	emit := s.stageEmit
	s.stageMu.Unlock()
	if emit != nil {
		emit("read:stage", stage)
	}
}
