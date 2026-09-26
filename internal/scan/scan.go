// Package scan walks a workspace root and reports the scripts in it.
//
// Inside a git repository the file list comes from git itself, so ignore rules
// are exact: .gitignore, .git/info/exclude and core.excludesFile are all
// honored because git is the one applying them. Outside a repository the
// scanner falls back to a directory walk with a conservative static ignore
// list, which is necessarily approximate.
//
// That asymmetry is deliberate. Getting it wrong in one direction hides a
// script the user is tracking, and getting it wrong in the other shows them a
// build artifact. So the fallback list is only ever applied where git has no
// opinion.
package scan

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lspoor/script-center/internal/detect"
	"github.com/lspoor/script-center/internal/git"
)

// Kind distinguishes something runnable in place from a directory that
// generates a set of tasks.
type Kind string

const (
	// KindScript is a single file executed with an interpreter.
	KindScript Kind = "script"
	// KindProject is a directory, such as a Cargo project, whose tasks are
	// enumerated from its manifest rather than run as one file.
	KindProject Kind = "project"
)

// Source records how a scan obtained its file list.
type Source string

const (
	// SourceGit means the list came from git, so ignore rules are exact.
	SourceGit Source = "git"
	// SourceWalk means the list came from a directory walk using the static
	// ignore list, so ignore rules are approximate.
	SourceWalk Source = "walk"
)

// Entry is one item in a scan result.
type Entry struct {
	// Root is the absolute workspace root the entry was found under.
	Root string `json:"root"`
	// Path is the absolute path of the script, or of the directory for a
	// project entry.
	Path string `json:"path"`
	// Rel is Path relative to Root, always slash-separated so it is stable
	// across platforms and safe to use as a map key.
	Rel string `json:"rel"`
	// Name is the final path element of Rel.
	Name string `json:"name"`
	// Dir is the slash-separated parent of Rel, empty for a root-level entry.
	Dir string `json:"dir"`

	Kind Kind            `json:"kind"`
	Lang detect.Language `json:"lang"`

	// Size and ModTime come from a single stat and together form the cache
	// key for anything expensive derived later, such as PowerShell help.
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
}

// Result is the outcome of scanning one root.
type Result struct {
	Root    string  `json:"root"`
	Entries []Entry `json:"entries"`
	Source  Source  `json:"source"`
	// IsRepo mirrors git.State.IsRepo, so callers need not shell out again.
	IsRepo bool `json:"isRepo"`
	// Warnings holds recoverable problems, such as directories that could not
	// be read. A scan that hits a permission error reports it and continues
	// rather than returning nothing at all.
	Warnings []string `json:"warnings,omitempty"`
}

// ByLang returns the entries matching a language, preserving order.
func (r Result) ByLang(lang detect.Language) []Entry {
	var out []Entry
	for _, e := range r.Entries {
		if e.Lang == lang {
			out = append(out, e)
		}
	}
	return out
}

// maxStatWorkers bounds concurrent stat calls. Scanning is almost entirely
// I/O bound, so a small pool helps on network shares without thrashing.
const maxStatWorkers = 16

// sniffBytes is how much of a candidate file is read for content detection.
const sniffBytes = 4096

// Scan enumerates the scripts and projects under root.
func Scan(ctx context.Context, root string) (Result, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return Result{}, fmt.Errorf("scan: resolve %s: %w", root, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return Result{}, fmt.Errorf("scan: %w", err)
	}
	if !info.IsDir() {
		return Result{}, fmt.Errorf("scan: %s is not a directory", abs)
	}

	if git.IsRepo(ctx, abs) {
		return scanWithGit(ctx, abs)
	}
	return scanWithWalk(ctx, abs)
}

// scanWithGit enumerates using git's own file list, so ignore rules are exact.
func scanWithGit(ctx context.Context, root string) (Result, error) {
	rels, err := git.TrackedFiles(ctx, root)
	if err != nil {
		return Result{}, err
	}

	entries, warnings := buildEntries(ctx, root, rels)
	return Result{
		Root:     root,
		Entries:  entries,
		Source:   SourceGit,
		IsRepo:   true,
		Warnings: warnings,
	}, nil
}

