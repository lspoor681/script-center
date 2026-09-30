// Package app is the application's behavior, kept out of package main so that it
// can be tested without a window.
//
// Everything here returns plain structs with JSON tags. Wails binds those
// directly, and a test reads them without a webview, so the boundary between the
// application's logic and its presentation is one struct field at a time rather
// than a layer of translation that can drift.
//
// The files divide by what is being worked on rather than by layer. service.go
// holds the state and the workspace; roots.go holds scanning and caching; explain
// holds documentation; views.go holds the types that cross to the frontend.
package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/lspoor/script-center/internal/detect"
	"github.com/lspoor/script-center/internal/editor"
	"github.com/lspoor/script-center/internal/params"
	"github.com/lspoor/script-center/internal/proc"
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

// The errors a request can fail with that are not a malfunction. They are
// sentinel values so the frontend can tell "you asked for something that is not
// there" from "something broke", which it cannot do from a message.
var (
	// ErrNoRoot reports that a request named a root the workspace does not have.
	ErrNoRoot = errors.New("app: that root is not in the workspace")

	// ErrNoScript reports that a request named a script the root does not have.
	ErrNoScript = errors.New("app: no such script in this root")

	// ErrNoDocument reports that a request named a document that discovery did not
	// find for the script. It is a rejection of the request rather than a failure,
	// so the frontend can leave the reading pane as it was.
	ErrNoDocument = errors.New("app: no such document for this script")
)

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
	//
	// A view in here is never modified after it is published. A request that
	// needs to change one builds a replacement and swaps it in, so a reader that
	// is part way through using a view keeps seeing a consistent one.
	roots   map[string]*RootView
	rootsMu sync.Mutex

	// runner starts scripts and streams their output to the window as events.
	// Zero runners are possible in tests that build a Service by hand, so the
	// run methods cope with a nil one.
	runner *Runner

	// startup holds problems found while loading, which every root view repeats.
	// They are kept here rather than stamped into each view, because a view is
	// published and a published view is not written to.
	startup   []string
	startupMu sync.Mutex

	// stageEmit sends read-progress events to the window. It is installed once at
	// startup, after the Service is built, so it is read under stageMu at emit
	// time rather than captured by whatever wires it up.
	stageEmit func(name string, data any)
	stageMu   sync.Mutex
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
	service := &Service{
		configDir: configDir,
		store:     workspace.NewStore(configDir),
		cache:     cache,
		reader:    reader,
		ws:        workspace.New(),
		roots:     map[string]*RootView{},
		runner:    NewRunner(),
	}
	// The reader's progress hook is bound to the Service rather than to an
	// emitter, because the emitter is installed later and a hook that captured
	// it at construction time would always see the nil one. emitStage reads the
	// current emitter when it is called.
	reader.OnGroup = func(ctx context.Context, progress params.GroupProgress) {
		service.emitStage(ReadStage{
			Root:     readRoot(ctx),
			Stage:    StageHarvest,
			Language: string(progress.Language),
			Done:     progress.Done,
			Total:    progress.Total,
			Scripts:  progress.Scripts,
		})
	}
	return service
}

// readRootKey carries the root a read is for through the read itself, so that a
// progress event raised deep in the parameter reader can name the root without
// the reader knowing anything about roots. It is a context value rather than a
// field on the Service because two reads of different roots can overlap, and a
// shared field would let one read's progress be labeled with the other's root.
type readRootKey struct{}

// withReadRoot returns a context that names the root being read.
func withReadRoot(ctx context.Context, root string) context.Context {
	return context.WithValue(ctx, readRootKey{}, root)
}

// readRoot returns the root a read is for, or "" outside a read.
func readRoot(ctx context.Context) string {
	root, _ := ctx.Value(readRootKey{}).(string)
	return root
}

// Start loads the persisted state.
//
// Both loads are best-effort. An absent workspace or cache is the normal first
// run, and a corrupt one is recoverable by reading the scripts again, so neither
// is worth refusing to open the window over. Recoverable is not the same as
// silent, though: a saved state file that cannot be read means the user's roots
// and stars are gone until they add them again, so that is reported the same way
// a cache that could not be read is. Without it a corrupt file is
// indistinguishable from a first run.
func (s *Service) Start() error {
	s.mu.Lock()
	loaded, err := s.store.Load()
	if err == nil && loaded != nil {
		s.ws = loaded
	}
	s.mu.Unlock()
	if err != nil {
		s.addStartupWarning(fmt.Sprintf("the saved workspace was not read and its roots will need to be added again: %v", err))
	}

	if err := s.cache.Load(); err != nil && !errors.Is(err, params.ErrNoCache) {
		// A cache that cannot be read is a slower open, not a broken one, so it
		// is reported and stepped over.
		s.addStartupWarning(fmt.Sprintf("the parameter cache was not read and will be rebuilt: %v", err))
	}
	return nil
}

