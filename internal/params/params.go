// Package params describes the inputs a script accepts and how to prompt for
// them.
//
// Two very different jobs live here, and keeping them in one model is
// deliberate. Parsing a PowerShell param() block, an argparse declaration, or a
// clap derive yields a precise machine-readable schema. Reading the help text
// next to it yields prose. The execution form needs both: the schema to build
// controls, the prose to explain the control. Splitting them would mean the UI
// layer joins two half-results and has to cope with one arriving without the
// other.
//
// Everything reported here is a best effort. A script that cannot be parsed
// still appears in the list and can still be run; it just gets no form and a
// warning explaining why. That is why Parse issues are Warnings rather than
// errors.
package params

import (
	"fmt"
	"sort"
	"strings"
)

// Kind classifies a parameter's value so the UI can pick an editor. The kinds
// are deliberately coarse. A form built from a wrong-but-plausible control is
// worse than no form, so anything not confidently recognized stays KindOther
// and is presented as raw text.
type Kind string

// The parameter kinds the execution form knows how to render.
const (
	KindString Kind = "string"
	KindInt    Kind = "int"
	KindFloat  Kind = "float"
	// KindBool is driven by a switch or a flag with a literal false default,
	// so the form can offer a checkbox rather than a text box.
	KindBool  Kind = "bool"
	KindArray Kind = "array"

	// KindEnum is a closed set of values, normally from a ValidateSet or an
	// argparse choices list. It renders as a dropdown because the legal values
	// are already known and letting the user type a typo wastes a run.
	KindEnum Kind = "enum"

	// KindPath is a filesystem path. It renders with a browse button.
	KindPath Kind = "path"

	// KindSecret is a credential or other value the user should not have to
	// type into a visible field, such as PSCredential, SecureString, or a
	// password argument. See internal/creds for how the value is transported.
	KindSecret Kind = "secret"

	// KindOther is an unrecognized parameter. It renders as text.
	KindOther Kind = "other"
)

// String implements fmt.Stringer.
func (k Kind) String() string { return string(k) }

// ConstraintKind names a validation rule attached to a parameter.
type ConstraintKind string

// The constraint kinds the UI can explain to the user.
const (
	// ConstraintSet is a closed set of allowed values, from PowerShell
	// ValidateSet, argparse choices, or an equivalent construct.
	ConstraintSet ConstraintKind = "set"
	// ConstraintRange is an inclusive numeric range, from ValidateRange,
	// argparse type=int plus manual checks, or a min/max attribute.
	ConstraintRange ConstraintKind = "range"
	// ConstraintPattern is a regular expression the value must match.
	ConstraintPattern ConstraintKind = "pattern"
	// ConstraintLength bounds a string or collection length.
	ConstraintLength ConstraintKind = "length"
	// ConstraintNotEmpty forbids null, empty, or whitespace values.
	ConstraintNotEmpty ConstraintKind = "notEmpty"
	// ConstraintCredential requires a structured credential rather than text.
	ConstraintCredential ConstraintKind = "credential"
)

// Constraint is one validation rule. Only the fields relevant to Kind are
// populated; the rest stay zero. Keeping them on one struct rather than in a
// sum type means the UI can render "everything we know" without a switch over
// variants, and adding a language never requires a new Go type.
type Constraint struct {
	Kind ConstraintKind `json:"kind"`

	// Label is a short human-readable summary, such as
	// "one of: Start, Stop, Restart" or "between 1 and 100". It is computed by
	// the harvester so every language phrases its rules the same way.
	Label string `json:"label,omitempty"`

	// Values holds the allowed values for ConstraintSet.
	Values []string `json:"values,omitempty"`

	// Min and Max bound ConstraintRange and ConstraintLength. HasMin and
	// HasMax distinguish "no bound" from a bound of zero, which matters for a
	// range whose lower limit is legitimately 0.
	Min        float64 `json:"min,omitempty"`
	Max        float64 `json:"max,omitempty"`
	HasMin     bool    `json:"hasMin,omitempty"`
	HasMax     bool    `json:"hasMax,omitempty"`
	IncludeMin bool    `json:"includeMin,omitempty"`
	IncludeMax bool    `json:"includeMax,omitempty"`

	// Pattern is the regular expression for ConstraintPattern.
	Pattern string `json:"pattern,omitempty"`
}

// IsZero reports whether the constraint carries no information, which happens
// when a harvester produces a constraint kind it cannot populate. Such
// constraints are dropped rather than shown as a blank rule.
func (c Constraint) IsZero() bool {
	switch c.Kind {
	case ConstraintSet:
		return len(c.Values) == 0
	case ConstraintRange:
		return !c.HasMin && !c.HasMax
	case ConstraintLength:
		return !c.HasMin && !c.HasMax
	case ConstraintPattern:
		return c.Pattern == ""
	default:
		return c.Label == ""
	}
}

