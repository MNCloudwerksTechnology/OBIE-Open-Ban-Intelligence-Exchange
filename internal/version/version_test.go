package version

import "testing"

func TestDefaultVersionIsDev(t *testing.T) {
	if Version != "dev" {
		t.Fatalf("default Version = %q, want %q", Version, "dev")
	}
}

func TestString(t *testing.T) {
	orig := Version
	t.Cleanup(func() { Version = orig })

	tests := []struct {
		name    string
		version string
		program string
		want    string
	}{
		{name: "default", version: "dev", program: "obied", want: "obied dev"},
		{name: "injected", version: "v0.1.0", program: "obiectl", want: "obiectl v0.1.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Version = tt.version
			if got := String(tt.program); got != tt.want {
				t.Errorf("String(%q) = %q, want %q", tt.program, got, tt.want)
			}
		})
	}
}
