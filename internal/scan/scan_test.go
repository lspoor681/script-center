package scan

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"

	"github.com/lspoor/script-center/internal/detect"
)

// newRepo builds a small git repository containing a spread of file types.
func newRepo(t *testing.T) string {
	t.Helper()
	requireGit(t)

	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test User")
	runGit(t, dir, "config", "commit.gpgsign", "false")

	files := map[string]string{
		"deploy.ps1":                "[CmdletBinding()]\nparam([string]$Server)\nWrite-Host $Server\n",
		"scripts/backup.sh":         "#!/bin/bash\nset -e\necho backup\n",
		"scripts/legacy.cmd":        "@echo off\necho legacy\n",
		"tools/cleanup.py":          "import os\nprint('clean')\n",
		"README.md":                 "# fixture\n",
		"data.json":                 "{\n  \"a\": 1\n}\n",
		".gitignore":                "ignored/\n*.log\n",
		"ignored/secret.sh":         "#!/bin/bash\necho should not appear\n",
		"changelog.log":             "noise\n",
		"no-extension":              "#!/usr/bin/env python3\nprint('hi')\n",
		"docs/notes.txt":            "just prose, not a script\n",
		"nested/deep/thing.sh":      "#!/bin/sh\necho deep\n",
		"weird name with spaces.sh": "#!/bin/bash\necho spaces\n",
	}
	for name, body := range files {
		write(t, dir, name, body)
	}

	// A Cargo project, which is a project entry rather than a script.
	write(t, dir, "rust-tool/Cargo.toml", "[package]\nname = \"tool\"\n")
	write(t, dir, "rust-tool/src/main.rs", "fn main() {}\n")

	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "fixture")
	return dir
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_TERMINAL_PROMPT=0",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// rels extracts the relative paths of a result for readable assertions.
func rels(r Result) map[string]Entry {
	out := make(map[string]Entry, len(r.Entries))
	for _, e := range r.Entries {
		out[e.Rel] = e
	}
	return out
}

