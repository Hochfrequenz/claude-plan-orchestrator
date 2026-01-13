# Build Agents Production Hardening Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Address known limitations in the distributed build agents system to make it production-ready.

**Architecture:** Fix output streaming from remote workers, implement job cancellation, add worker reconnection with exponential backoff, improve git daemon security, and wire up the worker status MCP tool to return real data.

**Tech Stack:** Go, WebSockets (gorilla/websocket), git daemon

---

## Epic 1: Output Accumulation

The coordinator discards streaming output from workers. Jobs completed on remote workers return empty output to MCP callers.

### Task 1.1: Add Output Accumulator to Coordinator

**Files:**
- Modify: `internal/buildpool/coordinator.go:26-34` (add field)
- Modify: `internal/buildpool/coordinator.go:135-142` (accumulate output)
- Modify: `internal/buildpool/coordinator.go:143-153` (include output in completion)

**Step 1: Write the failing test**

Add to `internal/buildpool/coordinator_test.go`:

```go
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
```

**Step 2: Run test to verify it fails**

Run: `go test -run TestCoordinator_OutputAccumulation ./internal/buildpool -v`
Expected: FAIL with "coord.AccumulateOutput undefined"

**Step 3: Add output accumulator to Coordinator struct**

In `internal/buildpool/coordinator.go`, modify the struct and add methods:

```go
// Coordinator manages workers and dispatches jobs
type Coordinator struct {
	config     CoordinatorConfig
	registry   *Registry
	dispatcher *Dispatcher
	upgrader   websocket.Upgrader

	server *http.Server
	mu     sync.Mutex

	// Output accumulator for streaming output from workers
	outputMu     sync.Mutex
	outputBuffer map[string]*strings.Builder
}
```

Add import for `"strings"` at top of file.

In `NewCoordinator`, initialize the map:

```go
c := &Coordinator{
	config:       config,
	registry:     registry,
	dispatcher:   dispatcher,
	outputBuffer: make(map[string]*strings.Builder),
	upgrader: websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	},
}
```

Add the accumulator methods:

```go
// AccumulateOutput appends output for a job
func (c *Coordinator) AccumulateOutput(jobID, stream, data string) {
	c.outputMu.Lock()
	defer c.outputMu.Unlock()

	if c.outputBuffer[jobID] == nil {
		c.outputBuffer[jobID] = &strings.Builder{}
	}
	c.outputBuffer[jobID].WriteString(data)
}

// GetAndClearOutput returns accumulated output and clears the buffer
func (c *Coordinator) GetAndClearOutput(jobID string) string {
	c.outputMu.Lock()
	defer c.outputMu.Unlock()

	if buf, ok := c.outputBuffer[jobID]; ok {
		output := buf.String()
		delete(c.outputBuffer, jobID)
		return output
	}
	return ""
}
```

**Step 4: Run test to verify it passes**

Run: `go test -run TestCoordinator_OutputAccumulation ./internal/buildpool -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/buildpool/coordinator.go internal/buildpool/coordinator_test.go
git commit -m "feat(buildpool): add output accumulator to coordinator"
```

---

### Task 1.2: Wire Output Accumulation into Message Handler

**Files:**
- Modify: `internal/buildpool/coordinator.go:135-153` (use accumulator)

**Step 1: Write the failing test**

Add to `internal/buildpool/coordinator_test.go`:

```go
func TestCoordinator_OutputMessage_Accumulates(t *testing.T) {
	registry := NewRegistry()
	dispatcher := NewDispatcher(registry, nil)
	coord := NewCoordinator(CoordinatorConfig{WebSocketPort: 0}, registry, dispatcher)

	// Pre-populate output buffer to verify accumulation works
	coord.AccumulateOutput("job-1", "stdout", "existing\n")

	// Simulate receiving OutputMessage - we'll test via the actual handler path
	// by verifying the buffer after direct accumulation calls
	coord.AccumulateOutput("job-1", "stdout", "new line\n")

	output := coord.GetAndClearOutput("job-1")
	if output != "existing\nnew line\n" {
		t.Errorf("output = %q, want concatenated output", output)
	}
}
```

