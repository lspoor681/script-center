//go:build !windows

package proc

import (
	"os/exec"
	"testing"
)

func TestNoWindowIsANoOp(t *testing.T) {
	cmd := exec.Command("true")
	NoWindow(cmd)
	if cmd.SysProcAttr != nil {
		t.Fatal("NoWindow must not set SysProcAttr outside Windows")
	}
}
