package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"time"

	"github.com/lspoor/script-center/internal/git"
	"github.com/lspoor/script-center/internal/params"
	"github.com/lspoor/script-center/internal/scan"
	"github.com/lspoor/script-center/internal/workspace"
)

// OpenRoot scans a root and reads metadata for every script in it.
//
// The read is the slow part, so an unchanged script is served from the cache and
// only the rest start a toolchain. The result is memoized, because reopening the
// window should not rescan every root the user has.
func (s *Service) OpenRoot(ctx context.Context, root string) (*RootView, error) {
	absolute, err := existingDir(root)
	if err != nil {
		return nil, err
	}

	// The root travels in the context so the parameter reader's progress hook,
	// which is called from inside the read, can name the root it is reporting
	// on. Two reads can overlap, so this cannot live on the Service.
	ctx = withReadRoot(ctx, absolute)

	started := time.Now()
	s.emitStage(ReadStage{Root: absolute, Stage: StageScan})
	result, err := scan.Scan(ctx, absolute)
	if err != nil {
		return nil, err
	}

	// Only real scripts are read. A project directory has no parameters of its
	// own, and reading it would produce a confusing report about a directory.
	readable := make([]scan.Entry, 0, len(result.Entries))
	for _, entry := range result.Entries {
		if entry.Kind == scan.KindScript {
			readable = append(readable, entry)
		}
	}

	// Which scripts are cached is decided before the read, because that is what
	// the Meta numbers describe. It is decided over the same set that will be
	// read, so a project directory cannot be counted as a cached script.
	cachedBefore := map[string]bool{}
	for _, entry := range readable {
		if _, ok := s.cache.Lookup(entry.Path, entry.Size, entry.ModTime, string(entry.Lang)); ok {
			cachedBefore[entry.Path] = true
		}
	}

	s.emitStage(ReadStage{Root: absolute, Stage: StageHarvest})
	reports, err := s.reader.Read(ctx, scriptsOf(readable))
	if err != nil {
		// A read failure is reported on the root rather than replacing the whole
		// view with nothing, so the user still sees their scripts.
		result.Warnings = append(result.Warnings, fmt.Sprintf("some scripts could not be read: %v", err))
	}
	byPath := make(map[string]params.Report, len(reports))
	for _, report := range reports {
		byPath[report.Path] = report
	}

	view := &RootView{
		Root:     result.Root,
		Name:     s.rootName(absolute),
		Source:   string(result.Source),
		IsRepo:   result.IsRepo,
		Warnings: result.Warnings,
		Scripts:  make([]ScriptView, 0, len(result.Entries)),
	}
	for _, entry := range result.Entries {
		view.Scripts = append(view.Scripts, s.viewFor(entry, byPath[entry.Path], cachedBefore[entry.Path]))
	}

	// The git summary is part of a root's identity: it is what tells the user
	// whether the folder in front of them is in sync with anywhere. It is
	// best-effort, because a repository that cannot be described is still a
	// repository with scripts in it, and a root that is not one simply has no
	// git at all.
	if result.IsRepo {
		s.emitStage(ReadStage{Root: absolute, Stage: StageGit})
		view.Git = s.gitFor(ctx, absolute)
		markModified(view.Scripts, gitModified(ctx, absolute))
	}

	// Reports for scripts that have been deleted or renamed can never be hit
	// again, so they go now rather than accumulating in the file. Only vanished
	// files are dropped: the cache holds every root in the workspace, and this
	// call knows about one of them.
	s.cache.PruneStale()
	if err := s.cache.Save(); err != nil {
		view.Warnings = append(view.Warnings, fmt.Sprintf("the parameter cache was not saved: %v", err))
	}

	hits, misses := s.cache.Stats()
	view.Meta = Meta{
		DurationMS:  time.Since(started).Milliseconds(),
		FromCache:   len(cachedBefore),
		Read:        len(readable) - len(cachedBefore),
		Total:       len(readable),
		CacheHits:   hits,
		CacheMisses: misses,
	}
	view.Warnings = mergeWarnings(s.startupWarnings(), view.Warnings)

	s.publishRoot(absolute, view)
	return view, nil
}

