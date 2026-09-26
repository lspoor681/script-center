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

// harvesterFor returns a harvester, skipping the test when no interpreter is
// available. The Linux and Windows CI images both ship one, so these run there;
// a developer machine without PowerShell still gets a green run rather than a
// failure about somebody else's environment.
func harvesterFor(t *testing.T) *PowerShellHarvester {
	t.Helper()
	exe, err := FindPowerShell()
	if err != nil {
		t.Skipf("skipping: %v", err)
	}
	return &PowerShellHarvester{Exe: exe, Timeout: 90 * time.Second}
}

// fixture returns the absolute path of a testdata script, skipping the test when
// the fixture is missing so that a packaging mistake is obvious.
func fixture(t *testing.T, name string) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("resolving fixture %s: %v", name, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return path
}

// findParam returns the named parameter, failing the test when it is absent.
func findParam(t *testing.T, report Report, name string) Param {
	t.Helper()
	for _, param := range report.Params {
		if param.Name == name {
			return param
		}
	}
	t.Fatalf("no parameter named %q in report; got %v", name, paramNames(report.Params))
	return Param{}
}

func paramNames(params []Param) []string {
	names := make([]string, 0, len(params))
	for _, param := range params {
		names = append(names, param.Name)
	}
	return names
}

// TestHarvestFull covers the whole schema at once because the attributes interact:
// a ValidateSet changes the kind, an alias changes how the form addresses the
// parameter, and a default changes what the control is pre-filled with.
func TestHarvestFull(t *testing.T) {
	h := harvesterFor(t)
	path := fixture(t, "full.ps1")

	reports, err := h.Harvest(context.Background(), []string{path})
	if err != nil {
		t.Fatalf("Harvest: %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("got %d reports, want 1", len(reports))
	}
	report := reports[0]

	if report.HasWarnings() {
		t.Errorf("unexpected warnings: %v", report.Warnings)
	}
	if report.Path != path {
		t.Errorf("Path = %q, want %q", report.Path, path)
	}
	if report.UnparsedHelpBlock {
		t.Error("UnparsedHelpBlock = true, but the help block is ahead of the param block")
	}

	wantNames := []string{
		"Server", "Count", "Mode", "Tag", "OutDir", "Extras", "Force",
		"Credential", "Generated",
	}
	if got := paramNames(report.Params); !equalStrings(got, wantNames) {
		t.Errorf("params = %v, want %v (declaration order must be preserved)", got, wantNames)
	}

	t.Run("validate set becomes an enum", func(t *testing.T) {
		server := findParam(t, report, "Server")
		if server.Kind != KindEnum {
			t.Errorf("Kind = %q, want %q", server.Kind, KindEnum)
		}
		if len(server.Constraints) != 1 {
			t.Fatalf("got %d constraints, want 1", len(server.Constraints))
		}
		got := server.Constraints[0]
		if got.Kind != ConstraintSet {
			t.Errorf("constraint kind = %q, want %q", got.Kind, ConstraintSet)
		}
		if !equalStrings(got.Values, []string{"Full", "Partial", "Incremental"}) {
			t.Errorf("constraint values = %v", got.Values)
		}
		if !strings.Contains(got.Label, "Full") {
			t.Errorf("constraint label = %q, want it to name the values", got.Label)
		}
	})

	t.Run("aliases and position", func(t *testing.T) {
		server := findParam(t, report, "Server")
		// Aliases are sorted so two harvests of one file produce identical
		// output, which is what lets the cache compare reports.
		if !equalStrings(server.Aliases, []string{"Host", "Srv"}) {
			t.Errorf("Aliases = %v, want sorted [Host Srv]", server.Aliases)
		}
		if !server.Required {
			t.Error("Server should be required: it is declared Mandatory")
		}
		// The parser numbers positions from zero and the form from one.
		if server.Position != 1 {
			t.Errorf("Position = %d, want 1", server.Position)
		}
		if findParam(t, report, "Count").Position != 0 {
			t.Error("a parameter with no declared position should report 0")
		}
	})

	t.Run("pipeline input", func(t *testing.T) {
		if !findParam(t, report, "Count").FromPipeline {
			t.Error("Count is declared ValueFromPipeline and should report it")
		}
		if findParam(t, report, "Server").FromPipeline {
			t.Error("Server is not declared ValueFromPipeline")
		}
	})

	t.Run("defaults", func(t *testing.T) {
		if got := findParam(t, report, "Count").Default.HelpString(); got != "50" {
			t.Errorf("Count default = %q, want 50", got)
		}
		// An array literal is a literal, not an expression, so it is worth
		// showing pre-filled with its elements flattened for display.
		extras := findParam(t, report, "Extras").Default
		if extras == nil {
			t.Fatal("Extras has a default but Default is nil")
		}
		if extras.IsExpression {
			t.Error("Extras default @('a','b') should not be reported as an expression")
		}
		if extras.Display != "a, b" {
			t.Errorf("Extras display = %q, want %q", extras.Display, "a, b")
		}
		// A default that calls something must not be evaluated, because running
		// it during discovery would have side effects and would not reflect what
		// the script does at run time.
		generated := findParam(t, report, "Generated").Default
		if generated == nil {
			t.Fatal("Generated has a default but Default is nil")
		}
		if !generated.IsExpression {
			t.Error("Generated default (Get-Date) should be reported as an expression")
		}
		if generated.Source != "(Get-Date)" {
			t.Errorf("Generated source = %q, want the text as written", generated.Source)
		}
		if findParam(t, report, "Tag").Default != nil {
			t.Error("Tag declares no default, so Default should be nil rather than zero-valued")
		}
	})

	t.Run("constraint kinds", func(t *testing.T) {
		count := findParam(t, report, "Count")
		if len(count.Constraints) != 1 || count.Constraints[0].Kind != ConstraintRange {
			t.Fatalf("Count constraints = %+v, want one range", count.Constraints)
		}
		rangeConstraint := count.Constraints[0]
		if !rangeConstraint.HasMin || !rangeConstraint.HasMax {
			t.Errorf("range bounds missing: %+v", rangeConstraint)
		}
		if rangeConstraint.Min != 1 || rangeConstraint.Max != 500 {
			t.Errorf("range = %v..%v, want 1..500", rangeConstraint.Min, rangeConstraint.Max)
		}

		tag := findParam(t, report, "Tag")
		if len(tag.Constraints) != 1 || tag.Constraints[0].Kind != ConstraintPattern {
			t.Fatalf("Tag constraints = %+v, want one pattern", tag.Constraints)
		}
		if tag.Constraints[0].Pattern != "^[a-z][a-z0-9-]*$" {
			t.Errorf("pattern = %q", tag.Constraints[0].Pattern)
		}

		out := findParam(t, report, "OutDir")
		if len(out.Constraints) != 1 || out.Constraints[0].Kind != ConstraintNotEmpty {
			t.Errorf("OutDir constraints = %+v, want one notEmpty", out.Constraints)
		}
	})

	t.Run("kinds", func(t *testing.T) {
		for name, want := range map[string]Kind{
			"Count":      KindInt,
			"Server":     KindEnum,
			"Tag":        KindString,
			"Extras":     KindArray,
			"Force":      KindBool,
			"Credential": KindSecret,
			// An untyped parameter must not be guessed at: a form built on a
			// wrong kind is worse than no form.
			"Generated": KindOther,
		} {
			if got := findParam(t, report, name).Kind; got != want {
				t.Errorf("%s kind = %q, want %q", name, got, want)
			}
		}
	})

	t.Run("requirements", func(t *testing.T) {
		req := report.Requirements
		if req.IsEmpty() {
			t.Fatal("Requirements is empty but the script declares three #Requires lines")
		}
		if !req.RunAsAdministrator {
			t.Error("RunAsAdministrator = false, want true for #Requires -RunAsAdministrator")
		}
		if req.PSVersion != "7.0" {
			t.Errorf("PSVersion = %q, want 7.0", req.PSVersion)
		}
		if len(req.Modules) != 1 || req.Modules[0].Name != "SqlServer" {
			t.Errorf("Modules = %+v, want one SqlServer", req.Modules)
		}
		if got := req.Modules[0].String(); got != "SqlServer" {
			t.Errorf("ModuleRequirement.String() = %q", got)
		}
	})

	t.Run("dot sources", func(t *testing.T) {
		want := []string{
			"$PSScriptRoot/lib/common.ps1",
			`$PSScriptRoot/lib\helpers.ps1`,
			"./sibling.ps1",
			"$shared",
		}
		if !equalStrings(report.DotSources, want) {
			t.Errorf("DotSources = %v, want %v", report.DotSources, want)
		}
	})

	t.Run("help", func(t *testing.T) {
		if report.Help.Source != HelpSourceComment {
			t.Errorf("Help.Source = %q, want %q", report.Help.Source, HelpSourceComment)
		}
		if report.Help.Synopsis != "Rebuilds the search index for one site." {
			t.Errorf("Synopsis = %q", report.Help.Synopsis)
		}
		if !strings.Contains(report.Help.Description, "walks the content tree") {
			t.Errorf("Description = %q", report.Help.Description)
		}
		if !strings.Contains(report.Help.Notes, "service account") {
			t.Errorf("Notes = %q", report.Help.Notes)
		}
		if len(report.Help.Links) != 2 {
			t.Errorf("Links = %v, want two", report.Help.Links)
		}
		if len(report.Help.Examples) != 2 {
			t.Fatalf("got %d examples, want 2: %+v", len(report.Help.Examples), report.Help.Examples)
		}
		// Get-Help synthesizes a banner title for every example. It is
		// presentation rather than something the author wrote, so it must not
		// reach the UI.
		for i, example := range report.Help.Examples {
			if strings.Contains(example.Title, "EXAMPLE") {
				t.Errorf("example %d title = %q, want the banner stripped", i, example.Title)
			}
			if strings.TrimSpace(example.Command) == "" {
				t.Errorf("example %d has no command", i)
			}
		}
		if !strings.Contains(report.Help.Examples[0].Command, "-Server dc01") {
			t.Errorf("first example = %q", report.Help.Examples[0].Command)
		}
	})

	t.Run("per parameter help", func(t *testing.T) {
		if got := findParam(t, report, "Server").Help; got != "The server to rebuild." {
			t.Errorf("Server help = %q", got)
		}
		if got := findParam(t, report, "Count").Help; got != "How many documents to process." {
			t.Errorf("Count help = %q", got)
		}
		// The schema is authoritative and the prose is merged onto it, so a
		// parameter the author never documented keeps an empty description
		// rather than borrowing another parameter's.
		if got := findParam(t, report, "Extras").Help; got != "" {
			t.Errorf("Extras help = %q, want empty", got)
		}
	})
}

// TestHarvestHelpBlockAfterParamBlock covers the case that makes the raw-text
// fallback necessary: Get-Help ignores a help block that is not ahead of the
// param block, but plenty of scripts put it at the end. The harvester must say
// so rather than presenting PowerShell's synthesized usage signature as the
// author's summary.
func TestHarvestHelpBlockAfterParamBlock(t *testing.T) {
	h := harvesterFor(t)

	reports, err := h.Harvest(context.Background(), []string{fixture(t, "bottomhelp.ps1")})
	if err != nil {
		t.Fatalf("Harvest: %v", err)
	}
	report := reports[0]

	if !report.UnparsedHelpBlock {
		t.Error("UnparsedHelpBlock = false, want true for a help block after the param block")
	}
	if report.Help.Source != HelpSourceComment {
		t.Errorf("Help.Source = %q, want %q: the author did write help", report.Help.Source, HelpSourceComment)
	}
	// The synopsis must be cleared rather than filled with the usage string
	// Get-Help invented from the parameter list.
	if report.Help.Synopsis != "" {
		t.Errorf("Synopsis = %q, want empty: it was synthesized, not written", report.Help.Synopsis)
	}
	if len(report.Params) != 2 {
		t.Errorf("got %d params, want 2", len(report.Params))
	}
}

// TestHarvestNoHelp checks that a script with no documentation is labeled as
// such instead of being described by a signature the interpreter generated.
func TestHarvestNoHelp(t *testing.T) {
	h := harvesterFor(t)

	reports, err := h.Harvest(context.Background(), []string{fixture(t, "nohelp.ps1")})
	if err != nil {
		t.Fatalf("Harvest: %v", err)
	}
	report := reports[0]

	if report.UnparsedHelpBlock {
		t.Error("UnparsedHelpBlock = true, but the file has no help block at all")
	}
	if report.Help.Source != HelpSourceGenerated {
		t.Errorf("Help.Source = %q, want %q", report.Help.Source, HelpSourceGenerated)
	}
	if !equalStrings(report.Requirements.PSEditions, []string{"Desktop"}) {
		t.Errorf("PSEditions = %v, want [Desktop]", report.Requirements.PSEditions)
	}
}

// TestHarvestBrokenScript checks that a file that does not parse still produces
// a report, because a script with a warning is runnable and a script missing
// from the list is not.
func TestHarvestBrokenScript(t *testing.T) {
	h := harvesterFor(t)

	reports, err := h.Harvest(context.Background(), []string{fixture(t, "broken.ps1")})
	if err != nil {
		t.Fatalf("Harvest: %v", err)
	}
	report := reports[0]

	if !report.HasWarnings() {
		t.Error("a file that does not parse should produce a warning")
	}
	if report.Path == "" {
		t.Error("Path should be set even for a file that does not parse")
	}
}

// TestHarvestMissingFile checks that a path the interpreter cannot read is
// reported rather than dropped.
func TestHarvestMissingFile(t *testing.T) {
	h := harvesterFor(t)
	missing := filepath.Join(t.TempDir(), "absent.ps1")

	reports, err := h.Harvest(context.Background(), []string{missing})
	if err != nil {
		t.Fatalf("Harvest: %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("got %d reports, want 1", len(reports))
	}
	if !reports[0].HasWarnings() {
		t.Error("a missing file should produce a warning")
	}
}

// TestHarvestPreservesOrderAndPath checks the alignment pass. A record that
// arrived out of order, or not at all, would otherwise be attributed to the
// wrong script and produce a form for parameters it does not have.
func TestHarvestPreservesOrderAndPath(t *testing.T) {
	h := harvesterFor(t)
	full := fixture(t, "full.ps1")
	bottom := fixture(t, "bottomhelp.ps1")

	// Requested in an order the interpreter is not obliged to echo back.
	reports, err := h.Harvest(context.Background(), []string{bottom, full})
	if err != nil {
		t.Fatalf("Harvest: %v", err)
	}
	if len(reports) != 2 {
		t.Fatalf("got %d reports, want 2", len(reports))
	}
	if reports[0].Path != bottom {
		t.Errorf("reports[0].Path = %q, want %q", reports[0].Path, bottom)
	}
	if reports[1].Path != full {
		t.Errorf("reports[1].Path = %q, want %q", reports[1].Path, full)
	}
	if !reports[0].UnparsedHelpBlock {
		t.Error("the first report should be the one with the trailing help block")
	}
	if reports[1].UnparsedHelpBlock {
		t.Error("the second report should not claim a trailing help block")
	}
}

// TestHarvestBatches checks that a batch larger than one interpreter run still
// produces exactly one report per script, in order.
func TestHarvestBatches(t *testing.T) {
	h := harvesterFor(t)
	small := &PowerShellHarvester{Exe: h.Exe, Timeout: 90 * time.Second, BatchSize: 1}
	full := fixture(t, "full.ps1")
	bottom := fixture(t, "bottomhelp.ps1")
	none := fixture(t, "nohelp.ps1")

	paths := []string{full, bottom, none, full}
	reports, err := small.Harvest(context.Background(), paths)
	if err != nil {
		t.Fatalf("Harvest: %v", err)
	}
	if len(reports) != len(paths) {
		t.Fatalf("got %d reports, want %d", len(reports), len(paths))
	}
	for i, path := range paths {
		if reports[i].Path != path {
			t.Errorf("reports[%d].Path = %q, want %q", i, reports[i].Path, path)
		}
	}
}

func TestHarvestEmptyInput(t *testing.T) {
	h := &PowerShellHarvester{Exe: "definitely-not-a-real-interpreter"}
	reports, err := h.Harvest(context.Background(), nil)
	if err != nil {
		t.Errorf("Harvest(nil) = %v, want no error", err)
	}
	if reports != nil {
		t.Errorf("Harvest(nil) = %v, want nil", reports)
	}
}

// TestHarvestMissingInterpreter checks that an unusable interpreter is a clear
// error rather than a panic or an empty result that looks like "no parameters".
func TestHarvestMissingInterpreter(t *testing.T) {
	h := &PowerShellHarvester{Exe: "definitely-not-a-real-interpreter"}
	_, err := h.Harvest(context.Background(), []string{"x.ps1"})
	if !errors.Is(err, ErrPowerShell) {
		t.Errorf("error = %v, want it to wrap ErrPowerShell", err)
	}
}

func TestFindPowerShell(t *testing.T) {
	path, err := FindPowerShell()
	if err != nil {
		if !errors.Is(err, ErrNoPowerShell) {
			t.Errorf("error = %v, want ErrNoPowerShell", err)
		}
		return
	}
	if path == "" {
		t.Error("FindPowerShell returned an empty path with no error")
	}
}

func TestDecodeRecords(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int
		wantErr bool
	}{
		{
			name:  "empty array",
			input: `[]`,
			want:  0,
		},
		{
			// One script serializes as a one-element array, and collapsing it
			// to a bare object is the mistake this decode guards against.
			name:  "single record stays an array",
			input: `[{"path":"a.ps1","params":[],"help":{"source":"none"}}]`,
			want:  1,
		},
		{
			name:  "bare object is still accepted",
			input: `{"path":"a.ps1","params":[],"help":{"source":"none"}}`,
			want:  1,
		},
		{
			name:  "several records",
			input: `[{"path":"a.ps1"},{"path":"b.ps1"},{"path":"c.ps1"}]`,
			want:  3,
		},
		{
			name:  "null help decodes to the none source",
			input: `[{"path":"a.ps1","help":null,"params":null}]`,
			want:  1,
		},
		{
			name:    "empty output is an error",
			input:   "   ",
			wantErr: true,
		},
		{
			name:    "malformed output is an error",
			input:   `not json`,
			wantErr: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reports, err := decodeRecords([]byte(test.input), nil)
			if test.wantErr {
				if err == nil {
					t.Fatal("want an error, got none")
				}
				if !errors.Is(err, ErrPowerShell) {
					t.Errorf("error = %v, want it to wrap ErrPowerShell", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("decodeRecords: %v", err)
			}
			if len(reports) != test.want {
				t.Errorf("got %d reports, want %d", len(reports), test.want)
			}
			for _, report := range reports {
				if report.Help.Source == "" {
					t.Error("Help.Source should never be the empty string")
				}
			}
		})
	}
}

func TestDecodeRecordsCarriesConstraintBounds(t *testing.T) {
	input := `[{"path":"a.ps1","params":[
		{"name":"N","kind":"int","constraints":[
			{"kind":"range","label":"between 0 and 10","min":0,"max":10,
			 "hasMin":true,"hasMax":true,"includeMin":true,"includeMax":true}]}]}]`

	reports, err := decodeRecords([]byte(input), nil)
	if err != nil {
		t.Fatalf("decodeRecords: %v", err)
	}
	if len(reports) != 1 || len(reports[0].Params) != 1 {
		t.Fatalf("unexpected shape: %+v", reports)
	}
	constraints := reports[0].Params[0].Constraints
	if len(constraints) != 1 {
		t.Fatalf("got %d constraints, want 1", len(constraints))
	}
	got := constraints[0]
	// A lower bound of zero is a real bound, which is why HasMin exists
	// alongside the value.
	if !got.HasMin || got.Min != 0 {
		t.Errorf("HasMin = %v, Min = %v, want true and 0", got.HasMin, got.Min)
	}
	if !got.HasMax || got.Max != 10 {
		t.Errorf("HasMax = %v, Max = %v, want true and 10", got.HasMax, got.Max)
	}
}

func TestAlign(t *testing.T) {
	t.Run("reorders to the requested sequence", func(t *testing.T) {
		reports := []Report{{Path: "b.ps1"}, {Path: "a.ps1"}}
		got := align(reports, []string{"a.ps1", "b.ps1"})
		if len(got) != 2 {
			t.Fatalf("got %d reports, want 2", len(got))
		}
		if got[0].Path != "a.ps1" || got[1].Path != "b.ps1" {
			t.Errorf("paths = %q, %q; want a.ps1, b.ps1", got[0].Path, got[1].Path)
		}
	})

	t.Run("a missing record becomes a warning", func(t *testing.T) {
		got := align(nil, []string{"gone.ps1"})
		if len(got) != 1 {
			t.Fatalf("got %d reports, want 1", len(got))
		}
		if !got[0].HasWarnings() {
			t.Error("a script with no record should carry a warning")
		}
		if got[0].Path != "gone.ps1" {
			t.Errorf("Path = %q, want gone.ps1", got[0].Path)
		}
		if got[0].Help.Source != HelpSourceNone {
			t.Errorf("Help.Source = %q, want %q", got[0].Help.Source, HelpSourceNone)
		}
	})

	t.Run("a record with no path adopts the requested one", func(t *testing.T) {
		got := align([]Report{{}}, []string{"x.ps1"})
		if got[0].Path != "x.ps1" {
			t.Errorf("Path = %q, want x.ps1", got[0].Path)
		}
	})
}

func TestDropEmptyConstraints(t *testing.T) {
	if got := dropEmptyConstraints(nil); got != nil {
		t.Errorf("dropEmptyConstraints(nil) = %v, want nil", got)
	}
	in := []Constraint{
		{Kind: ConstraintSet},
		{Kind: ConstraintRange},
		{Kind: ConstraintPattern},
		{Kind: ConstraintNotEmpty, Label: "must not be empty"},
	}
	got := dropEmptyConstraints(in)
	if len(got) != 1 {
		t.Fatalf("got %d constraints, want 1: %+v", len(got), got)
	}
	if got[0].Kind != ConstraintNotEmpty {
		t.Errorf("kept the wrong constraint: %+v", got[0])
	}
	// Dropping everything must yield nil rather than an empty slice, so that
	// "no constraints" is distinguishable from "constraints were all empty".
	if got := dropEmptyConstraints([]Constraint{{Kind: ConstraintSet}}); got != nil {
		t.Errorf("got %v, want nil when every constraint is empty", got)
	}
}

func TestConstraintIsZero(t *testing.T) {
	tests := []struct {
		kind ConstraintKind
		want bool
	}{
		{ConstraintSet, true},
		{ConstraintRange, true},
		{ConstraintLength, true},
		{ConstraintPattern, true},
		{ConstraintCredential, true},
		{ConstraintNotEmpty, true},
	}
	for _, test := range tests {
		if got := (Constraint{Kind: test.kind}).IsZero(); got != test.want {
			t.Errorf("Constraint{%s}.IsZero() = %v, want %v", test.kind, got, test.want)
		}
	}
	populated := []Constraint{
		{Kind: ConstraintSet, Values: []string{"a"}},
		{Kind: ConstraintRange, HasMin: true},
		{Kind: ConstraintLength, HasMax: true},
		{Kind: ConstraintPattern, Pattern: "^a$"},
		{Kind: ConstraintCredential, Label: "needs a credential"},
		{Kind: ConstraintNotEmpty, Label: "must not be empty"},
	}
	for _, constraint := range populated {
		if constraint.IsZero() {
			t.Errorf("Constraint{%s} should not be zero", constraint.Kind)
		}
	}
	// A range whose lower bound is legitimately zero is not empty.
	zero := Constraint{Kind: ConstraintRange, HasMin: true, Min: 0}
	if zero.IsZero() {
		t.Error("a range with an explicit lower bound of zero should not be zero")
	}
}

func TestSortedAliases(t *testing.T) {
	if got := SortedAliases(nil); got != nil {
		t.Errorf("SortedAliases(nil) = %v, want nil", got)
	}
	if got := SortedAliases([]string{"B"}); !equalStrings(got, []string{"B"}) {
		t.Errorf("SortedAliases single = %v", got)
	}
	in := []string{"Srv", "Host", "Alt"}
	got := SortedAliases(in)
	if !equalStrings(got, []string{"Alt", "Host", "Srv"}) {
		t.Errorf("SortedAliases = %v, want sorted", got)
	}
	// The input must not be reordered in place: the caller may reuse it.
	if !equalStrings(in, []string{"Srv", "Host", "Alt"}) {
		t.Errorf("SortedAliases modified its input: %v", in)
	}
}

func TestHelpParamHelp(t *testing.T) {
	help := Help{Params: map[string]string{
		"Server": "the target server",
		"H":      "described by alias",
	}}
	if got := help.ParamHelp(Param{Name: "Server"}); got != "the target server" {
		t.Errorf("by name = %q", got)
	}
	// A doc comment describing a short option still has to land on the right
	// parameter.
	if got := help.ParamHelp(Param{Name: "Host", Aliases: []string{"H"}}); got != "described by alias" {
		t.Errorf("by alias = %q", got)
	}
	// The primary name wins over an alias.
	both := Help{Params: map[string]string{"P": "by alias", "Primary": "by name"}}
	if got := both.ParamHelp(Param{Name: "Primary", Aliases: []string{"P"}}); got != "by name" {
		t.Errorf("name should win, got %q", got)
	}
	var empty Help
	if got := empty.ParamHelp(Param{Name: "X"}); got != "" {
		t.Errorf("empty help = %q, want empty", got)
	}
}

func TestReportAddWarningDeduplicates(t *testing.T) {
	var report Report
	report.AddWarning("same")
	report.AddWarning("same")
	report.AddWarning("other")
	report.AddWarning("")
	report.AddWarning("%s", "formatted")
	if len(report.Warnings) != 3 {
		t.Errorf("warnings = %v, want three distinct entries", report.Warnings)
	}
	if !report.HasWarnings() {
		t.Error("HasWarnings = false")
	}
}

func TestModuleRequirementString(t *testing.T) {
	withVersion := ModuleRequirement{Name: "Az", Version: "2.0"}
	if got := withVersion.String(); got != "Az 2.0" {
		t.Errorf("String() = %q, want %q", got, "Az 2.0")
	}
	without := ModuleRequirement{Name: "Az"}
	if got := without.String(); got != "Az" {
		t.Errorf("String() = %q, want %q", got, "Az")
	}
}

func TestValueHelpString(t *testing.T) {
	var nilValue *Value
	if got := nilValue.HelpString(); got != "" {
		t.Errorf("nil HelpString() = %q, want empty", got)
	}
	// Display is preferred because it is the flattened form, but Source is the
	// fallback so that a value the harvester could not flatten is still shown.
	sourceOnly := &Value{Source: "@('a','b')"}
	if got := sourceOnly.HelpString(); got != "@('a','b')" {
		t.Errorf("HelpString() = %q, want the source", got)
	}
	both := &Value{Source: "@('a','b')", Display: "a, b"}
	if got := both.HelpString(); got != "a, b" {
		t.Errorf("HelpString() = %q, want the display", got)
	}
}

func TestFirstLine(t *testing.T) {
	tests := map[string]string{
		"only one line":    "only one line",
		"first\nsecond":    "first",
		"first\r\nsecond":  "first",
		"  padded\nsecond": "padded",
		"trailing\n":       "trailing",
	}
	for input, want := range tests {
		if got := firstLine(input); got != want {
			t.Errorf("firstLine(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestWriteHarvestScript(t *testing.T) {
	path, cleanup, err := writeHarvestScript()
	if err != nil {
		t.Fatalf("writeHarvestScript: %v", err)
	}
	defer cleanup()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the extracted script: %v", err)
	}
	if string(data) != harvestScript {
		t.Error("the extracted script does not match the embedded one")
	}
	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("cleanup should have removed the extracted script")
	}
}

func TestParamFlag(t *testing.T) {
	if got := (Param{Name: "Server"}).Flag(); got != "Server" {
		t.Errorf("Flag() = %q", got)
	}
	withAliases := Param{Name: "Server", Aliases: []string{"S", "Srv"}}
	if got := withAliases.Flag(); got != "Server, -S, -Srv" {
		t.Errorf("Flag() = %q", got)
	}
}

func TestRequirementsIsEmpty(t *testing.T) {
	var empty Requirements
	if !empty.IsEmpty() {
		t.Error("the zero Requirements should be empty")
	}
	populated := []Requirements{
		{Modules: []ModuleRequirement{{Name: "Az"}}},
		{RunAsAdministrator: true},
		{PSVersion: "7.0"},
		{PSEditions: []string{"Core"}},
	}
	for _, requirement := range populated {
		if requirement.IsEmpty() {
			t.Errorf("%+v should not be empty", requirement)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
