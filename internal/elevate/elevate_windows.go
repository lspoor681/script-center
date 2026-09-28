//go:build windows

package elevate

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// platformProbe inspects the machine's elevation options. Windows has two: the
// sudo command, whose inline mode keeps the elevated command attached to the
// app's pseudo-terminal, and the runas verb, which always starts a separate
// elevated process in its own console.
func platformProbe() Status {
	sudo, err := exec.LookPath("sudo")
	if err != nil {
		return Status{
			Available: true,
			Inline:    false,
			Method:    "runas",
			Hint:      "Windows sudo (Windows 11 24H2 or later) is not installed, so an elevated run opens in its own window",
		}
	}
	if sudoEnabled() == inlineSudo {
		return Status{
			Available: true,
			Inline:    true,
			Method:    "sudo (inline)",
			Hint:      "Windows sudo is ready to use in inline mode",
			prefix:    []string{sudo},
		}
	}
	return Status{
		Available: true,
		Inline:    false,
		Method:    "runas",
		Hint:      "Windows sudo is present but its inline mode is off; enable it (Settings → System → For developers → “Enable sudo”, mode Inline) so the run can stay in this app. For now an elevated run opens in its own window",
	}
}

// inlineSudo is the value of the Windows sudo configuration that keeps the
// elevated command in the caller's console. Any other value, including an
// absent key, means the run would have to move to a window of its own.
const inlineSudo uint64 = 2

// sudoEnabled reads the Windows sudo mode, which lives under the current
// version's Sudo key for the whole machine and can be overridden per user.
func sudoEnabled() uint64 {
	for _, root := range []registry.Key{registry.LOCAL_MACHINE, registry.CURRENT_USER} {
		k, err := registry.OpenKey(root, `SOFTWARE\Microsoft\Windows\CurrentVersion\Sudo`, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		value, _, err := k.GetIntegerValue("Enabled")
		_ = k.Close()
		if err == nil {
			return value
		}
	}
	return 0
}

// StartElevated starts argv in dir with the user's administrator privileges,
// in a window of its own, which is what Windows must settle for when sudo
// inline mode is not enabled. The UAC prompt does the elevation, so a user who
// dismisses it gets an error rather than a run that quietly started anyway.
func StartElevated(argv []string, dir string) (*Process, error) {
	if len(argv) == 0 {
		return nil, errors.New("elevate: an argv is required")
	}

	verb, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return nil, err
	}
	file, err := windows.UTF16PtrFromString(argv[0])
	if err != nil {
		return nil, err
	}
	parameters, err := windows.UTF16PtrFromString(joinParameters(argv[1:]))
	if err != nil {
		return nil, err
	}

	info := &shellExecuteInfo{
		fMask:        seeMaskNoCloseProcess,
		lpVerb:       verb,
		lpFile:       file,
		lpParameters: parameters,
		nShow:        1, // SW_SHOWNORMAL: the elevated window has to be visible
	}
	if dir != "" {
		info.lpDirectory, err = windows.UTF16PtrFromString(dir)
		if err != nil {
			return nil, err
		}
	}
	info.cbSize = uint32(unsafe.Sizeof(*info))

	ok, _, callErr := shellExecuteEx.Call(uintptr(unsafe.Pointer(info)))
	if ok == 0 {
		//nolint:misspell // windows.ERROR_CANCELLED is the Win32 constant's own name
		if errors.Is(callErr, windows.ERROR_CANCELLED) {
			return nil, errors.New("elevate: the elevation prompt was dismissed")
		}
		return nil, fmt.Errorf("elevate: starting %q elevated: %w", argv[0], callErr)
	}
	if info.hProcess == 0 {
		return nil, fmt.Errorf("elevate: starting %q elevated: no process handle was returned", argv[0])
	}

	handle := windows.Handle(info.hProcess)
	return &Process{
		wait: func() int {
			_, _ = windows.WaitForSingleObject(handle, windows.INFINITE)
			var code uint32
			if err := windows.GetExitCodeProcess(handle, &code); err != nil {
				return -1
			}
			return int(code)
		},
		close: func() error {
			return windows.CloseHandle(handle)
		},
	}, nil
}

// shellExecuteEx invokes ShellExecuteExW, the only way to ask for the runas
// verb (a UAC prompt) from a process that is not itself elevated.
var shellExecuteEx = windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW")

// seeMaskNoCloseProcess asks ShellExecuteExW to hand back a live process
// handle, which is what lets the app wait for the elevated run to end.
const seeMaskNoCloseProcess uint32 = 0x00000040

// shellExecuteInfo is SHELLEXECUTEINFOW. The field order, sizes and alignment
// match the Windows definition on both 32- and 64-bit, which is what lets it
// be passed by address to ShellExecuteExW.
type shellExecuteInfo struct {
	cbSize       uint32
	fMask        uint32
	hwnd         uintptr
	lpVerb       *uint16
	lpFile       *uint16
	lpParameters *uint16
	lpDirectory  *uint16
	nShow        int32
	hInstApp     uintptr
	lpIDList     uintptr
	lpClass      *uint16
	hkeyClass    uintptr
	dwHotKey     uint32
	hIcon        uintptr
	hProcess     uintptr
}

// joinParameters renders argv[1:] into a single Windows command line, quoting
// each argument the way a CommandLineToArgvW-parsing program (PowerShell, cmd,
// and the C runtimes) expects.
func joinParameters(args []string) string {
	if len(args) == 0 {
		return ""
	}
	var b strings.Builder
	for i, arg := range args {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(syscall.EscapeArg(arg))
	}
	return b.String()
}
