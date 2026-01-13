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
