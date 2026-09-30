package params

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestHarvestPathHeuristic verifies that the PowerShell harvester infers KindPath
// from parameter name suffixes when the declared type is ambiguous (string/other).
func TestHarvestPathHeuristic(t *testing.T) {
	h := harvesterFor(t)

	// Create a temporary script with various parameter name patterns
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "heuristic.ps1")
	scriptContent := `<#
.SYNOPSIS
Test script for path heuristic.
#>
param(
    [string]$ConfigPath,
    [string]$InputFile,
    [string]$LogDir,
    [string]$OutputFolder,
    [string]$DataCsv,
    [string]$NotesTxt,
    [string]$ResultOutput,
    [string]$ServerName,
    [int]$RetryCount,
    [System.IO.FileInfo]$ExplicitPath,
    [string]$FilePathName
)
Write-Host "test"
`
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}

	reports, err := h.Harvest(context.Background(), []string{scriptPath})
	if err != nil {
		t.Fatalf("Harvest: %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("got %d reports, want 1", len(reports))
	}
	report := reports[0]

	tests := []struct {
		name     string
		param    string
		wantKind string
	}{
		{"ConfigPath suffix", "ConfigPath", "path"},
		{"InputFile suffix", "InputFile", "path"},
		{"LogDir suffix", "LogDir", "path"},
		{"OutputFolder suffix", "OutputFolder", "path"},
		{"DataCsv suffix", "DataCsv", "path"},
		{"NotesTxt suffix", "NotesTxt", "path"},
		{"ResultOutput suffix", "ResultOutput", "path"},
		{"ServerName no-match", "ServerName", "string"},
		{"RetryCount explicit int", "RetryCount", "int"},
		{"ExplicitPath FileInfo type", "ExplicitPath", "path"},
		{"FilePathName contains not suffix", "FilePathName", "string"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			param := findParam(t, report, tc.param)
			if string(param.Kind) != tc.wantKind {
				t.Errorf("%s: Kind = %q, want %q (typeName=%q)", tc.name, param.Kind, tc.wantKind, param.TypeName)
			}
		})
	}
}

func TestHarvestPathHeuristicCaseInsensitive(t *testing.T) {
	h := harvesterFor(t)

	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "heuristic_case.ps1")
	scriptContent := `param(
    [string]$configpath,
    [string]$INPUTFILE,
    [string]$logdir,
    [string]$outputfolder
)
Write-Host "test"
`
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}

	reports, err := h.Harvest(context.Background(), []string{scriptPath})
	if err != nil {
		t.Fatalf("Harvest: %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("got %d reports, want 1", len(reports))
	}
	report := reports[0]

	for _, name := range []string{"configpath", "INPUTFILE", "logdir", "outputfolder"} {
		t.Run(name, func(t *testing.T) {
			param := findParam(t, report, name)
			if string(param.Kind) != "path" {
				t.Errorf("%s: Kind = %q, want path", name, param.Kind)
			}
		})
	}
}

func TestHarvestPathHeuristicNoPowerShell(t *testing.T) {
	// This test documents that the heuristic only applies to PowerShell
	// (Python/Bash harvesters don't have this fallback)
	t.Skip("Documentation test - no action needed")
}
