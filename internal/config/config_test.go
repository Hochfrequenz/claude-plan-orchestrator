package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_Defaults(t *testing.T) {
	cfg := Default()

	if cfg.General.MaxParallelAgents != 3 {
		t.Errorf("MaxParallelAgents = %d, want 3", cfg.General.MaxParallelAgents)
	}
	if cfg.Web.Port != 8080 {
		t.Errorf("Web.Port = %d, want 8080", cfg.Web.Port)
	}
	if cfg.Web.Host != "127.0.0.1" {
		t.Errorf("Web.Host = %q, want 127.0.0.1", cfg.Web.Host)
	}
}

func TestLoad_FromFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	content := `
[general]
project_root = "/test/project"
max_parallel_agents = 5

[web]
port = 9000
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.General.ProjectRoot != "/test/project" {
		t.Errorf("ProjectRoot = %q, want /test/project", cfg.General.ProjectRoot)
	}
	if cfg.General.MaxParallelAgents != 5 {
		t.Errorf("MaxParallelAgents = %d, want 5", cfg.General.MaxParallelAgents)
	}
	if cfg.Web.Port != 9000 {
		t.Errorf("Web.Port = %d, want 9000", cfg.Web.Port)
	}
}

func TestExpandPath(t *testing.T) {
	home, _ := os.UserHomeDir()

	tests := []struct {
		input string
		want  string
	}{
		{"~/test", filepath.Join(home, "test")},
		{"/absolute/path", "/absolute/path"},
		{"relative", "relative"},
	}

	for _, tt := range tests {
		got := ExpandPath(tt.input)
		if got != tt.want {
			t.Errorf("ExpandPath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestConfig_BuildPoolDefaults(t *testing.T) {
	cfg := Default()

	if cfg.BuildPool.WebSocketPort != 8081 {
		t.Errorf("got websocket_port=%d, want 8081", cfg.BuildPool.WebSocketPort)
	}
	if cfg.BuildPool.GitDaemonPort != 9418 {
		t.Errorf("got git_daemon_port=%d, want 9418", cfg.BuildPool.GitDaemonPort)
	}
	if !cfg.BuildPool.LocalFallback.Enabled {
		t.Error("local fallback should be enabled by default")
	}
}
