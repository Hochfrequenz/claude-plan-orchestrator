// internal/buildpool/dispatcher.go
package buildpool

import (
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

// Dispatcher manages job queue and assignment
type Dispatcher struct {
	registry *Registry
	embedded EmbeddedWorkerFunc
	sendFunc SendFunc

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