// ignoredDirs is the static skip list for directories that are not under git.
//
// Every entry is a directory that is almost always machine-generated and large
// enough to dominate a walk. The list is consulted only outside a git
// repository, where there is no better information available.
var ignoredDirs = map[string]bool{
	".git": true, ".hg": true, ".svn": true,
	"node_modules": true, "bower_components": true,
	"vendor":      true,
	"target":      true, // Cargo build output
	"__pycache__": true, ".mypy_cache": true, ".pytest_cache": true, ".ruff_cache": true,
	".venv": true, "venv": true, "env": true, ".tox": true, "site-packages": true,
	"dist": true, "build": true, "out": true,
	".next": true, ".nuxt": true, ".svelte-kit": true,
	".gradle": true, ".terraform": true,
	".idea": true, ".vscode": true,
	".cache": true, "Pods": true, "coverage": true,
}

// scanWithWalk enumerates by walking the tree, for directories that are not
// git repositories.
func scanWithWalk(ctx context.Context, root string) (Result, error) {
	var rels, warnings []string

	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			// A directory we cannot read is a warning, not a failure: the rest
			// of the tree is still worth scanning.
			warnings = append(warnings, err.Error())
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		if ctx.Err() != nil {
			return ctx.Err()
		}

		if d.IsDir() {
			if p == root {
				return nil
			}
			// Never follow a directory symlink, which risks a cycle.
			if d.Type()&fs.ModeSymlink != 0 {
				return fs.SkipDir
			}
			if ignoredDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}

		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return nil
		}
		rels = append(rels, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return Result{}, fmt.Errorf("scan: walk %s: %w", root, err)
	}

	entries, buildWarnings := buildEntries(ctx, root, rels)
	return Result{
		Root:     root,
		Entries:  entries,
		Source:   SourceWalk,
		Warnings: append(warnings, buildWarnings...),
	}, nil
}

// buildEntries turns root-relative paths into entries, stat-ing and detecting
// them concurrently. The returned warnings describe files that were interesting
// enough to inspect but could not be read fully.
func buildEntries(ctx context.Context, root string, rels []string) ([]Entry, []string) {
	type outcome struct {
		entry    Entry
		ok       bool
		warnings []string
	}

	jobs := make(chan string)
	out := make(chan outcome, maxStatWorkers)

	var wg sync.WaitGroup
	for range maxStatWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for rel := range jobs {
				e, warnings, ok := describe(ctx, root, rel)
				out <- outcome{entry: e, ok: ok, warnings: warnings}
			}
		}()
	}

	go func() {
		defer close(jobs)
		for _, rel := range rels {
			if ctx.Err() != nil {
				return
			}
			jobs <- rel
		}
	}()

	go func() {
		wg.Wait()
		close(out)
	}()

	var (
		entries  []Entry
		warnings []string
	)
	for r := range out {
		warnings = append(warnings, r.warnings...)
		if r.ok {
			entries = append(entries, r.entry)
		}
	}

	entries = dropProjectSources(entries)
	sortEntries(entries)
	sort.Strings(warnings)
	return entries, warnings
}

