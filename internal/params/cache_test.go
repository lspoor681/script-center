package params

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// writeScript creates a script with a modification time the test controls, so
// that the cache key can be exercised without racing the clock.
func writeScript(t *testing.T, dir, name, body string, modTime time.Time) (string, int64, time.Time) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, info.Size(), info.ModTime()
}

// sampleReport is a report that is obviously different from any other, so a
// test can tell which one the cache returned.
func sampleReport(path, name string) Report {
	report := Report{Path: path, Help: Help{Synopsis: "from " + name, Source: HelpSourceComment}}
	report.Params = append(report.Params, Param{Name: name, TypeName: "string", Kind: KindString})
	return report
}

func TestCacheLookupMissesOnAnEmptyCache(t *testing.T) {
	cache := NewCache("")
	if _, ok := cache.Lookup("/a.ps1", 10, time.Unix(100, 0), "powershell"); ok {
		t.Error("an empty cache should not report a hit")
	}
}

func TestCacheReturnsAStoredReport(t *testing.T) {
	cache := NewCache("")
	when := time.Unix(1700000000, 0)
	cache.Store("/a.ps1", 42, when, "powershell", sampleReport("/a.ps1", "Force"))

	report, ok := cache.Lookup("/a.ps1", 42, when, "powershell")
	if !ok {
		t.Fatal("the stored report should be found")
	}
	if report.Params[0].Name != "Force" {
		t.Errorf("params = %+v, want Force", report.Params)
	}
}

func TestCacheNoticesAnEditedScript(t *testing.T) {
	// This is the point of the cache: it must never serve a report describing a
	// file that has since changed. A wrong form would let a user run a script
	// with arguments it does not accept.
	cache := NewCache("")
	before := time.Unix(1700000000, 0)
	cache.Store("/a.ps1", 40, before, "powershell", sampleReport("/a.ps1", "Old"))

	if _, ok := cache.Lookup("/a.ps1", 40, before, "powershell"); !ok {
		t.Fatal("the unchanged script should hit")
	}
	// A later timestamp alone is a change.
	if _, ok := cache.Lookup("/a.ps1", 40, before.Add(time.Second), "powershell"); ok {
		t.Error("a later modification time should miss")
	}
	// A different length alone is a change.
	if _, ok := cache.Lookup("/a.ps1", 41, before, "powershell"); ok {
		t.Error("a different size should miss")
	}
}

func TestCacheNoticesARenamedLanguage(t *testing.T) {
	// The same path with a different extension is a different script, and its
	// report would not describe the new file's arguments.
	cache := NewCache("")
	when := time.Unix(1700000000, 0)
	cache.Store("/a.ps1", 40, when, "powershell", sampleReport("/a.ps1", "Force"))

	if _, ok := cache.Lookup("/a.ps1", 40, when, "python"); ok {
		t.Error("a different language should miss")
	}
}

func TestCacheIgnoresPathCaseOnlyDifferences(t *testing.T) {
	// Windows and macOS both treat paths case-insensitively, so the same script
	// must not be cached twice under two spellings and read twice.
	cache := NewCache("")
	when := time.Unix(1700000000, 0)
	cache.Store(`/a/Script.ps1`, 40, when, "powershell", sampleReport(`/a/Script.ps1`, "Force"))

	if _, ok := cache.Lookup(`/a/script.ps1`, 40, when, "powershell"); !ok {
		t.Error("a case-different spelling of the same path should hit")
	}
}

func TestCacheSurvivesASaveAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "params.json")
	first := NewCache(path)
	when := time.Unix(1700000000, 0)
	first.Store("/a.ps1", 40, when, "powershell", sampleReport("/a.ps1", "Force"))
	if err := first.Save(); err != nil {
		t.Fatal(err)
	}

	// The nested directory is created rather than failing, because the caller's
	// config directory may not exist on a first run.
	second := NewCache(path)
	if err := second.Load(); err != nil {
		t.Fatalf("loading a saved cache: %v", err)
	}
	report, ok := second.Lookup("/a.ps1", 40, when, "powershell")
	if !ok {
		t.Fatal("the saved report should be found after a reload")
	}
	if report.Help.Synopsis != "from Force" {
		t.Errorf("synopsis = %q, want it preserved through JSON", report.Help.Synopsis)
	}
}

func TestCacheLoadOnAMissingFileIsNotAnError(t *testing.T) {
	// A first run has no cache, and that is the normal case rather than a fault.
	cache := NewCache(filepath.Join(t.TempDir(), "absent.json"))
	if err := cache.Load(); !errors.Is(err, ErrNoCache) {
		t.Errorf("Load() = %v, want ErrNoCache", err)
	}
	if cache.Len() != 0 {
		t.Error("a cache that loaded nothing should be empty")
	}
}

