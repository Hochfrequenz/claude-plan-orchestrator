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
	BuildPool     BuildPoolConfig     `toml:"build_pool"`
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

// BuildPoolConfig holds build pool settings
type BuildPoolConfig struct {
	Enabled             bool                   `toml:"enabled"`
	WebSocketPort       int                    `toml:"websocket_port"`
	GitDaemonPort       int                    `toml:"git_daemon_port"`
	GitDaemonListenAddr string                 `toml:"git_daemon_listen_addr"` // e.g., "127.0.0.1" for local only
	LocalFallback       LocalFallbackConfig    `toml:"local_fallback"`
	Timeouts            BuildPoolTimeoutConfig `toml:"timeouts"`
	Debug               bool                   `toml:"debug"` // Enable verbose heartbeat logging
}

// LocalFallbackConfig configures local job execution
type LocalFallbackConfig struct {
	Enabled     bool   `toml:"enabled"`
	MaxJobs     int    `toml:"max_jobs"`
	WorktreeDir string `toml:"worktree_dir"`
}

// BuildPoolTimeoutConfig configures timeouts
type BuildPoolTimeoutConfig struct {
	JobDefaultSecs        int `toml:"job_default_secs"`
	HeartbeatIntervalSecs int `toml:"heartbeat_interval_secs"`
	HeartbeatTimeoutSecs  int `toml:"heartbeat_timeout_secs"`
}

// Default returns a Config with sensible defaults
func Default() *Config {
	home, _ := os.UserHomeDir()
	return &Config{
		General: GeneralConfig{
			ProjectRoot:       "",
			WorktreeDir:       filepath.Join(home, ".claude-orchestrator", "worktrees"),
			MaxParallelAgents: 3,
			DatabasePath:      filepath.Join(home, ".claude-orchestrator", "orchestrator.db"),
		},
		Claude: ClaudeConfig{
			Model:     "claude-opus-4-5-20251101",
			MaxTokens: 16000,
		},
		Notifications: NotificationsConfig{
			Desktop: true,
		},
		Web: WebConfig{
			Port: 8080,
			Host: "127.0.0.1",
		},
		BuildPool: BuildPoolConfig{
			Enabled:       false,
			WebSocketPort: 8081,
			GitDaemonPort: 9418,
			LocalFallback: LocalFallbackConfig{
				Enabled:     true,
				MaxJobs:     2,
				WorktreeDir: filepath.Join(home, ".claude-orchestrator", "build-pool", "local"),
			},
			Timeouts: BuildPoolTimeoutConfig{
				JobDefaultSecs:        300,
				HeartbeatIntervalSecs: 30,
				HeartbeatTimeoutSecs:  90, // Allow missing 2 heartbeats before disconnect
			},
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
	cfg.BuildPool.LocalFallback.WorktreeDir = ExpandPath(cfg.BuildPool.LocalFallback.WorktreeDir)

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
	return filepath.Join(home, ".config", "claude-orchestrator", "config.toml")
}

// LocalConfigName is the name of the local config file
const LocalConfigName = ".claude-orchestrator.toml"

// FindLocalConfig searches for a local config file in the current directory
// and parent directories up to the filesystem root.
// Returns the path if found, empty string otherwise.
func FindLocalConfig() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}

	for {
		configPath := filepath.Join(dir, LocalConfigName)
		if _, err := os.Stat(configPath); err == nil {
			return configPath
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root
			break
		}
		dir = parent
	}

	return ""
}

// LoadWithLocalFallback loads config with the following precedence:
// 1. Explicit path (if provided)
// 2. Local config (.claude-orchestrator.toml in current or parent directories)
// 3. Global config (~/.config/claude-orchestrator/config.toml)
func LoadWithLocalFallback(explicitPath string) (*Config, error) {
	if explicitPath != "" {
		return Load(explicitPath)
	}

	// Try local config first
	if localPath := FindLocalConfig(); localPath != "" {
		return Load(localPath)
	}

	// Fall back to global config
	return Load(DefaultConfigPath())
}

// Save writes the configuration to a TOML file
func (c *Config) Save(path string) error {
	// Ensure parent directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := toml.Marshal(c)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}
