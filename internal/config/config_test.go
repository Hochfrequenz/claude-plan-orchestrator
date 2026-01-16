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

func TestFindLocalConfig(t *testing.T) {
	// Create a temp directory structure
	root := t.TempDir()
	subdir := filepath.Join(root, "sub", "dir")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create local config in root
	localConfig := filepath.Join(root, LocalConfigName)
	if err := os.WriteFile(localConfig, []byte("[general]\nproject_root = \"/local\""), 0644); err != nil {
		t.Fatal(err)
	}

	// Save current dir and change to subdir
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	if err := os.Chdir(subdir); err != nil {
		t.Fatal(err)
	}

	// Should find config in parent
	found := FindLocalConfig()
	if found != localConfig {
		t.Errorf("FindLocalConfig() = %q, want %q", found, localConfig)
	}
}

func TestFindLocalConfig_NotFound(t *testing.T) {
	// Create a temp directory without any config
	root := t.TempDir()

	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	found := FindLocalConfig()
	if found != "" {
		t.Errorf("FindLocalConfig() = %q, want empty string", found)
	}
}

func TestLoadWithLocalFallback_ExplicitPath(t *testing.T) {
	dir := t.TempDir()
	explicitPath := filepath.Join(dir, "explicit.toml")

	content := `[general]
project_root = "/explicit"
`
	if err := os.WriteFile(explicitPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadWithLocalFallback(explicitPath)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.General.ProjectRoot != "/explicit" {
		t.Errorf("ProjectRoot = %q, want /explicit", cfg.General.ProjectRoot)
	}
}

func TestLoadWithLocalFallback_LocalConfig(t *testing.T) {
	root := t.TempDir()
	localConfig := filepath.Join(root, LocalConfigName)

	content := `[general]
project_root = "/from-local"
`
	if err := os.WriteFile(localConfig, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadWithLocalFallback("")
	if err != nil {
		t.Fatal(err)
	}

	if cfg.General.ProjectRoot != "/from-local" {
		t.Errorf("ProjectRoot = %q, want /from-local", cfg.General.ProjectRoot)
	}
}

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestConfig_GitHubIssues(t *testing.T) {
	tomlContent := `
[general]
project_root = "/tmp/test"

[github_issues]
enabled = true
repo = "owner/repo"
candidate_label = "orchestrator-candidate"
ready_label = "implementation-ready"
refinement_label = "needs-refinement"
implemented_label = "implemented"
area_label_prefix = "area:"

[github_issues.priority_labels]
high = "priority:high"
medium = "priority:medium"
low = "priority:low"
`
	tmpFile := writeTempConfig(t, tomlContent)
	cfg, err := Load(tmpFile)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !cfg.GitHubIssues.Enabled {
		t.Error("expected GitHubIssues.Enabled = true")
	}
	if cfg.GitHubIssues.Repo != "owner/repo" {
		t.Errorf("Repo = %v, want owner/repo", cfg.GitHubIssues.Repo)
	}
	if cfg.GitHubIssues.CandidateLabel != "orchestrator-candidate" {
		t.Errorf("CandidateLabel = %v, want orchestrator-candidate", cfg.GitHubIssues.CandidateLabel)
	}
	if cfg.GitHubIssues.AreaLabelPrefix != "area:" {
		t.Errorf("AreaLabelPrefix = %v, want area:", cfg.GitHubIssues.AreaLabelPrefix)
	}
	// Verify priority labels were loaded
	if len(cfg.GitHubIssues.PriorityLabels) != 3 {
		t.Errorf("PriorityLabels length = %d, want 3", len(cfg.GitHubIssues.PriorityLabels))
	}
	if cfg.GitHubIssues.PriorityLabels["high"] != "priority:high" {
		t.Errorf("PriorityLabels[high] = %v, want priority:high", cfg.GitHubIssues.PriorityLabels["high"])
	}
}

func TestConfig_GitHubIssuesDefaults(t *testing.T) {
	cfg := Default()

	if cfg.GitHubIssues.Enabled {
		t.Error("expected GitHubIssues.Enabled = false by default")
	}
	if cfg.GitHubIssues.CandidateLabel != "orchestrator-candidate" {
		t.Errorf("CandidateLabel = %v, want orchestrator-candidate", cfg.GitHubIssues.CandidateLabel)
	}
	if cfg.GitHubIssues.ReadyLabel != "implementation-ready" {
		t.Errorf("ReadyLabel = %v, want implementation-ready", cfg.GitHubIssues.ReadyLabel)
	}
	if cfg.GitHubIssues.RefinementLabel != "needs-refinement" {
		t.Errorf("RefinementLabel = %v, want needs-refinement", cfg.GitHubIssues.RefinementLabel)
	}
	if cfg.GitHubIssues.ImplementedLabel != "implemented" {
		t.Errorf("ImplementedLabel = %v, want implemented", cfg.GitHubIssues.ImplementedLabel)
	}
	if cfg.GitHubIssues.AreaLabelPrefix != "area:" {
		t.Errorf("AreaLabelPrefix = %v, want area:", cfg.GitHubIssues.AreaLabelPrefix)
	}
	if cfg.GitHubIssues.PriorityLabels == nil {
		t.Error("PriorityLabels should not be nil")
	}
}
