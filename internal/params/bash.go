package params

import (
	"bufio"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// Bash has no introspection API to borrow the way PowerShell's parser and
// Python's ast module can be. There is no built-in way to ask a shell script
// what options it accepts, so this file reads the text. That makes the accuracy
// rules different from the other languages: where the PowerShell and Python
// harvesters are exact because they ask the toolchain, this one is a careful
// reader that prefers to report nothing over reporting something wrong.
//
// Every rule below is therefore conservative. An option is reported when the
// text states it plainly, and a script whose options cannot be recognized is
// listed with a warning rather than given a form that would send the wrong
// arguments.

// bashExtractor reads options out of shell scripts. It holds no state and needs
// no interpreter, so it is a value rather than a pointer and needs no
// configuration.
type BashExtractor struct{}

// Extract reads the given scripts and returns one report per requested path, in
// the requested order.
func (BashExtractor) Extract(paths []string) ([]Report, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	reports := make([]Report, 0, len(paths))
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		reports = append(reports, extractBashScript(path))
	}
	return reports, nil
}

func extractBashScript(path string) Report {
	report := Report{Path: path, Help: Help{Source: HelpSourceNone}}

	data, err := os.ReadFile(path)
	if err != nil {
		report.AddWarning("cannot read the file: %s", err)
		return report
	}

	lines := logicalLines(string(data))
	synopsis, documented, pairs := usageText(lines)

	params := map[string]*Param{}
	add := func(param *Param) {
		if existing, ok := params[param.Name]; ok {
			// The same option reached by two of the patterns below. A script may
			// handle --config and -c in two separate arms, so the spellings are
			// combined rather than the second one being dropped. The richer
			// description and the more specific kind win, since one pass sees the
			// option list and another sees the arm that handles it.
			for _, alias := range param.Aliases {
				if !slices.Contains(existing.Aliases, alias) {
					existing.Aliases = append(existing.Aliases, alias)
				}
			}
			existing.Aliases = SortedAliases(existing.Aliases)
			if len(param.Help) > len(existing.Help) {
				existing.Help = param.Help
			}
			if param.Kind != KindOther && existing.Kind == KindOther {
				existing.Kind = param.Kind
			}
			return
		}
		params[param.Name] = param
	}

	for _, param := range getoptsParams(lines, documented) {
		add(param)
	}
	for _, param := range caseParams(lines, documented, pairs) {
		add(param)
	}

	// Declaration order is not knowable in a shell script, because options can
	// be declared in any order and used in any order. Sorting by name keeps the
	// form stable between runs, which matters more here than the order the
	// author happened to write.
	names := make([]string, 0, len(params))
	for name := range params {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		report.Params = append(report.Params, *params[name])
	}

	if synopsis != "" {
		report.Help = Help{
			Source:   HelpSourceComment,
			Synopsis: synopsis,
			// Each documented option's own line is its description, so the
			// per-parameter map would duplicate the option rows.
			Notes: usageNotes(lines),
		}
	}

	// A script that parses its own arguments by hand, or shifts them off a
	// variable, has inputs this reader cannot see. Saying so is better than a
	// form that silently omits a required argument.
	if len(report.Params) == 0 {
		report.AddWarning("no options were recognized; this script parses its own " +
			"arguments, so it will run without a form")
	} else if positionals := positionalRefs(lines); positionals > 0 {
		report.AddWarning("this script also takes %d positional argument(s), which are "+
			"not shown", positionals)
	}
	return report
}

// logicalLines splits a script into lines with continuations joined and comments
// removed.
//
// Quotes are tracked so that a `#` inside a string is not mistaken for a
// comment. This is not a shell parser: it does not evaluate anything, and it is
// only ever asked to find patterns that are unambiguous in ordinary scripts.
func logicalLines(source string) []string {
	var out []string
	var current strings.Builder

	scanner := bufio.NewScanner(strings.NewReader(source))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimRight(line, " \t\r")

		// A backslash before the end of the line continues it.
		if strings.HasSuffix(trimmed, "\\") {
			current.WriteString(strings.TrimSuffix(trimmed, "\\"))
			current.WriteString(" ")
			continue
		}

		current.WriteString(stripComment(trimmed))
		out = append(out, current.String())
		current.Reset()
	}
	if current.Len() > 0 {
		out = append(out, current.String())
	}
	return out
}

