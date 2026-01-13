// internal/buildworker/executor_test.go
package buildworker

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func setupTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	}

	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v failed: %s", args, out)
		}
	}

	// Create initial commit
	readme := filepath.Join(dir, "README.md")
	os.WriteFile(readme, []byte("# Test"), 0644)

	cmd := exec.Command("git", "add", ".")
	cmd.Dir = dir
	cmd.Run()

	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = dir
	cmd.Run()

	return dir
}

func TestExecutor_RunJob_SimpleCommand(t *testing.T) {
	repoDir := setupTestRepo(t)
	worktreeDir := t.TempDir()

	executor := NewExecutor(ExecutorConfig{
		GitCacheDir: repoDir,
		WorktreeDir: worktreeDir,
		UseNixShell: false, // Skip nix for basic tests
	})

	// Get HEAD commit
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoDir
	commitBytes, _ := cmd.Output()
	commit := string(commitBytes[:len(commitBytes)-1])

	ctx := context.Background()
	result, err := executor.RunJob(ctx, Job{
		ID:      "test-job-1",
		Repo:    repoDir, // Use local path instead of git://
		Commit:  commit,
		Command: "echo hello",
	}, nil)

	if err != nil {
		t.Fatalf("RunJob failed: %v", err)
	}

	if result.ExitCode != 0 {
		t.Errorf("got exit code %d, want 0", result.ExitCode)
	}

	if result.Output != "hello\n" {
		t.Errorf("got output %q, want %q", result.Output, "hello\n")
	}
}

func TestExecutor_RunJob_NonZeroExitCode(t *testing.T) {
	repoDir := setupTestRepo(t)
	worktreeDir := t.TempDir()

	executor := NewExecutor(ExecutorConfig{
		GitCacheDir: repoDir,
		WorktreeDir: worktreeDir,
		UseNixShell: false,
	})

	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoDir
	commitBytes, _ := cmd.Output()
	commit := string(commitBytes[:len(commitBytes)-1])

	ctx := context.Background()
	result, err := executor.RunJob(ctx, Job{
		ID:      "test-job-2",
		Repo:    repoDir,
		Commit:  commit,
		Command: "exit 42",
	}, nil)

	if err != nil {
		t.Fatalf("RunJob failed: %v", err)
	}

	if result.ExitCode != 42 {
		t.Errorf("got exit code %d, want 42", result.ExitCode)
	}
}

func TestExecutor_RunJob_WithEnvVars(t *testing.T) {
	repoDir := setupTestRepo(t)
	worktreeDir := t.TempDir()

	executor := NewExecutor(ExecutorConfig{
		GitCacheDir: repoDir,
		WorktreeDir: worktreeDir,
		UseNixShell: false,
	})

	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoDir
	commitBytes, _ := cmd.Output()
	commit := string(commitBytes[:len(commitBytes)-1])

	ctx := context.Background()
	result, err := executor.RunJob(ctx, Job{
		ID:      "test-job-3",
		Repo:    repoDir,
		Commit:  commit,
		Command: "echo $TEST_VAR",
		Env:     map[string]string{"TEST_VAR": "test-value"},
	}, nil)

	if err != nil {
		t.Fatalf("RunJob failed: %v", err)
	}

	if result.Output != "test-value\n" {
		t.Errorf("got output %q, want %q", result.Output, "test-value\n")
	}
}

func TestExecutor_RunJob_WithOutputCallback(t *testing.T) {
	repoDir := setupTestRepo(t)
	worktreeDir := t.TempDir()

	executor := NewExecutor(ExecutorConfig{
		GitCacheDir: repoDir,
		WorktreeDir: worktreeDir,
		UseNixShell: false,
	})

	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoDir
	commitBytes, _ := cmd.Output()
	commit := string(commitBytes[:len(commitBytes)-1])

	var callbackOutput []struct {
		stream string
		data   string
	}

	ctx := context.Background()
	result, err := executor.RunJob(ctx, Job{
		ID:      "test-job-4",
		Repo:    repoDir,
		Commit:  commit,
		Command: "echo line1; echo line2",
	}, func(stream, data string) {
		callbackOutput = append(callbackOutput, struct {
			stream string
			data   string
		}{stream, data})
	})

	if err != nil {
		t.Fatalf("RunJob failed: %v", err)
	}

	if result.ExitCode != 0 {
		t.Errorf("got exit code %d, want 0", result.ExitCode)
	}

	if len(callbackOutput) != 2 {
		t.Errorf("got %d callback calls, want 2", len(callbackOutput))
	}

	// Verify callback received the lines
	if len(callbackOutput) >= 2 {
		if callbackOutput[0].stream != "stdout" {
			t.Errorf("got stream %q, want %q", callbackOutput[0].stream, "stdout")
		}
		if callbackOutput[0].data != "line1\n" {
			t.Errorf("got data %q, want %q", callbackOutput[0].data, "line1\n")
		}
	}
}

