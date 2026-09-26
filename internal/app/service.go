// Package app is the application's behavior, kept out of package main so that it
// can be tested without a window.
//
// Everything here returns plain structs with JSON tags. Wails binds those
// directly, and a test reads them without a webview, so the boundary between the
// application's logic and its presentation is one struct field at a time rather
// than a layer of translation that can drift.
package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lspoor/script-center/internal/detect"
	"github.com/lspoor/script-center/internal/docs"
	"github.com/lspoor/script-center/internal/params"
	"github.com/lspoor/script-center/internal/scan"
	"github.com/lspoor/script-center/internal/workspace"
)

// Defaults for a read. They are generous because the user is waiting on a
// progress indicator, and a script that has to be given up on is better reported
// than waited for.
const (
	// DefaultTimeout bounds one toolchain invocation. It is long enough for a
	// cold PowerShell start on a slow machine, which is the slowest thing here.
	DefaultTimeout = 20 * time.Second
	// DefaultBatchSize is how many scripts one toolchain invocation reads. A
	// larger batch amortizes the interpreter's startup over more scripts.
	DefaultBatchSize = 40
	// relatedLimit caps the suggestions offered for a script.
	relatedLimit = 6
)

// ErrNoRoot reports that a request named a root the workspace does not have.
var ErrNoRoot = errors.New("app: that root is not in the workspace")

// ErrNoScript reports that a request named a script the root does not have.
var ErrNoScript = errors.New("app: no such script in this root")

// ErrNoDocument reports that a request named a document that discovery did not
// find for the script. It is a rejection of the request rather than a failure,
// so the frontend can leave the reading pane as it was.
var ErrNoDocument = errors.New("app: no such document for this script")

// Service is the application's behavior.
type Service struct {
	// configDir is where the workspace and the parameter cache live.
	configDir string

	// store persists the roots and favorites.
	store *workspace.Store
	// cache persists harvested parameter reports, so that reopening a workspace
	// does not start a toolchain for every script again.
	cache *params.Cache
	// reader reads metadata out of scripts, whatever their language.
	reader *params.Reader

	// mu guards the workspace. A scan can finish after the user has moved on, so
	// the in-memory workspace is not only written by the requests the user is
	// waiting on.
	mu sync.Mutex
	ws *workspace.Workspace

	// roots memoizes the last scan of each root. Reopening the window should not
	// rescan every root, and a scan is the expensive part of opening.
	roots   map[string]*RootView
	rootsMu sync.Mutex
}

// New returns a Service that keeps its state under configDir.
//
// A missing or unreadable state file is not fatal: a first run has none, and a
// corrupt one should cost the user their saved roots rather than the ability to
// start the application at all.
func New(configDir string) *Service {
	reader := params.NewReader(params.Options{
		Timeout:   DefaultTimeout,
		BatchSize: DefaultBatchSize,
	})
	cache := params.NewCache(filepath.Join(configDir, "params-cache.json"))
	reader.Cache = cache
	return &Service{
		configDir: configDir,
		store:     workspace.NewStore(configDir),
		cache:     cache,
		reader:    reader,
		ws:        workspace.New(),
		roots:     map[string]*RootView{},
	}
}

// Start loads the persisted state.
//
// Both loads are best-effort. An absent workspace or cache is the normal first
// run, and a corrupt one is recoverable by reading the scripts again, so neither
// is worth refusing to open the window over.
func (s *Service) Start() error {
	s.mu.Lock()
	loaded, err := s.store.Load()
	if err == nil && loaded != nil {
		s.ws = loaded
	}
	s.mu.Unlock()

	if err := s.cache.Load(); err != nil && !errors.Is(err, params.ErrNoCache) {
		// A cache that cannot be read is a slower open, not a broken one, so it
		// is reported and stepped over.
		s.addWarning(fmt.Sprintf("the parameter cache was not read and will be rebuilt: %v", err))
	}
	return nil
}

// addWarning records a problem that is not worth failing a request over.
func (s *Service) addWarning(message string) {
	s.rootsMu.Lock()
	defer s.rootsMu.Unlock()
	for _, root := range s.roots {
		root.Warnings = append(root.Warnings, message)
	}
}

