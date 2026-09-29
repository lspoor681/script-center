# Changelog

All notable changes to Script Center are recorded here. Entries are grouped by
kind — Added, then Fixed — and listed in the order they were made, under
`[Unreleased]` until a version ships, then folded into a dated release section.
This file and `docs/TODO.md` are kept in sync: shipped work is added here and
checked off or retired in the TODO at the end of each session.

## [Unreleased]

### Added

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

### Fixed

- **Clean-checkout CI failed before tests ran.** The Go embed target had no
  tracked frontend files because the generated `frontend/dist` output is
  ignored. A retained placeholder now lets vet, tests, and lint compile from a
  fresh checkout, and the inline-elevation test uses a fake terminal session
  instead of launching external commands. The placeholder is a tracked file that
  nothing regenerates, so it is fragile: the parameter-encoding work below
  removed it again and broke lint and tests a second time, and `make clean`
  deletes it with the rest of the directory.
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