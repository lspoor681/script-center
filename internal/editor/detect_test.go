package editor

import "testing"

// platformDetect is defined in the build-tagged files alongside the tests
// (detect_expect_windows_test.go, detect_expect_darwin_test.go,
// detect_expect_other_test.go) and returns the detection routine this platform
// is expected to dispatch to. Naming it per platform rather than switching on
// runtime.GOOS is deliberate: a switch here would reference DetectWindows from
// every platform again, so the test suite would not build on Linux, which is
// the very failure it exists to catch.

// TestDetectPlatformMatchesCurrentOS is the regression test for the dispatch
// itself. The dispatch used to be a switch on runtime.GOOS inside detect.go,
// which compiles fine only on the platform whose file happens to be present:
// on Linux the Windows file is excluded by its build constraint, so the branch
// referencing it failed with "undefined: DetectWindows". This test names the
// platform routine directly, so it fails to build on any platform where the
// dispatch and the files disagree.
func TestDetectPlatformMatchesCurrentOS(t *testing.T) {
	want := platformDetect()
	if len(want) == 0 {
		t.Fatal("platform detection returned no editors")
	}

	got := (&Detector{}).detectPlatform()
	if len(got) != len(want) {
		t.Fatalf("detectPlatform() returned %d editors, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].ID != want[i].ID {
			t.Errorf("editor %d: detectPlatform() = %q, want %q", i, got[i].ID, want[i].ID)
		}
	}
}

// TestDetectionAlwaysOffersTheSystemDefault covers the invariant every
// platform implementation has to keep: the list ends with the system default,
// because it is the only editor guaranteed to exist on any machine and the
// picker would otherwise be able to come up empty.
func TestDetectionAlwaysOffersTheSystemDefault(t *testing.T) {
	editors := platformDetect()
	last := editors[len(editors)-1]
	if last.ID != "system" {
		t.Errorf("last editor = %q, want the system default %q", last.ID, "system")
	}
	if last.Executable != "" {
		t.Errorf("system default executable = %q, want empty so the OS picks the handler", last.Executable)
	}
	if last.Terminal {
		t.Error("system default is marked as a terminal editor, want a direct OS handoff")
	}
}

// TestEditorIDsAreUnique guards the editor picker, which keys off the ID. A
// duplicate would make two entries indistinguishable and silently drop one.
func TestEditorIDsAreUnique(t *testing.T) {
	seen := make(map[string]bool)
	for _, e := range platformDetect() {
		if seen[e.ID] {
			t.Errorf("duplicate editor ID %q", e.ID)
		}
		seen[e.ID] = true
	}
}

// TestDetectorCachesUntilInvalidated checks that the scan runs once. Detection
// stats and looks up a dozen executables, so a repeated call would be a visible
// cost every time the picker is opened.
func TestDetectorCachesUntilInvalidated(t *testing.T) {
	d := &Detector{}

	first := d.Detect()
	if !d.cached {
		t.Error("Detect() did not mark the detector cached")
	}

	// Invalidate then detect again: the fresh scan must produce the same set,
	// so a cache that never cleared would be indistinguishable from one that
	// works, and the IDs are what callers actually use.
	d.Invalidate()
	if d.cached || d.cache != nil {
		t.Error("Invalidate() left the cache populated")
	}

	second := d.Detect()
	if len(first) != len(second) {
		t.Fatalf("re-detected %d editors, want %d", len(second), len(first))
	}
	for i := range first {
		if first[i].ID != second[i].ID {
			t.Errorf("editor %d after re-detect = %q, want %q", i, second[i].ID, first[i].ID)
		}
	}
}

// TestDetectPlatformEditorsIsIndependent covers the exported entry point used
// by internal/proc, which must not hand back a caller-owned slice that a later
// Invalidate could pull the ground out from under.
func TestDetectPlatformEditorsIsIndependent(t *testing.T) {
	editors := DetectPlatformEditors()
	if len(editors) == 0 {
		t.Fatal("DetectPlatformEditors() returned no editors")
	}
	editors[0].ID = "mutated"
	if again := DetectPlatformEditors(); again[0].ID == "mutated" {
		t.Error("mutating the returned slice affected a later call")
	}
}
