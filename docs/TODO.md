# TODO

A living tracker for Script Center's work. Completed items are checked off; the
rest are the project's roadmap. The feature backlog described here is the
current plan of record.

## Done

- [x] **Script discovery and scanning.** A root that is a git repository is
      enumerated with `git ls-files`; anything else with a recursive walk and a
      conservative artifact/cache skip list.
- [x] **Language detection.** Extension, shebang, then content signatures for
      PowerShell, Python, Bash, Rust and batch.
- [x] **Parameter forms.** Metadata harvested from PowerShell, Python and bash,
      cached in `params-cache.json` (keyed on size and mtime).
- [x] **ReadMe / documentation.** Sidecar notes (`{stem}.md`, `docs/`) and
      project READMEs rendered to sanitized HTML, section-excerpted for READMEs.
- [x] **Dependency closure.** `#Requires` and dot-sourced files collapsed into a
      "Needs" list a copy of the script has to bring with it.
- [x] **Favorites.** Star scripts across roots; favorites survive restarts.
- [x] **Related scripts.** Up to six suggestions per script, each with a reason.
- [x] **Missing-toolchain reporting.** PowerShell/Python availability shown in
      the window instead of hiding scripts.
- [x] **Windows hardening.** Interactive `powershell.exe -NoLogo -NoProfile`
      argv, a Windows rename retry, portable tests, and a `wails build` verified
      locally on Windows. The race detector runs on Windows too, via a WinLibs
      POSIX/UCRT MinGW toolchain (`CGO_ENABLED=1`), installed on the CI test
      *and* build jobs.
- [x] **CI checks configured.** Lint, test (ubuntu + windows, `-race`) and
      build jobs are configured; `.gitattributes` pins checkouts to LF. The
      build job deliberately does not set `NODE_ENV=production`, because wails
      runs `npm install` and npm would then omit devDependencies, leaving `tsc`
      and `vite` missing.

## Shipped

- [x] **TODO tracker.** This file, linked from the README.
- [x] **ReadMe relabel.** Retitle the script view's "Documentation" section to
      "ReadMe" — not every script has a dedicated readme.
- [x] **Search.** A box above the list filters the currently selected root's
      already-loaded scripts as you type. Case-insensitive substring match over
      the fields the list shows (name, path relative to the root, directory,
      language) plus the synopsis and the parameter names, so a script matches
      on a word you remember even if you forgot its name. The first hit in each
      row is wrapped in a `<mark>`, the count is shown, and a cleared query
      restores the full list. The query persists while you switch roots.
- [x] **Git / GitHub status.** A per-root summary strip (branch or detached
      HEAD, ahead/behind the upstream, staged/unstaged/untracked/conflicted
      counts, the last commit, open-on-GitHub) plus a per-script modified badge,
      from `internal/git`'s already-tested plumbing. Helpers that would
      otherwise flash a console window on Windows go through `proc.NoWindow`.
- [x] **Run locally.** A run bar in the script view's header (plain run with
      optional extra arguments, an Admin checkbox, Stop) and a terminal pane
      pinned below the form while a run is open. Output arrives as Wails events,
      the panel shows the exact argv, and the process working directory is the
      script's own directory so relative paths and dot-sourced files resolve.
      Built on the existing `internal/pty` package. One run at a time.
- [x] **Right-click context menu.** Run, Run (Administrator), Star, Copy path
      and Open file location from a menu on a script row.
- [x] **Run as administrator.** Elevation wherever it can stay in the app, then
      a fallback: Windows sudo's inline mode keeps the elevated run attached to
      the terminal; when that is off, the script starts elevated in its own
      window under a UAC prompt, which the panel reports via `RunView.Blind`
      (no streaming, no stop, exit code still arrives). The run bar's Admin
      checkbox is auto-checked and locked for `#Requires -RunAsAdministrator`,
      and the context menu offers a one-click elevated run. Unix has only
      `sudo` and no detached fallback, so a missing `sudo` refuses the run.
      Decisions live in `internal/elevate`, one file per platform.
- [x] **Input to a running script.** The run panel's input box sends one line to
      the child (`pty.LineEnding()`), answering prompts like a sudo password.
      It is kept alongside free-text arguments until the parameter form exists.
- [x] **Run panel terminal pane.** The right pane splits horizontally while a
      run is open: the script form above, the terminal below. The terminal is
      committed to a third of the pane's height rather than collapsing to its
      content, stays until its close button (which stops an unfinished run
      first), keeps streaming output scrolled to the bottom, and offers
      Stop / Rerun / Copy command.
- [x] **Open file location.** `internal/proc.Reveal` opens a script's folder in
      the platform's file manager with the file selected (`explorer /select`,
      `open -R`, `xdg-open`), threaded through `Service.RevealFile`.
