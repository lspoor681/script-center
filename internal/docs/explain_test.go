// Package docs_test exercises the documentation path end to end.
//
// It is an external test package because it drives the real PowerShell
// interpreter through internal/params, and an internal test file cannot import a
// package that depends on the package under test.
package docs_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lspoor/script-center/internal/deps"
	"github.com/lspoor/script-center/internal/docs"
	"github.com/lspoor/script-center/internal/params"
)

// The script under test is the shape that makes each of the three packages
// necessary at once: it takes parameters, dot-sources a helper, and carries its
// help block after the code so that Get-Help declines to read it.
const rebuildScript = `
param(
    [Parameter(Mandatory = $true)]
    [string]$Server,

    [ValidateSet('Fast', 'Thorough')]
    [string]$Mode = 'Fast',

    [ValidateRange(1, 64)]
    [int]$Threads = 4
)

. $PSScriptRoot/lib/common.ps1

<#
.SYNOPSIS
Rebuilds the search index on a server.

.DESCRIPTION
Connects to the named server and rebuilds its index. The helper that does the
work lives in lib/common.ps1 and travels with this script.

.PARAMETER Server
The server to rebuild. Required.

.PARAMETER Mode
How much work to do.

.EXAMPLE
PS> .\rebuild.ps1 -Server sql1 -Mode Thorough

Rebuilds sql1 thoroughly.

.LINK
https://example.com/runbooks/rebuild
#>

# Get-Help only attaches a help block that is the first or the last thing in
# the file. A statement after it, as here, makes the block invisible to the
# toolchain, which is the case the raw-text recovery exists for.
Invoke-Rebuild -Server $Server -Mode $Mode
`

const commonScript = `
function Invoke-Rebuild {
    param($Server, $Mode)
    Write-Host "rebuilding $Server ($Mode)"
}
`

const readme = `# Ops

A small set of scripts.

## rebuild

Run this on the server host. See [the runbook](https://example.com/runbooks/rebuild).

<script>alert('readme')</script>
`

const sidecar = `# Rebuild

See [the other page](notes.md).

<iframe src="https://example.com"></iframe>

| Field | Meaning |
|-------|---------|
| Mode  | depth   |
`

// writeRepo lays out a small repository and returns the script's path.
func writeRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	files := map[string]string{
		"README.md":             readme,
		"tools/docs/rebuild.md": sidecar,
		"tools/rebuild.ps1":     rebuildScript,
		"tools/lib/common.ps1":  commonScript,
	}
	for rel, content := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(root, "tools", "rebuild.ps1")
}

// harvest runs the real interpreter over the given scripts.
func harvest(t *testing.T, paths ...string) map[string]params.Report {
	t.Helper()
	exe, err := params.FindPowerShell()
	if err != nil {
		t.Skipf("no PowerShell interpreter: %v", err)
	}
	harvester := &params.PowerShellHarvester{Exe: exe}
	reports, err := harvester.Harvest(context.Background(), paths)
	if err != nil {
		t.Fatalf("Harvest: %v", err)
	}
	byPath := make(map[string]params.Report, len(reports))
	for _, report := range reports {
		byPath[report.Path] = report
	}
	return byPath
}

