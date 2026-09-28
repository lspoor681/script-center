package app

import "github.com/lspoor/script-center/internal/proc"

// RevealFile opens a script's folder in the platform's file manager, with the
// script itself selected where the manager supports it. The right-click menu
// calls it because the frontend cannot reach the filesystem.
func (s *Service) RevealFile(path string) error {
	return proc.Reveal(path)
}

// CopyText places text on the system clipboard. The right-click menu's "Copy
// path" and the run panel's "Copy command" use it, because the browser context
// the frontend runs in has no reliable clipboard of its own.
func (s *Service) CopyText(text string) error {
	return proc.CopyText(text)
}
