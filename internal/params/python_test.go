package params

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// findPythonHarvester returns a harvester, skipping when the interpreter is
// absent. The tests that use it are about reading metadata, not about the
// interpreter, so a machine without Python should not fail them.
func findPythonHarvester(t *testing.T) *PythonHarvester {
	t.Helper()
	exe, err := FindPython()
	if err != nil {
		t.Skipf("no Python interpreter: %v", err)
	}
	return &PythonHarvester{Exe: exe, Timeout: 90 * time.Second}
}

func testdataPath(name string) string { return filepath.Join("testdata", name) }

func TestFindPython(t *testing.T) {
	exe, err := FindPython()
	if err != nil {
		// Not a failure: the machine may simply have no Python, which is why
		// every consumer of this treats it as a skip.
		if !errors.Is(err, ErrNoPython) {
			t.Errorf("error = %v, want it to wrap ErrNoPython", err)
		}
		return
	}
	if !strings.Contains(filepath.Base(exe), "python") && !strings.Contains(filepath.Base(exe), "py") {
		t.Errorf("FindPython = %q, which does not look like an interpreter", exe)
	}
}

func TestPythonHarvesterFull(t *testing.T) {
	harvester := findPythonHarvester(t)
	path := testdataPath("full.py")

	reports, err := harvester.Harvest(context.Background(), []string{path})
	if err != nil {
		t.Fatalf("Harvest: %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("got %d reports, want 1", len(reports))
	}
	report := reports[0]
	if report.Path != path {
		t.Errorf("Path = %q, want %q", report.Path, path)
	}

	// The module docstring is the author's own words, so it is preferred over
	// the parser's own description.
	if report.Help.Synopsis != "Rebuilds the search index on a server." {
		t.Errorf("synopsis = %q", report.Help.Synopsis)
	}
	if report.Help.Source != HelpSourceDocComment {
		t.Errorf("Help.Source = %q, want %q", report.Help.Source, HelpSourceDocComment)
	}
	if !strings.Contains(report.Help.Description, "rebuild_one") {
		t.Errorf("description = %q", report.Help.Description)
	}
	if report.Help.Notes != "See the runbook for key rotation." {
		t.Errorf("notes = %q", report.Help.Notes)
	}

	// A version gate in the script's own code is a requirement the user should
	// be told about before the run rather than from a traceback.
	if report.Requirements.PythonVersion != "3.9" {
		t.Errorf("PythonVersion = %q, want 3.9", report.Requirements.PythonVersion)
	}

	target := findNamedParam(report.Params, "target")
	if target.Kind != KindString {
		t.Errorf("target kind = %q, want %q", target.Kind, KindString)
	}
	// argparse treats a positional as required without saying so.
	if !target.Required {
		t.Error("target should be required")
	}
	if target.Position != 1 {
		t.Errorf("target position = %d, want 1", target.Position)
	}
	if target.Help != "The server to rebuild." {
		t.Errorf("target help = %q", target.Help)
	}

	// Both positional arguments are numbered, in declaration order, because that
	// is the order a caller has to supply them in.
	output := findNamedParam(report.Params, "output")
	if !output.Required || output.Position != 2 {
		t.Errorf("output = %+v, want required at position 2", output)
	}

	threads := findNamedParam(report.Params, "threads")
	if threads.Kind != KindInt {
		t.Errorf("threads kind = %q, want %q", threads.Kind, KindInt)
	}
	if threads.Default == nil || threads.Default.HelpString() != "4" {
		t.Errorf("threads default = %v, want 4", threads.Default)
	}

	ratio := findNamedParam(report.Params, "ratio")
	if ratio.Kind != KindFloat {
		t.Errorf("ratio kind = %q, want %q", ratio.Kind, KindFloat)
	}

	// A closed set is worth more than the declared type, because the form can
	// offer the values instead of asking the user to know them.
	mode := findNamedParam(report.Params, "mode")
	if mode.Kind != KindEnum {
		t.Errorf("mode kind = %q, want %q", mode.Kind, KindEnum)
	}
	if len(mode.Constraints) != 1 || mode.Constraints[0].Kind != ConstraintSet {
		t.Fatalf("mode constraints = %+v, want one set", mode.Constraints)
	}
	if got := mode.Constraints[0].Values; len(got) != 2 || got[0] != "fast" {
		t.Errorf("mode values = %v", got)
	}

	verbose := findNamedParam(report.Params, "verbose")
	if verbose.Kind != KindBool {
		t.Errorf("verbose kind = %q, want %q", verbose.Kind, KindBool)
	}

	config := findNamedParam(report.Params, "config")
	if config.Kind != KindPath {
		t.Errorf("config kind = %q, want %q", config.Kind, KindPath)
	}
	if config.TypeName != "pathlib.Path" {
		t.Errorf("config typeName = %q", config.TypeName)
	}

	tag := findNamedParam(report.Params, "tag")
	if tag.Kind != KindArray {
		t.Errorf("tag kind = %q, want %q", tag.Kind, KindArray)
	}

	// The short form is the alias the user is most likely to type, so dropping
	// it would lose something they actually use.
	server := findNamedParam(report.Params, "server")
	if len(server.Aliases) != 1 || server.Aliases[0] != "s" {
		t.Errorf("server aliases = %v, want [s]", server.Aliases)
	}
	if len(verbose.Aliases) != 1 || verbose.Aliases[0] != "v" {
		t.Errorf("verbose aliases = %v, want [v]", verbose.Aliases)
	}

	name := findNamedParam(report.Params, "name")
	if !name.Required {
		t.Error("name should be required")
	}

	// Declaration order is what a human reads and what the form shows.
	if len(report.Params) != 10 {
		t.Errorf("got %d parameters, want 10: %+v", len(report.Params), report.Params)
	}
}

func TestPythonHarvesterNoHelp(t *testing.T) {
	harvester := findPythonHarvester(t)
	path := testdataPath("nohelp.py")

	reports, err := harvester.Harvest(context.Background(), []string{path})
	if err != nil {
		t.Fatalf("Harvest: %v", err)
	}
	report := reports[0]
	if len(report.Params) != 1 {
		t.Fatalf("params = %+v, want one", report.Params)
	}
	if report.Params[0].Name != "name" {
		t.Errorf("name = %q", report.Params[0].Name)
	}
	// Nothing was written about the script, so no source may be claimed.
	if report.Help.Synopsis != "" {
		t.Errorf("synopsis = %q, want empty", report.Help.Synopsis)
	}
}

func TestPythonHarvesterBrokenFile(t *testing.T) {
	harvester := findPythonHarvester(t)
	path := testdataPath("broken.py")

	reports, err := harvester.Harvest(context.Background(), []string{path})
	if err != nil {
		t.Fatalf("Harvest: %v", err)
	}
	report := reports[0]
	// A file that does not parse is still listed and still runnable, with the
	// reason attached. Failing the batch would hide every other script.
	if report.Path != path {
		t.Errorf("Path = %q, want %q", report.Path, path)
	}
	if !report.HasWarnings() {
		t.Error("a file that does not parse should carry a warning")
	}
	if len(report.Params) != 0 {
		t.Errorf("params = %+v, want none", report.Params)
	}
}

func TestPythonHarvesterComputedArguments(t *testing.T) {
	harvester := findPythonHarvester(t)
	path := testdataPath("computed.py")

	reports, err := harvester.Harvest(context.Background(), []string{path})
	if err != nil {
		t.Fatalf("Harvest: %v", err)
	}
	report := reports[0]

	// The argument that was written as a literal is still found, so one unknown
	// definition does not cost the user the rest of the form.
	port := findNamedParam(report.Params, "port")
	if port.Kind != KindInt || port.Help != "A known port." {
		t.Errorf("port = %+v", port)
	}

	// The arguments that only exist at run time are reported rather than
	// guessed at, because a wrong form control is a silent failure.
	joined := strings.Join(report.Warnings, " ")
	if !strings.Contains(joined, "run time") {
		t.Errorf("warnings = %v, want one about arguments built at run time", report.Warnings)
	}
	if !strings.Contains(joined, "subcommand") {
		t.Errorf("warnings = %v, want one about subcommands", report.Warnings)
	}
	if strings.Contains(strings.ToLower(joined), "error") {
		t.Errorf("warnings = %v, want them to be explanations rather than errors", report.Warnings)
	}
}

func TestPythonHarvesterMissingFile(t *testing.T) {
	harvester := findPythonHarvester(t)
	missing := testdataPath("does-not-exist.py")

	reports, err := harvester.Harvest(context.Background(), []string{missing})
	if err != nil {
		t.Fatalf("Harvest: %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("got %d reports, want 1", len(reports))
	}
	// A file that is not there still gets a row, so the caller's list of
	// scripts and the list of forms never disagree.
	if reports[0].Path != missing {
		t.Errorf("Path = %q, want %q", reports[0].Path, missing)
	}
	if !reports[0].HasWarnings() {
		t.Error("a missing file should carry a warning")
	}
}

func TestPythonHarvesterBatches(t *testing.T) {
	harvester := findPythonHarvester(t)
	// A batch size of one forces several interpreter invocations, which is the
	// path where records could be lost or reordered between batches.
	harvester.BatchSize = 1

	paths := []string{
		testdataPath("full.py"),
		testdataPath("nohelp.py"),
		testdataPath("broken.py"),
	}
	reports, err := harvester.Harvest(context.Background(), paths)
	if err != nil {
		t.Fatalf("Harvest: %v", err)
	}
	if len(reports) != len(paths) {
		t.Fatalf("got %d reports, want %d", len(reports), len(paths))
	}
	// Order and identity have to survive batching, or a form would be shown for
	// the wrong script.
	for i, path := range paths {
		if reports[i].Path != path {
			t.Errorf("reports[%d].Path = %q, want %q", i, reports[i].Path, path)
		}
	}
	if len(reports[0].Params) != 10 {
		t.Errorf("full.py has %d params, want 10", len(reports[0].Params))
	}
	if len(reports[1].Params) != 1 {
		t.Errorf("nohelp.py has %d params, want 1", len(reports[1].Params))
	}
}

func TestPythonHarvesterNoPaths(t *testing.T) {
	harvester := findPythonHarvester(t)
	reports, err := harvester.Harvest(context.Background(), nil)
	if err != nil {
		t.Fatalf("Harvest: %v", err)
	}
	if len(reports) != 0 {
		t.Errorf("got %d reports, want none", len(reports))
	}
}

func TestPythonHarvesterMissingInterpreter(t *testing.T) {
	harvester := &PythonHarvester{Exe: filepath.Join(t.TempDir(), "not-python")}
	_, err := harvester.Harvest(context.Background(), []string{"x.py"})
	if err == nil {
		t.Fatal("want an error when the interpreter does not exist")
	}
}

func TestPythonHarvesterBlankPathsSkipped(t *testing.T) {
	harvester := findPythonHarvester(t)
	reports, err := harvester.Harvest(context.Background(), []string{"", "   "})
	if err != nil {
		t.Fatalf("Harvest: %v", err)
	}
	if len(reports) != 0 {
		t.Errorf("got %d reports, want none for blank paths", len(reports))
	}
}

func TestPythonHarvesterDoesNotExecuteTheScript(t *testing.T) {
	// A harvest must never run the user's code. A script whose body would leave
	// a trace if it were imported proves the reader is static.
	dir := t.TempDir()
	path := filepath.Join(dir, "dangerous.py")
	marker := filepath.Join(dir, "executed")
	source := `"""A script that would do damage if it were run."""
import argparse

with open(r"` + marker + `", "w") as handle:
    handle.write("the script was executed")

parser = argparse.ArgumentParser()
parser.add_argument("--name", help="Who to greet.")
`
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	harvester := findPythonHarvester(t)
	reports, err := harvester.Harvest(context.Background(), []string{path})
	if err != nil {
		t.Fatalf("Harvest: %v", err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the harvester executed the script it was reading")
	}
	// The arguments are still read, which is the point of doing it statically.
	if len(reports[0].Params) != 1 {
		t.Errorf("params = %+v, want the argument to still be found", reports[0].Params)
	}
}

func TestDecodeJSONLines(t *testing.T) {
	t.Run("blank lines are ignored", func(t *testing.T) {
		got, err := decodeJSONLines([]byte("\n{\"path\":\"a.py\"}\n\n"), 1)
		if err != nil {
			t.Fatalf("decodeJSONLines: %v", err)
		}
		if len(got) != 1 || got[0].Path != "a.py" {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("one bad line does not lose the batch", func(t *testing.T) {
		got, err := decodeJSONLines([]byte("{\"path\":\"a.py\"}\nnot json\n{\"path\":\"b.py\"}\n"), 2)
		if err != nil {
			t.Fatalf("decodeJSONLines: %v", err)
		}
		if len(got) != 2 {
			t.Errorf("got %d reports, want 2", len(got))
		}
	})

	t.Run("raw control byte in a line is stripped", func(t *testing.T) {
		got, err := decodeJSONLines([]byte("{\"path\":\"a.py\",\"help\":{\"source\":\"docstring\",\"description\":\"A1B2C3 \x1a C3B2A1\"}}\n"), 1)
		if err != nil {
			t.Fatalf("decodeJSONLines: %v", err)
		}
		if len(got) != 1 || got[0].Path != "a.py" {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("no output is an error", func(t *testing.T) {
		if _, err := decodeJSONLines(nil, 3); !errors.Is(err, ErrPython) {
			t.Errorf("error = %v, want it to wrap ErrPython", err)
		}
	})

	t.Run("undecodable output is an error", func(t *testing.T) {
		if _, err := decodeJSONLines([]byte("garbage\n"), 1); !errors.Is(err, ErrPython) {
			t.Errorf("error = %v, want it to wrap ErrPython", err)
		}
	})
}

func findNamedParam(list []Param, name string) Param {
	for _, p := range list {
		if p.Name == name {
			return p
		}
	}
	return Param{}
}
