package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lspoor/script-center/internal/detect"
	"github.com/lspoor/script-center/internal/elevate"
	"github.com/lspoor/script-center/internal/params"
	"github.com/lspoor/script-center/internal/pty"
	"github.com/lspoor/script-center/internal/scan"
)

// viewScript builds a ScriptView for a rel path in a fake root. The path is
// deliberately not a real file: commandFor only asks what the view says. The
// directory is stored relative to the root, as the scan does.
func viewScript(rel string, language detect.Language, kind scan.Kind) ScriptView {
	root := filepath.Join("C:", string(filepath.Separator), "work", "scripts")
	pathValue := filepath.Join(root, filepath.FromSlash(rel))
	return ScriptView{
		Path:     pathValue,
		Root:     root,
		Rel:      filepath.ToSlash(rel),
		Name:     filepath.Base(pathValue),
		Dir:      path.Dir(rel),
		Kind:     string(kind),
		Language: string(language),
	}
}

// pinInterpreter fixes the interpreter resolution for the duration of a test,
// so the outcome does not depend on what is installed on the machine running it.
func pinInterpreter(t *testing.T, resolve func(detect.Language) string) {
	t.Helper()
	original := resolveInterpreter
	resolveInterpreter = resolve
	t.Cleanup(func() { resolveInterpreter = original })
}

func TestCommandFor(t *testing.T) {
	// A single fixed executable for every language: the tests check the argv
	// shape, not which interpreter the machine happens to have.
	pinInterpreter(t, func(detect.Language) string { return "/sandbox/tool" })

	const extra = "--verbose"
	project := viewScript("tools", detect.Rust, scan.KindProject)
	powershell := viewScript("deploy.ps1", detect.PowerShell, scan.KindScript)
	python := viewScript("render.py", detect.Python, scan.KindScript)
	bash := viewScript("rebuild.sh", detect.Bash, scan.KindScript)
	batch := viewScript("build.bat", detect.Batch, scan.KindScript)

	tests := []struct {
		name    string
		script  ScriptView
		want    []string
		wantDir string
	}{
		{
			name:    "a cargo project runs from its own directory",
			script:  project,
			want:    []string{"/sandbox/tool", "run"},
			wantDir: project.Path,
		},
		{
			name:    "powershell",
			script:  powershell,
			want:    []string{"/sandbox/tool", "-NoLogo", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", powershell.Path},
			wantDir: filepath.Dir(powershell.Path),
		},
		{
			name:    "python",
			script:  python,
			want:    []string{"/sandbox/tool", python.Path},
			wantDir: filepath.Dir(python.Path),
		},
		{
			name:    "bash",
			script:  bash,
			want:    []string{"/sandbox/tool", bash.Path},
			wantDir: filepath.Dir(bash.Path),
		},
		{
			name:    "batch",
			script:  batch,
			want:    []string{"/sandbox/tool", "/c", batch.Path},
			wantDir: filepath.Dir(batch.Path),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			argv, dir, err := commandFor(tt.script, nil)
			if err != nil {
				t.Fatalf("commandFor: %v", err)
			}
			if dir != tt.wantDir {
				t.Errorf("dir = %q, want %q", dir, tt.wantDir)
			}
			if !slices.Equal(argv, tt.want) {
				t.Errorf("argv = %q, want %q", argv, tt.want)
			}
		})
	}

	t.Run("extra arguments are appended after the script", func(t *testing.T) {
		argv, _, err := commandFor(powershell, []string{"--input", "a b"})
		if err != nil {
			t.Fatalf("commandFor: %v", err)
		}
		want := []string{"/sandbox/tool", "-NoLogo", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", powershell.Path, "--input", "a b"}
		if !slices.Equal(argv, want) {
			t.Errorf("argv = %q, want %q", argv, want)
		}
	})

	t.Run("a cargo project takes the extras after run", func(t *testing.T) {
		argv, _, err := commandFor(project, []string{extra})
		if err != nil {
			t.Fatalf("commandFor: %v", err)
		}
		if !slices.Equal(argv, []string{"/sandbox/tool", "run", extra}) {
			t.Errorf("argv = %q, want `run` followed by the extras", argv)
		}
	})
}

// TestCommandForWorksFromRelativeDir covers the directory the scan actually
// reports: relative to the root ("." for a root-level script, "dir/sub" for a
// nested one). commandFor must resolve that into an absolute working directory,
// because the OS looks up a relative one against the app's own directory and a
// nested script would fail to start.
func TestCommandForWorksFromRelativeDir(t *testing.T) {
	// A single fixed executable for every language: the tests check the argv
	// shape, not which interpreter the machine happens to have.
	pinInterpreter(t, func(detect.Language) string { return "/sandbox/tool" })

	root := filepath.Join("C:", string(filepath.Separator), "work", "scripts")
	nested := ScriptView{
		Path:     filepath.Join(root, "tools", "harness.cmd"),
		Root:     root,
		Rel:      "tools/harness.cmd",
		Name:     "harness.cmd",
		Dir:      "tools",
		Kind:     string(scan.KindScript),
		Language: string(detect.Batch),
	}
	topLevel := ScriptView{
		Path:     filepath.Join(root, "Remediate.cmd"),
		Root:     root,
		Rel:      "Remediate.cmd",
		Name:     "Remediate.cmd",
		Dir:      ".",
		Kind:     string(scan.KindScript),
		Language: string(detect.Batch),
	}

	tests := []struct {
		name    string
		script  ScriptView
		wantDir string
	}{
		{
			name:    "a nested script resolves its directory against the root",
			script:  nested,
			wantDir: filepath.Join(root, "tools"),
		},
		{
			name:    "a root-level script runs from the root itself",
			script:  topLevel,
			wantDir: root,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, dir, err := commandFor(tt.script, nil)
			if err != nil {
				t.Fatalf("commandFor: %v", err)
			}
			if dir != tt.wantDir {
				t.Errorf("dir = %q, want %q", dir, tt.wantDir)
			}
		})
	}
}

