package params

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// CacheVersion identifies the shape of the cache and the behavior of the
// harvesters that filled it.
//
// It is bumped whenever a harvester starts reporting something different, so
// that a cache written by an older build is discarded rather than shown to the
// user as though it were current. A stale parameter form is a silent wrongness,
// so it is worth one wasted harvest after an upgrade to avoid.
const CacheVersion = 1

// ErrNoCache reports that no cache file was found, which is a normal state on a
// first run rather than a failure.
var ErrNoCache = errors.New("params: no parameter cache")

// Cache stores harvested reports so that reopening a workspace does not pay for
// every toolchain invocation again.
//
// The key is the path together with the size and modification time. That is the
// usual pairing for a derived-artifact cache: changing either means the file is
// not the file that was read, and a file that was merely touched is cheap to
// re-read rather than being worth the risk of trusting. The one case this
// cannot see is an edit that preserves both the length and the timestamp, which
// a deliberate overwrite can do; Invalidate exists for when a user knows better.
type Cache struct {
	// path is where the cache is persisted. An empty path keeps the cache in
	// memory only, which is what a caller that does not want to write files uses.
	path string

	mu      sync.Mutex
	version int
	entries map[string]cacheEntry
	// hits and misses are exposed for the window that shows the user why a
	// reopen was fast, and so that a cache which never hits is noticeable.
	hits   int
	misses int
}

// cacheEntry is one stored report with the file state it was read from.
type cacheEntry struct {
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
	Lang    string    `json:"lang"`
	Report  Report    `json:"report"`
}

// cacheFile is the on-disk shape.
type cacheFile struct {
	Version int                   `json:"version"`
	Entries map[string]cacheEntry `json:"entries"`
}

// NewCache returns a cache persisted at path. An empty path gives an in-memory
// cache, which is useful for tests and for a caller that has nowhere to write.
func NewCache(path string) *Cache {
	return &Cache{path: path, version: CacheVersion, entries: map[string]cacheEntry{}}
}

// Load reads the cache from disk.
//
// A missing file, an unreadable file, and a file written by a different version
// are all the same thing as far as the caller is concerned: there is nothing
// cached yet. They return ErrNoCache and leave an empty cache in place, because
// refusing to show parameters because a cache file is corrupt would be a worse
// outcome than reading them again.
func (c *Cache) Load() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.path == "" {
		return ErrNoCache
	}
	data, err := os.ReadFile(c.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNoCache
		}
		return fmt.Errorf("params: reading the parameter cache: %w", err)
	}
	var file cacheFile
	if err := json.Unmarshal(data, &file); err != nil {
		return ErrNoCache
	}
	if file.Version != CacheVersion {
		return ErrNoCache
	}
	if file.Entries != nil {
		c.entries = file.Entries
	}
	c.version = file.Version
	return nil
}

// Save writes the cache to disk.
//
// The write goes to a temporary file in the same directory and is then renamed,
// because a cache that is half written is worse than no cache: it would be read
// back as corrupt on the next run, and a crash during a plain write could leave
// it truncated.
func (c *Cache) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return fmt.Errorf("params: creating the cache directory: %w", err)
	}

	data, err := json.MarshalIndent(cacheFile{Version: c.version, Entries: c.entries}, "", "  ")
	if err != nil {
		return fmt.Errorf("params: encoding the parameter cache: %w", err)
	}

	temp, err := os.CreateTemp(filepath.Dir(c.path), ".params-cache-*")
	if err != nil {
		return fmt.Errorf("params: creating the cache file: %w", err)
	}
	tempName := temp.Name()
	// The temporary file is removed on every path out. A failure to remove it
	// leaves a stray file next to the cache, which is not worth failing a
	// successful save over, so the error is deliberately dropped.
	defer func() { _ = os.Remove(tempName) }()

	if _, err := temp.Write(data); err != nil {
		// The close error is not reported because the write already failed, and
		// the deferred remove cleans up either way.
		_ = temp.Close()
		return fmt.Errorf("params: writing the cache file: %w", err)
	}
	// A close error on a write means the data may not have reached the disk, so it
	// is worth reporting rather than renaming a file that is short.
	if err := temp.Close(); err != nil {
		return fmt.Errorf("params: closing the cache file: %w", err)
	}
	// A rename within a directory is atomic, so a reader either sees the old
	// file or the new one.
	if err := os.Rename(tempName, c.path); err != nil {
		return fmt.Errorf("params: replacing the cache file: %w", err)
	}
	return nil
}

