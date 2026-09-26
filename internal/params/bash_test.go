package params

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func extractBash(t *testing.T, path string) Report {
	t.Helper()
	reports, err := BashExtractor{}.Extract([]string{path})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("got %d reports, want 1", len(reports))
	}
	return reports[0]
}

func TestBashExtractorCaseOptions(t *testing.T) {
	report := extractBash(t, testdataPath("full.sh"))

	// The usage line is the author's own summary and is worth showing.
	if report.Help.Synopsis != "Usage: rebuild.sh [options] <server>" {
		t.Errorf("synopsis = %q", report.Help.Synopsis)
	}
	if report.Help.Source != HelpSourceComment {
		t.Errorf("Help.Source = %q, want %q", report.Help.Source, HelpSourceComment)
	}

	// Declaration order is unknowable in a shell script, so it is sorted to keep
	// the form stable between runs.
	want := []string{"config", "dry-run", "help", "mode", "server", "verbose"}
	if len(report.Params) != len(want) {
		t.Fatalf("got %d params %+v, want %d", len(report.Params), report.Params, len(want))
	}
	for i, name := range want {
		if report.Params[i].Name != name {
			t.Errorf("params[%d] = %q, want %q", i, report.Params[i].Name, name)
		}
	}

	// An option is a flag or it takes a value, and getting that backwards would
	// send a filename as a flag.
	server := findNamedParam(report.Params, "server")
	if server.Kind != KindString {
		t.Errorf("server kind = %q, want %q", server.Kind, KindString)
	}
	if len(server.Aliases) != 1 || server.Aliases[0] != "s" {
		t.Errorf("server aliases = %v, want [s]", server.Aliases)
	}
	if server.Help != "the server to rebuild" {
		t.Errorf("server help = %q", server.Help)
	}

	verbose := findNamedParam(report.Params, "verbose")
	if verbose.Kind != KindBool {
		t.Errorf("verbose kind = %q, want %q", verbose.Kind, KindBool)
	}

	// --config=* and -c are the same option. A script may handle them in two
	// separate arms, and showing them as two inputs would be wrong.
	config := findNamedParam(report.Params, "config")
	if len(config.Aliases) != 1 || config.Aliases[0] != "c" {
		t.Errorf("config aliases = %v, want [c]", config.Aliases)
	}
	if config.Kind != KindString {
		t.Errorf("config kind = %q, want %q", config.Kind, KindString)
	}
}

func TestBashExtractorCaseOptionsWarnsAboutPositionals(t *testing.T) {
	report := extractBash(t, testdataPath("full.sh"))
	// The script does take a bare argument, so saying so is better than a form
	// that would send the wrong thing.
	joined := strings.Join(report.Warnings, " ")
	if !strings.Contains(joined, "positional") {
		t.Errorf("warnings = %v, want one about positional arguments", report.Warnings)
	}
	// An option's own value is read as $2 inside the arm that handles it, and
	// counting that would report a positional the script does not have.
	if strings.Contains(joined, "2 positional") {
		t.Errorf("warnings = %v, want the option value not counted", report.Warnings)
	}
}

func TestBashExtractorGetopts(t *testing.T) {
	report := extractBash(t, testdataPath("getopts.sh"))

	// A colon belongs to the letter before it. Reading it as belonging to the
	// letter after would report every later option as taking a value.
	want := map[string]Kind{
		"s": KindString, // s:
		"m": KindString, // m:
		"v": KindBool,   // v, followed by h
		"h": KindBool,   // h, at the end
	}
	if len(report.Params) != len(want) {
		t.Fatalf("got %d params %+v, want %d", len(report.Params), report.Params, len(want))
	}
	for name, kind := range want {
		param := findNamedParam(report.Params, name)
		if param.Name != name {
			t.Errorf("no param named %q", name)
			continue
		}
		if param.Kind != kind {
			t.Errorf("%s kind = %q, want %q", name, param.Kind, kind)
		}
	}
	// A leading colon only changes how getopts reports errors.
	if findNamedParam(report.Params, ":").Name != "" {
		t.Error("the error-reporting colon should not be an option")
	}
}

