// Package detect identifies which interpreter a script needs.
//
// Detection runs in three stages, cheapest and most reliable first: file
// extension, then a shebang line, then content signatures. A stage only runs
// when the previous one was inconclusive.
//
// Content sniffing deliberately errs towards Unknown. A wrong answer is far
// more costly than no answer: the application would generate a parameter form
// for the wrong interpreter, and a form that is subtly wrong is harder to spot
// and more annoying than a plain "unrecognized script" notice.
package detect

import (
	"bytes"
	"path/filepath"
	"strings"
)

// Language identifies the interpreter or toolchain needed to run a script.
type Language string

// The languages the application can generate execution forms for.
const (
	PowerShell Language = "powershell"
	Batch      Language = "batch"
	Bash       Language = "bash"
	Python     Language = "python"
	Rust       Language = "rust"

	// Unknown means detection was inconclusive. The script is still listed so
	// the user can run it, but no parameter form is generated and only the
	// generic remote transports are offered.
	Unknown Language = "unknown"
)

// String implements fmt.Stringer.
func (l Language) String() string { return string(l) }

// DisplayName returns a human-readable label for the language.
func (l Language) DisplayName() string {
	switch l {
	case PowerShell:
		return "PowerShell"
	case Batch:
		return "Batch"
	case Bash:
		return "Bash"
	case Python:
		return "Python"
	case Rust:
		return "Rust"
	default:
		return "Unknown"
	}
}

// extensions maps a lowercase file extension to its language. The extension
// includes the leading dot.
var extensions = map[string]Language{
	".ps1":  PowerShell,
	".psm1": PowerShell,
	".psd1": PowerShell,
	".pssc": PowerShell,
	".bat":  Batch,
	".cmd":  Batch,
	".sh":   Bash,
	".bash": Bash,
	".zsh":  Bash,
	".ksh":  Bash,
	".py":   Python,
	".pyw":  Python,
	".rs":   Rust,
}

// Interpreters maps the executable name from a shebang, and the interpreter
// name from an extension, to a language.
var interpreters = map[string]Language{
	"pwsh":        PowerShell,
	"powershell":  PowerShell,
	"sh":          Bash,
	"bash":        Bash,
	"zsh":         Bash,
	"ksh":         Bash,
	"dash":        Bash,
	"python":      Python,
	"python2":     Python,
	"python3":     Python,
	"pypy":        Python,
	"rust-script": Rust,
	"cargo":       Rust,
}

// FromExtension returns the language implied by a file extension. The second
// return is false when the extension is not registered, which is the common
// case in a repository and must not be treated as an error.
func FromExtension(name string) (Language, bool) {
	// The only extensions that can be capitalised in practice are none, but
	// folding case costs nothing and guards against "Foo.PS1".
	ext := strings.ToLower(filepath.Ext(name))
	if lang, ok := extensions[ext]; ok {
		return lang, true
	}
	return Unknown, false
}

// FromShebang interprets a first line of the form "#! ...". It returns Unknown
// for anything that is not a recognized shebang.
func FromShebang(line string) Language {
	line = strings.TrimRight(line, "\r\n")
	if !strings.HasPrefix(line, "#!") {
		return Unknown
	}

	fields := strings.Fields(strings.TrimPrefix(line, "#!"))
	if len(fields) == 0 {
		return Unknown
	}

	// `#!/usr/bin/env bash` puts the real interpreter in the second field, and
	// `env -S` style shebangs interleave flags with it.
	if filepath.Base(fields[0]) == "env" {
		for _, f := range fields[1:] {
			if strings.HasPrefix(f, "-") {
				continue
			}
			return fromInterpreter(filepath.Base(f))
		}
		return Unknown
	}

	return fromInterpreter(filepath.Base(fields[0]))
}

func fromInterpreter(name string) Language {
	if lang, ok := interpreters[name]; ok {
		return lang
	}
	// Tolerate a version suffix, in either the dotted form used by Python
	// ("python3.12") or the bare form used by shells ("bash5").
	if i := strings.LastIndex(name, "."); i > 0 && isDigits(name[i+1:]) {
		if lang, ok := interpreters[name[:i]]; ok {
			return lang
		}
	}
	if trimmed := strings.TrimRight(name, "0123456789"); trimmed != "" && trimmed != name {
		if lang, ok := interpreters[trimmed]; ok {
			return lang
		}
	}
	return Unknown
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// sniffBytes is how much of a file is inspected by Detect when extension and
// shebang both fail to identify it. It is deliberately small: a script's
// identifying tokens appear in the first few lines, and reading a large
// proportion of every file in a repository would be slow.
const sniffBytes = 4096

// Detect identifies the language of a file from its name and leading content.
// content may be nil, in which case only the name is considered.
func Detect(name string, content []byte) Language {
	if lang, ok := FromExtension(name); ok {
		return lang
	}

	if lang := detectFromContent(content); lang != Unknown {
		return lang
	}
	return Unknown
}

// detectFromContent applies the shebang stage and then, if that was
// inconclusive, the signature stage.
func detectFromContent(content []byte) Language {
	if len(content) == 0 {
		return Unknown
	}

	firstLine := content
	if i := bytes.IndexByte(content, '\n'); i >= 0 {
		firstLine = content[:i]
	}
	if lang := FromShebang(string(firstLine)); lang != Unknown {
		return lang
	}

	return sniff(string(content[:min(len(content), sniffBytes)]))
}

// signatures are substrings that strongly suggest a language, weighted so that
// decisive tokens count more than generic ones. A bare "set" would be far too
// weak and is deliberately absent.
var signatures = map[Language][]string{
	PowerShell: {
		"#requires", "param(", "param [", "cmdletbinding(", "write-host",
		"$env:", "$psscriptroot", "$pscommandpath", "$myinvocation",
		"invoke-webrequest", "invoke-command", "import-module",
	},
	Batch: {
		"@echo off", "@echo on", "setlocal", "endlocal", "goto :eof",
		"rem ", "if exist", "%errorlevel%",
	},
	Bash: {
		"set -e", "set -u", "set -o", "esac", "fi\n", "local ", "readonly ",
		"declare -a", "source ", "echo \"$", "exit 0", "trap ",
	},
	Python: {
		"def ", "import ", "from ", "print(", "if __name__", "self.",
		"argparse", "subprocess.run", "def __init__",
	},
	Rust: {
		"fn main", "use std::", "#[derive", "println!", "let mut ", "impl ",
		"cargo", "mod tests", "#[test]",
	},
}

// sniff scores a file body against every language's signatures and returns the
// clear winner, or Unknown when the result is too close to call.
func sniff(body string) Language {
	lowered := strings.ToLower(body)

	scores := make(map[Language]int, len(signatures))
	for lang, sigs := range signatures {
		for _, sig := range sigs {
			if strings.Contains(lowered, sig) {
				scores[lang]++
			}
		}
	}

	var best Language
	bestScore, secondScore := 0, 0
	for lang, score := range scores {
		switch {
		case score > bestScore:
			secondScore = bestScore
			best, bestScore = lang, score
		case score > secondScore:
			secondScore = score
		}
	}

	// Require at least three signals and a clear margin. Two scripts written
	// in different languages often share incidental tokens, and guessing wrong
	// is worse than admitting uncertainty.
	if bestScore >= 3 && bestScore-secondScore >= 2 {
		return best
	}
	return Unknown
}