- [x] **Copy to clipboard.** `internal/proc.CopyText` writes text per platform
      (Windows clipboard API, `pbcopy`, `wl-copy`/`xclip`/`xsel`); Copy path in
      the context menu and Copy command in the run panel use it.
- [x] **Git status refresh.** A ↻ button on the git strip re-reads the root, so
      the summary does not stall after edits.
- [x] **Elevated run safety fix.** The terminal library rewrites a bare
      `argv[0]` in the caller's slice; `pty.Start` now clones the argument
      vector so the command reported to the panel stays truthful.
- [x] **Working directory fix.** The scanner stores a script's directory
      relative to the root; `commandFor` now resolves it to an absolute path
      before starting the run, so nested scripts no longer fail with
      "The Directory name is invalid".
- [x] **Clean-checkout embed target.** A retained placeholder under
      `frontend/dist` lets Go vet, tests, and lint compile before the frontend
      build populates the directory. It must survive being deleted: the params
      work that introduced the placeholder removed it again, which is why CI
      broke a second time, and `make clean` wipes the whole directory.
- [x] **Parameter harvest encoding fix.** A character the console codepage
      cannot represent arrives as a raw byte rather than JSON text — U+2192 (the
      arrow in "A1B2C3 → C3B2A1") became 0x1A on some PowerShell builds, and
      that one control byte failed the entire batch of scripts. `harvest.ps1`
      now pins both console streams to UTF-8 without a BOM, and the decoder
      strips control bytes that valid JSON never contains.
- [x] **Console-window flicker fix.** Wails builds a GUI-subsystem binary, so on
      Windows a helper that is a console-subsystem child (git, `pwsh`) and is
      started without `CREATE_NO_WINDOW` is given a brand-new console mapped to
      the desktop — a terminal window flashing once per helper while a directory
      is read. `proc.NoWindow` sets the flag and is a no-op elsewhere.
- [x] **Windows backend portability.** Five distinct root causes behind a
      failing Windows test run: `docs.Discover` checked every README spelling
      separately, so a case-insensitive filesystem reported one file under three
      paths; `pty.Shell` built `powershell.exe -Command -`, which Windows
      PowerShell ignores under a pty and then exits; `workspace.Store.Save`
      renamed over a destination another handle had open; a test stamped
      `XDG_CONFIG_HOME`, which Windows ignores for `%AppData%`; and a test
      assumed mode 000 means unreadable, which holds only where access is
      enforced by mode rather than by ACL.
- [x] **Reading stage events.** A read reports each of its three slow steps to
      the window as it happens: reading the directory, harvesting parameters, and
      querying git. The harvest names the language it is about to read and how
      many scripts that is, which is where the time actually goes, and each event
      carries the root it belongs to so a stage for a folder you have left is
      dropped rather than shown against the wrong one. Harvesting also ran the
      extracted-toolchain memo through an unguarded map, so two overlapping reads
      — switching folders while a read is still going — could crash the process;
      it is now behind the same lock as the rest of the reader.
- [x] **Git status refresh on window focus.** The strip and the edited badges
      now re-read themselves when the window comes back to the foreground, so a
      commit made in a terminal is reflected without pressing ↻. A focus
      re-reads the repository and nothing else — a commit changes the strip and
      the badges but not a script's metadata — and it is throttled so a window
      that regains focus repeatedly does not run git each time.
- [x] **Makefile tool resolution.** `make lint` and `make build` looked for
      `wails` and `golangci-lint` on PATH only, so they failed on the machines
      that had just run `make tools`, because `go install` writes to GOBIN. The
      Makefile already computed GOBIN_DIR to explain why and then never used it.
- [x] **Open with... / Editor preferences.** Right-click a script → "Open ▸" to
      open in a preferred editor (Neovim, VS Code, Notepad++, PowerShell ISE,
      system default). Neovim launches in a new terminal window (Windows Terminal,
      pwsh, cmd; gnome-terminal, konsole, etc. on Linux; Terminal/iTerm on macOS).
      Preferences persist per extension: workspace override → global config →
      Neovim if on PATH → OS default. A button in the run bar opens with the
      preferred editor in one click. Global config stored as TOML for future GUI
      settings window. Context menu supports both hover and click for submenus.
- [x] **Editor detection build tags.** `internal/editor` picked its platform with
      a `switch runtime.GOOS` in the shared file while naming all three platform
      routines. Go constrains a file by its filename, so `_windows.go` is excluded
      off Windows, and the compiler resolves every branch of a runtime switch even
      though only one runs: Linux and macOS failed with `undefined:
      DetectWindows`. It built on Windows, where the file is present. Each
      platform file now carries an explicit `//go:build` constraint and defines
      the dispatch itself, and the two builders only Windows uses moved into the
      Windows file because a shared home left them unused elsewhere. The package
      also gained its first tests, which reach the platform routine through three
      build-tagged files rather than a runtime switch, since a single test doing
      that would not build on Linux either.
