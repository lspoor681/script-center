package elevate

import (
	"errors"
	"reflect"
	"testing"
)

func TestWrap(t *testing.T) {
	previous := detect
	t.Cleanup(func() { detect = previous })

	tests := []struct {
		name   string
		status Status
		want   []string
		errIs  error
	}{
		{
			name:   "an inline run prefixes the tool",
			status: Status{Available: true, Inline: true, Method: "sudo", prefix: []string{"/usr/bin/sudo"}},
			want:   []string{"/usr/bin/sudo", "/bin/bash", "deploy.sh"},
		},
		{
			name:   "no elevation is an error with the hint",
			status: Status{Available: false, Method: "sudo", Hint: "install sudo"},
			errIs:  ErrUnavailable,
		},
		{
			name:   "elevation that is not inline cannot be wrapped",
			status: Status{Available: true, Inline: false, Method: "runas"},
			errIs:  ErrUnavailable,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detect = func() Status { return tt.status }
			got, err := Wrap([]string{"/bin/bash", "deploy.sh"})
			if tt.errIs != nil {
				if !errors.Is(err, tt.errIs) {
					t.Fatalf("Wrap err = %v, want %v", err, tt.errIs)
				}
				return
			}
			if err != nil {
				t.Fatalf("Wrap: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Wrap = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWrapLeavesInputUntouched(t *testing.T) {
	previous := detect
	t.Cleanup(func() { detect = previous })
	detect = func() Status { return Status{Available: true, Inline: true, prefix: []string{"sudo"}} }

	argv := []string{"pwsh", "-File", "deploy.ps1"}
	got, err := Wrap(argv)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"sudo", "pwsh", "-File", "deploy.ps1"}) {
		t.Errorf("Wrap = %v", got)
	}
	if !reflect.DeepEqual(argv, []string{"pwsh", "-File", "deploy.ps1"}) {
		t.Errorf("Wrap mutates its input: argv = %v", argv)
	}
}