func TestCommandForMissingInterpreter(t *testing.T) {
	pinInterpreter(t, func(detect.Language) string { return "" })

	t.Run("powershell", func(t *testing.T) {
		ps := viewScript("deploy.ps1", detect.PowerShell, scan.KindScript)
		if _, _, err := commandFor(ps, nil); err == nil || !strings.Contains(err.Error(), "PowerShell") {
			t.Fatalf("err = %v, want a message naming PowerShell", err)
		}
	})

	t.Run("cargo project", func(t *testing.T) {
		project := viewScript("tools", detect.Rust, scan.KindProject)
		if _, _, err := commandFor(project, nil); err == nil || !strings.Contains(err.Error(), "Cargo") {
			t.Fatalf("err = %v, want a message naming Cargo", err)
		}
	})
}

func TestCommandForUnknownLanguage(t *testing.T) {
	pinInterpreter(t, func(detect.Language) string { return "/sandbox/tool" })

	script := viewScript("notes.txt", detect.Unknown, scan.KindScript)
	if _, _, err := commandFor(script, nil); !errors.Is(err, ErrUnrunnable) {
		t.Fatalf("err = %v, want ErrUnrunnable", err)
	}
}

// runEvents collects a Runner's events, keeping the slice safe to read while
// the pump goroutine is writing it.
type runEvents struct {
	mu      sync.Mutex
	outputs []string
	exits   []RunExit
}

func (r *runEvents) emit(name string, data any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch v := data.(type) {
	case RunOutput:
		r.outputs = append(r.outputs, v.Chunk)
	case RunExit:
		r.exits = append(r.exits, v)
	default:
		panic(fmt.Sprintf("runEvents: unhandled event type %T", data))
	}
}

func (r *runEvents) output() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.outputs, "")
}

func (r *runEvents) exit() (RunExit, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.exits) == 0 {
		return RunExit{}, false
	}
	return r.exits[0], true
}

