//go:build darwin

package proc

import "os/exec"

// Reveal opens the file's folder in Finder with the file itself selected,
// which is the platform's "open file location".
func Reveal(path string) error {
	cmd := exec.Command("open", "-R", path)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
