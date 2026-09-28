package app

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/lspoor/script-center/internal/detect"
	"github.com/lspoor/script-center/internal/docs"
	"github.com/lspoor/script-center/internal/params"
	"github.com/lspoor/script-center/internal/scan"
)

// The types in this file are what the frontend receives. They exist as separate
// shapes from the ones the scanner, reader and docs packages use because those
// describe the work and these describe the screen: a scan entry is a fact about
// a file, a script view is a row in a list.

// Status reports what the app managed to load, so the UI can say so rather than
// leaving the user wondering why their roots are missing.
type Status struct {
	// ConfigDir is where state is kept, shown in Settings.
	ConfigDir string `json:"configDir"`
	// Roots is how many roots the workspace has.
	Roots int `json:"roots"`
	// CachedReports is how many harvested reports were available without
	// re-reading anything.
	CachedReports int `json:"cachedReports"`
	// Warnings holds startup problems that were stepped over.
	Warnings []string `json:"warnings,omitempty"`
}

// Toolchain describes whether each language's reader is available, so the UI can
// say "install PowerShell to get parameter forms" instead of showing a script
// with no form and no explanation.
type Toolchain struct {
	// Language is the language name.
	Language string `json:"language"`
	// Available reports whether an extractor exists for it at all.
	Available bool `json:"available"`
	// Reason explains why there is no form, when there is none.
	Reason string `json:"reason,omitempty"`
}

// Meta is what the UI shows about the last read of a root.
type Meta struct {
	// Duration is how long the read took, which the window shows so that a slow
	// toolchain does not look like a hang.
	DurationMS int64 `json:"durationMs"`
	// FromCache is how many scripts were answered without starting a toolchain.
	FromCache int `json:"fromCache"`
	// Read is how many scripts were actually read.
	Read int `json:"read"`
	// Total is how many scripts the root has.
	Total int `json:"total"`
	// CacheHits and CacheMisses are the cumulative counts, so a cache that never
	// hits is visible rather than silently useless.
	CacheHits   int `json:"cacheHits"`
	CacheMisses int `json:"cacheMisses"`
}

// ScriptView is one script with its metadata, ready for the frontend.
type ScriptView struct {
	// Path is absolute; Root and Rel identify the script within the workspace.
	Path string `json:"path"`
	Root string `json:"root"`
	Rel  string `json:"rel"`
	Name string `json:"name"`
	Dir  string `json:"dir"`
	// Kind is "script" or "project". A project has no parameter form of its own.
	Kind string `json:"kind"`
	// Language is the language's own name, such as "powershell". It is sent
	// because a later request has to name the same language again to re-read the
	// script, and a view cannot be asked to recover it from a display name.
	Language string `json:"language"`
	// Lang is the language name for display, such as "PowerShell".
	Lang string `json:"lang"`
	// Size and ModTime let the UI show a script's age and notice a change made
	// outside the app.
	//
	// ModTime is a formatted string rather than a time.Time because that is what
	// actually crosses to the frontend, where a typed value would arrive as an
	// untyped blob. The format is RFC 3339 in UTC, or empty when the file could
	// not be stat'd.
	Size    int64  `json:"size"`
	ModTime string `json:"modTime"`
	// Metadata is the harvested report, which is present for every script, with
	// Warnings carrying anything that went wrong.
	Metadata params.Report `json:"metadata"`
	// Cached reports that this view was served from the cache rather than read.
	Cached bool `json:"cached"`
	// Inferred reports that no harvester produced a form, whether because the
	// language has no reader or because the file is a project.
	Inferred bool `json:"inferred"`
	// Modified reports that git sees a change to this script, which the list
	// shows as a badge on the row. It is only ever true for a script in a
	// repository; everywhere else it stays false.
	Modified bool `json:"modified,omitempty"`
}

// NeedsForm reports whether this script can have a parameter form, which decides
// whether the UI offers a form at all.
func (v ScriptView) NeedsForm() bool {
	return v.Kind == string(scan.KindScript) && !v.Inferred
}