// awaitTrue polls until condition holds or a deadline passes, which is how a
// test waits for an event without counting on a particular scheduling.
func awaitTrue(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		if condition() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("condition did not become true within 20s")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// echoArgv runs a command that prints marker and exits cleanly, on any OS.
func echoArgv(marker string) []string {
	if runtime.GOOS == "windows" {
		return []string{"cmd.exe", "/C", "echo " + marker}
	}
	return []string{"/bin/sh", "-c", "echo " + marker}
}

// longLivingArgv runs a command that stays alive until it is terminated.
func longLivingArgv() []string {
	if runtime.GOOS == "windows" {
		return []string{"cmd.exe", "/C", "ping -n 120 127.0.0.1 >NUL"}
	}
	return []string{"/bin/sh", "-c", "while :; do sleep 1; done"}
}

func TestRunnerStreamsOutputAndExits(t *testing.T) {
	runner := NewRunner()
	events := &runEvents{}
	runner.SetEmitter(events.emit)

	const marker = "runner-says-hi-8321"
	id, err := runner.Start(echoArgv(marker), t.TempDir())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if id == "" {
		t.Fatal("Start returned an empty id")
	}
	t.Cleanup(func() { _ = runner.Stop(id) })

	awaitTrue(t, func() bool {
		_, ok := events.exit()
		return ok
	})
	exit, _ := events.exit()
	if exit.Stopped {
		t.Error("exit.Stopped = true, want false for a run that ended on its own")
	}
	if exit.Code != 0 {
		t.Errorf("exit.Code = %d, want 0 for echo", exit.Code)
	}

	if got := events.output(); !strings.Contains(got, marker) {
		t.Errorf("output = %q, want it to contain %q", got, marker)
	}
	if runner.Active() {
		t.Error("Active() = true after the exit event")
	}
}

// TestRunnerStopReportsStopped covers cancellation: stopping a run must end its
// child and tell the window the end came from the Stop button.
func TestRunnerStopReportsStopped(t *testing.T) {
	runner := NewRunner()
	events := &runEvents{}
	runner.SetEmitter(events.emit)

	id, err := runner.Start(longLivingArgv(), t.TempDir())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !runner.Active() {
		t.Fatal("Active() = false right after Start")
	}

	if err := runner.Stop(id); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	awaitTrue(t, func() bool {
		exit, ok := events.exit()
		return ok && exit.Stopped
	})
	awaitTrue(t, func() bool { return !runner.Active() })
}

func TestRunnerStopUnknownID(t *testing.T) {
	runner := NewRunner()
	if err := runner.Stop("run-0"); !errors.Is(err, ErrNoRun) {
		t.Errorf("Stop on an unknown id = %v, want ErrNoRun", err)
	}
}

// inputArgv runs a command that reads one line and echoes it back, on any OS.
// The echo is prefixed so a test can tell the child actually read the line
// from the terminal echo of the typed bytes.
func inputArgv() []string {
	if runtime.GOOS == "windows" {
		return []string{"powershell.exe", "-NoLogo", "-NoProfile", "-Command", "$l = Read-Host; Write-Output ('got:' + $l)"}
	}
	return []string{"/bin/sh", "-c", `read l; echo "got:$l"`}
}

// TestRunnerSendInput covers the interactive half of a run: a line sent to the
// runner arrives in the child and is answered. It runs a real pseudo-terminal
// with a real script that reads one line, which is the point of the feature.
func TestRunnerSendInput(t *testing.T) {
	runner := NewRunner()
	events := &runEvents{}
	runner.SetEmitter(events.emit)

	id, err := runner.Start(inputArgv(), t.TempDir())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = runner.Stop(id) })

	// The terminal buffers what is written, so a line that arrives before the
	// script is reading is still consumed; the pause only keeps the send from
	// racing the child's startup.
	time.Sleep(300 * time.Millisecond)
	const line = "offline-input-4711"
	if err := runner.SendInput(id, line); err != nil {
		t.Fatalf("SendInput: %v", err)
	}

	awaitTrue(t, func() bool {
		return strings.Contains(events.output(), "got:"+line)
	})
}

func TestRunnerSendInputUnknownID(t *testing.T) {
	runner := NewRunner()
	if err := runner.SendInput("run-0", "x"); !errors.Is(err, ErrNoRun) {
		t.Errorf("SendInput on an unknown id = %v, want ErrNoRun", err)
	}
}

