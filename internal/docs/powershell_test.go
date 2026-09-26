package docs

import (
	"strings"
	"testing"

	"github.com/lspoor/script-center/internal/params"
)

func TestParsePowerShellHelpBlockAtEndOfFile(t *testing.T) {
	// The case this package exists for: Get-Help ignores a help block that does
	// not precede the param block, so without this the script has no
	// documentation at all.
	script := `[CmdletBinding()]
param(
    [string]$Server,
    [int]$Count
)
<#
.SYNOPSIS
A script whose help sits after the param block.
.DESCRIPTION
Get-Help ignores this block.
.PARAMETER Server
The target server.
.PARAMETER Count
How many rounds.
.NOTES
Recovered from the raw text instead.
.LINK
https://example.com/runbook
#>
Write-Host $Server
`
	help, ok := ParsePowerShellHelpBlock([]byte(script))
	if !ok {
		t.Fatal("want a help block to be found, got none")
	}
	if help.Source != params.HelpSourceComment {
		t.Errorf("Source = %q, want %q", help.Source, params.HelpSourceComment)
	}
	if help.Synopsis != "A script whose help sits after the param block." {
		t.Errorf("Synopsis = %q", help.Synopsis)
	}
	if help.Description != "Get-Help ignores this block." {
		t.Errorf("Description = %q", help.Description)
	}
	if help.Notes != "Recovered from the raw text instead." {
		t.Errorf("Notes = %q", help.Notes)
	}
	if len(help.Links) != 1 || help.Links[0] != "https://example.com/runbook" {
		t.Errorf("Links = %v", help.Links)
	}
	if got := help.Params["Server"]; got != "The target server." {
		t.Errorf("Params[Server] = %q", got)
	}
	if got := help.Params["Count"]; got != "How many rounds." {
		t.Errorf("Params[Count] = %q", got)
	}
}

func TestParsePowerShellHelpBlockAtTopOfFile(t *testing.T) {
	script := `<#
.SYNOPSIS
Reads a manifest.
.PARAMETER Path
Where the manifest lives.
#>
param([string]$Path)
`
	help, ok := ParsePowerShellHelpBlock([]byte(script))
	if !ok {
		t.Fatal("want a help block to be found, got none")
	}
	if help.Synopsis != "Reads a manifest." {
		t.Errorf("Synopsis = %q", help.Synopsis)
	}
	if help.Params["Path"] != "Where the manifest lives." {
		t.Errorf("Params[Path] = %q", help.Params["Path"])
	}
}

func TestParsePowerShellHelpBlockNotFound(t *testing.T) {
	tests := map[string]string{
		"no comment at all":     "param([string]$A)\nWrite-Host $A\n",
		"only line comments":    "# just a note\nparam([string]$A)\n",
		"block without a tag":   "<#\njust prose, not help\n#>\nparam([string]$A)\n",
		"empty":                 "",
		"dot in prose is a tag": "param([string]$A)\n# run .Get-ChildItem first\n",
	}
	for name, script := range tests {
		t.Run(name, func(t *testing.T) {
			help, ok := ParsePowerShellHelpBlock([]byte(script))
			if ok {
				t.Errorf("want no help block, got %+v", help)
			}
			// A false return must carry the none source so a caller cannot
			// mistake the empty result for documentation.
			if help.Source != params.HelpSourceNone {
				t.Errorf("Source = %q, want %q", help.Source, params.HelpSourceNone)
			}
		})
	}
}

func TestParsePowerShellHelpBlockMultiLine(t *testing.T) {
	script := `<#
.SYNOPSIS
One line synopsis.
.DESCRIPTION
First paragraph.

Second paragraph after a blank line.
.NOTES
Note one.
Note two.
.INPUTS
System.String.
.OUTPUTS
None.
#>
param()
`
	help, ok := ParsePowerShellHelpBlock([]byte(script))
	if !ok {
		t.Fatal("want a help block")
	}
	if want := "First paragraph.\n\nSecond paragraph after a blank line."; help.Description != want {
		t.Errorf("Description = %q, want %q", help.Description, want)
	}
	if want := "Note one.\nNote two."; help.Notes != want {
		t.Errorf("Notes = %q, want %q", help.Notes, want)
	}
	if help.InputTypes != "System.String." {
		t.Errorf("InputTypes = %q", help.InputTypes)
	}
	if help.OutputTypes != "None." {
		t.Errorf("OutputTypes = %q", help.OutputTypes)
	}
}