**Step 2: Run test to verify it passes**

Run: `go test -run TestCoordinator_OutputMessage_Accumulates ./internal/buildpool -v`
Expected: PASS (method already exists from Task 1.1)

**Step 3: Update the TypeOutput handler**

In `internal/buildpool/coordinator.go`, modify the `TypeOutput` case (around line 135):

```go
case buildprotocol.TypeOutput:
	var output buildprotocol.OutputMessage
	if err := json.Unmarshal(env.Payload, &output); err != nil {
		log.Printf("failed to unmarshal %s message: %v", env.Type, err)
		continue
	}
	c.AccumulateOutput(output.JobID, output.Stream, output.Data)
```

**Step 4: Update the TypeComplete handler**

In `internal/buildpool/coordinator.go`, modify the `TypeComplete` case (around line 143):

```go
case buildprotocol.TypeComplete:
	var complete buildprotocol.CompleteMessage
	if err := json.Unmarshal(env.Payload, &complete); err != nil {
		log.Printf("failed to unmarshal %s message: %v", env.Type, err)
		continue
	}
	output := c.GetAndClearOutput(complete.JobID)
	c.dispatcher.Complete(complete.JobID, &buildprotocol.JobResult{
		JobID:        complete.JobID,
		ExitCode:     complete.ExitCode,
		Output:       output,
		DurationSecs: float64(complete.DurationMs) / 1000,
	})
```

**Step 5: Run all buildpool tests**

Run: `go test ./internal/buildpool -v`
Expected: All tests PASS

**Step 6: Commit**

```bash
git add internal/buildpool/coordinator.go internal/buildpool/coordinator_test.go
git commit -m "feat(buildpool): wire output accumulation into message handler"
```

---

## Epic 2: Job Cancellation

Workers receive `CancelMessage` but ignore it. Long-running builds cannot be terminated.

### Task 2.1: Track Running Jobs in Worker

**Files:**
- Modify: `internal/buildworker/client.go:38-48` (add job tracking)
- Modify: `internal/buildworker/client.go:131-182` (track and cancel)

**Step 1: Write the failing test**

Create `internal/buildworker/client_test.go` if it doesn't exist:

```go
package buildworker

import (
	"context"
	"testing"
	"time"
)

func TestWorker_JobTracking(t *testing.T) {
	config := WorkerConfig{
		ServerURL: "ws://localhost:9999/ws", // Won't connect
		WorkerID:  "test",
		MaxJobs:   2,
	}

	w, err := NewWorker(config)
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}

	// Track a job
	ctx, cancel := context.WithCancel(context.Background())
	w.TrackJob("job-1", cancel)

	if !w.HasJob("job-1") {
		t.Error("HasJob(job-1) = false, want true")
	}

	// Cancel the job
	w.CancelJob("job-1")

	// Verify context was cancelled
	select {
	case <-ctx.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("context was not cancelled")
	}

	// Verify job is untracked
	if w.HasJob("job-1") {
		t.Error("HasJob(job-1) after cancel = true, want false")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -run TestWorker_JobTracking ./internal/buildworker -v`
Expected: FAIL with "w.TrackJob undefined"

**Step 3: Add job tracking to Worker**

In `internal/buildworker/client.go`, modify the Worker struct:

```go
// Worker is a build agent that connects to a coordinator
type Worker struct {
	config   WorkerConfig
	pool     *Pool
	executor *Executor
	conn     *websocket.Conn
	mu       sync.Mutex

	// For graceful shutdown
	ctx    context.Context
	cancel context.CancelFunc

	// Job tracking for cancellation
	jobsMu   sync.Mutex
	jobs     map[string]context.CancelFunc
}
```

In `NewWorker`, initialize the map:

```go
return &Worker{
	config: config,
	pool:   NewPool(config.MaxJobs),
	executor: NewExecutor(ExecutorConfig{
		GitCacheDir: config.GitCacheDir,
		WorktreeDir: config.WorktreeDir,
		UseNixShell: config.UseNixShell,
	}),
	ctx:    ctx,
	cancel: cancel,
	jobs:   make(map[string]context.CancelFunc),
}, nil
```