func TestRunnerRefusesConcurrentRun(t *testing.T) {
	runner := NewRunner()
	runner.SetEmitter((&runEvents{}).emit)

	first, err := runner.Start(longLivingArgv(), t.TempDir())
	if err != nil {
		t.Fatalf("first Start: %v", err)
	}
	t.Cleanup(func() { _ = runner.Stop(first) })

	if _, err := runner.Start(echoArgv("second"), t.TempDir()); !errors.Is(err, ErrActiveRun) {
		t.Errorf("second Start = %v, want ErrActiveRun while the first is running", err)
	}
}

func TestRunScriptErrors(t *testing.T) {
	service, _ := newTestService(t)

	t.Run("a root that is not a directory", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "does-not-exist")
		if _, err := service.RunScript(context.Background(), missing, "x.ps1", nil, false); err == nil {
			t.Fatal("RunScript on a missing root returned no error")
		}
	})

	root := scriptTree(t)
	if _, err := service.OpenRoot(context.Background(), root); err != nil {
		t.Fatal(err)
	}

	t.Run("a script the root does not have", func(t *testing.T) {
		if _, err := service.RunScript(context.Background(), root, "missing.ps1", nil, false); !errors.Is(err, ErrNoScript) {
			t.Fatalf("err = %v, want ErrNoScript", err)
		}
	})
}

