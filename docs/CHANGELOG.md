# Changelog

All notable changes to Script Center are recorded here. Entries are grouped by
kind — Added, then Fixed — and listed in the order they were made, under
`[Unreleased]` until a version ships, then folded into a dated release section.
This file and `docs/TODO.md` are kept in sync: shipped work is added here and
checked off or retired in the TODO at the end of each session.

## [Unreleased]

### Added

- **The git strip follows you back to the window.** Bringing the window to the
  front re-reads the repository state and the edited badges, so a commit made in
  a terminal is reflected without pressing ↻. A focus re-reads the repository
  and nothing else, because a commit changes the strip and the badges but not a
  script's metadata, and it is throttled so a window that regains focus
  repeatedly — a dialog opening, another application clicked through — does not
  run git each time.
- **Reads say what they are waiting on.** Opening a folder reports each of its
  three slow steps as it happens instead of showing one "Reading…" label:
  reading the directory, harvesting parameters, and querying git. The harvest
  names the language it is about to read and how many scripts that is, with its
  position in the read, because the harvest is one toolchain invocation per
  language and is where the time goes. Every event carries the root it belongs
  to, so a stage arriving for a folder you have already left is dropped instead
  of being shown against the wrong one.
- **ReadMe relabel and document tabs.** The script view's documentation section
  is retitled "ReadMe" — not every script has a dedicated readme, so the tab
  now names what it is.
- **Search.** A box above the script list filters the selected root's loaded
  scripts as you type, matching case-insensitively against the fields the list
  shows plus the synopsis and the parameter names, so a script matches on a word
  you remember even if you forgot its name. The first hit in each row is
  highlighted, the number of matches is shown, and a cleared query restores the
  full list.
- **Git and GitHub status.** A strip under the root list summarises the
  repository: branch or detached HEAD, ahead/behind the upstream, staged,
  unstaged, untracked and conflicted counts, the last commit, and a link to open
  the repository on GitHub. A script you have edited since the last commit is
  badged in the list.
- **Run locally.** A run bar in the script view's header starts a script with
  any extra arguments you type and opens a terminal pane that streams its
  output. The panel shows the exact argv, offers Stop, and runs the script with
  its own directory as the process working directory so relative paths and
  dot-sourced files resolve. One run at a time.
- **Run as administrator.** A run can be elevated wherever the app can arrange
  it: Windows sudo's inline mode keeps the elevated run attached to the
  terminal, and when that is off the script starts elevated in its own window
  under a UAC prompt, which the panel reports via `RunView.Blind` (no
  streaming, no stop, but the exit code still arrives). The run bar's Admin
  checkbox is auto-checked and locked for `#Requires -RunAsAdministrator`, and
  the context menu offers a one-click elevated run.
- **Input to a running script.** The run panel's input box sends one line to
  the child (with the platform line ending), answering prompts like a sudo
  password. It lives alongside free-text arguments until the parameter form
  exists.
- **Persistent run terminal pane.** The right pane splits horizontally while a
  run is open: the script form above, the terminal below. The terminal stays
  open until its close button (which stops an unfinished run first), keeps
  streaming output scrolled to the bottom, and offers Stop / Rerun / Copy
  command.
- **Right-click context menu.** Run, Run (Administrator), Star, Copy path and
  Open file location from a menu on a script row.
- **Open file location.** `internal/proc.Reveal` opens a script's folder in the
  platform's file manager with the file selected (`explorer /select`,
  `open -R`, `xdg-open`), threaded through `Service.RevealFile`.
- **Copy to clipboard.** `internal/proc.CopyText` writes text per platform
  (Windows clipboard API, `pbcopy`, `wl-copy`/`xclip`/`xsel`); Copy path in the
  context menu and Copy command in the run panel use it.
- **Git status manual refresh.** A ↻ button on the git strip re-reads the root,
  so the summary does not stall after edits.
- **Star/favorite indicators.** The script list now shows a star column (★/☆)
  and highlights favorited rows with a subtle accent background. The detail
  panel's star button toggles between "☆ Star" and "★ Unstar". The right-click
  context menu dynamically shows "Star" or "Unstar" based on the current state.
  A "★ Favorites" filter button in the search row shows only starred scripts,
  laying groundwork for a future composable filter system.
