// Package docs finds and renders the documentation that explains a script.
//
// Three sources feed the same result. A script's own help, recovered from
// wherever it happens to sit in the file. A sidecar file next to the script. And
// a README in the script's directory or an ancestor of it. They are merged in
// that order because the further a document is from the script, the more likely
// it is to be about the project rather than the script.
//
// The recovery exists because the toolchain's own help reader is strict about
// placement. PowerShell attaches a comment-based help block to a script only when
// the block is the first or the last thing in the file; one sitting between the
// param block and a statement that follows it is ignored, and PowerShell then
// invents a synopsis out of the parameter list. A script with a real, detailed
// help block would then be shown to the user as though it had none.
package docs

import (
	"bufio"
	"regexp"
	"strings"

	"github.com/lspoor/script-center/internal/params"
)

// powerShellBlockPattern matches a block comment. PowerShell help lives in these,
// and they are the only comment form that can span lines.
var powerShellBlockPattern = regexp.MustCompile(`(?s)<#.*?#>`)

// tagLinePattern matches a help tag such as .SYNOPSIS, .PARAMETER Server, or
// .LINK. A tag is a dot, a word, and optionally the tag's argument on the same
// line. The word boundary keeps .PARAMETERIZED prose from being read as a tag.
var tagLinePattern = regexp.MustCompile(`^\s*\.[ \t]*([A-Za-z][A-Za-z0-9]*)[ \t]*(.*)$`)

// ParsePowerShellHelpBlock recovers comment-based help from a PowerShell file
// regardless of where the block sits.
//
// This exists because Get-Help only reads a help block that precedes the param
// block. Scripts that put their help at the end, after the code they document,
// are common enough that ignoring them would leave a large share of real scripts
// with no explanation at all. Get-Help remains the primary reader because it
// implements the full format; this is the fallback for the case it declines.
//
// The second return value reports whether a help block was found. False means
// the file has no comment-based help and the caller should not present anything
// in the returned help as the author's words.
func ParsePowerShellHelpBlock(text []byte) (params.Help, bool) {
	help := params.Help{Source: params.HelpSourceComment}

	block, ok := firstHelpBlock(text)
	if !ok {
		return params.Help{Source: params.HelpSourceNone}, false
	}

	sections := splitHelpSections(block)
	if len(sections) == 0 {
		return params.Help{Source: params.HelpSourceNone}, false
	}

	paramHelp := map[string]string{}
	var examples []params.Example
	var links []string

	for _, section := range sections {
		content := section.content
		switch section.tag {
		case "synopsis":
			if help.Synopsis == "" {
				help.Synopsis = content
			}
		case "description":
			help.Description = appendSection(help.Description, content)
		case "parameter":
			// The name is the tag's argument and the prose is the block that
			// follows it, so a description spanning several lines survives.
			name, _ := splitFirstWord(section.argument)
			if name == "" {
				continue
			}
			// A parameter documented twice keeps its first description, matching
			// how a reader takes the first statement as the real one.
			if existing, seen := paramHelp[name]; seen && existing != "" {
				continue
			}
			paramHelp[name] = content
		case "example":
			// Examples are kept verbatim. The user decided that examples are
			// shown read-only, so there is nothing to gain from splitting an
			// example into a title and a command and a lot to lose from
			// presenting a guess as the author's structure.
			examples = append(examples, params.Example{Command: content})
		case "notes":
			help.Notes = appendSection(help.Notes, content)
		case "link":
			if content != "" {
				links = append(links, splitLinks(content)...)
			}
		case "inputs":
			help.InputTypes = appendSection(help.InputTypes, content)
		case "outputs":
			help.OutputTypes = appendSection(help.OutputTypes, content)
		default:
			// An unrecognized tag is prose the author wrote. Keeping it in the
			// description is better than dropping it.
			help.Description = appendSection(help.Description, content)
		}
	}

	// An example may carry its own heading line above the command. It is only
	// treated as a title when the first line is clearly not a command, because
	// an example is usually just the command.
	for i := range examples {
		title, command := splitExampleHeading(examples[i].Command)
		examples[i].Title = title
		examples[i].Command = command
	}

	if len(paramHelp) > 0 {
		// Drop entries whose prose is empty so the UI does not render a blank
		// description row.
		described := make(map[string]string, len(paramHelp))
		for name, prose := range paramHelp {
			if prose != "" {
				described[name] = prose
			}
		}
		if len(described) > 0 {
			help.Params = described
		}
	}
	if len(links) > 0 {
		help.Links = dedupe(links)
	}
	if len(examples) > 0 {
		help.Examples = examples
	}

	// A block whose tags are all empty says nothing, and reporting it as
	// documentation would put a blank panel in front of the user.
	if help.Synopsis == "" && help.Description == "" && help.Notes == "" &&
		help.InputTypes == "" && help.OutputTypes == "" && len(help.Links) == 0 &&
		len(help.Examples) == 0 && len(help.Params) == 0 {
		return params.Help{Source: params.HelpSourceNone}, false
	}
	return help, true
}

// firstHelpBlock returns the contents of the first block comment that carries a
// recognized help tag. A script may contain several block comments and only one
// of them is documentation.
func firstHelpBlock(text []byte) (string, bool) {
	for _, match := range powerShellBlockPattern.FindAllString(string(text), -1) {
		body := strings.TrimSuffix(strings.TrimPrefix(match, "<#"), "#>")
		if !strings.Contains(strings.ToUpper(body), ".SYNOPSIS") {
			continue
		}
		// The block is returned with its common indentation removed so that a
		// help block indented inside a function still yields clean prose.
		return dedent(body), true
	}
	return "", false
}

