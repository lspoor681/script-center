package params

import (
	"fmt"
	"strings"
)

// BuildArgv builds the command-line arguments for a script from the harvested
// parameter report and user-provided values.
//
// The language parameter determines the argument syntax:
//   - PowerShell: -Name Value, -Switch, -Array @('v1','v2')
//   - Python: --name=value / --name value, --flag, repeat --name for arrays
//   - Bash: --name=value / --name value, --flag, repeat --name for arrays
//
// Values for parameters not in the report are ignored. Required parameters
// without a value (and no default) cause an error. KindSecret parameters are
// omitted from argv (the script should prompt interactively).
func BuildArgv(report Report, values map[string]any, language string) ([]string, error) {
	var argv []string
	seen := make(map[string]bool)

	// Build a map for quick lookup
	paramMap := make(map[string]Param, len(report.Params))
	for _, p := range report.Params {
		paramMap[p.Name] = p
		for _, alias := range p.Aliases {
			if _, exists := paramMap[alias]; !exists {
				paramMap[alias] = p
			}
		}
	}

	// Process params in declaration order (report.Params order)
	for _, param := range report.Params {
		if seen[param.Name] {
			continue
		}
		seen[param.Name] = true

		val, provided := values[param.Name]
		if !provided {
			// Check aliases for provided value
			for _, alias := range param.Aliases {
				if v, ok := values[alias]; ok {
					val = v
					provided = true
					break
				}
			}
		}

		if !provided {
			if param.Default != nil && !param.Default.IsExpression {
				// Use default if it's a literal (not an expression)
				val = param.Default.Source
				provided = true
			}
		}

		if !provided {
			if param.Required {
				return nil, fmt.Errorf("params: required parameter %q has no value", param.Name)
			}
			// Optional parameter not provided - skip (don't pass flag at all)
			continue
		}

		// Skip secrets - let the script prompt interactively
		if param.Kind == KindSecret {
			continue
		}

		args, err := buildParamArgs(param, val, language)
		if err != nil {
			return nil, err
		}
		argv = append(argv, args...)
	}

	return argv, nil
}

func buildParamArgs(param Param, val any, language string) ([]string, error) {
	switch language {
	case "powershell":
		return buildPowerShellArgs(param, val)
	case "python":
		return buildPythonArgs(param, val)
	case "bash":
		return buildBashArgs(param, val)
	default:
		return nil, fmt.Errorf("params: unknown language %q", language)
	}
}

func buildPowerShellArgs(param Param, val any) ([]string, error) {
	name := param.Name

	// PowerShell parameter names don't include the $ sigil
	switch param.Kind {
	case KindBool:
		// SwitchParameter: -Name (present = true, absent = false)
		// For bool with explicit false default, we still pass -Name:$false if user sets it
		b, ok := val.(bool)
		if !ok {
			// Try to parse string
			if s, ok := val.(string); ok {
				b = strings.ToLower(s) == "true" || s == "1" || strings.ToLower(s) == "yes"
			} else {
				return nil, fmt.Errorf("params: %s: expected bool, got %T", name, val)
			}
		}
		if b {
			return []string{"-" + name}, nil
		}
		// For false, we could pass -Name:$false but typically switches are omitted when false
		// Only pass if explicitly set to false by user (hard to detect), so omit for now
		return nil, nil

	case KindArray:
		arr, ok := toStringSlice(val)
		if !ok {
			return nil, fmt.Errorf("params: %s: expected array, got %T", name, val)
		}
		if len(arr) == 0 {
			return nil, nil
		}
		// Use comma-separated format (most common in PowerShell CLI)
		joined := strings.Join(arr, ",")
		return []string{"-" + name, joined}, nil

	case KindEnum:
		// Validate against allowed values
		for _, c := range param.Constraints {
			if c.Kind == ConstraintSet {
				s := fmt.Sprintf("%v", val)
				found := false
				for _, allowed := range c.Values {
					if allowed == s {
						found = true
						break
					}
				}
				if !found {
					return nil, fmt.Errorf("params: %s: value %q not in allowed set %v", name, s, c.Values)
				}
			}
		}
		return []string{"-" + name, fmt.Sprintf("%v", val)}, nil

	case KindPath:
		// Path is just a string argument
		return []string{"-" + name, fmt.Sprintf("%v", val)}, nil

	default:
		// string, int, float, other - pass as string
		return []string{"-" + name, fmt.Sprintf("%v", val)}, nil
	}
}