- **Open with... / Editor preferences.** Right-click a script → "Open ▸" opens a
  submenu with "Open (preferred editor)", "Open with..." (lists detected editors:
  Neovim, Visual Studio Code, Notepad++, PowerShell ISE, system default), and
  "Set default for .ext" (workspace or global). Neovim launches in a new terminal
  window (Windows: `wt` → `pwsh` → `powershell` → `cmd`; macOS: Terminal/iTerm;
  Linux: gnome-terminal/konsole/xterm/alacritty/kitty). Preferences persist per
  extension: workspace override → global TOML config (`~/.config/script-center/editors.toml`)
  → Neovim if on PATH → OS default. An "Open" button in the run bar opens with
  the preferred editor in one click. Context menu submenus support both hover and
  click. Global config stored as TOML for future GUI settings window.
- **The window has a test suite.** There was no frontend test runner at all, so
  every behaviour here was covered by building the app and looking at it. The
  components reach the backend through one module and the window through the
  generated Wails runtime, and both are now replaceable under jsdom, which is
  what makes a window test possible at all. The runtime mock is a small
  push-capable event bus rather than a pair of no-op functions, so a test can
  deliver an event at the moment it would really arrive. `npm test` in
  `frontend` runs them.

### Fixed

- **A fast script could leave the run panel stuck on "running" forever.** A run's
  output and its exit are pushed by the backend, which starts reading the moment
  it has registered the run and only then answers the call that returns the id.
  For a script that prints something and exits quickly, both events arrive before
  the window has an id to match them against, and the guard that ignores events
  from other runs dropped them. The panel then said the script was running with
  an empty output block and a disabled Run button, for a script that had already
  finished. Events arriving while the start call is still in flight are now kept
  and replayed once the id arrives. The distinction being drawn is "not known
  yet" against "belongs to another run": the buffer is opened for the duration of
  one start call, so a genuinely late event from a finished run is still
  ignored.
- **Clicking quickly through roots could paint the wrong folder's scripts.** A
  root read cannot be cancelled from the frontend, so selecting a second root
  leaves the first in flight, and a folder on a slow disk or a network share can
  take seconds while a local one takes milliseconds. The older answer then lands
  last and paints its script list while the sidebar shows the newer root as
  selected. Reads now carry a sequence number and a stale answer is dropped
  rather than applied.
- **The same race could show one script's documentation under another's name.**
  Explaining is a second read of the same file and fires once per click, so two
  are in flight whenever the user moves faster than the backend answers, which
  for a large script or one needing a toolchain that has to start is routine.
  The older answer arriving last put the wrong script's documentation in the
  panel. Explanation answers are now sequenced the same way.
- **A re-read could put a script into a different root that shares its name.**
  The answer identifies the script it re-read, and the panel applied it to
  whichever root the view held at the time, matching on the relative path alone.
  Two folders holding a `deploy.ps1` is ordinary rather than an accident, so the
  wrong root's script was replaced with one from elsewhere and the list stopped
  matching the folder on disk. The root is now part of the match, as it already
  was for deciding whether the detail panel should follow.
- **Losing every saved root was silent.** `Start` reported a parameter cache it
  could not read, but discarded the error from the workspace file without a
  word. A `workspaces.json` that was corrupt, unreadable or written by a newer
  build therefore looked exactly like a first run: the window opened empty and
  the user was given no reason to think their roots and stars still existed. It
  is now reported through the same startup-warning path as the cache. A missing
  file still says nothing, because a first run is not a problem.
- **The test that covered the above was not covering it.** It wrote its corrupt
  fixture as `workspace.json`, while the store reads `workspaces.json`, so the
  garbage was never loaded and the assertions passed against an empty config
  directory without ever reaching the corrupt path they describe. The fixture
  now uses the store's own file name, and it asserts that a warning was
  produced, which is the only thing a genuinely unreadable file can do. A
  companion test checks that an absent file produces no warning, so the pair
  cannot drift back.
- **A missing toolchain could become a permanent answer.** When a language's
  reader could not run, the report saying so was cached against the script's
  size and mtime like any other. Installing the toolchain afterwards then
  changed nothing, because nothing invalidated the entry and the file itself had
  not changed: the script showed no parameters until it was edited. Reports now
  carry a `ReadFailed` marker that keeps them out of the cache, and a script
  that was really read and genuinely has no parameters is still cached, which is
  the case caching exists for.
