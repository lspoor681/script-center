package app

import "github.com/lspoor/script-center/internal/proc"

// RevealFile opens a script's folder in the platform's file manager, with the
// script itself selected where the manager supports it. The right-click menu
// calls it because the frontend cannot reach the filesystem.
func (s *Service) RevealFile(path string) error {
	return proc.Reveal(path)
}