// detectedLanguage recovers the language a view was built from.
//
// detect.Language is a string type, so this is a conversion rather than a lookup.
// It used to be recovered by matching the display name against a list of known
// languages, which quietly turned any language missing from that list into
// Unknown, and a re-read of such a script would then produce no form at all.
func (v ScriptView) detectedLanguage() detect.Language {
	if v.Language == "" {
		return detect.Unknown
	}
	return detect.Language(v.Language)
}

// RootView is one scanned root with metadata for every script in it.
type RootView struct {
	Root    string       `json:"root"`
	Name    string       `json:"name"`
	Source  string       `json:"source"`
	IsRepo  bool         `json:"isRepo"`
	Scripts []ScriptView `json:"scripts"`
	// Warnings holds recoverable problems, such as a directory that could not
	// be read or a language with no reader installed.
	Warnings []string `json:"warnings,omitempty"`
	// Git is the repository state of the root, when the root is a repository.
	// It is absent otherwise, so the frontend can treat a non-repository as
	// having no git to show at all.
	Git *GitStatus `json:"git,omitempty"`
	// Meta describes the read that produced this view.
	Meta Meta `json:"meta"`
}

// GitStatus is the repository state of a root, as the frontend shows it.
//
// It is a separate shape from internal/git's State because that type reports
// what git said while this one describes what the screen shows.
type GitStatus struct {
	// Branch is the checked-out branch, or the short commit hash or "(detached)"
	// when HEAD is not on a branch.
	Branch   string `json:"branch"`
	Detached bool   `json:"detached"`
	// OID is the commit HEAD resolves to.
	OID string `json:"oid"`
	// Upstream is the tracking branch, empty when there is none.
	Upstream string `json:"upstream,omitempty"`
	// Ahead and Behind are commits relative to Upstream.
	Ahead  int `json:"ahead"`
	Behind int `json:"behind"`
	// Staged, Unstaged, Untracked and Conflicted are working-tree counters.
	Staged     int `json:"staged"`
	Unstaged   int `json:"unstaged"`
	Untracked  int `json:"untracked"`
	Conflicted int `json:"conflicted"`
	// Stashed is the number of stashed entries.
	Stashed int `json:"stashed"`
	// Commit is the branch's most recent commit, absent when it has none.
	Commit *GitCommit `json:"commit,omitempty"`
	// Remote is the origin URL, empty when the repository has no origin.
	Remote string `json:"remote,omitempty"`
}

// GitCommit is one commit, as the frontend shows it.
type GitCommit struct {
	OID      string    `json:"oid"`
	ShortOID string    `json:"shortOid"`
	Author   string    `json:"author"`
	Date     time.Time `json:"date"`
	Subject  string    `json:"subject"`
}

// ExplainView is everything known about one script's documentation.
type ExplainView struct {
	Script ScriptView `json:"script"`
	// Docs is the merged documentation: the script's own help plus the nearest
	// README and sidecars.
	Docs docs.Explanation `json:"docs"`
	// Related are the other scripts that mention or resemble this one.
	Related []Suggestion `json:"related,omitempty"`
	// Dependencies are the files this script dot-sources, itself excluded,
	// relative to the root so they read as paths within the project.
	Dependencies []string `json:"dependencies,omitempty"`
	// Unresolved are dot-source expressions that could not be resolved to a
	// file. They are reported because a script that dot-sources something unknown
	// is a script that may not run wherever it is copied to.
	Unresolved []string `json:"unresolved,omitempty"`
}

// Suggestion is a related script, as the frontend sees it.
type Suggestion struct {
	Path string `json:"path"`
	Root string `json:"root"`
	Rel  string `json:"rel"`
	Name string `json:"name"`
	// Reason explains the match, so a suggestion is not a mystery.
	Reason string `json:"reason"`
}

