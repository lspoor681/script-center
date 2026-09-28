//go:build !windows && !darwin

package proc

import (
	"os/exec"
	"path/filepath"
)

// Reveal opens the file's folder in the default file manager. Linux file
// managers do not share a standard "reveal this file" verb, so the folder is
// opened and the file is left to the user to find.
func Reveal(path string) error {
	cmd := exec.Command("xdg-open", filepath.Dir(path))
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
