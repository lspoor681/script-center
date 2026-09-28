package params

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/lspoor/script-center/internal/proc"
)

// harvestScript is the PowerShell side of the harvester. It is embedded rather
// than installed alongside the binary so that a single executable is all a user
// needs, and it is extracted to a temporary file per run because PowerShell has
// no way to execute a script supplied on stdin.
//
//go:embed harvest.ps1
var harvestScript string

// ErrNoPowerShell reports that neither pwsh nor powershell is on PATH.
var ErrNoPowerShell = errors.New("params: no PowerShell interpreter found")

// ErrPowerShell reports that the interpreter ran but could not be used, for
// example because it is too old to expose the language API this package needs.
var ErrPowerShell = errors.New("params: PowerShell harvest failed")

// ErrUnsupportedPowerShellVersion reports an interpreter older than the language
// API requires. Get-Help's object shape and the parser's ScriptRequirements are
// both PowerShell 5-era, but ParseFile's ref parameters and the MAML property
// names this relies on are only dependable from 5.1 onward.
var ErrUnsupportedPowerShellVersion = errors.New("params: PowerShell 5.1 or newer is required")

// PowerShellHarvester reads parameter, help, and requirement metadata out of
// PowerShell scripts.
//
// It shells out to the real interpreter instead of reimplementing PowerShell's
// grammar. A hand-written parser for param blocks would have to keep pace with
// attributes, type constraints, and the many ways a default value can be
// written, and every gap would show up as a form that silently omits an input.
type PowerShellHarvester struct {
	// Exe is the interpreter to run. Resolve it with FindPowerShell.
	Exe string
	// Timeout bounds one interpreter invocation. Zero means DefaultTimeout.
	Timeout time.Duration
	// BatchSize is how many scripts one invocation reads. Get-Help is the
	// dominant cost and each script is independent, so batching trades a little
	// parallelism for far fewer interpreter startups. Zero means
	// DefaultBatchSize.
	BatchSize int
}

// DefaultTimeout bounds one harvest batch. It is generous because a cold
// PowerShell start on a spinning disk is slow, and a batch of fifty scripts each
// paying a Get-Help call is slower still.
const DefaultTimeout = 2 * time.Minute

// DefaultBatchSize is the number of scripts per interpreter invocation.
const DefaultBatchSize = 50

// FindPowerShell locates a usable interpreter, preferring PowerShell 7 over
// Windows PowerShell 5.1. Both expose what this package needs, but 7 is what a
// user running modern admin scripts is far more likely to have, and it starts
// noticeably faster.
func FindPowerShell() (string, error) {
	candidates := []string{"pwsh"}
	if runtime.GOOS == "windows" {
		// powershell.exe is the 5.1 shim and is usually not on PATH for a
		// non-interactive process, so the absolute location is tried too.
		candidates = append(candidates, "powershell")
		if programFiles := os.Getenv("ProgramFiles"); programFiles != "" {
			candidates = append(candidates,
				filepath.Join(programFiles, "PowerShell", "7", "pwsh.exe"))
		}
		if systemRoot := os.Getenv("SystemRoot"); systemRoot != "" {
			candidates = append(candidates,
				filepath.Join(systemRoot, "System32", "WindowsPowerShell", "v1.0", "powershell.exe"))
		}
	} else {
		candidates = append(candidates, "powershell")
	}
	for _, candidate := range candidates {
		if path, err := exec.LookPath(candidate); err == nil {
			return path, nil
		}
	}
	return "", ErrNoPowerShell
}

