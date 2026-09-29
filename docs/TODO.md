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

## Backlog

- [ ] **Form-driven arguments.** Build the command line from the harvested
      parameter form instead of free-text extra arguments. Needs an argv builder
      in `internal/params` plus a fill-in form in the run panel. Once it ships,
      the free-text argument box and the run input box are retired in its favour.
- [ ] **Multiple concurrent runs.** Today the runner allows one active run.
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