// publishRoot puts a finished view into the memo, replacing any earlier one.
//
// The view is treated as immutable once it is in the memo. Anything that needs to
// change one calls replaceScript instead, so a request reading a view cannot see
// it change underneath.
func (s *Service) publishRoot(absolute string, view *RootView) {
	s.rootsMu.Lock()
	defer s.rootsMu.Unlock()
	s.roots[absolute] = view
}

// cachedRoot returns a memoized root view, scanning it if it is not there.
func (s *Service) cachedRoot(ctx context.Context, root string) (*RootView, error) {
	absolute, err := existingDir(root)
	if err != nil {
		return nil, err
	}
	s.rootsMu.Lock()
	view, ok := s.roots[absolute]
	s.rootsMu.Unlock()
	if ok {
		return view, nil
	}
	return s.OpenRoot(ctx, absolute)
}

// Invalidate forgets what is cached for one script, so the next read sees the
// file as it is now.
//
// This exists because the cache keys on size and timestamp, which an edit that
// preserves both can defeat. A user who has just edited a script and sees a stale
// form needs a way to be certain, and this is that way.
func (s *Service) Invalidate(ctx context.Context, root, rel string) (*ScriptView, error) {
	view, err := s.cachedRoot(ctx, root)
	if err != nil {
		return nil, err
	}
	index, ok := indexOfScript(view, rel)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNoScript, rel)
	}
	script := view.Scripts[index]
	s.cache.Invalidate(script.Path)

	// Re-read just this one script rather than the whole root, which is what the
	// user is waiting on.
	entry := scan.Entry{
		Root: script.Root, Path: script.Path, Rel: script.Rel, Name: script.Name,
		Dir: script.Dir, Kind: scan.Kind(script.Kind), Lang: script.detectedLanguage(),
		Size: script.Size,
	}
	// The stat below is the authority on what the file looks like now. The stored
	// timestamp is only a starting point, so a failure to parse it is not worth
	// reporting; a stat that fails leaves the entry at zero and the read reports
	// the problem on the script.
	if parsed, err := time.Parse(time.RFC3339, script.ModTime); err == nil {
		entry.ModTime = parsed
	}
	if info, err := os.Stat(script.Path); err == nil {
		entry.Size = info.Size()
		entry.ModTime = info.ModTime()
	}

	reports, err := s.reader.Read(ctx, []params.Script{scriptParams(entry)})
	if err != nil {
		return nil, err
	}

	// The view is published and other requests are reading it, so the re-read
	// result goes into a replacement rather than into the shared one. A reader
	// part way through keeps the consistent view it started with.
	updated := *view
	updated.Scripts = slices.Clone(view.Scripts)
	if len(reports) == 1 {
		updated.Scripts[index] = s.viewFor(entry, reports[0], false)
	} else {
		updated.Scripts[index].Cached = false
	}
	if err := s.cache.Save(); err != nil {
		updated.Warnings = append(slices.Clone(view.Warnings),
			fmt.Sprintf("the parameter cache was not saved: %v", err))
	}
	s.publishRoot(view.Root, &updated)

	fresh := updated.Scripts[index]
	return &fresh, nil
}

// indexOfScript finds a script's position in a view by relative path.
func indexOfScript(view *RootView, rel string) (int, bool) {
	want := filepath.ToSlash(filepath.Clean(rel))
	for i := range view.Scripts {
		if view.Scripts[i].Rel == want {
			return i, true
		}
	}
	return 0, false
}

// findScript looks up a script in a root view by relative path.
func findScript(view *RootView, rel string) (*ScriptView, bool) {
	index, ok := indexOfScript(view, rel)
	if !ok {
		return nil, false
	}
	return &view.Scripts[index], true
}

// rootName returns the label a root should carry, preferring the name the user
// gave it and falling back to the directory's own name.
func (s *Service) rootName(path string) string {
	for _, root := range s.Workspace().Roots {
		if root.Path == path {
			return rootLabel(path, root.Name)
		}
	}
	return rootLabel(path, "")
}

// warnings collects the problems the app is currently aware of, without repeats.
func (s *Service) warnings() []string {
	s.rootsMu.Lock()
	collected := make([]string, 0, 8)
	for _, root := range s.roots {
		collected = append(collected, root.Warnings...)
	}
	s.rootsMu.Unlock()
	return mergeWarnings(s.startupWarnings(), collected)
}

