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
