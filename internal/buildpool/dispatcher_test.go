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