// fingerprint is the file state a report was read from.
type fingerprint struct {
	size    int64
	modTime time.Time
	lang    string
}

// newFingerprint builds the key for a script as the caller last stat'd it. The
// size and time come from scan.Entry so that no second stat is needed.
func newFingerprint(size int64, modTime time.Time, language string) fingerprint {
	return fingerprint{size: size, modTime: modTime.UTC().Truncate(time.Second), lang: language}
}

// key identifies an entry in the map. The language is part of it because a file
// whose extension was changed is a different script even at the same path.
func (f fingerprint) key(path string) string {
	return strings.ToLower(filepath.Clean(path)) + "\x00" +
		fmt.Sprintf("%d\x00%d\x00%s", f.size, f.modTime.UnixNano(), f.lang)
}

// Lookup returns the cached report for a script, if the file is unchanged.
//
// A hit means the toolchain does not have to be started at all, which is the
// whole reason the cache exists: reopening a workspace of a hundred PowerShell
// scripts is otherwise a hundred interpreter startups.
func (c *Cache) Lookup(path string, size int64, modTime time.Time, language string) (Report, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[newFingerprint(size, modTime, language).key(path)]
	if !ok {
		c.misses++
		return Report{}, false
	}
	c.hits++
	return entry.Report, true
}

// Store records a report for a script.
func (c *Cache) Store(path string, size int64, modTime time.Time, language string, report Report) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := newFingerprint(size, modTime, language).key(path)
	// The language is recorded on the entry as well as in the key, so that a
	// cache file is legible to a person trying to work out why an entry is stale.
	report.Path = path
	c.entries[key] = cacheEntry{
		Path:    path,
		Size:    size,
		ModTime: modTime,
		Lang:    language,
		Report:  report,
	}
}

// Invalidate drops whatever is cached for a path, whatever the file state. It is
// for the case a size and timestamp cannot catch, such as a user who edited a
// script and wants to be certain.
func (c *Cache) Invalidate(path string) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	cleaned := strings.ToLower(filepath.Clean(path))
	removed := 0
	for key, entry := range c.entries {
		if strings.EqualFold(filepath.Clean(entry.Path), cleaned) {
			delete(c.entries, key)
			removed++
		}
	}
	return removed
}

// PruneStale drops entries whose file is no longer on disk, reporting how many
// went. A report for a deleted or renamed script can never be hit again, so
// keeping it only makes the cache file grow.
//
// Entries for files that still exist are kept even if they are not in the root
// currently open. A cache holds every root the workspace has, and a caller that
// is looking at one root does not know what the others hold; taking its word for
// it would throw away every other root's reports. A file that has merely changed
// is left too, because its key no longer matches and the next lookup will miss
// and overwrite it anyway.
func (c *Cache) PruneStale() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	removed := 0
	for key, entry := range c.entries {
		_, err := os.Stat(entry.Path)
		if err == nil {
			continue
		}
		// A stat that fails for a reason other than absence, such as a permission
		// error, is not proof the file is gone, so the entry is kept. Losing a
		// cache entry costs a re-read; keeping a dead one costs disk.
		if !errors.Is(err, fs.ErrNotExist) {
			continue
		}
		delete(c.entries, key)
		removed++
	}
	return removed
}

// Len reports how many reports are cached.
func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// Stats reports how many lookups hit and missed since the cache was created. The
// window shows this so a user can see that reopening was fast, and so a cache
// that never hits is visible rather than silently useless.
func (c *Cache) Stats() (hits, misses int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits, c.misses
}

// Reset discards everything, for a user who wants to be certain.
func (c *Cache) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = map[string]cacheEntry{}
	c.hits = 0
	c.misses = 0
}

// Paths returns every cached path, sorted, for diagnostics.
func (c *Cache) Paths() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	seen := map[string]bool{}
	var out []string
	for _, entry := range c.entries {
		if !seen[entry.Path] {
			seen[entry.Path] = true
			out = append(out, entry.Path)
		}
	}
	sort.Strings(out)
	return out
}
