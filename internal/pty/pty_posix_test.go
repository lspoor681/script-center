//go:build !windows

package pty

import (
	"errors"
	"syscall"
)

// processAlive reports whether pid still refers to a running process.
//
// Signal 0 runs the kernel's existence and permission checks without
// delivering anything. A process that exists but belongs to another user
// reports EPERM rather than ESRCH, so only ESRCH proves the process is gone and
// anything else is reported as alive. Reporting a live process on an error is
// deliberate: an inconclusive check must fail the test rather than pass it.
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	return !errors.Is(err, syscall.ESRCH)
}
