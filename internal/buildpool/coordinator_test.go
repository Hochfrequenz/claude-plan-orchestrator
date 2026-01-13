// internal/buildpool/coordinator_test.go
package buildpool

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/buildprotocol"
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
	// With separate stream accumulation, GetAndClearOutput returns stdout + stderr
	if output != "line 1\nline 2\nerror 1\n" {
		t.Errorf("output = %q, want stdout then stderr", output)
	}

	// Verify cleared
	output = coord.GetAndClearOutput(jobID)
	if output != "" {
		t.Errorf("output after clear = %q, want empty", output)
	}
}

func TestCoordinator_SeparateStreamAccumulation(t *testing.T) {
	registry := NewRegistry()
	dispatcher := NewDispatcher(registry, nil)
	coord := NewCoordinator(CoordinatorConfig{WebSocketPort: 0}, registry, dispatcher)

	jobID := "test-job-sep"
	coord.AccumulateOutput(jobID, "stdout", "stdout line 1\n")
	coord.AccumulateOutput(jobID, "stderr", "stderr line 1\n")
	coord.AccumulateOutput(jobID, "stdout", "stdout line 2\n")

	stdout, stderr := coord.GetSeparateOutput(jobID)
	if stdout != "stdout line 1\nstdout line 2\n" {
		t.Errorf("stdout = %q, want stdout lines only", stdout)
	}
	if stderr != "stderr line 1\n" {
		t.Errorf("stderr = %q, want stderr lines only", stderr)
	}
}

func TestCoordinator_LogRetention(t *testing.T) {
	registry := NewRegistry()
	dispatcher := NewDispatcher(registry, nil)
	coord := NewCoordinator(CoordinatorConfig{WebSocketPort: 0}, registry, dispatcher)

	// Complete a job (simulates output being moved to retention)
	coord.AccumulateOutput("job-1", "stdout", "stdout content")
	coord.AccumulateOutput("job-1", "stderr", "stderr content")
	coord.RetainLogs("job-1")

	// Should be able to retrieve logs
	stdout, stderr, found := coord.GetRetainedLogs("job-1")
	if !found {
		t.Fatal("expected to find retained logs")
	}
	if stdout != "stdout content" {
		t.Errorf("stdout = %q, want %q", stdout, "stdout content")
	}
	if stderr != "stderr content" {
		t.Errorf("stderr = %q, want %q", stderr, "stderr content")
	}

	// Should still be available (doesn't clear on read)
	_, _, found = coord.GetRetainedLogs("job-1")
	if !found {
		t.Error("retained logs should persist across reads")
	}
}

func TestCoordinator_LogRetentionEviction(t *testing.T) {
	registry := NewRegistry()
	dispatcher := NewDispatcher(registry, nil)
	coord := NewCoordinator(CoordinatorConfig{WebSocketPort: 0}, registry, dispatcher)

	// Fill buffer with 50 jobs
	for i := 0; i < 50; i++ {
		jobID := fmt.Sprintf("job-%d", i)
		coord.AccumulateOutput(jobID, "stdout", fmt.Sprintf("stdout-%d", i))
		coord.RetainLogs(jobID)
	}

	// job-0 should still be present
	_, _, found := coord.GetRetainedLogs("job-0")
	if !found {
		t.Error("job-0 should still be retained")
	}

	// Add one more job - should evict job-0
	coord.AccumulateOutput("job-50", "stdout", "stdout-50")
	coord.RetainLogs("job-50")

	// job-0 should be evicted
	_, _, found = coord.GetRetainedLogs("job-0")
	if found {
		t.Error("job-0 should have been evicted")
	}

	// job-1 and job-50 should be present
	_, _, found = coord.GetRetainedLogs("job-1")
	if !found {
		t.Error("job-1 should still be retained")
	}
	_, _, found = coord.GetRetainedLogs("job-50")
	if !found {
		t.Error("job-50 should be retained")
	}
}

