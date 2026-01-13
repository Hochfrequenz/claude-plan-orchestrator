// internal/buildpool/dispatcher.go
package buildpool

import (
	"fmt"
	"sync"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/buildprotocol"
)

// PendingJob tracks a job waiting for dispatch or completion
type PendingJob struct {
	Job      *buildprotocol.JobMessage
	ResultCh chan *buildprotocol.JobResult
	WorkerID string // Assigned worker (empty if queued)
}

// SendFunc sends a job to a worker
type SendFunc func(w *ConnectedWorker, job *buildprotocol.JobMessage) error

// EmbeddedWorkerFunc runs a job on the embedded worker
type EmbeddedWorkerFunc func(job *buildprotocol.JobMessage) *buildprotocol.JobResult

// CancelFunc sends a cancel message to a worker
type CancelFunc func(workerID, jobID string) error

// Dispatcher manages job queue and assignment
type Dispatcher struct {
	registry   *Registry
	embedded   EmbeddedWorkerFunc
	sendFunc   SendFunc
	cancelFunc CancelFunc

	queue   []*PendingJob
	pending map[string]*PendingJob // jobID -> pending job
	mu      sync.Mutex
}

// NewDispatcher creates a new job dispatcher
func NewDispatcher(registry *Registry, embedded EmbeddedWorkerFunc) *Dispatcher {
	return &Dispatcher{
		registry: registry,
		embedded: embedded,
		pending:  make(map[string]*PendingJob),
	}
}

// SetSendFunc sets the function used to send jobs to workers
func (d *Dispatcher) SetSendFunc(fn SendFunc) {
	d.sendFunc = fn
}

// SetCancelFunc sets the function used to cancel jobs on workers
func (d *Dispatcher) SetCancelFunc(fn CancelFunc) {
	d.cancelFunc = fn
}

// Submit adds a job to the queue and returns a channel for the result
func (d *Dispatcher) Submit(job *buildprotocol.JobMessage) chan *buildprotocol.JobResult {
	d.mu.Lock()
	defer d.mu.Unlock()

	resultCh := make(chan *buildprotocol.JobResult, 1)
	pending := &PendingJob{
		Job:      job,
		ResultCh: resultCh,
	}

	d.queue = append(d.queue, pending)
	d.pending[job.JobID] = pending

	return resultCh
}

// TryDispatch attempts to dispatch queued jobs to available workers
func (d *Dispatcher) TryDispatch() {
	d.mu.Lock()
	defer d.mu.Unlock()

	var remaining []*PendingJob

	for _, pj := range d.queue {
		// Try to find a ready worker
		worker := d.registry.FindReady()

		if worker != nil && d.sendFunc != nil {
			// Dispatch to worker
			worker.DecrementSlots()
			pj.WorkerID = worker.ID

			if err := d.sendFunc(worker, pj.Job); err != nil {
				// Send failed, keep in queue
				remaining = append(remaining, pj)
				continue
			}
		} else if d.embedded != nil && d.registry.Count() == 0 {
			// No workers, use embedded
			go func(pj *PendingJob) {
				result := d.embedded(pj.Job)
				d.Complete(pj.Job.JobID, result)
			}(pj)
		} else {
			// No available workers, keep in queue
			remaining = append(remaining, pj)
		}
	}

	d.queue = remaining
}

// Complete marks a job as complete and sends the result
func (d *Dispatcher) Complete(jobID string, result *buildprotocol.JobResult) {
	d.mu.Lock()
	pj, ok := d.pending[jobID]
	if ok {
		delete(d.pending, jobID)
	}
	d.mu.Unlock()

	if ok && pj.ResultCh != nil {
		pj.ResultCh <- result
		close(pj.ResultCh)
	}
}

// Cancel cancels a job
func (d *Dispatcher) Cancel(jobID string) error {
	d.mu.Lock()
	pj, ok := d.pending[jobID]
	if !ok {
		d.mu.Unlock()
		return fmt.Errorf("job %s not found", jobID)
	}

	workerID := pj.WorkerID

	// If still queued (not assigned), remove from queue and pending
	if workerID == "" {
		var remaining []*PendingJob
		for _, q := range d.queue {
			if q.Job.JobID != jobID {
				remaining = append(remaining, q)
			}
		}
		d.queue = remaining
		// Notify result channel before removing from pending
		if pj.ResultCh != nil {
			pj.ResultCh <- &buildprotocol.JobResult{JobID: jobID, ExitCode: -2, Output: "Job cancelled"}
			close(pj.ResultCh)
		}
		delete(d.pending, jobID)
		d.mu.Unlock()
		return nil
	}

	// Job is assigned to a worker - notify result channel and remove from pending
	if pj.ResultCh != nil {
		pj.ResultCh <- &buildprotocol.JobResult{JobID: jobID, ExitCode: -2, Output: "Job cancelled"}
		close(pj.ResultCh)
	}
	delete(d.pending, jobID)
	d.mu.Unlock()

	// Send cancel message to worker
	if d.cancelFunc == nil {
		return fmt.Errorf("job %s assigned to worker but no cancelFunc configured", jobID)
	}
	return d.cancelFunc(workerID, jobID)
}

// QueueLength returns the number of queued jobs
func (d *Dispatcher) QueueLength() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.queue)
}

// PendingCount returns the number of pending jobs (queued + in-progress)
func (d *Dispatcher) PendingCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.pending)
}

// RequeueWorkerJobs requeues all in-progress jobs assigned to a worker
func (d *Dispatcher) RequeueWorkerJobs(workerID string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for _, pj := range d.pending {
		if pj.WorkerID == workerID {
			pj.WorkerID = ""
			d.queue = append(d.queue, pj)
		}
	}
}

// QueuedCount returns the number of queued jobs (alias for QueueLength)
func (d *Dispatcher) QueuedCount() int {
	return d.QueueLength()
}

// LocalFallbackActive returns true if local fallback is configured
func (d *Dispatcher) LocalFallbackActive() bool {
	return d.embedded != nil
}
