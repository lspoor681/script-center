package workspace_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/lspoor/script-center/internal/detect"
	"github.com/lspoor/script-center/internal/git"
	"github.com/lspoor/script-center/internal/scan"
	"github.com/lspoor/script-center/internal/workspace"
)

// TestManualDemo is not an assertion. It exercises the M1 pipeline against real
// directories on this machine and prints what it finds, which is how the
// milestone was verified. It is skipped unless SCRIPT_CENTER_DEMO names a
// directory, so it never runs in CI.
func TestManualDemo(t *testing.T) {
	root := os.Getenv("SCRIPT_CENTER_DEMO")
	if root == "" {
		t.Skip("set SCRIPT_CENTER_DEMO to a directory to run the demo")
	}
	ctx := context.Background()

	ws := workspace.New().AddRoot(root, "")
	fmt.Printf("\n=== workspace ===\n")
	for _, r := range ws.Roots {
		fmt.Printf("  root=%q name=%q\n", r.Path, r.Name)
	}

	res, err := scan.Scan(ctx, root)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	fmt.Printf("\n=== scan ===\n")
	fmt.Printf("  source=%s isRepo=%v entries=%d warnings=%d\n",
		res.Source, res.IsRepo, len(res.Entries), len(res.Warnings))
	for _, w := range res.Warnings {
		fmt.Printf("  warning: %s\n", w)
	}

	st, err := git.Status(ctx, root)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	fmt.Printf("\n=== git state ===\n")
	fmt.Printf("  branch=%q detached=%v upstream=%q ahead=%d behind=%d\n",
		st.Branch, st.Detached, st.Upstream, st.Ahead, st.Behind)
	fmt.Printf("  staged=%d unstaged=%d untracked=%d conflicted=%d stashed=%d\n",
		st.Staged, st.Unstaged, st.Untracked, st.Conflicted, st.Stashed)
	if c, err := git.LastCommit(ctx, root); err == nil {
		fmt.Printf("  last=%s %q by %s\n", c.ShortOID, c.Subject, c.Author)
	}

	fmt.Printf("\n=== scripts (first 20) ===\n")
	for i, e := range res.Entries {
		if i >= 20 {
			fmt.Printf("  ... and %d more\n", len(res.Entries)-20)
			break
		}
		fmt.Printf("  %-10s %-8s %s\n", e.Lang, e.Kind, e.Rel)
	}

	fmt.Printf("\n=== by language ===\n")
	for _, lang := range []detect.Language{
		detect.PowerShell, detect.Batch, detect.Bash, detect.Python, detect.Rust,
	} {
		if n := len(res.ByLang(lang)); n > 0 {
			fmt.Printf("  %-12s %d\n", lang.DisplayName(), n)
		}
	}

	// Star the first script so the favorites round trip is exercised too.
	if len(res.Entries) > 0 {
		target := res.Entries[0]
		ws.ToggleFavorite(workspace.Ref{Root: target.Root, Rel: target.Rel})
		fmt.Printf("\n=== favorites ===\n")
		for _, f := range ws.Favorites {
			fmt.Printf("  %s\n", f.Rel)
		}

		fmt.Printf("\n=== suggestions for %q ===\n", target.Rel)
		for _, s := range workspace.Suggest(target, res.Entries, 5) {
			fmt.Printf("  %-4d %-44s %s\n", s.Score, s.Entry.Rel, s.Reason)
		}
	}
	fmt.Println()
}
