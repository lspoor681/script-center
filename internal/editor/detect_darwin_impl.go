package editor

import (
	"os"
	"path/filepath"
)

func DetectDarwin() []Editor {
	var editors []Editor

	// 1. Neovim - highest priority if available
	if nvimPath, ok := isExecutableInPath("nvim"); ok {
		editors = append(editors, buildNeovimEditor(nvimPath))
	} else {
		// Check Homebrew, MacPorts, and common locations
		if nvimPath, ok := findInCommonPaths("nvim", []string{
			"/opt/homebrew/bin",
			"/usr/local/bin",
			"/opt/local/bin",
			filepath.Join(os.Getenv("HOME"), ".local", "bin"),
		}); ok {
			editors = append(editors, buildNeovimEditor(nvimPath))
		}
	}

	// 2. Visual Studio Code
	if codePath, ok := isExecutableInPath("code"); ok {
		editors = append(editors, buildVSCodeEditor(codePath))
	} else {
		// Check common macOS locations
		if codePath, ok := findInCommonPaths("code", []string{
			"/Applications/Visual Studio Code.app/Contents/Resources/app/bin",
			"/Applications/Visual Studio Code - Insiders.app/Contents/Resources/app/bin",
			filepath.Join(os.Getenv("HOME"), ".local", "bin"),
		}); ok {
			editors = append(editors, buildVSCodeEditor(codePath))
		}
	}

	// 3. Vim / MacVim
	if vimPath, ok := isExecutableInPath("vim"); ok {
		editors = append(editors, Editor{
			ID:          "vim",
			Name:        "Vim",
			Executable:  vimPath,
			Args:        []string{},
			Terminal:    true,
			TerminalCmd: [][]string{{"open", "-a", "Terminal", "--args", "vim"}},
			Extensions:  []string{},
			Source:      "detected",
		})
	}
	// MacVim
	if mvimPath, ok := isExecutableInPath("mvim"); ok {
		editors = append(editors, Editor{
			ID:          "macvim",
			Name:        "MacVim",
			Executable:  mvimPath,
			Args:        []string{},
			Terminal:    false,
			TerminalCmd: nil,
			Extensions:  []string{},
			Source:      "detected",
		})
	}

	// 4. BBEdit / TextWrangler
	if bbeditPath, ok := isExecutableInPath("bbedit"); ok {
		editors = append(editors, Editor{
			ID:          "bbedit",
			Name:        "BBEdit",
			Executable:  bbeditPath,
			Args:        []string{},
			Terminal:    false,
			TerminalCmd: nil,
			Extensions:  []string{},
			Source:      "detected",
		})
	}

	// 5. Sublime Text
	if sublPath, ok := isExecutableInPath("subl"); ok {
		editors = append(editors, Editor{
			ID:          "sublime",
			Name:        "Sublime Text",
			Executable:  sublPath,
			Args:        []string{"--wait"},
			Terminal:    false,
			TerminalCmd: nil,
			Extensions:  []string{},
			Source:      "detected",
		})
	}

	// 6. System default (always available as fallback)
	editors = append(editors, buildSystemEditor())

	return editors
}
