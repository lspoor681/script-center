package workspace

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lspoor/script-center/internal/detect"
	"github.com/lspoor/script-center/internal/scan"
)

func TestAddRootNormalises(t *testing.T) {
	w := New()
	// A relative path with a trailing separator must collapse to the same
	// absolute entry as its canonical form.
	w.AddRoot(".", "here")
	w.AddRoot(filepath.Join("."), "")

	if len(w.Roots) != 1 {
		t.Fatalf("added the same root twice: %+v", w.Roots)
	}
	if !filepath.IsAbs(w.Roots[0].Path) {
		t.Errorf("Path = %q, want absolute", w.Roots[0].Path)
	}
	// The explicit name from the first call must survive, since a second add
	// with no name must not overwrite it with a default.
	if w.Roots[0].Name != "here" {
		t.Errorf("Name = %q, want the explicit name %q", w.Roots[0].Name, "here")
	}
}

func TestAddRootDefaultsNameToFinalPathElement(t *testing.T) {
	w := New()
	dir := filepath.Join(t.TempDir(), "my-scripts")

	w.AddRoot(dir, "")

	if w.Roots[0].Name != "my-scripts" {
		t.Errorf("Name = %q, want %q", w.Roots[0].Name, "my-scripts")
	}
}

func TestAddRootIsIdempotent(t *testing.T) {
	w := New()
	dir := t.TempDir()

	w.AddRoot(dir, "one")
	w.AddRoot(dir, "two")
	if len(w.Roots) != 1 {
		t.Fatalf("expected one root, got %d", len(w.Roots))
	}
	// A duplicate must not silently rename an existing root.
	if w.Roots[0].Name != "one" {
		t.Errorf("Name = %q, want the original %q", w.Roots[0].Name, "one")
	}
}

func TestAddRootRejectsNestedRoot(t *testing.T) {
	w := New()
	parent := t.TempDir()
	child := filepath.Join(parent, "sub")

	w.AddRoot(parent, "parent")
	w.AddRoot(child, "child")

	if len(w.Roots) != 1 {
		t.Fatalf("expected the nested root to be rejected, got %+v", w.Roots)
	}
	if w.Roots[0].Name != "parent" {
		t.Errorf("Name = %q, want parent", w.Roots[0].Name)
	}
}

func TestAddWiderRootReplacesNarrower(t *testing.T) {
	w := New()
	parent := t.TempDir()
	child := filepath.Join(parent, "sub")

	// Adding the child first, then its parent, should end with just the parent:
	// scanning both would list the child's scripts twice.
	w.AddRoot(child, "child")
	w.AddRoot(parent, "parent")

	if len(w.Roots) != 1 {
		t.Fatalf("expected one root, got %+v", w.Roots)
	}
	if w.Roots[0].Name != "parent" {
		t.Errorf("Name = %q, want parent", w.Roots[0].Name)
	}
}

func TestRemoveRootAlsoDropsFavorites(t *testing.T) {
	w := New()
	dir := t.TempDir()
	other := t.TempDir()

	w.AddRoot(dir, "a")
	w.AddRoot(other, "b")
	w.ToggleFavorite(Ref{Root: dir, Rel: "run.sh"})
	w.ToggleFavorite(Ref{Root: other, Rel: "keep.sh"})

	w.RemoveRoot(dir)

	if w.HasRoot(dir) {
		t.Error("root still present after removal")
	}
	if w.IsFavorite(Ref{Root: dir, Rel: "run.sh"}) {
		t.Error("a favorite outlived its root and would reappear if re-added")
	}
	if !w.IsFavorite(Ref{Root: other, Rel: "keep.sh"}) {
		t.Error("removing one root dropped an unrelated favorite")
	}
}

