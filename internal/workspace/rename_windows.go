//go:build windows

package workspace

import (
	"errors"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// How hard to try before giving up on a rename that only failed because
// something else had the file open for a moment.
const (
	// renameAttempts is the number of tries, the first included.
	renameAttempts = 8
	// renameBackoff is the pause before the second try. It doubles on every
	// failure, so the eight attempts spend about 0.6s waiting altogether: long
	// enough to ride out an antivirus lock on one small file, and still short
	// enough that a genuine permission problem is reported quickly.
	renameBackoff = 5 * time.Millisecond
)

// renameFile moves src onto dst, replacing whatever was there.
//
// Windows makes this unreliable in a way Unix does not. Rename is implemented as
// delete-the-destination followed by move, and the destination has to be open for
// deletion, which fails while any other handle to it is open without
// FILE_SHARE_DELETE. Go's own file handles never share delete, so two saves
// racing in the same process collide with each other, and so does a real-time
// antivirus scanner watching the directory the application writes to. Both hold
// the file for milliseconds and both report ERROR_ACCESS_DENIED or
// ERROR_SHARING_VIOLATION, neither of which a caller can act on except by trying
// again a moment later.
//
// A permission problem reports the same ERROR_ACCESS_DENIED, so the retry cannot
// tell the two apart. It is bounded, and the error is returned unchanged once the
// attempts run out, so a genuinely unwritable file still fails and still says why.
func renameFile(src, dst string) error {
	delay := renameBackoff
	var err error
	for attempt := 1; attempt <= renameAttempts; attempt++ {
		if err = os.Rename(src, dst); err == nil {
			return nil
		}
		if !isTransientRenameError(err) || attempt == renameAttempts {
			return err
		}
		time.Sleep(delay)
		delay *= 2
	}
	return err
}

// isTransientRenameError reports whether err is a Windows complaint that
// clearing itself would fix.
func isTransientRenameError(err error) bool {
	// os.Rename reports a *os.LinkError, so the reason is one level down and
	// has to be unwrapped rather than compared.
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	switch errno {
	case windows.ERROR_ACCESS_DENIED,
		windows.ERROR_SHARING_VIOLATION,
		windows.ERROR_LOCK_VIOLATION:
		return true
	}
	return false
}