func TestScanGitRepo(t *testing.T) {
	dir := newRepo(t)

	got, err := Scan(context.Background(), dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	if got.Source != SourceGit || !got.IsRepo {
		t.Errorf("Source = %q IsRepo = %v, want git/true", got.Source, got.IsRepo)
	}

	found := rels(got)

	for _, want := range []struct {
		rel  string
		lang detect.Language
		kind Kind
	}{
		{"deploy.ps1", detect.PowerShell, KindScript},
		{"scripts/backup.sh", detect.Bash, KindScript},
		{"scripts/legacy.cmd", detect.Batch, KindScript},
		{"tools/cleanup.py", detect.Python, KindScript},
		{"nested/deep/thing.sh", detect.Bash, KindScript},
		{"no-extension", detect.Python, KindScript},
		{"weird name with spaces.sh", detect.Bash, KindScript},
		{"rust-tool", detect.Rust, KindProject},
	} {
		e, ok := found[want.rel]
		if !ok {
			t.Errorf("missing entry %q; got %v", want.rel, keys(found))
			continue
		}
		if e.Lang != want.lang {
			t.Errorf("%s Lang = %q, want %q", want.rel, e.Lang, want.lang)
		}
		if e.Kind != want.kind {
			t.Errorf("%s Kind = %q, want %q", want.rel, e.Kind, want.kind)
		}
	}
}

// TestScanExcludesIgnoredAndNonScripts pins the two exclusions that matter:
// anything git ignores, and files that are not scripts.
func TestScanExcludesIgnoredAndNonScripts(t *testing.T) {
	dir := newRepo(t)

	got, err := Scan(context.Background(), dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	found := rels(got)

	for _, unwanted := range []string{
		"ignored/secret.sh",    // matches .gitignore
		"changelog.log",        // matches *.log
		"README.md",            // not a script
		"data.json",            // not a script
		"docs/notes.txt",       // a .txt file, but with no script in it
		".gitignore",           // not a script
		"rust-tool/Cargo.toml", // a manifest, superseded by the project entry
		"rust-tool/src/main.rs",
	} {
		if _, present := found[unwanted]; present {
			t.Errorf("entry %q should not be listed", unwanted)
		}
	}
}

// TestScanHonoursTrackedFilesInIgnoredDirs is the reason the git path exists. A
// script that is force-added despite a broad ignore rule is still the user's
// script and must appear, which a static ignore list could not achieve.
func TestScanHonoursTrackedFilesInIgnoredDirs(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "build/forced.sh", "#!/bin/bash\necho forced\n")
	runGit(t, dir, "add", "-f", "build/forced.sh")
	runGit(t, dir, "commit", "-m", "force add")

	got, err := Scan(context.Background(), dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if _, present := rels(got)["build/forced.sh"]; !present {
		t.Error("a tracked script under an ignored directory was not listed")
	}
}

func TestScanNonGitDirectory(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "run.sh", "#!/bin/bash\necho hi\n")
	write(t, dir, "sub/run.ps1", "Write-Host hi\n")
	write(t, dir, "node_modules/dep/index.js", "module.exports = {}\n")
	write(t, dir, "target/debug/build.rs", "fn main() {}\n")

	got, err := Scan(context.Background(), dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if got.Source != SourceWalk || got.IsRepo {
		t.Errorf("Source = %q IsRepo = %v, want walk/false", got.Source, got.IsRepo)
	}

	found := rels(got)
	if _, ok := found["run.sh"]; !ok {
		t.Error("run.sh missing")
	}
	if _, ok := found["sub/run.ps1"]; !ok {
		t.Error("sub/run.ps1 missing")
	}
	for _, skipped := range []string{"node_modules/dep/index.js", "target/debug/build.rs"} {
		if _, present := found[skipped]; present {
			t.Errorf("%q should be skipped by the static ignore list", skipped)
		}
	}
}

// TestScanSkipsSymlinks guards against cycles and against reporting metadata
// that describes the link rather than the script.
func TestScanSkipsSymlinks(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "real.sh", "#!/bin/bash\necho hi\n")
	if err := os.Symlink(filepath.Join(dir, "real.sh"), filepath.Join(dir, "link.sh")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	got, err := Scan(context.Background(), dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if _, present := rels(got)["link.sh"]; present {
		t.Error("symlink was listed")
	}
	if _, present := rels(got)["real.sh"]; !present {
		t.Error("the real file should still be listed")
	}
}

func TestScanEntryMetadata(t *testing.T) {
	dir := newRepo(t)
	got, err := Scan(context.Background(), dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	e, present := rels(got)["scripts/backup.sh"]
	if !present {
		t.Fatal("scripts/backup.sh missing")
	}
	if e.Dir != "scripts" {
		t.Errorf("Dir = %q, want %q", e.Dir, "scripts")
	}
	if e.Name != "backup.sh" {
		t.Errorf("Name = %q, want %q", e.Name, "backup.sh")
	}
	if e.Size <= 0 {
		t.Error("Size is zero")
	}
	if e.ModTime.IsZero() {
		t.Error("ModTime is zero")
	}
	if !filepath.IsAbs(e.Path) {
		t.Errorf("Path = %q, want an absolute path", e.Path)
	}
}

func TestScanProjectEntryPointsAtDirectory(t *testing.T) {
	dir := newRepo(t)
	got, err := Scan(context.Background(), dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	e, present := rels(got)["rust-tool"]
	if !present {
		t.Fatal("rust-tool project entry missing")
	}
	if e.Kind != KindProject {
		t.Errorf("Kind = %q, want %q", e.Kind, KindProject)
	}
	if info, err := os.Stat(e.Path); err != nil || !info.IsDir() {
		t.Errorf("project Path = %q, want the project directory", e.Path)
	}
}

func TestScanRootLevelProject(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "Cargo.toml", "[package]\nname = \"x\"\n")

	got, err := Scan(context.Background(), dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	e, present := rels(got)[""]
	if !present {
		t.Fatalf("expected a root-level project entry; got %v", keys(rels(got)))
	}
	if e.Dir != "" {
		t.Errorf("Dir = %q, want empty for a root-level project", e.Dir)
	}
}

func TestScanStableOrder(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()

	first, err := Scan(ctx, dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	// Scan repeatedly: the worker pool makes ordering non-deterministic unless
	// it is sorted, and the tree must not reshuffle between refreshes.
	for range 5 {
		next, err := Scan(ctx, dir)
		if err != nil {
			t.Fatalf("Scan: %v", err)
		}
		if len(next.Entries) != len(first.Entries) {
			t.Fatalf("entry count changed: %d then %d", len(first.Entries), len(next.Entries))
		}
		for i := range next.Entries {
			if next.Entries[i].Rel != first.Entries[i].Rel {
				t.Fatalf("order changed at %d: %q then %q",
					i, first.Entries[i].Rel, next.Entries[i].Rel)
			}
		}
	}
}

func TestScanMissingDirectory(t *testing.T) {
	if _, err := Scan(context.Background(), filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("Scan on a missing directory returned no error")
	}
}

func TestScanNotADirectory(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a-file")
	write(t, dir, "a-file", "not a dir\n")

	if _, err := Scan(context.Background(), file); err == nil {
		t.Error("Scan on a file returned no error")
	}
}

func TestScanHonoursContextCancellation(t *testing.T) {
	dir := newRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := Scan(ctx, dir); err != nil && !errors.Is(err, context.Canceled) {
		t.Errorf("Scan with a canceled context: %v", err)
	}
}

func TestByLang(t *testing.T) {
	dir := newRepo(t)
	got, err := Scan(context.Background(), dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	bash := got.ByLang(detect.Bash)
	if len(bash) < 2 {
		t.Errorf("ByLang(Bash) returned %d entries, want at least 2", len(bash))
	}
	for _, e := range bash {
		if e.Lang != detect.Bash {
			t.Errorf("ByLang(Bash) returned %q", e.Rel)
		}
	}
}

func TestIsCandidate(t *testing.T) {
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"a.ps1", true},
		{"a.sh", true},
		{"a.py", true},
		{"noext", true},
		{"notes.txt", true},
		{"run.command", true},
		{"README.md", false},
		{"data.json", false},
		{"page.html", false},
		{"styles.css", false},
	} {
		if got := isCandidate(tc.name); got != tc.want {
			t.Errorf("isCandidate(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestDropProjectSources pins the rule that a Cargo project appears once, as a
// project, rather than as a project plus every source file inside it.
func TestDropProjectSources(t *testing.T) {
	entries := []Entry{
		{Kind: KindProject, Rel: "tool", Lang: detect.Rust},
		{Kind: KindScript, Rel: "tool/src/main.rs", Dir: "tool/src", Lang: detect.Rust},
		{Kind: KindScript, Rel: "tool/tests/it.rs", Dir: "tool/tests", Lang: detect.Rust},
		// Outside the project, so still listed: rust-script can run it.
		{Kind: KindScript, Rel: "standalone.rs", Dir: "", Lang: detect.Rust},
		// A prefix match must be on a path boundary, so "tooling" is not
		// inside "tool".
		{Kind: KindScript, Rel: "tooling/other.rs", Dir: "tooling", Lang: detect.Rust},
		// A non-Rust script under a project directory is unaffected.
		{Kind: KindScript, Rel: "tool/build.rs", Dir: "tool", Lang: detect.Bash},
	}

	got := rels(Result{Entries: dropProjectSources(entries)})

	for _, dropped := range []string{"tool/src/main.rs", "tool/tests/it.rs"} {
		if _, present := got[dropped]; present {
			t.Errorf("%q should be covered by the project entry", dropped)
		}
	}
	for _, kept := range []string{"standalone.rs", "tooling/other.rs", "tool/build.rs"} {
		if _, present := got[kept]; !present {
			t.Errorf("%q should have been kept", kept)
		}
	}
}

// TestDropProjectSourcesRootProject covers a manifest at the workspace root,
// whose empty Rel means it covers the whole tree.
func TestDropProjectSourcesRootProject(t *testing.T) {
	entries := []Entry{
		{Kind: KindProject, Rel: "", Lang: detect.Rust},
		{Kind: KindScript, Rel: "src/main.rs", Dir: "src", Lang: detect.Rust},
		{Kind: KindScript, Rel: "deep/nested/lib.rs", Dir: "deep/nested", Lang: detect.Rust},
		{Kind: KindScript, Rel: "run.sh", Dir: "", Lang: detect.Bash},
	}

	got := rels(Result{Entries: dropProjectSources(entries)})

	if _, present := got["src/main.rs"]; present {
		t.Error("src/main.rs should be covered by a root-level project")
	}
	if _, present := got["deep/nested/lib.rs"]; present {
		t.Error("deep/nested/lib.rs should be covered by a root-level project")
	}
	if _, present := got["run.sh"]; !present {
		t.Error("run.sh should be kept")
	}
}

// TestScanProjectSuppressesItsSources is the end-to-end version of the two
// tests above, driven through a real repository.
func TestScanProjectSuppressesItsSources(t *testing.T) {
	dir := newRepo(t)
	got, err := Scan(context.Background(), dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	found := rels(got)

	if _, present := found["rust-tool"]; !present {
		t.Error("the project entry is missing")
	}
	if _, present := found["rust-tool/src/main.rs"]; present {
		t.Error("a source file inside the project was listed separately")
	}
}

func TestDropProjectSourcesWithoutProjects(t *testing.T) {
	entries := []Entry{
		{Kind: KindScript, Rel: "a.rs", Dir: "", Lang: detect.Rust},
	}
	if got := dropProjectSources(entries); len(got) != 1 {
		t.Errorf("dropProjectSources removed entries with no projects present")
	}
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
