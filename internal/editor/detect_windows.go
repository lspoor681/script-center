package editor

import (
	"os"
	"path/filepath"
)

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