func TestCoordinatorHeartbeat(t *testing.T) {
	registry := NewRegistry()
	dispatcher := NewDispatcher(registry, nil)
	config := CoordinatorConfig{
		WebSocketPort:     0,
		HeartbeatInterval: 50 * time.Millisecond,
		HeartbeatTimeout:  100 * time.Millisecond, // Read deadline - worker evicted if no pong received
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

	// Send a ping - the test client doesn't respond with pong, so the coordinator's
	// read deadline will expire and the worker will be disconnected
	coord.sendHeartbeats()

	// Wait for the read deadline to expire (HeartbeatTimeout = 100ms)
	// The worker should be evicted after the deadline passes
	time.Sleep(150 * time.Millisecond)

	if registry.Get("heartbeat-test") != nil {
		t.Error("worker should have been evicted due to heartbeat timeout")
	}
}

func TestCoordinator_CompleteWithVerbosity(t *testing.T) {
	registry := NewRegistry()
	dispatcher := NewDispatcher(registry, nil)
	coord := NewCoordinator(CoordinatorConfig{WebSocketPort: 0}, registry, dispatcher)

	// Accumulate output
	coord.AccumulateOutput("job-verb", "stdout", "stdout content\n")
	coord.AccumulateOutput("job-verb", "stderr", "stderr content\n")

	// Test minimal (default)
	result := coord.CompleteJob("job-verb", 0, 1500, "")
	if result.Stdout != "" {
		t.Errorf("minimal: stdout should be empty, got %q", result.Stdout)
	}
	if result.Stderr != "stderr content\n" {
		t.Errorf("minimal: stderr = %q, want %q", result.Stderr, "stderr content\n")
	}
}

func TestCoordinator_CompleteJobFields(t *testing.T) {
	registry := NewRegistry()
	dispatcher := NewDispatcher(registry, nil)
	coord := NewCoordinator(CoordinatorConfig{WebSocketPort: 0}, registry, dispatcher)

	// Accumulate output
	coord.AccumulateOutput("job-fields", "stdout", "stdout content\n")
	coord.AccumulateOutput("job-fields", "stderr", "stderr content\n")

	// Test result fields are set correctly
	result := coord.CompleteJob("job-fields", 42, 2500, "full")
	if result.JobID != "job-fields" {
		t.Errorf("JobID = %q, want %q", result.JobID, "job-fields")
	}
	if result.ExitCode != 42 {
		t.Errorf("ExitCode = %d, want %d", result.ExitCode, 42)
	}
	if result.DurationSecs != 2.5 {
		t.Errorf("DurationSecs = %f, want %f", result.DurationSecs, 2.5)
	}
	// Backwards-compat Output field
	if result.Output != "stdout content\nstderr content\n" {
		t.Errorf("Output = %q, want %q", result.Output, "stdout content\nstderr content\n")
	}
}

func TestCoordinator_CompleteJobRetainsLogs(t *testing.T) {
	registry := NewRegistry()
	dispatcher := NewDispatcher(registry, nil)
	coord := NewCoordinator(CoordinatorConfig{WebSocketPort: 0}, registry, dispatcher)

	// Accumulate output
	coord.AccumulateOutput("job-retain", "stdout", "retained stdout\n")
	coord.AccumulateOutput("job-retain", "stderr", "retained stderr\n")

	// CompleteJob should retain logs
	_ = coord.CompleteJob("job-retain", 0, 1000, "minimal")

	// Verify logs are retained (even after filtering removes stdout from result)
	stdout, stderr, found := coord.GetRetainedLogs("job-retain")
	if !found {
		t.Fatal("expected to find retained logs")
	}
	if stdout != "retained stdout\n" {
		t.Errorf("retained stdout = %q, want %q", stdout, "retained stdout\n")
	}
	if stderr != "retained stderr\n" {
		t.Errorf("retained stderr = %q, want %q", stderr, "retained stderr\n")
	}
}

func TestCoordinator_CompleteJobVerbosityLevels(t *testing.T) {
	tests := []struct {
		name         string
		verbosity    string
		expectStdout bool
	}{
		{"minimal", "minimal", false},
		{"default empty", "", false},
		{"normal", "normal", true},
		{"full", "full", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewRegistry()
			dispatcher := NewDispatcher(registry, nil)
			coord := NewCoordinator(CoordinatorConfig{WebSocketPort: 0}, registry, dispatcher)

			jobID := "job-" + tt.name
			coord.AccumulateOutput(jobID, "stdout", "stdout content\n")
			coord.AccumulateOutput(jobID, "stderr", "stderr content\n")

			result := coord.CompleteJob(jobID, 0, 1000, tt.verbosity)

			hasStdout := result.Stdout != ""
			if hasStdout != tt.expectStdout {
				t.Errorf("stdout present = %v, want %v", hasStdout, tt.expectStdout)
			}
			// stderr should always be present
			if result.Stderr != "stderr content\n" {
				t.Errorf("stderr = %q, want %q", result.Stderr, "stderr content\n")
			}
		})
	}
}

func TestCoordinator_HTTPJobVerbosity(t *testing.T) {
	registry := NewRegistry()

	// Embedded worker returns known stdout/stderr for verification
	const knownStdout = "STDOUT_MARKER_12345"
	const knownStderr = "STDERR_MARKER_67890"

	embedded := func(job *buildprotocol.JobMessage) *buildprotocol.JobResult {
		return &buildprotocol.JobResult{
			JobID:    job.JobID,
			ExitCode: 0,
			Stdout:   knownStdout,
			Stderr:   knownStderr,
		}
	}

	dispatcher := NewDispatcher(registry, embedded)
	coord := NewCoordinator(CoordinatorConfig{WebSocketPort: 0}, registry, dispatcher)

	server := httptest.NewServer(http.HandlerFunc(coord.HandleJobSubmit))
	defer server.Close()

	tests := []struct {
		name         string
		body         string
		expectStdout bool // whether stdout should be in response output
		expectStderr bool // whether stderr should be in response output
	}{
		{
			name:         "explicit minimal verbosity",
			body:         `{"command":"echo test","verbosity":"minimal"}`,
			expectStdout: false,
			expectStderr: true,
		},
		{
			name:         "explicit normal verbosity",
			body:         `{"command":"echo test","verbosity":"normal"}`,
			expectStdout: true,
			expectStderr: true,
		},
		{
			name:         "explicit full verbosity",
			body:         `{"command":"echo test","verbosity":"full"}`,
			expectStdout: true,
			expectStderr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := http.Post(server.URL, "application/json", strings.NewReader(tt.body))
			if err != nil {
				t.Fatalf("POST: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("expected status OK, got %d", resp.StatusCode)
			}

			// Decode into JobResponse which is what HandleJobSubmit returns
			var result JobResponse
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				t.Fatalf("decode response: %v", err)
			}

			// Verify stdout filtering via the Output field
			// (Output = filtered Stdout + Stderr)
			hasStdout := strings.Contains(result.Output, knownStdout)
			if hasStdout != tt.expectStdout {
				t.Errorf("stdout in output = %v, want %v (output=%q)", hasStdout, tt.expectStdout, result.Output)
			}

			// Verify stderr is always present
			hasStderr := strings.Contains(result.Output, knownStderr)
			if hasStderr != tt.expectStderr {
				t.Errorf("stderr in output = %v, want %v (output=%q)", hasStderr, tt.expectStderr, result.Output)
			}
		})
	}
}

