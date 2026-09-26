package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestNewAppIsUsableBeforeStartup(t *testing.T) {
	// The window's methods can be called before OnStartup has run, and a method
	// that panics on a nil context would take the window down rather than fail
	// one request.
	application := NewApp()
	if application.service == nil {
		t.Fatal("NewApp should build a service")
	}
	if err := application.service.Start(); err != nil {
		t.Fatal(err)
	}
	if got := application.ctxOrBackground(); got == nil {
		t.Error("ctxOrBackground should never be nil")
	}
	application.startup(context.Background())
	if got := application.ctxOrBackground(); got == nil {
		t.Error("startup should record the context")
	}
}

func TestShutdownIsSafeWithoutStartup(t *testing.T) {
	// Closing the window during a failed start must not panic on a state that was
	// never built.
	NewApp().shutdown(context.Background())
}

func TestConfigDirIsAbsoluteWhenTheEnvironmentCooperates(t *testing.T) {
	// State is written under the platform's config directory so that roots and
	// favorites survive a restart, and a relative path would not.
	dir := configDir()
	if dir == "" {
		t.Fatal("configDir returned nothing")
	}
	if dir != "." {
		if !filepath.IsAbs(dir) {
			t.Errorf("configDir() = %q, want an absolute path", dir)
		}
		// It is namespaced to the app so it cannot collide with another tool.
		if filepath.Base(dir) != "script-center" {
			t.Errorf("configDir() = %q, want it to end in the app's own name", dir)
		}
	}
}

func TestConfigDirFollowsTheEnvironment(t *testing.T) {
	// The user config directory is the one the platform designates, so the app
	// keeps its state where the rest of the user's configuration lives.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if home, err := os.UserConfigDir(); err == nil {
		if !filepath.IsAbs(home) {
			t.Skipf("this platform has no XDG support (UserConfigDir = %q)", home)
		}
	}
	if got := configDir(); got != filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "script-center") {
		t.Errorf("configDir() = %q, want it under XDG_CONFIG_HOME", got)
	}
}

func TestDialogIsUnavailableUntilTheWindowExists(t *testing.T) {
	// The dialog needs a desktop runtime, which only main.go has. Until one is
	// installed the call has to fail rather than pretend to have opened a dialog.
	if _, err := openDirectoryDialog(context.Background()); err == nil {
		t.Error("the dialog should report that it is unavailable")
	}
}
