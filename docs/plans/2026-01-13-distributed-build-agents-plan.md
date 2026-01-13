# Distributed Build Agents Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Offload build/clippy/test workloads from the central orchestrator to remote worker machines via WebSocket, with automatic fallback to local execution.

**Architecture:** Workers connect to a coordinator via WebSocket over Tailscale. Jobs are dispatched to ready workers who execute commands in isolated Nix shells + git worktrees. Claude Code agents interact via a new MCP server that routes requests through the coordinator.

**Tech Stack:** Go, gorilla/websocket, go-toml/v2, git daemon, Nix flakes

**Design Document:** `docs/plans/2026-01-13-distributed-build-agents-design.md`

---

## Epic 1: Build Protocol

Define the shared message types used by both coordinator and workers.

### Task 1.1: Message Types

**Files:**
- Create: `internal/buildprotocol/messages.go`
- Test: `internal/buildprotocol/messages_test.go`

**Step 1: Write the failing test**

```go
// internal/buildprotocol/messages_test.go
package buildprotocol

import (
	"encoding/json"
	"testing"
)

func TestRegisterMessage_Marshal(t *testing.T) {
	msg := RegisterMessage{
		WorkerID: "worker-1",
		MaxJobs:  4,
	}

	data, err := json.Marshal(Envelope{Type: "register", Payload: msg})
	if err != nil {
		t.Fatal(err)
	}

	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatal(err)
	}

	if env.Type != "register" {
		t.Errorf("got type %q, want %q", env.Type, "register")
	}
}

func TestJobMessage_Marshal(t *testing.T) {
	msg := JobMessage{
		JobID:   "job-123",
		Repo:    "git://central:9418/project",
		Commit:  "abc123",
		Command: "cargo test --lib",
	}

	data, err := json.Marshal(Envelope{Type: "job", Payload: msg})
	if err != nil {
		t.Fatal(err)
	}

	if len(data) == 0 {
		t.Error("expected non-empty JSON")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v ./internal/buildprotocol/...`
Expected: FAIL - package not found

**Step 3: Write the implementation**

```go
// internal/buildprotocol/messages.go
package buildprotocol

import "encoding/json"

// Envelope wraps all messages with a type discriminator
type Envelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// MarshalEnvelope creates an envelope with the given type and payload
func MarshalEnvelope(msgType string, payload interface{}) ([]byte, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return json.Marshal(Envelope{Type: msgType, Payload: payloadBytes})
}

// Worker → Coordinator messages

// RegisterMessage sent when worker first connects
type RegisterMessage struct {
	WorkerID string `json:"worker_id"`
	MaxJobs  int    `json:"max_jobs"`
}

// ReadyMessage sent when worker has available job slots
type ReadyMessage struct {
	Slots int `json:"slots"`
}

// OutputMessage sent for streaming command output
type OutputMessage struct {
	JobID  string `json:"job_id"`
	Stream string `json:"stream"` // "stdout" or "stderr"
	Data   string `json:"data"`
}

// CompleteMessage sent when job finishes
type CompleteMessage struct {
	JobID      string `json:"job_id"`
	ExitCode   int    `json:"exit_code"`
	DurationMs int64  `json:"duration_ms"`
}

// ErrorMessage sent when job fails before completion
type ErrorMessage struct {
	JobID   string `json:"job_id"`
	Message string `json:"message"`
}

// Coordinator → Worker messages

// JobMessage assigns work to a worker
type JobMessage struct {
	JobID   string            `json:"job_id"`
	Repo    string            `json:"repo"`
	Commit  string            `json:"commit"`
	Command string            `json:"command"`
	Env     map[string]string `json:"env,omitempty"`
	Timeout int               `json:"timeout_secs,omitempty"`
}

// CancelMessage requests job cancellation
type CancelMessage struct {
	JobID string `json:"job_id"`
}

// Message type constants
const (
	TypeRegister = "register"
	TypeReady    = "ready"
	TypeOutput   = "output"
	TypeComplete = "complete"
	TypeError    = "error"
	TypeJob      = "job"
	TypeCancel   = "cancel"
	TypePing     = "ping"
	TypePong     = "pong"
)
```

**Step 4: Run test to verify it passes**

