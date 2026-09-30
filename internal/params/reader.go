package params

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/lspoor/script-center/internal/detect"
)

// Extractor reads parameter and documentation metadata out of scripts of one
// language.
//
// The two implementations differ in kind. PowerShell and Python shell out to
// the real toolchain, because each has an API that can be asked what a script
// accepts. Bash is read in process, because a shell has no such API and asking
// one would mean running the script.
//
// An Extractor may be slow when a toolchain has to start, so implementations
// batch and callers should not assume a read is free.
type Extractor interface {
	// Extract returns one report per requested path, in the requested order.
	// A script that cannot be read still gets a report carrying the reason.
	Extract(ctx context.Context, paths []string) ([]Report, error)

	// Readability says whether this extractor can describe a script's arguments,
	// and why not when it cannot.
	//
	// This is a separate question from whether an Extractor exists, and the two
	// come apart. An UnsupportedExtractor exists for exactly those languages that
	// cannot be read, so a caller that treats "no error" as "readable" concludes
	// that Rust has a reader because Rust has a way of saying it does not.
	Readability() Readability
}

// Readability is what an extractor can tell the user about a language.
type Readability struct {
	// Readable reports whether arguments can be described.
	Readable bool
	// Reason explains an unreadable language in the user's terms, and is empty
	// when Readable is true.
	Reason string
}

// bashExtractorAdapter gives BashExtractor the context-based signature. Reading
// a file in process needs no cancellation, but matching the interface keeps the
// dispatcher free of type switches.
type bashExtractorAdapter struct{}

// Extract implements Extractor.
func (bashExtractorAdapter) Extract(_ context.Context, paths []string) ([]Report, error) {
	return BashExtractor{}.Extract(paths)
}

// Readability implements Extractor. A shell script is always read in process, so
// there is nothing to install and nothing to be missing.
func (bashExtractorAdapter) Readability() Readability { return Readability{Readable: true} }

// UnsupportedExtractor stands in for a language whose inputs cannot be read
// without building or running the user's program.
//
// It exists so that the rest of the application does not have to carry a
// special case for every language it cannot introspect. A report is still
// returned for each script, so the script is listed, is still runnable, and the
// user is told why there is no form rather than being left to wonder.
type UnsupportedExtractor struct {
	// Language is the language name shown to the user.
	Language string
	// Reason explains why there is no form.
	Reason string
}

// Readability implements Extractor.
func (u UnsupportedExtractor) Readability() Readability {
	return Readability{Readable: false, Reason: u.Reason}
}

// Extract implements Extractor.
func (u UnsupportedExtractor) Extract(_ context.Context, paths []string) ([]Report, error) {
	reports := make([]Report, 0, len(paths))
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		report := Report{Path: path, Help: Help{Source: HelpSourceNone}}
		report.AddWarning("this script's inputs cannot be read from a %s file, "+
			"so it will run without a form: %s", u.Language, u.Reason)
		reports = append(reports, report)
	}
	return reports, nil
}

// rustExtractor reports that a Rust program's inputs are not statically knowable.
func rustExtractor() Extractor {
	return UnsupportedExtractor{
		Language: "Rust",
		Reason: "its arguments are defined by a derive macro or read at run time, " +
			"which are only settled when the program is built",
	}
}

// batchExtractor reports that a batch file has no standard parameter system.
func batchExtractor() Extractor {
	return UnsupportedExtractor{
		Language: "batch",
		Reason: "batch has no way to declare options, so each script invents its own " +
			"argument handling",
	}
}

// toolchainFor locates the interpreter a language needs. An empty result means
// the language needs no interpreter.
func toolchainFor(language detect.Language) (string, error) {
	switch language {
	case detect.PowerShell:
		return FindPowerShell()
	case detect.Python:
		return FindPython()
	default:
		return "", nil
	}
}

// newExtractor builds the extractor for a language around a toolchain that has
// already been located. Passing an empty toolchain for a language that needs one
// is a programming error, so the harvester is left to report it.
func newExtractor(language detect.Language, exe string) Extractor {
	switch language {
	case detect.PowerShell:
		return &PowerShellHarvester{Exe: exe}
	case detect.Python:
		return &PythonHarvester{Exe: exe}
	case detect.Bash:
		return bashExtractorAdapter{}
	case detect.Rust:
		return rustExtractor()
	case detect.Batch:
		return batchExtractor()
	default:
		return UnsupportedExtractor{
			Language: language.DisplayName(),
			Reason:   "this language has no parameter reader yet",
		}
	}
}

// ForLanguage returns the extractor that reads the given language.
//
// The toolchain lookup happens here rather than being done by the caller, so
// that a missing interpreter is reported against one language instead of
// failing a whole scan. A language with no reader still gets one, because "there
// is no form" is a result the UI has to be able to show.
func ForLanguage(language detect.Language) (Extractor, error) {
	exe, err := toolchainFor(language)
	if err != nil {
		return nil, err
	}
	return newExtractor(language, exe), nil
}