// dropProjectSources removes source files that a project entry already
// represents.
//
// Without this, a Cargo project appears twice: once as the project node the user
// can expand into tasks, and once per .rs file, each of which is not something
// that can be run on its own. A .rs file outside any project is still listed,
// because rust-script can execute it directly.
func dropProjectSources(entries []Entry) []Entry {
	var projectDirs []string
	for _, e := range entries {
		if e.Kind == KindProject {
			projectDirs = append(projectDirs, e.Rel)
		}
	}
	if len(projectDirs) == 0 {
		return entries
	}

	out := entries[:0]
	for _, e := range entries {
		if e.Kind == KindScript && e.Lang == detect.Rust && underAny(e.Dir, projectDirs) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// underAny reports whether dir is one of the given directories or nested inside
// one of them. A project rooted at the workspace root has an empty Rel, which
// covers every path.
func underAny(dir string, parents []string) bool {
	for _, p := range parents {
		switch {
		case p == "":
			return true
		case dir == p:
			return true
		case strings.HasPrefix(dir, p+"/"):
			return true
		}
	}
	return false
}

// describe decides what a single path is and, if it is interesting, gathers its
// metadata. It returns ok=false for paths the scanner should not list.
func describe(ctx context.Context, root, rel string) (Entry, []string, bool) {
	name := path.Base(rel)

	// A manifest stands for a set of generated tasks rather than a runnable
	// file, so it becomes a project entry.
	if name == "Cargo.toml" {
		return projectEntry(root, rel)
	}

	if !isCandidate(name) {
		return Entry{}, nil, false
	}

	full := filepath.Join(root, filepath.FromSlash(rel))
	info, err := os.Lstat(full)
	if err != nil {
		// A path git knows about may since have been deleted, or be
		// unreadable. Only report the latter.
		if !errors.Is(err, fs.ErrNotExist) {
			return Entry{}, []string{fmt.Sprintf("stat %s: %v", rel, err)}, false
		}
		return Entry{}, nil, false
	}
	// A symlink's own size and timestamp describe the link, not the script, so
	// metadata would be wrong and detection unreliable.
	if info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return Entry{}, nil, false
	}
	if ctx.Err() != nil {
		return Entry{}, nil, false
	}

	// A recognized extension is decisive, so avoid opening the file at all in
	// the common case. Only names that need a second opinion are read.
	lang, known := detect.FromExtension(name)
	var warnings []string
	if !known {
		content, readErr := readHead(ctx, full, sniffBytes)
		if readErr != nil {
			// An unreadable file still belongs in the tree, flagged by having
			// no detected language, so the user knows something is there.
			warnings = append(warnings, fmt.Sprintf("read %s: %v", rel, readErr))
		}
		lang = detect.Detect(name, content)
		if lang == detect.Unknown {
			// Not a script, or not one we can identify. Listing it would put
			// noise in the tree for no benefit.
			return Entry{}, warnings, false
		}
	}

	return Entry{
		Root:    root,
		Path:    full,
		Rel:     rel,
		Name:    name,
		Dir:     path.Dir(rel),
		Kind:    KindScript,
		Lang:    lang,
		Size:    info.Size(),
		ModTime: info.ModTime(),
	}, warnings, true
}

// projectEntry describes a manifest that stands for a set of generated tasks.
// The entry represents the manifest's directory rather than the file, so the
// tree shows a project node that the user can expand.
func projectEntry(root, rel string) (Entry, []string, bool) {
	dir := path.Dir(rel)
	switch dir {
	case ".", "/":
		// A manifest at the workspace root represents the root itself.
		dir = ""
	}

	return Entry{
		Root: root,
		Path: filepath.Join(root, filepath.FromSlash(dir)),
		Rel:  dir,
		Name: path.Base(filepath.ToSlash(filepath.Join(root, filepath.FromSlash(dir)))),
		Dir:  parentOf(dir),
		Kind: KindProject,
		Lang: detect.Rust,
	}, nil, true
}

// parentOf returns the parent of a slash-separated directory, normalising the
// root to an empty string.
func parentOf(dir string) string {
	if dir == "" {
		return ""
	}
	if parent := path.Dir(dir); parent != "." {
		return parent
	}
	return ""
}

// candidateExts are extensions that sometimes hold a script despite not looking
// like one. Files with these extensions, and files with no extension at all,
// have their leading bytes inspected. Every other extension is decided by name
// alone, because content-sniffing common data files like .json or .md produces
// false positives far more often than it finds real scripts.
var candidateExts = map[string]bool{
	".txt": true, ".text": true, ".script": true, ".command": true, ".run": true,
}

// isCandidate reports whether a file name is worth inspecting at all.
func isCandidate(name string) bool {
	if _, ok := detect.FromExtension(name); ok {
		return true
	}
	ext := strings.ToLower(path.Ext(name))
	return ext == "" || candidateExts[ext]
}

// readHead reads at most n bytes from the start of a file. A read that returns
// some bytes and an error is not treated as a failure, because the leading
// bytes are usually enough to identify a script.
func readHead(ctx context.Context, full string, n int) ([]byte, error) {
	f, err := os.Open(full)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	buf := make([]byte, n)
	read, err := f.Read(buf)
	if read == 0 && err != nil {
		return nil, err
	}
	return buf[:read], nil
}

// sortEntries orders results so the tree is stable between scans: scripts
// before projects, then by relative path, then by name.
func sortEntries(entries []Entry) {
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.Kind != b.Kind {
			return a.Kind == KindScript
		}
		if a.Rel != b.Rel {
			return a.Rel < b.Rel
		}
		return a.Name < b.Name
	})
}