func TestFavoritesToggle(t *testing.T) {
	w := New()
	dir := t.TempDir()
	ref := Ref{Root: dir, Rel: "scripts/run.sh"}

	if w.IsFavorite(ref) {
		t.Fatal("a fresh workspace reported a favorite")
	}
	if on := w.ToggleFavorite(ref); !on {
		t.Error("ToggleFavorite returned false on the first call")
	}
	if !w.IsFavorite(ref) {
		t.Error("favorite not recorded")
	}
	if on := w.ToggleFavorite(ref); on {
		t.Error("ToggleFavorite returned true on the second call")
	}
	if w.IsFavorite(ref) {
		t.Error("favorite not cleared")
	}
	if len(w.Favorites) != 0 {
		t.Errorf("Favorites = %v, want empty", w.Favorites)
	}
}

func TestFavoritesAreScopedPerRoot(t *testing.T) {
	w := New()
	a, b := t.TempDir(), t.TempDir()
	w.AddRoot(a, "a")
	w.AddRoot(b, "b")

	ref := Ref{Root: a, Rel: "run.sh"}
	w.ToggleFavorite(ref)

	if w.IsFavorite(Ref{Root: b, Rel: "run.sh"}) {
		t.Error("the same relative path under another root was reported as favorited")
	}
	if got := w.FavoritesUnder(a); len(got) != 1 {
		t.Errorf("FavoritesUnder(a) = %v, want one entry", got)
	}
	if got := w.FavoritesUnder(b); len(got) != 0 {
		t.Errorf("FavoritesUnder(b) = %v, want none", got)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	root := t.TempDir()

	w := New()
	w.AddRoot(root, "my scripts")
	w.ToggleFavorite(Ref{Root: root, Rel: "a b/run.sh"})

	if err := store.Save(w); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Roots) != 1 || got.Roots[0].Name != "my scripts" {
		t.Errorf("Roots = %+v, want the saved root", got.Roots)
	}
	// A path with a space exercises the JSON encoding of keys.
	if !got.IsFavorite(Ref{Root: root, Rel: "a b/run.sh"}) {
		t.Errorf("favorite did not survive the round trip: %+v", got.Favorites)
	}
	if got.Version != currentVersion {
		t.Errorf("Version = %d, want %d", got.Version, currentVersion)
	}
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	store := NewStore(t.TempDir())

	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load on a missing file: %v", err)
	}
	if len(got.Roots) != 0 {
		t.Errorf("Roots = %+v, want empty", got.Roots)
	}
	if got.Version != currentVersion {
		t.Errorf("Version = %d, want %d", got.Version, currentVersion)
	}
}

func TestLoadRejectsNewerVersion(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	future := filepath.Join(dir, FileName)
	body := `{"version":99,"roots":[{"path":"/tmp","name":"x"}]}`
	if err := os.WriteFile(future, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := store.Load(); !errors.Is(err, ErrVersionTooNew) {
		t.Errorf("Load error = %v, want ErrVersionTooNew", err)
	}
}

func TestLoadTreatsMissingVersionAsCurrent(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	root := t.TempDir()
	body := `{"roots":[{"path":` + jsonString(root) + `,"name":"hand written"}]}`
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Roots) != 1 || got.Roots[0].Name != "hand written" {
		t.Errorf("Roots = %+v", got.Roots)
	}
}

func TestLoadDropsOrphanedAndDuplicateFavorites(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	live := t.TempDir()

	body := `{"roots":[{"path":` + jsonString(live) + `,"name":"live"}],` +
		`"favorites":[` +
		`{"root":` + jsonString(live) + `,"rel":"a.sh"},` +
		`{"root":` + jsonString(live) + `,"rel":"a.sh"},` +
		`{"root":` + jsonString(filepath.Join(live, "gone")) + `,"rel":"b.sh"}]}`
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Favorites) != 1 {
		t.Errorf("Favorites = %+v, want one entry (deduplicated, orphan dropped)", got.Favorites)
	}
}