func TestEndToEndScriptExplanation(t *testing.T) {
	scriptPath := writeRepo(t)
	root := filepath.Dir(filepath.Dir(scriptPath))

	byPath := harvest(t, scriptPath)
	report, ok := byPath[scriptPath]
	if !ok {
		t.Fatalf("no report for %s", scriptPath)
	}

	// The parser owns the parameter schema, and it is correct without help.
	t.Run("parameters", func(t *testing.T) {
		if len(report.Params) != 3 {
			t.Fatalf("got %d parameters, want 3: %+v", len(report.Params), report.Params)
		}
		server := findParam(report.Params, "Server")
		if server.Kind != params.KindString {
			t.Errorf("Server kind = %q, want %q", server.Kind, params.KindString)
		}
		if !server.Required {
			t.Error("Server should be required")
		}
		if server.Position != 0 {
			t.Errorf("Server position = %d, want 0", server.Position)
		}
		// A type constraint arrives in the same collection as real attributes,
		// and must not be reported as a validation constraint.
		if len(server.Constraints) != 0 {
			t.Errorf("Server constraints = %+v, want none from [string]", server.Constraints)
		}

		mode := findParam(report.Params, "Mode")
		if len(mode.Constraints) != 1 || mode.Constraints[0].Kind != params.ConstraintSet {
			t.Errorf("Mode constraints = %+v, want one set", mode.Constraints)
		}
		if mode.Default == nil || mode.Default.HelpString() != "Fast" {
			t.Errorf("Mode default = %v, want %q", mode.Default, "Fast")
		}

		threads := findParam(report.Params, "Threads")
		rangeConstraint := findConstraint(threads.Constraints, params.ConstraintRange)
		if rangeConstraint == nil {
			t.Fatalf("Threads constraints = %+v, want a range", threads.Constraints)
		}
		// A bound of zero is a real bound, which is why the model carries
		// explicit presence flags instead of relying on the zero value.
		if !rangeConstraint.HasMin || rangeConstraint.Min != 1 {
			t.Errorf("range min = %v (set %t), want 1", rangeConstraint.Min, rangeConstraint.HasMin)
		}
		if !rangeConstraint.HasMax || rangeConstraint.Max != 64 {
			t.Errorf("range max = %v (set %t), want 64", rangeConstraint.Max, rangeConstraint.HasMax)
		}
	})

	// Get-Help ignores a help block that has code after it, and the harvest has
	// to say so rather than passing off the synopsis it invented.
	t.Run("helpBlockFlagged", func(t *testing.T) {
		if !report.UnparsedHelpBlock {
			t.Error("UnparsedHelpBlock = false, want true for a help block with code after it")
		}
		// The source is the author's comment, not the toolchain's guess, so the
		// UI will not present the recovery as machine-written.
		if report.Help.Source != params.HelpSourceComment {
			t.Errorf("Help.Source = %q, want %q", report.Help.Source, params.HelpSourceComment)
		}
		// The synthesized synopsis is dropped rather than kept, because
		// "rebuild.ps1 [-Server] <string>" is not something the author wrote
		// and would be misleading as the one-line summary.
		if report.Help.Synopsis != "" {
			t.Errorf("Help.Synopsis = %q, want the synthesized synopsis discarded", report.Help.Synopsis)
		}
	})

	t.Run("dotSourceDetected", func(t *testing.T) {
		if len(report.DotSources) != 1 {
			t.Fatalf("got %v, want one dot-source", report.DotSources)
		}
		if !strings.Contains(report.DotSources[0], "lib/common.ps1") {
			t.Errorf("dot-source = %q, want it to name the helper", report.DotSources[0])
		}
	})

	// The documentation the user sees has to be the block the author wrote,
	// not the synopsis PowerShell made up from the parameter list.
	t.Run("explanation", func(t *testing.T) {
		explanation, err := docs.Explain(scriptPath, root, report)
		if err != nil {
			t.Fatalf("Explain: %v", err)
		}
		if !explanation.Recovered {
			t.Error("Recovered = false, want the raw help block to be used")
		}
		if explanation.Help.Source != params.HelpSourceComment {
			t.Errorf("Help.Source = %q, want %q", explanation.Help.Source, params.HelpSourceComment)
		}
		if explanation.Help.Synopsis != "Rebuilds the search index on a server." {
			t.Errorf("synopsis = %q", explanation.Help.Synopsis)
		}
		if !strings.Contains(explanation.Help.Description, "lib/common.ps1") {
			t.Errorf("description = %q, want it to mention the helper", explanation.Help.Description)
		}
		if explanation.Help.ParamHelp(findParam(report.Params, "Server")) != "The server to rebuild. Required." {
			t.Errorf("Server help = %q", explanation.Help.ParamHelp(findParam(report.Params, "Server")))
		}
		if len(explanation.Help.Links) != 1 || explanation.Help.Links[0] != "https://example.com/runbooks/rebuild" {
			t.Errorf("links = %v", explanation.Help.Links)
		}
		if len(explanation.Help.Examples) != 1 {
			t.Errorf("examples = %+v, want one", explanation.Help.Examples)
		}

		if !explanation.HasDocuments() || explanation.Primary == nil {
			t.Fatal("want the readme and sidecar to be found")
		}
		// A sidecar beside the script is more specific than a repository readme.
		if filepath.Base(explanation.Primary.Path) != "rebuild.md" {
			t.Errorf("primary document = %q, want the sidecar", explanation.Primary.Path)
		}
	})

	// A script that dot-sources a helper cannot run on a remote host on its own,
	// so the helper has to travel with it.
	t.Run("closure", func(t *testing.T) {
		byPath := harvest(t, scriptPath)
		report := byPath[scriptPath]

		find := func(path string) ([]string, error) {
			// The helper has no dot-sources of its own, so one harvest of the
			// known file is enough to stand in for a parser here.
			byPath := harvest(t, path)
			return byPath[path].DotSources, nil
		}
		closure, err := deps.Resolver{Find: find}.Resolve(scriptPath, report.DotSources)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if len(closure.Files) != 2 {
			t.Fatalf("closure = %v, want the script and its helper", closure.Files)
		}
		if closure.Files[0] != scriptPath {
			t.Errorf("closure[0] = %q, want the entry script", closure.Files[0])
		}
		if !strings.HasSuffix(closure.Files[1], filepath.Join("lib", "common.ps1")) {
			t.Errorf("closure[1] = %q, want the helper", closure.Files[1])
		}
		if closure.HasUnresolved() {
			t.Errorf("unresolved = %v, want none", closure.Unresolved)
		}
		for _, path := range closure.Files {
			if _, err := os.Stat(path); err != nil {
				t.Errorf("closure lists a missing file %s: %v", path, err)
			}
		}
	})

	// The readme is a file in a repository the user cloned, so it cannot be
	// allowed to bring markup with it.
	t.Run("readmeIsSanitized", func(t *testing.T) {
		documents, err := docs.Discover(scriptPath, root)
		if err != nil {
			t.Fatalf("Discover: %v", err)
		}
		var readmeDoc *docs.Document
		for i := range documents {
			if filepath.Base(documents[i].Path) == "README.md" {
				readmeDoc = &documents[i]
			}
		}
		if readmeDoc == nil {
			t.Fatal("README.md was not discovered")
		}
		// The readme has a heading naming this script, so only that section is
		// shown, and the script tag below it has to be stripped on the way.
		if !readmeDoc.Excerpted {
			t.Fatalf("README.md should have been excerpted to the rebuild section, got %q", readmeDoc.Section)
		}
		if !strings.Contains(readmeDoc.Section, "Run this on the server host") {
			t.Errorf("excerpt = %q, want the section about rebuild", readmeDoc.Section)
		}
		if strings.Contains(readmeDoc.Section, "A small set of scripts") {
			t.Errorf("excerpt should not include the readme preamble, got %q", readmeDoc.Section)
		}

		rendered, err := docs.Render(".md", []byte(readmeDoc.Section))
		if err != nil {
			t.Fatalf("Render: %v", err)
		}
		if strings.Contains(rendered, "<script") || strings.Contains(rendered, "alert(") {
			t.Errorf("readme markup survived:\n%s", rendered)
		}
		if !strings.Contains(rendered, "server host") {
			t.Errorf("readme content lost:\n%s", rendered)
		}
	})
}