// Status reports what the app managed to load, so the UI can say so rather than
// leaving the user wondering why their roots are missing.
type Status struct {
	// ConfigDir is where state is kept, shown in Settings.
	ConfigDir string `json:"configDir"`
	// Roots is how many roots the workspace has.
	Roots int `json:"roots"`
	// CachedReports is how many harvested reports were available without
	// re-reading anything.
	CachedReports int `json:"cachedReports"`
	// Warnings holds startup problems that were stepped over.
	Warnings []string `json:"warnings,omitempty"`
}

// Status returns the current state.
func (s *Service) Status() Status {
	s.mu.Lock()
	roots := len(s.ws.Roots)
	s.mu.Unlock()
	return Status{
		ConfigDir:     s.configDir,
		Roots:         roots,
		CachedReports: s.cache.Len(),
		Warnings:      s.warnings(),
	}
}

// warnings collects the warnings held by the memoized roots.
func (s *Service) warnings() []string {
	s.rootsMu.Lock()
	defer s.rootsMu.Unlock()
	var out []string
	seen := map[string]bool{}
	for _, root := range s.roots {
		for _, warning := range root.Warnings {
			if !seen[warning] {
				seen[warning] = true
				out = append(out, warning)
			}
		}
	}
	return out
}

// Workspace returns the roots and favorites.
func (s *Service) Workspace() *workspace.Workspace {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Copied, so a caller holding the result cannot change what is persisted.
	clone := *s.ws
	clone.Roots = append([]workspace.Root(nil), s.ws.Roots...)
	clone.Favorites = append([]workspace.Ref(nil), s.ws.Favorites...)
	return &clone
}