Add the tracking methods:

```go
// TrackJob registers a job's cancel function for later cancellation
func (w *Worker) TrackJob(jobID string, cancel context.CancelFunc) {
	w.jobsMu.Lock()
	defer w.jobsMu.Unlock()
	w.jobs[jobID] = cancel
}

// UntrackJob removes a job from tracking
func (w *Worker) UntrackJob(jobID string) {
	w.jobsMu.Lock()
	defer w.jobsMu.Unlock()
	delete(w.jobs, jobID)
}

// HasJob checks if a job is being tracked
func (w *Worker) HasJob(jobID string) bool {
	w.jobsMu.Lock()
	defer w.jobsMu.Unlock()
	_, ok := w.jobs[jobID]
	return ok
}

// CancelJob cancels a running job
func (w *Worker) CancelJob(jobID string) {
	w.jobsMu.Lock()
	cancel, ok := w.jobs[jobID]
	if ok {
		delete(w.jobs, jobID)
	}
	w.jobsMu.Unlock()

	if ok && cancel != nil {
		cancel()
	}
}
```

**Step 4: Run test to verify it passes**

Run: `go test -run TestWorker_JobTracking ./internal/buildworker -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/buildworker/client.go internal/buildworker/client_test.go
git commit -m "feat(buildworker): add job tracking for cancellation support"
```

---

### Task 2.2: Wire Job Tracking into handleJob

**Files:**
- Modify: `internal/buildworker/client.go:131-182` (track/untrack jobs)

**Step 1: Update handleJob to track jobs**

In `internal/buildworker/client.go`, modify `handleJob`:

```go
func (w *Worker) handleJob(jobMsg buildprotocol.JobMessage) {
	if !w.pool.Acquire() {
		w.send(buildprotocol.TypeError, buildprotocol.ErrorMessage{
			JobID:   jobMsg.JobID,
			Message: "no slots available",
		})
		return
	}
	defer func() {
		w.pool.Release()
		w.UntrackJob(jobMsg.JobID)
		w.sendReady()
	}()

	timeout := time.Duration(jobMsg.Timeout) * time.Second
	if timeout == 0 {
		timeout = 5 * time.Minute
	}

	ctx, cancel := context.WithTimeout(w.ctx, timeout)
	defer cancel()

	// Track this job for cancellation
	w.TrackJob(jobMsg.JobID, cancel)

	job := Job{
		ID:      jobMsg.JobID,
		Repo:    jobMsg.Repo,
		Commit:  jobMsg.Commit,
		Command: jobMsg.Command,
		Env:     jobMsg.Env,
		Timeout: timeout,
	}

	result, err := w.executor.RunJob(ctx, job, func(stream, data string) {
		w.send(buildprotocol.TypeOutput, buildprotocol.OutputMessage{
			JobID:  jobMsg.JobID,
			Stream: stream,
			Data:   data,
		})
	})

	if err != nil {
		w.send(buildprotocol.TypeError, buildprotocol.ErrorMessage{
			JobID:   jobMsg.JobID,
			Message: err.Error(),
		})
		return
	}

	w.send(buildprotocol.TypeComplete, buildprotocol.CompleteMessage{
		JobID:      jobMsg.JobID,
		ExitCode:   result.ExitCode,
		DurationMs: int64(result.DurationSecs * 1000),
	})
}
```

**Step 2: Run all buildworker tests**

Run: `go test ./internal/buildworker -v`
Expected: All tests PASS

**Step 3: Commit**

```bash
git add internal/buildworker/client.go
git commit -m "feat(buildworker): wire job tracking into handleJob"
```

---

### Task 2.3: Handle CancelMessage

**Files:**
- Modify: `internal/buildworker/client.go:123-127` (implement cancel handler)

**Step 1: Write the test**

Add to `internal/buildworker/client_test.go`:

```go
func TestWorker_CancelMessage_CancelsJob(t *testing.T) {
	config := WorkerConfig{
		ServerURL: "ws://localhost:9999/ws",
		WorkerID:  "test",
		MaxJobs:   2,
	}

	w, err := NewWorker(config)
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}

	// Track a mock job
	ctx, cancel := context.WithCancel(context.Background())
	w.TrackJob("job-to-cancel", cancel)

	// Simulate cancel - directly call CancelJob
	w.CancelJob("job-to-cancel")

	// Verify cancelled
	select {
	case <-ctx.Done():
		// Expected
	default:
		t.Error("job context was not cancelled")
	}
}
```

**Step 2: Run test**

Run: `go test -run TestWorker_CancelMessage_CancelsJob ./internal/buildworker -v`
Expected: PASS

**Step 3: Update the TypeCancel handler in Run()**

In `internal/buildworker/client.go`, modify the `TypeCancel` case:

```go
case buildprotocol.TypeCancel:
	var cancel buildprotocol.CancelMessage
	if err := json.Unmarshal(env.Payload, &cancel); err != nil {
		log.Printf("invalid cancel message: %v", err)
		continue
	}
	log.Printf("cancelling job %s", cancel.JobID)
	w.CancelJob(cancel.JobID)
```

**Step 4: Run all tests**

Run: `go test ./internal/buildworker -v`
Expected: All tests PASS

**Step 5: Commit**

```bash
git add internal/buildworker/client.go internal/buildworker/client_test.go
git commit -m "feat(buildworker): implement job cancellation handler"
```

---

### Task 2.4: Add Cancel Method to Dispatcher

**Files:**
- Modify: `internal/buildpool/dispatcher.go` (add Cancel method)
- Modify: `internal/buildpool/coordinator.go` (add sendCancel)

**Step 1: Write the failing test**

Add to `internal/buildpool/dispatcher_test.go`:

```go
func TestDispatcher_Cancel(t *testing.T) {
	registry := NewRegistry()
	dispatcher := NewDispatcher(registry, nil)

	var cancelledJobs []string
	dispatcher.SetCancelFunc(func(workerID, jobID string) error {
		cancelledJobs = append(cancelledJobs, jobID)
		return nil
	})

	// Submit a job
	job := &buildprotocol.JobMessage{JobID: "job-1"}
	dispatcher.Submit(job)

	// Simulate assignment to a worker
	dispatcher.mu.Lock()
	if pj, ok := dispatcher.pending["job-1"]; ok {
		pj.WorkerID = "worker-1"
	}
	dispatcher.mu.Unlock()

	// Cancel the job
	err := dispatcher.Cancel("job-1")
	if err != nil {
		t.Errorf("Cancel: %v", err)
	}

	if len(cancelledJobs) != 1 || cancelledJobs[0] != "job-1" {
		t.Errorf("cancelledJobs = %v, want [job-1]", cancelledJobs)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -run TestDispatcher_Cancel ./internal/buildpool -v`
Expected: FAIL with "dispatcher.SetCancelFunc undefined"

**Step 3: Add Cancel functionality to Dispatcher**

In `internal/buildpool/dispatcher.go`, add the cancel function type and field:

```go
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
```

Add the setter and Cancel method:

```go
// SetCancelFunc sets the function used to cancel jobs on workers
func (d *Dispatcher) SetCancelFunc(fn CancelFunc) {
	d.cancelFunc = fn
}

// Cancel cancels a job
func (d *Dispatcher) Cancel(jobID string) error {
	d.mu.Lock()
	pj, ok := d.pending[jobID]
	workerID := ""
	if ok {
		workerID = pj.WorkerID
	}
	d.mu.Unlock()

	if !ok {
		return fmt.Errorf("job %s not found", jobID)
	}

	// If assigned to a worker, send cancel message
	if workerID != "" && d.cancelFunc != nil {
		return d.cancelFunc(workerID, jobID)
	}

	// If still queued, just remove from queue
	d.mu.Lock()
	defer d.mu.Unlock()
	var remaining []*PendingJob
	for _, q := range d.queue {
		if q.Job.JobID != jobID {
			remaining = append(remaining, q)
		}
	}
	d.queue = remaining
	delete(d.pending, jobID)

	return nil
}
```