// TestRunScriptStreamsEvents runs the whole path from a request down to the
// runner, with the interpreter pinned to a shell that always exists, so the
// test verifies the wiring without depending on which interpreters are
// installed.
func TestRunScriptStreamsEvents(t *testing.T) {
	var rel, name, body, marker string
	if runtime.GOOS == "windows" {
		pinInterpreter(t, func(detect.Language) string { return "cmd.exe" })
		name, rel = "hello.bat", "hello.bat"
		body = "@echo offline-marker\r\nexit /b 0\r\n"
	} else {
		pinInterpreter(t, func(detect.Language) string { return "/bin/sh" })
		name, rel = "hello.sh", "hello.sh"
		body = "echo offline-marker\n"
	}
	marker = "offline-marker"

	service, _ := newTestService(t)
	events := &runEvents{}
	service.SetRunEmitter(events.emit)

	root := t.TempDir()
	batch := filepath.Join(root, name)
	if err := os.WriteFile(batch, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	view, err := service.OpenRoot(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	script := scriptAt(t, view, rel)

	run, err := service.RunScript(context.Background(), root, script.Rel, []string{"--count", "3"}, false)
	if err != nil {
		t.Fatalf("RunScript: %v", err)
	}
	if run.ID == "" {
		t.Fatal("RunScript returned an empty id")
	}

	got := strings.Join(run.Command, " ")
	if !strings.Contains(got, rel) || !strings.HasSuffix(got, "--count 3") {
		t.Errorf("Command = %q, want the script followed by the extra arguments", got)
	}
	if run.Warning != "" {
		t.Errorf("Warning = %q, want empty for a script with no elevation requirement", run.Warning)
	}

	awaitTrue(t, func() bool {
		_, ok := events.exit()
		return ok
	})
	if exit, _ := events.exit(); exit.Code != 0 {
		t.Errorf("exit.Code = %d, want 0", exit.Code)
	}
	if got := events.output(); !strings.Contains(got, marker) {
		t.Errorf("output = %q, want it to contain the script's echo", got)
	}
}

// pinElevation fixes the elevation mechanisms for the duration of a test, so
// that no test ever reaches the real ones: detectElevation can answer without
// inspecting the machine, and Wrap and StartElevated can be stubbed so a UAC
// prompt never appears and nothing is actually elevated.
func pinElevation(t *testing.T, detect func() elevate.Status, wrap func([]string) ([]string, error), start func([]string, string) (blindProc, error)) {
	t.Helper()
	originalDetect, originalWrap, originalStart := detectElevation, elevateWrap, startElevated
	detectElevation, elevateWrap, startElevated = detect, wrap, start
	t.Cleanup(func() {
		detectElevation, elevateWrap, startElevated = originalDetect, originalWrap, originalStart
	})
}

func TestRunScriptRefusesElevationWhenUnavailable(t *testing.T) {
	pinElevation(t,
		func() elevate.Status { return elevate.Status{Available: false, Method: "sudo", Hint: "install sudo"} },
		nil,
		nil,
	)
	service, _ := newTestService(t)
	root := t.TempDir()
	path := filepath.Join(root, "admin.ps1")
	service.publishRoot(root, &RootView{
		Root: root,
		Scripts: []ScriptView{{
			Path: path, Root: root, Rel: "admin.ps1", Name: filepath.Base(path), Dir: root,
			Kind: string(scan.KindScript), Language: string(detect.PowerShell), Lang: "PowerShell",
			Metadata: params.Report{
				Path:         path,
				Help:         params.Help{Source: params.HelpSourceNone},
				Requirements: params.Requirements{RunAsAdministrator: true},
			},
		}},
	})

	_, err := service.RunScript(context.Background(), root, "admin.ps1", nil, false)
	if err == nil || !strings.Contains(err.Error(), "install sudo") {
		t.Fatalf("RunScript = %v, want an error telling the user how to enable elevation", err)
	}
}

// TestRunScriptRefusesManualElevationWhenUnavailable is the same refusal for a
// script that never declared the requirement: the checkbox asks, and the answer
// has to be no when elevation cannot be had.
func TestRunScriptRefusesManualElevationWhenUnavailable(t *testing.T) {
	pinElevation(t,
		func() elevate.Status { return elevate.Status{Available: false, Method: "sudo", Hint: "install sudo"} },
		nil,
		nil,
	)
	service, _ := newTestService(t)
	root := t.TempDir()
	path := filepath.Join(root, "plain.ps1")
	service.publishRoot(root, &RootView{
		Root: root,
		Scripts: []ScriptView{{
			Path: path, Root: root, Rel: "plain.ps1", Name: filepath.Base(path), Dir: root,
			Kind: string(scan.KindScript), Language: string(detect.PowerShell), Lang: "PowerShell",
			Metadata: params.Report{Path: path, Help: params.Help{Source: params.HelpSourceNone}},
		}},
	})

	if _, err := service.RunScript(context.Background(), root, "plain.ps1", nil, true); err == nil {
		t.Fatal("RunScript with runAsAdmin on a machine without elevation returned no error")
	}
}

// blindFake is a blindProc whose exit code can be fixed by a test, so the
// runner's blind path is exercised without starting any real process.
type blindFake struct {
	code int
}

func (b *blindFake) Wait() int    { return b.code }
func (b *blindFake) Close() error { return nil }

type runSessionFake struct{}

func (runSessionFake) Read([]byte) (int, error)    { return 0, io.EOF }
func (runSessionFake) Write(p []byte) (int, error) { return len(p), nil }
func (runSessionFake) Wait() int                   { return 0 }
func (runSessionFake) Close() error                { return nil }

// TestRunScriptElevatedInOwnWindow covers the blind path end to end: when
// elevation is possible but cannot stay inline, the script starts in its own
// window, the panel gets no output, and the exit code still lands as an event.
func TestRunScriptElevatedInOwnWindow(t *testing.T) {
	pinElevation(t,
		func() elevate.Status { return elevate.Status{Available: true, Inline: false, Method: "runas"} },
		func([]string) ([]string, error) { panic("Wrap must not be called for a blind run") },
		func(argv []string, dir string) (blindProc, error) {
			return &blindFake{code: 7}, nil
		},
	)
	service, _ := newTestService(t)
	events := &runEvents{}
	service.SetRunEmitter(events.emit)

	root := t.TempDir()
	path := filepath.Join(root, "admin.ps1")
	service.publishRoot(root, &RootView{
		Root: root,
		Scripts: []ScriptView{{
			Path: path, Root: root, Rel: "admin.ps1", Name: filepath.Base(path), Dir: root,
			Kind: string(scan.KindScript), Language: string(detect.PowerShell), Lang: "PowerShell",
			Metadata: params.Report{
				Path:         path,
				Help:         params.Help{Source: params.HelpSourceNone},
				Requirements: params.Requirements{RunAsAdministrator: true},
			},
		}},
	})

	run, err := service.RunScript(context.Background(), root, "admin.ps1", nil, false)
	if err != nil {
		t.Fatalf("RunScript: %v", err)
	}
	if !run.Blind {
		t.Error("RunView.Blind = false, want true for a separate-window run")
	}
	if run.Warning == "" || !strings.Contains(run.Warning, "administrator") {
		t.Errorf("Warning = %q, want a mention of elevation", run.Warning)
	}

	awaitTrue(t, func() bool {
		_, ok := events.exit()
		return ok
	})
	if exit, _ := events.exit(); exit.Code != 7 {
		t.Errorf("exit.Code = %d, want 7", exit.Code)
	}
	if got := events.output(); got != "" {
		t.Errorf("output = %q, want none for a separate-window run", got)
	}
	if service.runner.Active() {
		t.Error("Active() = true after the exit event")
	}
}

// TestRunnerBlindNotStoppable checks that the runner itself refuses to stop a
// run whose window it cannot reach, which is what keeps the panel honest
// about a blind run.
func TestRunnerBlindNotStoppable(t *testing.T) {
	runner := NewRunner()
	id, err := runner.StartBlind(&blindFake{code: 0})
	if err != nil {
		t.Fatalf("StartBlind: %v", err)
	}
	if err := runner.Stop(id); err == nil || !strings.Contains(err.Error(), "cannot be stopped") {
		t.Errorf("Stop = %v, want an error saying the run cannot be stopped", err)
	}
	if err := runner.SendInput(id, "y"); err == nil || !strings.Contains(err.Error(), "cannot take input") {
		t.Errorf("SendInput = %v, want an error saying the run cannot take input", err)
	}
}

// TestRunScriptElevatesWhenRequired checks the inline path: a script that asks
// for elevation runs under the platform's elevation prefix, and the warning
// still tells the user the run is elevated.
func TestRunScriptElevatesWhenRequired(t *testing.T) {
	pinInterpreter(t, func(detect.Language) string { return "powershell.exe" })
	wrapped := []string{}
	pinElevation(t,
		func() elevate.Status { return elevate.Status{Available: true, Inline: true, Method: "sudo"} },
		func(argv []string) ([]string, error) {
			wrapped = append(wrapped, "sudo")
			return append([]string{"sudo"}, argv...), nil
		},
		nil,
	)
	var startedArgv []string
	originalStartPty := startPty
	startPty = func(cfg pty.Config) (runSession, error) {
		startedArgv = slices.Clone(cfg.Argv)
		return runSessionFake{}, nil
	}
	t.Cleanup(func() { startPty = originalStartPty })

	service, _ := newTestService(t)
	events := &runEvents{}
	service.SetRunEmitter(events.emit)
	root := t.TempDir()
	path := filepath.Join(root, "admin.ps1")

	// The view is published by hand so the test does not depend on the
	// PowerShell reader being installed; the elevation requirement is all that
	// is being asserted.
	service.publishRoot(root, &RootView{
		Root: root,
		Scripts: []ScriptView{{
			Path: path, Root: root, Rel: "admin.ps1", Name: filepath.Base(path), Dir: root,
			Kind: string(scan.KindScript), Language: string(detect.PowerShell), Lang: "PowerShell",
			Metadata: params.Report{
				Path:         path,
				Help:         params.Help{Source: params.HelpSourceNone},
				Requirements: params.Requirements{RunAsAdministrator: true},
			},
		}},
	})

	run, err := service.RunScript(context.Background(), root, "admin.ps1", nil, false)
	if err != nil {
		t.Fatalf("RunScript: %v", err)
	}
	if run.Blind {
		t.Error("RunView.Blind = true, want a pty-backed inline run")
	}
	if run.Command[0] != "sudo" {
		t.Errorf("Command = %q, want it to start with the elevation prefix", run.Command)
	}
	if !slices.Equal(startedArgv, run.Command) {
		t.Errorf("started argv = %q, want %q", startedArgv, run.Command)
	}
	if run.Warning == "" || !strings.Contains(run.Warning, "administrator") {
		t.Errorf("Warning = %q, want a mention of elevation", run.Warning)
	}
	if len(wrapped) != 1 {
		t.Errorf("elevateWrap called %d times, want once", len(wrapped))
	}
	awaitTrue(t, func() bool {
		_, ok := events.exit()
		return ok
	})
	if exit, _ := events.exit(); exit.Code != 0 {
		t.Errorf("exit.Code = %d, want 0", exit.Code)
	}
}