Run: `go test -v ./internal/buildprotocol/...`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/buildprotocol/
git commit -m "feat(buildprotocol): add message types for worker-coordinator communication"
```

---

### Task 1.2: Job Result Type

**Files:**
- Modify: `internal/buildprotocol/messages.go`
- Modify: `internal/buildprotocol/messages_test.go`

**Step 1: Write the failing test**

```go
// Add to messages_test.go
func TestJobResult_ParseTestOutput(t *testing.T) {
	output := `running 5 tests
test test_one ... ok
test test_two ... ok
test test_three ... FAILED
test test_four ... ok
test test_five ... ignored

test result: FAILED. 3 passed; 1 failed; 1 ignored`

	result := JobResult{
		ExitCode: 1,
		Output:   output,
	}

	result.ParseTestOutput()

	if result.TestsPassed != 3 {
		t.Errorf("got passed=%d, want 3", result.TestsPassed)
	}
	if result.TestsFailed != 1 {
		t.Errorf("got failed=%d, want 1", result.TestsFailed)
	}
	if result.TestsIgnored != 1 {
		t.Errorf("got ignored=%d, want 1", result.TestsIgnored)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v ./internal/buildprotocol/... -run TestJobResult`
Expected: FAIL - JobResult undefined

**Step 3: Write the implementation**

```go
// Add to messages.go
import (
	"encoding/json"
	"regexp"
	"strconv"
)

// JobResult is the complete result returned to MCP callers
type JobResult struct {
	JobID        string `json:"job_id"`
	ExitCode     int    `json:"exit_code"`
	Output       string `json:"output"`
	DurationSecs float64 `json:"duration_secs"`

	// Parsed from test output (optional)
	TestsPassed  int `json:"tests_passed,omitempty"`
	TestsFailed  int `json:"tests_failed,omitempty"`
	TestsIgnored int `json:"tests_ignored,omitempty"`

	// Parsed from clippy output (optional)
	ClippyWarnings int `json:"clippy_warnings,omitempty"`
	ClippyErrors   int `json:"clippy_errors,omitempty"`
}

var testResultRegex = regexp.MustCompile(`(\d+) passed; (\d+) failed; (\d+) ignored`)

// ParseTestOutput extracts test counts from cargo test output
func (r *JobResult) ParseTestOutput() {
	matches := testResultRegex.FindStringSubmatch(r.Output)
	if len(matches) == 4 {
		r.TestsPassed, _ = strconv.Atoi(matches[1])
		r.TestsFailed, _ = strconv.Atoi(matches[2])
		r.TestsIgnored, _ = strconv.Atoi(matches[3])
	}
}
```

**Step 4: Run test to verify it passes**

Run: `go test -v ./internal/buildprotocol/... -run TestJobResult`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/buildprotocol/
git commit -m "feat(buildprotocol): add JobResult type with test output parsing"
```

---

## Epic 2: Build Worker Core

Implement job execution logic that runs in isolated worktrees + Nix shells.

### Task 2.1: Job Executor Interface

**Files:**
- Create: `internal/buildworker/executor.go`
- Test: `internal/buildworker/executor_test.go`

**Step 1: Write the failing test**

```go
// internal/buildworker/executor_test.go
package buildworker

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func setupTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	}

	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v failed: %s", args, out)
		}
	}

	// Create initial commit
	readme := filepath.Join(dir, "README.md")
	os.WriteFile(readme, []byte("# Test"), 0644)

	cmd := exec.Command("git", "add", ".")
	cmd.Dir = dir
	cmd.Run()

	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = dir
	cmd.Run()

	return dir
}

func TestExecutor_RunJob_SimpleCommand(t *testing.T) {
	repoDir := setupTestRepo(t)
	worktreeDir := t.TempDir()

	exec := NewExecutor(ExecutorConfig{
		GitCacheDir:  repoDir,
		WorktreeDir:  worktreeDir,
		UseNixShell:  false, // Skip nix for basic tests
	})

	// Get HEAD commit
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoDir
	commitBytes, _ := cmd.Output()
	commit := string(commitBytes[:len(commitBytes)-1])

	ctx := context.Background()
	result, err := exec.RunJob(ctx, Job{
		ID:      "test-job-1",
		Repo:    repoDir, // Use local path instead of git://
		Commit:  commit,
		Command: "echo hello",
	}, nil)

	if err != nil {
		t.Fatalf("RunJob failed: %v", err)
	}

	if result.ExitCode != 0 {
		t.Errorf("got exit code %d, want 0", result.ExitCode)
	}

	if result.Output != "hello\n" {
		t.Errorf("got output %q, want %q", result.Output, "hello\n")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v ./internal/buildworker/... -run TestExecutor_RunJob`
Expected: FAIL - package not found

**Step 3: Write the implementation**

```go
// internal/buildworker/executor.go
package buildworker

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/buildprotocol"
)

// Job represents a job to execute
type Job struct {
	ID      string
	Repo    string
	Commit  string
	Command string
	Env     map[string]string
	Timeout time.Duration
}

// OutputCallback is called for each line of output
type OutputCallback func(stream, data string)

// ExecutorConfig configures the job executor
type ExecutorConfig struct {
	GitCacheDir string
	WorktreeDir string
	UseNixShell bool
}

// Executor runs jobs in isolated worktrees
type Executor struct {
	config ExecutorConfig
}

// NewExecutor creates a new job executor
func NewExecutor(config ExecutorConfig) *Executor {
	return &Executor{config: config}
}

// RunJob executes a job and returns the result
func (e *Executor) RunJob(ctx context.Context, job Job, onOutput OutputCallback) (*buildprotocol.JobResult, error) {
	start := time.Now()

	// Create worktree for this job
	wtPath, err := e.createWorktree(job.ID, job.Repo, job.Commit)
	if err != nil {
		return nil, fmt.Errorf("creating worktree: %w", err)
	}
	defer e.removeWorktree(wtPath)

	// Build command
	var cmd *exec.Cmd
	if e.config.UseNixShell {
		cmd = exec.CommandContext(ctx, "nix", "develop", "--command", "sh", "-c", job.Command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", job.Command)
	}
	cmd.Dir = wtPath

	// Set environment
	cmd.Env = os.Environ()
	for k, v := range job.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// Capture output
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	var output strings.Builder

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting command: %w", err)
	}

	// Stream output
	done := make(chan struct{})
	go func() {
		e.streamOutput(stdout, "stdout", &output, onOutput)
		done <- struct{}{}
	}()
	go func() {
		e.streamOutput(stderr, "stderr", &output, onOutput)
		done <- struct{}{}
	}()

	<-done
	<-done

	err = cmd.Wait()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("command failed: %w", err)
		}
	}

	duration := time.Since(start)

	return &buildprotocol.JobResult{
		JobID:        job.ID,
		ExitCode:     exitCode,
		Output:       output.String(),
		DurationSecs: duration.Seconds(),
	}, nil
}

func (e *Executor) createWorktree(jobID, repo, commit string) (string, error) {
	// Ensure worktree directory exists
	if err := os.MkdirAll(e.config.WorktreeDir, 0755); err != nil {
		return "", err
	}

	suffix := randomSuffix()
	wtPath := filepath.Join(e.config.WorktreeDir, fmt.Sprintf("job-%s-%s", jobID, suffix))

	// For local repos (testing), just create worktree directly
	if !strings.HasPrefix(repo, "git://") {
		cmd := exec.Command("git", "worktree", "add", "--detach", wtPath, commit)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("git worktree add: %s: %w", out, err)
		}
		return wtPath, nil
	}

	// For remote repos, fetch first
	cmd := exec.Command("git", "fetch", repo, commit)
	cmd.Dir = e.config.GitCacheDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git fetch: %s: %w", out, err)
	}

	cmd = exec.Command("git", "worktree", "add", "--detach", wtPath, "FETCH_HEAD")
	cmd.Dir = e.config.GitCacheDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git worktree add: %s: %w", out, err)
	}

	return wtPath, nil
}

func (e *Executor) removeWorktree(wtPath string) error {
	cmd := exec.Command("git", "worktree", "remove", "--force", wtPath)
	cmd.Dir = e.config.GitCacheDir
	cmd.Run() // Best effort
	return nil
}

func (e *Executor) streamOutput(r io.Reader, stream string, output *strings.Builder, callback OutputCallback) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text() + "\n"
		output.WriteString(line)
		if callback != nil {
			callback(stream, line)
		}
	}
}

func randomSuffix() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}
```

**Step 4: Run test to verify it passes**

Run: `go test -v ./internal/buildworker/... -run TestExecutor_RunJob`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/buildworker/
git commit -m "feat(buildworker): add job executor with worktree isolation"
```

---

### Task 2.2: Job Pool Manager

**Files:**
- Create: `internal/buildworker/pool.go`
- Test: `internal/buildworker/pool_test.go`

**Step 1: Write the failing test**

```go
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
```

**Step 2: Run test to verify it fails**

Run: `go test -v ./internal/buildworker/... -run TestPool`
Expected: FAIL - Pool undefined

**Step 3: Write the implementation**

```go
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
```

**Step 4: Run test to verify it passes**

Run: `go test -v ./internal/buildworker/... -run TestPool`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/buildworker/
git commit -m "feat(buildworker): add job pool for slot management"
```

---

### Task 2.3: Worker Client

**Files:**
- Create: `internal/buildworker/client.go`
- Test: `internal/buildworker/client_test.go`

**Step 1: Write the failing test**

```go
// internal/buildworker/client_test.go
package buildworker

import (
	"testing"
)

func TestWorkerConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  WorkerConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: WorkerConfig{
				ServerURL:   "wss://localhost:8080/ws",
				WorkerID:    "worker-1",
				MaxJobs:     4,
				GitCacheDir: "/var/cache/repos",
				WorktreeDir: "/tmp/jobs",
			},
			wantErr: false,
		},
		{
			name: "missing server URL",
			config: WorkerConfig{
				WorkerID: "worker-1",
				MaxJobs:  4,
			},
			wantErr: true,
		},
		{
			name: "invalid max jobs",
			config: WorkerConfig{
				ServerURL: "wss://localhost:8080/ws",
				MaxJobs:   0,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v ./internal/buildworker/... -run TestWorkerConfig`
Expected: FAIL - WorkerConfig undefined

**Step 3: Write the implementation**

```go
// internal/buildworker/client.go
package buildworker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/buildprotocol"
)

// WorkerConfig configures the worker client
type WorkerConfig struct {
	ServerURL   string
	WorkerID    string
	MaxJobs     int
	GitCacheDir string
	WorktreeDir string
	UseNixShell bool
}

// Validate checks the config is valid
func (c *WorkerConfig) Validate() error {
	if c.ServerURL == "" {
		return fmt.Errorf("server_url is required")
	}
	if c.MaxJobs <= 0 {
		return fmt.Errorf("max_jobs must be positive")
	}
	return nil
}

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
}

// NewWorker creates a new worker client
func NewWorker(config WorkerConfig) (*Worker, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())

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
	}, nil
}

// Connect establishes connection to the coordinator
func (w *Worker) Connect() error {
	conn, _, err := websocket.DefaultDialer.Dial(w.config.ServerURL, nil)
	if err != nil {
		return fmt.Errorf("dial failed: %w", err)
	}
	w.conn = conn

	// Send register message
	return w.send(buildprotocol.TypeRegister, buildprotocol.RegisterMessage{
		WorkerID: w.config.WorkerID,
		MaxJobs:  w.config.MaxJobs,
	})
}

// Run starts the worker loop
func (w *Worker) Run() error {
	// Send initial ready message
	if err := w.sendReady(); err != nil {
		return err
	}

	for {
		select {
		case <-w.ctx.Done():
			return nil
		default:
		}

		_, message, err := w.conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read failed: %w", err)
		}

		var env buildprotocol.Envelope
		if err := json.Unmarshal(message, &env); err != nil {
			log.Printf("invalid message: %v", err)
			continue
		}

		switch env.Type {
		case buildprotocol.TypeJob:
			var job buildprotocol.JobMessage
			if err := json.Unmarshal(env.Payload, &job); err != nil {
				log.Printf("invalid job message: %v", err)
				continue
			}
			go w.handleJob(job)

		case buildprotocol.TypePing:
			w.send(buildprotocol.TypePong, nil)

		case buildprotocol.TypeCancel:
			var cancel buildprotocol.CancelMessage
			json.Unmarshal(env.Payload, &cancel)
			// TODO: implement job cancellation
		}
	}
}

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
		w.sendReady()
	}()

	timeout := time.Duration(jobMsg.Timeout) * time.Second
	if timeout == 0 {
		timeout = 5 * time.Minute
	}

	ctx, cancel := context.WithTimeout(w.ctx, timeout)
	defer cancel()

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

func (w *Worker) sendReady() error {
	return w.send(buildprotocol.TypeReady, buildprotocol.ReadyMessage{
		Slots: w.pool.Available(),
	})
}

func (w *Worker) send(msgType string, payload interface{}) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	data, err := buildprotocol.MarshalEnvelope(msgType, payload)
	if err != nil {
		return err
	}
	return w.conn.WriteMessage(websocket.TextMessage, data)
}

// Stop gracefully shuts down the worker
func (w *Worker) Stop() {
	w.cancel()
	if w.conn != nil {
		w.conn.Close()
	}
}
```

**Step 4: Run test to verify it passes**

Run: `go test -v ./internal/buildworker/... -run TestWorkerConfig`
Expected: PASS

**Step 5: Add gorilla/websocket dependency and commit**

```bash
go get github.com/gorilla/websocket
git add internal/buildworker/ go.mod go.sum
git commit -m "feat(buildworker): add worker client with WebSocket connection"
```

---

## Epic 3: Build Coordinator

Implement the central server that dispatches jobs to workers.

### Task 3.1: Worker Registry

**Files:**
- Create: `internal/buildpool/registry.go`
- Test: `internal/buildpool/registry_test.go`

**Step 1: Write the failing test**

```go
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
```

**Step 2: Run test to verify it fails**

Run: `go test -v ./internal/buildpool/... -run TestRegistry`
Expected: FAIL - package not found

**Step 3: Write the implementation**

```go
// internal/buildpool/registry.go
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
	for _, w := range r.workers {
		w.mu.Lock()
		slots := w.Slots
		w.mu.Unlock()

		if slots > 0 {
			if best == nil || slots > best.Slots {
				best = w
			}
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
```

**Step 4: Run test to verify it passes**

Run: `go test -v ./internal/buildpool/... -run TestRegistry`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/buildpool/
git commit -m "feat(buildpool): add worker registry for tracking connected workers"
```

---

### Task 3.2: Job Dispatcher

**Files:**
- Create: `internal/buildpool/dispatcher.go`
- Test: `internal/buildpool/dispatcher_test.go`

**Step 1: Write the failing test**

```go
// internal/buildpool/dispatcher_test.go
package buildpool

import (
	"testing"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/buildprotocol"
)

func TestDispatcher_SubmitJob(t *testing.T) {
	reg := NewRegistry()
	disp := NewDispatcher(reg, nil) // nil embedded worker for now

	job := &buildprotocol.JobMessage{
		JobID:   "job-1",
		Repo:    "git://localhost/repo",
		Commit:  "abc123",
		Command: "cargo test",
	}

	// With no workers, should queue the job
	resultCh := disp.Submit(job)

	// Check job is in queue
	if disp.QueueLength() != 1 {
		t.Errorf("got queue length=%d, want 1", disp.QueueLength())
	}

	// Result channel should not have anything yet
	select {
	case <-resultCh:
		t.Error("should not have result yet")
	default:
		// Expected
	}
}

func TestDispatcher_DispatchToWorker(t *testing.T) {
	reg := NewRegistry()

	// Track what job was sent to worker
	var sentJob *buildprotocol.JobMessage
	mockWorker := &ConnectedWorker{
		ID:      "worker-1",
		MaxJobs: 4,
		Slots:   2,
	}
	reg.Register(mockWorker)

	disp := NewDispatcher(reg, nil)
	disp.SetSendFunc(func(w *ConnectedWorker, job *buildprotocol.JobMessage) error {
		sentJob = job
		return nil
	})

	job := &buildprotocol.JobMessage{
		JobID:   "job-1",
		Repo:    "git://localhost/repo",
		Commit:  "abc123",
		Command: "cargo test",
	}

	disp.Submit(job)

	// Trigger dispatch
	disp.TryDispatch()

	if sentJob == nil {
		t.Fatal("job was not dispatched")
	}
	if sentJob.JobID != "job-1" {
		t.Errorf("got job ID=%s, want job-1", sentJob.JobID)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v ./internal/buildpool/... -run TestDispatcher`
Expected: FAIL - Dispatcher undefined

**Step 3: Write the implementation**

```go
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
```

**Step 4: Run test to verify it passes**

Run: `go test -v ./internal/buildpool/... -run TestDispatcher`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/buildpool/
git commit -m "feat(buildpool): add job dispatcher with queue and worker assignment"
```

---

### Task 3.3: WebSocket Coordinator Server

**Files:**
- Create: `internal/buildpool/coordinator.go`
- Test: `internal/buildpool/coordinator_test.go`

**Step 1: Write the failing test**

```go
// internal/buildpool/coordinator_test.go
package buildpool

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestCoordinator_AcceptWorker(t *testing.T) {
	coord := NewCoordinator(CoordinatorConfig{
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
```

**Step 2: Run test to verify it fails**

Run: `go test -v ./internal/buildpool/... -run TestCoordinator`
Expected: FAIL - Coordinator undefined

**Step 3: Write the implementation**

```go
// internal/buildpool/coordinator.go
package buildpool

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/buildprotocol"
)

// CoordinatorConfig configures the coordinator
type CoordinatorConfig struct {
	WebSocketPort     int
	GitDaemonPort     int
	HeartbeatInterval time.Duration
	HeartbeatTimeout  time.Duration
}

// Coordinator manages workers and dispatches jobs
type Coordinator struct {
	config     CoordinatorConfig
	registry   *Registry
	dispatcher *Dispatcher
	upgrader   websocket.Upgrader

	server *http.Server
	mu     sync.Mutex
}

// NewCoordinator creates a new coordinator
func NewCoordinator(config CoordinatorConfig) *Coordinator {
	if config.HeartbeatInterval == 0 {
		config.HeartbeatInterval = 30 * time.Second
	}
	if config.HeartbeatTimeout == 0 {
		config.HeartbeatTimeout = 10 * time.Second
	}

	registry := NewRegistry()

	c := &Coordinator{
		config:   config,
		registry: registry,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}

	c.dispatcher = NewDispatcher(registry, nil)
	c.dispatcher.SetSendFunc(c.sendJobToWorker)

	return c
}

// Registry returns the worker registry
func (c *Coordinator) Registry() *Registry {
	return c.registry
}

// Dispatcher returns the job dispatcher
func (c *Coordinator) Dispatcher() *Dispatcher {
	return c.dispatcher
}

// HandleWebSocket handles incoming WebSocket connections from workers
func (c *Coordinator) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := c.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("upgrade failed: %v", err)
		return
	}

	// Handle this connection
	go c.handleWorkerConnection(conn)
}

func (c *Coordinator) handleWorkerConnection(conn *websocket.Conn) {
	var workerID string
	defer func() {
		conn.Close()
		if workerID != "" {
			c.registry.Unregister(workerID)
			log.Printf("worker %s disconnected", workerID)
		}
	}()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("read error: %v", err)
			}
			return
		}

		var env buildprotocol.Envelope
		if err := json.Unmarshal(message, &env); err != nil {
			log.Printf("invalid message: %v", err)
			continue
		}

		switch env.Type {
		case buildprotocol.TypeRegister:
			var reg buildprotocol.RegisterMessage
			if err := json.Unmarshal(env.Payload, &reg); err != nil {
				log.Printf("invalid register: %v", err)
				continue
			}
			workerID = reg.WorkerID
			c.registry.Register(&ConnectedWorker{
				ID:      reg.WorkerID,
				MaxJobs: reg.MaxJobs,
				Slots:   reg.MaxJobs,
				Conn:    conn,
			})
			log.Printf("worker %s registered (max_jobs=%d)", reg.WorkerID, reg.MaxJobs)

		case buildprotocol.TypeReady:
			var ready buildprotocol.ReadyMessage
			if err := json.Unmarshal(env.Payload, &ready); err != nil {
				continue
			}
			if w := c.registry.Get(workerID); w != nil {
				w.UpdateSlots(ready.Slots)
				c.dispatcher.TryDispatch()
			}

		case buildprotocol.TypeOutput:
			var output buildprotocol.OutputMessage
			if err := json.Unmarshal(env.Payload, &output); err != nil {
				continue
			}
			// TODO: forward to MCP result stream

		case buildprotocol.TypeComplete:
			var complete buildprotocol.CompleteMessage
			if err := json.Unmarshal(env.Payload, &complete); err != nil {
				continue
			}
			c.dispatcher.Complete(complete.JobID, &buildprotocol.JobResult{
				JobID:        complete.JobID,
				ExitCode:     complete.ExitCode,
				DurationSecs: float64(complete.DurationMs) / 1000,
			})

		case buildprotocol.TypeError:
			var errMsg buildprotocol.ErrorMessage
			if err := json.Unmarshal(env.Payload, &errMsg); err != nil {
				continue
			}
			c.dispatcher.Complete(errMsg.JobID, &buildprotocol.JobResult{
				JobID:    errMsg.JobID,
				ExitCode: 1,
				Output:   "Error: " + errMsg.Message,
			})

		case buildprotocol.TypePong:
			if w := c.registry.Get(workerID); w != nil {
				w.LastHeartbeat = time.Now()
			}
		}
	}
}

func (c *Coordinator) sendJobToWorker(w *ConnectedWorker, job *buildprotocol.JobMessage) error {
	data, err := buildprotocol.MarshalEnvelope(buildprotocol.TypeJob, job)
	if err != nil {
		return err
	}
	return w.Conn.WriteMessage(websocket.TextMessage, data)
}

// Start starts the coordinator server
func (c *Coordinator) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", c.HandleWebSocket)

	addr := fmt.Sprintf(":%d", c.config.WebSocketPort)
	c.server = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go c.heartbeatLoop(ctx)

	log.Printf("coordinator listening on %s", addr)
	return c.server.ListenAndServe()
}

// Stop stops the coordinator server
func (c *Coordinator) Stop() error {
	if c.server != nil {
		return c.server.Close()
	}
	return nil
}

func (c *Coordinator) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(c.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.sendHeartbeats()
		}
	}
}

func (c *Coordinator) sendHeartbeats() {
	ping, _ := buildprotocol.MarshalEnvelope(buildprotocol.TypePing, nil)

	for _, w := range c.registry.All() {
		if err := w.Conn.WriteMessage(websocket.TextMessage, ping); err != nil {
			log.Printf("ping to %s failed: %v", w.ID, err)
		}
	}
}
```

**Step 4: Run test to verify it passes**

Run: `go test -v ./internal/buildpool/... -run TestCoordinator`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/buildpool/
git commit -m "feat(buildpool): add WebSocket coordinator server"
```

---

### Task 3.4: Git Daemon Manager

**Files:**
- Create: `internal/buildpool/gitdaemon.go`
- Test: `internal/buildpool/gitdaemon_test.go`

**Step 1: Write the failing test**

```go
// internal/buildpool/gitdaemon_test.go
package buildpool

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGitDaemon_Config(t *testing.T) {
	repoDir := t.TempDir()

	// Initialize a git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("git init failed: %v", err)
	}

	// Enable git-daemon-export
	exportFile := filepath.Join(repoDir, ".git", "git-daemon-export-ok")
	if err := os.WriteFile(exportFile, nil, 0644); err != nil {
		t.Fatalf("create export file: %v", err)
	}

	daemon := NewGitDaemon(GitDaemonConfig{
		Port:    9418,
		BaseDir: repoDir,
	})

	args := daemon.Args()

	// Check expected arguments
	hasPort := false
	hasBaseDir := false
	for i, arg := range args {
		if arg == "--port=9418" {
			hasPort = true
		}
		if arg == "--base-path" && i+1 < len(args) && args[i+1] == repoDir {
			hasBaseDir = true
		}
	}

	if !hasPort {
		t.Error("missing --port argument")
	}
	if !hasBaseDir {
		t.Error("missing --base-path argument")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v ./internal/buildpool/... -run TestGitDaemon`
Expected: FAIL - GitDaemon undefined

**Step 3: Write the implementation**

```go
// internal/buildpool/gitdaemon.go
package buildpool

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

// GitDaemonConfig configures the git daemon
type GitDaemonConfig struct {
	Port    int
	BaseDir string
}

// GitDaemon manages a git daemon process
type GitDaemon struct {
	config GitDaemonConfig
	cmd    *exec.Cmd
}

// NewGitDaemon creates a new git daemon manager
func NewGitDaemon(config GitDaemonConfig) *GitDaemon {
	if config.Port == 0 {
		config.Port = 9418
	}
	return &GitDaemon{config: config}
}

// Args returns the command-line arguments for git daemon
func (d *GitDaemon) Args() []string {
	return []string{
		"daemon",
		"--reuseaddr",
		fmt.Sprintf("--port=%d", d.config.Port),
		"--base-path", d.config.BaseDir,
		"--export-all",
		"--verbose",
		d.config.BaseDir,
	}
}

// Start starts the git daemon
func (d *GitDaemon) Start(ctx context.Context) error {
	// Ensure git-daemon-export-ok exists in .git directory
	gitDir := filepath.Join(d.config.BaseDir, ".git")
	if _, err := os.Stat(gitDir); err == nil {
		exportFile := filepath.Join(gitDir, "git-daemon-export-ok")
		if _, err := os.Stat(exportFile); os.IsNotExist(err) {
			if err := os.WriteFile(exportFile, nil, 0644); err != nil {
				return fmt.Errorf("creating git-daemon-export-ok: %w", err)
			}
		}
	}

	d.cmd = exec.CommandContext(ctx, "git", d.Args()...)
	d.cmd.Stdout = os.Stdout
	d.cmd.Stderr = os.Stderr

	if err := d.cmd.Start(); err != nil {
		return fmt.Errorf("starting git daemon: %w", err)
	}

	log.Printf("git daemon started on port %d, serving %s", d.config.Port, d.config.BaseDir)
	return nil
}

// Stop stops the git daemon
func (d *GitDaemon) Stop() error {
	if d.cmd != nil && d.cmd.Process != nil {
		return d.cmd.Process.Kill()
	}
	return nil
}

// Wait waits for the daemon to exit
func (d *GitDaemon) Wait() error {
	if d.cmd != nil {
		return d.cmd.Wait()
	}
	return nil
}

// Address returns the git:// URL for connecting
func (d *GitDaemon) Address(hostname string) string {
	return fmt.Sprintf("git://%s:%d", hostname, d.config.Port)
}
```

**Step 4: Run test to verify it passes**

Run: `go test -v ./internal/buildpool/... -run TestGitDaemon`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/buildpool/
git commit -m "feat(buildpool): add git daemon manager"
```

---

### Task 3.5: Embedded Worker

**Files:**
- Create: `internal/buildpool/embedded.go`
- Test: `internal/buildpool/embedded_test.go`

**Step 1: Write the failing test**

```go
// internal/buildpool/embedded_test.go
package buildpool

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/buildprotocol"
)

func TestEmbeddedWorker_RunJob(t *testing.T) {
	// Create a test repo
	repoDir := t.TempDir()
	worktreeDir := t.TempDir()

	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = repoDir
		cmd.Run()
	}

	os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Test"), 0644)
	cmd := exec.Command("git", "add", ".")
	cmd.Dir = repoDir
	cmd.Run()
	cmd = exec.Command("git", "commit", "-m", "init")
	cmd.Dir = repoDir
	cmd.Run()

	// Get commit
	cmd = exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoDir
	out, _ := cmd.Output()
	commit := string(out[:len(out)-1])

	embedded := NewEmbeddedWorker(EmbeddedConfig{
		RepoDir:     repoDir,
		WorktreeDir: worktreeDir,
		MaxJobs:     2,
		UseNixShell: false,
	})

	job := &buildprotocol.JobMessage{
		JobID:   "test-job",
		Repo:    repoDir,
		Commit:  commit,
		Command: "echo embedded-test",
	}

	result := embedded.Run(job)

	if result.ExitCode != 0 {
		t.Errorf("got exit code %d, want 0", result.ExitCode)
	}
	if result.Output != "embedded-test\n" {
		t.Errorf("got output %q, want %q", result.Output, "embedded-test\n")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v ./internal/buildpool/... -run TestEmbeddedWorker`
Expected: FAIL - EmbeddedWorker undefined

**Step 3: Write the implementation**

```go
// internal/buildpool/embedded.go
package buildpool

import (
	"context"
	"time"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/buildprotocol"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/buildworker"
)

// EmbeddedConfig configures the embedded worker
type EmbeddedConfig struct {
	RepoDir     string
	WorktreeDir string
	MaxJobs     int
	UseNixShell bool
}

// EmbeddedWorker runs jobs locally as fallback
type EmbeddedWorker struct {
	executor *buildworker.Executor
	pool     *buildworker.Pool
}

// NewEmbeddedWorker creates a new embedded worker
func NewEmbeddedWorker(config EmbeddedConfig) *EmbeddedWorker {
	return &EmbeddedWorker{
		executor: buildworker.NewExecutor(buildworker.ExecutorConfig{
			GitCacheDir: config.RepoDir,
			WorktreeDir: config.WorktreeDir,
			UseNixShell: config.UseNixShell,
		}),
		pool: buildworker.NewPool(config.MaxJobs),
	}
}

// Run executes a job and returns the result
func (e *EmbeddedWorker) Run(job *buildprotocol.JobMessage) *buildprotocol.JobResult {
	if !e.pool.Acquire() {
		return &buildprotocol.JobResult{
			JobID:    job.JobID,
			ExitCode: 1,
			Output:   "embedded worker: no slots available",
		}
	}
	defer e.pool.Release()

	timeout := time.Duration(job.Timeout) * time.Second
	if timeout == 0 {
		timeout = 5 * time.Minute
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	result, err := e.executor.RunJob(ctx, buildworker.Job{
		ID:      job.JobID,
		Repo:    job.Repo,
		Commit:  job.Commit,
		Command: job.Command,
		Env:     job.Env,
		Timeout: timeout,
	}, nil) // No streaming for embedded worker

	if err != nil {
		return &buildprotocol.JobResult{
			JobID:    job.JobID,
			ExitCode: 1,
			Output:   "embedded worker error: " + err.Error(),
		}
	}

	return result
}

// Available returns the number of available slots
func (e *EmbeddedWorker) Available() int {
	return e.pool.Available()
}
```

**Step 4: Run test to verify it passes**

Run: `go test -v ./internal/buildpool/... -run TestEmbeddedWorker`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/buildpool/
git commit -m "feat(buildpool): add embedded worker for local fallback"
```

---

## Epic 4: MCP Integration

Build the MCP server that Claude Code agents use.

### Task 4.1: Build MCP Server

**Files:**
- Create: `internal/buildpool/mcp_server.go`
- Test: `internal/buildpool/mcp_server_test.go`

**Step 1: Write the failing test**

```go
// internal/buildpool/mcp_server_test.go
package buildpool

import (
	"encoding/json"
	"testing"
)

func TestMCPServer_ToolsList(t *testing.T) {
	server := NewMCPServer(MCPServerConfig{
		WorktreePath: "/tmp/test-worktree",
	}, nil)

	tools := server.ListTools()

	expectedTools := []string{"build", "clippy", "test", "run_command", "worker_status"}

	if len(tools) != len(expectedTools) {
		t.Errorf("got %d tools, want %d", len(tools), len(expectedTools))
	}

	toolNames := make(map[string]bool)
	for _, tool := range tools {
		toolNames[tool.Name] = true
	}

	for _, name := range expectedTools {
		if !toolNames[name] {
			t.Errorf("missing tool: %s", name)
		}
	}
}

func TestMCPServer_BuildCommandArgs(t *testing.T) {
	server := NewMCPServer(MCPServerConfig{
		WorktreePath: "/tmp/test-worktree",
	}, nil)

	tests := []struct {
		name     string
		args     map[string]interface{}
		expected string
	}{
		{
			name:     "default build",
			args:     nil,
			expected: "cargo build",
		},
		{
			name:     "release build",
			args:     map[string]interface{}{"release": true},
			expected: "cargo build --release",
		},
		{
			name:     "with features",
			args:     map[string]interface{}{"features": []interface{}{"foo", "bar"}},
			expected: "cargo build --features foo,bar",
		},
		{
			name:     "specific package",
			args:     map[string]interface{}{"package": "mylib"},
			expected: "cargo build -p mylib",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := server.buildCommand("build", tt.args)
			if cmd != tt.expected {
				t.Errorf("got %q, want %q", cmd, tt.expected)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v ./internal/buildpool/... -run TestMCPServer`
Expected: FAIL - MCPServer undefined

**Step 3: Write the implementation**

```go
// internal/buildpool/mcp_server.go
package buildpool

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/buildprotocol"
)

// MCPServerConfig configures the MCP server
type MCPServerConfig struct {
	WorktreePath string
}

// MCPServer implements the MCP protocol for build tools
type MCPServer struct {
	config     MCPServerConfig
	dispatcher *Dispatcher
	repoURL    string
	commit     string
}

// MCPTool describes an available tool
type MCPTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// NewMCPServer creates a new MCP server
func NewMCPServer(config MCPServerConfig, dispatcher *Dispatcher) *MCPServer {
	s := &MCPServer{
		config:     config,
		dispatcher: dispatcher,
	}

	// Get repo URL and commit from worktree
	s.loadRepoInfo()

	return s
}

func (s *MCPServer) loadRepoInfo() {
	// Get current commit
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = s.config.WorktreePath
	if out, err := cmd.Output(); err == nil {
		s.commit = strings.TrimSpace(string(out))
	}

	// Get remote URL
	cmd = exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = s.config.WorktreePath
	if out, err := cmd.Output(); err == nil {
		s.repoURL = strings.TrimSpace(string(out))
	}
}

// ListTools returns available tools
func (s *MCPServer) ListTools() []MCPTool {
	return []MCPTool{
		{
			Name:        "build",
			Description: "Build the Rust project with cargo",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"release":  map[string]interface{}{"type": "boolean", "description": "Build in release mode"},
					"features": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
					"package":  map[string]interface{}{"type": "string", "description": "Specific package to build"},
				},
			},
		},
		{
			Name:        "clippy",
			Description: "Run clippy lints",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"fix":      map[string]interface{}{"type": "boolean", "description": "Apply suggested fixes"},
					"features": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
				},
			},
		},
		{
			Name:        "test",
			Description: "Run tests",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"filter":    map[string]interface{}{"type": "string", "description": "Test name filter"},
					"package":   map[string]interface{}{"type": "string", "description": "Specific package to test"},
					"features":  map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
					"nocapture": map[string]interface{}{"type": "boolean", "description": "Show stdout/stderr"},
				},
			},
		},
		{
			Name:        "run_command",
			Description: "Run an arbitrary shell command",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"command":      map[string]interface{}{"type": "string", "description": "Command to run"},
					"timeout_secs": map[string]interface{}{"type": "integer", "description": "Timeout in seconds"},
				},
				"required": []string{"command"},
			},
		},
		{
			Name:        "worker_status",
			Description: "Get status of connected workers",
			InputSchema: map[string]interface{}{
				"type": "object",
			},
		},
	}
}

