// Command script-center is the desktop application.
//
// The window is created here and nowhere else; every behavior it can reach lives
// in internal/app, which is an ordinary package that can be tested without a
// webview. That split is why main.go has almost no logic in it: there is one
// implementation of each behavior, and this file only decides how to show it.
package main

import (
	"context"
	"embed"
	"fmt"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	if err := run(); err != nil {
		// A failed window leaves nothing to show the error in, so it goes to the
		// terminal. A user who launched from a file manager will not see it, which
		// is why the exit code matters too.
		fmt.Fprintln(os.Stderr, "script-center:", err)
		os.Exit(1)
	}
}

// run starts the window.
func run() error {
	application := NewApp()

	// The dialog is the one thing that needs the window, so it is filled in here
	// where the runtime exists and stays nil-able in the service package, which
	// never asks.
	openDirectoryDialog = func(ctx context.Context) (string, error) {
		return runtime.OpenDirectoryDialog(ctx, runtime.OpenDialogOptions{
			Title: "Add a folder to your workspace",
		})
	}

	openFileDialog = func(ctx context.Context, title string) (string, error) {
		return runtime.OpenFileDialog(ctx, runtime.OpenDialogOptions{
			Title: title,
			Filters: []runtime.FileFilter{
				{DisplayName: "All Files", Pattern: "*"},
			},
		})
	}

	return wails.Run(&options.App{
		Title:     "Script Center",
		Width:     1280,
		Height:    800,
		MinWidth:  900,
		MinHeight: 600,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 18, G: 18, B: 20, A: 1},
		OnStartup:        application.startup,
		OnShutdown:       application.shutdown,
		Mac: &mac.Options{
			// The app has a single window and a menu bar of its own would only
			// offer actions the user cannot perform without one.
			TitleBar:             mac.TitleBarHiddenInset(),
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			About: &mac.AboutInfo{
				Title:   "Script Center",
				Message: "Browse, understand, and run the scripts in your projects.",
			},
		},
		Bind: []any{application},
	})
}