// AddRoot adds a directory to the workspace.
//
// The path is resolved to an absolute one first, because the same directory
// reached by two spellings is one root, and a relative path would stop matching
// once the process's working directory moved.
func (s *Service) AddRoot(path, name string) (*workspace.Workspace, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("app: resolving %q: %w", path, err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return nil, fmt.Errorf("app: opening %q: %w", path, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("app: %q is a file, not a directory", path)
	}

	s.mu.Lock()
	s.ws = s.ws.AddRoot(absolute, name)
	saved := s.ws
	s.mu.Unlock()

	if err := s.store.Save(saved); err != nil {
		return nil, err
	}
	return s.Workspace(), nil
}

// RemoveRoot forgets a directory, its scan, and everything cached from it.
//
// Removing a root says nothing about whether the directory is gone from disk, so
// the favorites pointing into it are dropped, and its cached reports go with it:
// a root the user has deliberately removed from the workspace should stop
// costing anything, and if they add it back the scripts will simply be read
// again.
func (s *Service) RemoveRoot(path string) (*workspace.Workspace, error) {
	// Resolved the same way AddRoot does, so removing by a different spelling of
	// the same directory works.
	absolute := path
	if resolved, err := filepath.Abs(path); err == nil {
		absolute = resolved
	}

	s.mu.Lock()
	s.ws = s.ws.RemoveRoot(absolute)
	saved := s.ws
	s.mu.Unlock()

	if err := s.store.Save(saved); err != nil {
		return nil, err
	}

	s.rootsMu.Lock()
	delete(s.roots, absolute)
	s.rootsMu.Unlock()

	if s.pruneRoot(absolute) > 0 {
		if err := s.cache.Save(); err != nil {
			return nil, err
		}
	}
	return s.Workspace(), nil
}

// pruneRoot drops cached reports for scripts under a root, reporting how many
// went.
//
// The separator is appended to the root so that removing "/a/scripts" does not
// also drop everything cached from a sibling directory called
// "/a/scripts-archive".
func (s *Service) pruneRoot(root string) int {
	if strings.TrimSpace(root) == "" {
		return 0
	}
	prefix := filepath.Clean(root) + string(filepath.Separator)
	removed := 0
	for _, path := range s.cache.Paths() {
		if strings.HasPrefix(filepath.Clean(path), prefix) {
			removed += s.cache.Invalidate(path)
		}
	}
	return removed
}

// ToggleFavorite stars or unstars a script, returning the new workspace.
func (s *Service) ToggleFavorite(root, rel string) (*workspace.Workspace, error) {
	s.mu.Lock()
	s.ws.ToggleFavorite(workspace.Ref{Root: root, Rel: rel})
	saved := s.ws
	s.mu.Unlock()
	if err := s.store.Save(saved); err != nil {
		return nil, err
	}
	return s.Workspace(), nil
}

// IsFavorite reports whether a script is starred.
func (s *Service) IsFavorite(root, rel string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ws.IsFavorite(workspace.Ref{Root: root, Rel: rel})
}

// Toolchain describes whether each language's reader is available, so the UI can
// say "install PowerShell to get parameter forms" instead of showing a script
// with no form and no explanation.
type Toolchain struct {
	// Language is the language name.
	Language string `json:"language"`
	// Available reports whether an extractor exists for it at all.
	Available bool `json:"available"`
	// Reason explains why there is no form, when there is none.
	Reason string `json:"reason,omitempty"`
}

// Toolchains reports the state of every language the scanner can find.
func (s *Service) Toolchains() []Toolchain {
	out := make([]Toolchain, 0, len(knownLanguages))
	for _, language := range knownLanguages {
		// The extractor is asked rather than the toolchain lookup, because a
		// language can have a working reader and still be unreflectable, as Rust
		// and batch are: they have an extractor precisely so it can say so.
		readability := params.ReadabilityOf(language)
		out = append(out, Toolchain{
			Language:  language.DisplayName(),
			Available: readability.Readable,
			Reason:    readability.Reason,
		})
	}
	return out
}

// Meta is what the UI shows about the last read of a root.
type Meta struct {
	// Duration is how long the read took, which the window shows so that a slow
	// toolchain does not look like a hang.
	DurationMS int64 `json:"durationMs"`
	// FromCache is how many scripts were answered without starting a toolchain.
	FromCache int `json:"fromCache"`
	// Read is how many scripts were actually read.
	Read int `json:"read"`
	// Total is how many scripts the root has.
	Total int `json:"total"`
	// CacheHits and CacheMisses are the cumulative counts, so a cache that never
	// hits is visible rather than silently useless.
	CacheHits   int `json:"cacheHits"`
	CacheMisses int `json:"cacheMisses"`
}

// ScriptView is one script with its metadata, ready for the frontend.
type ScriptView struct {
	// Path is absolute; Root and Rel identify the script within the workspace.
	Path string `json:"path"`
	Root string `json:"root"`
	Rel  string `json:"rel"`
	Name string `json:"name"`
	Dir  string `json:"dir"`
	// Kind is "script" or "project". A project has no parameter form of its own.
	Kind string `json:"kind"`
	// Lang is the language name for display.
	Lang string `json:"lang"`
	// Size and ModTime let the UI show a script's age and notice a change made
	// outside the app.
	//
	// ModTime is a formatted string rather than a time.Time because that is what
	// actually crosses to the frontend, where a typed value would arrive as an
	// untyped blob. The format is RFC 3339 in UTC, or empty when the file could
	// not be stat'd.
	Size    int64  `json:"size"`
	ModTime string `json:"modTime"`
	// Metadata is the harvested report, which is present for every script, with
	// Warnings carrying anything that went wrong.
	Metadata params.Report `json:"metadata"`
	// Cached reports that this view was served from the cache rather than read.
	Cached bool `json:"cached"`
	// Inferred reports that no harvester produced a form, whether because the
	// language has no reader or because the file is a project.
	Inferred bool `json:"inferred"`
}

// NeedsForm reports whether this script can have a parameter form, which decides
// whether the UI offers a form at all.
func (v ScriptView) NeedsForm() bool {
	return v.Kind == string(scan.KindScript) && !v.Inferred
}

// RootView is one scanned root with metadata for every script in it.
type RootView struct {
	Root    string       `json:"root"`
	Name    string       `json:"name"`
	Source  string       `json:"source"`
	IsRepo  bool         `json:"isRepo"`
	Scripts []ScriptView `json:"scripts"`
	// Warnings holds recoverable problems, such as a directory that could not
	// be read or a language with no reader installed.
	Warnings []string `json:"warnings,omitempty"`
	// Meta describes the read that produced this view.
	Meta Meta `json:"meta"`
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

// OpenRoot scans a root and reads metadata for every script in it.
//
// The read is the slow part, so an unchanged script is served from the cache and
// only the rest start a toolchain. The result is memoized, because reopening the
// window should not rescan every root the user has.
func (s *Service) OpenRoot(ctx context.Context, root string) (*RootView, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("app: resolving %q: %w", root, err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return nil, fmt.Errorf("app: opening %q: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("app: %q is a file, not a directory", root)
	}

	started := time.Now()
	result, err := scan.Scan(ctx, absolute)
	if err != nil {
		return nil, err
	}

	// Only real scripts are read. A project directory has no parameters of its
	// own, and reading it would produce a confusing report about a directory.
	var readable []scan.Entry
	for _, entry := range result.Entries {
		if entry.Kind != scan.KindScript {
			continue
		}
		readable = append(readable, entry)
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

	view := &RootView{
		Root:     result.Root,
		Name:     s.rootName(absolute),
		Source:   string(result.Source),
		IsRepo:   result.IsRepo,
		Warnings: append([]string(nil), result.Warnings...),
		Scripts:  make([]ScriptView, 0, len(result.Entries)),
	}

	reports, err := s.reader.Read(ctx, scriptsOf(readable))
	if err != nil {
		// A read failure is reported on the root rather than replacing the whole
		// view with nothing, so the user still sees their scripts.
		view.Warnings = append(view.Warnings, fmt.Sprintf("some scripts could not be read: %v", err))
	}
	byPath := make(map[string]params.Report, len(reports))
	for _, report := range reports {
		byPath[report.Path] = report
	}

	for _, entry := range result.Entries {
		view.Scripts = append(view.Scripts, s.viewFor(entry, byPath[entry.Path], cachedBefore[entry.Path]))
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
	for _, warning := range s.warnings() {
		if !contains(view.Warnings, warning) {
			view.Warnings = append(view.Warnings, warning)
		}
	}
	sort.Strings(view.Warnings)

	s.rootsMu.Lock()
	s.roots[absolute] = view
	s.rootsMu.Unlock()
	return view, nil
}

// viewFor builds the view of one entry, filling in a report when the reader had
// nothing to say about it.
func (s *Service) viewFor(entry scan.Entry, report params.Report, cached bool) ScriptView {
	view := ScriptView{
		Path:     entry.Path,
		Root:     entry.Root,
		Rel:      entry.Rel,
		Name:     entry.Name,
		Dir:      entry.Dir,
		Kind:     string(entry.Kind),
		Lang:     entry.Lang.DisplayName(),
		Size:     entry.Size,
		ModTime:  formatTime(entry.ModTime),
		Metadata: report,
		Cached:   cached,
		Inferred: entry.Kind != scan.KindScript,
	}
	if strings.TrimSpace(view.Metadata.Path) == "" {
		view.Metadata = params.Report{Path: entry.Path, Help: params.Help{Source: params.HelpSourceNone}}
		if entry.Kind != scan.KindScript {
			view.Inferred = true
			view.Metadata.AddWarning("this is a project directory, so its tasks are enumerated " +
				"from its manifest rather than run as one script")
		}
	}
	// A report with no parameters and no help came from a language with no
	// reader, or a reader that found nothing. Either way there is no form, and
	// the UI should not offer one.
	view.Inferred = view.Inferred || !hasForm(view.Metadata)
	return view
}

// scriptsOf adapts entries for the reader.
func scriptsOf(entries []scan.Entry) []params.Script {
	out := make([]params.Script, 0, len(entries))
	for _, entry := range entries {
		out = append(out, scriptParams(entry))
	}
	return out
}

// ExplainView is everything known about one script's documentation.
type ExplainView struct {
	Script ScriptView `json:"script"`
	// Docs is the merged documentation: the script's own help plus the nearest
	// README and sidecars.
	Docs docs.Explanation `json:"docs"`
	// Related are the other scripts that mention or resemble this one.
	Related []Suggestion `json:"related,omitempty"`
	// Dependencies are the files this script dot-sources.
	Dependencies []string `json:"dependencies,omitempty"`
}

// Suggestion is a related script, as the frontend sees it.
type Suggestion struct {
	Path string `json:"path"`
	Root string `json:"root"`
	Rel  string `json:"rel"`
	Name string `json:"name"`
	// Reason explains the match, so a suggestion is not a mystery.
	Reason string `json:"reason"`
}

// Explain returns a script's documentation along with the scripts related to it.
func (s *Service) Explain(ctx context.Context, root, rel string) (*ExplainView, error) {
	view, err := s.cachedRoot(ctx, root)
	if err != nil {
		return nil, err
	}
	script, ok := findScript(view, rel)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNoScript, rel)
	}

	explanation, err := docs.Explain(script.Path, view.Root, script.Metadata)
	if err != nil {
		return nil, err
	}

	out := &ExplainView{Script: *script, Docs: explanation}
	for _, suggestion := range s.relatedFor(ctx, view, script) {
		out.Related = append(out.Related, Suggestion{
			Path:   suggestion.Entry.Path,
			Root:   suggestion.Entry.Root,
			Rel:    suggestion.Entry.Rel,
			Name:   suggestion.Entry.Name,
			Reason: suggestion.Reason,
		})
	}
	return out, nil
}

// DocumentView is one documentation file with its content ready to show.
//
// The content is rendered here rather than sent as raw bytes because the
// frontend displays it as HTML. Rendering in the backend is also what keeps the
// sanitizer in one place: a readme is untrusted input, and only the backend
// decides what markup survives.
type DocumentView struct {
	Path  string `json:"path"`
	Kind  string `json:"kind"`
	Title string `json:"title"`
	// HTML is the sanitized, rendered document. It is empty when Error is set.
	HTML string `json:"html"`
	// Error says why a document could not be shown, so the sidebar can list it
	// and the reading pane can say why it is empty instead of rendering nothing.
	Error string `json:"error,omitempty"`
}

// RenderDocument returns one of a script's discovered documents, rendered to
// sanitized HTML.
//
// The path is checked against what discovery actually found for this script.
// Without that check the binding would read any file the user account can read,
// which is a much larger capability than "show me the readme next to this
// script" and is not one a script browser should hand to a web view.
func (s *Service) RenderDocument(ctx context.Context, root, rel, path string) (*DocumentView, error) {
	view, err := s.cachedRoot(ctx, root)
	if err != nil {
		return nil, err
	}
	script, ok := findScript(view, rel)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNoScript, rel)
	}

	documents, err := docs.Discover(script.Path, view.Root)
	if err != nil {
		return nil, err
	}
	document, ok := findDocument(documents, path)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNoDocument, path)
	}

	out := &DocumentView{
		Path:  document.Path,
		Kind:  string(document.Kind),
		Title: document.Title,
	}
	// A readme that has a section about this script is rendered as that section
	// alone. A project readme that happens to be an ancestor is often about
	// something else, and rendering all of it would bury the few lines that
	// answer the question.
	html, err := renderDocument(document)
	out.HTML = html
	if err != nil {
		// A document that cannot be rendered is still worth listing, so the
		// failure is carried in the view instead of returned as an error that
		// would hide the other documents that did work.
		out.Error = err.Error()
	}
	return out, nil
}

