// internal/buildpool/registry_test.go
package buildpool

import (
	"testing"
)

func TestRegistry_RegisterUnregister(t *testing.T) {
	reg := NewRegistry()

	// Register a worker
	w := &ConnectedWorker{
		ID:      "worker-1",
		MaxJobs: 4,
		Slots:   4,
	}
	reg.Register(w)

	if got := reg.Count(); got != 1 {
		t.Errorf("got count=%d, want 1", got)
	}

	// Get the worker
	found := reg.Get("worker-1")
	if found == nil {
		t.Fatal("worker not found")
	}
	if found.MaxJobs != 4 {
		t.Errorf("got maxJobs=%d, want 4", found.MaxJobs)
	}

	// Unregister
	reg.Unregister("worker-1")
	if got := reg.Count(); got != 0 {
		t.Errorf("got count=%d, want 0", got)
	}
}

func TestRegistry_FindReady(t *testing.T) {
	reg := NewRegistry()

	reg.Register(&ConnectedWorker{ID: "worker-1", MaxJobs: 4, Slots: 0}) // No slots
	reg.Register(&ConnectedWorker{ID: "worker-2", MaxJobs: 4, Slots: 2}) // 2 slots
	reg.Register(&ConnectedWorker{ID: "worker-3", MaxJobs: 4, Slots: 4}) // 4 slots

	ready := reg.FindReady()
	if ready == nil {
		t.Fatal("expected to find a ready worker")
	}

	// Should pick worker with most slots (worker-3)
	if ready.ID != "worker-3" {
		t.Errorf("got worker %s, want worker-3", ready.ID)
	}
}
