// internal/buildworker/pool.go
package buildworker

import "sync"

// Pool manages a fixed number of job slots
type Pool struct {
	maxJobs        int
	available      int
	mu             sync.Mutex
	onSlotsChanged func(available int) // Callback when slots change
}

// NewPool creates a pool with the given capacity
func NewPool(maxJobs int) *Pool {
	return &Pool{
		maxJobs:   maxJobs,
		available: maxJobs,
	}
}

// SetOnSlotsChanged sets a callback to be invoked when slot availability changes
func (p *Pool) SetOnSlotsChanged(callback func(available int)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onSlotsChanged = callback
}

// Acquire tries to claim a job slot. Returns true if successful.
func (p *Pool) Acquire() bool {
	p.mu.Lock()
	if p.available <= 0 {
		p.mu.Unlock()
		return false
	}
	p.available--
	callback := p.onSlotsChanged
	available := p.available
	p.mu.Unlock()

	// Notify outside of lock to avoid deadlock
	if callback != nil {
		callback(available)
	}
	return true
}

// Release returns a job slot to the pool.
func (p *Pool) Release() {
	p.mu.Lock()
	if p.available < p.maxJobs {
		p.available++
	}
	callback := p.onSlotsChanged
	available := p.available
	p.mu.Unlock()

	// Notify outside of lock to avoid deadlock
	if callback != nil {
		callback(available)
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
