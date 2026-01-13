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
