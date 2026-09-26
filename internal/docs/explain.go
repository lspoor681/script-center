package docs

import (
	"os"

	"github.com/lspoor/script-center/internal/params"
)

// Explanation is everything known about one script's documentation, merged into
// the form the UI shows.
type Explanation struct {
	// Help is the script's own documentation. When the toolchain's help reader
	// skipped a help block, this holds the prose recovered from the raw file
	// instead, and Recovered says so.
	Help params.Help
	// Recovered reports that Help came from the raw-text fallback rather than
	// from the language's own help reader. The two are not equally trustworthy
	// in every respect, so the UI is told which produced it.
	Recovered bool

	// Documents are the readmes and sidecars found for the script, most
	// specific first.
	Documents []Document
	// Primary is the document to show first, or nil when none was found.
	Primary *Document
}

// HasDocuments reports whether any file was found to explain the script.
func (e Explanation) HasDocuments() bool { return len(e.Documents) > 0 }

// Explain merges a harvested report with the raw file and the surrounding
// documents.
//
// The report's own help is preferred wherever it is real. Get-Help describes a
// param block that has no comment help at all, inventing a synopsis from the
// parameter list, so a report is only taken at its word when it either produced
// prose or flagged that a help block went unread. In the flagged case the raw
// file is consulted, because a script whose help sits after its code is common
// and would otherwise appear to have no documentation.
func Explain(scriptPath, root string, report params.Report) (Explanation, error) {
	explanation := Explanation{Help: report.Help}

	switch {
	case report.UnparsedHelpBlock:
		recovered, ok := readRecoveredHelp(scriptPath)
		if ok {
			explanation.Help = mergeHelp(recovered, report)
			explanation.Recovered = true
		}
	case report.Help.Source == params.HelpSourceNone:
		// The toolchain found nothing. A block comment carrying help tags is
		// worth reading anyway, since that is exactly the case it declines.
		if recovered, ok := readRecoveredHelp(scriptPath); ok {
			explanation.Help = recovered
			explanation.Recovered = true
		}
	}

	documents, err := Discover(scriptPath, root)
	if err != nil {
		return explanation, err
	}
	explanation.Documents = documents
	if len(documents) > 0 {
		primary := documents[0]
		explanation.Primary = &primary
	}
	return explanation, nil
}

// readRecoveredHelp parses a script's own text for a comment-based help block.
func readRecoveredHelp(scriptPath string) (params.Help, bool) {
	data, err := os.ReadFile(scriptPath)
	if err != nil {
		return params.Help{Source: params.HelpSourceNone}, false
	}
	return ParsePowerShellHelpBlock(data)
}

// mergeHelp combines recovered prose with what the parser already knew.
//
// The parser's parameter schema is never in question, so its per-parameter help
// and its requirements are kept as they are. Only the prose is taken from the
// recovered block, and only where the parser had none: the toolchain's own reader
// implements the format properly and is preferred wherever it succeeded.
func mergeHelp(recovered params.Help, report params.Report) params.Help {
	merged := recovered
	if report.Help.Synopsis != "" {
		merged.Synopsis = report.Help.Synopsis
	}
	if report.Help.Description != "" {
		merged.Description = report.Help.Description
	}
	if report.Help.Notes != "" {
		merged.Notes = report.Help.Notes
	}
	if len(report.Help.Links) > 0 {
		merged.Links = report.Help.Links
	}
	if len(report.Help.Examples) > 0 {
		merged.Examples = report.Help.Examples
	}
	if report.Help.InputTypes != "" {
		merged.InputTypes = report.Help.InputTypes
	}
	if report.Help.OutputTypes != "" {
		merged.OutputTypes = report.Help.OutputTypes
	}
	// Parameter descriptions from either source are combined, because a parser
	// that read the synopsis successfully may still have missed a parameter's
	// description, and the recovered block may document parameters the parser
	// never saw.
	if len(report.Help.Params) > 0 {
		if merged.Params == nil {
			merged.Params = make(map[string]string, len(report.Help.Params))
		}
		for name, text := range report.Help.Params {
			if text != "" {
				merged.Params[name] = text
			}
		}
	}
	merged.Source = params.HelpSourceComment
	return merged
}