- [x] **Git status path base for subdirectory roots.** `TrackedFiles` and
      `Porcelain` disagreed about what a path was relative to: `git status
      --porcelain` always reports from the top of the repository and has no flag
      to change that, while `git ls-files` reports from the directory it runs in.
      For a root inside a repository the two never lined up, so the edited badge
      missed on load and a refresh cleared it, treating an absent entry as clean.
      The status keys are rebased onto the scanned directory with `git rev-parse
      --show-prefix`, and paths above the root are dropped.
- [x] **Four backend defects, and a test that was passing for the wrong reason.**
      A workspace file that could not be read was silently discarded, so a
      corrupt or too-new `workspaces.json` looked exactly like a first run: an
      empty window with no reason to think the saved roots still existed. A
      report saying a language's reader could not run was cached against the
      script's size and timestamp like any other, so installing the toolchain
      afterwards changed nothing and the script showed no parameters until it
      was edited. The run panel's exit reason was read outside the runner's lock.
      And the test meant to cover the first of these wrote its corrupt fixture as
      `workspace.json` while the store reads `workspaces.json`, so the garbage was
      never loaded and the assertions passed against an empty config directory.
      All four are fixed; the fixture uses the store's own file name and asserts
      a warning was produced, and a failed read is now marked `ReadFailed` and
      kept out of the cache.
- [x] **Three races that let the window show the wrong thing.** Clicking quickly
      through roots could paint one folder's script list while the sidebar showed
      another, because a root read cannot be cancelled and a slow one can land
      last. The same applied to explaining a script, so one script's
      documentation could appear under another's name. Re-reading matched on the
      relative path alone, so a re-read in one root replaced the same-named script
      in another — and two folders holding a `deploy.ps1` is ordinary, not an
      accident. Reads and explanations are now sequenced and a stale answer is
      dropped; a re-read matches on the root too.
- [x] **A fast script could leave the run panel stuck on "running" forever.** A
      run's output and exit are pushed by the backend, which starts pumping before
      it answers the call returning the id, so for a script that prints something
      and exits quickly both arrived before the window had an id to match them
      against and were dropped by the guard against other runs' events. The panel
      then claimed to be running a finished script, with a disabled Run button.
      Events arriving while the start call is in flight are buffered and replayed.
- [x] **A frontend test suite, and three operating systems in CI.** There was no
      frontend test runner at all, so the window was covered only by building it
      and looking at it. Vitest with jsdom now runs it, replacing `./api` and the
      generated Wails runtime. macOS is in both CI matrices, because the tree has
      platform files for it that no other runner compiled. A new Frontend job runs
      the type-check and the tests directly rather than relying on them being
      incidental to the Wails build.

## Backlog

- [ ] **Cancelling a run can take up to five seconds on Windows.** `crosspty`'s
      Close first closes the input pipe to give the child a chance to exit on
      its own, and waits out its kill delay before killing the job object.
      Those defaults are five and ten seconds, and `internal/pty` never sets
      them, so pressing Stop can leave the run panel waiting on
      `api.stopScript` for several seconds. Bounded and not a correctness
      problem, and the delay is what lets a script clean up after itself, so
      shortening it is a judgement call rather than a bug fix. It needs a
      `CloseConfig` passthrough on `internal/pty.Config` to be tunable at all.
- [ ] **Form-driven arguments.** Build the command line from the harvested
      parameter form instead of free-text extra arguments. Needs an argv builder
      in `internal/params` plus a fill-in form in the run panel. Once it ships,
      the free-text argument box and the run input box are retired in its favour.
- [ ] **Multiple concurrent runs.** Today the runner allows one active run.
- [ ] **Complex filter system.** A composable filter bar above the script list
      that combines free-text search with facet chips (language, has-params,
      modified, favorited, path prefix). Each facet adds a token; tokens can be
      removed individually or cleared all at once. The favorites filter added
      recently is the first facet; the rest generalize the pattern.
- [ ] **Remote execution.** See the design sketch below.

## Remote execution design (not built)

The local runner is deliberately shaped so it can be generalized to a remote
target later.

- **Transports.** SSH is the pragmatic Windows-to-Windows default (OpenSSH ships
  with Windows, key auth, run via `pwsh -File` on the far side). WinRM
  `Invoke-Command -FilePath` is the alternative where it is already configured;
  a workgroup HTTP listener needs TrustedHosts and credential handling.
- **Modules and local path references.** A script's dot-sourced files, `#Requires`
  modules and relative paths only resolve if they exist in the same layout on the
  target. So a remote run stages the dependency closure (already computed by
  `internal/deps`) into a temp directory, copies it to the target (`scp`, or
  `Copy-Item -ToSession` for WinRM), and runs with the process working directory
  set to the staging directory — the same convention the local runner uses
  (cwd = the script's directory).
- **Interactive sessions** (`Enter-PSSession`, ssh under the pseudo-terminal) are
  a later phase on top of the same `internal/pty` substrate.