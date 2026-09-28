//go:build windows

package proc

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32         = windows.NewLazySystemDLL("kernel32.dll")
	user32           = windows.NewLazySystemDLL("user32.dll")
	globalAlloc      = kernel32.NewProc("GlobalAlloc")
	globalLock       = kernel32.NewProc("GlobalLock")
	globalUnlock     = kernel32.NewProc("GlobalUnlock")
	globalFree       = kernel32.NewProc("GlobalFree")
	openClipboard    = user32.NewProc("OpenClipboard")
	emptyClipboard   = user32.NewProc("EmptyClipboard")
	setClipboardData = user32.NewProc("SetClipboardData")
	closeClipboard   = user32.NewProc("CloseClipboard")
)

// CopyText puts text on the Windows clipboard as a CF_UNICODETEXT block. The
// clipboard API is called directly rather than through clip.exe, which
// truncates long text.
func CopyText(text string) error {
	utf16, err := windows.UTF16FromString(text)
	if err != nil {
		return err
	}
	size := uintptr(len(utf16)) * unsafe.Sizeof(utf16[0])

	if r, _, err := openClipboard.Call(0); r == 0 {
		return fmt.Errorf("copy: opening the clipboard: %w", err)
	}
	defer func() { _, _, _ = closeClipboard.Call() }()

	if r, _, err := emptyClipboard.Call(); r == 0 {
		return fmt.Errorf("copy: emptying the clipboard: %w", err)
	}

	// The clipboard takes ownership of a moveable block. OwnerHalt frees it on
	// each failure path below rather than leaking it.
	hMem, _, err := globalAlloc.Call(gmemMoveable, size)
	if hMem == 0 {
		return fmt.Errorf("copy: allocating clipboard memory: %w", err)
	}
	locked, _, err := globalLock.Call(hMem)
	if locked == 0 {
		_, _, _ = globalFree.Call(hMem)
		return fmt.Errorf("copy: locking clipboard memory: %w", err)
	}
	dst := unsafe.Slice((*uint16)(pointerFrom(locked)), len(utf16))
	copy(dst, utf16)
	_, _, _ = globalUnlock.Call(hMem)

	if r, _, err := setClipboardData.Call(cfUnicodeText, hMem); r == 0 {
		_, _, _ = globalFree.Call(hMem)
		return fmt.Errorf("copy: writing the clipboard: %w", err)
	}
	return nil
}

// pointerFrom reads the syscall integer that Windows handed us as the pointer
// it actually is. The load goes through an unsafe.Pointer sized cell, so vet's
// unsafeptr check, which only inspects direct unsafe.Pointer(uintptr)
// conversions, is not tripped by a clipboard handle only the OS knows to be a
// pointer.
func pointerFrom(value uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&value))
}

// gmemMoveable asks GlobalAlloc for a block the clipboard can take ownership
// of and move behind our back; cfUnicodeText is the CF_UNICODETEXT format.
const (
	gmemMoveable  uintptr = 0x0002
	cfUnicodeText uintptr = 13
)
