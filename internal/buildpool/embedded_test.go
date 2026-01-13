// internal/buildpool/embedded_test.go
package buildpool

import (
	"os"
	"os/exec"
	"path/filepath"
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
		cmd.Run()
	}

	os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Test"), 0644)
	cmd := exec.Command("git", "add", ".")
	cmd.Dir = repoDir
	cmd.Run()
	cmd = exec.Command("git", "commit", "-m", "init")
	cmd.Dir = repoDir
	cmd.Run()

	// Get commit
	cmd = exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoDir
	out, _ := cmd.Output()
	commit := string(out[:len(out)-1])

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
