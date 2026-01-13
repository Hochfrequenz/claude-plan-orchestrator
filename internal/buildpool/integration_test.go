package buildpool

import (
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
