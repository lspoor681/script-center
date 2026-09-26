package params

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lspoor/script-center/internal/detect"
)

func TestReaderMixedLanguages(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}

	shell := write("rebuild.sh", "#!/bin/bash\nusage() {\n    cat <<'EOF'\nUsage: rebuild.sh [options]\n  -v, --verbose  chatty\nEOF\n}\nwhile [[ $# -gt 0 ]]; do\n    case \"$1\" in\n        -v|--verbose) shift ;;\n    esac\ndone\n")
	unknown := write("notes.txt", "just some notes\n")

	scripts := []Script{
		{Path: shell, Language: detect.Bash},
		{Path: unknown, Language: detect.Unknown},
	}

	reports, err := NewReader(Options{}).Read(context.Background(), scripts)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(reports) != 2 {
		t.Fatalf("got %d reports, want 2", len(reports))
	}
	// The caller's order is the order the user is looking at.
	if reports[0].Path != shell {
		t.Errorf("reports[0].Path = %q, want %q", reports[0].Path, shell)
	}
	if len(reports[0].Params) != 1 || reports[0].Params[0].Name != "verbose" {
		t.Errorf("shell params = %+v, want verbose", reports[0].Params)
	}
	// A language with no reader still produces a report that explains itself.
	if !reports[1].HasWarnings() {
		t.Error("an unreadable language should carry a warning")
	}
	if len(reports[1].Params) != 0 {
		t.Errorf("params = %+v, want none", reports[1].Params)
	}
}