func TestCoordinator_VerbosityFiltering(t *testing.T) {
	registry := NewRegistry()
	dispatcher := NewDispatcher(registry, nil)
	coord := NewCoordinator(CoordinatorConfig{WebSocketPort: 0}, registry, dispatcher)

	// Create 60 lines of stdout
	var stdoutLines []string
	for i := 1; i <= 60; i++ {
		stdoutLines = append(stdoutLines, fmt.Sprintf("line %d", i))
	}
	stdout := strings.Join(stdoutLines, "\n") + "\n"
	stderr := "error output\n"

	tests := []struct {
		name             string
		verbosity        string
		expectStdout     bool
		expectFullStdout bool
		expectStderr     bool
	}{
		{"minimal", "minimal", false, false, true},
		{"minimal default", "", false, false, true},
		{"normal", "normal", true, false, true},
		{"full", "full", true, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := coord.FilterOutput(stdout, stderr, tt.verbosity)

			hasStdout := result.Stdout != ""
			hasStderr := result.Stderr != ""

			if hasStdout != tt.expectStdout {
				t.Errorf("stdout present = %v, want %v", hasStdout, tt.expectStdout)
			}
			if hasStderr != tt.expectStderr {
				t.Errorf("stderr present = %v, want %v", hasStderr, tt.expectStderr)
			}

			if tt.expectFullStdout && result.Stdout != stdout {
				t.Errorf("expected full stdout")
			}

			// For normal verbosity, should have last 50 lines only
			if tt.verbosity == "normal" && result.Stdout != "" {
				lines := strings.Split(strings.TrimSuffix(result.Stdout, "\n"), "\n")
				if len(lines) != 50 {
					t.Errorf("normal verbosity: got %d lines, want 50", len(lines))
				}
				// First retained line should be "line 11" (60-50+1)
				if lines[0] != "line 11" {
					t.Errorf("normal verbosity: first line = %q, want %q", lines[0], "line 11")
				}
			}
		})
	}
}

func TestCoordinator_HandleGetLogs(t *testing.T) {
	registry := NewRegistry()
	dispatcher := NewDispatcher(registry, nil)
	coord := NewCoordinator(CoordinatorConfig{WebSocketPort: 0}, registry, dispatcher)

	// Add some retained logs
	coord.AccumulateOutput("test-logs-123", "stdout", "stdout content here")
	coord.AccumulateOutput("test-logs-123", "stderr", "stderr content here")
	coord.RetainLogs("test-logs-123")

	server := httptest.NewServer(http.HandlerFunc(coord.HandleGetLogs))
	defer server.Close()

	t.Run("retrieve existing logs", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/logs/test-logs-123")
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected status OK, got %d", resp.StatusCode)
		}

		var result LogsResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decode response: %v", err)
		}

		if result.JobID != "test-logs-123" {
			t.Errorf("job_id = %q, want %q", result.JobID, "test-logs-123")
		}
		if result.Stdout != "stdout content here" {
			t.Errorf("stdout = %q, want %q", result.Stdout, "stdout content here")
		}
		if result.Stderr != "stderr content here" {
			t.Errorf("stderr = %q, want %q", result.Stderr, "stderr content here")
		}
	})

	t.Run("retrieve with stream filter", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/logs/test-logs-123?stream=stdout")
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		defer resp.Body.Close()

		var result LogsResponse
		json.NewDecoder(resp.Body).Decode(&result)

		if result.Stdout != "stdout content here" {
			t.Errorf("stdout = %q, want %q", result.Stdout, "stdout content here")
		}
		if result.Stderr != "" {
			t.Errorf("stderr should be empty with stdout filter, got %q", result.Stderr)
		}
	})

	t.Run("not found", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/logs/nonexistent-job")
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("expected status NotFound, got %d", resp.StatusCode)
		}

		var result LogsResponse
		json.NewDecoder(resp.Body).Decode(&result)

		if result.Error == "" {
			t.Error("expected error message for not found")
		}
	})
}
