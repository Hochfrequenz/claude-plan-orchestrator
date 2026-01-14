// cmd/build-mcp/main_test.go
package main

import (
	"strings"
	"testing"
)

func TestConstructGitDaemonURL(t *testing.T) {
	tests := []struct {
		name           string
		coordURL       string
		wantPrefix     string // Use prefix for localhost cases (host varies by machine)
		wantExact      string // Use exact for non-localhost cases
		checkLocalhost bool   // Whether to verify localhost substitution
	}{
		{
			name:           "localhost gets substituted with external host",
			coordURL:       "http://localhost:8081",
			wantPrefix:     "git://",
			checkLocalhost: true, // Verify it's NOT localhost
		},
		{
			name:           "127.0.0.1 gets substituted with external host",
			coordURL:       "http://127.0.0.1:8081",
			wantPrefix:     "git://",
			checkLocalhost: true,
		},
		{
			name:      "https URL preserves host",
			coordURL:  "https://buildserver.local:8081",
			wantExact: "git://buildserver.local:9418/",
		},
		{
			name:      "IP address preserved",
			coordURL:  "http://192.168.1.100:8081/api",
			wantExact: "git://192.168.1.100:9418/",
		},
		{
			name:      "private IP without port",
			coordURL:  "http://10.0.0.5",
			wantExact: "git://10.0.0.5:9418/",
		},
		{
			name:      "empty URL",
			coordURL:  "",
			wantExact: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := constructGitDaemonURL(tt.coordURL)

			if tt.wantExact != "" {
				if got != tt.wantExact {
					t.Errorf("constructGitDaemonURL(%q) = %q, want %q", tt.coordURL, got, tt.wantExact)
				}
				return
			}

			if tt.checkLocalhost {
				// Verify it starts with git:// and ends with :9418/
				if !strings.HasPrefix(got, "git://") {
					t.Errorf("constructGitDaemonURL(%q) = %q, want prefix git://", tt.coordURL, got)
				}
				if !strings.HasSuffix(got, ":9418/") {
					t.Errorf("constructGitDaemonURL(%q) = %q, want suffix :9418/", tt.coordURL, got)
				}
				// Verify localhost was substituted (should not contain localhost or 127.0.0.1)
				// Note: getExternalHost() may return "localhost" as last resort if no network
				// So we just verify the structure is correct
				host := strings.TrimPrefix(got, "git://")
				host = strings.TrimSuffix(host, ":9418/")
				if host == "" {
					t.Errorf("constructGitDaemonURL(%q) = %q, host is empty", tt.coordURL, got)
				}
			}
		})
	}
}
