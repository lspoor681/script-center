// Package deps works out which files have to travel with a script.
//
// A PowerShell script that dot-sources a helper is not runnable on its own. That
// is invisible locally, where the helper sits next to the script, and it is the
// single most common reason a script copied to a remote host fails immediately.
// Resolving the closure is therefore a precondition for any remote run rather
// than an optimisation.
package deps

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// MaxDepth bounds how far the closure is followed. A helper that dot-sources
// another helper is normal; a cycle is a mistake in the script, and depth is what
// stops one from being walked forever.
const MaxDepth = 16

// ErrNoEntry is returned when there is no script to resolve.
var ErrNoEntry = errors.New("deps: no entry script")

// Find reports the paths a script dot-sources, as written. It is supplied by the
// caller so that this package does not need to know how a language is parsed:
// internal/params supplies a PowerShell implementation, and a language with no
// dot-source concept supplies nil.
type Find func(path string) ([]string, error)

// Closure is the set of files that must accompany a script.
type Closure struct {
	// Entry is the script the closure was built for.
	Entry string
	// Files are the absolute paths of every file that must be copied, including
	// the entry itself. They are ordered so that a helper appears before the
	// script that dot-sources it, which makes a copy log readable.
	Files []string
	// Unresolved are the dot-source expressions that could not be turned into a
	// path, such as a bare variable or one built from an environment variable.
	// They are reported rather than dropped, because a script that dot-sources
	// something unknown is a script that may not run on a remote host.
	Unresolved []string
	// Warnings explain anything else that went wrong.
	Warnings []string
}

// HasUnresolved reports whether part of the closure could not be determined.
func (c Closure) HasUnresolved() bool { return len(c.Unresolved) > 0 }

// AddWarning records a problem, ignoring blanks and exact repeats.
func (c *Closure) AddWarning(format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	if message == "" {
		return
	}
	for _, existing := range c.Warnings {
		if existing == message {
			return
		}
	}
	c.Warnings = append(c.Warnings, message)
}

// Resolver builds a closure, following nested dot-sources.
type Resolver struct {
	// Find supplies the dot-sources of a file. When nil, only the expressions
	// given to ResolveOnce are resolved and no file is read.
	Find Find
	// MaxDepth overrides the default depth limit.
	MaxDepth int
}

// Resolve walks the closure of a script, following nested dot-sources when Find
// is set. The entry itself is always the first file.
func (r Resolver) Resolve(entry string, dotSources []string) (Closure, error) {
	if strings.TrimSpace(entry) == "" {
		return Closure{}, ErrNoEntry
	}
	entry, err := filepath.Abs(entry)
	if err != nil {
		return Closure{}, fmt.Errorf("deps: resolving the entry path: %w", err)
	}

	closure := Closure{Entry: entry}
	// seen guards against both a cycle and the same helper being reached by two
	// different routes, which would otherwise be copied twice.
	seen := map[string]bool{entry: true}
	closure.Files = append(closure.Files, entry)

	r.walk(entry, dotSources, 0, seen, &closure)
	return closure, nil
}

// ResolveOnce builds a closure from a known list of dot-sources without reading
// any file. It is the right choice when only the top-level script has been
// parsed, which is the common case for a list the user is browsing.
func ResolveOnce(entry string, dotSources []string) (Closure, error) {
	return Resolver{}.Resolve(entry, dotSources)
}