func (s *MCPServer) buildCommand(tool string, args map[string]interface{}) string {
	var parts []string

	switch tool {
	case "build":
		parts = []string{"cargo", "build"}
		if release, ok := args["release"].(bool); ok && release {
			parts = append(parts, "--release")
		}
	case "clippy":
		parts = []string{"cargo", "clippy", "--all-targets", "--all-features", "--", "-D", "warnings"}
		if fix, ok := args["fix"].(bool); ok && fix {
			parts = []string{"cargo", "clippy", "--fix", "--all-targets", "--all-features"}
		}
	case "test":
		parts = []string{"cargo", "test"}
		if filter, ok := args["filter"].(string); ok && filter != "" {
			parts = append(parts, filter)
		}
		if nocapture, ok := args["nocapture"].(bool); ok && nocapture {
			parts = append(parts, "--", "--nocapture")
		}
	}

	// Common args
	if features, ok := args["features"].([]interface{}); ok && len(features) > 0 {
		featureStrs := make([]string, len(features))
		for i, f := range features {
			featureStrs[i] = f.(string)
		}
		// Insert before any -- args
		insertPos := len(parts)
		for i, p := range parts {
			if p == "--" {
				insertPos = i
				break
			}
		}
		newParts := make([]string, 0, len(parts)+2)
		newParts = append(newParts, parts[:insertPos]...)
		newParts = append(newParts, "--features", strings.Join(featureStrs, ","))
		newParts = append(newParts, parts[insertPos:]...)
		parts = newParts
	}

	if pkg, ok := args["package"].(string); ok && pkg != "" {
		parts = append(parts, "-p", pkg)
	}

	return strings.Join(parts, " ")
}

