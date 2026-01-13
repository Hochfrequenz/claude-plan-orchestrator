// Package buildpool provides worker registry and job dispatcher for the
// distributed build coordinator. It tracks connected workers and their
// available capacity for job scheduling.
package buildpool

import (
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ConnectedWorker represents a worker connection
type ConnectedWorker struct {
	ID            string
	MaxJobs       int
	Slots         int
	Conn          *websocket.Conn
	ConnectedAt   time.Time
	LastHeartbeat time.Time
	mu            sync.Mutex
	writeMu       sync.Mutex // protects Conn writes
}

// UpdateSlots updates available slots (thread-safe)
func (w *ConnectedWorker) UpdateSlots(slots int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.Slots = slots
}

// DecrementSlots reduces available slots by 1
func (w *ConnectedWorker) DecrementSlots() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.Slots > 0 {
		w.Slots--
	}
}

// GetLastHeartbeat returns the last heartbeat time (thread-safe)
func (w *ConnectedWorker) GetLastHeartbeat() time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.LastHeartbeat
}

// SetLastHeartbeat sets the last heartbeat time (thread-safe)
func (w *ConnectedWorker) SetLastHeartbeat(t time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.LastHeartbeat = t
}

// GetStatus returns a snapshot of worker status fields (thread-safe)
func (w *ConnectedWorker) GetStatus() (maxJobs, slots int, connectedAt time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.MaxJobs, w.Slots, w.ConnectedAt
}

// WriteMessage sends a message to the worker connection (thread-safe)
func (w *ConnectedWorker) WriteMessage(messageType int, data []byte) error {
	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	return w.Conn.WriteMessage(messageType, data)
}

// Registry tracks connected workers
type Registry struct {
	workers map[string]*ConnectedWorker
	mu      sync.RWMutex
}

// NewRegistry creates a new worker registry
func NewRegistry() *Registry {
	return &Registry{
		workers: make(map[string]*ConnectedWorker),
	}
}

// Register adds a worker to the registry
func (r *Registry) Register(w *ConnectedWorker) {
	r.mu.Lock()
	defer r.mu.Unlock()
	w.ConnectedAt = time.Now()
	w.LastHeartbeat = time.Now()
	r.workers[w.ID] = w
}

// Unregister removes a worker from the registry
func (r *Registry) Unregister(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.workers, id)
}

// Get returns a worker by ID
func (r *Registry) Get(id string) *ConnectedWorker {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.workers[id]
}

// Count returns the number of connected workers
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.workers)
}

// FindReady returns a worker with available slots, preferring workers with more slots
func (r *Registry) FindReady() *ConnectedWorker {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var best *ConnectedWorker
	var bestSlots int
	for _, w := range r.workers {
		w.mu.Lock()
		slots := w.Slots
		w.mu.Unlock()

		if slots > 0 && slots > bestSlots {
			best = w
			bestSlots = slots
		}
	}
	return best
}

// All returns all connected workers
func (r *Registry) All() []*ConnectedWorker {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*ConnectedWorker, 0, len(r.workers))
	for _, w := range r.workers {
		result = append(result, w)
	}
	return result
}

// TotalSlots returns sum of all available slots
func (r *Registry) TotalSlots() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	total := 0
	for _, w := range r.workers {
		w.mu.Lock()
		total += w.Slots
		w.mu.Unlock()
	}
	return total
}
