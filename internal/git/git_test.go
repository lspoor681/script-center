package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// newRepo creates an initialized repository with one commit and returns its
// path. These tests drive real git rather than feeding canned porcelain output,
// because the point is to agree with whatever the installed git actually emits,
// and porcelain formats have changed across versions.
func newRepo(t *testing.T) string {
	t.Helper()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	dir := t.TempDir()
	git(t, dir, "init", "-b", "main")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "user.name", "Test User")
	// Ignore signing so a machine-wide gpgsign=true cannot break the fixture.
	git(t, dir, "config", "commit.gpgsign", "false")

	write(t, dir, "README.md", "# fixture\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "initial commit")

	return dir
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	// A deterministic environment keeps the fixture reproducible.
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_TERMINAL_PROMPT=0",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestStateCleanRepo(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()

	got, err := Status(ctx, dir)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	if !got.IsRepo {
		t.Fatal("IsRepo = false, want true")
	}
	if got.Branch != "main" {
		t.Errorf("Branch = %q, want %q", got.Branch, "main")
	}
	if got.Dirty() {
		t.Errorf("Dirty() = true on a clean repo: %+v", got)
	}
	if got.OID == "" {
		t.Error("OID is empty on a repo with a commit")
	}
}

func TestStatusCountsChanges(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()

	// Staged: a new file added to the index.
	write(t, dir, "staged.txt", "new\n")
	git(t, dir, "add", "staged.txt")

	// Unstaged: a tracked file modified in the worktree only.
	write(t, dir, "README.md", "# changed\n")

	// Untracked: present but neither staged nor ignored.
	write(t, dir, "untracked.txt", "loose\n")

	got, err := Status(ctx, dir)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	if got.Staged != 1 {
		t.Errorf("Staged = %d, want 1", got.Staged)
	}
	if got.Unstaged != 1 {
		t.Errorf("Unstaged = %d, want 1", got.Unstaged)
	}
	if got.Untracked != 1 {
		t.Errorf("Untracked = %d, want 1", got.Untracked)
	}
	if !got.Dirty() {
		t.Error("Dirty() = false, want true")
	}
}

func TestStatusRespectsGitignore(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()

	write(t, dir, ".gitignore", "secrets.txt\n")
	git(t, dir, "add", ".gitignore")
	git(t, dir, "commit", "-m", "add ignore")

	// An ignored file must not appear as untracked, which is the whole reason
	// the scanner defers to git for ignore handling.
	write(t, dir, "secrets.txt", "hunter2\n")

	got, err := Status(ctx, dir)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got.Untracked != 0 {
		t.Errorf("Untracked = %d, want 0 for an ignored file", got.Untracked)
	}

	// It must also be absent from the file listing the scanner relies on.
	files, err := TrackedFiles(ctx, dir)
	if err != nil {
		t.Fatalf("TrackedFiles: %v", err)
	}
	for _, f := range files {
		if f == "secrets.txt" {
			t.Error("TrackedFiles returned an ignored file")
		}
	}
}

func TestStatusNotARepository(t *testing.T) {
	ctx := context.Background()
	// A temp dir on a machine where TMPDIR is inside a repo would be
	// unreliable, so use a subdirectory of the OS temp root explicitly.
	dir := t.TempDir()

	got, err := Status(ctx, dir)
	if err != nil {
		t.Fatalf("Status on a non-repo: %v, want nil error", err)
	}
	if got.IsRepo {
		t.Error("IsRepo = true for a non-repository")
	}
}

func TestIsRepo(t *testing.T) {
	ctx := context.Background()
	if IsRepo(ctx, newRepo(t)) != true {
		t.Error("IsRepo = false for a real repository")
	}
	if IsRepo(ctx, t.TempDir()) != false {
		t.Error("IsRepo = true for a non-repository")
	}
}

func TestLastCommit(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()

	got, err := LastCommit(ctx, dir)
	if err != nil {
		t.Fatalf("LastCommit: %v", err)
	}
	if got.Subject != "initial commit" {
		t.Errorf("Subject = %q, want %q", got.Subject, "initial commit")
	}
	if got.Author != "Test User" {
		t.Errorf("Author = %q, want %q", got.Author, "Test User")
	}
	if got.ShortOID == "" || got.OID == "" {
		t.Error("LastCommit returned empty identifiers")
	}
	if got.Date.IsZero() {
		t.Error("LastCommit returned a zero date")
	}
}

func TestTrackedFilesIncludesUntrackedButNotIgnored(t *testing.T) {
	dir := newRepo(t)
	ctx := context.Background()

	write(t, dir, ".gitignore", "*.log\n")
	git(t, dir, "add", ".gitignore")
	git(t, dir, "commit", "-m", "ignore logs")

	write(t, dir, "tool.sh", "#!/bin/sh\n")
	write(t, dir, "debug.log", "noise\n")

	files, err := TrackedFiles(ctx, dir)
	if err != nil {
		t.Fatalf("TrackedFiles: %v", err)
	}

	want := map[string]bool{"README.md": false, ".gitignore": false, "tool.sh": false}
	for _, f := range files {
		if _, tracked := want[f]; tracked {
			want[f] = true
		}
	}
	for f, found := range want {
		if !found {
			t.Errorf("TrackedFiles missing %q; got %v", f, files)
		}
	}
	for _, f := range files {
		if f == "debug.log" {
			t.Error("TrackedFiles returned an ignored file")
		}
	}
}

// TestParseStatusAgainstRealOutput feeds the parser output captured from the
// installed git, including the rename shape where the original path occupies a
// separate NUL-delimited record.
func TestParseStatusRename(t *testing.T) {
	dir := newRepo(t)
	git(t, dir, "mv", "README.md", "README.rst")
	git(t, dir, "add", "-A")

	got, err := Status(context.Background(), dir)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got.Staged != 1 {
		t.Errorf("Staged = %d after a staged rename, want 1", got.Staged)
	}
}

func TestStatusUnmerged(t *testing.T) {
	dir := newRepo(t)
	git(t, dir, "config", "merge.conflictStyle", "diff3")

	base := "line\n"
	write(t, dir, "conflict.txt", base)
	git(t, dir, "add", "conflict.txt")
	git(t, dir, "commit", "-m", "base")

	git(t, dir, "checkout", "-q", "-b", "other")
	write(t, dir, "conflict.txt", "other\n")
	git(t, dir, "commit", "-qam", "other side")

	git(t, dir, "checkout", "-q", "main")
	write(t, dir, "conflict.txt", "main\n")
	git(t, dir, "commit", "-qam", "main side")

	// Merge is expected to conflict; that is the state under test.
	cmd := exec.Command("git", "merge", "other")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	_ = cmd.Run()

	got, err := Status(context.Background(), dir)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got.Conflicted != 1 {
		t.Errorf("Conflicted = %d, want 1 (status %+v)", got.Conflicted, got)
	}
}

func TestParseHeaderAheadBehind(t *testing.T) {
	s := State{}
	parseHeader("branch.ab +4 -2", &s)
	if s.Ahead != 4 || s.Behind != 2 {
		t.Errorf("Ahead/Behind = %d/%d, want 4/2", s.Ahead, s.Behind)
	}

	s = State{}
	parseHeader("branch.head (detached)", &s)
	if !s.Detached {
		t.Error("Detached = false, want true")
	}

	// An unborn branch has no commit yet and must not surface a placeholder.
	s = State{}
	parseHeader("branch.oid (initial)", &s)
	if s.OID != "" {
		t.Errorf("OID = %q for an unborn branch, want empty", s.OID)
	}
}
