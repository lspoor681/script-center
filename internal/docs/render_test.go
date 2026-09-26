package docs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRenderRejectsExecutableMarkup is the security-relevant test in this file.
// The file being rendered is found by walking a directory tree, so it may be a
// README from a repository the user cloned moments ago. Nothing in it may reach
// the application's page as markup that can run.
func TestRenderRejectsExecutableMarkup(t *testing.T) {
	tests := map[string]struct {
		input   string
		absent  []string
		present []string
	}{
		"script element": {
			input:   "# Title\n\n<script>alert('xss')</script>\n\nAfter.\n",
			absent:  []string{"<script", "alert("},
			present: []string{"After."},
		},
		"image event handler": {
			input:  "<img src=x onerror=alert(1)>\n\ntext\n",
			absent: []string{"onerror", "<img"},
		},
		"javascript link": {
			input:  "[click](javascript:alert(1))\n",
			absent: []string{"javascript:"},
			// The visible text is kept, so the reader still sees what the author
			// wrote even though the link cannot be followed.
			present: []string{"click"},
		},
		"inline event handler": {
			input:  `<div onclick="evil()">text</div>` + "\n",
			absent: []string{"onclick", "evil()"},
		},
		"style element": {
			input:  "<style>body{display:none}</style>\n\ntext\n",
			absent: []string{"<style", "display:none"},
		},
		"iframe": {
			input:  `<iframe src="https://example.com"></iframe>` + "\n",
			absent: []string{"<iframe"},
		},
		"form": {
			input:  `<form action="https://example.com"><input name="a"></form>` + "\n",
			absent: []string{"<form", "<input"},
		},
		"svg onload": {
			input:  `<svg onload="alert(1)"></svg>` + "\n",
			absent: []string{"<svg", "onload"},
		},
		"data uri link": {
			input:  "[x](data:text/html;base64,PHNjcmlwdD4=)\n",
			absent: []string{"data:text/html"},
		},
		"foreign class name": {
			// A document must not borrow a class from the application's own
			// stylesheet and have its text restyled.
			input:  `<div class="modal-overlay">text</div>` + "\n",
			absent: []string{"modal-overlay"},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := Render(".md", []byte(test.input))
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			for _, unwanted := range test.absent {
				if strings.Contains(got, unwanted) {
					t.Errorf("output contains %q:\n%s", unwanted, got)
				}
			}
			for _, wanted := range test.present {
				if !strings.Contains(got, wanted) {
					t.Errorf("output lost %q:\n%s", wanted, got)
				}
			}
		})
	}
}

func TestRenderKeepsDocumentationFormatting(t *testing.T) {
	input := "# Rebuild\n\n" +
		"Run with **care** and see `Get-ChildItem`.\n\n" +
		"```powershell\nGet-ChildItem -Recurse\n```\n\n" +
		"| Option | Meaning |\n|--------|---------|\n" +
		"| `-Mode` | which index |\n\n" +
		"- first\n- second\n\n" +
		"> quoted\n\n" +
		"[runbook](https://example.com/runbook)\n"

	got, err := Render(".md", []byte(input))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, wanted := range []string{
		"<h1", "Rebuild",
		"<strong>care</strong>",
		"<code>Get-ChildItem</code>",
		"<pre>", "language-powershell",
		"<table>", "<th>Option</th>",
		"<li>first</li>",
		"<blockquote>",
		`href="https://example.com/runbook"`,
	} {
		if !strings.Contains(got, wanted) {
			t.Errorf("output lost %q:\n%s", wanted, got)
		}
	}
	// An external link must not be able to navigate the application away.
	if !strings.Contains(got, `rel="nofollow"`) {
		t.Errorf("external link is missing rel=nofollow:\n%s", got)
	}
}

func TestRenderHeadingIDs(t *testing.T) {
	// Documentation links to its own sections, so heading identifiers have to
	// survive, but only in a form that cannot break out of an attribute.
	got, err := Render(".md", []byte("## Rebuild the index\n"))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(got, `id="rebuild-the-index"`) {
		t.Errorf("heading id missing:\n%s", got)
	}
}