// walk adds one file's dot-sources to the closure, recursively.
func (r Resolver) walk(path string, dotSources []string, depth int, seen map[string]bool, closure *Closure) {
	maxDepth := r.MaxDepth
	if maxDepth <= 0 {
		maxDepth = MaxDepth
	}
	if depth > maxDepth {
		closure.AddWarning("%s: gave up after following %d levels of dot-sources",
			filepath.Base(path), maxDepth)
		return
	}

	scriptDir := filepath.Dir(path)

	// Sort so that a script with two helpers always produces the same order, and
	// so that a helper listed before the entry's other helper is copied first.
	expressions := append([]string(nil), dotSources...)
	sort.Strings(expressions)

	for _, expression := range expressions {
		trimmed := strings.TrimSpace(expression)
		if trimmed == "" {
			continue
		}

		resolved, err := ResolveExpression(trimmed, scriptDir)
		if err != nil {
			closure.Unresolved = appendUnique(closure.Unresolved, trimmed)
			closure.AddWarning("%s: cannot resolve the dot-source %q: %v",
				filepath.Base(path), trimmed, err)
			continue
		}

		if seen[resolved] {
			// Either a cycle or a helper shared by two scripts. Either way it is
			// already accounted for.
			continue
		}
		seen[resolved] = true

		info, err := os.Stat(resolved)
		switch {
		case errors.Is(err, os.ErrNotExist):
			closure.AddWarning("%s: the dot-sourced file %s does not exist",
				filepath.Base(path), resolved)
			continue
		case err != nil:
			closure.AddWarning("%s: cannot read the dot-sourced file %s: %v",
				filepath.Base(path), resolved, err)
			continue
		case info.IsDir():
			closure.AddWarning("%s: the dot-source %s is a directory, not a script",
				filepath.Base(path), resolved)
			continue
		}

		closure.Files = append(closure.Files, resolved)

		// Follow the helper's own dot-sources when a parser is available.
		if r.Find != nil {
			nested, err := r.Find(resolved)
			if err != nil {
				closure.AddWarning("%s: cannot inspect the dot-sources of %s: %v",
					filepath.Base(path), resolved, err)
				continue
			}
			r.walk(resolved, nested, depth+1, seen, closure)
		}
	}
}

// ResolveExpression turns one dot-source expression into an absolute path,
// relative to the directory of the script that wrote it.
//
// The expression is text taken from the source file, so it may be a quoted or
// interpolated string, a bare relative path, or something computed at run time.
// Only the forms whose value is knowable without running the script are
// resolved; anything else is an error naming what could not be read.
func ResolveExpression(expression, scriptDir string) (string, error) {
	text := strings.TrimSpace(expression)

	// Strip one layer of matching quotes, which survive when the parser reported
	// the source text rather than the string's value.
	if len(text) >= 2 {
		first, last := text[0], text[len(text)-1]
		if (first == '\'' || first == '"') && first == last {
			text = text[1 : len(text)-1]
		}
	}

	// $PSScriptRoot is the only variable whose value is knowable here, because it
	// is the directory of the script that wrote the expression. Any other
	// variable depends on the caller's session.
	if strings.Contains(text, "$PSScriptRoot") {
		// What is left after removing the variable is the path within the
		// script's own directory. An expression that was nothing but the
		// variable names a directory rather than a file, which is a different
		// problem and is reported as one.
		within := strings.Trim(strings.ReplaceAll(text, "$PSScriptRoot", ""), "/\\ \t")
		if within == "" {
			return "", errors.New("the expression names $PSScriptRoot itself rather than a file in it")
		}
		return normalize(filepath.Join(scriptDir, filepath.FromSlash(within))), nil
	}

	if strings.Contains(text, "$") {
		return "", fmt.Errorf("the expression depends on a variable, whose value is only known at run time (%s)", text)
	}

	if filepath.IsAbs(text) {
		return normalize(text), nil
	}

	// A relative path is relative to the script, which is how PowerShell
	// resolves it, not to the working directory the user happens to be in.
	if strings.HasPrefix(text, ".") || strings.ContainsAny(text, `/\`) {
		return normalize(filepath.Join(scriptDir, filepath.FromSlash(text))), nil
	}

	// A bare word with no path separator is ambiguous: it could be a command on
	// PATH, a file in the current directory, or a name in the caller's module
	// path. Guessing would produce a copy list that is wrong in a way the user
	// cannot see.
	return "", fmt.Errorf("the expression names no path, so it cannot be resolved without running the script (%s)", text)
}

// normalize cleans a path and settles its separators. A dot-source written on
// Windows may use either separator, and the file has to be found on whichever
// platform is doing the resolving.
func normalize(path string) string {
	cleaned := filepath.Clean(filepath.FromSlash(path))
	// Windows accepts a forward slash everywhere, but a mixed path such as
	// lib\common.ps1 does not survive FromSlash on Unix, so backslashes are
	// folded there too. A backslash is a legal character in a Unix file name,
	// which is the one case where this could be wrong, and it is far less likely
	// than a script written with Windows separators.
	if filepath.Separator == '/' {
		cleaned = strings.ReplaceAll(cleaned, `\`, "/")
	}
	return cleaned
}

// appendUnique adds a value to a list unless it is already there, so that a
// script dot-sourcing the same unknown thing twice reports it once.
func appendUnique(list []string, value string) []string {
	for _, existing := range list {
		if existing == value {
			return list
		}
	}
	return append(list, value)
}