// addStartupWarning records a problem found while loading that every root view
// should repeat.
func (s *Service) addStartupWarning(message string) {
	s.startupMu.Lock()
	defer s.startupMu.Unlock()
	s.startup = append(s.startup, message)
}

// startupWarnings returns the load-time problems recorded so far.
func (s *Service) startupWarnings() []string {
	s.startupMu.Lock()
	defer s.startupMu.Unlock()
	return slices.Clone(s.startup)
}

// Status returns the current state.
func (s *Service) Status() Status {
	return Status{
		ConfigDir:     s.configDir,
		Roots:         len(s.Workspace().Roots),
		CachedReports: s.cache.Len(),
		Warnings:      s.warnings(),
	}
}

// Workspace returns the roots and favorites.
func (s *Service) Workspace() *workspace.Workspace {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ws.Snapshot()
}

// saveWorkspace writes the workspace to disk from a snapshot.
//
// The snapshot matters. The workspace's own mutators edit their slices in place,
// so handing the store the live value would let it read a slice that another
// request is rewriting, which is a race rather than merely untidy.
func (s *Service) saveWorkspace() error {
	return s.store.Save(s.Workspace())
}

// AddRoot adds a directory to the workspace.
//
// The path is resolved to an absolute one first, because the same directory
// reached by two spellings is one root, and a relative path would stop matching
// once the process's working directory moved.
func (s *Service) AddRoot(path, name string) (*workspace.Workspace, error) {
	absolute, err := existingDir(path)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.ws = s.ws.AddRoot(absolute, name)
	s.mu.Unlock()

	if err := s.saveWorkspace(); err != nil {
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
	s.mu.Unlock()

	if err := s.saveWorkspace(); err != nil {
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
	s.mu.Unlock()

	if err := s.saveWorkspace(); err != nil {
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

// globalEditorConfigPath returns the path to the global editor config file.
func (s *Service) globalEditorConfigPath() string {
	return filepath.Join(s.configDir, "editors.toml")
}

// loadGlobalEditorConfig loads the global editor configuration from disk.
func (s *Service) loadGlobalEditorConfig() (EditorConfig, error) {
	var cfg EditorConfig
	path := s.globalEditorConfigPath()
	if _, err := os.Stat(path); err != nil {
		return EditorConfig{
			Preferences:  make(map[string]string),
			KnownEditors: make(map[string]Editor),
		}, nil
	}
	// Decode into a temporary struct with internal editor types
	type internalConfig struct {
		Preferences  map[string]string        `toml:"preferences"`
		KnownEditors map[string]editor.Editor `toml:"known_editors"`
	}
	var internal internalConfig
	_, err := toml.DecodeFile(path, &internal)
	if internal.Preferences == nil {
		internal.Preferences = make(map[string]string)
	}
	if internal.KnownEditors == nil {
		internal.KnownEditors = make(map[string]editor.Editor)
	}
	cfg.Preferences = internal.Preferences
	cfg.KnownEditors = make(map[string]Editor, len(internal.KnownEditors))
	for k, v := range internal.KnownEditors {
		cfg.KnownEditors[k] = EditorFromInternal(v)
	}
	return cfg, err
}

// saveGlobalEditorConfig saves the global editor configuration to disk.
func (s *Service) saveGlobalEditorConfig(cfg EditorConfig) error {
	f, err := os.Create(s.globalEditorConfigPath())
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	// Encode with internal editor types for TOML
	type internalConfig struct {
		Preferences  map[string]string        `toml:"preferences"`
		KnownEditors map[string]editor.Editor `toml:"known_editors"`
	}
	internal := internalConfig{
		Preferences:  cfg.Preferences,
		KnownEditors: make(map[string]editor.Editor, len(cfg.KnownEditors)),
	}
	for k, v := range cfg.KnownEditors {
		internal.KnownEditors[k] = editor.Editor{
			ID:          v.ID,
			Name:        v.Name,
			Executable:  v.Executable,
			Args:        v.Args,
			Terminal:    v.Terminal,
			TerminalCmd: v.TerminalCmd,
			Extensions:  v.Extensions,
			Source:      v.Source,
		}
	}
	if err := toml.NewEncoder(f).Encode(internal); err != nil {
		return err
	}
	return f.Close()
}

// DetectEditors returns the list of available editors on the system.
func (s *Service) DetectEditors() []Editor {
	internal := editor.DetectPlatformEditors()
	out := make([]Editor, len(internal))
	for i, e := range internal {
		out[i] = EditorFromInternal(e)
	}
	return out
}

// OpenFile opens a file with the specified editor.
// editorID "system" means use OS default.
func (s *Service) OpenFile(path, editorID string) error {
	return proc.OpenFile(path, editorID)
}

// GetPreferredEditor returns the preferred editor for a file extension.
// It checks workspace override first, then global config, then defaults.
func (s *Service) GetPreferredEditor(ext string) PreferredEditorResult {
	// 1. Workspace override
	s.mu.Lock()
	ws := s.ws.Snapshot()
	s.mu.Unlock()
	if ws.EditorPrefs != nil {
		if id, ok := ws.EditorPrefs[ext]; ok {
			return PreferredEditorResult{EditorID: id, Source: "workspace", Found: true}
		}
	}

	// 2. Global config
	cfg, err := s.loadGlobalEditorConfig()
	if err == nil && cfg.Preferences != nil {
		if id, ok := cfg.Preferences[ext]; ok {
			return PreferredEditorResult{EditorID: id, Source: "global", Found: true}
		}
	}

	// 3. Default: Neovim if available
	editors := s.DetectEditors()
	for _, ed := range editors {
		if ed.ID == "neovim" {
			return PreferredEditorResult{EditorID: "neovim", Source: "default", Found: true}
		}
	}

	// 4. System default
	return PreferredEditorResult{EditorID: "system", Source: "default", Found: true}
}

// SetWorkspacePreferredEditor sets the workspace-specific preferred editor for an extension.
func (s *Service) SetWorkspacePreferredEditor(ext, editorID string) error {
	s.mu.Lock()
	if s.ws.EditorPrefs == nil {
		s.ws.EditorPrefs = make(map[string]string)
	}
	s.ws.EditorPrefs[ext] = editorID
	s.mu.Unlock()
	return s.saveWorkspace()
}

// SetGlobalPreferredEditor sets the global preferred editor for an extension.
func (s *Service) SetGlobalPreferredEditor(ext, editorID string) error {
	cfg, err := s.loadGlobalEditorConfig()
	if err != nil {
		return err
	}
	if cfg.Preferences == nil {
		cfg.Preferences = make(map[string]string)
	}
	cfg.Preferences[ext] = editorID
	return s.saveGlobalEditorConfig(cfg)
}

// GetGlobalEditorConfig returns the full global editor configuration.
func (s *Service) GetGlobalEditorConfig() (EditorConfig, error) {
	return s.loadGlobalEditorConfig()
}

// SaveGlobalEditorConfig saves the full global editor configuration.
func (s *Service) SaveGlobalEditorConfig(cfg EditorConfig) error {
	return s.saveGlobalEditorConfig(cfg)
}

// knownLanguages are the languages the app can read, in the order the window
// lists them. The order is a presentation choice; the set of languages the
// scanner can find is detect's business.
var knownLanguages = []detect.Language{
	detect.PowerShell,
	detect.Python,
	detect.Bash,
	detect.Rust,
	detect.Batch,
}

// Toolchains reports the state of every language the app can read, so the UI can
// say "install PowerShell to get parameter forms" instead of showing a script
// with no form and no explanation.
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

// Flush writes the workspace and the cache to disk.
//
// The cache is written after every read already, so this is about the workspace
// and about anything a read produced after the last save. It is called when the
// window closes, where there is no later opportunity to write.
func (s *Service) Flush() error {
	if err := s.saveWorkspace(); err != nil {
		return err
	}
	return s.cache.Save()
}

// existingDir resolves a path and confirms it is a directory, which is what both
// adding and opening a root require.
func existingDir(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("app: resolving %q: %w", path, err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", fmt.Errorf("app: opening %q: %w", path, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("app: %q is a file, not a directory", path)
	}
	return absolute, nil
}