func TestExecutor_RunJob_StderrOutput(t *testing.T) {
	repoDir := setupTestRepo(t)
	worktreeDir := t.TempDir()

	executor := NewExecutor(ExecutorConfig{
		GitCacheDir: repoDir,
		WorktreeDir: worktreeDir,
		UseNixShell: false,
	})

	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoDir
	commitBytes, _ := cmd.Output()
	commit := string(commitBytes[:len(commitBytes)-1])

	var streams []string
	ctx := context.Background()
	result, err := executor.RunJob(ctx, Job{
		ID:      "test-job-5",
		Repo:    repoDir,
		Commit:  commit,
		Command: "echo stderr-message >&2",
	}, func(stream, data string) {
		streams = append(streams, stream)
	})

	if err != nil {
		t.Fatalf("RunJob failed: %v", err)
	}

	if result.ExitCode != 0 {
		t.Errorf("got exit code %d, want 0", result.ExitCode)
	}

	// Stderr should be captured
	if result.Output != "stderr-message\n" {
		t.Errorf("got output %q, want %q", result.Output, "stderr-message\n")
	}

	// Callback should have been called with stderr stream
	foundStderr := false
	for _, s := range streams {
		if s == "stderr" {
			foundStderr = true
			break
		}
	}
	if !foundStderr {
		t.Error("expected stderr callback, got none")
	}
}

func TestExecutor_RunJob_WorktreeCleanup(t *testing.T) {
	repoDir := setupTestRepo(t)
	worktreeDir := t.TempDir()

	executor := NewExecutor(ExecutorConfig{
		GitCacheDir: repoDir,
		WorktreeDir: worktreeDir,
		UseNixShell: false,
	})

	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoDir
	commitBytes, _ := cmd.Output()
	commit := string(commitBytes[:len(commitBytes)-1])

	ctx := context.Background()
	_, err := executor.RunJob(ctx, Job{
		ID:      "test-job-6",
		Repo:    repoDir,
		Commit:  commit,
		Command: "echo done",
	}, nil)

	if err != nil {
		t.Fatalf("RunJob failed: %v", err)
	}

	// Verify worktree was cleaned up
	entries, _ := os.ReadDir(worktreeDir)
	if len(entries) != 0 {
		t.Errorf("worktree not cleaned up, found %d entries", len(entries))
	}
}

func TestExecutor_RunJob_WorksWithRepoFiles(t *testing.T) {
	repoDir := setupTestRepo(t)
	worktreeDir := t.TempDir()

	executor := NewExecutor(ExecutorConfig{
		GitCacheDir: repoDir,
		WorktreeDir: worktreeDir,
		UseNixShell: false,
	})

	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoDir
	commitBytes, _ := cmd.Output()
	commit := string(commitBytes[:len(commitBytes)-1])

	ctx := context.Background()
	result, err := executor.RunJob(ctx, Job{
		ID:      "test-job-7",
		Repo:    repoDir,
		Commit:  commit,
		Command: "cat README.md",
	}, nil)

	if err != nil {
		t.Fatalf("RunJob failed: %v", err)
	}

	if result.Output != "# Test\n" {
		t.Errorf("got output %q, want %q", result.Output, "# Test\n")
	}
}

func TestExecutor_RunJob_DurationTracked(t *testing.T) {
	repoDir := setupTestRepo(t)
	worktreeDir := t.TempDir()

	executor := NewExecutor(ExecutorConfig{
		GitCacheDir: repoDir,
		WorktreeDir: worktreeDir,
		UseNixShell: false,
	})

	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoDir
	commitBytes, _ := cmd.Output()
	commit := string(commitBytes[:len(commitBytes)-1])

	ctx := context.Background()
	result, err := executor.RunJob(ctx, Job{
		ID:      "test-job-8",
		Repo:    repoDir,
		Commit:  commit,
		Command: "sleep 0.1",
	}, nil)

	if err != nil {
		t.Fatalf("RunJob failed: %v", err)
	}

	if result.DurationSecs < 0.1 {
		t.Errorf("got duration %f, want >= 0.1", result.DurationSecs)
	}
}

func TestExecutor_RunJob_JobIDPreserved(t *testing.T) {
	repoDir := setupTestRepo(t)
	worktreeDir := t.TempDir()

	executor := NewExecutor(ExecutorConfig{
		GitCacheDir: repoDir,
		WorktreeDir: worktreeDir,
		UseNixShell: false,
	})

	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoDir
	commitBytes, _ := cmd.Output()
	commit := string(commitBytes[:len(commitBytes)-1])

	ctx := context.Background()
	result, err := executor.RunJob(ctx, Job{
		ID:      "my-unique-job-id",
		Repo:    repoDir,
		Commit:  commit,
		Command: "echo test",
	}, nil)

	if err != nil {
		t.Fatalf("RunJob failed: %v", err)
	}

	if result.JobID != "my-unique-job-id" {
		t.Errorf("got job ID %q, want %q", result.JobID, "my-unique-job-id")
	}
}
