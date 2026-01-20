// internal/buildworker/pool_test.go
package buildworker

import (
	"sync"
	"testing"
)

func TestPool_AcquireRelease(t *testing.T) {
	pool := NewPool(2)

	if pool.Available() != 2 {
		t.Errorf("got available=%d, want 2", pool.Available())
	}

	// Acquire first slot
	if !pool.Acquire() {
		t.Error("first acquire should succeed")
	}
	if pool.Available() != 1 {
		t.Errorf("got available=%d, want 1", pool.Available())
	}

	// Acquire second slot
	if !pool.Acquire() {
		t.Error("second acquire should succeed")
	}
	if pool.Available() != 0 {
		t.Errorf("got available=%d, want 0", pool.Available())
	}

	// Third acquire should fail
	if pool.Acquire() {
		t.Error("third acquire should fail when pool exhausted")
	}

	// Release one
	pool.Release()
	if pool.Available() != 1 {
		t.Errorf("got available=%d, want 1", pool.Available())
	}
}

func TestPool_Concurrent(t *testing.T) {
	pool := NewPool(5)

	var wg sync.WaitGroup
	acquired := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			acquired <- pool.Acquire()
		}()
	}

	wg.Wait()
	close(acquired)

	successCount := 0
	for ok := range acquired {
		if ok {
			successCount++
		}
	}

	if successCount != 5 {
		t.Errorf("got %d successful acquires, want 5", successCount)
	}
}

func TestPool_OnSlotsChanged(t *testing.T) {
	pool := NewPool(3)

	var mu sync.Mutex
	notifications := []int{}

	pool.SetOnSlotsChanged(func(available int) {
		mu.Lock()
		notifications = append(notifications, available)
		mu.Unlock()
	})

	// Acquire should trigger callback
	pool.Acquire()
	pool.Acquire()

	// Release should also trigger callback
	pool.Release()

	mu.Lock()
	got := notifications
	mu.Unlock()

	want := []int{2, 1, 2}
	if len(got) != len(want) {
		t.Errorf("got %d notifications, want %d", len(got), len(want))
		return
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("notification[%d]: got %d, want %d", i, got[i], want[i])
		}
	}
}

func TestPool_OnSlotsChanged_NilCallback(t *testing.T) {
	pool := NewPool(2)

	// Should not panic with nil callback
	pool.Acquire()
	pool.Release()
}

func TestPool_OnSlotsChanged_FailedAcquireNoCallback(t *testing.T) {
	pool := NewPool(1)

	callCount := 0
	pool.SetOnSlotsChanged(func(available int) {
		callCount++
	})

	// First acquire succeeds - triggers callback
	pool.Acquire()
	if callCount != 1 {
		t.Errorf("got %d callbacks, want 1", callCount)
	}

	// Second acquire fails - should NOT trigger callback
	pool.Acquire()
	if callCount != 1 {
		t.Errorf("failed acquire triggered callback: got %d callbacks, want 1", callCount)
	}
}