- **The run panel's exit reason was read outside the lock.** `Stop` records that
  the user asked for a run to end under the runner's mutex, but the exit path
  read that flag after releasing it, leaving the two accesses unordered. It is
  now read inside the critical section. Worth being precise about: no test can
  force the interleaving, because `Stop` finds its entry by looking it up in the
  map and the exit path deletes that entry under the same lock immediately
  before reading it, so the reachable orderings come out ordered in practice.
  The change removes a violation of the memory model rather than fixing an
  observed wrong value.
- **A test covering blind runs was failing about one run in many, for the wrong
  reason.** `TestRunnerBlindNotStoppable` starts a fake blind run whose `Wait`
  returns immediately, so the exit path could delete the run before `Stop` and
  `SendInput` were called, and both then reported "not active" instead of the
  refusal the test is about. It passed nearly every time and failed now and then,
  which made it look like whatever else had just changed was at fault. The run
  is now held open for the duration of the test.
- **The Windows build was red about half the time, for no reason.** The test
  that covers canceling a run asserted that the child's *exit code* was
  non-zero after the session was closed, and on Windows that code is not a
  reliable proxy for whether the process was stopped. `crosspty` first closes
  the input pipe to ask the child to exit on its own, and only kills the job
  object if the child has not gone within its kill delay; the graceful path
  reports the control-c exit code and the forced path reports `KillExitCode`,
  which defaults to `0`. So the same child that was in fact terminated cleanly
  failed the test whenever it took the forced path, which is what happened on
  roughly half of all Windows CI runs and on none of the Unix ones. There was
  never a leaked process. The test now asks the question it was written to ask,
  which is whether the process is still running, using `kill(pid, 0)` on Unix
  and `OpenProcess` plus the exit code on Windows, where a handle to a
  terminated-but-unreaped process still opens. A companion test checks the
  helper against both a running and a finished child, because the first test
  only ever asks about a dead one and would otherwise pass forever against a
  helper that always answered "gone".
- **Edited badges were missing, and then cleared, for a folder inside a
  repository.** `internal/git` has two functions that are meant to agree on a
  coordinate system: `TrackedFiles` enumerates the scripts under a root, and
  `Porcelain` reports their working-tree status. They did not. `git status
  --porcelain` always writes paths relative to the *repository root* and offers
  no flag to change that, while `git ls-files` writes them relative to the
  directory it is run in. A root such as `<repo>/tools/scripts` therefore got
  statuses keyed `tools/scripts/deploy.ps1` against scan entries keyed
  `deploy.ps1`, and the lookup behind the badge never matched. It looked like a
  badge that appeared and then disappeared, because the refresh treats an absent
  entry as clean and a path outside the root was reported as though it were
  inside it. The status keys are now rebased onto the scanned directory using
  `git rev-parse --show-prefix`, which is the one piece of information git will
  volunteer here, and paths above the root are dropped. Tests drive a real
  repository at both the git and application level, and both fail without the
  change.
- **The application would not build on Linux or macOS.** The editor detection
  added for "Open with…" chose its platform with a `switch runtime.GOOS` in the
  shared file, but that file also named all three platform routines. Go derives a
  build constraint from a filename, so `_windows.go` is excluded off Windows —
  and the compiler still has to resolve every branch of the switch even though it
  only runs one, so the Linux build stopped at `undefined: DetectWindows`. It
  compiled and ran on Windows, which is where the feature was written and tested.
  The three platform files now carry explicit `//go:build` constraints and each
  defines the dispatch itself, so exactly one is compiled per platform and the
  shared file names none of them. The two builders only Windows uses moved into
  the Windows file with it, since a shared home left them unused everywhere else,
  and a first test package covers the package that had none. The constraint that
  a platform file must not be referenced from the shared one is why the test
  expects its routine through three build-tagged files as well: a single test
  that switched on `runtime.GOOS` would not have built on Linux either.
