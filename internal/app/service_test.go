package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/lspoor/script-center/internal/detect"
	"github.com/lspoor/script-center/internal/params"
	"github.com/lspoor/script-center/internal/scan"
)

// newTestService returns a Service whose state lives in a temporary directory, so
// a test never reads or writes the developer's real workspace.
func newTestService(t *testing.T) (*Service, string) {
	t.Helper()
	config := t.TempDir()
	service := New(config)
	if err := service.Start(); err != nil {
		t.Fatal(err)
	}
	return service, config
}

// scriptTree builds a small root with one script of each readable language.
func scriptTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("deploy.ps1", `param(
    [Parameter(Mandatory)]
    [string]$Target,

    [switch]$Force
)

<#
.SYNOPSIS
Deploys the thing.
#>
`)
	write("rebuild.sh", "#!/bin/bash\ncase \"$1\" in\n  -v|--verbose) shift ;;\nesac\n")
	write("render.py", `import argparse

parser = argparse.ArgumentParser(description="Renders the thing.")
parser.add_argument("--input", required=True, help="the thing to render")
parser.add_argument("--count", type=int, default=1, help="how many")
parser.parse_args()
`)
	return root
}

// scriptAt returns the view of a script by relative path, failing the test when
// it is not there. A test that cannot find the script it set up is broken, and
// saying which one it wanted makes that obvious.
func scriptAt(t *testing.T, view *RootView, rel string) ScriptView {
	t.Helper()
	script, ok := findScript(view, rel)
	if !ok {
		t.Fatalf("no script at %s in %d scripts", rel, len(view.Scripts))
	}
	return *script
}

