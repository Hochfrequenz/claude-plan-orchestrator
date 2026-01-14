package buildpool

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/buildprotocol"
)

func TestVerbosityIntegration(t *testing.T) {
	registry := NewRegistry()

	// Create mock embedded worker that returns 60 lines
	embedded := func(job *buildprotocol.JobMessage) *buildprotocol.JobResult {
		var lines []string
		for i := 1; i <= 60; i++ {
			lines = append(lines, "stdout line")
		}
		return &buildprotocol.JobResult{
			JobID:    job.JobID,
			ExitCode: 0,
			Stdout:   strings.Join(lines, "\n") + "\n",
			Stderr:   "warning: something\n",
		}
	}

	dispatcher := NewDispatcher(registry, embedded)
	coord := NewCoordinator(CoordinatorConfig{WebSocketPort: 0}, registry, dispatcher)
	server := NewMCPServer(MCPServerConfig{WorktreePath: "."}, dispatcher, registry)
	server.SetCoordinator(coord)

	t.Run("minimal verbosity", func(t *testing.T) {
		result, err := server.CallTool("run_command", map[string]interface{}{
			"command":   "test",
			"verbosity": "minimal",
		})
		if err != nil {
			t.Fatalf("CallTool: %v", err)
		}
		if result.Stdout != "" {
			t.Error("minimal should have no stdout")
		}
		if result.Stderr == "" {
			t.Error("minimal should have stderr")
		}
	})

	t.Run("normal verbosity truncates to 50 lines", func(t *testing.T) {
		result, err := server.CallTool("run_command", map[string]interface{}{
			"command":   "test",
			"verbosity": "normal",
		})
		if err != nil {
			t.Fatalf("CallTool: %v", err)
		}
		lines := strings.Split(strings.TrimSuffix(result.Stdout, "\n"), "\n")
		if len(lines) != 50 {
			t.Errorf("normal should have 50 lines, got %d", len(lines))
		}
	})

	t.Run("full verbosity returns all 60 lines", func(t *testing.T) {
		result, err := server.CallTool("run_command", map[string]interface{}{
			"command":   "test",
			"verbosity": "full",
		})
		if err != nil {
			t.Fatalf("CallTool: %v", err)
		}
		lines := strings.Split(strings.TrimSuffix(result.Stdout, "\n"), "\n")
		if len(lines) != 60 {
			t.Errorf("full should have 60 lines, got %d", len(lines))
		}
	})

	t.Run("get_job_logs retrieves full logs from coordinator retention", func(t *testing.T) {
		// Simulate a WebSocket worker completing a job by accumulating output
		// and retaining logs in the coordinator (this is how real workers work)
		testJobID := "test-job-retained"

		// Accumulate 60 lines of stdout and some stderr
		var lines []string
		for i := 1; i <= 60; i++ {
			lines = append(lines, "stdout line")
		}
		coord.AccumulateOutput(testJobID, "stdout", strings.Join(lines, "\n")+"\n")
		coord.AccumulateOutput(testJobID, "stderr", "warning: something\n")

		// Retain the logs (this happens when CompleteJob is called in the real flow)
		coord.RetainLogs(testJobID)

		// Retrieve full logs using get_job_logs
		logs, err := server.CallTool("get_job_logs", map[string]interface{}{
			"job_id": testJobID,
		})
		if err != nil {
			t.Fatalf("get_job_logs: %v", err)
		}
		if !strings.Contains(logs.Output, "stdout line") {
			t.Error("get_job_logs should return full stdout")
		}
		if !strings.Contains(logs.Output, "warning: something") {
			t.Error("get_job_logs should return stderr")
		}
	})
}

