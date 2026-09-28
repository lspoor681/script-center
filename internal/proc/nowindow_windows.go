// Package proc configures the child processes the application launches in the
// background.
//
// The application is built as a GUI subsystem binary (Wails adds
// -H windowsgui), so the process has no console. On Windows, a console
// subsystem child such as git or pwsh that is started without CREATE_NO_WINDOW
// is given a brand-new console mapped to the desktop, which flashes a terminal
// window for every short-lived helper the app runs while reading a directory.
// proc.NoWindow prevents that. On other platforms the flag does not exist and
// nothing is needed, so the helper is a no-op there.
package proc

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// NoWindow tells the operating system not to create a console window for cmd.
// It must be called before cmd.Run or cmd.Start, and only has an effect on
// Windows.
func NoWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
}
