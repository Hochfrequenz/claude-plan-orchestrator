package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Config holds all application configuration
type Config struct {
	General       GeneralConfig       `toml:"general"`
	Claude        ClaudeConfig        `toml:"claude"`
	Notifications NotificationsConfig `toml:"notifications"`
	Web           WebConfig           `toml:"web"`
}

// GeneralConfig holds general settings
type GeneralConfig struct {
	ProjectRoot       string `toml:"project_root"`
	WorktreeDir       string `toml:"worktree_dir"`
	MaxParallelAgents int    `toml:"max_parallel_agents"`
	DatabasePath      string `toml:"database_path"`
}

// ClaudeConfig holds Claude API settings
type ClaudeConfig struct {
	Model     string `toml:"model"`
	MaxTokens int    `toml:"max_tokens"`
}

// NotificationsConfig holds notification settings
type NotificationsConfig struct {
	Desktop      bool   `toml:"desktop"`
	SlackWebhook string `toml:"slack_webhook"`
}

// WebConfig holds web UI settings
type WebConfig struct {
	Port int    `toml:"port"`
	Host string `toml:"host"`
}

// Default returns a Config with sensible defaults
func Default() *Config {
	home, _ := os.UserHomeDir()
	return &Config{
		General: GeneralConfig{
			ProjectRoot:       "",
			WorktreeDir:       filepath.Join(home, ".erp-orchestrator", "worktrees"),
			MaxParallelAgents: 3,
			DatabasePath:      filepath.Join(home, ".erp-orchestrator", "orchestrator.db"),
		},
		Claude: ClaudeConfig{
			Model:     "claude-sonnet-4-20250514",
			MaxTokens: 16000,
		},
		Notifications: NotificationsConfig{
			Desktop: true,
		},
		Web: WebConfig{
			Port: 8080,
			Host: "127.0.0.1",
		},
	}
}

// Load reads configuration from a TOML file, falling back to defaults
func Load(path string) (*Config, error) {
	cfg := Default()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}

	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	// Expand paths
	cfg.General.ProjectRoot = ExpandPath(cfg.General.ProjectRoot)
	cfg.General.WorktreeDir = ExpandPath(cfg.General.WorktreeDir)
	cfg.General.DatabasePath = ExpandPath(cfg.General.DatabasePath)

	return cfg, nil
}

// ExpandPath expands ~ to the user's home directory
func ExpandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}

// DefaultConfigPath returns the default config file location
func DefaultConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "erp-orchestrator", "config.toml")
}