// ReadabilityOf reports whether a language's arguments can be described, and why
// not when they cannot.
//
// It starts no toolchain, so it is cheap enough to call while drawing a window.
// A language that needs an interpreter is reported unreadable when that
// interpreter is missing, because to a user "no form" and "no PowerShell" are the
// same outcome and the second one is actionable.
func ReadabilityOf(language detect.Language) Readability {
	exe, err := toolchainFor(language)
	if err != nil {
		return Readability{Readable: false, Reason: err.Error()}
	}
	return newExtractor(language, exe).Readability()
}

// Options configures a set of reads.
type Options struct {
	// Timeout bounds one toolchain invocation.
	Timeout time.Duration
	// BatchSize is how many scripts one invocation reads.
	BatchSize int
}

// Reader reads metadata out of a mixed set of scripts.
//
// The language of each script decides how it is read, so a scan of a directory
// holding PowerShell, Python, and shell scripts issues one call per language
// rather than one per script. Scripts whose language has no working extractor
// still come back as reports carrying the reason.
type Reader struct {
	// Options configures the toolchain invocations.
	Options Options
	// mu guards interpreters. A Reader is shared by every read the application
	// makes, and Go treats a concurrent map read and write as a fatal runtime
	// error rather than a race the caller can recover from, so two reads
	// overlapping have to be serialized here rather than left to the caller.
	mu sync.Mutex
	// interpreters caches the toolchain lookup, because a scan of a hundred
	// scripts should not search the filesystem a hundred times.
	interpreters map[detect.Language]string
	// lookup finds a language's toolchain. It is a field so that a test can make
	// a lookup fail on a machine that happens to have every toolchain installed.
	lookup func(detect.Language) (string, error)
	// Cache, when set, lets an unchanged script be served without starting its
	// toolchain at all. It is optional so that a caller with nowhere to persist
	// results can simply leave it nil.
	Cache *Cache
	// OnGroup, when set, is called once per language group just before that
	// group is harvested. The harvest is the slow part of reading a directory
	// and it is one toolchain invocation per group, so a caller that shows
	// progress needs to know which group it is waiting on. Scripts answered
	// from the cache never enter a group, so they are not reported.
	//
	// The context is passed through so a caller can attribute the report to the
	// request that caused it; the reader itself has no idea which root it is
	// reading, only the paths it was handed.
	//
	// It is a plain callback rather than an event name so that this package
	// stays independent of any window: the caller decides what to do with it.
	OnGroup func(ctx context.Context, progress GroupProgress)
}

// GroupProgress describes one language group of a read, reported through
// Reader.OnGroup just before the group is harvested.
type GroupProgress struct {
	// Language is the language about to be harvested.
	Language detect.Language
	// Scripts is how many scripts in this group are being read. Scripts served
	// from the cache are not in any group and are not counted.
	Scripts int
	// Done and Total count groups, not scripts: Done groups have been
	// harvested, of Total in this read.
	Done  int
	Total int
}

// NewReader returns a Reader with the given options.
func NewReader(options Options) *Reader {
	return &Reader{Options: options, interpreters: map[detect.Language]string{}}
}

// Read returns one report per requested script, in the requested order.
//
// Every requested path comes back, whatever its language and whatever goes
// wrong, so the caller's list of scripts and its list of forms never disagree. A
// failure is attached to the scripts it affects rather than raised for the
// batch.
func (r *Reader) Read(ctx context.Context, scripts []Script) ([]Report, error) {
	if len(scripts) == 0 {
		return nil, nil
	}

	// Group by language so that each toolchain is started once per batch rather
	// than once per script. The order of first appearance is kept so that a
	// missing interpreter is reported against the script the user is looking at.
	//
	// An unchanged script is answered from the cache and never enters a group,
	// which is what makes reopening a workspace cost nothing but a stat.
	var order []detect.Language
	grouped := map[detect.Language][]Script{}
	byPath := make(map[string]Report, len(scripts))
	for _, script := range scripts {
		if strings.TrimSpace(script.Path) == "" {
			continue
		}
		if r.Cache != nil {
			if report, ok := r.Cache.Lookup(script.Path, script.Size, script.ModTime, string(languageOf(script))); ok {
				byPath[script.Path] = report
				continue
			}
		}
		language := languageOf(script)
		if _, seen := grouped[language]; !seen {
			order = append(order, language)
		}
		grouped[language] = append(grouped[language], script)
	}
	if len(grouped) == 0 {
		return r.ordered(scripts, byPath), nil
	}

	for i, language := range order {
		group := grouped[language]
		paths := make([]string, 0, len(group))
		for _, script := range group {
			paths = append(paths, script.Path)
		}

		// Reported before the group is harvested, not after, because the harvest
		// is the part the caller is waiting on and can be the slowest thing a
		// read does when a toolchain has to start.
		if r.OnGroup != nil {
			r.OnGroup(ctx, GroupProgress{
				Language: language,
				Scripts:  len(group),
				Done:     i,
				Total:    len(order),
			})
		}

		reports, err := r.readGroup(ctx, language, paths)
		if err != nil {
			// A language whose toolchain is missing must not cost the other
			// languages their forms.
			for _, path := range paths {
				report := Report{Path: path, Help: Help{Source: HelpSourceNone}, ReadFailed: true}
				report.AddWarning("%s", err)
				byPath[path] = report
			}
			continue
		}
		for _, report := range reports {
			byPath[report.Path] = report
		}
	}

	// Whatever was read is remembered against the file state it was read from, so
	// that reopening the workspace does not pay for it again. A report that
	// records a failed read is left out: it describes a missing toolchain rather
	// than the script, and the entry would be keyed by a file that never
	// changed, so the missing parameters would outlive the reason for them.
	if r.Cache != nil {
		for _, script := range scripts {
			report, ok := byPath[script.Path]
			if !ok || !report.Cacheable() {
				continue
			}
			r.Cache.Store(script.Path, script.Size, script.ModTime, string(languageOf(script)), report)
		}
	}

	return r.ordered(scripts, byPath), nil
}