// CallTool executes a tool and returns the result
func (s *MCPServer) CallTool(name string, args map[string]interface{}) (*buildprotocol.JobResult, error) {
	var command string
	var timeout int

	switch name {
	case "build", "clippy", "test":
		command = s.buildCommand(name, args)
	case "run_command":
		command = args["command"].(string)
		if t, ok := args["timeout_secs"].(float64); ok {
			timeout = int(t)
		}
	case "worker_status":
		// Return worker status without dispatching a job
		return s.workerStatus()
	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}

	// Create job
	jobID := fmt.Sprintf("mcp-%d", os.Getpid())
	job := &buildprotocol.JobMessage{
		JobID:   jobID,
		Repo:    s.repoURL,
		Commit:  s.commit,
		Command: command,
		Timeout: timeout,
	}

	// Submit to dispatcher
	if s.dispatcher == nil {
		return nil, fmt.Errorf("no dispatcher configured")
	}

	resultCh := s.dispatcher.Submit(job)
	s.dispatcher.TryDispatch()

	// Wait for result
	result := <-resultCh

	// Parse output for test results
	if name == "test" {
		result.ParseTestOutput()
	}

	return result, nil
}

func (s *MCPServer) workerStatus() (*buildprotocol.JobResult, error) {
	// This would query the coordinator's registry
	// For now, return a placeholder
	status := map[string]interface{}{
		"workers":               []interface{}{},
		"queued_jobs":           0,
		"local_fallback_active": true,
	}

	output, _ := json.MarshalIndent(status, "", "  ")

	return &buildprotocol.JobResult{
		JobID:    "status",
		ExitCode: 0,
		Output:   string(output),
	}, nil
}

