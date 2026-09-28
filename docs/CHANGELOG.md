# Changelog

All notable changes to Script Center are recorded here. Entries are grouped by
working session under `[Unreleased]` until a version ships, then folded into a
dated release section. This file and `docs/TODO.md` are kept in sync: shipped
work is added here and checked off or retired in the TODO at the end of each
session.

## [Unreleased]

### Added

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
  instead of launching external commands.
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