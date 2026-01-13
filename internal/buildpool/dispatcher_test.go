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