// languageOf is a script's language, defaulting the empty case so that callers
// do not each have to repeat it.
func languageOf(script Script) detect.Language {
	if script.Language == "" {
		return detect.Unknown
	}
	return script.Language
}

// ordered returns the reports in the caller's order.
//
// The caller's order is the one the user is looking at, so it is the order the
// reports come back in, and it is why the reports are matched back by path rather
// than by the order a toolchain happened to finish.
func (r *Reader) ordered(scripts []Script, byPath map[string]Report) []Report {
	out := make([]Report, 0, len(scripts))
	for _, script := range scripts {
		if strings.TrimSpace(script.Path) == "" {
			continue
		}
		report, ok := byPath[script.Path]
		if !ok {
			report = Report{Path: script.Path, Help: Help{Source: HelpSourceNone}}
			report.AddWarning("this script was not read, so it will run without a form")
		}
		out = append(out, report)
	}
	return out
}

// readGroup reads every script of one language.
func (r *Reader) readGroup(ctx context.Context, language detect.Language, paths []string) ([]Report, error) {
	extractor, err := r.extractorFor(language)
	if err != nil {
		return nil, err
	}
	// The harvesters read their configuration from their own fields, so the
	// caller's options are applied here rather than at construction.
	if batcher, ok := extractor.(batchable); ok {
		batcher.apply(r.Options)
	}
	return extractor.Extract(ctx, paths)
}

// extractorFor returns the extractor for a language, locating the toolchain once
// and remembering it. A scan of a hundred scripts should not search the
// filesystem a hundred times.
//
// The memo is held under the lock across the lookup, which looks like more
// contention than it is: the lookup runs at most once per language per process
// and every read after it is served from the map. Serializing those is cheaper
// than a double-checked read, and it cannot deadlock because nothing reached
// under this lock takes another lock.
func (r *Reader) extractorFor(language detect.Language) (Extractor, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.interpreters == nil {
		r.interpreters = map[detect.Language]string{}
	}
	if exe, ok := r.interpreters[language]; ok {
		return newExtractor(language, exe), nil
	}
	lookup := r.lookup
	if lookup == nil {
		lookup = toolchainFor
	}
	exe, err := lookup(language)
	if err != nil {
		return nil, err
	}
	if exe != "" {
		r.interpreters[language] = exe
	}
	return newExtractor(language, exe), nil
}

// batchable is implemented by the extractors that shell out to a toolchain and
// therefore take their configuration from the caller, which is how the Reader
// applies one timeout and batch size to harvesters it did not construct.
type batchable interface {
	apply(Options)
}

// apply lets the Reader configure a harvester it did not construct.
func (h *PowerShellHarvester) apply(options Options) {
	if options.Timeout > 0 {
		h.Timeout = options.Timeout
	}
	if options.BatchSize > 0 {
		h.BatchSize = options.BatchSize
	}
}

// apply lets the Reader configure a harvester it did not construct.
func (h *PythonHarvester) apply(options Options) {
	if options.Timeout > 0 {
		h.Timeout = options.Timeout
	}
	if options.BatchSize > 0 {
		h.BatchSize = options.BatchSize
	}
}

// Script is one script to read.
type Script struct {
	// Path is the script to read.
	Path string
	// Language decides how it is read. Empty means detect.Unknown.
	Language detect.Language
	// Size and ModTime are the file state the caller already had from scanning.
	// They are the cache key, so the reader does not have to stat a file the
	// scan already stat'd.
	Size    int64
	ModTime time.Time
}

// String renders a script for a log line.
func (s Script) String() string {
	if s.Language == "" {
		return s.Path
	}
	return fmt.Sprintf("%s (%s)", s.Path, s.Language)
}
