// internal/buildpool/dispatcher_test.go
package buildpool

import (
	"testing"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/buildprotocol"
)

func TestDispatcher_SubmitJob(t *testing.T) {
	reg := NewRegistry()
	disp := NewDispatcher(reg, nil) // nil embedded worker for now

	job := &buildprotocol.JobMessage{
		JobID:   "job-1",
		Repo:    "git://localhost/repo",
		Commit:  "abc123",
		Command: "cargo test",
	}

	// With no workers, should queue the job
	resultCh := disp.Submit(job)

	// Check job is in queue
	if disp.QueueLength() != 1 {
		t.Errorf("got queue length=%d, want 1", disp.QueueLength())
	}

	// Result channel should not have anything yet
	select {
	case <-resultCh:
		t.Error("should not have result yet")
	default:
		// Expected
	}
}

func TestDispatcher_DispatchToWorker(t *testing.T) {
	reg := NewRegistry()

	// Track what job was sent to worker
	var sentJob *buildprotocol.JobMessage
	mockWorker := &ConnectedWorker{
		ID:      "worker-1",
		MaxJobs: 4,
		Slots:   2,
	}
	reg.Register(mockWorker)

	disp := NewDispatcher(reg, nil)
	disp.SetSendFunc(func(w *ConnectedWorker, job *buildprotocol.JobMessage) error {
		sentJob = job
		return nil
	})

	job := &buildprotocol.JobMessage{
		JobID:   "job-1",
		Repo:    "git://localhost/repo",
		Commit:  "abc123",
		Command: "cargo test",
	}

	disp.Submit(job)

	// Trigger dispatch
	disp.TryDispatch()

	if sentJob == nil {
		t.Fatal("job was not dispatched")
	}
	if sentJob.JobID != "job-1" {
		t.Errorf("got job ID=%s, want job-1", sentJob.JobID)
	}
}

func TestDispatcher_EmbeddedWorkerUsesLocalRepoPath(t *testing.T) {
	reg := NewRegistry()

	// Track what repo was passed to embedded worker
	var receivedRepo string
	embedded := func(job *buildprotocol.JobMessage) *buildprotocol.JobResult {
		receivedRepo = job.Repo
		return &buildprotocol.JobResult{JobID: job.JobID, ExitCode: 0}
	}

	disp := NewDispatcher(reg, embedded)
	disp.SetLocalRepoPath("/home/user/project") // Set local path

	job := &buildprotocol.JobMessage{
		JobID:   "job-1",
		Repo:    "https://github.com/user/project.git", // Remote URL
		Commit:  "abc123",
		Command: "cargo test",
	}

	resultCh := disp.Submit(job)
	disp.TryDispatch()

	// Wait for result
	result := <-resultCh

	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}

	// Verify embedded worker received local path, not remote URL
	if receivedRepo != "/home/user/project" {
		t.Errorf("embedded worker got repo=%q, want /home/user/project", receivedRepo)
	}
}

func TestDispatcher_EmbeddedWorkerFallsBackToRemoteURL(t *testing.T) {
	reg := NewRegistry()

	// Track what repo was passed to embedded worker
	var receivedRepo string
	embedded := func(job *buildprotocol.JobMessage) *buildprotocol.JobResult {
		receivedRepo = job.Repo
		return &buildprotocol.JobResult{JobID: job.JobID, ExitCode: 0}
	}

	disp := NewDispatcher(reg, embedded)
	// Don't set local repo path - should use remote URL

	job := &buildprotocol.JobMessage{
		JobID:   "job-1",
		Repo:    "https://github.com/user/project.git",
		Commit:  "abc123",
		Command: "cargo test",
	}

	resultCh := disp.Submit(job)
	disp.TryDispatch()

	// Wait for result
	<-resultCh

	// Verify embedded worker received remote URL when no local path set
	if receivedRepo != "https://github.com/user/project.git" {
		t.Errorf("embedded worker got repo=%q, want https://github.com/user/project.git", receivedRepo)
	}
}

