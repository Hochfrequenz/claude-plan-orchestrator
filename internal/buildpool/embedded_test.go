// internal/buildpool/embedded_test.go
package buildpool

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/buildprotocol"
)

func TestEmbeddedWorker_RunJob(t *testing.T) {
	// Create a test repo
	repoDir := t.TempDir()
	worktreeDir := t.TempDir()

	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = repoDir
		if err := cmd.Run(); err != nil {
			t.Fatalf("setup command %v failed: %v", args, err)
		}
	}

	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Test"), 0644); err != nil {
		t.Fatalf("failed to write README.md: %v", err)
	}
	cmd := exec.Command("git", "add", ".")
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("git add failed: %v", err)
	}
	cmd = exec.Command("git", "commit", "-m", "init")
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("git commit failed: %v", err)
	}

	// Get commit
	cmd = exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD failed: %v", err)
	}
	commit := strings.TrimSpace(string(out))

	embedded := NewEmbeddedWorker(EmbeddedConfig{
		RepoDir:     repoDir,
		WorktreeDir: worktreeDir,
		MaxJobs:     2,
		UseNixShell: false,
	})

	job := &buildprotocol.JobMessage{
		JobID:   "test-job",
		Repo:    repoDir,
		Commit:  commit,
		Command: "echo embedded-test",
	}

	result := embedded.Run(job)

	if result.ExitCode != 0 {
		t.Errorf("got exit code %d, want 0", result.ExitCode)
	}
	if result.Output != "embedded-test\n" {
		t.Errorf("got output %q, want %q", result.Output, "embedded-test\n")
	}
}

func TestEmbeddedWorker_PoolExhaustion(t *testing.T) {
	// Create a test repo
	repoDir := t.TempDir()
	worktreeDir := t.TempDir()

	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = repoDir
		if err := cmd.Run(); err != nil {
			t.Fatalf("setup command %v failed: %v", args, err)
		}
	}

	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Test"), 0644); err != nil {
		t.Fatalf("failed to write README.md: %v", err)
	}
	cmd := exec.Command("git", "add", ".")
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("git add failed: %v", err)
	}
	cmd = exec.Command("git", "commit", "-m", "init")
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("git commit failed: %v", err)
	}

	// Get commit
	cmd = exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD failed: %v", err)
	}
	commit := strings.TrimSpace(string(out))

	// Create worker with MaxJobs=1
	embedded := NewEmbeddedWorker(EmbeddedConfig{
		RepoDir:     repoDir,
		WorktreeDir: worktreeDir,
		MaxJobs:     1,
		UseNixShell: false,
	})

	// Start a long-running job that holds the only slot
	started := make(chan struct{})
	done := make(chan *buildprotocol.JobResult)
	go func() {
		close(started)
		result := embedded.Run(&buildprotocol.JobMessage{
			JobID:   "long-job",
			Repo:    repoDir,
			Commit:  commit,
			Command: "sleep 2",
		})
		done <- result
	}()

	// Wait for the first job to start and acquire the slot
	<-started
	// Give a moment for the goroutine to acquire the pool slot
	// (the job will block on git worktree setup anyway)

	// Try to run another job - should fail with no slots available
	result := embedded.Run(&buildprotocol.JobMessage{
		JobID:   "exhaustion-test",
		Repo:    repoDir,
		Commit:  commit,
		Command: "echo should-not-run",
	})

	if result.ExitCode != 1 {
		t.Errorf("got exit code %d, want 1 (no slots)", result.ExitCode)
	}
	if !strings.Contains(result.Output, "no slots available") {
		t.Errorf("expected 'no slots available' in output, got: %q", result.Output)
	}

	// Wait for the long job to complete
	<-done
}

func TestEmbeddedWorker_ErrorHandling(t *testing.T) {
	// Create temp directories for the worker (no git repo needed for direct execution)
	tempDir := t.TempDir()
	worktreeDir := filepath.Join(tempDir, "worktrees")
	if err := os.MkdirAll(worktreeDir, 0755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}

	worker := NewEmbeddedWorker(EmbeddedConfig{
		RepoDir:     filepath.Join(tempDir, "repos"),
		WorktreeDir: worktreeDir,
		MaxJobs:     1,
		UseNixShell: false,
	})

	tests := []struct {
		name       string
		command    string
		wantExit   int
		wantStderr bool
		wantOutput bool
	}{
		{
			name:       "exit_42_with_stderr",
			command:    "echo 'stdout line' && echo 'error message' >&2 && exit 42",
			wantExit:   42,
			wantStderr: true,
			wantOutput: true,
		},
		{
			name:       "stderr_only",
			command:    "echo 'stderr message' >&2 && exit 1",
			wantExit:   1,
			wantStderr: true,
			wantOutput: true,
		},
		{
			name:       "bad_command",
			command:    "/nonexistent_command_12345",
			wantExit:   127, // Command not found
			wantStderr: true,
			wantOutput: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := &buildprotocol.JobMessage{
				JobID:   "test-" + tt.name,
				Repo:    "", // No repo - direct execution
				Commit:  "",
				Command: tt.command,
				Timeout: 30,
			}

			result := worker.Run(job)

			t.Logf("Result: ExitCode=%d, Output=%q, Stdout=%q, Stderr=%q",
				result.ExitCode, result.Output, result.Stdout, result.Stderr)

			if result.ExitCode != tt.wantExit {
				t.Errorf("ExitCode = %d, want %d", result.ExitCode, tt.wantExit)
			}

			if tt.wantStderr && result.Stderr == "" {
				t.Errorf("Stderr is empty, want non-empty")
			}

			if tt.wantOutput && result.Output == "" {
				t.Errorf("Output is empty, want non-empty")
			}
		})
	}
}

func TestEmbeddedWorker_ExecutorError(t *testing.T) {
	// Test what happens when executor returns an error (not just non-zero exit)
	// This triggers when worktree creation fails (e.g., non-existent git remote)
	tempDir := t.TempDir()
	worktreeDir := filepath.Join(tempDir, "worktrees")
	if err := os.MkdirAll(worktreeDir, 0755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}

	worker := NewEmbeddedWorker(EmbeddedConfig{
		RepoDir:     filepath.Join(tempDir, "repos"),
		WorktreeDir: worktreeDir,
		MaxJobs:     1,
		UseNixShell: false,
	})

	// Try to fetch from a non-existent git remote - this should cause an executor error
	job := &buildprotocol.JobMessage{
		JobID:   "test-executor-error",
		Repo:    "git://nonexistent-host-12345.invalid/repo.git",
		Commit:  "abc123",
		Command: "echo hello",
		Timeout: 5, // Short timeout
	}

	result := worker.Run(job)

	t.Logf("Executor error result: ExitCode=%d, Output=%q, Stderr=%q",
		result.ExitCode, result.Output, result.Stderr)

	// Should have ExitCode -1 (our error indicator)
	if result.ExitCode != -1 {
		t.Errorf("ExitCode = %d, want -1 for executor error", result.ExitCode)
	}

	// Should have error message in Output
	if result.Output == "" {
		t.Errorf("Output is empty, want error message")
	}

	// Should have error message in Stderr (for verbosity filtering)
	if result.Stderr == "" {
		t.Errorf("Stderr is empty, want error message for verbosity filtering")
	}
}