// section is one tag and the prose that followed it.
type section struct {
	tag      string
	argument string
	content  string
}

// splitHelpSections reads a help block into tagged sections. Content runs from a
// tag line to the line before the next tag, which is the whole of the format's
// structure and does not require tracking indentation.
func splitHelpSections(block string) []section {
	var sections []section
	var current *section
	var lines []string

	flush := func() {
		if current == nil {
			return
		}
		current.content = strings.TrimSpace(strings.Join(lines, "\n"))
		sections = append(sections, *current)
		lines = nil
	}

	scanner := bufio.NewScanner(strings.NewReader(block))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if match := tagLinePattern.FindStringSubmatch(line); match != nil {
			// A line that begins with a dot followed by a word is a tag, even when
			// this package does not recognize it, so that prose under a tag added
			// by a future PowerShell is kept rather than lost. Requiring the dot
			// to start the line is what keeps prose such as "run .Get-ChildItem
			// first" intact, and a path like ./configure does not match because
			// no word follows the dot.
			flush()
			current = &section{
				tag:      strings.ToLower(match[1]),
				argument: strings.TrimSpace(match[2]),
			}
			continue
		}
		if current != nil {
			lines = append(lines, line)
		}
	}
	flush()

	return sections
}

// appendSection joins two pieces of prose with a blank line between them, which
// is how a reader expects separate paragraphs of a help block to be separated.
func appendSection(existing, addition string) string {
	switch {
	case addition == "":
		return existing
	case existing == "":
		return addition
	default:
		return existing + "\n\n" + addition
	}
}

// splitFirstWord separates a tag's argument into its first word and the rest.
func splitFirstWord(argument string) (string, string) {
	argument = strings.TrimSpace(argument)
	if argument == "" {
		return "", ""
	}
	index := strings.IndexAny(argument, " \t")
	if index < 0 {
		return argument, ""
	}
	return argument[:index], strings.TrimSpace(argument[index+1:])
}

// splitLinks breaks a .LINK section into individual targets. Authors write one
// link per line, but a wrapped URL or a bare sentence after it must not be turned
// into a broken link, so only the first word of each line is considered and only
// when it looks like a URI.
func splitLinks(content string) []string {
	var links []string
	for _, line := range strings.Split(content, "\n") {
		candidate := strings.TrimSpace(line)
		if candidate == "" {
			continue
		}
		if fields := strings.Fields(candidate); len(fields) > 0 {
			candidate = fields[0]
		}
		if isAbsoluteURI(candidate) {
			links = append(links, candidate)
		}
	}
	return links
}

// uriPattern matches an absolute URI with a scheme a reader would click.
var uriPattern = regexp.MustCompile(`^(?i)(https?|ftps?)://\S+$`)

// isAbsoluteURI reports whether a string is worth rendering as a link. Scheme
// relative and mailto targets are excluded on purpose: the help is shown inside
// the application, where an unexpected navigation is worse than a plain line of
// text the user can still copy.
func isAbsoluteURI(candidate string) bool {
	return uriPattern.MatchString(candidate)
}

// splitExampleHeading separates an example's optional heading from its command.
// The decision is made from the second line rather than the first: a heading is
// only a heading when the line after it is a command. Judging the first line on
// its own is unreliable, because a heading and a bare invocation are both short
// lines of ordinary words.
func splitExampleHeading(content string) (string, string) {
	lines := strings.Split(content, "\n")
	if len(lines) < 2 {
		return "", content
	}
	first := strings.TrimSpace(lines[0])
	second := strings.TrimSpace(lines[1])
	if first == "" || looksLikeCommand(first) {
		return "", content
	}
	if second == "" || !looksLikeCommand(second) {
		return "", content
	}
	return first, strings.TrimSpace(strings.Join(lines[1:], "\n"))
}

// commandStarters are the things an example's first line commonly begins with.
// A line beginning with any of them is a command, not a heading.
var commandStarters = []string{
	"./", "../", "/", "~/",
	".\\", "..\\", "\\",
	"$", "&", "'", "\"",
	"#", "PS", "PS>", "C:\\", "C:/",
}

// cmdletPattern matches a PowerShell verb-noun name such as Get-ChildItem or
// invoke-restmethod, and executablePattern matches a lower-case command with
// arguments such as python3 script.py. Together they separate an invocation from
// a prose heading, which is otherwise the same shape: both are words followed by
// spaces. A heading is capitalised and unhyphenated, which is what these leave
// out.
var (
	cmdletPattern     = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*-`)
	executablePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]*[ \t]+\S`)
)

// looksLikeCommand reports whether a line reads as an invocation rather than a
// heading. It is deliberately conservative, because guessing wrong here moves a
// line of the author's example into a heading field and back.
func looksLikeCommand(line string) bool {
	upper := strings.ToUpper(line)
	for _, starter := range commandStarters {
		if strings.HasPrefix(line, starter) || strings.HasPrefix(upper, starter) {
			return true
		}
	}
	return cmdletPattern.MatchString(line) || executablePattern.MatchString(line)
}

// dedent removes the common leading whitespace from a block of lines.
func dedent(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	common := -1
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		if common < 0 || indent < common {
			common = indent
		}
	}
	if common <= 0 {
		return text
	}
	for i, line := range lines {
		if len(line) >= common {
			lines[i] = line[common:]
		} else {
			lines[i] = strings.TrimLeft(line, " \t")
		}
	}
	return strings.Join(lines, "\n")
}

// dedupe removes repeated entries while keeping the first occurrence's position,
// so that a document listing the same link twice still shows it once.
func dedupe(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, value := range in {
		// Blanks are dropped rather than kept, because a list holding only
		// whitespace would otherwise read as "this document has a link" while
		// rendering nothing.
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
