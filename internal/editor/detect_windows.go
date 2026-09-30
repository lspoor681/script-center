//go:build windows

package editor

import (
	"os"
	"path/filepath"
)

// DetectWindows returns the editors installed on Windows, ending with the
// system default so the list is never empty.
func DetectWindows() []Editor {
	var editors []Editor

	// 1. Neovim - highest priority if available
	if nvimPath, ok := isExecutableInPath("nvim"); ok {
		editors = append(editors, buildNeovimEditor(nvimPath))
	} else {
		// Check common Windows package managers
		if nvimPath, ok := findInCommonPaths("nvim.exe", []string{
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Neovim", "bin"),
			filepath.Join(os.Getenv("PROGRAMFILES"), "Neovim", "bin"),
			filepath.Join(os.Getenv("PROGRAMFILES(X86)"), "Neovim", "bin"),
			filepath.Join(os.Getenv("USERPROFILE"), "scoop", "apps", "neovim", "current", "bin"),
			filepath.Join(os.Getenv("USERPROFILE"), "scoop", "shims"),
			`C:\ProgramData\chocolatey\bin`,
			`C:\tools\neovim\bin`,
		}); ok {
			editors = append(editors, buildNeovimEditor(nvimPath))
		}
	}

	// 2. Visual Studio Code
	if codePath, ok := isExecutableInPath("code"); ok {
		editors = append(editors, buildVSCodeEditor(codePath))
	} else {
		// Check common VS Code install locations
		if codePath, ok := findInCommonPaths("Code.exe", []string{
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Microsoft VS Code", "bin"),
			filepath.Join(os.Getenv("PROGRAMFILES"), "Microsoft VS Code", "bin"),
			filepath.Join(os.Getenv("PROGRAMFILES(X86)"), "Microsoft VS Code", "bin"),
		}); ok {
			editors = append(editors, buildVSCodeEditor(codePath))
		}
		// Also check for VS Code Insiders
		if codePath, ok := isExecutableInPath("code-insiders"); ok {
			editors = append(editors, Editor{
				ID:          "vscode-insiders",
				Name:        "Visual Studio Code Insiders",
				Executable:  codePath,
				Args:        []string{"--wait"},
				Terminal:    false,
				TerminalCmd: nil,
				Extensions:  []string{},
				Source:      "detected",
			})
		}
	}

	// 3. Notepad++
	if nppPath, ok := isExecutableInPath("notepad++"); ok {
		editors = append(editors, buildNotepadppEditor(nppPath))
	} else {
		if nppPath, ok := findInCommonPaths("notepad++.exe", []string{
			filepath.Join(os.Getenv("PROGRAMFILES"), "Notepad++"),
			filepath.Join(os.Getenv("PROGRAMFILES(X86)"), "Notepad++"),
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Notepad++"),
		}); ok {
			editors = append(editors, buildNotepadppEditor(nppPath))
		}
	}

	// 4. PowerShell ISE (Windows only, for .ps1 files)
	isePath := filepath.Join(os.Getenv("WINDIR"), "System32", "WindowsPowerShell", "v1.0", "powershell_ise.exe")
	if _, err := os.Stat(isePath); err == nil {
		editors = append(editors, buildPSISEEditor(isePath))
	}

	// 5. Vim (if Neovim not found)
	if len(editors) == 0 || !hasEditor(editors, "neovim") {
		if vimPath, ok := isExecutableInPath("vim"); ok {
			editors = append(editors, Editor{
				ID:          "vim",
				Name:        "Vim",
				Executable:  vimPath,
				Args:        []string{},
				Terminal:    true,
				TerminalCmd: [][]string{{"wt", "vim"}, {"cmd", "/c", "start", "vim"}},
				Extensions:  []string{},
				Source:      "detected",
			})
		}
	}

	// 6. System default (always available as fallback)
	editors = append(editors, buildSystemEditor())

	return editors
}

func hasEditor(editors []Editor, id string) bool {
	for _, e := range editors {
		if e.ID == id {
			return true
		}
	}
	return false
}

// detectPlatform supplies the Windows half of the per-platform dispatch. The
// caller cannot choose it at runtime: a switch on runtime.GOOS would still need
// every branch's symbols to compile everywhere, and this file is excluded by
// its own build constraint off Windows. Exactly one of these three definitions
// is therefore compiled per platform, and detect.go carries no dispatcher.
func (d *Detector) detectPlatform() []Editor { return DetectWindows() }

// buildNotepadppEditor creates the Notepad++ editor entry. It lives here
// rather than in detect.go because Notepad++ is only ever detected here, so a
// shared home would leave it unused on every other platform.
func buildNotepadppEditor(nppPath string) Editor {
	return Editor{
		ID:          "notepadpp",
		Name:        "Notepad++",
		Executable:  nppPath,
		Args:        []string{},
		Terminal:    false,
		TerminalCmd: nil,
		Extensions:  []string{},
		Source:      "detected",
	}
}

// buildPSISEEditor creates the PowerShell ISE editor entry. It lives here for
// the same reason as buildNotepadppEditor: PowerShell ISE is a Windows shell.
func buildPSISEEditor(isePath string) Editor {
	return Editor{
		ID:          "psise",
		Name:        "PowerShell ISE",
		Executable:  isePath,
		Args:        []string{},
		Terminal:    false,
		TerminalCmd: nil,
		Extensions:  []string{".ps1", ".psm1", ".psd1", ".pssc"},
		Source:      "detected",
	}
}
