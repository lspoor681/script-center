//go:build darwin

package proc

import (
	"os/exec"
	"strings"
)

// CopyText puts text on the macOS clipboard via pbcopy, which reads the text
// on standard input.
func CopyText(text string) error {
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}