// Run starts the MCP server on stdin/stdout
func (s *MCPServer) Run() error {
	reader := bufio.NewReader(os.Stdin)

	for {
		line, err := reader.ReadBytes('\n')
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read error: %w", err)
		}

		var request map[string]interface{}
		if err := json.Unmarshal(line, &request); err != nil {
			continue
		}

		response := s.handleRequest(request)

		respBytes, _ := json.Marshal(response)
		fmt.Println(string(respBytes))
	}
}

func (s *MCPServer) handleRequest(req map[string]interface{}) map[string]interface{} {
	method, _ := req["method"].(string)
	id, _ := req["id"].(float64)

	switch method {
	case "initialize":
		return map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      id,
			"result": map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities": map[string]interface{}{
					"tools": map[string]interface{}{},
				},
				"serverInfo": map[string]interface{}{
					"name":    "build-pool",
					"version": "1.0.0",
				},
			},
		}

	case "tools/list":
		return map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      id,
			"result": map[string]interface{}{
				"tools": s.ListTools(),
			},
		}

	case "tools/call":
		params, _ := req["params"].(map[string]interface{})
		name, _ := params["name"].(string)
		args, _ := params["arguments"].(map[string]interface{})

		result, err := s.CallTool(name, args)
		if err != nil {
			return map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      id,
				"error": map[string]interface{}{
					"code":    -32000,
					"message": err.Error(),
				},
			}
		}

		return map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      id,
			"result": map[string]interface{}{
				"content": []map[string]interface{}{
					{"type": "text", "text": result.Output},
				},
			},
		}

	default:
		return map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      id,
			"error": map[string]interface{}{
				"code":    -32601,
				"message": "method not found",
			},
		}
	}
}
```

**Step 4: Run test to verify it passes**

Run: `go test -v ./internal/buildpool/... -run TestMCPServer`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/buildpool/
git commit -m "feat(buildpool): add MCP server for build/clippy/test tools"
```

