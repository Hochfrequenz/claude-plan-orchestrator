// internal/buildpool/gitdaemon_test.go
package buildpool

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestGitDaemon_Config(t *testing.T) {
	repoDir := t.TempDir()

	// Initialize a git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("git init failed: %v", err)
	}

	// Enable git-daemon-export
	exportFile := filepath.Join(repoDir, ".git", "git-daemon-export-ok")
	if err := os.WriteFile(exportFile, nil, 0644); err != nil {
		t.Fatalf("create export file: %v", err)
	}

	daemon := NewGitDaemon(GitDaemonConfig{
		Port:    9418,
		BaseDir: repoDir,
	})

	args := daemon.Args()

	// Check expected arguments
	hasPort := false
	hasBaseDir := false
	expectedBasePath := "--base-path=" + repoDir
	for _, arg := range args {
		if arg == "--port=9418" {
			hasPort = true
		}
		if arg == expectedBasePath {
			hasBaseDir = true
		}
	}

	if !hasPort {
		t.Error("missing --port argument")
	}
	if !hasBaseDir {
		t.Error("missing --base-path argument")
	}
}

func TestGitDaemon_DefaultPort(t *testing.T) {
	daemon := NewGitDaemon(GitDaemonConfig{
		BaseDir: "/tmp/test",
	})

	args := daemon.Args()

	hasDefaultPort := false
	for _, arg := range args {
		if arg == "--port=9418" {
			hasDefaultPort = true
			break
		}
	}

	if !hasDefaultPort {
		t.Error("expected default port 9418 when port not specified")
	}
}

func TestGitDaemon_Args(t *testing.T) {
	// Test without debug (no --verbose)
	daemon := NewGitDaemon(GitDaemonConfig{
		Port:    9999,
		BaseDir: "/test/repo",
	})

	args := daemon.Args()

	expected := []string{
		"daemon",
		"--reuseaddr",
		"--port=9999",
		"--base-path=/test/repo",
		"--export-all",
		"/test/repo",
	}

	if len(args) != len(expected) {
		t.Errorf("expected %d args, got %d", len(expected), len(args))
	}

	for i, exp := range expected {
		if i >= len(args) {
			break
		}
		if args[i] != exp {
			t.Errorf("arg[%d]: expected %q, got %q", i, exp, args[i])
		}
	}

	// Test with debug (includes --verbose)
	daemonDebug := NewGitDaemon(GitDaemonConfig{
		Port:    9999,
		BaseDir: "/test/repo",
		Debug:   true,
	})

	argsDebug := daemonDebug.Args()

	expectedDebug := []string{
		"daemon",
		"--reuseaddr",
		"--port=9999",
		"--base-path=/test/repo",
		"--export-all",
		"--verbose",
		"/test/repo",
	}

	if len(argsDebug) != len(expectedDebug) {
		t.Errorf("debug: expected %d args, got %d", len(expectedDebug), len(argsDebug))
	}

	for i, exp := range expectedDebug {
		if i >= len(argsDebug) {
			break
		}
		if argsDebug[i] != exp {
			t.Errorf("debug arg[%d]: expected %q, got %q", i, exp, argsDebug[i])
		}
	}
}

func TestGitDaemon_StartStop(t *testing.T) {
	// Skip if git is not available
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoDir := t.TempDir()

	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("git init failed: %v", err)
	}

	// Use a high port to avoid conflicts
	daemon := NewGitDaemon(GitDaemonConfig{
		Port:    19418, // High port to avoid conflicts
		BaseDir: repoDir,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start daemon
	if err := daemon.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Give it a moment to start
	time.Sleep(100 * time.Millisecond)

	// Stop daemon
	if err := daemon.Stop(); err != nil {
		t.Errorf("Stop failed: %v", err)
	}
}

func TestGitDaemon_ActuallyListens(t *testing.T) {
	// Skip if git is not available
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoDir := t.TempDir()

	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("git init failed: %v", err)
	}

	// Use a unique high port to avoid conflicts
	port := 29418
	daemon := NewGitDaemon(GitDaemonConfig{
		Port:    port,
		BaseDir: repoDir,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer daemon.Stop()

	// Start daemon
	if err := daemon.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Give daemon time to bind to port
	time.Sleep(200 * time.Millisecond)

	// Verify port is actually listening by attempting a TCP connection
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 2*time.Second)
	if err != nil {
		t.Fatalf("daemon not listening on port %d: %v", port, err)
	}
	conn.Close()
}

func TestGitDaemon_ListenAddr(t *testing.T) {
	tests := []struct {
		name       string
		listenAddr string
		wantInArgs bool
		wantValue  string
	}{
		{"default", "", false, ""},
		{"localhost", "127.0.0.1", true, "--listen=127.0.0.1"},
		{"any", "0.0.0.0", true, "--listen=0.0.0.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := NewGitDaemon(GitDaemonConfig{
				Port:       9418,
				BaseDir:    "/tmp",
				ListenAddr: tt.listenAddr,
			})

			args := d.Args()
			found := false
			for _, arg := range args {
				if arg == tt.wantValue {
					found = true
					break
				}
			}

			if tt.wantInArgs && !found {
				t.Errorf("Args() missing %q", tt.wantValue)
			}
			if !tt.wantInArgs && found {
				t.Errorf("Args() unexpectedly contains listen flag")
			}
		})
	}
}