func TestBashExtractorNoOptions(t *testing.T) {
	report := extractBash(t, testdataPath("nooptions.sh"))
	if len(report.Params) != 0 {
		t.Errorf("params = %+v, want none", report.Params)
	}
	// The script is still listed and still runnable, with the reason attached.
	joined := strings.Join(report.Warnings, " ")
	if !strings.Contains(joined, "own") {
		t.Errorf("warnings = %v, want an explanation", report.Warnings)
	}
}

func TestBashExtractorMissingFile(t *testing.T) {
	missing := filepath.Join("testdata", "does-not-exist.sh")
	report := extractBash(t, missing)
	if report.Path != missing {
		t.Errorf("Path = %q, want %q", report.Path, missing)
	}
	if !report.HasWarnings() {
		t.Error("a missing file should carry a warning")
	}
}

func TestBashExtractorNoPaths(t *testing.T) {
	reports, err := BashExtractor{}.Extract(nil)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(reports) != 0 {
		t.Errorf("got %d reports, want none", len(reports))
	}
}

func TestBashExtractorIgnoresCommentsAndStrings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tricky.sh")
	source := `#!/bin/bash
# A comment mentioning --not-an-option and a usage line that is not real.
echo "the word getopts ':abc' must not be read here"
usage() {
    cat <<'EOF'
Usage: tricky.sh [options]
  -q, --quiet  say nothing
EOF
}
while [[ $# -gt 0 ]]; do
    case "$1" in
        -q|--quiet) quiet=1; shift ;;
    esac
done
`
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	report := extractBash(t, path)

	if len(report.Params) != 1 || report.Params[0].Name != "quiet" {
		t.Fatalf("params = %+v, want only quiet", report.Params)
	}
	// A getopts call inside a string is not a call.
	for _, param := range report.Params {
		if param.Name == "a" || param.Name == "b" || param.Name == "c" {
			t.Errorf("got %q from inside a string", param.Name)
		}
	}
	// The comment's fake usage line must not become the synopsis.
	if report.Help.Synopsis != "Usage: tricky.sh [options]" {
		t.Errorf("synopsis = %q", report.Help.Synopsis)
	}
}

func TestBashExtractorIgnoresNonOptionCase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subcommands.sh")
	// A case over a subcommand name is not a set of switches, and reading it as
	// one would invent options the script does not have.
	source := `#!/bin/bash
case "$1" in
    deploy)  deploy_it ;;
    status)  show_status ;;
    *)       echo "unknown" ;;
esac
`
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	report := extractBash(t, path)
	if len(report.Params) != 0 {
		t.Errorf("params = %+v, want none", report.Params)
	}
}

func TestBashExtractorUnterminatedHeredoc(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "truncated.sh")
	// A file that ends inside a here-document is not what it appears to be, so
	// nothing after the opening marker is read.
	source := "cat <<'EOF'\nUsage: truncated.sh\n  -x  do a thing\n"
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	report := extractBash(t, path)
	if report.Help.Synopsis != "" {
		t.Errorf("synopsis = %q, want none from an unterminated here-document", report.Help.Synopsis)
	}
}

func TestBashExtractorLineContinuation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "continued.sh")
	// An option list split across lines with a backslash is one case arm, and
	// splitting it would lose the body that says the option takes a value.
	source := `#!/bin/bash
case "$1" in
    -o|--output) \
        out="$2"; \
        shift ;;
esac
`
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	report := extractBash(t, path)
	output := findNamedParam(report.Params, "output")
	if output.Name != "output" {
		t.Fatalf("params = %+v, want output", report.Params)
	}
	if output.Kind != KindString {
		t.Errorf("output kind = %q, want %q", output.Kind, KindString)
	}
}

func TestBashExtractorNotes(t *testing.T) {
	report := extractBash(t, testdataPath("full.sh"))
	// Prose in the usage block that is neither the summary nor an option line is
	// still worth showing the user.
	if !strings.Contains(report.Help.Notes, "first argument") {
		t.Errorf("notes = %q", report.Help.Notes)
	}
}
