package docs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile creates a file under dir, making parent directories as needed.
func writeFile(t *testing.T, dir, rel, content string) string {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("creating %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return path
}

func TestDiscoverFindsSidecar(t *testing.T) {
	dir := t.TempDir()
	script := writeFile(t, dir, "rebuild.ps1", "param()\n")
	writeFile(t, dir, "rebuild.md", "# Rebuild\n")

	documents, err := Discover(script, dir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(documents) != 1 {
		t.Fatalf("got %d documents, want 1: %+v", len(documents), documents)
	}
	got := documents[0]
	if got.Kind != KindSidecar {
		t.Errorf("Kind = %q, want %q", got.Kind, KindSidecar)
	}
	if got.Title != "Rebuild" {
		t.Errorf("Title = %q, want the readme heading", got.Title)
	}
	// A sidecar is written about this one script, so it is never excerpted.
	if got.Excerpted {
		t.Error("a sidecar should not be excerpted")
	}
}

func TestDiscoverFindsSidecarInDocsDirectory(t *testing.T) {
	dir := t.TempDir()
	script := writeFile(t, dir, "rebuild.ps1", "param()\n")
	writeFile(t, dir, "docs/rebuild.md", "# Rebuild\n")

	documents, err := Discover(script, dir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(documents) != 1 || documents[0].Kind != KindSidecar {
		t.Fatalf("got %+v, want one sidecar", documents)
	}
}

func TestDiscoverFindsNearestReadme(t *testing.T) {
	root := t.TempDir()
	script := writeFile(t, root, "scripts/ops/rebuild.ps1", "param()\n")
	writeFile(t, root, "README.md", "# Project\n")
	writeFile(t, root, "scripts/README.md", "# Scripts\n")
	writeFile(t, root, "scripts/ops/README.md", "# Operations\n")

	documents, err := Discover(script, root)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(documents) != 3 {
		t.Fatalf("got %d readmes, want 3: %+v", len(documents), documents)
	}
	for _, document := range documents {
		if document.Kind != KindReadme {
			t.Errorf("%s kind = %q, want readme", document.Path, document.Kind)
		}
	}
	// Nearest first, because a readme in the script's own directory is more
	// likely to be about the script than one at the project root.
	if !strings.Contains(documents[0].Path, filepath.Join("scripts", "ops")) {
		t.Errorf("first document = %q, want the nearest readme", documents[0].Path)
	}
	if !strings.HasSuffix(documents[0].Path, "scripts/ops/README.md") &&
		!strings.HasSuffix(documents[0].Path, `scripts\ops\README.md`) {
		t.Errorf("first document = %q", documents[0].Path)
	}
}

func TestDiscoverStopsAtRoot(t *testing.T) {
	// A readme above the workspace root describes a different project.
	parent := t.TempDir()
	root := filepath.Join(parent, "workspace")
	script := writeFile(t, root, "rebuild.ps1", "param()\n")
	writeFile(t, parent, "README.md", "# Something else entirely\n")

	documents, err := Discover(script, root)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(documents) != 0 {
		t.Fatalf("got %+v, want nothing: the readme is above the root", documents)
	}
}

func TestDiscoverExcerptsScriptSection(t *testing.T) {
	dir := t.TempDir()
	script := writeFile(t, dir, "rebuild-index.ps1", "param()\n")
	writeFile(t, dir, "README.md", strings.Join([]string{
		"# Operations",
		"",
		"General guidance that applies to everything.",
		"",
		"## rebuild-index",
		"",
		"Rebuilds the search index. Safe to re-run.",
		"",
		"## backup",
		"",
		"Backs up the database.",
		"",
	}, "\n"))

	documents, err := Discover(script, dir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(documents) != 1 {
		t.Fatalf("got %d documents, want 1", len(documents))
	}
	document := documents[0]
	if !document.Excerpted {
		t.Fatal("want the matching section to be excerpted")
	}
	if !strings.Contains(document.Section, "Rebuilds the search index") {
		t.Errorf("Section = %q", document.Section)
	}
	// The excerpt stops at the next heading of the same level, so a neighboring
	// script's section is not shown as if it were this one.
	if strings.Contains(document.Section, "Backs up the database") {
		t.Errorf("Section leaked the next script's documentation: %q", document.Section)
	}
	if document.Title != "rebuild-index" {
		t.Errorf("Title = %q, want the section heading", document.Title)
	}
}

func TestDiscoverMatchesScriptNameAcrossSeparators(t *testing.T) {
	dir := t.TempDir()
	script := writeFile(t, dir, "rebuild-index.ps1", "param()\n")
	writeFile(t, dir, "README.md", "# Project\n\n## Rebuild_Index\n\nBody.\n")

	documents, err := Discover(script, dir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(documents) != 1 || !documents[0].Excerpted {
		t.Skipf("heading form not matched: %+v", documents)
	}
}

func TestDiscoverNoExcerptWhenNoHeadingMatches(t *testing.T) {
	dir := t.TempDir()
	script := writeFile(t, dir, "rebuild.ps1", "param()\n")
	writeFile(t, dir, "README.md", "# Project\n\nJust prose about the project.\n")

	documents, err := Discover(script, dir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(documents) != 1 {
		t.Fatalf("got %d documents, want 1", len(documents))
	}
	// The whole readme still applies, so it is offered unexcerpted.
	if documents[0].Excerpted {
		t.Error("want no excerpt when no heading names the script")
	}
	if documents[0].Title != "Project" {
		t.Errorf("Title = %q, want the first heading", documents[0].Title)
	}
}

func TestDiscoverIgnoresNonDocumentation(t *testing.T) {
	dir := t.TempDir()
	script := writeFile(t, dir, "rebuild.ps1", "param()\n")
	// A file a script listing must never offer as instructions.
	writeFile(t, dir, "CHANGELOG.md", "# Changes\n")
	writeFile(t, dir, "LICENSE", "MIT\n")
	writeFile(t, dir, "rebuild.ps1.md", "not a sidecar\n")

	documents, err := Discover(script, dir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(documents) != 0 {
		t.Fatalf("got %+v, want nothing", documents)
	}
}

func TestDiscoverMissingScript(t *testing.T) {
	dir := t.TempDir()
	// A script that does not exist still has a name to search on, and a
	// directory that does exist may still hold its documentation.
	writeFile(t, dir, "README.md", "# Project\n")

	documents, err := Discover(filepath.Join(dir, "absent.ps1"), dir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(documents) != 1 || documents[0].Kind != KindReadme {
		t.Errorf("got %+v, want the readme", documents)
	}
}

func TestParseHeading(t *testing.T) {
	tests := []struct {
		line      string
		wantText  string
		wantLevel int
		wantOK    bool
	}{
		{"# Title", "Title", 1, true},
		{"## Two", "Two", 2, true},
		{"###### Six", "Six", 6, true},
		{"####### Seven", "", 0, false},
		{"## Closed ##", "Closed", 2, true},
		{"  ## Indented", "Indented", 2, true},
		{"#NoSpace", "", 0, false},
		{"#", "", 0, false},
		{"#hashtag", "", 0, false},
		{"plain text", "", 0, false},
		{"", "", 0, false},
	}
	for _, test := range tests {
		text, level, ok := parseHeading(test.line)
		if ok != test.wantOK || text != test.wantText || level != test.wantLevel {
			t.Errorf("parseHeading(%q) = %q, %d, %v; want %q, %d, %v",
				test.line, text, level, ok, test.wantText, test.wantLevel, test.wantOK)
		}
	}
}

func TestNormalizeHeading(t *testing.T) {
	equal := [][2]string{
		{"rebuild-index", "rebuild_index"},
		{"Rebuild_Index", "rebuild_index"},
		{"rebuild index", "rebuild_index"},
		{"rebuild  --  index", "rebuild_index"},
		{"rebuild.ps1", "rebuild"},
		{"./rebuild-index.ps1", "rebuild_index"},
		{"REBUILD-INDEX", "rebuild_index"},
	}
	for _, pair := range equal {
		if got := normalizeHeading(pair[0]); got != pair[1] {
			t.Errorf("normalizeHeading(%q) = %q, want %q", pair[0], got, pair[1])
		}
	}
	if got := normalizeHeading("---"); got != "" {
		t.Errorf("normalizeHeading(%q) = %q, want empty", got, got)
	}
}
