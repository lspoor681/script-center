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
      locally on Windows. The race detector runs on Windows too, via a MinGW
      toolchain (`CGO_ENABLED=1`).
- [x] **CI green.** Lint, test (ubuntu + windows, `-race`) and build jobs all
      pass; `.gitattributes` pins checkouts to LF.

## In progress

- [x] **TODO tracker.** This file, linked from the README.
- [x] **ReadMe relabel.** Retitle the script view's "Documentation" section to
      "ReadMe" — not every script has a dedicated readme.
- [x] **Search.** Filter the currently selected root's scripts as you type
      (name, path, directory, language, synopsis, parameter names).
- [x] **Git / GitHub status.** A per-root summary strip (branch, ahead/behind,
      staged/unstaged/untracked counts, last commit, open-on-GitHub) plus a
      per-script modified badge, from `internal/git`'s already-tested plumbing.
- [x] **Run locally.** A live streaming run panel in the detail pane header:
      plain run with optional extra arguments, Stop button, output via Wails
      events, process working directory = the script's directory so relative
      paths and dot-sourced files resolve. Uses the existing `internal/pty`
      package.
- [x] **Right-click context menu.** Run, Run (Administrator), Star, Copy path
      and Open file location from a menu on a script row.
- [x] **Run as administrator.** Elevation wherever it can stay in the app, then
      a fallback: Windows sudo's inline mode keeps the elevated run attached to
      the terminal; when that is off, the script starts elevated in its own
      window under a UAC prompt, which the panel reports via `RunView.Blind`
      (no streaming, no stop, exit code still arrives). The run bar's Admin
      checkbox is auto-checked and locked for `#Requires -RunAsAdministrator`,
      and the context menu offers a one-click elevated run.
- [x] **Input to a running script.** The run panel's input box sends one line to
      the child (`pty.LineEnding()`), answering prompts like a sudo password.
      It is kept alongside free-text arguments until the parameter form exists.
- [x] **Run panel terminal pane.** The right pane splits horizontally while a
      run is open: the script form above, the terminal below. The terminal
      stays until its close button (which stops an unfinished run first), keeps
      streaming output scrolled to the bottom, and offers Stop / Rerun / Copy
      command.
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

## Backlog

- [ ] **Form-driven arguments.** Build the command line from the harvested
      parameter form instead of free-text extra arguments. Needs an argv builder
      in `internal/params` plus a fill-in form in the run panel. Once it ships,
      the free-text argument box and the run input box are retired in its favour.
- [ ] **Multiple concurrent runs.** Today the runner allows one active run.
- [ ] **Reading stage events.** Replace the single "busy" string with emitted
      stage events ("reading directory", "querying git", "harvesting parameters")
      so a slow open shows what is happening instead of one opaque label.
- [ ] **Git status refresh.** A trigger (window focus or a manual refresh) so the
      strip stays current after edits.
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