// Package git wraps the git plumbing the application needs to describe a
// workspace root.
//
// Everything here shells out to git rather than using a Go git library. The
// application needs to agree exactly with whatever git itself reports, including
// .git/info/exclude, core.excludesFile and the user's global gitignore. A
// reimplementation of those rules would drift, and the user is the one who
// decides what git means on their machine.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ErrNotARepository is returned by Status when the directory is not inside a
// git work tree. Callers that only want to know whether git applies should use
// IsRepo, which does not treat this as a failure.
var ErrNotARepository = errors.New("git: not a repository")

// State is a summary of the working tree state of one repository.
type State struct {
	// IsRepo is false when the directory is not inside a git work tree, in
	// which case every other field is zero.
	IsRepo bool

	// Branch is the checked-out branch name, or the short commit hash when
	// HEAD is detached.
	Branch string
	// Detached reports that HEAD points at a commit rather than a branch.
	Detached bool
	// OID is the full commit hash HEAD resolves to.
	OID string
	// Upstream is the configured upstream ref, empty when none is set.
	Upstream string
	// Ahead and Behind are commit counts relative to Upstream.
	Ahead  int
	Behind int

	// Staged counts paths with index changes, Unstaged counts paths with
	// worktree changes, and Conflicted counts unmerged paths.
	Staged     int
	Unstaged   int
	Untracked  int
	Conflicted int

	// Stashed is the number of stashed entries, reported by git as a header.
	Stashed int
}

// Dirty reports whether anything other than untracked files has changed. It is
// the flag the tree view uses to decide whether a root needs attention.
func (s State) Dirty() bool {
	return s.Staged > 0 || s.Unstaged > 0 || s.Conflicted > 0
}

// Commit describes the most recent commit on the current branch.
type Commit struct {
	OID      string    `json:"oid"`
	ShortOID string    `json:"shortOid"`
	Author   string    `json:"author"`
	Date     time.Time `json:"date"`
	Subject  string    `json:"subject"`
}

// IsRepo reports whether dir is inside a git work tree.
func IsRepo(ctx context.Context, dir string) bool {
	out, err := run(ctx, dir, "rev-parse", "--is-inside-work-tree")
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

// RepoRoot returns the absolute path of the work tree containing dir. It fails
// with ErrNotARepository when dir is not inside a work tree.
func RepoRoot(ctx context.Context, dir string) (string, error) {
	out, err := run(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrNotARepository, dir)
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return "", fmt.Errorf("%w: %s", ErrNotARepository, dir)
	}
	return root, nil
}

// Status returns a summary of the working tree containing dir.
//
// The NUL-delimited form of porcelain v2 is used deliberately: the default
// output quotes paths containing spaces, quotes or non-ASCII characters, and
// unquoting that is a reliable source of subtle path bugs.
func Status(ctx context.Context, dir string) (State, error) {
	if !IsRepo(ctx, dir) {
		return State{IsRepo: false}, nil
	}

	out, err := run(ctx, dir, "status", "--porcelain=v2", "--branch", "-z")
	if err != nil {
		return State{}, fmt.Errorf("git status in %s: %w", dir, err)
	}
	return parseStatus(string(out))
}

func parseStatus(raw string) (State, error) {
	s := State{IsRepo: true}

	// NUL terminates each record. A renamed entry is spread over two records:
	// the change itself, then the original path.
	records := strings.Split(raw, "\x00")
	for i := 0; i < len(records); i++ {
		rec := records[i]
		if rec == "" {
			continue
		}

		if strings.HasPrefix(rec, "# ") {
			parseHeader(rec[2:], &s)
			continue
		}

		switch rec[0] {
		case '1':
			parseOrdinary(rec, &s)
		case '2':
			// Type 2 is a rename or copy, spread over two records: the change
			// itself, then the original path in the following record. The XY
			// pair is positioned as in type 1, so a staged rename counts as
			// staged.
			parseRename(rec, &s)
			i++
		case 'u':
			s.Conflicted++
		case '?':
			s.Untracked++
		case '!':
			// Ignored files are not requested and need no accounting.
		}
	}
	return s, nil
}

// parseHeader reads the "# branch.*" and "# stash" metadata lines.
func parseHeader(line string, s *State) {
	key, value, _ := strings.Cut(line, " ")
	switch key {
	case "branch.oid":
		s.OID = value
		// git reports a literal placeholder before the first commit.
		if value == "(initial)" {
			s.OID = ""
		}
	case "branch.head":
		s.Branch = value
		// A detached HEAD is reported as the literal "detached", which is the
		// only branch-like information available on that line.
		if value == "(detached)" {
			s.Branch = "(detached)"
			s.Detached = true
		}
	case "branch.upstream":
		s.Upstream = value
	case "branch.ab":
		// Format is "+<ahead> -<behind>".
		for _, field := range strings.Fields(value) {
			n, err := strconv.Atoi(field[1:])
			if err != nil {
				continue
			}
			switch field[0] {
			case '+':
				s.Ahead = n
			case '-':
				s.Behind = n
			}
		}
	case "stash":
		if n, err := strconv.Atoi(value); err == nil {
			s.Stashed = n
		}
	}
}

// parseOrdinary handles a type 1 record:
//
//	1 <XY> <sub> <mH> <mI> <mW> <hH> <hI> <path>
func parseOrdinary(rec string, s *State) {
	fields := strings.SplitN(rec, " ", 9)
	if len(fields) < 9 {
		return
	}
	tallyXY(fields[1], s)
}