---

## Epic 5: CLI & Configuration Integration

Wire everything into the orchestrator CLI.

### Task 5.1: Build Pool Config

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

**Step 1: Write the failing test**

```go
// Add to config_test.go
func TestConfig_BuildPoolDefaults(t *testing.T) {
	cfg := Default()

	if cfg.BuildPool.WebSocketPort != 8081 {
		t.Errorf("got websocket_port=%d, want 8081", cfg.BuildPool.WebSocketPort)
	}
	if cfg.BuildPool.GitDaemonPort != 9418 {
		t.Errorf("got git_daemon_port=%d, want 9418", cfg.BuildPool.GitDaemonPort)
	}
	if !cfg.BuildPool.LocalFallback.Enabled {
		t.Error("local fallback should be enabled by default")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v ./internal/config/... -run TestConfig_BuildPool`
Expected: FAIL - BuildPool undefined

**Step 3: Add BuildPoolConfig to config.go**

```go
// Add to config.go after WebConfig

// BuildPoolConfig holds build pool settings
type BuildPoolConfig struct {
	Enabled       bool                   `toml:"enabled"`
	WebSocketPort int                    `toml:"websocket_port"`
	GitDaemonPort int                    `toml:"git_daemon_port"`
	LocalFallback LocalFallbackConfig    `toml:"local_fallback"`
	Timeouts      BuildPoolTimeoutConfig `toml:"timeouts"`
}

// LocalFallbackConfig configures local job execution
type LocalFallbackConfig struct {
	Enabled     bool   `toml:"enabled"`
	MaxJobs     int    `toml:"max_jobs"`
	WorktreeDir string `toml:"worktree_dir"`
}

// BuildPoolTimeoutConfig configures timeouts
type BuildPoolTimeoutConfig struct {
	JobDefaultSecs       int `toml:"job_default_secs"`
	HeartbeatIntervalSecs int `toml:"heartbeat_interval_secs"`
	HeartbeatTimeoutSecs  int `toml:"heartbeat_timeout_secs"`
}

// Update Config struct to include BuildPool
type Config struct {
	General       GeneralConfig       `toml:"general"`
	Claude        ClaudeConfig        `toml:"claude"`
	Notifications NotificationsConfig `toml:"notifications"`
	Web           WebConfig           `toml:"web"`
	BuildPool     BuildPoolConfig     `toml:"build_pool"`
}

// Update Default() to include BuildPool defaults
func Default() *Config {
	home, _ := os.UserHomeDir()
	return &Config{
		// ... existing defaults ...
		BuildPool: BuildPoolConfig{
			Enabled:       false,
			WebSocketPort: 8081,
			GitDaemonPort: 9418,
			LocalFallback: LocalFallbackConfig{
				Enabled:     true,
				MaxJobs:     2,
				WorktreeDir: filepath.Join(home, ".claude-orchestrator", "build-pool", "local"),
			},
			Timeouts: BuildPoolTimeoutConfig{
				JobDefaultSecs:        300,
				HeartbeatIntervalSecs: 30,
				HeartbeatTimeoutSecs:  10,
			},
		},
	}
}
```

