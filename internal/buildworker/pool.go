// internal/buildworker/pool.go
package buildworker

import "sync"

// Pool manages a fixed number of job slots
type Pool struct {
	maxJobs   int
	available int
	mu        sync.Mutex
}

// NewPool creates a pool with the given capacity
func NewPool(maxJobs int) *Pool {
	return &Pool{
		maxJobs:   maxJobs,
		available: maxJobs,
	}
}

// Acquire tries to claim a job slot. Returns true if successful.
func (p *Pool) Acquire() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.available <= 0 {
		return false
	}
	p.available--
	return true
}

// Release returns a job slot to the pool.
func (p *Pool) Release() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.available < p.maxJobs {
		p.available++
	}
}

// Available returns the number of free slots.
func (p *Pool) Available() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.available
}

// MaxJobs returns the pool capacity.
func (p *Pool) MaxJobs() int {
	return p.maxJobs
}