// parseRename handles a type 2 record:
//
//	2 <XY> <sub> <mH> <mI> <mW> <hH> <hI> <X><score> <path>
//
// The original path arrives in the next NUL-delimited record and is not
// retained, because the caller only needs the counters.
func parseRename(rec string, s *State) {
	fields := strings.SplitN(rec, " ", 10)
	if len(fields) < 2 {
		return
	}
	tallyXY(fields[1], s)
}

// tallyXY increments the staged, unstaged and conflicted counters for an XY
// status pair, where the first character is the index status and the second is
// the worktree status. A dot means unchanged.
func tallyXY(xy string, s *State) {
	if len(xy) < 2 {
		return
	}

	index, worktree := xy[0], xy[1]
	if index == 'U' || worktree == 'U' ||
		(index == 'A' && worktree == 'A') ||
		(index == 'D' && worktree == 'D') {
		s.Conflicted++
		return
	}
	if index != '.' && index != ' ' {
		s.Staged++
	}
	if worktree != '.' && worktree != ' ' {
		s.Unstaged++
	}
}

// LastCommit returns the most recent commit reachable from HEAD, or a zero
// Commit when the repository has no commits yet.
func LastCommit(ctx context.Context, dir string) (Commit, error) {
	if !IsRepo(ctx, dir) {
		return Commit{}, ErrNotARepository
	}

	// 0x1f (unit separator) and 0x1e (record separator) cannot appear in a
	// hash, author or ISO date, and a commit subject containing a tab or
	// newline would defeat a simpler delimiter.
	const format = "%H\x1f%h\x1f%an\x1f%aI\x1f%s"
	out, err := run(ctx, dir, "log", "-1", "--format="+format)
	if err != nil {
		return Commit{}, fmt.Errorf("git log in %s: %w", dir, err)
	}

	line := strings.TrimSpace(string(out))
	if line == "" {
		return Commit{}, nil
	}

	parts := strings.SplitN(line, "\x1f", 5)
	if len(parts) < 5 {
		return Commit{}, fmt.Errorf("git log: unexpected output %q", line)
	}

	date, err := time.Parse(time.RFC3339, parts[3])
	if err != nil {
		return Commit{}, fmt.Errorf("git log: unparseable date %q: %w", parts[3], err)
	}

	return Commit{
		OID:      parts[0],
		ShortOID: parts[1],
		Author:   parts[2],
		Date:     date,
		Subject:  parts[4],
	}, nil
}

// TrackedFiles returns the paths of every file git knows about under dir, both
// tracked and untracked-but-not-ignored, as paths relative to dir.
//
// This is what makes ignore handling exact rather than approximate: the answer
// comes from git's own rules, so it matches what `git status` shows and cannot
// drift from the user's configuration.
func TrackedFiles(ctx context.Context, dir string) ([]string, error) {
	out, err := run(ctx, dir, "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, fmt.Errorf("git ls-files in %s: %w", dir, err)
	}

	var paths []string
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

// Porcelain returns the per-path working-tree status of a repository, keyed by
// the slash-separated path relative to dir. The value is git's two-character
// status pair: the index status, then the worktree status.
//
// The scanner lists scripts with git's help, and this uses the same NUL-delimited
// porcelain form, so its keys line up with scan entries. A renamed or copied
// entry maps both the new and the original path to the same status, because the
// scanner may be listing the script under either name.
func Porcelain(ctx context.Context, dir string) (map[string]string, error) {
	// --untracked-files=all lists each untracked file rather than collapsing an
	// untracked directory into one "?? dir/" entry, which would hide a new
	// script from its modified badge.
	out, err := run(ctx, dir, "status", "--porcelain", "--untracked-files=all", "-z")
	if err != nil {
		return nil, fmt.Errorf("git status --porcelain in %s: %w", dir, err)
	}
	return parsePorcelain(string(out)), nil
}

// parsePorcelain reads the NUL-delimited porcelain v1 output produced with -z.
func parsePorcelain(raw string) map[string]string {
	statuses := map[string]string{}

	// NUL terminates each record. A rename or copy appears as two records: the
	// change itself with its new path, then the original path, so the scanner
	// can badge a script whether it is listed under the new name or the old.
	records := strings.Split(raw, "\x00")
	for i := 0; i < len(records); i++ {
		rec := records[i]
		if rec == "" {
			continue
		}
		statuses[filepath.ToSlash(rec[3:])] = rec[:2]
		// The rename or copy flag is the index status (first character) for a
		// staged move and the worktree status (second character) for one that
		// only happened in the working directory, so either position counts.
		if rec[0] == 'R' || rec[1] == 'R' || rec[0] == 'C' || rec[1] == 'C' {
			if i+1 < len(records) && records[i+1] != "" {
				statuses[filepath.ToSlash(records[i+1])] = rec[:2]
				i++
			}
		}
	}
	return statuses
}

// RemoteURL returns the URL of the origin remote, or "" when the repository has
// no such remote. A missing remote is normal for a local-only repository, so it
// is reported as an absence rather than an error.
func RemoteURL(ctx context.Context, dir string) (string, error) {
	out, err := run(ctx, dir, "config", "--get", "remote.origin.url")
	if err != nil {
		return "", nil
	}
	return strings.TrimSpace(string(out)), nil
}

// run executes git in dir and returns stdout.
func run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// git's own message is far more useful than the exit status, and it
		// is the difference between "not a repository" and a real failure.
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, errors.New(msg)
	}
	return stdout.Bytes(), nil
}