func TestSaveIsAtomic(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	if err := store.Save(New()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// A failed save must leave the previous file readable rather than
	// truncated. Making the save fail is easiest by making the path a
	// directory, which the rename cannot replace.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var workspaceFile string
	for _, e := range entries {
		if e.Name() == FileName {
			workspaceFile = e.Name()
		}
	}
	if workspaceFile == "" {
		t.Fatal("workspace file was not written")
	}

	before, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}

	// Saving again over a valid file must still succeed and leave no temp
	// files behind.
	if err := store.Save(New()); err != nil {
		t.Fatalf("second Save: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatalf("read after save: %v", err)
	}
	if string(before) != string(after) {
		t.Error("rewriting an unchanged workspace altered the file")
	}

	entries, err = os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".workspaces-") {
			t.Errorf("temporary file %q was left behind", e.Name())
		}
	}
}

func TestSaveCreatesConfigDir(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "nested", "deeper"))
	if err := store.Save(New()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(store.Path()); err != nil {
		t.Errorf("workspace file not created: %v", err)
	}
}

func TestPruneMissing(t *testing.T) {
	present := t.TempDir()
	absent := filepath.Join(t.TempDir(), "gone")

	w := New()
	w.AddRoot(present, "present")
	w.AddRoot(absent, "absent")
	w.ToggleFavorite(Ref{Root: absent, Rel: "x.sh"})

	removed := w.PruneMissing(func(p string) bool { return p == present })

	if len(removed) != 1 || removed[0] != absent {
		t.Errorf("PruneMissing = %v, want [%s]", removed, absent)
	}
	if len(w.Roots) != 1 {
		t.Errorf("Roots = %+v, want the present root only", w.Roots)
	}
	if len(w.Favorites) != 0 {
		t.Errorf("Favorites = %+v, want the orphan dropped", w.Favorites)
	}
}

func TestRootsExist(t *testing.T) {
	present := t.TempDir()
	absent := filepath.Join(t.TempDir(), "gone")

	w := New()
	w.AddRoot(present, "present")
	w.AddRoot(absent, "absent")

	missing := w.RootsExist(func(p string) bool { return p == present })
	if len(missing) != 1 || missing[0].Path != absent {
		t.Errorf("RootsExist = %+v, want just the absent root", missing)
	}
}

func jsonString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// --- related-script suggestions -------------------------------------------

func entry(root, rel, dir, name string, lang detect.Language) scan.Entry {
	return scan.Entry{
		Root: root, Rel: rel, Dir: dir, Name: name,
		Kind: scan.KindScript, Lang: lang,
	}
}

func TestSuggestSameDirectoryWins(t *testing.T) {
	root := "/w"
	target := entry(root, "scripts/deploy.sh", "scripts", "deploy.sh", detect.Bash)
	all := []scan.Entry{
		target,
		entry(root, "scripts/backup.sh", "scripts", "backup.sh", detect.Bash),
		entry(root, "other/thing.sh", "other", "thing.sh", detect.Bash),
		entry(root, "scripts/deploy.ps1", "scripts", "deploy.ps1", detect.PowerShell),
	}

	got := Suggest(target, all, 0)
	if len(got) == 0 {
		t.Fatal("no suggestions")
	}
	if got[0].Entry.Rel != "scripts/backup.sh" {
		t.Errorf("first suggestion = %q, want the same-directory script", got[0].Entry.Rel)
	}
	if got[0].Reason == "" {
		t.Error("suggestion has no reason, so the user cannot judge it")
	}
}

func TestSuggestSameStemAcrossLanguages(t *testing.T) {
	root := "/w"
	target := entry(root, "deploy.sh", "", "deploy.sh", detect.Bash)
	all := []scan.Entry{
		target,
		entry(root, "windows/deploy.ps1", "windows", "deploy.ps1", detect.PowerShell),
		entry(root, "elsewhere/unrelated.sh", "elsewhere", "unrelated.sh", detect.Bash),
	}

	got := Suggest(target, all, 0)
	if len(got) == 0 || got[0].Entry.Rel != "windows/deploy.ps1" {
		t.Fatalf("suggestions = %+v, want the PowerShell port first", got)
	}
	if got[0].Score <= scoreSameDir {
		t.Errorf("same-stem score %d should outrank a same-directory match", got[0].Score)
	}
}