// TestTUILocalWorkerIntegration tests the full stack as used by the TUI:
// Coordinator + Dispatcher + EmbeddedWorker with a real git repository.
// This simulates the setup from cmd/claude-orch/commands.go when build pool is enabled.
func TestTUILocalWorkerIntegration(t *testing.T) {
	// Create a temporary git repo to simulate the project root
	tmpDir := t.TempDir()
	repoDir := filepath.Join(tmpDir, "project")

	// Initialize git repo
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	cmd := exec.Command("git", "init")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %s: %v", out, err)
	}

	cmd = exec.Command("git", "config", "user.email", "test@test.com")
	cmd.Dir = repoDir
	cmd.Run()
	cmd = exec.Command("git", "config", "user.name", "Test")
	cmd.Dir = repoDir
	cmd.Run()

	// Create a test file and commit
	testFile := filepath.Join(repoDir, "README.md")
	if err := os.WriteFile(testFile, []byte("# Test Project\n"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = repoDir
	cmd.Run()

	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %s: %v", out, err)
	}

	// Add an unpushed file (commit exists locally but not pushed to remote)
	unpushedFile := filepath.Join(repoDir, "UNPUSHED.md")
	if err := os.WriteFile(unpushedFile, []byte("# Unpushed Content\n"), 0644); err != nil {
		t.Fatalf("write unpushed file: %v", err)
	}

	cmd = exec.Command("git", "add", "UNPUSHED.md")
	cmd.Dir = repoDir
	cmd.Run()

	cmd = exec.Command("git", "commit", "-m", "Add unpushed file")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit unpushed: %s: %v", out, err)
	}

	// Create worktree directory for the embedded worker
	worktreeDir := filepath.Join(tmpDir, "worktrees")
	if err := os.MkdirAll(worktreeDir, 0755); err != nil {
		t.Fatalf("mkdir worktrees: %v", err)
	}

	// Set up the full stack as done in TUI command
	registry := NewRegistry()

	// Create real embedded worker (like TUI does)
	embedded := NewEmbeddedWorker(EmbeddedConfig{
		RepoDir:     repoDir,
		WorktreeDir: worktreeDir,
		MaxJobs:     2,
		UseNixShell: false, // Don't require nix for tests
	})

	// Create dispatcher with embedded worker
	dispatcher := NewDispatcher(registry, embedded.Run)

	// Create coordinator (port 0 = don't actually listen)
	coord := NewCoordinator(CoordinatorConfig{WebSocketPort: 0}, registry, dispatcher)

	// Create MCP server pointing at the repo
	server := NewMCPServer(MCPServerConfig{
		WorktreePath: repoDir,
	}, dispatcher, registry)
	server.SetCoordinator(coord)

	t.Run("run command via MCP tool", func(t *testing.T) {
		result, err := server.CallTool("run_command", map[string]interface{}{
			"command":   "cat README.md",
			"verbosity": "full",
		})
		if err != nil {
			t.Fatalf("CallTool: %v", err)
		}
		t.Logf("Result: ExitCode=%d, Output=%q, Stdout=%q, Stderr=%q",
			result.ExitCode, result.Output, result.Stdout, result.Stderr)

		if result.ExitCode != 0 {
			t.Errorf("expected exit code 0, got %d", result.ExitCode)
		}
		if !strings.Contains(result.Output, "# Test Project") {
			t.Errorf("output should contain '# Test Project', got %q", result.Output)
		}
		if !strings.Contains(result.Stdout, "# Test Project") {
			t.Errorf("stdout should contain '# Test Project', got %q", result.Stdout)
		}
	})

	t.Run("run command with minimal verbosity", func(t *testing.T) {
		result, err := server.CallTool("run_command", map[string]interface{}{
			"command":   "echo 'hello from minimal'",
			"verbosity": "minimal",
		})
		if err != nil {
			t.Fatalf("CallTool: %v", err)
		}
		t.Logf("Result: ExitCode=%d, Output=%q, Stdout=%q, Stderr=%q",
			result.ExitCode, result.Output, result.Stdout, result.Stderr)

		if result.ExitCode != 0 {
			t.Errorf("expected exit code 0, got %d", result.ExitCode)
		}
		// Minimal verbosity should not include stdout
		if result.Stdout != "" {
			t.Errorf("minimal verbosity should have empty stdout, got %q", result.Stdout)
		}
	})

	t.Run("run failing command", func(t *testing.T) {
		result, err := server.CallTool("run_command", map[string]interface{}{
			"command":   "exit 42",
			"verbosity": "full",
		})
		if err != nil {
			t.Fatalf("CallTool: %v", err)
		}
		t.Logf("Result: ExitCode=%d, Output=%q", result.ExitCode, result.Output)

		if result.ExitCode != 42 {
			t.Errorf("expected exit code 42, got %d", result.ExitCode)
		}
	})

	t.Run("worker status shows local fallback active", func(t *testing.T) {
		result, err := server.CallTool("worker_status", nil)
		if err != nil {
			t.Fatalf("CallTool: %v", err)
		}
		t.Logf("Worker status: %s", result.Output)

		if !strings.Contains(result.Output, "local_fallback_active") {
			t.Error("worker_status should include local_fallback_active field")
		}
		if !strings.Contains(result.Output, "true") {
			t.Error("local_fallback_active should be true (no remote workers)")
		}
	})

	t.Run("run command that writes to stderr", func(t *testing.T) {
		result, err := server.CallTool("run_command", map[string]interface{}{
			"command":   "echo 'stdout line' && echo 'stderr line' >&2",
			"verbosity": "full",
		})
		if err != nil {
			t.Fatalf("CallTool: %v", err)
		}
		t.Logf("Result: ExitCode=%d, Stdout=%q, Stderr=%q",
			result.ExitCode, result.Stdout, result.Stderr)

		if !strings.Contains(result.Stdout, "stdout line") {
			t.Errorf("stdout should contain 'stdout line', got %q", result.Stdout)
		}
		if !strings.Contains(result.Stderr, "stderr line") {
			t.Errorf("stderr should contain 'stderr line', got %q", result.Stderr)
		}
	})

	t.Run("run command on unpushed changes", func(t *testing.T) {
		// The unpushed file was created during setup (commit exists locally but not pushed)
		// This tests that the local worker can access unpushed commits
		result, err := server.CallTool("run_command", map[string]interface{}{
			"command":   "cat UNPUSHED.md",
			"verbosity": "full",
		})
		if err != nil {
			t.Fatalf("CallTool: %v", err)
		}
		t.Logf("Result: ExitCode=%d, Output=%q", result.ExitCode, result.Output)

		if result.ExitCode != 0 {
			t.Errorf("expected exit code 0, got %d", result.ExitCode)
		}
		if !strings.Contains(result.Output, "# Unpushed Content") {
			t.Errorf("should read unpushed file content, got %q", result.Output)
		}
	})
}
