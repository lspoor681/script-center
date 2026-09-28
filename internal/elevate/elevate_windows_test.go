//go:build windows

package elevate

import (
	"testing"
)

// TestJoinParameters checks the command line argv[1:] is rendered into. Round
// trips are asserted against syscall.EscapeArg's rules, which are the ones
// CommandLineToArgvW-parsing programs apply.
func TestJoinParameters(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{nil, ""},
		{[]string{"-File", "a b.ps1", "--name", `C:\dir\file`}, `-File "a b.ps1" --name C:\dir\file`},
		{[]string{`has"quote`}, `has\"quote`},
		{[]string{`ends\`}, `ends\`},
		{[]string{""}, `""`},
	}
	for _, tt := range tests {
		if got := joinParameters(tt.args); got != tt.want {
			t.Errorf("joinParameters(%q) = %q, want %q", tt.args, got, tt.want)
		}
	}
}

// TestPlatformProbeSmoke pins the one invariant the probe promises: on
// Windows an elevated run can always be started, and the report names the
// mechanism.
func TestPlatformProbeSmoke(t *testing.T) {
	status := Detect()
	if !status.Available {
		t.Errorf("Detect().Available = false, want true (runas always works on Windows)")
	}
	if status.Method == "" {
		t.Error("Detect().Method is empty")
	}
	if status.Inline && len(status.prefix) == 0 {
		t.Error("Detect() reports inline elevation without a prefix")
	}
}
