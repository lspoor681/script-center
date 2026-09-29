package proc

import (
	"os/exec"
	"runtime"

	"github.com/lspoor/script-center/internal/editor"
)

// OpenFile opens a file with the specified editor.
// editorID "system" means use OS default.
// For terminal editors, launches in a new terminal window.
func OpenFile(path, editorID string) error {
	editors := editor.DetectPlatformEditors()
	var ed *editor.Editor
	for i := range editors {
		if editors[i].ID == editorID {
			ed = &editors[i]
			break
		}
	}
	if ed == nil {
		// Fallback to system default
		return openWithSystemDefault(path)
	}

	if ed.Terminal {
		return openInTerminal(path, ed)
	}
	return openGUI(path, ed)
}

// openWithSystemDefault uses the OS default application for the file type.
func openWithSystemDefault(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}

// openGUI opens a file with a GUI editor.
func openGUI(path string, ed *editor.Editor) error {
	args := append([]string{}, ed.Args...)
	args = append(args, path)
	cmd := exec.Command(ed.Executable, args...)
	return cmd.Start()
}

// openInTerminal opens a file with a terminal editor in a new terminal window.
func openInTerminal(path string, ed *editor.Editor) error {
	for _, termCmd := range ed.TerminalCmd {
		args := make([]string, len(termCmd))
		copy(args, termCmd)
		// Append the file path to the terminal command
		args = append(args, path)

		cmd := exec.Command(args[0], args[1:]...)
		if err := cmd.Start(); err == nil {
			return cmd.Process.Release()
		}
		// Try next terminal launcher
	}
	// All terminal launchers failed, try direct execution
	cmd := exec.Command(ed.Executable, path)
	return cmd.Start()
}