// renderDocument renders a discovered document, honoring its section excerpt.
func renderDocument(document docs.Document) (string, error) {
	if document.Excerpted {
		return docs.Render(filepath.Ext(document.Path), []byte(document.Section))
	}
	return docs.RenderFile(document.Path)
}

// findDocument locates a discovered document by path. The comparison is on
// cleaned paths so that a frontend passing an equivalent form still matches.
func findDocument(documents []docs.Document, path string) (docs.Document, bool) {
	want := filepath.Clean(path)
	for _, document := range documents {
		if filepath.Clean(document.Path) == want {
			return document, true
		}
	}
	return docs.Document{}, false
}

// relatedFor finds the scripts related to one, using the memoized scan so that
// opening a script's documentation does not rescan the root.
func (s *Service) relatedFor(ctx context.Context, view *RootView, script *ScriptView) []workspace.Suggestion {
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

// cachedRoot returns a memoized root view, scanning it if it is not there.
func (s *Service) cachedRoot(ctx context.Context, root string) (*RootView, error) {
	absolute, err := filepath.Abs(root)
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

// findScript looks up a script in a root view by its relative path.
func findScript(view *RootView, rel string) (*ScriptView, bool) {
	want := filepath.ToSlash(filepath.Clean(rel))
	for i := range view.Scripts {
		if view.Scripts[i].Rel == want {
			return &view.Scripts[i], true
		}
	}
	return nil, false
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
	script, ok := findScript(view, rel)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNoScript, rel)
	}
	s.cache.Invalidate(script.Path)

	// Re-read just this one script rather than the whole root, which is what the
	// user is waiting on.
	entry := scan.Entry{
		Root: script.Root, Path: script.Path, Rel: script.Rel, Name: script.Name,
		Dir: script.Dir, Kind: scan.Kind(script.Kind), Lang: s.languageOf(script),
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
	if len(reports) == 1 {
		*script = s.viewFor(entry, reports[0], false)
	}
	if err := s.cache.Save(); err != nil {
		view.Warnings = append(view.Warnings, fmt.Sprintf("the parameter cache was not saved: %v", err))
	}
	return script, nil
}

// knownLanguages are the languages the app can read, in the order the window
// lists them.
var knownLanguages = []detect.Language{
	detect.PowerShell,
	detect.Python,
	detect.Bash,
	detect.Rust,
	detect.Batch,
}

// languageOf recovers a language from a view, whose Lang is a display name.
//
// The view carries the display name because that is what the UI shows, so the
// original has to be recovered for the reader. Matching on the display name is
// sufficient because it is a function of the language, not a lossy label.
func (s *Service) languageOf(view *ScriptView) detect.Language {
	for _, language := range knownLanguages {
		if language.DisplayName() == view.Lang {
			return language
		}
	}
	return detect.Unknown
}

// rootName returns the label a root should carry, preferring the name the user
// gave it and falling back to the directory's own name.
func (s *Service) rootName(path string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, root := range s.ws.Roots {
		if root.Path == path {
			return rootName(path, root.Name)
		}
	}
	return rootName(path, "")
}

// hasForm reports whether a report carries anything worth showing as a form.
//
// A report with no parameters and no help describes a script whose inputs could
// not be read, or a script that takes none. Either way there is no form to offer,
// and offering an empty one would be worse than saying so.
func hasForm(report params.Report) bool {
	if len(report.Params) > 0 {
		return true
	}
	help := report.Help
	return strings.TrimSpace(help.Synopsis) != "" ||
		strings.TrimSpace(help.Description) != "" ||
		len(help.Examples) > 0 ||
		len(help.Notes) > 0 ||
		len(help.Links) > 0
}

// rootName returns the label a root should carry.
func rootName(path, preferred string) string {
	if strings.TrimSpace(preferred) != "" {
		return preferred
	}
	return filepath.Base(path)
}

// formatTime renders a timestamp for the frontend.
//
// UTC with a numeric zone, so a timestamp shown next to one from another tool is
// comparable without the reader having to know the local zone.
func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

// contains reports whether a list holds a value.
func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

// Flush writes the workspace and the cache to disk.
//
// The cache is written after every read already, so this is about the workspace
// and about anything a read produced after the last save. It is called when the
// window closes, where there is no later opportunity to write.
func (s *Service) Flush() error {
	s.mu.Lock()
	saved := s.ws
	s.mu.Unlock()

	if err := s.store.Save(saved); err != nil {
		return err
	}
	return s.cache.Save()
}