// Value is a parameter default. Source is the text as it appears in the file,
// which is what a user expects to see and is the only form that round-trips
// safely. Display is Source after light formatting for the form, and Display
// falls back to Source when the harvester has nothing better.
//
// Default values are never evaluated at harvest time. An expression such as
// (Get-Date) or $(Get-Host) is left alone: running it during discovery would
// have side effects and would report a value the script never intended to use.
type Value struct {
	Source  string `json:"source"`
	Display string `json:"display,omitempty"`
	// IsExpression reports that Source is not a literal and cannot be shown
	// as a pre-filled control. The form leaves such a parameter empty.
	IsExpression bool `json:"isExpression,omitempty"`
}

// HelpString returns the best text to show for the default value.
func (v *Value) HelpString() string {
	if v == nil {
		return ""
	}
	if v.Display != "" {
		return v.Display
	}
	return v.Source
}

// Param is one input to a script.
type Param struct {
	// Name is the primary name, without any sigil. For PowerShell that is
	// the variable name; for argparse the first long or short option.
	Name string `json:"name"`

	// Aliases are the alternative names that select this parameter, such as
	// PowerShell Alias attributes or argparse short options.
	Aliases []string `json:"aliases,omitempty"`

	Kind Kind `json:"kind"`
	// TypeName is the language's own name for the type, such as "String",
	// "Int32", "System.String[]", or "int". It is shown as secondary text
	// because it is more precise than Kind but less familiar than Kind.
	TypeName string `json:"typeName,omitempty"`

	// Required reports that the script refuses to run without a value, from
	// Mandatory or an argparse required=True.
	Required bool `json:"required"`

	// Position is the 1-based positional index, or 0 when the parameter can
	// only be passed by name. PowerShell reports positions starting at 0 and
	// -1 for "none"; the harvester normalises that here.
	Position int `json:"position,omitempty"`

	// FromPipeline reports that the parameter accepts pipeline input, which
	// changes nothing about the form but is worth surfacing because it means
	// the parameter is rarely set directly.
	FromPipeline bool `json:"fromPipeline,omitempty"`

	// Default is the parameter's default, or nil when there is none.
	Default *Value `json:"default,omitempty"`

	// Help is the prose description of this parameter, from comment-based
	// help, an argparse help string, or a doc comment. Empty when unknown.
	Help string `json:"help,omitempty"`

	// Constraints are the validation rules the script enforces.
	Constraints []Constraint `json:"constraints,omitempty"`
}

// Flag returns a short label for the parameter, suitable for a form row.
func (p Param) Flag() string {
	if len(p.Aliases) == 0 {
		return p.Name
	}
	return p.Name + ", -" + strings.Join(p.Aliases, ", -")
}

// Example is one documented invocation.
type Example struct {
	// Title is the example's heading. Comment-based help fills Title with a
	// banner when the author gave none, so harvester output normalises that
	// to empty rather than leaking "--- EXAMPLE 1 ---" into the UI.
	Title string `json:"title,omitempty"`
	// Command is the invocation itself. This is the part worth copying.
	Command string `json:"command"`
	// Remarks is prose that followed the invocation.
	Remarks string `json:"remarks,omitempty"`
}

// HelpSource records where a Help value came from, so the UI can tell
// documented help apart from a placeholder.
type HelpSource string

// The help sources.
const (
	// HelpSourceNone means no help was found. Description and friends are
	// empty.
	HelpSourceNone HelpSource = "none"
	// HelpSourceComment is comment-based help, which is what the author
	// deliberately wrote for a reader.
	HelpSourceComment HelpSource = "comment"
	// HelpSourceUsage is a help message recovered by running the script with
	// a help flag, as for Python.
	HelpSourceUsage HelpSource = "usage"
	// HelpSourceGenerated means the toolchain synthesized a description from
	// the code, as PowerShell does for a param block with no comment help.
	// It is shown, but never presented as if the author wrote it.
	HelpSourceGenerated HelpSource = "generated"
	// HelpSourceDocComment is help lifted from a doc comment adjacent to the
	// declaration, as in Python or Rust.
	HelpSourceDocComment HelpSource = "docComment"
)