func TestDispatcher_Cancel(t *testing.T) {
	registry := NewRegistry()
	dispatcher := NewDispatcher(registry, nil)

	var cancelledJobs []string
	dispatcher.SetCancelFunc(func(workerID, jobID string) error {
		cancelledJobs = append(cancelledJobs, jobID)
		return nil
	})

	// Submit a job
	job := &buildprotocol.JobMessage{JobID: "job-1"}
	dispatcher.Submit(job)

	// Simulate assignment to a worker
	dispatcher.mu.Lock()
	if pj, ok := dispatcher.pending["job-1"]; ok {
		pj.WorkerID = "worker-1"
	}
	dispatcher.mu.Unlock()

	// Cancel the job
	err := dispatcher.Cancel("job-1")
	if err != nil {
		t.Errorf("Cancel: %v", err)
	}

	if len(cancelledJobs) != 1 || cancelledJobs[0] != "job-1" {
		t.Errorf("cancelledJobs = %v, want [job-1]", cancelledJobs)
	}
}

func TestDispatcher_VerbosityFilterPreservesErrorMessages(t *testing.T) {
	// Test that verbosity filtering preserves error messages
	tests := []struct {
		name      string
		verbosity string
		input     *buildprotocol.JobResult
		wantError bool // should Output contain error message?
	}{
		{
			name:      "minimal_with_stderr_error",
			verbosity: buildprotocol.VerbosityMinimal,
			input: &buildprotocol.JobResult{
				JobID:    "test-1",
				ExitCode: -1,
				Stderr:   "embedded worker error: something went wrong",
				Output:   "embedded worker error: something went wrong",
			},
			wantError: true, // Should preserve error message
		},
		{
			name:      "minimal_with_output_only",
			verbosity: buildprotocol.VerbosityMinimal,
			input: &buildprotocol.JobResult{
				JobID:    "test-2",
				ExitCode: -1,
				Output:   "embedded worker error: something went wrong",
				// Stderr not set - tests fallback
			},
			wantError: true, // Should use Output fallback
		},
		{
			name:      "normal_with_error",
			verbosity: buildprotocol.VerbosityNormal,
			input: &buildprotocol.JobResult{
				JobID:    "test-3",
				ExitCode: -1,
				Stderr:   "error message",
				Stdout:   "some stdout output\nmore lines\n",
				Output:   "some stdout output\nmore lines\nerror message",
			},
			wantError: true,
		},
		{
			name:      "full_with_error",
			verbosity: buildprotocol.VerbosityFull,
			input: &buildprotocol.JobResult{
				JobID:    "test-4",
				ExitCode: -1,
				Stderr:   "error message",
				Stdout:   "stdout output",
				Output:   "stdout outputerror message",
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filtered := applyVerbosityFilter(tt.input, tt.verbosity)

			t.Logf("Input:  Output=%q, Stderr=%q", tt.input.Output, tt.input.Stderr)
			t.Logf("Result: Output=%q, Stderr=%q", filtered.Output, filtered.Stderr)

			if tt.wantError {
				// Error message should be preserved in Output
				if filtered.Output == "" {
					t.Errorf("Output is empty, want error message preserved")
				}
			}

			// ExitCode should always be preserved
			if filtered.ExitCode != tt.input.ExitCode {
				t.Errorf("ExitCode = %d, want %d", filtered.ExitCode, tt.input.ExitCode)
			}
		})
	}
}

func TestDispatcher_VerbosityFilterWithEmbeddedWorker(t *testing.T) {
	// Full integration test: embedded worker error → dispatcher → verbosity filter
	reg := NewRegistry()

	// Mock embedded worker that returns an error result
	embedded := func(job *buildprotocol.JobMessage) *buildprotocol.JobResult {
		return &buildprotocol.JobResult{
			JobID:    job.JobID,
			ExitCode: -1,
			Stderr:   "embedded worker error: test error message",
			Output:   "embedded worker error: test error message",
		}
	}

	disp := NewDispatcher(reg, embedded)

	job := &buildprotocol.JobMessage{
		JobID:   "test-job",
		Repo:    "", // Empty repo triggers embedded worker
		Command: "test",
	}

	// Submit with minimal verbosity (strictest filtering)
	resultCh := disp.SubmitWithVerbosity(job, buildprotocol.VerbosityMinimal)
	disp.TryDispatch()

	// Wait for result
	result := <-resultCh

	t.Logf("Result: ExitCode=%d, Output=%q, Stderr=%q", result.ExitCode, result.Output, result.Stderr)

	// Should preserve error details
	if result.ExitCode != -1 {
		t.Errorf("ExitCode = %d, want -1", result.ExitCode)
	}

	if result.Output == "" {
		t.Errorf("Output is empty, want error message")
	}

	if result.Stderr == "" {
		t.Errorf("Stderr is empty, want error message")
	}
}
