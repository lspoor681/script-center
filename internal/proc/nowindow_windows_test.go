//go:build windows

package proc

import (
	"os/exec"
	"testing"

	"golang.org/x/sys/windows"
)

func TestNoWindowSetsCreateNoWindow(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/c", "exit 0")
	NoWindow(cmd)
	if cmd.SysProcAttr == nil {
		t.Fatal("NoWindow did not set SysProcAttr")
	}
	if cmd.SysProcAttr.CreationFlags&windows.CREATE_NO_WINDOW == 0 {
		t.Fatalf("CreationFlags = 0x%x, want CREATE_NO_WINDOW (0x%x) set",
			cmd.SysProcAttr.CreationFlags, windows.CREATE_NO_WINDOW)
	}
}