Add import `"fmt"` at the top if not present.

**Step 4: Run test to verify it passes**

Run: `go test -run TestDispatcher_Cancel ./internal/buildpool -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/buildpool/dispatcher.go internal/buildpool/dispatcher_test.go
git commit -m "feat(buildpool): add Cancel method to dispatcher"
```

---

### Task 2.5: Wire Cancel into Coordinator

**Files:**
- Modify: `internal/buildpool/coordinator.go` (add sendCancel, wire to dispatcher)

**Step 1: Add sendCancel method and wire to dispatcher**

In `internal/buildpool/coordinator.go`, in `NewCoordinator`, add after `SetSendFunc`:

```go
c.dispatcher.SetSendFunc(c.sendJobToWorker)
c.dispatcher.SetCancelFunc(c.sendCancelToWorker)
```

Add the sendCancelToWorker method:

```go
func (c *Coordinator) sendCancelToWorker(workerID, jobID string) error {
	w := c.registry.Get(workerID)
	if w == nil {
		return fmt.Errorf("worker %s not found", workerID)
	}

	data, err := buildprotocol.MarshalEnvelope(buildprotocol.TypeCancel, buildprotocol.CancelMessage{
		JobID: jobID,
	})
	if err != nil {
		return err
	}
	return w.WriteMessage(websocket.TextMessage, data)
}
```

**Step 2: Run all tests**

Run: `go test ./internal/buildpool -v`
Expected: All tests PASS

**Step 3: Commit**

```bash
git add internal/buildpool/coordinator.go
git commit -m "feat(buildpool): wire cancel function into coordinator"
```

---

## Epic 3: Worker Reconnection

Workers disconnect permanently if the coordinator restarts. They need automatic reconnection with exponential backoff.

### Task 3.1: Add Reconnection Loop to Worker

**Files:**
- Modify: `internal/buildworker/client.go` (add reconnection logic)

**Step 1: Write the test**

Add to `internal/buildworker/client_test.go`:

```go
func TestWorker_ReconnectBackoff(t *testing.T) {
	// Test backoff calculation
	delays := []time.Duration{
		calculateBackoff(0),
		calculateBackoff(1),
		calculateBackoff(2),
		calculateBackoff(3),
		calculateBackoff(10), // Should cap at max
	}

	if delays[0] != 1*time.Second {
		t.Errorf("backoff(0) = %v, want 1s", delays[0])
	}
	if delays[1] != 2*time.Second {
		t.Errorf("backoff(1) = %v, want 2s", delays[1])
	}
	if delays[2] != 4*time.Second {
		t.Errorf("backoff(2) = %v, want 4s", delays[2])
	}
	if delays[4] > 60*time.Second {
		t.Errorf("backoff(10) = %v, want <= 60s (capped)", delays[4])
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -run TestWorker_ReconnectBackoff ./internal/buildworker -v`
Expected: FAIL with "calculateBackoff undefined"

**Step 3: Add backoff calculation and reconnection logic**

In `internal/buildworker/client.go`, add the backoff function:

```go
const (
	initialBackoff = 1 * time.Second
	maxBackoff     = 60 * time.Second
	backoffFactor  = 2
)

func calculateBackoff(attempt int) time.Duration {
	delay := initialBackoff
	for i := 0; i < attempt; i++ {
		delay *= backoffFactor
		if delay > maxBackoff {
			return maxBackoff
		}
	}
	return delay
}
```

Add a `RunWithReconnect` method:

