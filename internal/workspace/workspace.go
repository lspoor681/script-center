// Package workspace holds the set of directories the user has chosen to work
// in, the scripts they have starred, and the relationships between scripts.
//
// The model here is deliberately plain data with no behavior that depends on
// the filesystem or on git, so it can be serialized, compared and tested
// without touching a disk.
package workspace

import (
	"path/filepath"
	"strings"
)

// currentVersion is the schema version written to disk. A file with a higher
// version is rejected rather than guessed at, so a newer build's layout is
// never silently reinterpreted by an older one.
const currentVersion = 1

// Root is a directory the user has added to the workspace.
type Root struct {
	// Path is absolute and cleaned, and is the identity of the root: two roots
	// with the same path are the same root however they were added.
	Path string `json:"path"`
	// Name is the label shown in the tree. It defaults to the final path
	// element but is stored so the user can rename a root without the label
	// jumping when a directory happens to be renamed on disk.
	Name string `json:"name"`
}

// Ref identifies a script within the workspace. Root is included because the
// same relative path can exist under more than one root, and starring it in one
// place says nothing about the other.
type Ref struct {
	Root string `json:"root"`
	Rel  string `json:"rel"`
}

// Workspace is the complete persisted state.
type Workspace struct {
	Version int    `json:"version"`
	Roots   []Root `json:"roots"`
	// Favorites is a slice rather than a set so the file stays legible and
	// editable by hand. It is small by nature, so linear lookups are fine.
	Favorites []Ref `json:"favorites,omitempty"`
	// EditorPrefs maps file extensions to editor IDs for workspace-specific
	// editor preferences. These override global settings.
	EditorPrefs map[string]string `json:"editor_prefs,omitempty"`
}

// New returns an empty workspace.
func New() *Workspace {
	return &Workspace{Version: currentVersion}
}

// normalizeRoot fills in a default name and canonicalises the path, so that a
// root added as "./scripts/" and the same root added as an absolute path
// collapse to one entry.
func normalizeRoot(path, name string) Root {
	abs, err := filepath.Abs(path)
	if err != nil {
		// A path that cannot be resolved is kept as given rather than dropped;
		// the user can fix it in the UI and losing the entry silently is worse.
		abs = filepath.Clean(path)
	}
	abs = filepath.Clean(abs)

	if name == "" {
		name = defaultRootName(abs)
	}
	return Root{Path: abs, Name: name}
}

// defaultRootName labels a root by its final path element, falling back to the
// full path for a filesystem root, which has no meaningful final element.
func defaultRootName(abs string) string {
	base := filepath.Base(abs)
	if base == string(filepath.Separator) || base == "." || base == "" {
		return abs
	}
	return base
}

// AddRoot appends a root and returns the updated workspace. Adding a root that
// is already present is a no-op rather than a duplicate, and neither a parent
// nor a child of an existing root is added, because scanning both would list
// the same scripts twice.
func (w *Workspace) AddRoot(path, name string) *Workspace {
	added := normalizeRoot(path, name)

	for _, existing := range w.Roots {
		if existing.Path == added.Path {
			return w
		}
	}

	// A nested root is redundant. If the new root sits inside an existing one,
	// it is dropped; if it contains an existing one, that existing root is
	// replaced so the wider root wins.
	kept := w.Roots[:0]
	for _, existing := range w.Roots {
		switch {
		case isInside(added.Path, existing.Path):
			return w
		case isInside(existing.Path, added.Path):
			// Superseded by the new, wider root.
		default:
			kept = append(kept, existing)
		}
	}
	w.Roots = append(kept, added)
	return w
}

// RemoveRoot drops a root and any favorites that belonged to it, so a stale
// favorite cannot reappear if the directory is added again later.
func (w *Workspace) RemoveRoot(path string) *Workspace {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = filepath.Clean(path)
	}
	abs = filepath.Clean(abs)

	keptRoots := w.Roots[:0]
	for _, r := range w.Roots {
		if r.Path != abs {
			keptRoots = append(keptRoots, r)
		}
	}
	w.Roots = keptRoots

	keptFavs := w.Favorites[:0]
	for _, f := range w.Favorites {
		if f.Root != abs {
			keptFavs = append(keptFavs, f)
		}
	}
	w.Favorites = keptFavs
	return w
}

// isInside reports whether child is a strict descendant of parent.
func isInside(child, parent string) bool {
	if child == parent {
		return false
	}
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	// A relative path that escapes upward with ".." is not inside.
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// HasRoot reports whether a directory is part of the workspace.
func (w *Workspace) HasRoot(path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = filepath.Clean(path)
	}
	for _, r := range w.Roots {
		if r.Path == filepath.Clean(abs) {
			return true
		}
	}
	return false
}

// IsFavorite reports whether a script is starred.
func (w *Workspace) IsFavorite(ref Ref) bool {
	for _, f := range w.Favorites {
		if f == ref {
			return true
		}
	}
	return false
}

// ToggleFavorite stars or unstars a script and reports the new state.
func (w *Workspace) ToggleFavorite(ref Ref) bool {
	if w.IsFavorite(ref) {
		kept := w.Favorites[:0]
		for _, f := range w.Favorites {
			if f != ref {
				kept = append(kept, f)
			}
		}
		w.Favorites = kept
		return false
	}
	w.Favorites = append(w.Favorites, ref)
	return true
}

// Snapshot returns a copy of the workspace that shares no slice with the
// original.
//
// The mutators on Workspace edit their slices in place, which is the cheapest way
// to keep them readable and is fine while one goroutine owns the value. It stops
// being fine the moment somebody holds the workspace across a lock boundary, for
// instance to write it to disk, so a caller that needs to keep a copy past an
// unlock takes one of these.
func (w *Workspace) Snapshot() *Workspace {
	clone := *w
	clone.Roots = append([]Root(nil), w.Roots...)
	clone.Favorites = append([]Ref(nil), w.Favorites...)
	if w.EditorPrefs != nil {
		clone.EditorPrefs = make(map[string]string, len(w.EditorPrefs))
		for k, v := range w.EditorPrefs {
			clone.EditorPrefs[k] = v
		}
	}
	return &clone
}

// FavoritesUnder returns the starred scripts belonging to one root, preserving
// the order they were starred in.
func (w *Workspace) FavoritesUnder(root string) []Ref {
	var out []Ref
	for _, f := range w.Favorites {
		if f.Root == root {
			out = append(out, f)
		}
	}
	return out
}

// RootsExist returns the roots whose directories are no longer present, so the
// UI can offer to drop them. A root that has been deleted is common enough
// after unmounting a share, and silently rescanning it on every refresh would
// be a persistent source of errors in the tree.
func (w *Workspace) RootsExist(lookup func(string) bool) []Root {
	var missing []Root
	for _, r := range w.Roots {
		if !lookup(r.Path) {
			missing = append(missing, r)
		}
	}
	return missing
}

// PruneMissing removes roots that no longer exist, returning the removed paths.
func (w *Workspace) PruneMissing(lookup func(string) bool) []string {
	var removed []string
	for _, r := range w.Roots {
		if !lookup(r.Path) {
			removed = append(removed, r.Path)
			w.RemoveRoot(r.Path)
		}
	}
	return removed
}
