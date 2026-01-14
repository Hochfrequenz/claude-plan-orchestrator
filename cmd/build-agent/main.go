// cmd/build-agent/main.go
package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/buildworker"
	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
)

var (
	configPath string
	serverURL  string
	workerID   string
	maxJobs    int
	debug      bool
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "build-agent",
		Short: "Build worker agent that connects to a coordinator",
		RunE:  run,
	}

	rootCmd.Flags().StringVar(&configPath, "config", "", "Path to config file")
	rootCmd.Flags().StringVar(&serverURL, "server", "", "Coordinator WebSocket URL")
	rootCmd.Flags().StringVar(&workerID, "id", "", "Worker ID")
	rootCmd.Flags().IntVar(&maxJobs, "jobs", 4, "Maximum concurrent jobs")
	rootCmd.Flags().BoolVar(&debug, "debug", false, "Enable verbose logging for heartbeat diagnostics")

	// Add service management subcommand
	rootCmd.AddCommand(newServiceCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// Config defines the build-agent configuration file format
type Config struct {
	Server struct {
		URL string `toml:"url"`
	} `toml:"server"`
	Worker struct {
		ID      string `toml:"id"`
		MaxJobs int    `toml:"max_jobs"`
	} `toml:"worker"`
	Storage struct {
		GitCacheDir string `toml:"git_cache_dir"`
		WorktreeDir string `toml:"worktree_dir"`
	} `toml:"storage"`
	Nix struct {
		PrewarmPackages []string `toml:"prewarm_packages"`
	} `toml:"nix"`
}

// Default config file locations (checked in order)
var defaultConfigPaths = []string{
	"/etc/build-agent/config.toml",
	"/etc/build-agent.toml",
}

func run(cmd *cobra.Command, args []string) error {
	// Check prerequisites
	if err := checkPrerequisites(); err != nil {
		return err
	}

	var cfg Config

	// Determine config file path
	cfgPath := configPath
	if cfgPath == "" {
		// Try default locations
		for _, p := range defaultConfigPaths {
			if _, err := os.Stat(p); err == nil {
				cfgPath = p
				break
			}
		}
	}

	// Load config file if found
	if cfgPath != "" {
		data, err := os.ReadFile(cfgPath)
		if err != nil {
			return fmt.Errorf("reading config %s: %w", cfgPath, err)
		}
		if err := toml.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("parsing config %s: %w", cfgPath, err)
		}
		fmt.Printf("Loaded config from %s\n", cfgPath)
	}

	// CLI flags override config (only if explicitly set)
	if serverURL != "" {
		cfg.Server.URL = serverURL
	}
	if workerID != "" {
		cfg.Worker.ID = workerID
	}
	if cmd.Flags().Changed("jobs") {
		cfg.Worker.MaxJobs = maxJobs
	}

	// Defaults
	if cfg.Worker.MaxJobs == 0 {
		cfg.Worker.MaxJobs = 4
	}
	if cfg.Worker.ID == "" {
		hostname, _ := os.Hostname()
		cfg.Worker.ID = hostname
	}
	if cfg.Storage.GitCacheDir == "" {
		cfg.Storage.GitCacheDir = "/var/cache/build-agent/repos"
	}
	if cfg.Storage.WorktreeDir == "" {
		cfg.Storage.WorktreeDir = "/tmp/build-agent/jobs"
	}

	// Prewarm nix store with common packages
	if len(cfg.Nix.PrewarmPackages) > 0 {
		if err := prewarmNix(cfg.Nix.PrewarmPackages); err != nil {
			// Log warning but don't fail - prewarm is best-effort
			fmt.Printf("Warning: nix prewarm failed: %v\n", err)
		}
	}

	// Create worker
	worker, err := buildworker.NewWorker(buildworker.WorkerConfig{
		ServerURL:   cfg.Server.URL,
		WorkerID:    cfg.Worker.ID,
		MaxJobs:     cfg.Worker.MaxJobs,
		GitCacheDir: cfg.Storage.GitCacheDir,
		WorktreeDir: cfg.Storage.WorktreeDir,
		UseNixShell: true,
		Debug:       debug,
	})
	if err != nil {
		return fmt.Errorf("creating worker: %w", err)
	}

	// Handle shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		fmt.Println("\nShutting down...")
		worker.Stop()
	}()

	fmt.Printf("Starting worker %s connecting to %s (max_jobs=%d)...\n",
		cfg.Worker.ID, cfg.Server.URL, cfg.Worker.MaxJobs)

	// Run with automatic reconnection (blocks until stopped)
	return worker.RunWithReconnect()
}

func checkPrerequisites() error {
	// Check for nix (required for reproducible builds)
	if _, err := exec.LookPath("nix"); err != nil {
		return fmt.Errorf(`nix is required but not found in PATH

Build agents run jobs inside 'nix develop' for reproducible builds.

Install Nix:
  # Recommended: Determinate Nix Installer
  curl --proto '=https' --tlsv1.2 -sSf -L https://install.determinate.systems/nix | sh -s -- install

  # Or official installer
  sh <(curl -L https://nixos.org/nix/install) --daemon

After installation, ensure nix is in your PATH:
  . /nix/var/nix/profiles/default/etc/profile.d/nix-daemon.sh`)
	}

	// Check for git (required for cloning repos)
	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("git is required but not found in PATH")
	}

	return nil
}

// prewarmNix downloads packages into the nix store to speed up subsequent nix develop calls
func prewarmNix(packages []string) error {
	if len(packages) == 0 {
		return nil
	}

	fmt.Printf("Prewarming nix store with %d packages...\n", len(packages))

	// Build args: nix build <pkg1> <pkg2> ... --no-link
	args := append([]string{"build"}, packages...)
	args = append(args, "--no-link")

	cmd := exec.Command("nix", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("nix build failed: %w", err)
	}

	fmt.Println("Nix store prewarm complete")
	return nil
}
