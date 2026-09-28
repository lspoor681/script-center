package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lspoor/script-center/internal/detect"
	"github.com/lspoor/script-center/internal/params"
	"github.com/lspoor/script-center/internal/scan"
)

// viewScript builds a ScriptView for a rel path in a fake root. The path is
// deliberately not a real file: commandFor only asks what the view says.
func viewScript(rel string, language detect.Language, kind scan.Kind) ScriptView {
	root := filepath.Join("C:", "work", "scripts")
	path := filepath.Join(root, filepath.FromSlash(rel))
	return ScriptView{
		Path:     path,
		Root:     root,
		Rel:      filepath.ToSlash(rel),
		Name:     filepath.Base(path),
		Dir:      filepath.Dir(path),
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
			wantDir: powershell.Dir,
		},
		{
			name:    "python",
			script:  python,
			want:    []string{"/sandbox/tool", python.Path},
			wantDir: python.Dir,
		},
		{
			name:    "bash",
			script:  bash,
			want:    []string{"/sandbox/tool", bash.Path},
			wantDir: bash.Dir,
		},
		{
			name:    "batch",
			script:  batch,
			want:    []string{"/sandbox/tool", "/c", batch.Path},
			wantDir: batch.Dir,
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
		if _, err := service.RunScript(context.Background(), missing, "x.ps1", nil); err == nil {
			t.Fatal("RunScript on a missing root returned no error")
		}
	})

	root := scriptTree(t)
	if _, err := service.OpenRoot(context.Background(), root); err != nil {
		t.Fatal(err)
	}

	t.Run("a script the root does not have", func(t *testing.T) {
		if _, err := service.RunScript(context.Background(), root, "missing.ps1", nil); !errors.Is(err, ErrNoScript) {
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

	run, err := service.RunScript(context.Background(), root, script.Rel, []string{"--count", "3"})
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

// TestRunScriptWarnsAboutElevation checks the one piece of RunScript that is
// not about starting the process: a script that asks to be elevated gets told
// it will not be.
func TestRunScriptWarnsAboutElevation(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the local CI host cannot start Windows PowerShell to host the elevation test")
	}
	pinInterpreter(t, func(detect.Language) string { return "powershell.exe" })

	service, _ := newTestService(t)
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

	run, err := service.RunScript(context.Background(), root, "admin.ps1", nil)
	if err != nil {
		t.Fatalf("RunScript: %v", err)
	}
	if run.Warning == "" || !strings.Contains(run.Warning, "administrator") {
		t.Errorf("Warning = %q, want a mention of elevation", run.Warning)
	}
	t.Cleanup(func() { _ = service.StopScript(run.ID) })
}