// Harvest reads metadata for the given scripts. The returned reports are in the
// same order as paths, and a script that could not be read still gets a report
// carrying the reason, because a script with no form is better than a script
// missing from the list.
func (h *PowerShellHarvester) Harvest(ctx context.Context, paths []string) ([]Report, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	exe := h.Exe
	if exe == "" {
		var err error
		if exe, err = FindPowerShell(); err != nil {
			return nil, err
		}
	}
	batchSize := h.BatchSize
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	timeout := h.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	// The script is written once per Harvest rather than once per batch so that
	// a large workspace does not churn temporary files.
	scriptPath, cleanup, err := writeHarvestScript()
	if err != nil {
		return nil, err
	}
	defer cleanup()

	reports := make([]Report, 0, len(paths))
	for start := 0; start < len(paths); start += batchSize {
		end := start + batchSize
		if end > len(paths) {
			end = len(paths)
		}
		batch, err := h.runBatch(ctx, exe, scriptPath, paths[start:end], timeout)
		if err != nil {
			return nil, err
		}
		reports = append(reports, batch...)
	}

	// A batch that dropped or reordered a record would silently misalign the
	// caller's view of which script produced which form, so the two are matched
	// back up by path rather than trusted to line up.
	return align(reports, paths, "PowerShell"), nil
}

// Extract implements Extractor. It is the same read as Harvest, under the name
// the multi-language reader uses, so that a caller holding an Extractor does not
// have to know which concrete harvester it has.
func (h *PowerShellHarvester) Extract(ctx context.Context, paths []string) ([]Report, error) {
	return h.Harvest(ctx, paths)
}

// runBatch invokes the interpreter once for one batch of scripts.
func (h *PowerShellHarvester) runBatch(ctx context.Context, exe, scriptPath string, paths []string, timeout time.Duration) ([]Report, error) {
	request, err := json.Marshal(struct {
		Paths []string `json:"paths"`
	}{Paths: paths})
	if err != nil {
		return nil, fmt.Errorf("params: encoding harvest request: %w", err)
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// NoProfile matters more than it looks: a user profile can define aliases,
	// prompt functions, and error handlers that change what the parser sees and
	// how failures surface. NonInteractive keeps a stray prompt from hanging the
	// harvest until the context deadline.
	cmd := exec.CommandContext(runCtx, exe,
		"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
	proc.NoWindow(cmd)
	cmd.Stdin = bytes.NewReader(request)
	cmd.Env = append(os.Environ(), "POWERSHELL_TELEMETRY_OPTOUT=1")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("%w: timed out after %s harvesting %d script(s)",
				ErrPowerShell, timeout, len(paths))
		}
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return nil, fmt.Errorf("%w: %s", ErrPowerShell, firstLine(detail))
	}

	return decodeRecords(stdout.Bytes(), paths)
}

// decodeRecords turns the interpreter's output into reports.
func decodeRecords(data []byte, requested []string) ([]Report, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("%w: the interpreter produced no output for %d script(s)",
			ErrPowerShell, len(requested))
	}

	// One script yields a one-element array, and PowerShell's ConvertTo-Json is
	// called with -InputObject precisely to avoid collapsing that to a bare
	// object. Both shapes are still accepted, because a script that produced no
	// records at all serializes as an empty array and a future edit could
	// reintroduce the collapse.
	var raw []harvestRecord
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		var single harvestRecord
		if singleErr := json.Unmarshal(trimmed, &single); singleErr != nil {
			return nil, fmt.Errorf("%w: decoding output: %w", ErrPowerShell, err)
		}
		raw = []harvestRecord{single}
	}

	reports := make([]Report, 0, len(raw))
	for _, record := range raw {
		reports = append(reports, record.report())
	}
	return reports, nil
}

// harvestRecord mirrors one record of the interpreter's JSON output. The shape is
// flattened rather than nested so that a missing sub-object decodes to a zero
// value instead of failing the whole batch.
type harvestRecord struct {
	Path              string       `json:"path"`
	Error             *string      `json:"error"`
	Params            []Param      `json:"params"`
	Help              Help         `json:"help"`
	Requirements      Requirements `json:"requirements"`
	DotSources        []string     `json:"dotSources"`
	UnparsedHelpBlock bool         `json:"unparsedHelpBlock"`
	// Warnings are the interpreter's own explanations, as opposed to Error,
	// which stops the script being read at all. A harvest carries both: one
	// script in a batch can fail to parse while the rest are fine.
	Warnings []string `json:"warnings"`
}