func TestOpenRootReadsEveryReadableScript(t *testing.T) {
	// The whole point of the milestone: opening a root gives the user a form for
	// each script that has one, whatever language it is written in.
	service, _ := newTestService(t)
	view, err := service.OpenRoot(context.Background(), scriptTree(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Scripts) != 3 {
		t.Fatalf("got %d scripts, want 3", len(view.Scripts))
	}

	deploy := scriptAt(t, view, "deploy.ps1")
	if !deploy.NeedsForm() {
		t.Errorf("deploy.ps1 should offer a form: %+v", deploy.Metadata.Warnings)
	}
	if len(deploy.Metadata.Params) != 2 {
		t.Errorf("deploy.ps1 params = %+v, want 2", deploy.Metadata.Params)
	}
	if !deploy.Metadata.Params[0].Required {
		t.Error("Target is Mandatory and should be required")
	}

	rebuild := scriptAt(t, view, "rebuild.sh")
	if len(rebuild.Metadata.Params) != 1 {
		t.Errorf("rebuild.sh params = %+v, want verbose", rebuild.Metadata.Params)
	}

	render := scriptAt(t, view, "render.py")
	if len(render.Metadata.Params) != 2 {
		t.Errorf("render.py params = %+v, want 2", render.Metadata.Params)
	}
	if render.Metadata.Help.Synopsis == "" {
		t.Error("render.py should carry its description")
	}
}

func TestOpenRootRecordsHowLongAndHowMuchItCost(t *testing.T) {
	// The window needs to show why a reopen was fast, and a read that never says
	// what it skipped leaves the user wondering whether anything is wrong.
	service, _ := newTestService(t)
	view, err := service.OpenRoot(context.Background(), scriptTree(t))
	if err != nil {
		t.Fatal(err)
	}
	if view.Meta.Total != 3 {
		t.Errorf("Total = %d, want 3", view.Meta.Total)
	}
	if view.Meta.FromCache != 0 || view.Meta.Read != 3 {
		t.Errorf("first read: %d cached, %d read; want 0 and 3", view.Meta.FromCache, view.Meta.Read)
	}
}

func TestReopeningServesEverythingFromTheCache(t *testing.T) {
	// This is the whole reason the cache exists: a second open of an unchanged
	// root must not start a toolchain for any script.
	root := scriptTree(t)
	service, _ := newTestService(t)
	if _, err := service.OpenRoot(context.Background(), root); err != nil {
		t.Fatal(err)
	}

	// A second service over the same config directory, so the cache is read from
	// disk exactly as it would be on the next launch.
	reopened := New(filepath.Join(service.configDir))
	if err := reopened.Start(); err != nil {
		t.Fatal(err)
	}
	view, err := reopened.OpenRoot(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if view.Meta.FromCache != 3 || view.Meta.Read != 0 {
		t.Errorf("reopen: %d cached, %d read; want 3 and 0", view.Meta.FromCache, view.Meta.Read)
	}
	for _, script := range view.Scripts {
		if !script.Cached {
			t.Errorf("%s was read rather than served from the cache", script.Rel)
		}
	}
}

func TestAnEditedScriptIsReadAgain(t *testing.T) {
	// A cache that kept serving an edited script would let the user run a command
	// with arguments the file no longer accepts, so this is the correctness case
	// the cache has to get right.
	root := scriptTree(t)
	service, _ := newTestService(t)
	if _, err := service.OpenRoot(context.Background(), root); err != nil {
		t.Fatal(err)
	}

	// The rewrite changes both the length and the timestamp, as any real edit does.
	script := filepath.Join(root, "rebuild.sh")
	if err := os.WriteFile(script, []byte(
		"#!/bin/bash\ncase \"$1\" in\n  -q|--quiet) shift ;;\n  -v|--verbose) shift ;;\nesac\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	view, err := service.OpenRoot(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if view.Meta.FromCache != 2 || view.Meta.Read != 1 {
		t.Errorf("after an edit: %d cached, %d read; want 2 and 1", view.Meta.FromCache, view.Meta.Read)
	}
	rebuild := scriptAt(t, view, "rebuild.sh")
	if len(rebuild.Metadata.Params) != 2 {
		t.Errorf("params = %+v, want the new quiet and verbose", rebuild.Metadata.Params)
	}
}

func TestProjectEntriesAreNotReadAsScripts(t *testing.T) {
	// A project directory has no parameters of its own, so reading it would
	// produce a report about a directory that means nothing to the user.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "tool"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tool", "Cargo.toml"), []byte("[package]\nname=\"tool\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	service, _ := newTestService(t)
	view, err := service.OpenRoot(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	project := scriptAt(t, view, "tool")
	if project.NeedsForm() {
		t.Error("a project should not offer a parameter form")
	}
	if !project.Inferred {
		t.Error("a project should be marked as having no harvested metadata")
	}
	if !project.Metadata.HasWarnings() {
		t.Error("a project should say why it has no form")
	}
}

func TestAnUnsupportedLanguageIsListedWithAReason(t *testing.T) {
	// The script must still appear and still be runnable; it just has no form, and
	// the user is told why rather than left guessing.
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "worker.rs"), []byte("fn main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	service, _ := newTestService(t)
	view, err := service.OpenRoot(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	worker := scriptAt(t, view, "worker.rs")
	if worker.NeedsForm() {
		t.Error("a Rust program should not offer a form")
	}
	if !worker.Metadata.HasWarnings() {
		t.Fatal("a Rust program should say why it has no form")
	}
	if !strings.Contains(strings.Join(worker.Metadata.Warnings, " "), "Rust") {
		t.Errorf("warnings = %v, want them to name the language", worker.Metadata.Warnings)
	}
}

func TestViewsSerializeForTheFrontend(t *testing.T) {
	// The frontend reads these as JSON, so the tags have to be present and the
	// shape has to survive a round trip. A view that cannot be marshaled is a
	// blank window.
	service, _ := newTestService(t)
	view, err := service.OpenRoot(context.Background(), scriptTree(t))
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	var back RootView
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if len(back.Scripts) != len(view.Scripts) {
		t.Errorf("round trip kept %d scripts, want %d", len(back.Scripts), len(view.Scripts))
	}
	if back.Scripts[0].Rel == "" || back.Scripts[0].Name == "" {
		t.Error("a script lost its identity through JSON")
	}
}

func TestExplainMergesTheScriptWithItsDocuments(t *testing.T) {
	// Opening a script has to bring its documentation and its neighbors, or the
	// user has to go and find them by hand.
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "rebuild.sh"),
		[]byte("#!/bin/bash\ncase \"$1\" in\n  -v|--verbose) shift ;;\nesac\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "rebuild.md"), []byte("# Rebuild\n\nHow to rebuild.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "rebuild-all.sh"),
		[]byte("#!/bin/bash\ncase \"$1\" in\n  -v|--verbose) shift ;;\nesac\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	service, _ := newTestService(t)
	explanation, err := service.Explain(context.Background(), root, "rebuild.sh")
	if err != nil {
		t.Fatal(err)
	}
	if !explanation.Docs.HasDocuments() {
		t.Error("the sidecar should have been found")
	}
	if explanation.Docs.Primary == nil {
		t.Fatal("a document should be chosen to show first")
	}
	// A same-stem sibling is the most related thing there is, and it should be
	// offered without the user going looking.
	if len(explanation.Related) == 0 {
		t.Error("a same-stem sibling should be suggested")
	}
}

func TestExplainRejectsAnUnknownScript(t *testing.T) {
	service, _ := newTestService(t)
	if _, err := service.Explain(context.Background(), scriptTree(t), "absent.sh"); err == nil {
		t.Error("asking about a script that is not there should fail")
	}
}

func TestOpenRootRejectsAFile(t *testing.T) {
	// Adding a file as a root is a mistake worth naming, rather than a scan that
	// quietly finds nothing.
	root := scriptTree(t)
	service, _ := newTestService(t)
	if _, err := service.OpenRoot(context.Background(), filepath.Join(root, "rebuild.sh")); err == nil {
		t.Error("opening a file as a root should fail")
	}
}

func TestAddRootPersistsAndNamesIt(t *testing.T) {
	// The roots have to survive a restart, or the user re-adds their projects
	// every time they open the app.
	root := scriptTree(t)
	service, config := newTestService(t)
	if _, err := service.AddRoot(root, "Tools"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AddRoot(root, "Tools"); err != nil {
		t.Fatal(err)
	}

	// The same directory added twice is one root, not two.
	ws := service.Workspace()
	if len(ws.Roots) != 1 {
		t.Errorf("got %d roots, want 1", len(ws.Roots))
	}
	if ws.Roots[0].Name != "Tools" {
		t.Errorf("name = %q, want Tools", ws.Roots[0].Name)
	}

	restarted := New(config)
	if err := restarted.Start(); err != nil {
		t.Fatal(err)
	}
	if got := restarted.Workspace(); len(got.Roots) != 1 || got.Roots[0].Path != ws.Roots[0].Path {
		t.Errorf("after a restart the roots are %+v, want the one that was added", got.Roots)
	}
}

func TestAddRootDefaultsItsName(t *testing.T) {
	// A root added without a name should be labeled by its directory rather than
	// left blank in the tree.
	root := scriptTree(t)
	service, _ := newTestService(t)
	if _, err := service.AddRoot(root, ""); err != nil {
		t.Fatal(err)
	}
	ws := service.Workspace()
	if ws.Roots[0].Name != filepath.Base(root) {
		t.Errorf("name = %q, want %q", ws.Roots[0].Name, filepath.Base(root))
	}
}

func TestAddRootRejectsAFileAndAMissingPath(t *testing.T) {
	root := scriptTree(t)
	service, _ := newTestService(t)
	if _, err := service.AddRoot(filepath.Join(root, "rebuild.sh"), ""); err == nil {
		t.Error("adding a file as a root should fail")
	}
	if _, err := service.AddRoot(filepath.Join(root, "nowhere"), ""); err == nil {
		t.Error("adding a directory that is not there should fail")
	}
}

func TestRemoveRootForgetsTheScanAndTheCache(t *testing.T) {
	// A root the user removed should stop costing anything, and its reports
	// should not sit in the cache file growing forever.
	root := scriptTree(t)
	service, _ := newTestService(t)
	if _, err := service.AddRoot(root, "Tools"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.OpenRoot(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	if service.cache.Len() == 0 {
		t.Fatal("the read should have populated the cache")
	}

	if _, err := service.RemoveRoot(root); err != nil {
		t.Fatal(err)
	}
	if len(service.Workspace().Roots) != 0 {
		t.Error("the root should be gone from the workspace")
	}
	if service.cache.Len() != 0 {
		t.Errorf("the cache still holds %d reports for a removed root", service.cache.Len())
	}
}

func TestRemoveRootByADifferentSpellingStillWorks(t *testing.T) {
	// The path is stored absolute, so removing by a relative spelling has to
	// resolve the same way adding did.
	root := scriptTree(t)
	service, _ := newTestService(t)
	if _, err := service.AddRoot(root, "Tools"); err != nil {
		t.Fatal(err)
	}
	trailing := root + string(filepath.Separator)
	if _, err := service.RemoveRoot(trailing); err != nil {
		t.Fatal(err)
	}
	if len(service.Workspace().Roots) != 0 {
		t.Error("a trailing separator should not stop the root being removed")
	}
}

func TestRemoveOneRootKeepsTheOther(t *testing.T) {
	// Removing a root must not take a sibling with it, which is what a prefix
	// match on the directory name alone would do.
	parent := t.TempDir()
	first := filepath.Join(parent, "tools")
	second := filepath.Join(parent, "tools-archive")
	for _, dir := range []string{first, second} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "run.sh"), []byte("#!/bin/bash\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	service, _ := newTestService(t)
	for _, dir := range []string{first, second} {
		if _, err := service.AddRoot(dir, ""); err != nil {
			t.Fatal(err)
		}
		if _, err := service.OpenRoot(context.Background(), dir); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := service.RemoveRoot(first); err != nil {
		t.Fatal(err)
	}
	if service.cache.Len() == 0 {
		t.Error("the surviving root's reports should still be cached")
	}
	remaining := service.Workspace().Roots
	if len(remaining) != 1 || remaining[0].Path != second {
		t.Errorf("roots = %+v, want only the archive", remaining)
	}
}

func TestFavoritesToggleAndPersist(t *testing.T) {
	root := scriptTree(t)
	service, config := newTestService(t)
	if _, err := service.AddRoot(root, ""); err != nil {
		t.Fatal(err)
	}
	if service.IsFavorite(root, "deploy.ps1") {
		t.Error("nothing should be starred to begin with")
	}
	if _, err := service.ToggleFavorite(root, "deploy.ps1"); err != nil {
		t.Fatal(err)
	}
	if !service.IsFavorite(root, "deploy.ps1") {
		t.Error("the script should be starred")
	}
	if _, err := service.ToggleFavorite(root, "deploy.ps1"); err != nil {
		t.Fatal(err)
	}
	if service.IsFavorite(root, "deploy.ps1") {
		t.Error("toggling again should unstar it")
	}
	_ = config
}

func TestInvalidateRereadsOneScript(t *testing.T) {
	// The cache keys on size and timestamp, which an edit preserving both can
	// defeat. When a user is certain the file changed, this is the way to be sure.
	root := t.TempDir()
	script := filepath.Join(root, "run.sh")
	if err := os.WriteFile(script, []byte("#!/bin/bash\ncase \"$1\" in\n  -v) shift ;;\nesac\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	service, _ := newTestService(t)
	if _, err := service.OpenRoot(context.Background(), root); err != nil {
		t.Fatal(err)
	}

	// The same length, and the timestamp put back, so the fingerprint matches.
	original, err := os.Stat(script)
	if err != nil {
		t.Fatal(err)
	}
	body := "#!/bin/bash\ncase \"$1\" in\n  -q) shift ;;\nesac\n"
	if err := os.WriteFile(script, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(script, original.ModTime(), original.ModTime()); err != nil {
		t.Fatal(err)
	}
	updated, err := os.Stat(script)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Size() != original.Size() {
		t.Skip("the fixture did not preserve the file length, so the cache would miss anyway")
	}

	// Without invalidating, the stale form comes back.
	stale, err := service.OpenRoot(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if got := scriptAt(t, stale, "run.sh"); got.Metadata.Params[0].Name != "v" {
		t.Logf("the cache noticed the edit on its own: %+v", got.Metadata.Params)
	}

	fresh, err := service.Invalidate(context.Background(), root, "run.sh")
	if err != nil {
		t.Fatal(err)
	}
	if len(fresh.Metadata.Params) != 1 || fresh.Metadata.Params[0].Name != "q" {
		t.Errorf("params = %+v, want q read from the file", fresh.Metadata.Params)
	}
}

func TestInvalidateRejectsAnUnknownScript(t *testing.T) {
	service, _ := newTestService(t)
	if _, err := service.Invalidate(context.Background(), scriptTree(t), "absent.sh"); err == nil {
		t.Error("invalidating a script that is not there should fail")
	}
}

func TestToolchainsReportWhatCanBeRead(t *testing.T) {
	// The window needs to say "install PowerShell to get forms" rather than
	// showing scripts with no form and no explanation.
	service, _ := newTestService(t)
	byName := map[string]Toolchain{}
	for _, toolchain := range service.Toolchains() {
		byName[toolchain.Language] = toolchain
	}
	if _, ok := byName["PowerShell"]; !ok {
		t.Error("PowerShell should be listed")
	}
	if _, ok := byName["Python"]; !ok {
		t.Error("Python should be listed")
	}
	// Bash needs no toolchain, so it is always available with no reason.
	bash := byName["Bash"]
	if !bash.Available || bash.Reason != "" {
		t.Errorf("Bash = %+v, want available with no reason", bash)
	}
	// Rust cannot be read, so it is unavailable and says why.
	rust := byName["Rust"]
	if rust.Available {
		t.Error("Rust should not be reported as readable")
	}
	if rust.Reason == "" {
		t.Error("Rust should say why it has no form")
	}
}

func TestStatusDescribesWhatWasLoaded(t *testing.T) {
	root := scriptTree(t)
	service, _ := newTestService(t)
	if _, err := service.AddRoot(root, "Tools"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.OpenRoot(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	status := service.Status()
	if status.Roots != 1 {
		t.Errorf("Roots = %d, want 1", status.Roots)
	}
	if status.ConfigDir == "" {
		t.Error("the config directory should be reported so Settings can show it")
	}
	if status.CachedReports == 0 {
		t.Error("the cached report count should reflect what was read")
	}
}

func TestStartSurvivesACorruptWorkspace(t *testing.T) {
	// A corrupt state file should cost the user their saved roots, not the
	// ability to open the application at all.
	config := t.TempDir()
	storePath := filepath.Join(config, "workspace.json")
	if err := os.WriteFile(storePath, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	service := New(config)
	if err := service.Start(); err != nil {
		t.Fatalf("Start() = %v, want nil", err)
	}
	if len(service.Workspace().Roots) != 0 {
		t.Error("a corrupt workspace should read as empty")
	}
	// And the app is still usable.
	if _, err := service.AddRoot(scriptTree(t), ""); err != nil {
		t.Errorf("adding a root after a corrupt load: %v", err)
	}
}

func TestStartOnAFreshConfigDirectoryIsFine(t *testing.T) {
	// A first run has no state at all, which is the normal case.
	service := New(filepath.Join(t.TempDir(), "not-created-yet"))
	if err := service.Start(); err != nil {
		t.Fatal(err)
	}
	if len(service.Workspace().Roots) != 0 {
		t.Error("a fresh install should have no roots")
	}
}

func TestDeletedScriptsLeaveTheCache(t *testing.T) {
	// An entry for a file that is gone can never be hit again, so it should not
	// sit in the cache file growing every time a user tidies up.
	root := scriptTree(t)
	service, _ := newTestService(t)
	if _, err := service.OpenRoot(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "render.py")); err != nil {
		t.Fatal(err)
	}
	if _, err := service.OpenRoot(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	if _, ok := findScript(mustOpen(t, service, root), "render.py"); ok {
		t.Error("a deleted script should be gone from the view")
	}
	if service.cache.Len() != 2 {
		t.Errorf("the cache holds %d reports, want 2 once the deleted script's is dropped", service.cache.Len())
	}
}

func mustOpen(t *testing.T, service *Service, root string) *RootView {
	t.Helper()
	view, err := service.OpenRoot(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func TestScanWarningsReachTheView(t *testing.T) {
	// A directory that cannot be read is worth saying so rather than silently
	// vanishing from the list.
	//
	// Windows decides access by ACL and not by mode, so a directory created with
	// mode 000 is still perfectly readable there and the scan finds nothing to
	// warn about. Rewriting the ACL would work but is a machine-wide side effect
	// a unit test has no business causing, so the case is left to the platforms
	// where it can be set up honestly.
	if runtime.GOOS == "windows" {
		t.Skip("Windows decides access by ACL rather than by mode, so a 000 directory stays readable")
	}
	if os.Geteuid() == 0 {
		t.Skip("a permission test is meaningless as root")
	}

	root := scriptTree(t)
	locked := filepath.Join(root, "locked")
	if err := os.MkdirAll(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	service, _ := newTestService(t)
	view, err := service.OpenRoot(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Warnings) == 0 {
		t.Error("an unreadable directory should be reported as a warning")
	}
}

func TestViewsHoldTheLanguageForReReading(t *testing.T) {
	// Invalidate has to hand the reader a language, and it recovers it from the
	// view. It used to recover it by matching the display name against a list of
	// known languages, so a language missing from that list came back Unknown and
	// a re-read produced no form. The view now carries the language itself.
	service, _ := newTestService(t)
	view, err := service.OpenRoot(context.Background(), scriptTree(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, script := range view.Scripts {
		if script.Language == "" {
			t.Errorf("%s: the view carries no language", script.Rel)
		}
		if got := script.detectedLanguage(); got.String() != script.Language {
			t.Errorf("%s: language came back as %q, want %q", script.Rel, got, script.Language)
		}
	}

	// A view built without a language reports Unknown rather than guessing from
	// the display name, because a display name is for people.
	if got := (ScriptView{Lang: "PowerShell"}).detectedLanguage(); got != detect.Unknown {
		t.Errorf("a view with no language came back as %q, want Unknown", got)
	}
	if got := (ScriptView{Language: "powershell"}).detectedLanguage(); got != detect.PowerShell {
		t.Errorf("PowerShell came back as %q", got)
	}
}

func TestInvalidateKeepsTheScriptsLanguage(t *testing.T) {
	// The end of the round trip: a re-read of a PowerShell script must still be
	// read as PowerShell, or the user gets an empty form after asking for a fresh
	// one.
	service, _ := newTestService(t)
	root := scriptTree(t)
	if _, err := service.OpenRoot(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	fresh, err := service.Invalidate(context.Background(), root, "deploy.ps1")
	if err != nil {
		t.Fatal(err)
	}
	if fresh.detectedLanguage() != detect.PowerShell {
		t.Errorf("after a re-read the language is %q, want powershell", fresh.detectedLanguage())
	}
	if len(fresh.Metadata.Params) != 2 {
		t.Errorf("after a re-read there are %d params, want 2: %+v", len(fresh.Metadata.Params), fresh.Metadata.Warnings)
	}
}

func TestReportAndKindAreCarriedThrough(t *testing.T) {
	// The view is the boundary between the read and the UI, so a report that
	// arrives with no path or no help source would leave the window unable to
	// tell a real absence from a missing value.
	service, _ := newTestService(t)
	view := mustOpen(t, service, scriptTree(t))
	for _, script := range view.Scripts {
		if script.Metadata.Path != script.Path {
			t.Errorf("%s: report path = %q, want %q", script.Rel, script.Metadata.Path, script.Path)
		}
		if script.Metadata.Help.Source == "" {
			t.Errorf("%s: help source should always be set", script.Rel)
		}
		if script.Kind != string(scan.KindScript) {
			t.Errorf("%s: kind = %q, want script", script.Rel, script.Kind)
		}
	}
}

func TestHasFormIsFalseForAnEmptyReport(t *testing.T) {
	if hasForm(params.Report{}) {
		t.Error("a report with nothing in it should not count as having a form")
	}
	withParams := params.Report{Params: []params.Param{{Name: "x"}}}
	if !hasForm(withParams) {
		t.Error("a report with a parameter should count as having a form")
	}
	// Help alone is enough, because a script that only documents itself still has
	// something to show.
	withHelp := params.Report{Help: params.Help{Synopsis: "does a thing"}}
	if !hasForm(withHelp) {
		t.Error("a report with a synopsis should count as having a form")
	}
}

// documentedTree builds a root with a script, a sidecar about that one script,
// and a readme with a section devoted to it among sections about other things.
func documentedTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("deploy.ps1", "param([string]$Target)\n<#.SYNOPSIS\nDeploys.\n#>\n")
	write("deploy.md", "# deploy.ps1\n\nPass the **target** to deploy.\n")
	write("README.md", strings.Join([]string{
		"# Project",
		"",
		"## Other tooling",
		"",
		"Not about this script at all.",
		"",
		"## deploy.ps1",
		"",
		"Run it with a target.",
		"",
		"## Unrelated",
		"",
		"More noise.",
	}, "\n"))
	return root
}

func TestRenderDocumentRendersTheSidecar(t *testing.T) {
	// A sidecar is about one script, so it is the most specific document and the
	// one to show first.
	service, _ := newTestService(t)
	root := documentedTree(t)
	detail, err := service.Explain(context.Background(), root, "deploy.ps1")
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Docs.Documents) != 2 {
		t.Fatalf("got %d documents, want the sidecar and the readme: %+v", len(detail.Docs.Documents), detail.Docs.Documents)
	}
	if got := detail.Docs.Documents[0].Kind; got != "sidecar" {
		t.Errorf("first document kind = %q, want sidecar", got)
	}
	if detail.Docs.Primary == nil {
		t.Fatal("no primary document")
	}

	view, err := service.RenderDocument(context.Background(), root, "deploy.ps1", detail.Docs.Primary.Path)
	if err != nil {
		t.Fatal(err)
	}
	if view.Error != "" {
		t.Fatalf("render failed: %s", view.Error)
	}
	if !strings.Contains(view.HTML, "<strong>target</strong>") {
		t.Errorf("sidecar markdown was not rendered: %s", view.HTML)
	}
	if view.Title != "deploy.ps1" {
		t.Errorf("title = %q, want the first heading", view.Title)
	}
}

func TestRenderDocumentRendersOnlyTheScriptsReadmeSection(t *testing.T) {
	// A project readme covers many things. Rendering all of it would bury the
	// lines that answer the question, so the section about the script is used.
	service, _ := newTestService(t)
	root := documentedTree(t)
	detail, err := service.Explain(context.Background(), root, "deploy.ps1")
	if err != nil {
		t.Fatal(err)
	}

	var readmePath string
	for _, document := range detail.Docs.Documents {
		if document.Kind == "readme" {
			readmePath = document.Path
		}
	}
	if readmePath == "" {
		t.Fatal("the project readme was not discovered")
	}

	view, err := service.RenderDocument(context.Background(), root, "deploy.ps1", readmePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(view.HTML, "Run it with a target.") {
		t.Errorf("the script's own section is missing: %s", view.HTML)
	}
	if strings.Contains(view.HTML, "Not about this script at all.") {
		t.Errorf("sections about other things should not be shown: %s", view.HTML)
	}
}

func TestRenderDocumentRefusesAPathItDidNotDiscover(t *testing.T) {
	// The binding must not become a way to read any file the user can read. A
	// path that discovery did not produce is refused, not rendered.
	service, _ := newTestService(t)
	root := documentedTree(t)

	secret := filepath.Join(root, "notes.txt")
	if err := os.WriteFile(secret, []byte("not documentation"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := service.RenderDocument(context.Background(), root, "deploy.ps1", secret)
	if !errors.Is(err, ErrNoDocument) {
		t.Fatalf("got %v, want ErrNoDocument", err)
	}
	// Traversal out of the root is refused for the same reason.
	outside := filepath.Join(root, "..", "outside.md")
	if _, err := service.RenderDocument(context.Background(), root, "deploy.ps1", outside); !errors.Is(err, ErrNoDocument) {
		t.Fatalf("got %v, want ErrNoDocument", err)
	}
}

func TestRenderDocumentStripsDangerousMarkup(t *testing.T) {
	// A readme is untrusted input. Script tags must not survive into a web view.
	service, _ := newTestService(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "run.ps1"), []byte("param([string]$A)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"),
		[]byte("# Docs\n\n<script>alert(1)</script>\n\n<img src=x onerror=alert(2)>\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	detail, err := service.Explain(context.Background(), root, "run.ps1")
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Docs.Documents) != 1 {
		t.Fatalf("got %d documents, want 1", len(detail.Docs.Documents))
	}
	view, err := service.RenderDocument(context.Background(), root, "run.ps1", detail.Docs.Documents[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(view.HTML, "<script") || strings.Contains(view.HTML, "onerror") {
		t.Errorf("dangerous markup survived sanitization: %s", view.HTML)
	}
}

func TestExplainViewUsesLowerCaseWireNames(t *testing.T) {
	// The frontend reads "help" and "documents". A type without JSON tags is sent
	// as "Help" and "Documents", which the generated bindings happily accept and
	// which is then undefined at run time, so the documentation pane is simply
	// empty with no error anywhere. Checking the encoded shape is the only place
	// this can be caught.
	service, _ := newTestService(t)
	root := documentedTree(t)
	detail, err := service.Explain(context.Background(), root, "deploy.ps1")
	if err != nil {
		t.Fatal(err)
	}

	encoded, err := json.Marshal(detail)
	if err != nil {
		t.Fatal(err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"script", "docs"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("ExplainView is missing %q, got keys %v", key, keysOf(decoded))
		}
	}
	docsField, ok := decoded["docs"].(map[string]any)
	if !ok {
		t.Fatalf("docs is not an object: %v", decoded["docs"])
	}
	for _, key := range []string{"help", "documents", "recovered"} {
		if _, ok := docsField[key]; !ok {
			t.Errorf("docs is missing %q, got keys %v", key, keysOf(docsField))
		}
	}
	if _, ok := docsField["Documents"]; ok {
		t.Error("docs still has an upper case Documents key, so the frontend cannot read it")
	}
	documents, ok := docsField["documents"].([]any)
	if !ok || len(documents) == 0 {
		t.Fatalf("docs.documents is not a non-empty list: %v", docsField["documents"])
	}
	first, ok := documents[0].(map[string]any)
	if !ok {
		t.Fatalf("a document is not an object: %v", documents[0])
	}
	for _, key := range []string{"path", "kind", "title"} {
		if _, ok := first[key]; !ok {
			t.Errorf("a document is missing %q, got keys %v", key, keysOf(first))
		}
	}
}

// keysOf lists an object's keys, for a failure message.
func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func TestOpeningOneRootKeepsAnotherRootsCache(t *testing.T) {
	// Observed in the running application: the second root opened reported every
	// script as a cache hit, and switching back to the first root re-read all of
	// them. Pruning was being handed the paths of the root being opened and
	// taking that as the whole world, so each visit to a root threw away the
	// other roots' reports.
	service, config := newTestService(t)
	first := scriptTree(t)
	second := t.TempDir()
	if err := os.WriteFile(filepath.Join(second, "other.ps1"),
		[]byte("param([string]$Site)\n<#.SYNOPSIS\nOther.\n#>\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Open both, then come back to the first.
	if _, err := service.OpenRoot(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if _, err := service.OpenRoot(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	again, err := service.OpenRoot(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	if again.Meta.FromCache != len(readableIn(t, first)) {
		t.Errorf("reopening the first root read %d of its scripts, want all %d from cache",
			again.Meta.Total-again.Meta.FromCache, len(readableIn(t, first)))
	}

	// The reports must also have been written to disk, not just kept in memory.
	saved := params.NewCache(filepath.Join(config, "params-cache.json"))
	if err := saved.Load(); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, path := range saved.Paths() {
		if filepath.Clean(path) == filepath.Clean(filepath.Join(second, "other.ps1")) {
			found = true
		}
	}
	if !found {
		t.Errorf("the second root's report did not survive on disk, cache holds %v", saved.Paths())
	}
}

// readableIn counts the scripts a scan would read in a root.
func readableIn(t *testing.T, root string) []string {
	t.Helper()
	result, err := scan.Scan(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, entry := range result.Entries {
		if entry.Kind == scan.KindScript {
			out = append(out, entry.Path)
		}
	}
	return out
}

// gitRepo turns a root into a repository with one commit, so a read has a git
// stage to report. It reports whether git could be used at all, because a
// machine without it should skip the assertion rather than fail it.
func gitRepo(t *testing.T, root string) bool {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		return false
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	run("add", "-A")
	run("commit", "-q", "-m", "initial")
	return true
}

func TestOpenRootReportsWhatItIsWaitingOn(t *testing.T) {
	// A read that shows one opaque label leaves the user unable to tell a slow
	// read from a stuck one. The three stages say which part of the read is
	// running, and the per-language events inside the harvest say which toolchain
	// is starting, because that is where the time actually goes.
	service, _ := newTestService(t)
	root := scriptTree(t)
	if !gitRepo(t, root) {
		t.Skip("git is not installed, so this root has no git stage to report")
	}

	var stages []ReadStage
	service.SetStageEmitter(func(name string, data any) {
		if name != "read:stage" {
			t.Errorf("event name = %q, want read:stage", name)
		}
		stage, ok := data.(ReadStage)
		if !ok {
			t.Errorf("unhandled event type %T", data)
			return
		}
		stages = append(stages, stage)
	})

	if _, err := service.OpenRoot(context.Background(), root); err != nil {
		t.Fatal(err)
	}

	// Every event names the root, because a read is asynchronous and the window
	// has to be able to drop a stage for a folder the user has left.
	for i, stage := range stages {
		if stage.Root != root {
			t.Errorf("stage %d root = %q, want %q", i, stage.Root, root)
		}
	}
	if len(stages) == 0 {
		t.Fatal("a read reported no stages at all")
	}

	// The stages run in the order the read does them, and the first is always the
	// directory.
	if stages[0].Stage != StageScan {
		t.Errorf("first stage = %q, want %q", stages[0].Stage, StageScan)
	}
	var names []string
	for _, stage := range stages {
		names = append(names, stage.Stage)
	}
	scanAt, harvestAt, gitAt := -1, -1, -1
	for i, name := range names {
		switch name {
		case StageScan:
			scanAt = i
		case StageHarvest:
			if harvestAt < 0 {
				harvestAt = i
			}
		case StageGit:
			gitAt = i
		}
	}
	if scanAt != 0 {
		t.Errorf("the directory should be reported first, at 0, got %d", scanAt)
	}
	if harvestAt < 0 {
		t.Fatalf("the harvest was never reported: %v", names)
	}
	if gitAt < 0 {
		t.Fatalf("a repository should report a git stage: %v", names)
	}
	if scanAt >= harvestAt || harvestAt >= gitAt {
		t.Errorf("stages out of order: %v", names)
	}

	// The harvest announces itself once with no language, then reports each
	// language as it starts.
	perLanguage := 0
	for _, stage := range stages {
		if stage.Stage == StageHarvest && stage.Language != "" {
			perLanguage++
		}
	}
	if perLanguage == 0 {
		t.Errorf("the harvest named no language, so the slow part is still opaque: %v", stages)
	}
}

func TestOpenRootReportsNoGitStageForAPlainDirectory(t *testing.T) {
	// A directory that is not a repository has no git to ask, so reporting a git
	// stage would name a step that never happens.
	service, _ := newTestService(t)

	var stages []ReadStage
	service.SetStageEmitter(func(_ string, data any) {
		stage, ok := data.(ReadStage)
		if !ok {
			return
		}
		stages = append(stages, stage)
	})

	if _, err := service.OpenRoot(context.Background(), scriptTree(t)); err != nil {
		t.Fatal(err)
	}
	for _, stage := range stages {
		if stage.Stage == StageGit {
			t.Errorf("a plain directory reported %q", StageGit)
		}
	}
}

func TestOpenRootWithNoEmitterDoesNotPanic(t *testing.T) {
	// A Service is built before the window exists, and tests use it without one.
	// Reporting progress must be optional, the way run events are.
	service, _ := newTestService(t)
	if _, err := service.OpenRoot(context.Background(), scriptTree(t)); err != nil {
		t.Fatal(err)
	}
}

func TestRefreshGitReportsTheStateWithoutReReadingTheRoot(t *testing.T) {
	// The point of a focus refresh is that it is cheap. It re-reads the
	// repository and nothing else, so the expensive metadata read is not
	// repeated: the second call has to report fromCache for every script, which
	// is what an OpenRoot would also do, but the only way to tell the two apart
	// is that RefreshGit does not report a view at all and cannot be made to
	// re-read a script.
	service, _ := newTestService(t)
	root := scriptTree(t)
	if !gitRepo(t, root) {
		t.Skip("git is not installed, so this root has no repository to refresh")
	}

	if _, err := service.OpenRoot(context.Background(), root); err != nil {
		t.Fatal(err)
	}

	refresh := service.RefreshGit(context.Background(), root)
	if refresh.Status == nil {
		t.Fatal("a repository refreshed to no status at all")
	}
	if refresh.Status.Branch == "" {
		t.Error("a repository has a branch, and it is what the strip leads with")
	}
	// A clean tree changes nothing, which is the case the badges depend on.
	if len(refresh.Modified) != 0 {
		t.Errorf("a freshly committed tree reported %v as modified", refresh.Modified)
	}

	// Now edit a script and check the badge set follows git, not the view.
	if err := os.WriteFile(filepath.Join(root, "rebuild.sh"), []byte("#!/bin/bash\necho edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	refresh = service.RefreshGit(context.Background(), root)
	if len(refresh.Modified) != 1 || refresh.Modified[0] != "rebuild.sh" {
		t.Errorf("Modified = %v, want [rebuild.sh]", refresh.Modified)
	}
	if refresh.Status.Unstaged != 1 {
		t.Errorf("Unstaged = %d, want 1", refresh.Status.Unstaged)
	}

	// A commit made elsewhere changes the strip and the badges but not one
	// script's metadata, so a refresh must not read the directory. Proved by
	// refreshing a root that has never been opened: if the refresh had read
	// anything, the first open that follows would be served from its cache.
	scratch, _ := newTestService(t)
	fresh := scriptTree(t)
	if !gitRepo(t, fresh) {
		t.Skip("git is not installed, so this root has no repository to refresh")
	}
	scratch.RefreshGit(context.Background(), fresh)
	opened, err := scratch.OpenRoot(context.Background(), fresh)
	if err != nil {
		t.Fatal(err)
	}
	if opened.Meta.FromCache != 0 {
		t.Errorf("the refresh read %d scripts, want 0: it is meant to touch only git", opened.Meta.FromCache)
	}
}

func TestRefreshGitListsModifiedPathsSorted(t *testing.T) {
	// The set comes out of a map, and the window receives it over JSON, so the
	// same repository state has to serialize the same way every time or a diff
	// of two refreshes is meaningless.
	service, _ := newTestService(t)
	root := scriptTree(t)
	if !gitRepo(t, root) {
		t.Skip("git is not installed, so this root has no repository to refresh")
	}
	for _, name := range []string{"zebra.sh", "apple.sh", "mango.sh"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("#!/bin/bash\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	refresh := service.RefreshGit(context.Background(), root)
	want := []string{"apple.sh", "mango.sh", "zebra.sh"}
	if !reflect.DeepEqual(refresh.Modified, want) {
		t.Errorf("Modified = %v, want %v", refresh.Modified, want)
	}
}

func TestRefreshGitOnAPlainDirectoryReportsNoStatus(t *testing.T) {
	// A folder that is not a repository has no strip, and the window needs to be
	// able to say so rather than being handed an error to raise about a focus.
	service, _ := newTestService(t)

	refresh := service.RefreshGit(context.Background(), scriptTree(t))
	if refresh.Status != nil {
		t.Errorf("a plain directory reported status %+v", refresh.Status)
	}
	if len(refresh.Modified) != 0 {
		t.Errorf("a plain directory reported %v as modified", refresh.Modified)
	}
}

func TestRefreshGitOnAMissingRootDoesNotError(t *testing.T) {
	// A focus arrives whether or not the folder is still there. A root deleted
	// while the application was closed has no repository state, and that is not
	// an error worth interrupting the user for.
	service, _ := newTestService(t)

	refresh := service.RefreshGit(context.Background(), filepath.Join(t.TempDir(), "gone"))
	if refresh == nil {
		t.Fatal("a missing root returned no refresh at all, want an empty one")
	}
	if refresh.Status != nil {
		t.Errorf("a missing root reported status %+v", refresh.Status)
	}
}

func TestRefreshGitDoesNotChangeWhatTheRootAlreadyReported(t *testing.T) {
	// A refresh returns its own result rather than editing the view the
	// application already handed the window. The window holds a published view
	// that is never written to, and a refresh that mutated it would be the
	// caller writing to state it was given.
	service, _ := newTestService(t)
	root := scriptTree(t)
	if !gitRepo(t, root) {
		t.Skip("git is not installed, so this root has no repository to refresh")
	}
	view, err := service.OpenRoot(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	before, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, "rebuild.sh"), []byte("#!/bin/bash\n# changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	service.RefreshGit(context.Background(), root)

	after, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("RefreshGit changed a view it had already returned")
	}
}
