package params

import (
	"testing"
)

func TestBuildArgvPowerShell(t *testing.T) {
	report := Report{
		Params: []Param{
			{Name: "Server", Kind: KindString, Required: true},
			{Name: "Port", Kind: KindInt, Required: false, Default: &Value{Source: "8080"}},
			{Name: "Verbose", Kind: KindBool, Required: false},
			{Name: "Tags", Kind: KindArray, Required: false},
			{Name: "Mode", Kind: KindEnum, Required: true, Constraints: []Constraint{
				{Kind: ConstraintSet, Values: []string{"fast", "thorough"}},
			}},
			{Name: "Config", Kind: KindPath, Required: false},
			{Name: "Password", Kind: KindSecret, Required: false},
		},
	}

	tests := []struct {
		name        string
		values      map[string]any
		wantArgv    []string
		wantError   bool
	}{
		{
			name: "all required provided",
			values: map[string]any{
				"Server": "myserver",
				"Mode":   "fast",
			},
			wantArgv: []string{"-Server", "myserver", "-Port", "8080", "-Mode", "fast"},
		},
		{
			name: "with defaults",
			values: map[string]any{
				"Server": "myserver",
				"Mode":   "thorough",
			},
			wantArgv: []string{"-Server", "myserver", "-Port", "8080", "-Mode", "thorough"},
		},
		{
			name: "bool true",
			values: map[string]any{
				"Server":  "myserver",
				"Mode":    "fast",
				"Verbose": true,
			},
			wantArgv: []string{"-Server", "myserver", "-Port", "8080", "-Verbose", "-Mode", "fast"},
		},
		{
			name: "bool false omitted",
			values: map[string]any{
				"Server":  "myserver",
				"Mode":    "fast",
				"Verbose": false,
			},
			wantArgv: []string{"-Server", "myserver", "-Port", "8080", "-Mode", "fast"},
		},
		{
			name: "array comma-separated",
			values: map[string]any{
				"Server": "myserver",
				"Mode":   "fast",
				"Tags":   []string{"prod", "web", "api"},
			},
			wantArgv: []string{"-Server", "myserver", "-Port", "8080", "-Tags", "prod,web,api", "-Mode", "fast"},
		},
		{
			name: "array from comma string",
			values: map[string]any{
				"Server": "myserver",
				"Mode":   "fast",
				"Tags":   "prod, web, api",
			},
			wantArgv: []string{"-Server", "myserver", "-Port", "8080", "-Tags", "prod,web,api", "-Mode", "fast"},
		},
		{
			name: "enum validation success",
			values: map[string]any{
				"Server": "myserver",
				"Mode":   "thorough",
			},
			wantArgv: []string{"-Server", "myserver", "-Port", "8080", "-Mode", "thorough"},
		},
		{
			name: "enum validation failure",
			values: map[string]any{
				"Server": "myserver",
				"Mode":   "invalid",
			},
			wantError: true,
		},
		{
			name: "secret omitted",
			values: map[string]any{
				"Server":   "myserver",
				"Mode":     "fast",
				"Password": "secret123",
			},
			wantArgv: []string{"-Server", "myserver", "-Port", "8080", "-Mode", "fast"},
		},
		{
			name: "missing required",
			values: map[string]any{
				"Mode": "fast",
			},
			wantError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			argv, err := BuildArgv(report, tc.values, "powershell")
			if tc.wantError {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !equalArgv(argv, tc.wantArgv) {
				t.Errorf("argv mismatch\ngot:  %v\nwant: %v", argv, tc.wantArgv)
			}
		})
	}
}

func TestBuildArgvPython(t *testing.T) {
	report := Report{
		Params: []Param{
			{Name: "target", Kind: KindString, Required: true, Position: 1},
			{Name: "server", Kind: KindString, Required: false, Aliases: []string{"s"}},
			{Name: "threads", Kind: KindInt, Required: false, Default: &Value{Source: "4"}},
			{Name: "verbose", Kind: KindBool, Required: false},
			{Name: "tag", Kind: KindArray, Required: false, Nargs: "*"},
			{Name: "mode", Kind: KindEnum, Required: true, Constraints: []Constraint{
				{Kind: ConstraintSet, Values: []string{"fast", "thorough"}},
			}},
			{Name: "config", Kind: KindPath, Required: false},
			{Name: "password", Kind: KindSecret, Required: false},
		},
	}

	tests := []struct {
		name      string
		values    map[string]any
		wantArgv  []string
		wantError bool
	}{
		{
			name: "required provided",
			values: map[string]any{
				"target": "myserver",
				"mode":   "fast",
			},
			wantArgv: []string{"--target", "myserver", "--threads", "4", "--mode", "fast"},
		},
		{
			name: "with defaults",
			values: map[string]any{
				"target": "myserver",
				"mode":   "thorough",
			},
			wantArgv: []string{"--target", "myserver", "--threads", "4", "--mode", "thorough"},
		},
		{
			name: "bool true",
			values: map[string]any{
				"target":  "myserver",
				"mode":    "fast",
				"verbose": true,
			},
			wantArgv: []string{"--target", "myserver", "--threads", "4", "--verbose", "--mode", "fast"},
		},
		{
			name: "array nargs=* repeats flag",
			values: map[string]any{
				"target": "myserver",
				"mode":   "fast",
				"tag":    []string{"prod", "web"},
			},
			wantArgv: []string{"--target", "myserver", "--threads", "4", "--tag", "prod", "--tag", "web", "--mode", "fast"},
		},
		{
			name: "enum validation",
			values: map[string]any{
				"target": "myserver",
				"mode":   "invalid",
			},
			wantError: true,
		},
		{
			name: "secret omitted",
			values: map[string]any{
				"target":   "myserver",
				"mode":     "fast",
				"password": "secret123",
			},
			wantArgv: []string{"--target", "myserver", "--threads", "4", "--mode", "fast"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			argv, err := BuildArgv(report, tc.values, "python")
			if tc.wantError {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !equalArgv(argv, tc.wantArgv) {
				t.Errorf("argv mismatch\ngot:  %v\nwant: %v", argv, tc.wantArgv)
			}
		})
	}
}

func TestBuildArgvBash(t *testing.T) {
	report := Report{
		Params: []Param{
			{Name: "server", Kind: KindString, Required: true},
			{Name: "verbose", Kind: KindBool, Required: false},
			{Name: "tag", Kind: KindArray, Required: false},
		},
	}

	tests := []struct {
		name      string
		values    map[string]any
		wantArgv  []string
		wantError bool
	}{
		{
			name: "basic",
			values: map[string]any{
				"server": "myserver",
			},
			wantArgv: []string{"--server", "myserver"},
		},
		{
			name: "bool true",
			values: map[string]any{
				"server":  "myserver",
				"verbose": true,
			},
			wantArgv: []string{"--server", "myserver", "--verbose"},
		},
		{
			name: "array repeats flag",
			values: map[string]any{
				"server": "myserver",
				"tag":    []string{"prod", "web"},
			},
			wantArgv: []string{"--server", "myserver", "--tag", "prod", "--tag", "web"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			argv, err := BuildArgv(report, tc.values, "bash")
			if tc.wantError {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !equalArgv(argv, tc.wantArgv) {
				t.Errorf("argv mismatch\ngot:  %v\nwant: %v", argv, tc.wantArgv)
			}
		})
	}
}

func equalArgv(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}