- **`make lint` and `make build` could not find the tools they had installed.**
  The Makefile computed `GOBIN_DIR` and then never referenced it, so it looked
  for `wails` and `golangci-lint` on PATH alone. `go install` writes to GOBIN,
  which points into the mise-managed Go tree here, so both commands failed with
  "No such file or directory" on exactly the machines that had run `make tools`.
  They now prefer a tool on PATH and fall back to GOBIN, or to `GOPATH/bin` when
  GOBIN is unset. This also means lint can be run without exporting anything,
  which is how the two entries above were checked: they are real
  golangci-lint findings, not cosmetic ones.
- **Two overlapping directory reads could take the window down.** The parameter
  reader memoises the toolchain it finds for each language, and that memo is a
  map. A read re-run by the window and one started by its background refresh are
  independent requests that can overlap, and Go treats a concurrent map read and
  write as a fatal runtime error rather than a recoverable race, so a double
  click on ↻ could kill the application mid-read. The memo is now held under a
  lock; a test drives concurrent reads and fails without it.
- **Clean-checkout CI failed before tests ran.** The Go embed target had no
  tracked frontend files because the generated `frontend/dist` output is
  ignored. A retained placeholder now lets vet, tests, and lint compile from a
  fresh checkout, and the inline-elevation test uses a fake terminal session
  instead of launching external commands. The placeholder is a tracked file that
  nothing regenerates, so it is fragile, and it has now been deleted three times:
  the parameter-encoding work below removed it once, and the documentation
  rewrite removed it again because the deletion was sitting unstaged in the
  working tree. `make clean` deletes it with the rest of the directory too. It
  is restored, and `go vet` is verified against a checkout that contains
  nothing but the placeholder.
- **The backend failed its Windows test run for five unrelated reasons.**
  `docs.Discover` checked every README spelling separately, so a
  case-insensitive filesystem reported one file under three paths; `pty.Shell`
  built `powershell.exe -Command -`, which Windows PowerShell ignores under a
  pseudo-terminal and then exits; `workspace.Store.Save` renamed over a
  destination another handle had open, which Windows rejects; a test stamped
  `XDG_CONFIG_HOME`, which Windows ignores in favor of `%AppData%`; and a test
  assumed mode 000 means unreadable, which holds only where access is enforced
  by mode rather than by ACL. `.gitattributes` also pins every checkout to LF,
  which gofmt and golangci-lint require to stay green.
- **The Windows CI jobs had no C compiler.** The race detector links through
  cgo, so `go test -race` could not build on windows-latest at all. A WinLibs
  POSIX/UCRT toolchain is installed first — the POSIX thread model matters,
  because the win32-thread MinGW builds cannot link the race runtime — and the
  build job installs it too, so a dependency that gains a cgo component cannot
  break the Windows build in CI while passing locally.
- **The CI build failed for want of the frontend toolchain.** `wails build` runs
  `npm install`, and with `NODE_ENV=production` set npm omits devDependencies,
  leaving `tsc` and `vite` missing. The variable is not set.
- **A console window flashed for every helper while reading a directory.** Wails
  builds a GUI-subsystem binary, so a console-subsystem child such as git or
  `pwsh`, started without `CREATE_NO_WINDOW`, is given a brand-new console
  mapped to the desktop. The helpers the backend launches now go through
  `proc.NoWindow`, which sets the flag on Windows and is a no-op where it does
  not exist.
- **One unprintable character failed a whole batch of parameter harvests.** The
  console host encodes stdout through the session codepage, so a character it
  cannot represent arrives as a raw byte instead of JSON text: the arrow in
  "A1B2C3 → C3B2A1" became 0x1A on some PowerShell builds, and that single
  control byte failed every script in the batch. `harvest.ps1` now pins both
  console streams to UTF-8 without a BOM, and the decoder strips control bytes
  that valid JSON never contains.
- **Elevated runs reported a rewritten command.** The terminal library
  normalizes a bare `argv[0]`, mutating the caller's slice in place, so the
  panel showed a resolved `sudo.exe` instead of the script. `pty.Start` now
  clones the argument vector before handing it over.
- **Nested scripts failed to start with "The Directory name is invalid".** The
  scanner records a script's directory relative to the root, but the run
  command passed it straight to the operating system as the process working
  directory, which the OS resolved against the app's own directory. `commandFor`
  now resolves a relative directory against the root before starting the
  script.
- **The run terminal collapsed to its content.** It is now committed to a third
  of the right pane's height, which is enough to read a scrollback of output
  without squeezing the parameter form out of view.