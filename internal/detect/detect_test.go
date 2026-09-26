package detect

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFromExtension(t *testing.T) {
	for _, tc := range []struct {
		name string
		want Language
		ok   bool
	}{
		{"script.ps1", PowerShell, true},
		{"SCRIPT.PS1", PowerShell, true},
		{"module.psm1", PowerShell, true},
		{"manifest.psd1", PowerShell, true},
		{"run.bat", Batch, true},
		{"run.CMD", Batch, true},
		{"deploy.sh", Bash, true},
		{"deploy.bash", Bash, true},
		{"tool.py", Python, true},
		{"main.rs", Rust, true},
		// A manifest is not something to execute, so it is not a script.
		{"Cargo.toml", Unknown, false},
		{"README.md", Unknown, false},
		{"Makefile", Unknown, false},
		{"noextension", Unknown, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := FromExtension(tc.name)
			if got != tc.want || ok != tc.ok {
				t.Errorf("FromExtension(%q) = (%q, %v), want (%q, %v)",
					tc.name, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestFromShebang(t *testing.T) {
	for _, tc := range []struct {
		name string
		line string
		want Language
	}{
		{"abs bash", "#!/bin/bash", Bash},
		{"abs sh", "#!/bin/sh", Bash},
		{"env bash", "#!/usr/bin/env bash", Bash},
		{"env python3", "#!/usr/bin/env python3", Python},
		{"env python", "#!/usr/bin/env python", Python},
		{"env pwsh", "#!/usr/bin/env pwsh", PowerShell},
		{"env -S split", "#!/usr/bin/env -S bash -e", Bash},
		{"crlf", "#!/bin/bash\r", Bash},
		{"versioned", "#!/usr/bin/python3.12", Python},
		{"trailing args", "#!/bin/sh -eu", Bash},
		{"not a shebang", "echo hello", Unknown},
		{"unknown interpreter", "#!/usr/bin/perl", Unknown},
		{"empty", "#!", Unknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := FromShebang(tc.line); got != tc.want {
				t.Errorf("FromShebang(%q) = %q, want %q", tc.line, got, tc.want)
			}
		})
	}
}

// TestDetectFixtures checks detection against real files rather than strings,
// so the shebang and sniff stages are exercised the way they are in practice.
func TestDetectFixtures(t *testing.T) {
	for _, tc := range []struct {
		file string
		want Language
	}{
		{"pwsh-param.ps1", PowerShell},
		{"ps-module.psm1", PowerShell},
		{"batch.cmd", Batch},
		{"bash-shebang.sh", Bash},
		// No extension, so only the shebang can save it.
		{"no-ext-python", Python},
		{"rust-main.rs", Rust},
		// A config file must not be turned into a PowerShell form just because
		// it happens to contain the word "name".
		{"ambiguous-config", Unknown},
	} {
		t.Run(tc.file, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", tc.file))
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			if got := Detect(tc.file, data); got != tc.want {
				t.Errorf("Detect(%q) = %q, want %q", tc.file, got, tc.want)
			}
		})
	}
}

// TestDetectPrefersExtensionOverContent guards the stage ordering. A ".sh"
// file containing Python-ish words is still a bash script, because the
// extension is the stronger signal.
func TestDetectPrefersExtensionOverContent(t *testing.T) {
	body := []byte("def main():\n    import sys\n    print('hi')\n")
	if got := Detect("script.sh", body); got != Bash {
		t.Errorf("Detect(.sh with python body) = %q, want %q", got, Bash)
	}
}

// TestSniffRequiresMargin checks that genuinely ambiguous content yields
// Unknown rather than a coin flip between two languages.
func TestSniffRequiresMargin(t *testing.T) {
	// A one-token match is never enough on its own.
	if got := sniff("local x=1"); got != Unknown {
		t.Errorf("sniff(weak signal) = %q, want %q", got, Unknown)
	}
	// Balanced hits across two languages must not produce a winner.
	if got := sniff("import os set -e local x def f() import sys"); got != Unknown {
		t.Errorf("sniff(tied) = %q, want %q", got, Unknown)
	}
	// A decisive PowerShell body should still win.
	if got := sniff("$env:PATH #requires param( [string]$x write-host $env:HOME"); got != PowerShell {
		t.Errorf("sniff(powerShell) = %q, want %q", got, PowerShell)
	}
}

func TestDetectEmptyAndNil(t *testing.T) {
	if got := Detect("mystery", nil); got != Unknown {
		t.Errorf("Detect(nil content) = %q, want %q", got, Unknown)
	}
	if got := Detect("mystery", []byte{}); got != Unknown {
		t.Errorf("Detect(empty content) = %q, want %q", got, Unknown)
	}
}

func TestDisplayName(t *testing.T) {
	for _, tc := range []struct {
		lang Language
		want string
	}{
		{PowerShell, "PowerShell"},
		{Batch, "Batch"},
		{Bash, "Bash"},
		{Python, "Python"},
		{Rust, "Rust"},
		{Unknown, "Unknown"},
	} {
		if got := tc.lang.DisplayName(); got != tc.want {
			t.Errorf("Language(%q).DisplayName() = %q, want %q", tc.lang, got, tc.want)
		}
	}
}
