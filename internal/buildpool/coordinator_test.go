// internal/buildpool/coordinator_test.go
package buildpool

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// newTestCoordinator creates a coordinator with default registry and dispatcher for testing
func newTestCoordinator(config CoordinatorConfig) *Coordinator {
	registry := NewRegistry()
	dispatcher := NewDispatcher(registry, nil)
	return NewCoordinator(config, registry, dispatcher)
}

func TestCoordinator_AcceptWorker(t *testing.T) {
	coord := newTestCoordinator(CoordinatorConfig{
		WebSocketPort: 0, // Use any available port
	})

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(coord.HandleWebSocket))
	defer server.Close()

	// Connect as worker
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	// Send register message
	registerMsg := `{"type":"register","payload":{"worker_id":"test-worker","max_jobs":4}}`
	if err := conn.WriteMessage(websocket.TextMessage, []byte(registerMsg)); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// Give time for registration
	time.Sleep(50 * time.Millisecond)

	// Verify worker is registered
	if coord.Registry().Count() != 1 {
		t.Errorf("got worker count=%d, want 1", coord.Registry().Count())
	}

	worker := coord.Registry().Get("test-worker")
	if worker == nil {
		t.Fatal("worker not found in registry")
	}
	if worker.MaxJobs != 4 {
		t.Errorf("got max_jobs=%d, want 4", worker.MaxJobs)
	}
}

func TestCoordinator_WorkerDisconnect(t *testing.T) {
	coord := newTestCoordinator(CoordinatorConfig{})

	server := httptest.NewServer(http.HandlerFunc(coord.HandleWebSocket))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	// Register worker
	registerMsg := `{"type":"register","payload":{"worker_id":"disconnect-test","max_jobs":2}}`
	if err := conn.WriteMessage(websocket.TextMessage, []byte(registerMsg)); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	if coord.Registry().Count() != 1 {
		t.Fatalf("worker not registered")
	}

	// Close connection
	conn.Close()

	// Give time for cleanup
	time.Sleep(50 * time.Millisecond)

	// Verify worker is unregistered
	if coord.Registry().Count() != 0 {
		t.Errorf("got worker count=%d, want 0", coord.Registry().Count())
	}
}

func TestCoordinator_ReadyMessage(t *testing.T) {
	coord := newTestCoordinator(CoordinatorConfig{})

	server := httptest.NewServer(http.HandlerFunc(coord.HandleWebSocket))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	// Register
	registerMsg := `{"type":"register","payload":{"worker_id":"ready-test","max_jobs":4}}`
	if err := conn.WriteMessage(websocket.TextMessage, []byte(registerMsg)); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	worker := coord.Registry().Get("ready-test")
	if worker == nil {
		t.Fatal("worker not found")
	}

	// Verify initial slots
	if worker.Slots != 4 {
		t.Errorf("got initial slots=%d, want 4", worker.Slots)
	}

	// Send ready message with updated slots
	readyMsg := `{"type":"ready","payload":{"slots":2}}`
	if err := conn.WriteMessage(websocket.TextMessage, []byte(readyMsg)); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// Verify slots updated
	if worker.Slots != 2 {
		t.Errorf("got slots=%d, want 2", worker.Slots)
	}
}

func TestCoordinator_CompleteMessage(t *testing.T) {
	coord := newTestCoordinator(CoordinatorConfig{})

	server := httptest.NewServer(http.HandlerFunc(coord.HandleWebSocket))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	// Register
	registerMsg := `{"type":"register","payload":{"worker_id":"complete-test","max_jobs":2}}`
	if err := conn.WriteMessage(websocket.TextMessage, []byte(registerMsg)); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// Submit a job to dispatcher
	job := &struct {
		JobID   string `json:"job_id"`
		Command string `json:"command"`
	}{
		JobID:   "test-job-1",
		Command: "echo hello",
	}
	_ = job // We'll test complete message handling through the dispatcher

	// Send complete message
	completeMsg := `{"type":"complete","payload":{"job_id":"test-job-1","exit_code":0,"duration_ms":1500}}`
	if err := conn.WriteMessage(websocket.TextMessage, []byte(completeMsg)); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// The complete should be processed without errors
	// (actual result handling tested through dispatcher integration)
}

func TestCoordinator_Pong(t *testing.T) {
	coord := newTestCoordinator(CoordinatorConfig{})

	server := httptest.NewServer(http.HandlerFunc(coord.HandleWebSocket))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	// Register
	registerMsg := `{"type":"register","payload":{"worker_id":"pong-test","max_jobs":1}}`
	if err := conn.WriteMessage(websocket.TextMessage, []byte(registerMsg)); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	worker := coord.Registry().Get("pong-test")
	if worker == nil {
		t.Fatal("worker not found")
	}

	initialHeartbeat := worker.LastHeartbeat

	// Wait a bit then send pong
	time.Sleep(10 * time.Millisecond)
	pongMsg := `{"type":"pong"}`
	if err := conn.WriteMessage(websocket.TextMessage, []byte(pongMsg)); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// Verify heartbeat was updated
	if !worker.LastHeartbeat.After(initialHeartbeat) {
		t.Error("heartbeat was not updated after pong")
	}
}

