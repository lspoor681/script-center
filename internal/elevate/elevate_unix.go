//go:build !windows

package elevate

import (
	"errors"
	"os/exec"
)

// platformProbe reports how an elevated command can be started on this
// machine. Unix has one option, sudo; without it a script that needs root
// privileges cannot be run from the app.
func platformProbe() Status {
	if sudo, err := exec.LookPath("sudo"); err == nil {
		return Status{
			Available: true,
			Inline:    true,
			Method:    "sudo",
			Hint:      "sudo found on PATH",
			prefix:    []string{sudo},
		}
	}
	return Status{
		Available: false,
		Method:    "sudo",
		Hint:      "install sudo (on Ubuntu: apt install sudo) so scripts that need root privileges can run from this app",
	}
}

// StartElevated is meaningless off Windows, where an inline elevated run is
// always available once sudo is; something else has gone wrong if it is asked.
func StartElevated(argv []string, dir string) (*Process, error) {
	return nil, errors.New("elevate: an elevated run in a separate window is a Windows-only path; use sudo instead")
}