func TestReaderUnsupportedLanguages(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		language detect.Language
		wantIn   string
		excluded string
	}{
		{detect.Rust, "cannot be read", "derive macro"},
		{detect.Batch, "cannot be read", "batch"},
	}
	for _, test := range tests {
		t.Run(string(test.language), func(t *testing.T) {
			path := filepath.Join(dir, "main."+string(test.language))
			if err := os.WriteFile(path, []byte("fn main() {}\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			scripts := []Script{{Path: path, Language: test.language}}
			reports, err := NewReader(Options{}).Read(context.Background(), scripts)
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			if len(reports) != 1 {
				t.Fatalf("got %d reports, want 1", len(reports))
			}
			report := reports[0]
			if report.Path != path {
				t.Errorf("Path = %q, want %q", report.Path, path)
			}
			// The script is still listed and still runnable. Only the form is
			// missing, and the user is told why.
			if len(report.Params) != 0 {
				t.Errorf("params = %+v, want none", report.Params)
			}
			joined := strings.Join(report.Warnings, " ")
			if !strings.Contains(joined, test.wantIn) {
				t.Errorf("warnings = %v, want them to explain the missing form", report.Warnings)
			}
			if !strings.Contains(joined, test.excluded) {
				t.Errorf("warnings = %v, want them to name %q", report.Warnings, test.excluded)
			}
			if report.Help.Source != HelpSourceNone {
				t.Errorf("Help.Source = %q, want %q", report.Help.Source, HelpSourceNone)
			}
		})
	}
}

func TestReaderMissingToolchainDoesNotAffectOtherLanguages(t *testing.T) {
	dir := t.TempDir()
	shell := filepath.Join(dir, "rebuild.sh")
	if err := os.WriteFile(shell, []byte(
		"#!/bin/bash\nwhile [[ $# -gt 0 ]]; do\n    case \"$1\" in\n"+
			"        -v|--verbose) shift ;;\n    esac\ndone\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A PowerShell script in a directory with no interpreter available. The
	// shell script beside it must still get its form.
	ps := filepath.Join(dir, "rebuild.ps1")
	if err := os.WriteFile(ps, []byte("param([string]$Server)\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Forcing the PowerShell lookup to fail keeps the test independent of
	// whether the machine running it has PowerShell installed.
	reader := &Reader{
		lookup: func(language detect.Language) (string, error) {
			if language == detect.PowerShell {
				return "", ErrNoPowerShell
			}
			return "", nil
		},
	}

	scripts := []Script{
		{Path: ps, Language: detect.PowerShell},
		{Path: shell, Language: detect.Bash},
	}
	reports, err := reader.Read(context.Background(), scripts)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(reports) != 2 {
		t.Fatalf("got %d reports, want 2", len(reports))
	}
	if !reports[0].HasWarnings() {
		t.Fatal("the script with no toolchain should carry a warning")
	}
	// The reason has to name the toolchain, or the user cannot tell whether to
	// install something or to fix their script.
	if !strings.Contains(strings.Join(reports[0].Warnings, " "), "PowerShell") {
		t.Errorf("warnings = %v, want them to name PowerShell", reports[0].Warnings)
	}
	// The shell script was still read in full, which is the point: one language
	// failing to find its toolchain must not cost another language its form.
	if reports[1].HasWarnings() {
		t.Errorf("the shell script should not be affected: %v", reports[1].Warnings)
	}
	if len(reports[1].Params) != 1 || reports[1].Params[0].Name != "verbose" {
		t.Errorf("shell params = %+v, want verbose", reports[1].Params)
	}
}

func TestReaderSkipsBlankPaths(t *testing.T) {
	scripts := []Script{{Path: ""}, {Path: "  "}}
	reports, err := NewReader(Options{}).Read(context.Background(), scripts)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(reports) != 0 {
		t.Errorf("got %d reports, want none", len(reports))
	}
}

func TestReaderNoScripts(t *testing.T) {
	reports, err := NewReader(Options{}).Read(context.Background(), nil)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(reports) != 0 {
		t.Errorf("got %d reports, want none", len(reports))
	}
}

func TestReaderAppliesOptions(t *testing.T) {
	// The reader has to be able to configure a harvester it did not construct,
	// which is what lets a caller set one timeout for the whole scan.
	ps := &PowerShellHarvester{}
	ps.apply(Options{Timeout: 5, BatchSize: 7})
	if ps.Timeout != 5 || ps.BatchSize != 7 {
		t.Errorf("PowerShell harvester = %v/%v, want 5/7", ps.Timeout, ps.BatchSize)
	}
	py := &PythonHarvester{}
	py.apply(Options{Timeout: 9, BatchSize: 3})
	if py.Timeout != 9 || py.BatchSize != 3 {
		t.Errorf("Python harvester = %v/%v, want 9/3", py.Timeout, py.BatchSize)
	}
	// Zero means leave the harvester's own default alone.
	untouched := &PythonHarvester{Timeout: 99, BatchSize: 11}
	untouched.apply(Options{})
	if untouched.Timeout != 99 || untouched.BatchSize != 11 {
		t.Errorf("a zero option overwrote a default: %v/%v", untouched.Timeout, untouched.BatchSize)
	}
}

func TestForLanguage(t *testing.T) {
	t.Run("bash needs no toolchain", func(t *testing.T) {
		extractor, err := ForLanguage(detect.Bash)
		if err != nil {
			t.Fatalf("ForLanguage: %v", err)
		}
		if _, ok := extractor.(bashExtractorAdapter); !ok {
			t.Errorf("got %T, want a bash adapter", extractor)
		}
	})

	t.Run("an unknown language still yields an extractor", func(t *testing.T) {
		// The dispatcher must never return nothing, because "no form" is
		// something the UI has to be able to show.
		extractor, err := ForLanguage(detect.Unknown)
		if err != nil {
			t.Fatalf("ForLanguage: %v", err)
		}
		if extractor == nil {
			t.Fatal("got nil")
		}
	})
}

func TestScriptString(t *testing.T) {
	if got := (Script{Path: "a.sh"}).String(); got != "a.sh" {
		t.Errorf("got %q, want a.sh", got)
	}
	if got := (Script{Path: "a.sh", Language: detect.Bash}).String(); !strings.Contains(got, "bash") {
		t.Errorf("got %q, want it to name the language", got)
	}
}

// The reader must serve an unchanged script from the cache without starting a
// toolchain. A fabricated entry for a path that does not exist on disk proves
// that: if anything tried to read the file, it would come back as a warning.
func TestReaderServesAnUnchangedScriptFromTheCache(t *testing.T) {
	dir := t.TempDir()
	path, size, modTime := writeScript(t, dir, "rebuild.sh", "#!/bin/bash\n", time.Unix(1700000000, 0))
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatal(err)
	}

	cache := NewCache("")
	cached := Report{Path: path, Help: Help{Synopsis: "from the cache", Source: HelpSourceComment}}
	cached.Params = append(cached.Params, Param{Name: "Cached", Kind: KindString})
	cache.Store(path, size, modTime, "bash", cached)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	reader := NewReader(Options{})
	reader.Cache = cache
	reports, err := reader.Read(context.Background(), []Script{{
		Path: path, Language: detect.Bash, Size: size, ModTime: modTime,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 1 {
		t.Fatalf("got %d reports, want 1", len(reports))
	}
	if reports[0].Help.Synopsis != "from the cache" {
		t.Errorf("synopsis = %q, want the cached one", reports[0].Help.Synopsis)
	}
}

func TestReaderIgnoresACacheEntryForAnEditedScript(t *testing.T) {
	// The cached report describes a file that no longer exists, and the size has
	// changed, so the reader has to read the file and find the truth.
	dir := t.TempDir()
	path, size, modTime := writeScript(t, dir, "rebuild.sh",
		"#!/bin/bash\ncase \"$1\" in\n  -v|--verbose) shift ;;\nesac\n", time.Unix(1700000000, 0))
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatal(err)
	}

	cache := NewCache("")
	cache.Store(path, size, modTime, "bash", Report{Path: path, Help: Help{Synopsis: "stale"}})
	editedSize := size + 1000

	reader := NewReader(Options{})
	reader.Cache = cache
	reports, err := reader.Read(context.Background(), []Script{{
		Path: path, Language: detect.Bash, Size: editedSize, ModTime: modTime,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if reports[0].Help.Synopsis == "stale" {
		t.Error("an edited script must not be answered from the cache")
	}
	if len(reports[0].Params) != 1 || reports[0].Params[0].Name != "verbose" {
		t.Errorf("params = %+v, want verbose read from the file", reports[0].Params)
	}
}

func TestReaderRemembersWhatItReads(t *testing.T) {
	// A first read stores the report, and a second read of the same unchanged
	// script is served from the cache with no further work.
	dir := t.TempDir()
	path, size, modTime := writeScript(t, dir, "rebuild.sh",
		"#!/bin/bash\ncase \"$1\" in\n  -v|--verbose) shift ;;\nesac\n", time.Unix(1700000000, 0))
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatal(err)
	}

	cache := NewCache("")
	reader := NewReader(Options{})
	reader.Cache = cache
	script := Script{Path: path, Language: detect.Bash, Size: size, ModTime: modTime}

	if _, err := reader.Read(context.Background(), []Script{script}); err != nil {
		t.Fatal(err)
	}
	if cache.Len() != 1 {
		t.Fatalf("cache holds %d reports after a read, want 1", cache.Len())
	}
	if _, ok := cache.Lookup(path, size, modTime, "bash"); !ok {
		t.Error("the read should have been stored against the file's fingerprint")
	}
}

func TestReaderWithEveryScriptCachedDoesNoWork(t *testing.T) {
	// A workspace where nothing changed should not start a toolchain at all, so
	// the reader has to return before building any group.
	cache := NewCache("")
	var scripts []Script
	for _, name := range []string{"a.sh", "b.sh", "c.py"} {
		path := filepath.Join(t.TempDir(), name)
		language := detect.Bash
		if name == "c.py" {
			language = detect.Python
		}
		scripts = append(scripts, Script{Path: path, Language: language, Size: 1, ModTime: time.Unix(1, 0)})
	}
	for _, script := range scripts {
		cache.Store(script.Path, script.Size, script.ModTime, string(script.Language), sampleReport(script.Path, "Force"))
	}

	reader := NewReader(Options{})
	reader.Cache = cache
	// A toolchain lookup that fails proves the cached path is taken, because
	// reaching a group would call it.
	reader.lookup = func(detect.Language) (string, error) {
		return "", errors.New("no toolchain should have been looked up")
	}

	reports, err := reader.Read(context.Background(), scripts)
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != len(scripts) {
		t.Fatalf("got %d reports, want %d", len(reports), len(scripts))
	}
	for i, report := range reports {
		if report.Path != scripts[i].Path {
			t.Errorf("report %d path = %q, want %q", i, report.Path, scripts[i].Path)
		}
	}
	if hits, _ := cache.Stats(); hits != len(scripts) {
		t.Errorf("hits = %d, want %d", hits, len(scripts))
	}
}

// An extractor existing and an extractor being able to describe a script are
// different questions. Rust has an extractor for exactly the second reason, so
// treating a successful lookup as readability would tell the user a Rust program
// has a parameter form.
func TestReadabilityDistinguishesHavingAnExtractorFromCanRead(t *testing.T) {
	cases := []struct {
		language detect.Language
		readable bool
	}{
		{detect.Bash, true},
		// Read in process, so it is readable wherever the file is.
		{detect.Rust, false},
		{detect.Batch, false},
	}
	for _, test := range cases {
		got := ReadabilityOf(test.language)
		if got.Readable != test.readable {
			t.Errorf("%s: Readable = %v, want %v", test.language, got.Readable, test.readable)
		}
		if !test.readable && strings.TrimSpace(got.Reason) == "" {
			t.Errorf("%s: an unreadable language must say why", test.language)
		}
		if test.readable && got.Reason != "" {
			t.Errorf("%s: a readable language should have no reason, got %q", test.language, got.Reason)
		}
	}
}

func TestEveryExtractorImplementsTheInterface(t *testing.T) {
	// A new language added to newExtractor without a Readability would fail to
	// compile here rather than at the point a user is waiting for a window.
	for _, language := range []detect.Language{
		detect.PowerShell, detect.Python, detect.Bash, detect.Rust, detect.Batch, detect.Unknown,
	} {
		extractor, err := ForLanguage(language)
		if err != nil {
			// A missing toolchain still has to answer the question, so this is
			// only a failure if the language genuinely needs one.
			continue
		}
		extractor.Readability()
	}
}