func TestParsePowerShellHelpBlockPicksTheTaggedBlock(t *testing.T) {
	// A script may hold several block comments and only one of them is help.
	script := `<#
A license header that happens to use block comments.
#>
<#
.SYNOPSIS
The real help.
#>
param()
`
	help, ok := ParsePowerShellHelpBlock([]byte(script))
	if !ok {
		t.Fatal("want the tagged help block to be found")
	}
	if help.Synopsis != "The real help." {
		t.Errorf("Synopsis = %q, want the tagged block rather than the first one", help.Synopsis)
	}
}

func TestParsePowerShellHelpBlockUnknownTagKeepsProse(t *testing.T) {
	// A tag this package does not know about must not lose the author's words.
	script := `<#
.SYNOPSIS
Has a future tag.
.COMPONENT
Some component.
#>
param()
`
	help, ok := ParsePowerShellHelpBlock([]byte(script))
	if !ok {
		t.Fatal("want a help block")
	}
	if !strings.Contains(help.Description, "Some component.") {
		t.Errorf("Description = %q, want the unknown tag's prose kept", help.Description)
	}
}

func TestParsePowerShellHelpBlockExamples(t *testing.T) {
	script := `<#
.SYNOPSIS
Has examples.
.EXAMPLE
PS> .\run.ps1 -Server dc01
Rebuilds the index.
.EXAMPLE
A quick partial run
PS> .\run.ps1 -Mode Partial
#>
param()
`
	help, ok := ParsePowerShellHelpBlock([]byte(script))
	if !ok {
		t.Fatal("want a help block")
	}
	if len(help.Examples) != 2 {
		t.Fatalf("got %d examples, want 2: %+v", len(help.Examples), help.Examples)
	}
	// A bare invocation stays a command, with no invented heading.
	if help.Examples[0].Title != "" {
		t.Errorf("first example title = %q, want empty", help.Examples[0].Title)
	}
	if !strings.Contains(help.Examples[0].Command, "-Server dc01") {
		t.Errorf("first example command = %q", help.Examples[0].Command)
	}
	// A prose first line is treated as the heading, because an example is
	// usually just a command.
	if help.Examples[1].Title != "A quick partial run" {
		t.Errorf("second example title = %q", help.Examples[1].Title)
	}
	if !strings.Contains(help.Examples[1].Command, "-Mode Partial") {
		t.Errorf("second example command = %q", help.Examples[1].Command)
	}
}

func TestParsePowerShellHelpBlockLinks(t *testing.T) {
	script := `<#
.SYNOPSIS
Has links.
.LINK
https://example.com/one
https://example.com/two
not a link at all
.LINK
https://example.com/one
#>
param()
`
	help, ok := ParsePowerShellHelpBlock([]byte(script))
	if !ok {
		t.Fatal("want a help block")
	}
	want := []string{"https://example.com/one", "https://example.com/two"}
	if len(help.Links) != len(want) {
		t.Fatalf("Links = %v, want %v", help.Links, want)
	}
	for i := range want {
		if help.Links[i] != want[i] {
			t.Errorf("Links[%d] = %q, want %q", i, help.Links[i], want[i])
		}
	}
}

func TestParsePowerShellHelpBlockParameterWithoutProse(t *testing.T) {
	script := `<#
.SYNOPSIS
Documents a parameter with no prose.
.PARAMETER Orphan
#>
param([string]$Orphan)
`
	help, ok := ParsePowerShellHelpBlock([]byte(script))
	if !ok {
		t.Fatal("want a help block")
	}
	// An entry with no text would render as a blank description row.
	if _, present := help.Params["Orphan"]; present {
		t.Errorf("Params = %v, want no entry for an undocumented parameter", help.Params)
	}
}

func TestParsePowerShellHelpBlockIndented(t *testing.T) {
	// Help inside a function is indented, and the prose should come back clean.
	script := `function Invoke-Thing {
    <#
    .SYNOPSIS
    Does the thing.
    .DESCRIPTION
    A description that is indented in the source.
    #>
    param()
}
`
	help, ok := ParsePowerShellHelpBlock([]byte(script))
	if !ok {
		t.Fatal("want a help block")
	}
	if help.Synopsis != "Does the thing." {
		t.Errorf("Synopsis = %q", help.Synopsis)
	}
	if help.Description != "A description that is indented in the source." {
		t.Errorf("Description = %q", help.Description)
	}
}

