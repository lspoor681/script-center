package docs

import (
	"os"
	"path/filepath"
	"strings"
)

// Kind says how a document relates to the script it was found for.
type Kind string

// The kinds of document.
const (
	// KindSidecar is a file written about this one script, sitting beside it.
	// It is the most specific source and the most likely to be current.
	KindSidecar Kind = "sidecar"
	// KindReadme is a project readme found in the script's directory or an
	// ancestor. It describes the project, so it is the least specific source.
	KindReadme Kind = "readme"
)

// Document is one documentation file that explains a script.
type Document struct {
	// Path is the absolute path of the file.
	Path string
	// Kind says whether the file is about this script alone or its project.
	Kind Kind
	// Title is a heading to show for the document, taken from the first
	// markdown heading when there is one and from the file name otherwise.
	Title string
	// Section is the part of a project readme that is about this script, when a
	// heading matching the script was found. It is empty when the whole file
	// applies, which is the common case for a readme in a scripts directory.
	Section string
	// Excerpt reports whether Section is non-empty.
	Excerpted bool
}

// MaxReadmeDepth bounds how far up the tree a readme is looked for. A readme in
// a parent of the workspace root is describing a different project, and showing
// it would be worse than showing nothing.
const MaxReadmeDepth = 4

// readmeNames are the file names tried at each level, in order of preference. The
// list is not exhaustive on purpose: an exhaustive one would pick up CHANGELOG and
// LICENSE, which are files a script listing should never offer as instructions.
var readmeNames = []string{
	"README.md",
	"readme.md",
	"Readme.md",
	"README.markdown",
	"README.rst",
	"README.txt",
	"README",
}

// sidecarNames builds the file names that would document one script, given the
// script's own name. The stem is what remains after removing the extension, so
// rebuild.ps1 is documented by rebuild.md.
func sidecarNames(stem string) []string {
	names := []string{
		stem + ".md",
		stem + ".markdown",
		stem + ".txt",
		stem + ".rst",
		stem + ".help.md",
	}
	// A docs directory beside the script is a common enough convention to be
	// worth one extra stat per candidate.
	return append(names, filepath.Join("docs", stem+".md"), filepath.Join("docs", stem+".txt"))
}

// Discover finds the documents that explain the script at scriptPath. root bounds
// the search for project readmes; pass the workspace root.
//
// Results are ordered most specific first, so a caller can show the first document
// as the primary explanation and offer the rest as further reading.
func Discover(scriptPath, root string) ([]Document, error) {
	dir := filepath.Dir(scriptPath)
	stem := strings.TrimSuffix(filepath.Base(scriptPath), filepath.Ext(scriptPath))
	if stem == "" {
		stem = filepath.Base(scriptPath)
	}

	var documents []Document
	seen := map[string]bool{}

	add := func(path string, kind Kind) {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return
		}
		if seen[absolute] {
			return
		}
		info, err := os.Stat(absolute)
		if err != nil || info.IsDir() {
			return
		}
		seen[absolute] = true
		document := Document{
			Path: absolute,
			Kind: kind,
			// The file name is a usable title on its own, and is the fallback
			// when the file turns out to have no heading to use.
			Title: strings.TrimSuffix(filepath.Base(absolute), filepath.Ext(absolute)),
		}
		if content, err := os.ReadFile(absolute); err == nil {
			text := string(content)
			// Only a project readme is excerpted. A sidecar is written about
			// this one script, so cutting a section out of it would discard the
			// very documentation that was found for it.
			if section, ok := extractSectionFrom(text, stem); ok && kind == KindReadme {
				document.Section = section
				document.Excerpted = true
				if heading := firstHeading(section); heading != "" {
					document.Title = heading
				}
			} else if heading := firstHeading(text); heading != "" {
				// Either the whole document applies, or it is a sidecar. Either
				// way its own first heading is the best label for it.
				document.Title = heading
			}
		}
		documents = append(documents, document)
	}

	for _, name := range sidecarNames(stem) {
		add(filepath.Join(dir, name), KindSidecar)
	}

	// A readme in the script's own directory is the closest project document.
	// The walk stops at root so that a parent of the workspace is never reached.
	current := dir
	for depth := 0; depth <= MaxReadmeDepth; depth++ {
		for _, name := range readmeNames {
			add(filepath.Join(current, name), KindReadme)
		}
		if current == root {
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}

	// The order is already most specific first: sidecars in their preference
	// order, then readmes from the script's own directory upwards. It is left
	// unsorted because sorting by path would put the project readme ahead of the
	// one in the script's own directory, which is the less relevant of the two.
	return documents, nil
}

// extractSectionFrom returns the part of a markdown document that is about the
// given script, when a heading names it. A heading that is not there means the
// whole file applies, which is the normal case.
func extractSectionFrom(text, stem string) (string, bool) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")

	normalized := normalizeHeading(stem)
	if normalized == "" {
		return "", false
	}

	start, level := -1, 0
	for i, line := range lines {
		heading, depth, ok := parseHeading(line)
		if !ok {
			continue
		}
		if start < 0 {
			if normalizeHeading(heading) == normalized {
				start, level = i, depth
			}
			continue
		}
		// The section runs until the next heading of the same or higher level.
		if depth <= level {
			return strings.Join(lines[start:i], "\n"), true
		}
	}
	if start >= 0 {
		return strings.Join(lines[start:], "\n"), true
	}
	return "", false
}

// parseHeading returns the text and level of a markdown ATX heading.
func parseHeading(line string) (string, int, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "#") {
		return "", 0, false
	}
	level := 0
	for level < len(trimmed) && trimmed[level] == '#' {
		level++
	}
	if level > 6 {
		return "", 0, false
	}
	rest := trimmed[level:]
	if rest == "" || (rest[0] != ' ' && rest[0] != '\t') {
		// A line of hashes with no text is not a heading, and neither is "#tag".
		return "", 0, false
	}
	return strings.TrimSpace(strings.TrimRight(rest, "# \t")), level, true
}

// normalizeHeading reduces a heading or a file name to a comparable form, so that
// "Rebuild-Index", "rebuild_index", and "rebuild index" all match the script
// rebuild-index.ps1.
func normalizeHeading(text string) string {
	text = filepath.Base(strings.TrimSpace(text))
	text = strings.TrimSuffix(text, filepath.Ext(text))
	var builder strings.Builder
	lastUnderscore := false
	for _, r := range strings.ToLower(text) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			builder.WriteRune(r)
			lastUnderscore = false
		default:
			// Every run of separators collapses to one underscore so that
			// "rebuild - index" and "rebuild_index" are the same heading.
			if !lastUnderscore {
				builder.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	return strings.Trim(builder.String(), "_")
}

// firstHeading returns the text of a markdown file's first heading.
func firstHeading(markdown string) string {
	for _, line := range strings.Split(markdown, "\n") {
		if heading, _, ok := parseHeading(line); ok {
			return heading
		}
	}
	return ""
}