func findParam(list []params.Param, name string) params.Param {
	for _, p := range list {
		if p.Name == name {
			return p
		}
	}
	return params.Param{}
}

func findConstraint(list []params.Constraint, kind params.ConstraintKind) *params.Constraint {
	for i := range list {
		if list[i].Kind == kind {
			return &list[i]
		}
	}
	return nil
}

// TestExplanationKeepsToolchainHelp is the guard against the raw-text recovery
// firing when it should not.
//
// Get-Help attaches a help block that is the first or the last thing in a file.
// For those two shapes it reads the help properly, and re-parsing the file would
// be redundant work that could only lose detail. A block sandwiched between the
// param block and code that follows it is the only case worth recovering, which
// is what TestEndToEndScriptExplanation covers.
func TestExplanationKeepsToolchainHelp(t *testing.T) {
	tests := map[string]struct {
		script   string
		synopsis string
	}{
		"last thing in the file": {
			script:   "param([string]$Server)\n\n<#\n.SYNOPSIS\nRebuilds the index.\n\n.PARAMETER Server\nThe server.\n#>\n",
			synopsis: "Rebuilds the index.",
		},
		"first thing in the file": {
			script:   "<#\n.SYNOPSIS\nRebuilds the index.\n\n.PARAMETER Server\nThe server.\n#>\nparam([string]$Server)\n\nWrite-Host $Server\n",
			synopsis: "Rebuilds the index.",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			scriptPath := filepath.Join(dir, "rebuild.ps1")
			if err := os.WriteFile(scriptPath, []byte(test.script), 0o644); err != nil {
				t.Fatal(err)
			}

			byPath := harvest(t, scriptPath)
			report, ok := byPath[scriptPath]
			if !ok {
				t.Fatalf("no report for %s", scriptPath)
			}
			if report.UnparsedHelpBlock {
				t.Error("UnparsedHelpBlock = true, want false for help the toolchain reads itself")
			}

			explanation, err := docs.Explain(scriptPath, dir, report)
			if err != nil {
				t.Fatalf("Explain: %v", err)
			}
			if explanation.Recovered {
				t.Error("Recovered = true, want the toolchain's own help to stand")
			}
			if explanation.Help.Synopsis != test.synopsis {
				t.Errorf("synopsis = %q, want %q", explanation.Help.Synopsis, test.synopsis)
			}
			if explanation.Help.ParamHelp(findParam(report.Params, "Server")) != "The server." {
				t.Errorf("Server help = %q", explanation.Help.ParamHelp(findParam(report.Params, "Server")))
			}
		})
	}
}