func buildPythonArgs(param Param, val any) ([]string, error) {
	name := param.Name
	// Python uses --long-name format
	flagName := "--" + name

	switch param.Kind {
	case KindBool:
		b, ok := val.(bool)
		if !ok {
			if s, ok := val.(string); ok {
				b = strings.ToLower(s) == "true" || s == "1" || strings.ToLower(s) == "yes"
			} else {
				return nil, fmt.Errorf("params: %s: expected bool, got %T", name, val)
			}
		}
		if b {
			return []string{flagName}, nil
		}
		// For store_false actions, we'd need to know the action type
		// For now, omit when false (argparse default handles it)
		return nil, nil

	case KindArray:
		arr, ok := toStringSlice(val)
		if !ok {
			return nil, fmt.Errorf("params: %s: expected array, got %T", name, val)
		}
		if len(arr) == 0 {
			return nil, nil
		}
		// Check nargs to determine format
		switch param.Nargs {
		case "*", "+":
			// Repeat the flag for each value: --tag v1 --tag v2
			var args []string
			for _, v := range arr {
				args = append(args, flagName, v)
			}
			return args, nil
		default:
			// Space-separated: --tag v1 v2 v3
			args := []string{flagName}
			args = append(args, arr...)
			return args, nil
		}

	case KindEnum:
		for _, c := range param.Constraints {
			if c.Kind == ConstraintSet {
				s := fmt.Sprintf("%v", val)
				found := false
				for _, allowed := range c.Values {
					if allowed == s {
						found = true
						break
					}
				}
				if !found {
					return nil, fmt.Errorf("params: %s: value %q not in allowed set %v", name, s, c.Values)
				}
			}
		}
		return []string{flagName, fmt.Sprintf("%v", val)}, nil

	case KindPath:
		return []string{flagName, fmt.Sprintf("%v", val)}, nil

	default:
		return []string{flagName, fmt.Sprintf("%v", val)}, nil
	}
}

func buildBashArgs(param Param, val any) ([]string, error) {
	// Bash uses similar syntax to Python (GNU long options)
	name := param.Name
	flagName := "--" + name

	switch param.Kind {
	case KindBool:
		b, ok := val.(bool)
		if !ok {
			if s, ok := val.(string); ok {
				b = strings.ToLower(s) == "true" || s == "1" || strings.ToLower(s) == "yes"
			} else {
				return nil, fmt.Errorf("params: %s: expected bool, got %T", name, val)
			}
		}
		if b {
			return []string{flagName}, nil
		}
		return nil, nil

	case KindArray:
		arr, ok := toStringSlice(val)
		if !ok {
			return nil, fmt.Errorf("params: %s: expected array, got %T", name, val)
		}
		if len(arr) == 0 {
			return nil, nil
		}
		// Repeat flag for each value (most compatible with getopts/case)
		var args []string
		for _, v := range arr {
			args = append(args, flagName, v)
		}
		return args, nil

	case KindEnum:
		for _, c := range param.Constraints {
			if c.Kind == ConstraintSet {
				s := fmt.Sprintf("%v", val)
				found := false
				for _, allowed := range c.Values {
					if allowed == s {
						found = true
						break
					}
				}
				if !found {
					return nil, fmt.Errorf("params: %s: value %q not in allowed set %v", name, s, c.Values)
				}
			}
		}
		return []string{flagName, fmt.Sprintf("%v", val)}, nil

	case KindPath:
		return []string{flagName, fmt.Sprintf("%v", val)}, nil

	default:
		return []string{flagName, fmt.Sprintf("%v", val)}, nil
	}
}

func toStringSlice(val any) ([]string, bool) {
	switch v := val.(type) {
	case []string:
		return v, true
	case []any:
		out := make([]string, len(v))
		for i, item := range v {
			out[i] = fmt.Sprintf("%v", item)
		}
		return out, true
	case string:
		// Comma-separated string -> split
		if v == "" {
			return []string{}, true
		}
		parts := strings.Split(v, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		return parts, true
	default:
		return nil, false
	}
}
