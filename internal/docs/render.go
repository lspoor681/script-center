package docs

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
)

// ErrUnsupportedFormat reports a document whose extension has no known renderer.
var ErrUnsupportedFormat = errors.New("docs: unsupported document format")

// ErrDocumentTooLarge reports a file too big to be worth rendering. Documentation
// is prose, so a multi-megabyte candidate is a sign that something other than
// documentation was found.
var ErrDocumentTooLarge = errors.New("docs: document is too large to render")

// maxDocumentBytes bounds how much of a file is rendered.
const maxDocumentBytes = 4 << 20 // 4 MiB

// codeClassPattern matches only the class names the markdown extensions emit. A
// document that names some other class could otherwise borrow styling from the
// application's own stylesheet.
var codeClassPattern = regexp.MustCompile(`^(language-[a-zA-Z0-9_+#.-]+|chroma|chroma-[a-zA-Z0-9_-]+)$`)

// headingIDPattern matches the identifier goldmark generates for a heading, which
// a documentation file may use to link to its own sections.
var headingIDPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_:.-]*$`)

// renderer and policy are built once. Both are safe for concurrent use, and
// constructing a goldmark instance walks every extension, which is far too much
// work to repeat for each script in a list.
var (
	rendererOnce sync.Once
	renderer     goldmark.Markdown
	policy       *bluemonday.Policy
)

func initRendering() {
	renderer = goldmark.New(
		goldmark.WithExtensions(
			// Documentation uses tables and strikethrough, and a README written
			// on a forge will use both without saying so.
			extension.Table,
			extension.Strikethrough,
			extension.Linkify,
		),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
		// Raw HTML is deliberately not enabled. A README is untrusted input
		// chosen by walking a directory tree, and letting it emit markup the
		// renderer did not produce is how documentation becomes a script
		// injection. goldmark drops raw HTML, and the sanitizer below is a second
		// line of defense rather than the only one.
	)

	policy = bluemonday.UGCPolicy()
	policy.AllowTables()
	policy.AllowLists()
	policy.AllowElements(
		"h1", "h2", "h3", "h4", "h5", "h6",
		"p", "br", "hr", "pre", "code", "blockquote",
		"em", "strong", "del", "sup", "sub",
	)
	policy.AllowAttrs("class").Matching(codeClassPattern).
		OnElements("code", "pre", "span", "div", "table")
	policy.AllowAttrs("id").Matching(headingIDPattern).
		OnElements("h1", "h2", "h3", "h4", "h5", "h6")
	policy.AllowAttrs("href", "title").OnElements("a")
	policy.AllowAttrs("align").OnElements("td", "th")
	// A documentation link resolves inside the document. Allowing an
	// application navigation from a README would let a cloned repository decide
	// where the user ends up.
	policy.AllowRelativeURLs(true)
	policy.RequireNoFollowOnLinks(true)
}

// Render turns a documentation file into HTML that is safe to insert into the
// application's page. The extension selects the renderer and is matched without
// regard to case, with or without its leading dot.
func Render(extension string, data []byte) (string, error) {
	if len(data) > maxDocumentBytes {
		return "", ErrDocumentTooLarge
	}
	switch strings.ToLower(strings.TrimPrefix(extension, ".")) {
	case "md", "markdown", "":
		return renderMarkdown(data)
	case "txt", "text", "rst":
		return renderPlainText(data)
	default:
		return "", ErrUnsupportedFormat
	}
}

// renderMarkdown converts markdown and then sanitizes the result.
func renderMarkdown(data []byte) (string, error) {
	rendererOnce.Do(initRendering)
	var rendered bytes.Buffer
	if err := renderer.Convert(data, &rendered); err != nil {
		return "", err
	}
	return policy.Sanitize(rendered.String()), nil
}

// renderPlainText keeps text as a preformatted block. Documentation written as
// plain text is common in operations repositories, and running its lines together
// into one paragraph would destroy the alignment that makes such a file readable.
func renderPlainText(data []byte) (string, error) {
	rendererOnce.Do(initRendering)
	var escaped bytes.Buffer
	escaped.Grow(len(data))
	for _, b := range data {
		switch b {
		case '&':
			escaped.WriteString("&amp;")
		case '<':
			escaped.WriteString("&lt;")
		case '>':
			escaped.WriteString("&gt;")
		default:
			escaped.WriteByte(b)
		}
	}
	return policy.Sanitize("<pre><code>" + escaped.String() + "</code></pre>"), nil
}

// RenderFile renders a document from disk, choosing the renderer by extension.
func RenderFile(path string) (string, error) {
	// The size is checked before the read so that an oversized file is never
	// pulled into memory only to be truncated.
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.Size() > maxDocumentBytes {
		return "", ErrDocumentTooLarge
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return Render(filepath.Ext(path), data)
}
