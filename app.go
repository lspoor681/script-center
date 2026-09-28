package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/lspoor/script-center/internal/app"
)

// App is the object Wails binds to the frontend.
//
// It holds no logic of its own. Everything the window can do is a method on
// internal/app.Service, which is an ordinary Go package that can be tested
// without a webview, so there is one implementation of each behavior rather than
// one here and one there.
type App struct {
	ctx     context.Context
	service *app.Service
}

// NewApp creates the application, keeping its state in the platform's config
// directory so that roots and favorites survive a restart.
func NewApp() *App {
	return &App{service: app.New(configDir())}
}

// startup is called when the app starts. The context is saved so the runtime
// methods can reach it, and the runner's events are routed to the window.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	// A failure to load saved state is not worth refusing to open a window over:
	// the user can add their roots again, and being unable to start at all would
	// leave them with no way to do even that.
	a.service.SetRunEmitter(func(name string, data any) {
		runtime.EventsEmit(a.ctxOrBackground(), name, data)
	})
	_ = a.service.Start()
}

// shutdown is called when the app closes, which is the last chance to write
// anything down.
func (a *App) shutdown(_ context.Context) {
	_ = a.service.Flush()
}

// ctxOrBackground returns the startup context, or a background one before startup
// has run, so a method called early fails nothing by panicking.
func (a *App) ctxOrBackground() context.Context {
	if a.ctx == nil {
		return context.Background()
	}
	return a.ctx
}

// Status reports what the app loaded, so the window can show it.
func (a *App) Status() app.Status { return a.service.Status() }

// Workspace returns the roots and favorites.
func (a *App) Workspace() any { return a.service.Workspace() }

// AddRoot adds a directory to the workspace.
func (a *App) AddRoot(path, name string) (any, error) {
	return a.service.AddRoot(path, name)
}

// RemoveRoot forgets a directory.
func (a *App) RemoveRoot(path string) (any, error) { return a.service.RemoveRoot(path) }

// OpenRoot scans a root and reads metadata for every script in it.
func (a *App) OpenRoot(root string) (*app.RootView, error) {
	return a.service.OpenRoot(a.ctxOrBackground(), root)
}

// Explain returns a script's documentation and the scripts related to it.
func (a *App) Explain(root, rel string) (*app.ExplainView, error) {
	return a.service.Explain(a.ctxOrBackground(), root, rel)
}

// RenderDocument returns one of a script's readmes or sidecars as sanitized
// HTML, ready to display.
func (a *App) RenderDocument(root, rel, path string) (*app.DocumentView, error) {
	return a.service.RenderDocument(a.ctxOrBackground(), root, rel, path)
}

// Invalidate forgets what is cached for one script and reads it again.
func (a *App) Invalidate(root, rel string) (*app.ScriptView, error) {
	return a.service.Invalidate(a.ctxOrBackground(), root, rel)
}

// RunScript starts a script and streams its output to the window as events.
//
// Extra is passed through to the script, so a user can run it with options
// without editing anything.
func (a *App) RunScript(root, rel string, extra []string) (*app.RunView, error) {
	return a.service.RunScript(a.ctxOrBackground(), root, rel, extra)
}

// StopScript stops a running script.
func (a *App) StopScript(id string) error {
	return a.service.StopScript(id)
}

// ToggleFavorite stars or unstars a script.
func (a *App) ToggleFavorite(root, rel string) (any, error) {
	return a.service.ToggleFavorite(root, rel)
}

// Toolchains reports which languages can be read on this machine.
func (a *App) Toolchains() []app.Toolchain { return a.service.Toolchains() }

// ChooseRoot asks the user for a directory and adds it to the workspace.
func (a *App) ChooseRoot() (any, error) {
	path, err := openDirectoryDialog(a.ctxOrBackground())
	if err != nil {
		return nil, err
	}
	if path == "" {
		// A canceled dialog is not an error; the user changed their mind.
		return a.service.Workspace(), nil
	}
	return a.service.AddRoot(path, "")
}

// openDirectoryDialog asks for a directory.
//
// The dialog is a runtime service, so it is reached through a small indirection
// that main can fill in. Keeping it out of the service package is what lets that
// package be tested without a window, and the window is the only thing that can
// know where to put the dialog.
var openDirectoryDialog = func(context.Context) (string, error) {
	return "", errNoDialog
}

// errNoDialog reports that the dialog is unavailable, which happens in a build
// without a desktop runtime.
var errNoDialog = errors.New("main: no desktop dialog is available in this build")

// configDir returns the directory the app keeps its state in.
func configDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		// Falling back to the working directory keeps a broken environment from
		// stopping the app, at the cost of state that does not outlive it.
		return "."
	}
	return filepath.Join(dir, "script-center")
}