```go
// RunWithReconnect runs the worker with automatic reconnection
func (w *Worker) RunWithReconnect() error {
	attempt := 0

	for {
		select {
		case <-w.ctx.Done():
			return nil
		default:
		}

		// Try to connect
		err := w.Connect()
		if err != nil {
			delay := calculateBackoff(attempt)
			log.Printf("connection failed: %v, retrying in %v", err, delay)
			attempt++

			select {
			case <-w.ctx.Done():
				return nil
			case <-time.After(delay):
				continue
			}
		}

		// Connected - reset backoff
		attempt = 0
		log.Printf("connected to coordinator")

		// Run until disconnected
		err = w.Run()
		if err != nil {
			log.Printf("disconnected: %v", err)
		}

		// Don't reconnect if we're shutting down
		select {
		case <-w.ctx.Done():
			return nil
		default:
			// Will reconnect
		}
	}
}
```

**Step 4: Run test to verify it passes**

Run: `go test -run TestWorker_ReconnectBackoff ./internal/buildworker -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/buildworker/client.go internal/buildworker/client_test.go
git commit -m "feat(buildworker): add reconnection with exponential backoff"
```

---

### Task 3.2: Update build-agent Binary to Use Reconnection

**Files:**
- Modify: `cmd/build-agent/main.go` (use RunWithReconnect)

**Step 1: Update main.go**

In `cmd/build-agent/main.go`, change the run logic from:

```go
if err := worker.Connect(); err != nil {
	return fmt.Errorf("connect failed: %w", err)
}
log.Println("Connected to coordinator")

return worker.Run()
```

To:

```go
return worker.RunWithReconnect()
```

**Step 2: Build to verify**

Run: `go build ./cmd/build-agent`
Expected: Build succeeds

**Step 3: Commit**

```bash
git add cmd/build-agent/main.go
git commit -m "feat(build-agent): use reconnection loop"
```

---

## Epic 4: Git Daemon Security

The git daemon uses `--export-all` which exposes all repos. Add a listen address option.

### Task 4.1: Add Listen Address to Git Daemon

**Files:**
- Modify: `internal/buildpool/gitdaemon.go` (add ListenAddr config)

**Step 1: Write the test**

Add to `internal/buildpool/gitdaemon_test.go`:

```go
func TestGitDaemon_ListenAddr(t *testing.T) {
	tests := []struct {
		name       string
		listenAddr string
		wantInArgs bool
		wantValue  string
	}{
		{"default", "", false, ""},
		{"localhost", "127.0.0.1", true, "--listen=127.0.0.1"},
		{"any", "0.0.0.0", true, "--listen=0.0.0.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := NewGitDaemon(GitDaemonConfig{
				Port:       9418,
				BaseDir:    "/tmp",
				ListenAddr: tt.listenAddr,
			})

			args := d.Args()
			found := false
			for _, arg := range args {
				if arg == tt.wantValue {
					found = true
					break
				}
			}

			if tt.wantInArgs && !found {
				t.Errorf("Args() missing %q", tt.wantValue)
			}
			if !tt.wantInArgs && found {
				t.Errorf("Args() unexpectedly contains listen flag")
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -run TestGitDaemon_ListenAddr ./internal/buildpool -v`
Expected: FAIL (ListenAddr field doesn't exist)

**Step 3: Add ListenAddr to GitDaemonConfig**

In `internal/buildpool/gitdaemon.go`, modify the config:

```go
// GitDaemonConfig configures the git daemon
type GitDaemonConfig struct {
	Port       int
	BaseDir    string
	ListenAddr string // Optional: address to listen on (e.g., "127.0.0.1" for local only)
}
```

Modify `Args()`:

```go
// Args returns the command-line arguments for git daemon
func (d *GitDaemon) Args() []string {
	args := []string{
		"daemon",
		"--reuseaddr",
		fmt.Sprintf("--port=%d", d.config.Port),
		"--base-path", d.config.BaseDir,
		"--export-all",
		"--verbose",
	}

	if d.config.ListenAddr != "" {
		args = append(args, fmt.Sprintf("--listen=%s", d.config.ListenAddr))
	}

	args = append(args, d.config.BaseDir)
	return args
}
```

**Step 4: Run test to verify it passes**

Run: `go test -run TestGitDaemon_ListenAddr ./internal/buildpool -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/buildpool/gitdaemon.go internal/buildpool/gitdaemon_test.go
git commit -m "feat(gitdaemon): add ListenAddr option for security"
```

---

### Task 4.2: Expose ListenAddr in Config

**Files:**
- Modify: `internal/config/config.go` (add GitDaemonListenAddr)

**Step 1: Add to BuildPoolConfig**

In `internal/config/config.go`, modify `BuildPoolConfig`:

```go
type BuildPoolConfig struct {
	Enabled            bool                   `toml:"enabled"`
	WebSocketPort      int                    `toml:"websocket_port"`
	GitDaemonPort      int                    `toml:"git_daemon_port"`
	GitDaemonListenAddr string                `toml:"git_daemon_listen_addr"` // e.g., "127.0.0.1" for local only
	LocalFallback      LocalFallbackConfig    `toml:"local_fallback"`
	Timeouts           BuildPoolTimeoutConfig `toml:"timeouts"`
}
```

**Step 2: Wire into CLI commands**

In `cmd/claude-orch/commands.go`, find where `GitDaemon` is created in `buildPoolStartCmd` and update:

```go
gitDaemon := buildpool.NewGitDaemon(buildpool.GitDaemonConfig{
	Port:       cfg.BuildPool.GitDaemonPort,
	BaseDir:    cfg.ProjectRoot,
	ListenAddr: cfg.BuildPool.GitDaemonListenAddr,
})
```

**Step 3: Build and verify**

Run: `go build ./cmd/claude-orch`
Expected: Build succeeds

**Step 4: Commit**

```bash
git add internal/config/config.go cmd/claude-orch/commands.go
git commit -m "feat(config): expose git daemon listen address"
```

---

## Epic 5: Worker Status MCP Tool

The `worker_status` MCP tool returns hardcoded empty data. Wire it to the actual registry.

### Task 5.1: Inject Registry into MCP Server

**Files:**
- Modify: `internal/buildpool/mcp_server.go` (add registry, update workerStatus)

**Step 1: Write the test**

Add to `internal/buildpool/mcp_server_test.go`:

```go
func TestMCPServer_WorkerStatus_ReturnsRealData(t *testing.T) {
	registry := NewRegistry()
	dispatcher := NewDispatcher(registry, nil)

	// Register a worker
	registry.Register(&ConnectedWorker{
		ID:      "worker-1",
		MaxJobs: 4,
		Slots:   3,
	})

	server := NewMCPServer(MCPServerConfig{WorktreePath: "."}, dispatcher, registry)

	result, err := server.CallTool("worker_status", nil)
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	if !strings.Contains(result.Output, "worker-1") {
		t.Errorf("output missing worker-1: %s", result.Output)
	}
	if !strings.Contains(result.Output, `"max_jobs": 4`) {
		t.Errorf("output missing max_jobs: %s", result.Output)
	}
}
```

Add import `"strings"` if needed.

**Step 2: Run test to verify it fails**

Run: `go test -run TestMCPServer_WorkerStatus_ReturnsRealData ./internal/buildpool -v`
Expected: FAIL (NewMCPServer doesn't accept registry)

**Step 3: Update MCPServer to accept registry**

In `internal/buildpool/mcp_server.go`, modify the struct:

```go
// MCPServer implements the MCP protocol for build tools
type MCPServer struct {
	config     MCPServerConfig
	dispatcher *Dispatcher
	registry   *Registry
	repoURL    string
	commit     string
}
```

Modify `NewMCPServer`:

```go
// NewMCPServer creates a new MCP server
func NewMCPServer(config MCPServerConfig, dispatcher *Dispatcher, registry *Registry) *MCPServer {
	s := &MCPServer{
		config:     config,
		dispatcher: dispatcher,
		registry:   registry,
	}

	// Get repo URL and commit from worktree
	s.loadRepoInfo()

	return s
}
```

Update `workerStatus`:

```go
func (s *MCPServer) workerStatus() (*buildprotocol.JobResult, error) {
	workers := []map[string]interface{}{}

	if s.registry != nil {
		for _, w := range s.registry.All() {
			workers = append(workers, map[string]interface{}{
				"id":             w.ID,
				"max_jobs":       w.MaxJobs,
				"active_jobs":    w.MaxJobs - w.GetSlots(),
				"connected_since": w.ConnectedAt.Format(time.RFC3339),
			})
		}
	}

	queuedJobs := 0
	if s.dispatcher != nil {
		queuedJobs = s.dispatcher.QueueLength()
	}

	status := map[string]interface{}{
		"workers":               workers,
		"queued_jobs":           queuedJobs,
		"local_fallback_active": s.registry == nil || s.registry.Count() == 0,
	}

	output, _ := json.MarshalIndent(status, "", "  ")

	return &buildprotocol.JobResult{
		JobID:    "status",
		ExitCode: 0,
		Output:   string(output),
	}, nil
}
```

Add import `"time"` if not present.

**Step 4: Run test to verify it passes**

Run: `go test -run TestMCPServer_WorkerStatus_ReturnsRealData ./internal/buildpool -v`
Expected: PASS

**Step 5: Fix any other NewMCPServer calls**

Search for other uses and update. In tests, pass `nil` for registry if not needed.

**Step 6: Run all tests**

Run: `go test ./internal/buildpool -v`
Expected: All tests PASS

**Step 7: Commit**

```bash
git add internal/buildpool/mcp_server.go internal/buildpool/mcp_server_test.go
git commit -m "feat(mcp): wire worker_status to actual registry"
```

---

## Epic 6: Add ConnectedAt Field to Worker

The worker status needs to show when each worker connected.

### Task 6.1: Add ConnectedAt to ConnectedWorker

**Files:**
- Modify: `internal/buildpool/registry.go` (add ConnectedAt field)

**Step 1: Check if ConnectedAt exists**

Read `internal/buildpool/registry.go` to see if `ConnectedAt` exists. If not:

**Step 2: Add ConnectedAt field**

In `internal/buildpool/registry.go`, modify `ConnectedWorker`:

```go
// ConnectedWorker represents a connected worker
type ConnectedWorker struct {
	ID            string
	MaxJobs       int
	Slots         int
	Conn          *websocket.Conn
	ConnectedAt   time.Time
	lastHeartbeat time.Time
	mu            sync.Mutex
	writeMu       sync.Mutex
}
```

Add import `"time"` if not present.

**Step 3: Set ConnectedAt in Register**

In `Register` method or where workers are created, ensure `ConnectedAt` is set:

```go
func (r *Registry) Register(w *ConnectedWorker) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if w.ConnectedAt.IsZero() {
		w.ConnectedAt = time.Now()
	}
	r.workers[w.ID] = w
}
```

**Step 4: Run all tests**

Run: `go test ./internal/buildpool -v`
Expected: All tests PASS

**Step 5: Commit**

```bash
git add internal/buildpool/registry.go
git commit -m "feat(registry): add ConnectedAt timestamp to workers"
```

---

## Testing Checklist

After implementing all tasks, run these commands to verify:

```bash
# Run all unit tests
go test ./...

# Build both binaries
go build -o claude-orch ./cmd/claude-orch
go build -o build-agent ./cmd/build-agent

# Manual test: Start coordinator
./claude-orch build-pool start

# Manual test: Start worker with reconnection (in another terminal)
./build-agent --server ws://localhost:8081/ws --id test-worker

# Kill and restart coordinator - worker should reconnect automatically
```

---

## Summary

This plan covers 6 epics with 12 tasks:

1. **Output Accumulation** (2 tasks) - Accumulate streaming output from workers
2. **Job Cancellation** (5 tasks) - Track running jobs and cancel on request
3. **Worker Reconnection** (2 tasks) - Exponential backoff reconnection loop
4. **Git Daemon Security** (2 tasks) - Add listen address option
5. **Worker Status Tool** (1 task) - Wire to actual registry
6. **ConnectedAt Field** (1 task) - Add connection timestamp

**Open Items (not addressed):**
- Worker authentication beyond Tailscale
- Prometheus metrics endpoint