// stripComment removes a trailing comment from a line.
//
// A # inside a quoted string is left alone, so a default value or a message
// that mentions one is not cut in half. The quote state is deliberately not
// carried between lines: a shell string does not continue across a newline, and
// a here-document's contents are read separately.
func stripComment(line string) string {
	var out strings.Builder
	var quote byte
	for i := 0; i < len(line); i++ {
		char := line[i]
		switch {
		case quote != 0:
			if char == quote {
				quote = 0
			}
			// A backslash escapes the next character inside double quotes.
			if quote == '"' && char == '\\' && i+1 < len(line) {
				out.WriteByte(char)
				i++
				out.WriteByte(line[i])
				continue
			}
		case char == '\'' || char == '"':
			quote = char
		case char == '#' && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t' || line[i-1] == ';'):
			return out.String()
		}
		out.WriteByte(char)
	}
	return out.String()
}

// getoptsPattern matches a getopts call.
//
// The call has to be at a command position. A script that merely mentions
// getopts inside a string, such as an error message quoting the option letters,
// would otherwise be read as though the script accepted those options.
//
// The two quoting styles are captured separately rather than with a
// backreference, because Go's regexp engine has none.
var getoptsPattern = regexp.MustCompile(
	`^\s*(?:(?:while|until|if|elif|then|else|do|time|command|exec|!)\s+)*` +
		`(?:[;|(]\s*)*` +
		`getopts\s+(?:'([^']*)'|"([^"]*)")\s+([A-Za-z_][A-Za-z0-9_]*)`)

// getoptsParams reads options declared with the getopts builtin.
//
// In the optstring, a letter followed by a colon takes a value. A leading colon
// only changes how errors are reported and is not an option.
func getoptsParams(lines []string, documented map[string]string) []*Param {
	var params []*Param
	seen := map[string]bool{}

	for _, line := range lines {
		match := getoptsPattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		optstring := match[1]
		if optstring == "" {
			optstring = match[2]
		}
		// In the optstring a colon belongs to the letter before it, so whether
		// an option takes a value is read by looking ahead rather than by
		// carrying a flag forward. Attributing the colon to the following letter
		// would report every option after the first as taking a value.
		letters := []rune(optstring)
		for i, char := range letters {
			if char == ':' {
				continue
			}
			letter := string(char)
			if letter == " " || seen[letter] {
				continue
			}
			seen[letter] = true
			takesValue := i+1 < len(letters) && letters[i+1] == ':'
			param := &Param{
				Name: letter,
				Help: documented[letter],
			}
			if takesValue {
				param.Kind = KindString
			} else {
				param.Kind = KindBool
			}
			params = append(params, param)
		}
	}
	return params
}

// caseArmPattern matches one arm of a case statement, capturing the label up to
// the closing parenthesis and the body after it. Whether the label is actually a
// set of options is decided by onlySeparators and the option tokens, not here.
var caseArmPattern = regexp.MustCompile(`^\s*([^()]*)\)\s*(.*)$`)

// optionTokenPattern matches one option in an arm label, such as `-v`, `--verbose`
// or `--config=*`.
var optionTokenPattern = regexp.MustCompile(`(-{1,2}[A-Za-z0-9?][A-Za-z0-9_.-]*)(\*|=)?`)

