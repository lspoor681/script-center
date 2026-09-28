//go:build !windows

package workspace

import "os"

// renameFile moves src onto dst, replacing whatever was there.
//
// Renaming over an existing file is atomic everywhere except Windows, so this is
// the plain call. See rename_windows.go for why that platform needs more.
func renameFile(src, dst string) error { return os.Rename(src, dst) }