**Step 4: Run test to verify it passes**

Run: `go test -v ./internal/config/... -run TestConfig_BuildPool`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/config/
git commit -m "feat(config): add build pool configuration"
```

---

### Task 5.2: Build Pool CLI Commands

**Files:**
- Modify: `cmd/claude-orch/commands.go`

**Step 1: Add build-pool commands**

```go
// Add to init() in commands.go

// build-pool command group
buildPoolCmd := &cobra.Command{
	Use:   "build-pool",
	Short: "Manage the build worker pool",
}

buildPoolStartCmd := &cobra.Command{
	Use:   "start",
	Short: "Start the build pool coordinator",
	RunE:  runBuildPoolStart,
}

buildPoolStatusCmd := &cobra.Command{
	Use:   "status",
	Short: "Show build pool status",
	RunE:  runBuildPoolStatus,
}

buildPoolStopCmd := &cobra.Command{
	Use:   "stop",
	Short: "Stop the build pool coordinator",
	RunE:  runBuildPoolStop,
}

buildPoolCmd.AddCommand(buildPoolStartCmd, buildPoolStatusCmd, buildPoolStopCmd)
rootCmd.AddCommand(buildPoolCmd)
```

**Step 2: Implement command handlers**

```go
// Add to commands.go

func runBuildPoolStart(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	if !cfg.BuildPool.Enabled {
		fmt.Println("Build pool is not enabled in config. Set build_pool.enabled = true")
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create coordinator
	coord := buildpool.NewCoordinator(buildpool.CoordinatorConfig{
		WebSocketPort:     cfg.BuildPool.WebSocketPort,
		GitDaemonPort:     cfg.BuildPool.GitDaemonPort,
		HeartbeatInterval: time.Duration(cfg.BuildPool.Timeouts.HeartbeatIntervalSecs) * time.Second,
		HeartbeatTimeout:  time.Duration(cfg.BuildPool.Timeouts.HeartbeatTimeoutSecs) * time.Second,
	})

	// Set up embedded worker if enabled
	if cfg.BuildPool.LocalFallback.Enabled {
		embedded := buildpool.NewEmbeddedWorker(buildpool.EmbeddedConfig{
			RepoDir:     cfg.General.ProjectRoot,
			WorktreeDir: cfg.BuildPool.LocalFallback.WorktreeDir,
			MaxJobs:     cfg.BuildPool.LocalFallback.MaxJobs,
			UseNixShell: true,
		})
		coord.Dispatcher().SetEmbeddedWorker(embedded.Run)
	}

	// Start git daemon
	gitDaemon := buildpool.NewGitDaemon(buildpool.GitDaemonConfig{
		Port:    cfg.BuildPool.GitDaemonPort,
		BaseDir: cfg.General.ProjectRoot,
	})
	if err := gitDaemon.Start(ctx); err != nil {
		return fmt.Errorf("starting git daemon: %w", err)
	}

	fmt.Printf("Build pool coordinator starting...\n")
	fmt.Printf("  WebSocket: :%d\n", cfg.BuildPool.WebSocketPort)
	fmt.Printf("  Git daemon: :%d\n", cfg.BuildPool.GitDaemonPort)

	// Start coordinator (blocks)
	return coord.Start(ctx)
}

func runBuildPoolStatus(cmd *cobra.Command, args []string) error {
	// TODO: Connect to running coordinator and query status
	fmt.Println("Build pool status:")
	fmt.Println("  (status query not yet implemented)")
	return nil
}

func runBuildPoolStop(cmd *cobra.Command, args []string) error {
	// TODO: Signal running coordinator to stop
	fmt.Println("Stopping build pool...")
	fmt.Println("  (graceful stop not yet implemented)")
	return nil
}
```

**Step 3: Add import and commit**

```bash
git add cmd/claude-orch/
git commit -m "feat(cli): add build-pool commands"
```

---

### Task 5.3: Build Agent Binary

**Files:**
- Create: `cmd/build-agent/main.go`

**Step 1: Create the worker binary**

```go
// cmd/build-agent/main.go
package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/buildworker"
	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
)

var (
	configPath string
	serverURL  string
	workerID   string
	maxJobs    int
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "build-agent",
		Short: "Build worker agent that connects to a coordinator",
		RunE:  run,
	}

	rootCmd.Flags().StringVar(&configPath, "config", "", "Path to config file")
	rootCmd.Flags().StringVar(&serverURL, "server", "", "Coordinator WebSocket URL")
	rootCmd.Flags().StringVar(&workerID, "id", "", "Worker ID")
	rootCmd.Flags().IntVar(&maxJobs, "jobs", 4, "Maximum concurrent jobs")

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

type Config struct {
	Server struct {
		URL string `toml:"url"`
	} `toml:"server"`
	Worker struct {
		ID      string `toml:"id"`
		MaxJobs int    `toml:"max_jobs"`
	} `toml:"worker"`
	Storage struct {
		GitCacheDir string `toml:"git_cache_dir"`
		WorktreeDir string `toml:"worktree_dir"`
	} `toml:"storage"`
}