// caseParams reads options declared as the arms of a case statement over the
// arguments, which is the other common way a shell script accepts them.
//
// An arm is only treated as an option arm when its label consists only of option
// tokens and separators. A `case` over anything else is skipped, so a switch on a
// subcommand or a status code cannot be mistaken for a set of switches.
func caseParams(lines []string, documented, pairs map[string]string) []*Param {
	var params []*Param

	for _, line := range lines {
		match := caseArmPattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		label := match[1]
		body := match[2]

		tokens := optionTokenPattern.FindAllStringSubmatch(label, -1)
		if len(tokens) == 0 {
			continue
		}
		// Every gap between the tokens has to be a separator. Anything else,
		// such as a word or a glob, means this is not an option arm.
		if !onlySeparators(label) {
			continue
		}

		var names []string
		var aliases []string
		// `--name=*` and `--name` followed by a reference to the next argument
		// both mean the option takes a value. A bare `shift` is not enough on
		// its own, because a script commonly shifts inside a flag arm too.
		takesValue := false
		for _, token := range tokens {
			token, suffix := token[1], token[2]
			name := strings.TrimLeft(token, "-")
			if name == "" {
				continue
			}
			if suffix == "*" || suffix == "=" {
				takesValue = true
			}
			if strings.HasPrefix(token, "--") {
				names = append(names, name)
			} else {
				aliases = append(aliases, name)
			}
		}
		if takesValue || consumesNextArgument(body) {
			takesValue = true
		}
		if len(names) == 0 && len(aliases) == 0 {
			continue
		}

		// A long option names the parameter; a short-only arm falls back to the
		// letter, which is all the script gives us to go on.
		name := ""
		if len(names) > 0 {
			name = names[0]
			aliases = append(aliases, names[1:]...)
		} else {
			name = aliases[0]
			aliases = aliases[1:]
			// The arm only spelled the option the short way, but a usage line
			// may have said which long option it belongs to. Using that name
			// keeps one input from being shown twice.
			if long, ok := pairs[name]; ok && long != name {
				aliases = append(aliases, name)
				name = long
			}
		}
		param := &Param{
			Name:    name,
			Aliases: SortedAliases(aliases),
			Help:    documented[name],
		}
		if takesValue {
			param.Kind = KindString
		} else {
			param.Kind = KindBool
		}
		params = append(params, param)
	}
	return params
}

// consumesNextArgument reports whether an arm body reads the argument after the
// current one.
func consumesNextArgument(body string) bool {
	return secondArgumentPattern.MatchString(body)
}

// secondArgumentPattern matches a reference to the argument after the current
// one, in each of the forms a shell script writes it.
var secondArgumentPattern = regexp.MustCompile(`\$\{?2\b|\$\{2\}`)

// onlySeparators reports whether the text between option tokens consists only of
// the characters that join them in a case label.
func onlySeparators(label string) bool {
	remainder := optionTokenPattern.ReplaceAllString(label, "")
	for _, char := range remainder {
		switch char {
		case '|', ' ', '\t', ')', ']', '*':
		default:
			return false
		}
	}
	return true
}

// positionalRefs counts the distinct positional parameter references, so a
// script that takes both options and bare arguments can say so.
func positionalRefs(lines []string) int {
	seen := map[string]bool{}
	pattern := regexp.MustCompile(`\$\{?([1-9])\}?`)
	for _, line := range lines {
		// An arm that reads $2 is reading the value belonging to the option it
		// matched, which the caller never supplies positionally. Counting those
		// would report a positional argument the script does not have.
		if caseArmPattern.MatchString(line) {
			continue
		}
		// The subject of a case statement is the argument being dispatched,
		// which is one of the options already reported rather than an extra
		// input. Every option-parsing shell script has one, so counting it
		// would put a warning on nearly all of them and train the user to
		// ignore it.
		if caseSubjectPattern.MatchString(line) {
			continue
		}
		for _, match := range pattern.FindAllStringSubmatch(line, -1) {
			seen[match[1]] = true
		}
		// "$@" and "$*" stand for every remaining argument.
		if strings.Contains(line, `"$@"`) || strings.Contains(line, `"$*"`) {
			seen["all"] = true
		}
	}
	return len(seen)
}

// caseSubjectPattern matches the opening line of a case statement, whose
// references are the argument being dispatched.
var caseSubjectPattern = regexp.MustCompile(`^\s*case\b.*\bin\s*$`)

