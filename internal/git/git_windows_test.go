//go:build windows

package git

import (
	"context"
	"testing"

	"golang.org/x/sys/windows"
)

func TestNewGitCmdHidesTheConsoleWindow(t *testing.T) {
	cmd := newGitCmd(context.Background(), t.TempDir(), "rev-parse")

	if cmd.Dir == "" {
		t.Fatal("newGitCmd did not set the working directory")
	}
	if cmd.SysProcAttr == nil {
		t.Fatal("newGitCmd did not set SysProcAttr")
	}
	if cmd.SysProcAttr.CreationFlags&windows.CREATE_NO_WINDOW == 0 {
		t.Fatalf("CreationFlags = 0x%x, want CREATE_NO_WINDOW (0x%x) set",
			cmd.SysProcAttr.CreationFlags, windows.CREATE_NO_WINDOW)
	}
}
