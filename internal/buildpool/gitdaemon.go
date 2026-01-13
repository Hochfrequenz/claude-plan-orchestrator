// internal/buildpool/gitdaemon.go
package buildpool

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

// GitDaemonConfig configures the git daemon
type GitDaemonConfig struct {
	Port    int
	BaseDir string
}

// GitDaemon manages a git daemon process
type GitDaemon struct {
	config GitDaemonConfig
	cmd    *exec.Cmd
}

// NewGitDaemon creates a new git daemon manager
func NewGitDaemon(config GitDaemonConfig) *GitDaemon {
	if config.Port == 0 {
		config.Port = 9418
	}
	return &GitDaemon{config: config}
}

// Args returns the command-line arguments for git daemon
func (d *GitDaemon) Args() []string {
	return []string{
		"daemon",
		"--reuseaddr",
		fmt.Sprintf("--port=%d", d.config.Port),
		"--base-path", d.config.BaseDir,
		"--export-all",
		"--verbose",
		d.config.BaseDir,
	}
}

// Start starts the git daemon
func (d *GitDaemon) Start(ctx context.Context) error {
	// Ensure git-daemon-export-ok exists in .git directory
	gitDir := filepath.Join(d.config.BaseDir, ".git")
	if _, err := os.Stat(gitDir); err == nil {
		exportFile := filepath.Join(gitDir, "git-daemon-export-ok")
		if _, err := os.Stat(exportFile); os.IsNotExist(err) {
			if err := os.WriteFile(exportFile, nil, 0644); err != nil {
				return fmt.Errorf("creating git-daemon-export-ok: %w", err)
			}
		}
	}

	d.cmd = exec.CommandContext(ctx, "git", d.Args()...)
	d.cmd.Stdout = os.Stdout
	d.cmd.Stderr = os.Stderr

	if err := d.cmd.Start(); err != nil {
		return fmt.Errorf("starting git daemon: %w", err)
	}

	log.Printf("git daemon started on port %d, serving %s", d.config.Port, d.config.BaseDir)
	return nil
}

// Stop stops the git daemon
func (d *GitDaemon) Stop() error {
	if d.cmd != nil && d.cmd.Process != nil {
		return d.cmd.Process.Kill()
	}
	return nil
}

// Wait waits for the daemon to exit
func (d *GitDaemon) Wait() error {
	if d.cmd != nil {
		return d.cmd.Wait()
	}
	return nil
}
