package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// FileName is the name of the workspace file inside the configuration
// directory.
const FileName = "workspaces.json"

// ErrVersionTooNew is returned when the file was written by a newer build whose
// layout this one cannot interpret. Refusing is the safe choice: guessing at an
// unknown layout could drop roots the user still depends on.
var ErrVersionTooNew = errors.New("workspace: file written by a newer version")

// Store reads and writes the workspace file.
type Store struct {
	// path is the full path of the workspace file.
	path string
}

// NewStore returns a Store writing to the given configuration directory. The
// directory is created on first write rather than here, so constructing a Store
// never has a side effect.
func NewStore(configDir string) *Store {
	return &Store{path: filepath.Join(configDir, FileName)}
}

// Path returns the file the Store reads and writes.
func (s *Store) Path() string { return s.path }

// Load reads the workspace file.
//
// A missing file is not an error: it means the application has not been set up
// yet, and the caller should start from an empty workspace.
func (s *Store) Load() (*Workspace, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return New(), nil
		}
		return nil, fmt.Errorf("workspace: read %s: %w", s.path, err)
	}

	// A file written by hand may carry a zero version, so treat that as
	// version 1 rather than rejecting it.
	var w Workspace
	if err := json.Unmarshal(data, &w); err != nil {
		return nil, fmt.Errorf("workspace: parse %s: %w", s.path, err)
	}
	if w.Version == 0 {
		w.Version = currentVersion
	}
	if w.Version > currentVersion {
		return nil, fmt.Errorf("%w: version %d, this build understands %d",
			ErrVersionTooNew, w.Version, currentVersion)
	}

	w.normalize()
	return &w, nil
}

// normalize re-canonicalises paths loaded from disk, so a file edited by hand
// or written by another platform still compares equal to one this build wrote.
func (w *Workspace) normalize() {
	roots := make([]Root, 0, len(w.Roots))
	for _, r := range w.Roots {
		roots = append(roots, normalizeRoot(r.Path, r.Name))
	}
	w.Roots = roots

	// Favorites whose root is gone are dropped, and duplicates collapsed, so
	// toggling a star cannot produce a duplicate entry.
	seen := make(map[Ref]bool, len(w.Favorites))
	kept := make([]Ref, 0, len(w.Favorites))
	live := make(map[string]bool, len(w.Roots))
	for _, r := range w.Roots {
		live[r.Path] = true
	}
	for _, f := range w.Favorites {
		if live[f.Root] && !seen[f] {
			seen[f] = true
			kept = append(kept, f)
		}
	}
	w.Favorites = kept
}

// Save writes the workspace file atomically.
//
// The file is written to a temporary name in the same directory and then
// renamed, so an interrupted write or a full disk leaves the previous contents
// intact instead of a truncated file that would lose every root the user has.
func (s *Store) Save(w *Workspace) error {
	if w.Version == 0 {
		w.Version = currentVersion
	}

	data, err := json.MarshalIndent(w, "", "  ")
	if err != nil {
		return fmt.Errorf("workspace: encode: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("workspace: create %s: %w", dir, err)
	}

	// CreateTemp in the target directory guarantees the rename stays on the
	// same filesystem, which a rename across devices would not.
	tmp, err := os.CreateTemp(dir, ".workspaces-*.json")
	if err != nil {
		return fmt.Errorf("workspace: temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once the rename succeeds

	// The file holds directory paths and star selections, nothing secret, but
	// it is still user state that should not be world readable.
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("workspace: chmod temp: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("workspace: write temp: %w", err)
	}
	// Sync before the rename, otherwise a crash can leave the renamed file
	// present but empty.
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("workspace: sync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("workspace: close temp: %w", err)
	}
	if err := renameFile(tmpName, s.path); err != nil {
		return fmt.Errorf("workspace: rename into place: %w", err)
	}
	return nil
}