func TestRenderPlainTextPreservesLayout(t *testing.T) {
	input := "Usage: rebuild [options]\n\n  -Mode   which index to build\n\nSee <notes> & README\n"
	got, err := Render(".txt", []byte(input))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	// A preformatted block, not a run-together paragraph.
	if !strings.Contains(got, "<pre>") {
		t.Errorf("plain text should stay preformatted:\n%s", got)
	}
	for _, wanted := range []string{"Usage: rebuild", "  -Mode", "See "} {
		if !strings.Contains(got, wanted) {
			t.Errorf("output lost %q:\n%s", wanted, got)
		}
	}
	// The characters that would otherwise become markup are escaped.
	if !strings.Contains(got, "&lt;notes&gt;") || !strings.Contains(got, "&amp;") {
		t.Errorf("plain text was not escaped:\n%s", got)
	}
	if strings.Contains(got, "<notes>") {
		t.Errorf("raw tag survived:\n%s", got)
	}
}

func TestRenderExtensionHandling(t *testing.T) {
	markdown := []byte("# Title\n")
	// Extensions are matched without regard to case and with or without the
	// leading dot, because they arrive from a file name the user did not type
	// for this purpose.
	for _, extension := range []string{".md", "md", ".MD", ".markdown", ""} {
		if _, err := Render(extension, markdown); err != nil {
			t.Errorf("Render(%q): %v", extension, err)
		}
	}
	for _, extension := range []string{".txt", ".TXT", ".rst", "text"} {
		if _, err := Render(extension, []byte("plain\n")); err != nil {
			t.Errorf("Render(%q): %v", extension, err)
		}
	}
	// A format with no renderer is an error rather than a silent empty panel.
	for _, extension := range []string{".pdf", ".exe", ".png"} {
		if _, err := Render(extension, []byte("x")); !errors.Is(err, ErrUnsupportedFormat) {
			t.Errorf("Render(%q) error = %v, want ErrUnsupportedFormat", extension, err)
		}
	}
}

func TestRenderRejectsOversizedInput(t *testing.T) {
	oversized := make([]byte, maxDocumentBytes+1)
	for i := range oversized {
		oversized[i] = 'a'
	}
	if _, err := Render(".md", oversized); !errors.Is(err, ErrDocumentTooLarge) {
		t.Errorf("error = %v, want ErrDocumentTooLarge", err)
	}
}

func TestRenderEmpty(t *testing.T) {
	got, err := Render(".md", nil)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.TrimSpace(got) != "" {
		t.Errorf("empty input rendered to %q, want empty", got)
	}
}

func TestRenderFile(t *testing.T) {
	dir := t.TempDir()

	markdown := filepath.Join(dir, "README.md")
	if err := os.WriteFile(markdown, []byte("# Title\n\n<script>bad()</script>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := RenderFile(markdown)
	if err != nil {
		t.Fatalf("RenderFile: %v", err)
	}
	if strings.Contains(got, "bad()") {
		t.Errorf("RenderFile did not sanitize:\n%s", got)
	}
	if !strings.Contains(got, "Title") {
		t.Errorf("RenderFile lost the title:\n%s", got)
	}

	text := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(text, []byte("line one\n  two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := RenderFile(text); err != nil {
		t.Fatalf("RenderFile: %v", err)
	} else if !strings.Contains(got, "line one") {
		t.Errorf("RenderFile text = %q", got)
	}

	if _, err := RenderFile(filepath.Join(dir, "absent.md")); err == nil {
		t.Error("want an error for a file that does not exist")
	}

	unsupported := filepath.Join(dir, "manual.pdf")
	if err := os.WriteFile(unsupported, []byte("%PDF"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := RenderFile(unsupported); !errors.Is(err, ErrUnsupportedFormat) {
		t.Errorf("error = %v, want ErrUnsupportedFormat", err)
	}
}

func TestRenderFileRejectsOversizedFileWithoutReadingIt(t *testing.T) {
	dir := t.TempDir()
	// A sparse file is enough: the size check must happen before the read, or a
	// huge file would be pulled into memory just to be rejected.
	path := filepath.Join(dir, "huge.md")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxDocumentBytes + 1); err != nil {
		t.Skipf("cannot create a sparse file here: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := RenderFile(path); !errors.Is(err, ErrDocumentTooLarge) {
		t.Errorf("error = %v, want ErrDocumentTooLarge", err)
	}
}
