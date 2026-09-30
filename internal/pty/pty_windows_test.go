//go:build windows

package pty

import (
	"errors"

	"golang.org/x/sys/windows"
)

// stillActive is the value GetExitCodeProcess reports for a process that has
// not exited yet. It is STATUS_PENDING, documented as STILL_ACTIVE in the
// Win32 header, and neither x/sys/windows nor syscall exports it, so it is
// spelled out here rather than inlined at the comparison.
const stillActive = 259

// processAlive reports whether pid still refers to a running process.
//
// Windows makes this less direct than on Unix. OpenProcess fails with
// ERROR_INVALID_PARAMETER only once nothing is left under that id, but a handle
// to a process that has exited and not yet been reaped still opens, so success
// alone proves nothing. The exit code is what separates the two cases:
// STILL_ACTIVE is the only value Windows reports for a process that is still
// executing, and a finished process reports its real status instead. The test
// calls Wait first, so the process has been reaped by then and this is not
// racing a teardown.
func processAlive(pid int) bool {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		// ERROR_INVALID_PARAMETER means no process carries that id. Any other
		// failure, access denied being the likely one, cannot establish that the
		// process is gone, so it is reported as alive and the test fails.
		return !errors.Is(err, windows.ERROR_INVALID_PARAMETER)
	}
	defer windows.CloseHandle(handle)

	var code uint32
	if err := windows.GetExitCodeProcess(handle, &code); err != nil {
		return true
	}
	return code == stillActive
}
