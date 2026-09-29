# Script Center

Script Center is a desktop library for the scripts you already keep in folders.
Point it at a directory and it finds every PowerShell, Python, Bash, Rust or
batch script in it, works out what each one accepts, what it needs, and what
explains it, shows the whole thing in one window, and runs them.

It is a Wails v2 application: a Go backend (the engine, the file access, the
state) wrapped in a React 19 + TypeScript + Vite 7 frontend (the window).

The feature backlog and completed work are tracked in
[`docs/TODO.md`](docs/TODO.md); recent changes are recorded in
[`docs/CHANGELOG.md`](docs/CHANGELOG.md).

## What it does

- **Folders as roots.** Add any number of directories and give them names. A
  root that is a git repository is enumerated with `git ls-files`, so ignore
  rules are exact; any other directory is walked with a conservative skip list
  for build artifacts and caches.
- **Search.** A box above the list filters the selected root's loaded scripts as
  you type, matching case-insensitively against the fields the list shows plus
  the synopsis and the parameter names, with the first hit in each row
  highlighted so you can see why a script matched.
- **Parameter forms.** For each script, Script Center reports its inputs: name,
  aliases, type, default, whether it is required or positional, and any
  validation rules (allowed values, ranges, regexes). It harvests these from the
  language itself where one exists — PowerShell's own parser, Python's `ast`
  and argparse declarations, and a careful text reader for bash — and prefers to
  show nothing over showing something wrong. Scripts whose inputs it cannot read
  (Rust, batch, or hand-parsed arguments) are still listed, with a warning that
  says so.
- **ReadMe.** Sidecar notes (same-stem `.md` files, or a `docs/` folder)
  and project READMEs, rendered through sanitized HTML. A README is excerpted to
  the section that names the script, and the most specific document is shown
  first.
- **Dependencies.** PowerShell `#Requires` declarations, dot-sourced files, and
  a "Needs" list telling you what a copy of the script has to bring with it.
- **Run locally.** A run bar in the script view starts the script with any extra
  arguments you type and opens a terminal pane that streams its output. The
  process working directory is the script's own directory, so relative paths and
  dot-sourced files resolve; the pane reports the exact argv it used, and offers
  Stop, Rerun and Copy command. One run at a time.
- **Run as administrator.** Elevation is used wherever the platform lets the app
  keep the run attached: Unix `sudo`, or Windows sudo in its inline mode
  (Windows 11 24H2 or later). When Windows sudo is absent or not in inline mode,
  the script starts elevated in a window of its own under a UAC prompt and the
  panel says the run is blind — no streaming, no stop, but the exit code still
  arrives. Unix has no such fallback, so an elevated run there is refused rather
  than detached. The Admin checkbox is auto-checked and locked for
  `#Requires -RunAsAdministrator`.
- **Input to a running script.** A box under the terminal sends one line to the
  child, with the platform's line ending, so interactive prompts — a sudo
  password, a `Read-Host` — can be answered.
- **Right-click menu.** Run, Run (Administrator), Star, Copy path and Open file
  location from a menu on any script row. Opening a file location reveals it
  selected in Explorer, Finder or the Linux file manager.
- **Git and GitHub status.** A strip under the root list summarises the
  repository: branch or detached HEAD, ahead/behind the upstream, staged,
  unstaged, untracked and conflicted counts, the last commit, and a link to open
  the repository on GitHub. A script you have edited since the last commit is
  badged in the list. A ↻ button re-reads the root so the strip does not stall
  after edits.
- **Favorites.** Star scripts across all roots; favorites survive restarts.
- **Related scripts.** Each script suggests up to six other scripts in its root,
  with the reason.
- **Re-read.** A button on a root re-reads the whole folder and one on a script
  re-reads just that file, because the caches trust a file's size and timestamp.

Scripts that cannot be fully read are never hidden: they appear with the reason
as a warning. Missing toolchains (PowerShell, Python) are reported in the window
instead of making their scripts disappear.

## Requirements

- **Go 1.25 or newer.** The module declares `go 1.25.0`; CI runs the current 1.27.x.
- **Wails CLI v2.14.0** and, for local lint, **golangci-lint v2.14.0**. Both are
  installed identically, see below.
- **Node.js and npm** for the frontend build. Vite 7 requires `^20.19.0 ||
  >=22.12.0`; CI uses Node 22.
- **Windows:** the WebView2 runtime (present on current Windows 10/11). A normal
  build needs no C compiler; the race detector does, see the note below.
- **Linux:** GTK3 and WebKitGTK 4.1 development packages plus `pkg-config`
  (`libgtk-3-dev libwebkit2gtk-4.1-dev` on Debian/Ubuntu), and the
  `webkit2_41` build tag used by the Makefile and CI.
- **To run scripts:** the interpreter each script needs — PowerShell (PowerShell
  7 / `pwsh`, or Windows PowerShell 5.1) for PowerShell, Python 3 for Python,
  Bash for shell — and, for an elevated run, `sudo`: the Unix `sudo`, or the
  Windows sudo of Windows 11 24H2 or later. Without it an elevated run falls
  back to a detached, UAC-prompted window on Windows and is refused on Unix. The
  same interpreters harvest parameter metadata; a missing one is reported in the
  toolchain footer rather than treated as a failure.

## Installing the toolchain

```sh
go install github.com/wailsapp/wails/v2/cmd/wails@v2.14.0
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0
cd frontend && npm install
```

`go install` puts the binaries in `$(go env GOBIN)` (usually `$(go env
GOPATH)/bin`); make sure that directory is on your `PATH`.

## Development and building

With the Wails CLI installed and the frontend dependencies installed, the
`Makefile` drives everything on Unix-like systems:

```sh
make dev        # run the app with frontend hot reload
make build      # build a redistributable (passes the webkit2_41 tag)
make check      # fmt-check + vet + lint + test, everything CI runs
```

The `Makefile` is Unix-oriented (`rm`, `grep`, `awk`), so on Windows call the
tools directly:

```sh
wails dev
wails build
```

Both commands run the frontend through npm (`npm install` then `npm run build`)
as part of the build, per `wails.json`.

## Testing

The backend is ordinary Go, tested without a webview:

```sh
go test ./...                 # the suite
go test -race -count=1 ./...  # what CI runs
go test -v -count=1 -run 'TestPTY' ./internal/pty/   # interactive-shell smoke tests
go vet ./...
golangci-lint run
cd frontend && npm run build  # type-check and bundle the frontend
```

CI (`.github/workflows/ci.yml`) runs three jobs on pushes to `main`, on every
pull request, and on demand:

- **Lint** (ubuntu): `gofmt -s -l` over the Go tree, then golangci-lint.
- **Test** (ubuntu + windows): `go vet`, the `TestPTY` smoke tests, then
  `go test -race -count=1 ./...`. The Windows job installs a MinGW
  toolchain first, because the race detector needs cgo and a C compiler.
- **Build** (ubuntu + windows): installs Node and Wails, runs
  `wails build -tags webkit2_41` (the tag is unconditional — it is required on
  ubuntu-latest, which has no WebKitGTK 4.0), and uploads the produced
  binaries. The Windows job installs the same MinGW toolchain, so a dependency
  that gains a cgo component cannot break the Windows build in CI while passing
  locally.

The PTY smoke tests deliberately run on Windows: a loss of pseudo-terminal
support cross-compiles cleanly and only fails at runtime, so those tests are the
one thing that catches it.

## Notes for Windows developers

- **Line endings and formatting.** This repository pins every checkout to LF
  via `.gitattributes`. gofmt and golangci-lint require LF, so a checkout that
  predates the file will show whole-repository gofmt noise. Fix an existing
  clone once by renormalizing the working tree:
  `git add --renormalize . && git checkout-index -f -a` (on a clean tree), or
  simply re-clone. The committed bytes are LF and CI is unaffected.
- **The race detector needs a C compiler.** `go test -race` requires CGO
  (`CGO_ENABLED=1` and a working `gcc`-compatible toolchain). CI installs the
  WinLibs POSIX/UCRT build of MinGW on the Windows jobs; the POSIX thread model
  matters, because the win32-thread builds cannot link the race runtime. A stock
  Go install on Windows with no compiler cannot run the race detector, only the
  plain suite.
- **`make` is optional.** On Windows run the `go` and `wails` commands directly.

## Project layout

Mirrors the frontend bindings in [`app.go`](app.go), which is deliberately thin:
every behavior lives in `internal/app.Service` so it can be tested without a
window.

| Path | Role |
| --- | --- |
| `app.go` | Wails binding: `App` forwards every method to `internal/app`. |
| `internal/app` | The application's behavior: workspace, scanning + a memoized view cache, documentation, status, the script runner, and the reveal and clipboard actions. |
| `internal/workspace` | Roots and favorites, persisted to `workspaces.json` with atomic writes; related-script suggestions. |
| `internal/scan` | Enumerates a root: `git ls-files` inside a repository, a recursive walk with a static ignore list otherwise; separates scripts from Cargo-style project directories. |
| `internal/detect` | Language detection by extension, shebang, or leading content (`.ps1`, `.py`, `.sh`, `.rs`, `.bat`, extensionless candidates). |
| `internal/params` | Harvests parameter metadata and help from PowerShell, Python and bash; a report cache keyed on size and mtime, written to `params-cache.json`. |
| `internal/docs` | Finds sidecar notes and project READMEs, excerpts the section about a script, renders Markdown to sanitized HTML (goldmark + bluemonday). |
| `internal/deps` | Turns dot-sourced files and `#Requires` lines into a copy closure. |
| `internal/git` | Shells out to git for tracked-file lists and repository state. |
| `internal/pty` | Cross-platform pseudo-terminal wrapper (`crosspty`): the interactive shells, and the substrate a script run streams through. |
| `internal/elevate` | Decides how to run a script with administrator rights on each platform, and how to fall back when the run cannot stay attached. |
| `internal/proc` | The platform details of launching background helpers: `NoWindow` (stop a helper flashing a console window on Windows), `Reveal` in the file manager, `CopyText` to the clipboard. |
| `frontend/` | React 19 + TypeScript + Vite UI: folders sidebar, search, git strip, script list, and a detail pane with the parameter form, documentation, dependencies, related scripts, a run bar and a live terminal pane. |

## Where state lives

Everything is stored under the platform's config directory, so roots and
favorites survive restarts:

```
Windows: %AppData%\script-center\
Linux and macOS: $XDG_CONFIG_HOME/script-center/   (default ~/.config/script-center/)

workspaces.json     roots and favorites
params-cache.json   harvested parameter reports, keyed on file size and mtime
```

Both files are reloaded best-effort: a first run has none, and a corrupt or
unreadable one costs the saved state rather than the ability to start the app.

## Why `frontend/dist` is tracked

`main.go` embeds `all:frontend/dist`, so the directory must exist for `go
build`, `go test`, `go vet` and the linters too — not just for `wails build`,
which produces it from the frontend. `frontend/dist/DIST.placeholder` makes a
clean clone satisfy the embed before the frontend has ever been built, and a
real build writes its output alongside that placeholder rather than removing it.

Nothing regenerates the placeholder, so if you delete it — `make clean` and a
spare `git clean` both will — Go's vet, test and lint stages will fail on an
empty embed. Restore it with:

```sh
git checkout frontend/dist/DIST.placeholder
```