func TestParsePowerShellHelpBlockCRLF(t *testing.T) {
	script := "<#\r\n.SYNOPSIS\r\nWindows line endings.\r\n#>\r\nparam()\r\n"
	help, ok := ParsePowerShellHelpBlock([]byte(script))
	if !ok {
		t.Fatal("want a help block")
	}
	if help.Synopsis != "Windows line endings." {
		t.Errorf("Synopsis = %q", help.Synopsis)
	}
}

func TestParsePowerShellHelpBlockLongLine(t *testing.T) {
	// The scanner has a buffer limit, and a pasted stack trace inside a help
	// block is long enough to reach it.
	long := strings.Repeat("x", 200*1024)
	script := "<#\n.SYNOPSIS\n" + long + "\n#>\nparam()\n"
	help, ok := ParsePowerShellHelpBlock([]byte(script))
	if !ok {
		t.Fatal("want a help block")
	}
	if len(help.Synopsis) != len(long) {
		t.Errorf("Synopsis length = %d, want %d", len(help.Synopsis), len(long))
	}
}

func TestLooksLikeCommand(t *testing.T) {
	commands := []string{
		"PS> .\\run.ps1",
		"./run.ps1",
		"$env:PATH",
		"Get-ChildItem -Recurse",
		"invoke-restmethod https://example.com",
		"# a commented command",
		"C:\\scripts\\run.ps1",
		"& 'C:\\run.ps1'",
	}
	for _, line := range commands {
		if !looksLikeCommand(line) {
			t.Errorf("looksLikeCommand(%q) = false, want true", line)
		}
	}
	headings := []string{
		"A quick partial run",
		"Rebuilds",
		"This is the slow path",
	}
	for _, line := range headings {
		if looksLikeCommand(line) {
			t.Errorf("looksLikeCommand(%q) = true, want false", line)
		}
	}
}

func TestIsAbsoluteURI(t *testing.T) {
	links := []string{
		"https://example.com/a",
		"http://example.com",
		"ftp://files.example.com/x",
		"HTTPS://EXAMPLE.COM",
	}
	for _, link := range links {
		if !isAbsoluteURI(link) {
			t.Errorf("isAbsoluteURI(%q) = false, want true", link)
		}
	}
	// Anything that is not a web or file URI is left as plain text rather than
	// becoming a link the application might follow unexpectedly.
	notLinks := []string{
		"example.com",
		"/relative/path",
		"mailto:someone@example.com",
		"javascript:alert(1)",
		"not a uri at all",
		"",
	}
	for _, candidate := range notLinks {
		if isAbsoluteURI(candidate) {
			t.Errorf("isAbsoluteURI(%q) = true, want false", candidate)
		}
	}
}

func TestDedupe(t *testing.T) {
	if got := dedupe(nil); got != nil {
		t.Errorf("dedupe(nil) = %v, want nil", got)
	}
	if got := dedupe([]string{"", "  "}); got != nil {
		t.Errorf("dedupe of blanks = %v, want nil", got)
	}
	got := dedupe([]string{"b", "a", "b", "", "a", "c"})
	want := []string{"b", "a", "c"}
	if len(got) != len(want) {
		t.Fatalf("dedupe = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("dedupe[%d] = %q, want %q: first position must be kept", i, got[i], want[i])
		}
	}
}

func TestDedent(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"no indent", "a\nb", "a\nb"},
		{"uniform indent", "    a\n    b", "a\nb"},
		{"mixed indent", "    a\n      b\n    c", "a\n  b\nc"},
		{"blank lines ignored", "    a\n\n    b", "a\n\nb"},
		{"all blank", "   \n  ", "   \n  "},
		{"tab indent", "\ta\n\tb", "a\nb"},
		// The smallest indent sets the common prefix, so a line indented less
		// than the rest keeps its difference rather than being flattened.
		{"shorter trailing line", "    a\n  b", "  a\nb"},
		{"empty", "", ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := dedent(test.input); got != test.want {
				t.Errorf("dedent(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

func TestSplitFirstWord(t *testing.T) {
	tests := []struct {
		argument string
		wantName string
		wantRest string
	}{
		{"Server", "Server", ""},
		{"Server The target server.", "Server", "The target server."},
		{"  Count   How many rounds.  ", "Count", "How many rounds."},
		{"", "", ""},
		{"   ", "", ""},
	}
	for _, test := range tests {
		name, rest := splitFirstWord(test.argument)
		if name != test.wantName || rest != test.wantRest {
			t.Errorf("splitFirstWord(%q) = %q, %q; want %q, %q",
				test.argument, name, rest, test.wantName, test.wantRest)
		}
	}
}
