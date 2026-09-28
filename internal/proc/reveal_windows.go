//go:build windows

package proc

import "os/exec"

// Reveal opens the file's folder in Explorer with the file itself selected,
// which is the platform's "open file location". explorer is a GUI subsystem
// program, so no console window flashes, and it hands the request to the shell
// and returns, so the process is started and released rather than waited on.
func Reveal(path string) error {
	cmd := exec.Command("explorer", "/select,"+path)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
