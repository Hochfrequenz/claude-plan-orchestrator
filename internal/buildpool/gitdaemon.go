// internal/buildpool/gitdaemon.go
package buildpool

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// GitDaemonConfig configures the git daemon
type GitDaemonConfig struct {
	Port       int
	BaseDir    string
	ListenAddr string // Optional: address to listen on (e.g., "127.0.0.1" for local only)
}

// GitDaemon manages a git daemon process
type GitDaemon struct {
	config GitDaemonConfig
	cmd    *exec.Cmd
	mu     sync.Mutex
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
	args := []string{
		"daemon",
		"--reuseaddr",
		fmt.Sprintf("--port=%d", d.config.Port),
		fmt.Sprintf("--base-path=%s", d.config.BaseDir),
		"--export-all",
		"--verbose",
	}

	if d.config.ListenAddr != "" {
		args = append(args, fmt.Sprintf("--listen=%s", d.config.ListenAddr))
	}

	args = append(args, d.config.BaseDir)
	return args
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

	d.mu.Lock()
	defer d.mu.Unlock()

	d.cmd = exec.CommandContext(ctx, "git", d.Args()...)
	d.cmd.Stdout = os.Stdout
	d.cmd.Stderr = os.Stderr

	if err := d.cmd.Start(); err != nil {
		return fmt.Errorf("starting git daemon: %w", err)
	}

	// Give the daemon a moment to start and verify it's still running
	time.Sleep(100 * time.Millisecond)

	// Check if process exited immediately (indicates startup failure)
	if d.cmd.Process != nil {
		// Try to check if process is still alive
		if err := d.cmd.Process.Signal(syscall.Signal(0)); err != nil {
			// Process died - try to get exit status
			d.cmd.Wait()
			return fmt.Errorf("git daemon exited immediately after start")
		}
	}

	log.Printf("git daemon started on port %d, serving %s", d.config.Port, d.config.BaseDir)
	return nil
}

// Stop stops the git daemon gracefully
func (d *GitDaemon) Stop() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.cmd == nil || d.cmd.Process == nil {
		return nil
	}

	// Try graceful shutdown first
	d.cmd.Process.Signal(syscall.SIGTERM)

	// Wait briefly for graceful exit
	done := make(chan error, 1)
	go func() {
		done <- d.cmd.Wait()
	}()

	select {
	case <-done:
		return nil
	case <-time.After(5 * time.Second):
		// Force kill if not responding
		return d.cmd.Process.Kill()
	}
}

// Wait waits for the daemon to exit
func (d *GitDaemon) Wait() error {
	d.mu.Lock()
	cmd := d.cmd
	d.mu.Unlock()

	if cmd != nil {
		return cmd.Wait()
	}
	return nil
}
