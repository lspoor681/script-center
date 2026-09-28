//go:build !windows

package proc

import "os/exec"

// NoWindow is a no-op on platforms where child processes cannot create their
// own console windows.
func NoWindow(_ *exec.Cmd) {}