// mergeWarnings combines warning lists, dropping repeats and sorting so the order
// does not depend on which request happened to finish first.
func mergeWarnings(lists ...[]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, list := range lists {
		for _, warning := range list {
			if seen[warning] {
				continue
			}
			seen[warning] = true
			out = append(out, warning)
		}
	}
	sort.Strings(out)
	return out
}

// scriptParams adapts a scan entry for the reader.
func scriptParams(entry scan.Entry) params.Script {
	return params.Script{
		Path:     entry.Path,
		Language: entry.Lang,
		// The scan already stat'd the file, so its numbers are the cache key and
		// the reader does not have to look again.
		Size:    entry.Size,
		ModTime: entry.ModTime,
	}
}

// scriptsOf adapts entries for the reader.
func scriptsOf(entries []scan.Entry) []params.Script {
	out := make([]params.Script, 0, len(entries))
	for _, entry := range entries {
		out = append(out, scriptParams(entry))
	}
	return out
}

// relatedFor finds the scripts related to one, using the memoized scan so that
// opening a script's documentation does not rescan the root.
func (s *Service) relatedFor(view *RootView, script *ScriptView) []workspace.Suggestion {
	result := scan.Result{Root: view.Root, IsRepo: view.IsRepo}
	for _, entry := range view.Scripts {
		if entry.Kind != string(scan.KindScript) {
			continue
		}
		result.Entries = append(result.Entries, scan.Entry{
			Root: entry.Root, Path: entry.Path, Rel: entry.Rel,
			Name: entry.Name, Dir: entry.Dir, Kind: scan.KindScript,
		})
	}
	// Suggest wants the target as a scan.Entry, which the view already carries
	// the fields for.
	target := scan.Entry{
		Root: script.Root, Path: script.Path, Rel: script.Rel,
		Name: script.Name, Dir: script.Dir, Kind: scan.KindScript,
	}
	return workspace.Suggest(target, result.Entries, relatedLimit)
}

// gitFor describes the repository state of a root, or nil when the root is not
// a repository or git cannot describe it. Everything here is best-effort: a
// repository that cannot be described is still a repository with scripts in it,
// and the user is better served by seeing those than by an error page.
func (s *Service) gitFor(ctx context.Context, root string) *GitStatus {
	state, err := git.Status(ctx, root)
	if err != nil || !state.IsRepo {
		return nil
	}
	commit, err := git.LastCommit(ctx, root)
	if err != nil {
		// A repository with no commits still has a branch and a state; the
		// commit is simply absent from its strip.
		commit = git.Commit{}
	}

	status := &GitStatus{
		Branch:     state.Branch,
		Detached:   state.Detached,
		OID:        state.OID,
		Upstream:   state.Upstream,
		Ahead:      state.Ahead,
		Behind:     state.Behind,
		Staged:     state.Staged,
		Unstaged:   state.Unstaged,
		Untracked:  state.Untracked,
		Conflicted: state.Conflicted,
		Stashed:    state.Stashed,
	}
	if commit.ShortOID != "" {
		status.Commit = &GitCommit{
			OID:      commit.OID,
			ShortOID: commit.ShortOID,
			Author:   commit.Author,
			Date:     commit.Date,
			Subject:  commit.Subject,
		}
	}
	if remote, err := git.RemoteURL(ctx, root); err == nil {
		status.Remote = remote
	}
	return status
}

// gitModified returns the set of paths git reports as changed under a root,
// keyed by the scan's relative-path form so it can be looked up per script. A
// repository that fails to be read leaves the set empty rather than failing the
// root with it.
func gitModified(ctx context.Context, root string) map[string]bool {
	statuses, err := git.Porcelain(ctx, root)
	if err != nil {
		return map[string]bool{}
	}
	changed := make(map[string]bool, len(statuses))
	for rel := range statuses {
		changed[rel] = true
	}
	return changed
}

// markModified flags every script git reports as changed, in place. The badge is
// a property of the row, not of the metadata read that started the view, so it
// is a step on the view build rather than part of it.
func markModified(scripts []ScriptView, changed map[string]bool) {
	for i := range scripts {
		if changed[scripts[i].Rel] {
			scripts[i].Modified = true
		}
	}
}
