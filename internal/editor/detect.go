package editor

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
)

// Editor represents a text/code editor that can open script files.
type Editor struct {
	ID          string
	Name        string
	Executable  string
	Args        []string
	Terminal    bool
	TerminalCmd [][]string
	Extensions  []string
	Source      string
}

// Detector finds and caches available editors on the system.
type Detector struct {
	cache  []Editor
	cached bool
	mu     sync.Mutex
}

// Detect returns the list of available editors, cached for the session.
func (d *Detector) Detect() []Editor {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cached {
		return d.cache
	}
	d.cache = d.detectPlatform()
	d.cached = true
	return d.cache
}

// Invalidate clears the cache so the next Detect() re-scans.
func (d *Detector) Invalidate() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cached = false
	d.cache = nil
}

// detectPlatform is deliberately not defined here. A switch on runtime.GOOS
// would look equivalent, but it only chooses a branch at run time: the Go
// compiler still has to resolve every branch's symbols, so calling
// DetectWindows from it fails on any platform where the Windows file is
// excluded by its build constraint. The three platform files each define
// detectPlatform instead, so exactly one is compiled.

// isExecutableInPath checks if an executable exists in PATH.
func isExecutableInPath(name string) (string, bool) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", false
	}
	return path, true
}

// findInCommonPaths checks common installation directories for an executable.
func findInCommonPaths(name string, paths []string) (string, bool) {
	for _, p := range paths {
		full := filepath.Join(p, name)
		if _, err := os.Stat(full); err == nil {
			return full, true
		}
	}
	return "", false
}

// DetectPlatformEditors returns the list of available editors for the current platform.
// This is exported for use by other packages (e.g., proc).
func DetectPlatformEditors() []Editor {
	d := &Detector{}
	return d.Detect()
}

// buildNeovimEditor creates the Neovim editor entry with platform-specific terminal launchers.
func buildNeovimEditor(nvimPath string) Editor {
	var terminalCmd [][]string
	switch runtime.GOOS {
	case "windows":
		terminalCmd = [][]string{
			{"wt", "nvim"},
			{"pwsh", "-c", "nvim"},
			{"powershell", "-c", "nvim"},
			{"cmd", "/c", "start", "nvim"},
		}
	case "darwin":
		terminalCmd = [][]string{
			{"osascript", "-e", `tell app "Terminal" to do script "nvim "`},
			{"open", "-a", "iTerm", "--args", "nvim"},
			{"open", "-a", "Terminal", "--args", "nvim"},
		}
	default:
		terminalCmd = [][]string{
			{"gnome-terminal", "--", "nvim"},
			{"konsole", "-e", "nvim"},
			{"xterm", "-e", "nvim"},
			{"alacritty", "-e", "nvim"},
			{"kitty", "-e", "nvim"},
			{"nvim"},
		}
	}
	return Editor{
		ID:          "neovim",
		Name:        "Neovim",
		Executable:  nvimPath,
		Args:        []string{},
		Terminal:    true,
		TerminalCmd: terminalCmd,
		Extensions:  []string{},
		Source:      "detected",
	}
}

// buildVSCodeEditor creates the VS Code editor entry.
func buildVSCodeEditor(codePath string) Editor {
	return Editor{
		ID:          "vscode",
		Name:        "Visual Studio Code",
		Executable:  codePath,
		Args:        []string{"--wait"},
		Terminal:    false,
		TerminalCmd: nil,
		Extensions:  []string{},
		Source:      "detected",
	}
}

// buildSystemEditor creates the "system default" editor entry.
func buildSystemEditor() Editor {
	return Editor{
		ID:          "system",
		Name:        "System Default",
		Executable:  "",
		Args:        []string{},
		Terminal:    false,
		TerminalCmd: nil,
		Extensions:  []string{},
		Source:      "configured",
	}
}