func TestCacheLoadDiscardsACorruptFile(t *testing.T) {
	// A cache that cannot be read is discarded rather than reported: refusing to
	// show parameters because a cache file is corrupt would be the worse fault.
	path := filepath.Join(t.TempDir(), "params.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	cache := NewCache(path)
	if err := cache.Load(); !errors.Is(err, ErrNoCache) {
		t.Errorf("Load() = %v, want ErrNoCache", err)
	}
	if cache.Len() != 0 {
		t.Error("a corrupt file should leave the cache empty")
	}
}

func TestCacheLoadDiscardsAnotherVersion(t *testing.T) {
	// After a harvester starts reporting something different, last run's answers
	// are wrong and must not be shown as though they were current.
	path := filepath.Join(t.TempDir(), "params.json")
	stale := `{"version":` + itoa(CacheVersion+1) + `,"entries":{"k":{"path":"/a.ps1","size":40,` +
		`"modTime":"2023-11-14T22:13:20Z","lang":"powershell","report":{"path":"/a.ps1"}}}}`
	if err := os.WriteFile(path, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	cache := NewCache(path)
	if err := cache.Load(); !errors.Is(err, ErrNoCache) {
		t.Errorf("Load() = %v, want ErrNoCache", err)
	}
	if cache.Len() != 0 {
		t.Error("a cache from another version should be ignored")
	}
}

func TestCacheSaveLeavesNoTemporaryFile(t *testing.T) {
	// The write goes to a temporary file and is renamed, and the temporary file
	// must not be left behind on either path.
	dir := t.TempDir()
	path := filepath.Join(dir, "params.json")
	cache := NewCache(path)
	cache.Store("/a.ps1", 40, time.Unix(1700000000, 0), "powershell", sampleReport("/a.ps1", "Force"))
	if err := cache.Save(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "params.json" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("directory holds %v, want only params.json", names)
	}
}

func TestCacheInvalidateDropsEveryEntryForAPath(t *testing.T) {
	// A user who has edited a script by some means that preserved its size and
	// timestamp needs a way to be certain, so every entry for the path goes
	// regardless of the fingerprint it was stored under.
	cache := NewCache("")
	first := time.Unix(1700000000, 0)
	second := first.Add(time.Hour)
	cache.Store("/a.ps1", 40, first, "powershell", sampleReport("/a.ps1", "Force"))
	cache.Store("/a.ps1", 40, second, "powershell", sampleReport("/a.ps1", "Force"))
	cache.Store("/b.ps1", 40, first, "powershell", sampleReport("/b.ps1", "Force"))

	if removed := cache.Invalidate("/a.ps1"); removed != 2 {
		t.Errorf("Invalidate removed %d, want 2", removed)
	}
	if _, ok := cache.Lookup("/b.ps1", 40, first, "powershell"); !ok {
		t.Error("another script's entry should survive")
	}
}

func TestCachePruneStaleDropsOnlyFilesThatAreGone(t *testing.T) {
	// The cache holds every root in the workspace, so pruning must decide from
	// the filesystem and not from the root that happens to be open. Dropping
	// another root's entries would silently undo its caching.
	dir := t.TempDir()
	kept := filepath.Join(dir, "kept.ps1")
	if err := os.WriteFile(kept, []byte("param([switch]$Force)"), 0o644); err != nil {
		t.Fatal(err)
	}
	gone := filepath.Join(dir, "gone.ps1")
	if err := os.WriteFile(gone, []byte("param([switch]$Force)"), 0o644); err != nil {
		t.Fatal(err)
	}

	when := time.Unix(1700000000, 0)
	cache := NewCache("")
	cache.Store(kept, 40, when, "powershell", sampleReport(kept, "Force"))
	cache.Store(gone, 40, when, "powershell", sampleReport(gone, "Force"))
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}

	if removed := cache.PruneStale(); removed != 1 {
		t.Errorf("PruneStale removed %d, want 1", removed)
	}
	if _, ok := cache.Lookup(gone, 40, when, "powershell"); ok {
		t.Error("an entry for a deleted script should be gone")
	}
	if _, ok := cache.Lookup(kept, 40, when, "powershell"); !ok {
		t.Error("an entry for a file that still exists should be kept")
	}
}

func TestCachePruneStaleKeepsAChangedFile(t *testing.T) {
	// A file that changed is not gone. Its key no longer matches, so the next
	// lookup misses and overwrites it; removing it here would only cost a
	// re-read and would throw away a root the caller never mentioned.
	dir := t.TempDir()
	path := filepath.Join(dir, "edit.ps1")
	if err := os.WriteFile(path, []byte("param([switch]$Force)"), 0o644); err != nil {
		t.Fatal(err)
	}
	when := time.Unix(1700000000, 0)
	cache := NewCache("")
	cache.Store(path, 40, when, "powershell", sampleReport(path, "Force"))

	if err := os.WriteFile(path, []byte("param([switch]$Force, [string]$Target)"), 0o644); err != nil {
		t.Fatal(err)
	}
	if removed := cache.PruneStale(); removed != 0 {
		t.Errorf("PruneStale removed %d, want 0 for a file that changed", removed)
	}
	if _, ok := cache.Lookup(path, 40, when, "powershell"); !ok {
		t.Error("the old entry should still be there, it just no longer matches")
	}
}

func TestCacheStatsCountHitsAndMisses(t *testing.T) {
	// The window shows these, and a cache that never hits should be noticeable
	// rather than silently useless.
	cache := NewCache("")
	when := time.Unix(1700000000, 0)
	cache.Store("/a.ps1", 40, when, "powershell", sampleReport("/a.ps1", "Force"))
	cache.Lookup("/a.ps1", 40, when, "powershell")
	cache.Lookup("/new.ps1", 40, when, "powershell")

	hits, misses := cache.Stats()
	if hits != 1 || misses != 1 {
		t.Errorf("Stats() = %d hits, %d misses; want 1 and 1", hits, misses)
	}
}

func TestCacheResetEmptiesIt(t *testing.T) {
	cache := NewCache("")
	when := time.Unix(1700000000, 0)
	cache.Store("/a.ps1", 40, when, "powershell", sampleReport("/a.ps1", "Force"))
	cache.Lookup("/a.ps1", 40, when, "powershell")

	cache.Reset()
	if cache.Len() != 0 {
		t.Error("Reset should empty the cache")
	}
	if hits, misses := cache.Stats(); hits != 0 || misses != 0 {
		t.Errorf("Stats() = %d, %d; want both zeroed", hits, misses)
	}
}

func TestCachePathsAreSortedAndUnique(t *testing.T) {
	cache := NewCache("")
	when := time.Unix(1700000000, 0)
	cache.Store("/b.ps1", 40, when, "powershell", sampleReport("/b.ps1", "Force"))
	cache.Store("/a.ps1", 40, when, "powershell", sampleReport("/a.ps1", "Force"))
	// A second fingerprint for the same path must not be listed twice.
	cache.Store("/a.ps1", 41, when, "powershell", sampleReport("/a.ps1", "Force"))

	got := cache.Paths()
	if len(got) != 2 || got[0] != "/a.ps1" || got[1] != "/b.ps1" {
		t.Errorf("Paths() = %v, want /a.ps1 and /b.ps1", got)
	}
}

func TestCacheIgnoresSubSecondModTime(t *testing.T) {
	// Modification times are stored at whatever resolution the filesystem gives,
	// which is not always the nanosecond resolution the value claims. Truncating
	// keeps a script that was not actually edited from missing on every reopen.
	cache := NewCache("")
	stored := time.Unix(1700000000, 0)
	cache.Store("/a.ps1", 40, stored, "powershell", sampleReport("/a.ps1", "Force"))

	// A filesystem that rounds the timestamp up by a fraction is the same file.
	rounded := stored.Add(400 * time.Millisecond)
	if _, ok := cache.Lookup("/a.ps1", 40, rounded, "powershell"); !ok {
		t.Error("a sub-second difference in the same second should still hit")
	}
}

func TestCacheWithoutAPathDoesNotWriteFiles(t *testing.T) {
	// An empty path gives an in-memory cache, so saving it must be a no-op rather
	// than an error or a file in the working directory.
	cache := NewCache("")
	if err := cache.Save(); err != nil {
		t.Errorf("Save() on a memory-only cache = %v, want nil", err)
	}
	if err := cache.Load(); !errors.Is(err, ErrNoCache) {
		t.Errorf("Load() on a memory-only cache = %v, want ErrNoCache", err)
	}
}

func TestCacheStoreRecordsThePathOnTheReport(t *testing.T) {
	// The report is looked up by path, so it carries one itself and a caller
	// displaying it does not have to set it again.
	cache := NewCache("")
	cache.Store("/a.ps1", 40, time.Unix(1700000000, 0), "powershell", Report{})
	report, _ := cache.Lookup("/a.ps1", 40, time.Unix(1700000000, 0), "powershell")
	if report.Path != "/a.ps1" {
		t.Errorf("report path = %q, want /a.ps1", report.Path)
	}
}

func TestCacheIsSafeForConcurrentUse(t *testing.T) {
	// A scan reads across several goroutines, and the race detector is the only
	// thing that will catch a missing lock here.
	cache := NewCache("")
	when := time.Unix(1700000000, 0)
	done := make(chan struct{})
	for worker := range 8 {
		go func() {
			defer func() { done <- struct{}{} }()
			for i := range 50 {
				path := filepath.Join("/tmp", string(rune('a'+worker)), itoa(i))
				cache.Store(path, 40, when, "bash", sampleReport(path, "Force"))
				cache.Lookup(path, 40, when, "bash")
				cache.Stats()
				cache.Paths()
			}
		}()
	}
	for range 8 {
		<-done
	}
}

// itoa is a local helper so the test file does not pull in strconv for two uses.
func itoa(n int) string { return strconv.Itoa(n) }