// DocumentView is one documentation file with its content ready to show.
//
// The content is rendered here rather than sent as raw bytes because the
// frontend displays it as HTML. Rendering in the backend is also what keeps the
// sanitizer in one place: a readme is untrusted input, and only the backend
// decides what markup survives.
type DocumentView struct {
	Path  string `json:"path"`
	Kind  string `json:"kind"`
	Title string `json:"title"`
	// HTML is the sanitized, rendered document. It is empty when Error is set.
	HTML string `json:"html"`
	// Error says why a document could not be shown, so the sidebar can list it
	// and the reading pane can say why it is empty instead of rendering nothing.
	Error string `json:"error,omitempty"`
}

// RunView is the start of a script run, returned to the frontend so the panel
// can attach the streaming events to it by id.
type RunView struct {
	// ID is the session identifier, echoed in every output and exit event.
	ID string `json:"id"`
	// Command is the argv under which the script is running, shown so a user can
	// see exactly how their script was invoked.
	Command []string `json:"command"`
	// Dir is the working directory the script runs in.
	Dir string `json:"dir"`
	// Warning explains something the user should know about the run, such as a
	// script that asks to run as administrator but will run with the app's own
	// privileges.
	Warning string `json:"warning,omitempty"`
}

// RunOutput is one chunk of a running script's output, delivered as an event.
type RunOutput struct {
	ID    string `json:"id"`
	Chunk string `json:"chunk"`
}

// RunExit reports how a run ended.
type RunExit struct {
	ID string `json:"id"`
	// Code is the exit status, or the best the platform can report when the run
	// was stopped rather than ended (see pty.Session.Wait).
	Code int `json:"code"`
	// Stopped is true when the user asked for the run to end, rather than the
	// script ending on its own.
	Stopped bool `json:"stopped"`
}

// viewFor builds the view of one entry, filling in a report when the reader had
// nothing to say about it.
func (s *Service) viewFor(entry scan.Entry, report params.Report, cached bool) ScriptView {
	view := ScriptView{
		Path:     entry.Path,
		Root:     entry.Root,
		Rel:      entry.Rel,
		Name:     entry.Name,
		Dir:      entry.Dir,
		Kind:     string(entry.Kind),
		Language: string(entry.Lang),
		Lang:     entry.Lang.DisplayName(),
		Size:     entry.Size,
		ModTime:  formatTime(entry.ModTime),
		Metadata: report,
		Cached:   cached,
		Inferred: entry.Kind != scan.KindScript,
	}
	if strings.TrimSpace(view.Metadata.Path) == "" {
		view.Metadata = params.Report{Path: entry.Path, Help: params.Help{Source: params.HelpSourceNone}}
		if entry.Kind != scan.KindScript {
			view.Inferred = true
			view.Metadata.AddWarning("this is a project directory, so its tasks are enumerated " +
				"from its manifest rather than run as one script")
		}
	}
	// A report with no parameters and no help came from a language with no
	// reader, or a reader that found nothing. Either way there is no form, and
	// the UI should not offer one.
	view.Inferred = view.Inferred || !hasForm(view.Metadata)
	return view
}

// hasForm reports whether a report carries anything worth showing as a form.
//
// A report with no parameters and no help describes a script whose inputs could
// not be read, or a script that takes none. Either way there is no form to offer,
// and offering an empty one would be worse than saying so.
func hasForm(report params.Report) bool {
	if len(report.Params) > 0 {
		return true
	}
	help := report.Help
	return strings.TrimSpace(help.Synopsis) != "" ||
		strings.TrimSpace(help.Description) != "" ||
		len(help.Examples) > 0 ||
		len(help.Notes) > 0 ||
		len(help.Links) > 0
}

// rootLabel returns the name a root should carry, preferring the name the user
// gave it and falling back to the directory's own name.
func rootLabel(path, preferred string) string {
	if strings.TrimSpace(preferred) != "" {
		return preferred
	}
	return filepath.Base(path)
}

// formatTime renders a timestamp for the frontend.
//
// UTC with a numeric zone, so a timestamp shown next to one from another tool is
// comparable without the reader having to know the local zone.
func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}