func run(cmd *cobra.Command, args []string) error {
	var cfg Config

	// Load config file if specified
	if configPath != "" {
		data, err := os.ReadFile(configPath)
		if err != nil {
			return fmt.Errorf("reading config: %w", err)
		}
		if err := toml.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("parsing config: %w", err)
		}
	}

	// CLI flags override config
	if serverURL != "" {
		cfg.Server.URL = serverURL
	}
	if workerID != "" {
		cfg.Worker.ID = workerID
	}
	if maxJobs > 0 {
		cfg.Worker.MaxJobs = maxJobs
	}

	// Defaults
	if cfg.Worker.MaxJobs == 0 {
		cfg.Worker.MaxJobs = 4
	}
	if cfg.Worker.ID == "" {
		hostname, _ := os.Hostname()
		cfg.Worker.ID = hostname
	}
	if cfg.Storage.GitCacheDir == "" {
		cfg.Storage.GitCacheDir = "/var/cache/build-agent/repos"
	}
	if cfg.Storage.WorktreeDir == "" {
		cfg.Storage.WorktreeDir = "/tmp/build-agent/jobs"
	}

	// Create worker
	worker, err := buildworker.NewWorker(buildworker.WorkerConfig{
		ServerURL:   cfg.Server.URL,
		WorkerID:    cfg.Worker.ID,
		MaxJobs:     cfg.Worker.MaxJobs,
		GitCacheDir: cfg.Storage.GitCacheDir,
		WorktreeDir: cfg.Storage.WorktreeDir,
		UseNixShell: true,
	})
	if err != nil {
		return fmt.Errorf("creating worker: %w", err)
	}

	// Connect
	fmt.Printf("Connecting to %s as %s (max_jobs=%d)...\n",
		cfg.Server.URL, cfg.Worker.ID, cfg.Worker.MaxJobs)

	if err := worker.Connect(); err != nil {
		return fmt.Errorf("connecting: %w", err)
	}

	fmt.Println("Connected. Waiting for jobs...")

	// Handle shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		fmt.Println("\nShutting down...")
		worker.Stop()
	}()

	// Run (blocks until stopped)
	return worker.Run()
}
```

**Step 2: Commit**

```bash
git add cmd/build-agent/
git commit -m "feat: add build-agent worker binary"
```

---

## Epic 6: TUI Integration

Add build pool visibility to the TUI.

### Task 6.1: Worker Status Panel

**Files:**
- Modify: `tui/model.go`
- Modify: `tui/view.go`

This task involves adding a workers panel to the existing TUI. The implementation details depend on the existing TUI structure.

**Step 1: Add WorkerView type**

```go
// Add to tui/model.go

// WorkerView represents a connected worker for display
type WorkerView struct {
	ID          string
	MaxJobs     int
	ActiveJobs  int
	ConnectedAt time.Time
}
```

**Step 2: Add workers to Model**

```go
// Add field to ModelConfig
type ModelConfig struct {
	// ... existing fields ...
	Workers []*WorkerView
}

// Add field to Model
type Model struct {
	// ... existing fields ...
	workers []*WorkerView
}
```

**Step 3: Add rendering in view**

```go
// Add to renderWorkers() in view.go
func (m Model) renderWorkers() string {
	if len(m.workers) == 0 {
		return "No workers connected (using local fallback)"
	}

	var sb strings.Builder
	sb.WriteString("Workers:\n")
	for _, w := range m.workers {
		sb.WriteString(fmt.Sprintf("  %s: %d/%d jobs\n",
			w.ID, w.ActiveJobs, w.MaxJobs))
	}
	return sb.String()
}
```

**Step 4: Commit**

```bash
git add tui/
git commit -m "feat(tui): add worker status panel"
```

---

## Epic 7: NixOS Module

Create NixOS module for worker deployment.

### Task 7.1: NixOS Module

**Files:**
- Create: `nix/build-agent.nix`

**Step 1: Create the module**

```nix
# nix/build-agent.nix
{ config, lib, pkgs, ... }:

with lib;

let
  cfg = config.services.build-agent;

  configFile = pkgs.writeText "build-agent.toml" ''
    [server]
    url = "${cfg.serverUrl}"

    [worker]
    id = "${cfg.workerId}"
    max_jobs = ${toString cfg.maxJobs}

    [storage]
    git_cache_dir = "${cfg.gitCacheDir}"
    worktree_dir = "${cfg.worktreeDir}"
  '';
in {
  options.services.build-agent = {
    enable = mkEnableOption "build agent worker";

    serverUrl = mkOption {
      type = types.str;
      description = "WebSocket URL of the coordinator";
      example = "wss://central-server:8080/ws";
    };

    workerId = mkOption {
      type = types.str;
      default = config.networking.hostName;
      description = "Unique worker identifier";
    };

    maxJobs = mkOption {
      type = types.int;
      default = 4;
      description = "Maximum concurrent jobs";
    };

    gitCacheDir = mkOption {
      type = types.str;
      default = "/var/cache/build-agent/repos";
      description = "Directory for git cache";
    };

    worktreeDir = mkOption {
      type = types.str;
      default = "/tmp/build-agent/jobs";
      description = "Directory for job worktrees";
    };

    package = mkOption {
      type = types.package;
      description = "build-agent package to use";
    };
  };

  config = mkIf cfg.enable {
    systemd.services.build-agent = {
      description = "Build Agent Worker";
      wantedBy = [ "multi-user.target" ];
      after = [ "network-online.target" ];
      wants = [ "network-online.target" ];

      serviceConfig = {
        Type = "simple";
        ExecStart = "${cfg.package}/bin/build-agent --config ${configFile}";
        Restart = "always";
        RestartSec = 10;

        # Security hardening
        NoNewPrivileges = true;
        ProtectSystem = "strict";
        ReadWritePaths = [ cfg.gitCacheDir cfg.worktreeDir ];

        # Create directories
        StateDirectory = "build-agent";
        CacheDirectory = "build-agent";
      };
    };

    # Ensure directories exist
    systemd.tmpfiles.rules = [
      "d ${cfg.gitCacheDir} 0755 root root -"
      "d ${cfg.worktreeDir} 0755 root root -"
    ];

    # Ensure nix with flakes is available
    nix.settings.experimental-features = [ "nix-command" "flakes" ];
  };
}
```

**Step 2: Commit**

```bash
git add nix/
git commit -m "feat(nix): add NixOS module for build-agent"
```

---

## Testing Checklist

After implementing all epics, run these commands to verify:

```bash
# Run all unit tests
go test ./...

# Build both binaries
go build -o claude-orch ./cmd/claude-orch
go build -o build-agent ./cmd/build-agent

# Start coordinator (in one terminal)
./claude-orch build-pool start

# Start worker (in another terminal)
./build-agent --server wss://localhost:8081/ws --id test-worker

# Verify connection in coordinator logs
```

---

## Summary

This plan covers 7 epics with detailed, atomic tasks:

1. **Build Protocol** (2 tasks) - Message types for worker-coordinator communication
2. **Build Worker Core** (3 tasks) - Job executor, pool, WebSocket client
3. **Build Coordinator** (5 tasks) - Registry, dispatcher, WebSocket server, git daemon, embedded worker
4. **MCP Integration** (1 task) - MCP server for build/clippy/test tools
5. **CLI Integration** (3 tasks) - Config, CLI commands, worker binary
6. **TUI Integration** (1 task) - Worker status panel
7. **NixOS Module** (1 task) - Deployment configuration

Each task follows TDD: write failing test, implement, verify, commit.
