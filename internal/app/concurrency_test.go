package app

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

// These tests are about what happens when two requests overlap, which is the
// normal state of a window: a scan can still be finishing when the user stars a
// script, and a re-read can be running while the same script's documentation is
// being opened. They are written to be run under -race, where a shared slice
// written by one request and read by another is a failure rather than a comment.

// dotSourceTree builds a root with a script that dot-sources a helper, so the
// dependency resolution has something to resolve.
func dotSourceTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("helpers.ps1", "function Get-Target { 'example.com' }\n")
	write("deploy.ps1", `param([string]$Target)

. "$PSScriptRoot/helpers.ps1"

<#.SYNOPSIS
Deploys.
#>
`)
	return root
}

func TestExplainCarriesTheScriptsDependencies(t *testing.T) {
	// A script that dot-sources a helper is not one file, and a browser that
	// shows it as one file is misleading. The helper is named relative to the
	// root so it reads as a path within the project.
	service, _ := newTestService(t)
	root := dotSourceTree(t)
	detail, err := service.Explain(context.Background(), root, "deploy.ps1")
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Dependencies) != 1 || detail.Dependencies[0] != "helpers.ps1" {
		t.Fatalf("dependencies = %v, want [helpers.ps1]", detail.Dependencies)
	}
	if contains(detail.Dependencies, "deploy.ps1") {
		t.Error("the script itself should not be listed as its own dependency")
	}
	if len(detail.Unresolved) != 0 {
		t.Errorf("unresolved = %v, want none", detail.Unresolved)
	}
}

func TestExplainHasNoDependenciesForAScriptThatDotSourcesNothing(t *testing.T) {
	// The field is omitted rather than sent empty, so the frontend can tell
	// "nothing to report" from "not looked at".
	service, _ := newTestService(t)
	detail, err := service.Explain(context.Background(), scriptTree(t), "deploy.ps1")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Dependencies != nil {
		t.Errorf("dependencies = %v, want none", detail.Dependencies)
	}
}

func TestConcurrentWorkspaceSavesDoNotRace(t *testing.T) {
	// The workspace's mutators edit their slices in place. Handing the store the
	// live workspace let it read a slice another request was rewriting, which the
	// race detector reports as a write at workspace.go against a read at
	// store.go. Saving goes through a snapshot now.
	service, _ := newTestService(t)
	root := scriptTree(t)
	if _, err := service.AddRoot(root, "a"); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				if _, err := service.ToggleFavorite(root, "deploy.ps1"); err != nil {
					t.Error(err)
					return
				}
				_ = service.Workspace()
			}
		}()
	}
	// Flush is what runs when the window closes, which can be while a request is
	// still in flight.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 20; j++ {
			if err := service.Flush(); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	wg.Wait()
}

