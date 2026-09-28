package params

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/lspoor/script-center/internal/proc"
)

// harvestPythonScript is the Python side of the harvester, kept alongside
// harvest.ps1 for the same reason. Unlike PowerShell, Python can execute a
// script supplied on standard input, so it is piped rather than written to a
// temporary file, which means no temporary directory has to be created and
// cleaned up per run.
//
//go:embed harvest.py
var harvestPythonScript string

// ErrNoPython reports that no Python interpreter is on PATH.
var ErrNoPython = errors.New("params: no Python interpreter found")

// ErrPython reports that an interpreter ran but could not be used.
var ErrPython = errors.New("params: Python harvest failed")

// PythonHarvester reads parameter, help, and requirement metadata out of Python
// scripts.
//
// It runs the script through the interpreter's own ast module rather than
// parsing the text in Go. argparse describes its arguments in ordinary function
// calls, and which calls those are is exactly the question a text-level reader
// cannot answer without guessing. The tree answers it, and the same interpreter
// is already a precondition for running the script, so asking it costs nothing
// extra.
//
// The user's code is never imported or executed. A parser object only exists
// after the module body has run, and running untrusted code in order to describe
// it is not a trade worth making for the sake of a form.
type PythonHarvester struct {
	// Exe is the interpreter to run. Resolve it with FindPython.
	Exe string
	// Timeout bounds one interpreter invocation. Zero means DefaultTimeout.
	Timeout time.Duration
	// BatchSize is how many scripts one invocation reads. Zero means
	// DefaultBatchSize.
	BatchSize int
}

// pythonCandidates are the interpreter names tried in order.
//
// python3 is preferred over python because on many systems python is Python 2,
// which cannot parse the modern syntax these scripts are written in. On Windows
// the launcher is tried first because it is the one that reliably finds an
// installed interpreter.
func pythonCandidates() []string {
	if runtime.GOOS == "windows" {
		return append([]string{"py", "py -3"}, "python3", "python")
	}
	return []string{"python3", "python"}
}

// FindPython locates a usable interpreter, preferring Python 3.
func FindPython() (string, error) {
	var tried []string
	for _, candidate := range pythonCandidates() {
		fields := strings.Fields(candidate)
		exe := fields[0]
		tried = append(tried, candidate)

		// "py -3" is a launcher with an argument, so the probe has to carry it
		// through rather than just naming the executable.
		args := append(append([]string{}, fields[1:]...), "-c",
			"import sys; sys.exit(0 if sys.version_info[0] >= 3 else 1)")
		if path, err := exec.LookPath(exe); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			command := exec.CommandContext(ctx, path, args...)
			proc.NoWindow(command)
			err := command.Run()
			cancel()
			if err == nil {
				return path, nil
			}
		}
	}
	return "", fmt.Errorf("%w: tried %s", ErrNoPython, strings.Join(tried, ", "))
}

// Harvest reads the given scripts and returns one report per requested path, in
// the requested order.
func (h *PythonHarvester) Harvest(ctx context.Context, paths []string) ([]Report, error) {
	exe := h.Exe
	if exe == "" {
		return nil, ErrNoPython
	}
	if len(paths) == 0 {
		return nil, nil
	}

	// A path that cannot even be named is reported in place rather than
	// dropped, so that the caller's list and this list line up.
	readable := make([]string, 0, len(paths))
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		readable = append(readable, path)
	}

	batchSize := h.BatchSize
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	timeout := h.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	reports := make([]Report, 0, len(readable))
	for start := 0; start < len(readable); start += batchSize {
		end := start + batchSize
		if end > len(readable) {
			end = len(readable)
		}
		batch := readable[start:end]
		records, err := h.runBatch(ctx, exe, batch, timeout)
		if err != nil {
			return nil, err
		}
		reports = append(reports, records...)
	}

	// A batch that dropped or reordered a record would silently misalign the
	// caller's view of which script produced which form, so the records are
	// matched back to the requested paths by value rather than trusted to line
	// up by position.
	return align(reports, readable, "Python"), nil
}

// Extract implements Extractor. It is the same read as Harvest, under the name
// the multi-language reader uses, so that a caller holding an Extractor does not
// have to know which concrete harvester it has.
func (h *PythonHarvester) Extract(ctx context.Context, paths []string) ([]Report, error) {
	return h.Harvest(ctx, paths)
}

// runBatch pipes the harvester into one interpreter invocation and decodes the
// records it writes.
func (h *PythonHarvester) runBatch(ctx context.Context, exe string, paths []string, timeout time.Duration) ([]Report, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// The script arrives on standard input and the paths as arguments, so no
	// temporary file is involved and a path containing shell metacharacters
	// cannot influence anything.
	command := exec.CommandContext(ctx, exe, append([]string{"-"}, paths...)...)
	proc.NoWindow(command)
	command.Stdin = strings.NewReader(harvestPythonScript)

	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	if err := command.Run(); err != nil {
		// A file that does not parse is reported by that file's own record, so
		// a non-zero exit here means the interpreter itself failed and the
		// whole batch is affected.
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return nil, fmt.Errorf("%w: %s", ErrPython, firstLine(detail))
	}

	records, err := decodeJSONLines(stdout.Bytes(), len(paths))
	if err != nil {
		return nil, err
	}
	return records, nil
}

// decodeJSONLines reads the harvester's output, which is one JSON record per
// line rather than a single array. One line per script means a script that fails
// cannot cost the batch the records around it, which is the whole point of the
// warning-carrying design.
func decodeJSONLines(data []byte, requested int) ([]Report, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("%w: the interpreter produced no output for %d script(s)",
			ErrPython, requested)
	}

	var reports []Report
	scanner := bufio.NewScanner(bytes.NewReader(data))
	// A thorough module docstring makes a long line, and the default 64 KiB
	// limit is not enough for one.
	scanner.Buffer(make([]byte, 0, 64*1024), maxRecordBytes)

	for scanner.Scan() {
		line := strings.TrimSpace(string(stripControlBytes(scanner.Bytes())))
		if line == "" {
			continue
		}
		var record harvestRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			// A single undecodable line is not worth losing the batch over. The
			// path it belonged to is unknown, so it cannot be reported against
			// anything, and alignment below will account for the gap.
			continue
		}
		reports = append(reports, record.report())
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("%w: reading output: %w", ErrPython, err)
	}
	if len(reports) == 0 {
		return nil, fmt.Errorf("%w: no output could be decoded for %d script(s)",
			ErrPython, requested)
	}
	return reports, nil
}

// maxRecordBytes bounds one line of harvester output.
const maxRecordBytes = 8 * 1024 * 1024

// Readability implements Extractor. Python's own ast module describes an
// argparse script exactly, so a found interpreter means a readable one.
func (h *PythonHarvester) Readability() Readability { return Readability{Readable: true} }