// report converts a wire record into a Report, normalising the loose ends that
// JSON decoding leaves behind.
func (r harvestRecord) report() Report {
	report := Report{
		Path:              r.Path,
		Params:            r.Params,
		Help:              r.Help,
		Requirements:      r.Requirements,
		DotSources:        r.DotSources,
		UnparsedHelpBlock: r.UnparsedHelpBlock,
		Warnings:          r.Warnings,
	}
	// A null help object decodes to the zero Help, whose Source is the empty
	// string rather than one of the named values.
	if report.Help.Source == "" {
		report.Help.Source = HelpSourceNone
	}
	for i := range report.Params {
		report.Params[i].Aliases = SortedAliases(report.Params[i].Aliases)
		report.Params[i].Constraints = dropEmptyConstraints(report.Params[i].Constraints)
		// PowerShell's help reader reports parameter descriptions in a map
		// beside the parameters rather than on them, so a parameter with no
		// prose of its own is filled in from there. A harvester that already
		// attached the prose to the parameter keeps what it has.
		if report.Params[i].Help == "" {
			report.Params[i].Help = report.Help.ParamHelp(report.Params[i])
		}
	}
	if r.Error != nil && *r.Error != "" {
		report.AddWarning("%s", *r.Error)
	}
	return report
}

// dropEmptyConstraints removes constraints the interpreter could not populate. A
// rule with no content is worse than no rule, because the form would render a
// blank validation message.
func dropEmptyConstraints(in []Constraint) []Constraint {
	if len(in) == 0 {
		return nil
	}
	out := make([]Constraint, 0, len(in))
	for _, constraint := range in {
		if constraint.IsZero() {
			continue
		}
		out = append(out, constraint)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// align returns one report per requested path, in the requested order, matching
// records to paths by value. A path with no record gets a report explaining that
// the interpreter returned nothing for it, so the caller never has to handle a
// short slice.
//
// The harvester's name is passed in because the warning is shown to the user and
// has to name the language that failed rather than whichever one happens to be
// built in.
func align(reports []Report, paths []string, harvester string) []Report {
	byPath := make(map[string]Report, len(reports))
	for _, report := range reports {
		if report.Path != "" {
			byPath[report.Path] = report
		}
	}
	out := make([]Report, len(paths))
	for i, path := range paths {
		report, ok := byPath[path]
		if !ok {
			report = Report{Path: path, Help: Help{Source: HelpSourceNone}}
			report.AddWarning("the %s harvester returned no record for this script, "+
				"so it will run without a form", harvester)
		}
		if report.Path == "" {
			report.Path = path
		}
		out[i] = report
	}
	return out
}

// writeHarvestScript puts the embedded interpreter script on disk so that
// PowerShell can run it by path. The file is not executable and holds no user
// data, so a plain temporary file is enough.
func writeHarvestScript() (string, func(), error) {
	dir, err := os.MkdirTemp("", "script-center-params-")
	if err != nil {
		return "", nil, fmt.Errorf("params: creating a directory for the harvest script: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	path := filepath.Join(dir, "harvest.ps1")
	if err := os.WriteFile(path, []byte(harvestScript), 0o600); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("params: writing the harvest script: %w", err)
	}
	return path, cleanup, nil
}

// firstLine trims a multi-line error down to its first line, because interpreter
// diagnostics are long and the first line is the part that says what failed.
func firstLine(text string) string {
	if index := strings.IndexByte(text, '\n'); index >= 0 {
		return strings.TrimSpace(text[:index])
	}
	return text
}

// Readability implements Extractor. PowerShell can always describe its own
// parameters, provided the interpreter was found; a missing one is reported by
// the harvester's own report rather than here, so that the reason reaches the
// user attached to the script it applies to.
func (h *PowerShellHarvester) Readability() Readability { return Readability{Readable: true} }