func TestConcurrentInvalidateAndExplainDoNotRace(t *testing.T) {
	// A memoized root view is shared by every request that names that root. A
	// re-read used to write the fresh script back into the shared view's slice
	// while Explain was walking the same slice to find related scripts. Views are
	// now replaced rather than edited.
	service, _ := newTestService(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.ps1"),
		[]byte("param([string]$A, [switch]$B)\n<#.SYNOPSIS\nA.\n#>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := service.OpenRoot(context.Background(), root); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 12; j++ {
				if _, err := service.Invalidate(context.Background(), root, "a.ps1"); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				if _, err := service.Explain(context.Background(), root, "a.ps1"); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestInvalidateReplacesTheViewRatherThanEditingIt(t *testing.T) {
	// The fix for the race is structural rather than a lock: a re-read publishes a
	// new view and leaves the old one exactly as it was, so a request part way
	// through reading the old one keeps a consistent set. That means the new view
	// must be a different object with its own storage, and the old one must come
	// through the re-read unchanged.
	service, _ := newTestService(t)
	root := scriptTree(t)
	first, err := service.OpenRoot(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	before := len(first.Scripts)
	beforeTotal := first.Meta.Total
	// A copy of the one script the re-read touches, to compare against afterwards.
	beforeDeploy := scriptAt(t, first, "deploy.ps1")

	updatedScript, err := service.Invalidate(context.Background(), root, "deploy.ps1")
	if err != nil {
		t.Fatal(err)
	}
	after, err := service.cachedRoot(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	if after == first {
		t.Error("the memoized view was edited in place instead of replaced, " +
			"so a request reading it can see it change underneath")
	}
	if len(after.Scripts) != before || after.Meta.Total != beforeTotal {
		t.Errorf("the replacement has %d scripts and a total of %d, want %d and %d",
			len(after.Scripts), after.Meta.Total, before, beforeTotal)
	}
	if &after.Scripts[0] == &first.Scripts[0] {
		t.Error("the replacement shares its script storage with the view it replaced")
	}
	// The view a reader was already holding must be untouched, down to the script
	// the re-read was about.
	held := scriptAt(t, first, "deploy.ps1")
	if !reflect.DeepEqual(beforeDeploy, held) {
		t.Error("the earlier view's copy was updated in place")
	}
	fresh := scriptAt(t, after, "deploy.ps1")
	if fresh.Cached {
		t.Error("the re-read script should not be marked as cached")
	}
	if len(fresh.Metadata.Params) != len(beforeDeploy.Metadata.Params) {
		t.Errorf("the re-read found %d params, the original had %d",
			len(fresh.Metadata.Params), len(beforeDeploy.Metadata.Params))
	}
	if updatedScript.Rel != fresh.Rel {
		t.Errorf("the returned script is %q but the memoized view holds %q", updatedScript.Rel, fresh.Rel)
	}
}

func TestSnapshotSharesNothingWithTheOriginal(t *testing.T) {
	// The snapshot is the fix for the save race, so it has to be a real copy.
	// Slices.Clone alone would not be enough if the struct held a slice that
	// Workspace's mutators rewrite in place through the shared backing array.
	original := scriptTree(t)
	service, _ := newTestService(t)
	if _, err := service.AddRoot(original, "first"); err != nil {
		t.Fatal(err)
	}
	before := service.Workspace()
	if len(before.Roots) != 1 {
		t.Fatalf("got %d roots, want 1", len(before.Roots))
	}

	if _, err := service.AddRoot(t.TempDir(), "second"); err != nil {
		t.Fatal(err)
	}
	if len(before.Roots) != 1 {
		t.Errorf("the earlier snapshot now has %d roots, so it shares storage with the live workspace", len(before.Roots))
	}
	if len(service.Workspace().Roots) != 2 {
		t.Errorf("the workspace should have 2 roots, got %d", len(service.Workspace().Roots))
	}
}

// contains is the small membership check the tests use, kept here so a test file
// does not have to import slices for one line.
func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

func TestConcurrentOpenRootDoesNotRace(t *testing.T) {
	// A re-read and a background refresh are independent requests that can
	// overlap, and both end up in the parameter reader, whose toolchain memo is a
	// shared map. Nothing in OpenRoot serializes them, and Go treats a concurrent
	// map read and write as a fatal runtime error rather than a recoverable
	// race, so a double-click on the refresh button could once take the window
	// down. The reader holds the memo under a lock; this proves it stays that way.
	service, _ := newTestService(t)
	root := scriptTree(t)

	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 6; j++ {
				if _, err := service.OpenRoot(context.Background(), root); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestConcurrentOpenRootAttributesStagesToTheirOwnRoot(t *testing.T) {
	// Two reads of different roots overlap when the user switches folders
	// quickly. The root travels in the context rather than on the Service, so
	// one read's progress is never labeled with the other read's root.
	service, _ := newTestService(t)
	first, second := scriptTree(t), dotSourceTree(t)

	var mu sync.Mutex
	byRoot := map[string][]ReadStage{}
	service.SetStageEmitter(func(_ string, data any) {
		stage, ok := data.(ReadStage)
		if !ok {
			t.Errorf("unhandled event type %T", data)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		byRoot[stage.Root] = append(byRoot[stage.Root], stage)
	})

	var wg sync.WaitGroup
	for _, root := range []string{first, second, first, second} {
		wg.Add(1)
		go func(root string) {
			defer wg.Done()
			if _, err := service.OpenRoot(context.Background(), root); err != nil {
				t.Error(err)
			}
		}(root)
	}
	wg.Wait()

	for root, stages := range byRoot {
		// Every event about a read has to name that read's root, or the window
		// cannot tell which read a late event belongs to.
		if root != first && root != second {
			t.Errorf("stage reported for an unknown root %q", root)
		}
		for _, stage := range stages {
			if stage.Root != root {
				t.Errorf("event filed under %q carries root %q", root, stage.Root)
			}
			if stage.Stage == "" {
				t.Error("a stage arrived with no name")
			}
		}
	}
}

func TestConcurrentGitRefreshesDoNotRace(t *testing.T) {
	// A focus, the manual refresh button, and a background status poll are three
	// independent requests that can land together, and they all reach the
	// repository through the same helpers. Nothing here serializes them, so this
	// is the test that keeps a double click on ↻ from reaching a shared map
	// unsynchronized.
	service, _ := newTestService(t)
	root := scriptTree(t)

	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 6; j++ {
				// Half the goroutines refresh and half re-read, so the two paths
				// that share git state overlap rather than merely coexisting.
				if (i+j)%2 == 0 {
					if _, err := service.OpenRoot(context.Background(), root); err != nil {
						t.Error(err)
						return
					}
					continue
				}
				service.RefreshGit(context.Background(), root)
			}
		}(i)
	}
	wg.Wait()
}