// Help is everything the script says about itself beyond its parameters.
type Help struct {
	Source HelpSource `json:"source"`

	Synopsis    string    `json:"synopsis,omitempty"`
	Description string    `json:"description,omitempty"`
	Notes       string    `json:"notes,omitempty"`
	Links       []string  `json:"links,omitempty"`
	Examples    []Example `json:"examples,omitempty"`
	InputTypes  string    `json:"inputTypes,omitempty"`
	OutputTypes string    `json:"outputTypes,omitempty"`

	// Params maps a parameter name to its description. It is keyed by name
	// only; the UI resolves aliases through Param.Name so that a doc comment
	// describing "-s/--server" still lands on the Server parameter.
	Params map[string]string `json:"params,omitempty"`
}

// ParamHelp returns the documented description for a parameter, trying the
// primary name and then each alias.
func (h Help) ParamHelp(p Param) string {
	if h.Params == nil {
		return ""
	}
	if s, ok := h.Params[p.Name]; ok && s != "" {
		return s
	}
	for _, a := range p.Aliases {
		if s, ok := h.Params[a]; ok && s != "" {
			return s
		}
	}
	return ""
}

// ModuleRequirement is one #Requires -Modules entry.
type ModuleRequirement struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// String renders the requirement the way it would be written in a script.
func (m ModuleRequirement) String() string {
	if m.Version == "" {
		return m.Name
	}
	return m.Name + " " + m.Version
}

// Requirements records the preconditions a script declares. The UI turns these
// into a preflight list so a user finds out about a missing module before the
// script runs, rather than from a red error halfway through.
type Requirements struct {
	// Modules are the PowerShell modules that must be importable.
	Modules []ModuleRequirement `json:"modules,omitempty"`
	// RunAsAdministrator requires elevation.
	RunAsAdministrator bool `json:"runAsAdministrator"`
	// PSVersion is the minimum PowerShell version, as written in the script.
	PSVersion string `json:"psVersion,omitempty"`
	// PSEditions are the required PowerShell editions, such as "Core".
	PSEditions []string `json:"psEditions,omitempty"`
	// PythonVersion is the minimum Python version, taken from a version gate in
	// the script's own code such as a sys.version_info comparison. It has its
	// own field rather than sharing PSVersion because a version means nothing
	// without the interpreter it applies to.
	PythonVersion string `json:"pythonVersion,omitempty"`
}

// IsEmpty reports whether the script declared no requirements.
func (r Requirements) IsEmpty() bool {
	return len(r.Modules) == 0 && !r.RunAsAdministrator && r.PSVersion == "" &&
		len(r.PSEditions) == 0 && r.PythonVersion == ""
}

// Report is everything harvested about one script.
type Report struct {
	// Path is the script the report describes, as the harvester was given it.
	Path string `json:"path"`

	// Params are the script's inputs, in declaration order. Order matters
	// because it is the order a positional caller must supply them, and it is
	// the order a human expects to read.
	Params []Param `json:"params"`

	// Help is the script's own documentation.
	Help Help `json:"help"`

	// Requirements are declared preconditions, for languages that have them.
	Requirements Requirements `json:"requirements"`

	// DotSources are the scripts this one dot-sources, as written in the
	// file, before any resolution to a path. internal/deps turns these into
	// a copy closure. It stays a raw string here because resolution needs
	// $PSScriptRoot, which only the importing context knows.
	DotSources []string `json:"dotSources,omitempty"`

	// Warnings explain anything that could not be understood. A script with
	// warnings is still runnable; the UI shows them next to the script rather
	// than hiding it.
	Warnings []string `json:"warnings,omitempty"`

	// UnparsedHelpBlock reports that the file contains a comment-based help
	// block that PowerShell's own help reader skipped, which happens whenever
	// the block is not positioned ahead of the param block. The prose is
	// recovered from the raw file by internal/docs, so callers should consult
	// it before showing Help as the script's own words.
	UnparsedHelpBlock bool `json:"unparsedHelpBlock,omitempty"`
}

// HasWarnings reports whether any problem was recorded.
func (r Report) HasWarnings() bool { return len(r.Warnings) > 0 }

// AddWarning records a problem, ignoring blanks and exact duplicates so a
// harvester that reports the same issue per parameter does not spam the UI.
func (r *Report) AddWarning(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if msg == "" {
		return
	}
	for _, w := range r.Warnings {
		if w == msg {
			return
		}
	}
	r.Warnings = append(r.Warnings, msg)
}

// SortedAliases returns the aliases in a stable order, so that two harvests of
// the same script produce byte-identical output and cache entries compare
// equal. PowerShell attributes are read in source order, but a harvester that
// gathers them into a map would otherwise leak map iteration order.
func SortedAliases(in []string) []string {
	if len(in) < 2 {
		return in
	}
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
