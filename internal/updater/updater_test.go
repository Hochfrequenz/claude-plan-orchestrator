package updater

import "testing"

func TestNeedsUpdate(t *testing.T) {
	tests := []struct {
		name    string
		current string
		latest  string
		want    bool
	}{
		{"same version", "v0.3.19", "v0.3.19", false},
		{"patch update", "v0.3.19", "v0.3.20", true},
		{"minor update", "v0.3.19", "v0.4.0", true},
		{"major update", "v0.3.19", "v1.0.0", true},
		{"current is newer", "v0.4.0", "v0.3.19", false},
		{"without v prefix", "0.3.19", "0.3.20", true},
		{"mixed prefixes", "v0.3.19", "0.3.20", true},
		{"dev version needs update", "dev", "v0.3.20", true},
		{"dev to dev", "dev", "dev", false},
		{"multi-digit versions", "v0.3.9", "v0.3.10", true},
		{"same major minor", "v1.2.3", "v1.2.3", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NeedsUpdate(tt.current, tt.latest)
			if got != tt.want {
				t.Errorf("NeedsUpdate(%q, %q) = %v, want %v", tt.current, tt.latest, got, tt.want)
			}
		})
	}
}

func TestParseVersion(t *testing.T) {
	tests := []struct {
		input string
		want  [3]int
	}{
		{"0.3.19", [3]int{0, 3, 19}},
		{"1.0.0", [3]int{1, 0, 0}},
		{"10.20.30", [3]int{10, 20, 30}},
		{"invalid", [3]int{0, 0, 0}},
		{"1.2", [3]int{1, 2, 0}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseVersion(tt.input)
			if got != tt.want {
				t.Errorf("parseVersion(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
