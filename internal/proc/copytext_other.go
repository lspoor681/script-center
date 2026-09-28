//go:build !windows && !darwin

package proc

import (
	"errors"
	"os/exec"
	"strings"
)

// CopyText puts text on the clipboard with whichever of wl-copy (Wayland),
// xclip or xsel (X11) is installed. Clipboard access is best effort on a
// workstation; a headless session has no clipboard, which surfaces as an
// error so the caller can tell the user.
func CopyText(text string) error {
	for _, tool := range []struct {
		name string
		args string
	}{
		{"wl-copy", ""},
		{"xclip", "-selection clipboard"},
		{"xsel", "-b"},
	} {
		if _, err := exec.LookPath(tool.name); err != nil {
			continue
		}
		cmd := exec.Command(tool.name)
		cmd.Args = append(cmd.Args, strings.Fields(tool.args)...)
		cmd.Stdin = strings.NewReader(text)
		return cmd.Run()
	}
	return errors.New("copy: no clipboard tool found (wl-copy, xclip or xsel)")
}