func TestCoordinator_ErrorMessage(t *testing.T) {
	coord := newTestCoordinator(CoordinatorConfig{})

	server := httptest.NewServer(http.HandlerFunc(coord.HandleWebSocket))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	// Register
	registerMsg := `{"type":"register","payload":{"worker_id":"error-test","max_jobs":2}}`
	if err := conn.WriteMessage(websocket.TextMessage, []byte(registerMsg)); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// Send error message
	errorMsg := `{"type":"error","payload":{"job_id":"test-job-2","message":"command not found"}}`
	if err := conn.WriteMessage(websocket.TextMessage, []byte(errorMsg)); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// Error message should be processed without panic
	// (actual error handling tested through dispatcher integration)
}

func TestCoordinator_Dispatcher(t *testing.T) {
	coord := newTestCoordinator(CoordinatorConfig{})

	if coord.Dispatcher() == nil {
		t.Error("dispatcher should not be nil")
	}

	if coord.Registry() == nil {
		t.Error("registry should not be nil")
	}
}

func TestCoordinatorNewCoordinator(t *testing.T) {
	// Test constructor with injected dependencies
	registry := NewRegistry()
	dispatcher := NewDispatcher(registry, nil)
	config := CoordinatorConfig{
		WebSocketPort:     8080,
		HeartbeatInterval: 15 * time.Second,
		HeartbeatTimeout:  5 * time.Second,
	}

	coord := NewCoordinator(config, registry, dispatcher)

	if coord == nil {
		t.Fatal("NewCoordinator returned nil")
	}

	// Verify injected dependencies are used
	if coord.Registry() != registry {
		t.Error("Registry should match injected registry")
	}
	if coord.Dispatcher() != dispatcher {
		t.Error("Dispatcher should match injected dispatcher")
	}

	// Test default values are not overridden when provided
	// (config values should be preserved)
}

func TestCoordinatorNewCoordinatorDefaults(t *testing.T) {
	// Test that defaults are applied when not provided
	registry := NewRegistry()
	dispatcher := NewDispatcher(registry, nil)
	config := CoordinatorConfig{
		WebSocketPort: 8080,
		// HeartbeatInterval and HeartbeatTimeout not set
	}

	coord := NewCoordinator(config, registry, dispatcher)

	if coord == nil {
		t.Fatal("NewCoordinator returned nil")
	}

	// Defaults should be applied internally (30s interval, 10s timeout)
	// We can't directly check config values but the coordinator should function
}

func TestCoordinatorStartStop(t *testing.T) {
	registry := NewRegistry()
	dispatcher := NewDispatcher(registry, nil)
	config := CoordinatorConfig{
		WebSocketPort:     0, // Let OS pick available port
		HeartbeatInterval: 100 * time.Millisecond,
	}

	coord := NewCoordinator(config, registry, dispatcher)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start in goroutine since it blocks
	errCh := make(chan error, 1)
	go func() {
		errCh <- coord.Start(ctx)
	}()

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	// Stop should work without error
	err := coord.Stop()
	if err != nil {
		t.Errorf("Stop returned error: %v", err)
	}

	// Cancel context to clean up heartbeat loop
	cancel()

	// Wait for Start to return
	select {
	case <-errCh:
		// Expected - server was closed
	case <-time.After(time.Second):
		t.Error("Start did not return after Stop was called")
	}
}

func TestCoordinator_OutputAccumulation(t *testing.T) {
	registry := NewRegistry()
	dispatcher := NewDispatcher(registry, nil)
	coord := NewCoordinator(CoordinatorConfig{WebSocketPort: 0}, registry, dispatcher)

	// Simulate output accumulation
	jobID := "test-job-123"
	coord.AccumulateOutput(jobID, "stdout", "line 1\n")
	coord.AccumulateOutput(jobID, "stderr", "error 1\n")
	coord.AccumulateOutput(jobID, "stdout", "line 2\n")

	output := coord.GetAndClearOutput(jobID)
	if output != "line 1\nerror 1\nline 2\n" {
		t.Errorf("output = %q, want accumulated output", output)
	}

	// Verify cleared
	output = coord.GetAndClearOutput(jobID)
	if output != "" {
		t.Errorf("output after clear = %q, want empty", output)
	}
}

func TestCoordinatorHeartbeat(t *testing.T) {
	registry := NewRegistry()
	dispatcher := NewDispatcher(registry, nil)
	config := CoordinatorConfig{
		WebSocketPort:     0,
		HeartbeatInterval: 50 * time.Millisecond,
		HeartbeatTimeout:  100 * time.Millisecond,
	}

	coord := NewCoordinator(config, registry, dispatcher)

	server := httptest.NewServer(http.HandlerFunc(coord.HandleWebSocket))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	// Register worker
	registerMsg := `{"type":"register","payload":{"worker_id":"heartbeat-test","max_jobs":2}}`
	if err := conn.WriteMessage(websocket.TextMessage, []byte(registerMsg)); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	worker := registry.Get("heartbeat-test")
	if worker == nil {
		t.Fatal("worker not registered")
	}

	// Set a past heartbeat time to simulate timeout
	worker.LastHeartbeat = time.Now().Add(-200 * time.Millisecond)

	// Manually trigger heartbeat check (normally done by heartbeatLoop)
	coord.sendHeartbeats()

	// Worker should be evicted due to timeout
	time.Sleep(50 * time.Millisecond)

	if registry.Get("heartbeat-test") != nil {
		t.Error("worker should have been evicted due to heartbeat timeout")
	}
}