func TestSuggestSharedNameToken(t *testing.T) {
	root := "/w"
	// Different directories, so the same-directory rules do not apply and the
	// shared name token is the only thing connecting them.
	target := entry(root, "staging/backup-staging.sh", "staging", "backup-staging.sh", detect.Bash)
	all := []scan.Entry{
		target,
		entry(root, "production/restore-staging.sh", "production", "restore-staging.sh", detect.Bash),
	}

	got := Suggest(target, all, 0)
	if len(got) != 1 {
		t.Fatalf("suggestions = %+v, want one", got)
	}
	if got[0].Score != scoreSharedNameToken {
		t.Errorf("Score = %d, want %d", got[0].Score, scoreSharedNameToken)
	}
	if got[0].Reason == "" {
		t.Error("a shared-token suggestion should explain which token matched")
	}
}

func TestSuggestNeverCrossesRoots(t *testing.T) {
	target := entry("/a", "run.sh", "", "run.sh", detect.Bash)
	other := entry("/b", "run.sh", "", "run.sh", detect.Bash)

	if got := Suggest(target, []scan.Entry{target, other}, 0); len(got) != 0 {
		t.Errorf("suggestions = %+v, want none across roots", got)
	}
}

func TestSuggestExcludesSelfAndProjects(t *testing.T) {
	root := "/w"
	target := entry(root, "run.sh", "", "run.sh", detect.Bash)
	project := scan.Entry{
		Root: root, Rel: "tool", Dir: "", Name: "tool",
		Kind: scan.KindProject, Lang: detect.Rust,
	}
	sameNameAsProject := entry(root, "tool", "", "tool", detect.Bash)

	got := Suggest(target, []scan.Entry{target, project, sameNameAsProject}, 0)
	for _, s := range got {
		if s.Entry.Rel == target.Rel {
			t.Error("the target was suggested to itself")
		}
		if s.Entry.Kind == scan.KindProject {
			t.Error("a project was suggested as a peer script")
		}
	}
}

func TestSuggestIsDeterministicAndLimited(t *testing.T) {
	root := "/w"
	target := entry(root, "run.sh", "", "run.sh", detect.Bash)
	var all []scan.Entry
	for _, name := range []string{"a.sh", "b.sh", "c.sh", "d.sh", "e.sh", "f.sh", "g.sh", "h.sh"} {
		all = append(all, entry(root, name, "", name, detect.Bash))
	}

	first := Suggest(target, all, 0)
	if len(first) > suggestionLimit {
		t.Fatalf("returned %d suggestions, want at most %d", len(first), suggestionLimit)
	}
	// Equal scores are common here, so the tiebreak is what keeps the panel
	// from reshuffling between refreshes.
	for range 5 {
		next := Suggest(target, all, 0)
		for i := range next {
			if next[i].Entry.Rel != first[i].Entry.Rel {
				t.Fatalf("order changed at %d: %q then %q", i, first[i].Entry.Rel, next[i].Entry.Rel)
			}
		}
	}

	if got := Suggest(target, all, 2); len(got) != 2 {
		t.Errorf("limit 2 returned %d suggestions", len(got))
	}
}

func TestNameStemAndTokens(t *testing.T) {
	for _, tc := range []struct{ in, wantStem string }{
		{"backup.sh", "backup"},
		{"backup.ps1", "backup"},
		{"Makefile", "Makefile"},
		{"a.b.py", "a.b"},
	} {
		if got := nameStem(tc.in); got != tc.wantStem {
			t.Errorf("nameStem(%q) = %q, want %q", tc.in, got, tc.wantStem)
		}
	}

	tokens := nameTokens("backup-user_data+report.sh")
	for _, want := range []string{"backup", "user", "data", "report"} {
		if !tokens[want] {
			t.Errorf("nameTokens missing %q in %v", want, tokens)
		}
	}
}
