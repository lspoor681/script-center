//go:build !windows && !darwin

package editor

import (
	"os"
	"path/filepath"
)

// DetectUnix returns the editors installed on Linux and the other unixes,
// ending with the system default so the list is never empty.
func DetectUnix() []Editor {
	var editors []Editor

	// 1. Neovim - highest priority if available
	if nvimPath, ok := isExecutableInPath("nvim"); ok {
		editors = append(editors, buildNeovimEditor(nvimPath))
	} else {
		// Check common Linux locations
		if nvimPath, ok := findInCommonPaths("nvim", []string{
			"/usr/bin",
			"/usr/local/bin",
			"/opt/nvim/bin",
			filepath.Join(os.Getenv("HOME"), ".local", "bin"),
			filepath.Join(os.Getenv("HOME"), "go", "bin"),
		}); ok {
			editors = append(editors, buildNeovimEditor(nvimPath))
		}
		// Check Flatpak
		if _, err := os.Stat("/var/lib/flatpak/exports/bin/org.neovim.nvim"); err == nil {
			editors = append(editors, buildNeovimEditor("/var/lib/flatpak/exports/bin/org.neovim.nvim"))
		}
		// Check Snap
		if _, err := os.Stat("/snap/bin/nvim"); err == nil {
			editors = append(editors, buildNeovimEditor("/snap/bin/nvim"))
		}
		// Check AppImage common locations
		if nvimPath, ok := findInCommonPaths("nvim.appimage", []string{
			filepath.Join(os.Getenv("HOME"), "Applications"),
			filepath.Join(os.Getenv("HOME"), ".local", "bin"),
			"/opt",
		}); ok {
			editors = append(editors, buildNeovimEditor(nvimPath))
		}
	}

	// 2. Visual Studio Code
	if codePath, ok := isExecutableInPath("code"); ok {
		editors = append(editors, buildVSCodeEditor(codePath))
	} else {
		if codePath, ok := findInCommonPaths("code", []string{
			"/usr/bin",
			"/usr/local/bin",
			"/opt/visual-studio-code/bin",
			filepath.Join(os.Getenv("HOME"), ".local", "bin"),
		}); ok {
			editors = append(editors, buildVSCodeEditor(codePath))
		}
		// Check Flatpak
		if _, err := os.Stat("/var/lib/flatpak/exports/bin/com.visualstudio.code"); err == nil {
			editors = append(editors, Editor{
				ID:          "vscode-flatpak",
				Name:        "Visual Studio Code (Flatpak)",
				Executable:  "flatpak",
				Args:        []string{"run", "com.visualstudio.code", "--wait"},
				Terminal:    false,
				TerminalCmd: nil,
				Extensions:  []string{},
				Source:      "detected",
			})
		}
	}

	// 3. Vim
	if vimPath, ok := isExecutableInPath("vim"); ok {
		editors = append(editors, Editor{
			ID:         "vim",
			Name:       "Vim",
			Executable: vimPath,
			Args:       []string{},
			Terminal:   true,
			TerminalCmd: [][]string{
				{"gnome-terminal", "--", "vim"},
				{"konsole", "-e", "vim"},
				{"xterm", "-e", "vim"},
				{"alacritty", "-e", "vim"},
				{"kitty", "-e", "vim"},
				{"vim"},
			},
			Extensions: []string{},
			Source:     "detected",
		})
	}

	// 4. Other common Linux editors
	editorChecks := []struct {
		id       string
		name     string
		cmd      string
		args     []string
		terminal bool
	}{
		{"nano", "Nano", "nano", []string{}, true},
		{"micro", "Micro", "micro", []string{}, true},
		{"emacs", "Emacs", "emacs", []string{"-nw"}, true},
		{"gedit", "Gedit", "gedit", []string{}, false},
		{"kate", "Kate", "kate", []string{}, false},
		{"subl", "Sublime Text", "subl", []string{"--wait"}, false},
		{"code-oss", "VS Code OSS", "code-oss", []string{"--wait"}, false},
	}

	for _, ec := range editorChecks {
		if path, ok := isExecutableInPath(ec.cmd); ok {
			var termCmds [][]string
			if ec.terminal {
				termCmds = [][]string{
					{"gnome-terminal", "--", ec.cmd},
					{"konsole", "-e", ec.cmd},
					{"xterm", "-e", ec.cmd},
					{"alacritty", "-e", ec.cmd},
					{"kitty", "-e", ec.cmd},
				}
			}
			editors = append(editors, Editor{
				ID:          ec.id,
				Name:        ec.name,
				Executable:  path,
				Args:        ec.args,
				Terminal:    ec.terminal,
				TerminalCmd: termCmds,
				Extensions:  []string{},
				Source:      "detected",
			})
		}
	}

	// 5. System default (always available as fallback)
	editors = append(editors, buildSystemEditor())

	return editors
}

// detectPlatform supplies the unix half of the per-platform dispatch. See the
// note on detectPlatform in detect_windows.go for why this is chosen at compile
// time rather than by switching on runtime.GOOS.
func (d *Detector) detectPlatform() []Editor { return DetectUnix() }