// heredocPattern matches the start of a here-document, capturing its delimiter.
// The quoting styles are captured separately because Go's regexp engine has no
// backreference.
var heredocPattern = regexp.MustCompile(`<<-?\s*(?:'([A-Za-z_][A-Za-z0-9_]*)'|"([A-Za-z_][A-Za-z0-9_]*)"|([A-Za-z_][A-Za-z0-9_]*))`)

// usageText finds the help a script prints for itself.
//
// A here-document is the reliable place to look: that is where a usage message
// lives, and its contents are the author's own words rather than something
// assembled from fragments. The first line beginning "usage" is taken as the
// summary, and each following line that begins with a dash documents one option.
func usageText(lines []string) (string, map[string]string, map[string]string) {
	documented := map[string]string{}
	pairs := map[string]string{}
	synopsis := ""

	for _, block := range heredocs(lines) {
		for _, line := range block {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			if synopsis == "" && strings.HasPrefix(strings.ToLower(trimmed), "usage") {
				synopsis = trimmed
				continue
			}
			if !strings.HasPrefix(trimmed, "-") {
				continue
			}
			name, shorts, description := splitDocumentedOption(trimmed)
			if name == "" {
				continue
			}
			if _, seen := documented[name]; !seen {
				documented[name] = description
			}
			// A usage line is where a script states that -c and --config are the
			// same option, which is the one place it says so explicitly.
			for _, short := range shorts {
				if _, seen := pairs[short]; !seen {
					pairs[short] = name
				}
			}
		}
	}
	return synopsis, documented, pairs
}

// usageNotes returns the usage text that is not the summary line and not an
// option description, which is the part a reader still wants to see.
func usageNotes(lines []string) string {
	var kept []string
	for _, block := range heredocs(lines) {
		for _, line := range block {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			lower := strings.ToLower(trimmed)
			if strings.HasPrefix(lower, "usage") || strings.HasPrefix(trimmed, "-") {
				continue
			}
			kept = append(kept, trimmed)
		}
	}
	return strings.Join(kept, "\n")
}

// heredocs returns the contents of every here-document in the script.
func heredocs(lines []string) [][]string {
	var blocks [][]string
	for index := 0; index < len(lines); index++ {
		match := heredocPattern.FindStringSubmatch(lines[index])
		if match == nil {
			continue
		}
		delimiter := match[1]
		if delimiter == "" {
			delimiter = match[2]
		}
		if delimiter == "" {
			delimiter = match[3]
		}
		var body []string
		found := false
		for index+1 < len(lines) {
			index++
			if strings.TrimSpace(lines[index]) == delimiter {
				found = true
				break
			}
			body = append(body, lines[index])
		}
		if !found {
			// An unterminated here-document means the rest of the file is not
			// what it looks like, so nothing more is read from it.
			break
		}
		blocks = append(blocks, body)
	}
	return blocks
}

// splitDocumentedOption separates an option's flags from its description in a
// usage line. The separator is a run of spaces, because that is what makes the
// line readable in a terminal.
func splitDocumentedOption(line string) (string, []string, string) {
	fields := regexp.MustCompile(`\s{2,}`).Split(strings.TrimSpace(line), 2)
	if len(fields) == 0 {
		return "", nil, ""
	}
	description := ""
	if len(fields) == 2 {
		description = strings.TrimSpace(fields[1])
	}

	// The first field holds every spelling of the option, separated by commas
	// or pipes. The longest one is the parameter's name, because a short letter
	// alone says less than the long form does.
	tokens := optionTokenPattern.FindAllString(fields[0], -1)
	best := ""
	var shorts []string
	for _, token := range tokens {
		name := strings.TrimLeft(token, "-")
		if name == "" {
			continue
		}
		if strings.HasPrefix(token, "--") {
			if len(name) > len(best) {
				best = name
			}
		} else {
			shorts = append(shorts, name)
		}
	}
	if best == "" {
		for _, short := range shorts {
			if len(short) > len(best) {
				best = short
			}
		}
	}
	return best, shorts, description
}
