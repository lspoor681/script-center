package deps

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeScript(t *testing.T, dir, rel string) string {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("Write-Host hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestResolveExpression(t *testing.T) {
	dir := t.TempDir()
	// A platform-specific expected path is built rather than hard coded, so the
	// test means the same thing on Linux and Windows.
	tests := []struct {
		name       string
		expression string
		// scriptDir names a directory under the temporary root to resolve
		// against, so that a parent-relative result still lands inside it.
		scriptDir  string
		wantSuffix string
		wantErr    bool
	}{
		{
			name:       "script root with forward slash",
			expression: "$PSScriptRoot/lib/common.ps1",
			wantSuffix: filepath.Join("lib", "common.ps1"),
		},
		{
			name:       "script root with backslash",
			expression: `$PSScriptRoot\lib\common.ps1`,
			wantSuffix: filepath.Join("lib", "common.ps1"),
		},
		{
			name:       "script root with a single quoted string",
			expression: "'$PSScriptRoot/lib/common.ps1'",
			wantSuffix: filepath.Join("lib", "common.ps1"),
		},
		{
			name:       "script root with a double quoted string",
			expression: `"$PSScriptRoot/lib/common.ps1"`,
			wantSuffix: filepath.Join("lib", "common.ps1"),
		},
		{
			name:       "dot slash relative",
			expression: "./sibling.ps1",
			wantSuffix: "sibling.ps1",
		},
		{
			name:       "parent relative",
			expression: "../shared/common.ps1",
			// The script sits one level down, so the result lands beside it
			// rather than above the temporary root.
			scriptDir:  "sub",
			wantSuffix: filepath.Join("shared", "common.ps1"),
		},
		{
			name:       "bare relative with a separator",
			expression: "lib/common.ps1",
			wantSuffix: filepath.Join("lib", "common.ps1"),
		},
		{
			name:       "surrounding whitespace",
			expression: "  ./sibling.ps1  ",
			wantSuffix: "sibling.ps1",
		},
		{
			name:       "script root alone",
			expression: "$PSScriptRoot",
			wantErr:    true,
		},
		{
			name:       "environment variable",
			expression: "$env:ProgramData/common.ps1",
			wantErr:    true,
		},
		{
			name:       "bare variable",
			expression: "$common",
			wantErr:    true,
		},
		{
			name:       "bare word",
			expression: "common",
			wantErr:    true,
		},
		{
			name:       "computed call",
			expression: "(Join-Path $x 'y')",
			wantErr:    true,
		},
		{
			name:       "empty",
			expression: "",
			wantErr:    true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			base := dir
			if test.scriptDir != "" {
				base = filepath.Join(dir, test.scriptDir)
			}
			got, err := ResolveExpression(test.expression, base)
			if test.wantErr {
				if err == nil {
					t.Fatalf("want an error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveExpression: %v", err)
			}
			if !strings.HasSuffix(got, test.wantSuffix) {
				t.Errorf("got %q, want it to end with %q", got, test.wantSuffix)
			}
			// A relative path is resolved against the script's directory, not
			// against wherever the user happens to be.
			if !filepath.IsAbs(got) {
				t.Errorf("got %q, want an absolute path", got)
			}
			if !strings.HasPrefix(got, dir) {
				t.Errorf("got %q, want it under %q", got, dir)
			}
		})
	}
}

func TestResolveOnceIncludesEntryAndHelper(t *testing.T) {
	dir := t.TempDir()
	entry := writeScript(t, dir, "rebuild.ps1")
	writeScript(t, dir, "lib/common.ps1")

	closure, err := ResolveOnce(entry, []string{"$PSScriptRoot/lib/common.ps1"})
	if err != nil {
		t.Fatalf("ResolveOnce: %v", err)
	}
	if len(closure.Files) != 2 {
		t.Fatalf("got %v, want the entry and one helper", closure.Files)
	}
	// The entry comes first so that a copy log reads in dependency order.
	if closure.Files[0] != entry {
		t.Errorf("Files[0] = %q, want the entry %q", closure.Files[0], entry)
	}
	if !strings.HasSuffix(closure.Files[1], filepath.Join("lib", "common.ps1")) {
		t.Errorf("Files[1] = %q", closure.Files[1])
	}
	if closure.HasUnresolved() {
		t.Errorf("Unresolved = %v, want none", closure.Unresolved)
	}
}

func TestResolveOnceReportsMissingHelper(t *testing.T) {
	dir := t.TempDir()
	entry := writeScript(t, dir, "rebuild.ps1")

	closure, err := ResolveOnce(entry, []string{"$PSScriptRoot/lib/absent.ps1"})
	if err != nil {
		t.Fatalf("ResolveOnce: %v", err)
	}
	// A missing helper is a warning rather than an error: the script is still
	// listed, and the user is told why it will not run elsewhere.
	if len(closure.Files) != 1 {
		t.Errorf("Files = %v, want only the entry", closure.Files)
	}
	if len(closure.Warnings) == 0 {
		t.Error("want a warning for a dot-source that does not exist")
	}
	if !strings.Contains(strings.Join(closure.Warnings, " "), "absent.ps1") {
		t.Errorf("Warnings = %v, want them to name the missing file", closure.Warnings)
	}
}

func TestResolveOnceReportsDirectory(t *testing.T) {
	dir := t.TempDir()
	entry := writeScript(t, dir, "rebuild.ps1")
	if err := os.MkdirAll(filepath.Join(dir, "lib"), 0o755); err != nil {
		t.Fatal(err)
	}

	closure, err := ResolveOnce(entry, []string{"$PSScriptRoot/lib"})
	if err != nil {
		t.Fatalf("ResolveOnce: %v", err)
	}
	if len(closure.Files) != 1 {
		t.Errorf("Files = %v, want only the entry", closure.Files)
	}
	if !strings.Contains(strings.Join(closure.Warnings, " "), "directory") {
		t.Errorf("Warnings = %v, want them to say it is a directory", closure.Warnings)
	}
}

func TestResolveOnceUnresolvedExpressions(t *testing.T) {
	dir := t.TempDir()
	entry := writeScript(t, dir, "rebuild.ps1")

	closure, err := ResolveOnce(entry, []string{
		"$shared",
		"$env:ProgramData/common.ps1",
		"$shared",
	})
	if err != nil {
		t.Fatalf("ResolveOnce: %v", err)
	}
	// The same unknown expression reported twice is listed once.
	if len(closure.Unresolved) != 2 {
		t.Errorf("Unresolved = %v, want two distinct entries", closure.Unresolved)
	}
	if !closure.HasUnresolved() {
		t.Error("HasUnresolved = false")
	}
}

func TestResolveFollowsNestedDotSources(t *testing.T) {
	dir := t.TempDir()
	entry := writeScript(t, dir, "rebuild.ps1")
	writeScript(t, dir, "lib/outer.ps1")
	writeScript(t, dir, "lib/inner.ps1")

	// A parser stand-in: outer pulls in inner, which pulls in nothing.
	find := func(path string) ([]string, error) {
		switch filepath.Base(path) {
		case "rebuild.ps1":
			return []string{"$PSScriptRoot/lib/outer.ps1"}, nil
		case "outer.ps1":
			// outer's own directory is lib/, which is the point: a nested
			// dot-source resolves against the helper, not the entry.
			return []string{"$PSScriptRoot/inner.ps1"}, nil
		default:
			return nil, nil
		}
	}

	resolver := Resolver{Find: find}
	closure, err := resolver.Resolve(entry, find2(find, entry))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(closure.Files) != 3 {
		t.Fatalf("Files = %v, want the entry and two nested helpers", closure.Files)
	}
	if closure.Files[0] != entry {
		t.Errorf("Files[0] = %q, want the entry", closure.Files[0])
	}
	if !strings.HasSuffix(closure.Files[1], "outer.ps1") {
		t.Errorf("Files[1] = %q, want the direct helper first", closure.Files[1])
	}
	if !strings.HasSuffix(closure.Files[2], "inner.ps1") {
		t.Errorf("Files[2] = %q, want the nested helper last", closure.Files[2])
	}
}

// find2 asks the stand-in parser about the entry, so the test does not restate
// the entry's own dot-sources.
func find2(find Find, entry string) []string {
	expressions, err := find(entry)
	if err != nil {
		return nil
	}
	return expressions
}

func TestResolveSurvivesCycle(t *testing.T) {
	dir := t.TempDir()
	entry := writeScript(t, dir, "a.ps1")
	writeScript(t, dir, "b.ps1")

	// a dot-sources b, b dot-sources a. Without a visited set this never ends.
	find := func(path string) ([]string, error) {
		switch filepath.Base(path) {
		case "a.ps1":
			return []string{"$PSScriptRoot/b.ps1"}, nil
		case "b.ps1":
			return []string{"$PSScriptRoot/a.ps1"}, nil
		default:
			return nil, nil
		}
	}

	closure, err := Resolver{Find: find}.Resolve(entry, find2(find, entry))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(closure.Files) != 2 {
		t.Errorf("Files = %v, want the entry and one helper", closure.Files)
	}
}

func TestResolveSharedHelperIncludedOnce(t *testing.T) {
	dir := t.TempDir()
	entry := writeScript(t, dir, "a.ps1")
	writeScript(t, dir, "b.ps1")
	writeScript(t, dir, "shared.ps1")

	// Both helpers pull in the same file, by two different routes.
	find := func(path string) ([]string, error) {
		switch filepath.Base(path) {
		case "a.ps1":
			return []string{"$PSScriptRoot/b.ps1", "$PSScriptRoot/shared.ps1"}, nil
		case "b.ps1":
			return []string{"$PSScriptRoot/../" + filepath.Base(dir) + "/shared.ps1"}, nil
		default:
			return nil, nil
		}
	}

	closure, err := Resolver{Find: find}.Resolve(entry, find2(find, entry))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	seen := 0
	for _, path := range closure.Files {
		if filepath.Base(path) == "shared.ps1" {
			seen++
		}
	}
	if seen != 1 {
		t.Errorf("shared helper appears %d times in %v, want once", seen, closure.Files)
	}
}

func TestResolveStopsAtDepthLimit(t *testing.T) {
	dir := t.TempDir()
	entry := writeScript(t, dir, "hop0.ps1")
	// Each hop dot-sources the next, well past any sane limit.
	for i := 1; i <= 6; i++ {
		writeScript(t, dir, "hop"+string(rune('0'+i))+".ps1")
	}
	find := func(path string) ([]string, error) {
		base := filepath.Base(path)
		if !strings.HasPrefix(base, "hop") {
			return nil, nil
		}
		index := base[len("hop")]
		if index == '5' {
			return nil, nil
		}
		return []string{"$PSScriptRoot/hop" + string(rune(index+1)) + ".ps1"}, nil
	}

	closure, err := Resolver{Find: find, MaxDepth: 2}.Resolve(entry, find2(find, entry))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(closure.Files) > 4 {
		t.Errorf("Files = %v, want the walk to stop at the depth limit", closure.Files)
	}
}

func TestResolveNoEntry(t *testing.T) {
	if _, err := ResolveOnce("", nil); !errors.Is(err, ErrNoEntry) {
		t.Errorf("error = %v, want ErrNoEntry", err)
	}
}

func TestResolveNoDotSources(t *testing.T) {
	dir := t.TempDir()
	entry := writeScript(t, dir, "standalone.ps1")

	closure, err := ResolveOnce(entry, nil)
	if err != nil {
		t.Fatalf("ResolveOnce: %v", err)
	}
	if len(closure.Files) != 1 || closure.Files[0] != entry {
		t.Errorf("Files = %v, want only the entry", closure.Files)
	}
	if len(closure.Warnings) != 0 || closure.HasUnresolved() {
		t.Errorf("unexpected problems: %v %v", closure.Warnings, closure.Unresolved)
	}
}

func TestResolveSkipsBlankExpressions(t *testing.T) {
	dir := t.TempDir()
	entry := writeScript(t, dir, "rebuild.ps1")

	closure, err := ResolveOnce(entry, []string{"", "   "})
	if err != nil {
		t.Fatalf("ResolveOnce: %v", err)
	}
	if len(closure.Files) != 1 {
		t.Errorf("Files = %v, want only the entry", closure.Files)
	}
	if len(closure.Warnings) != 0 {
		t.Errorf("Warnings = %v, want none for blank expressions", closure.Warnings)
	}
}

func TestResolveStableOrder(t *testing.T) {
	dir := t.TempDir()
	entry := writeScript(t, dir, "rebuild.ps1")
	writeScript(t, dir, "lib/a.ps1")
	writeScript(t, dir, "lib/b.ps1")
	writeScript(t, dir, "lib/c.ps1")

	expressions := []string{
		"$PSScriptRoot/lib/c.ps1",
		"$PSScriptRoot/lib/a.ps1",
		"$PSScriptRoot/lib/b.ps1",
	}
	first, err := ResolveOnce(entry, expressions)
	if err != nil {
		t.Fatalf("ResolveOnce: %v", err)
	}
	second, err := ResolveOnce(entry, expressions)
	if err != nil {
		t.Fatalf("ResolveOnce: %v", err)
	}
	// The same input must always produce the same order, or a copy step would
	// look nondeterministic in a log the user is reading to debug a failure.
	for i := range first.Files {
		if first.Files[i] != second.Files[i] {
			t.Fatalf("order is not stable:\n%v\n%v", first.Files, second.Files)
		}
	}
	if !strings.HasSuffix(first.Files[1], "a.ps1") {
		t.Errorf("Files[1] = %q, want the helpers sorted", first.Files[1])
	}
}

func TestAppendUnique(t *testing.T) {
	got := appendUnique(nil, "a")
	got = appendUnique(got, "a")
	got = appendUnique(got, "b")
	if len(got) != 2 {
		t.Errorf("got %v, want two entries", got)
	}
}
