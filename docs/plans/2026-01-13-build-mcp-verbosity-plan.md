# Build-MCP Verbosity Modes Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add three verbosity levels (minimal, normal, full) to build-mcp tools to reduce LLM context window clutter, plus a get_job_logs tool for retrieving complete logs.

**Architecture:** Modify the coordinator to accumulate stdout/stderr separately, add a ring buffer for log retention, and filter output based on verbosity level at retrieval time. Add verbosity parameter to all MCP tools and a new get_job_logs tool.

**Tech Stack:** Go, gorilla/websocket, MCP protocol (JSON-RPC over stdio)

---

### Task 1: Add Verbosity Constants and Update JobResult

**Files:**
- Modify: `internal/buildprotocol/messages.go:94-109`
- Test: `internal/buildprotocol/messages_test.go`

**Step 1: Write the failing test**

Add to `internal/buildprotocol/messages_test.go`:

```go
func TestVerbosityConstants(t *testing.T) {
	// Verify constants are defined
	if VerbosityMinimal != "minimal" {
		t.Errorf("VerbosityMinimal = %q, want %q", VerbosityMinimal, "minimal")
	}
	if VerbosityNormal != "normal" {
		t.Errorf("VerbosityNormal = %q, want %q", VerbosityNormal, "normal")
	}
	if VerbosityFull != "full" {
		t.Errorf("VerbosityFull = %q, want %q", VerbosityFull, "full")
	}
}

func TestJobResultSeparateStreams(t *testing.T) {
	result := JobResult{
		JobID:    "test-123",
		ExitCode: 0,
		Stdout:   "build output",
		Stderr:   "warnings here",
	}

	if result.Stdout != "build output" {
		t.Errorf("Stdout = %q, want %q", result.Stdout, "build output")
	}
	if result.Stderr != "warnings here" {
		t.Errorf("Stderr = %q, want %q", result.Stderr, "warnings here")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -run "TestVerbosityConstants|TestJobResultSeparateStreams" ./internal/buildprotocol/...`
Expected: FAIL with "undefined: VerbosityMinimal" and "unknown field Stdout"

**Step 3: Write minimal implementation**

In `internal/buildprotocol/messages.go`, add after line 92:

```go
// Verbosity levels for MCP tool output
const (
	VerbosityMinimal = "minimal" // Exit code + stderr only (default)
	VerbosityNormal  = "normal"  // Exit code + stderr + last 50 lines stdout
	VerbosityFull    = "full"    // Exit code + all output
)
```

Then update `JobResult` struct (lines 94-109):

```go
// JobResult is the complete result returned to MCP callers
type JobResult struct {
	JobID        string  `json:"job_id"`
	ExitCode     int     `json:"exit_code"`
	Stdout       string  `json:"stdout,omitempty"`
	Stderr       string  `json:"stderr,omitempty"`
	Output       string  `json:"output,omitempty"` // Deprecated: kept for backwards compat
	DurationSecs float64 `json:"duration_secs"`

	// Parsed from test output (optional)
	TestsPassed  int `json:"tests_passed,omitempty"`
	TestsFailed  int `json:"tests_failed,omitempty"`
	TestsIgnored int `json:"tests_ignored,omitempty"`

	// Parsed from clippy output (optional)
	ClippyWarnings int `json:"clippy_warnings,omitempty"`
	ClippyErrors   int `json:"clippy_errors,omitempty"`
}
```

**Step 4: Run test to verify it passes**

Run: `go test -run "TestVerbosityConstants|TestJobResultSeparateStreams" ./internal/buildprotocol/...`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/buildprotocol/messages.go internal/buildprotocol/messages_test.go
git commit -m "feat(buildprotocol): add verbosity constants and separate stdout/stderr in JobResult"
```

---

### Task 2: Update Coordinator to Separate Stdout/Stderr Buffers

**Files:**
- Modify: `internal/buildpool/coordinator.go:37-40,388-410`
- Test: `internal/buildpool/coordinator_test.go`

**Step 1: Write the failing test**

Add to `internal/buildpool/coordinator_test.go`:

```go
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
```

**Step 2: Run test to verify it fails**

Run: `go test -run TestCoordinator_SeparateStreamAccumulation ./internal/buildpool/...`
Expected: FAIL with "coord.GetSeparateOutput undefined"

**Step 3: Write minimal implementation**

In `internal/buildpool/coordinator.go`, replace the output buffer fields (lines 37-40):

```go
	// Output accumulator for streaming output from workers
	outputMu     sync.Mutex
	outputBuffer map[string]*jobOutput
}

// jobOutput holds separate stdout and stderr buffers
type jobOutput struct {
	stdout strings.Builder
	stderr strings.Builder
}
```

Update `NewCoordinator` (around line 58):

```go
		outputBuffer: make(map[string]*jobOutput),
```

Update `AccumulateOutput` (lines 388-397):

```go
// AccumulateOutput appends output for a job, tracking streams separately
func (c *Coordinator) AccumulateOutput(jobID, stream, data string) {
	c.outputMu.Lock()
	defer c.outputMu.Unlock()

	if c.outputBuffer[jobID] == nil {
		c.outputBuffer[jobID] = &jobOutput{}
	}
	if stream == "stderr" {
		c.outputBuffer[jobID].stderr.WriteString(data)
	} else {
		c.outputBuffer[jobID].stdout.WriteString(data)
	}
}
```

Update `GetAndClearOutput` (lines 399-410):

```go
// GetAndClearOutput returns accumulated output and clears the buffer (backwards compat)
func (c *Coordinator) GetAndClearOutput(jobID string) string {
	c.outputMu.Lock()
	defer c.outputMu.Unlock()

	if buf, ok := c.outputBuffer[jobID]; ok {
		output := buf.stdout.String() + buf.stderr.String()
		delete(c.outputBuffer, jobID)
		return output
	}
	return ""
}

// GetSeparateOutput returns stdout and stderr separately and clears the buffer
func (c *Coordinator) GetSeparateOutput(jobID string) (stdout, stderr string) {
	c.outputMu.Lock()
	defer c.outputMu.Unlock()

	if buf, ok := c.outputBuffer[jobID]; ok {
		stdout = buf.stdout.String()
		stderr = buf.stderr.String()
		delete(c.outputBuffer, jobID)
	}
	return
}
```

**Step 4: Run test to verify it passes**

Run: `go test -run TestCoordinator_SeparateStreamAccumulation ./internal/buildpool/...`
Expected: PASS

**Step 5: Run existing tests to ensure no regression**

Run: `go test ./internal/buildpool/...`
Expected: PASS (existing test `TestCoordinator_OutputAccumulation` should still pass)

**Step 6: Commit**

```bash
git add internal/buildpool/coordinator.go internal/buildpool/coordinator_test.go
git commit -m "feat(coordinator): separate stdout/stderr accumulation buffers"
```

---

### Task 3: Add Log Retention Ring Buffer

**Files:**
- Modify: `internal/buildpool/coordinator.go`
- Test: `internal/buildpool/coordinator_test.go`

**Step 1: Write the failing test**

Add to `internal/buildpool/coordinator_test.go`:

```go
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
```

**Step 2: Run test to verify it fails**

Run: `go test -run "TestCoordinator_LogRetention" ./internal/buildpool/...`
Expected: FAIL with "coord.RetainLogs undefined"

**Step 3: Write minimal implementation**

Add to `Coordinator` struct in `coordinator.go`:

```go
	// Log retention ring buffer for completed jobs
	retainedLogs [50]*completedLog
	retainIndex  int
	retainByID   map[string]*completedLog
```

Add type definition:

```go
// completedLog holds logs for a completed job
type completedLog struct {
	jobID  string
	stdout string
	stderr string
}
```

Update `NewCoordinator` to initialize:

```go
		retainByID:   make(map[string]*completedLog),
```

Add retention methods:

```go
// RetainLogs moves logs from active buffer to retention ring buffer
func (c *Coordinator) RetainLogs(jobID string) {
	c.outputMu.Lock()
	defer c.outputMu.Unlock()

	buf, ok := c.outputBuffer[jobID]
	if !ok {
		return
	}

	// Evict old entry at this index if present
	if old := c.retainedLogs[c.retainIndex]; old != nil {
		delete(c.retainByID, old.jobID)
	}

	// Store new entry
	entry := &completedLog{
		jobID:  jobID,
		stdout: buf.stdout.String(),
		stderr: buf.stderr.String(),
	}
	c.retainedLogs[c.retainIndex] = entry
	c.retainByID[jobID] = entry
	c.retainIndex = (c.retainIndex + 1) % 50

	// Clear from active buffer
	delete(c.outputBuffer, jobID)
}

// GetRetainedLogs retrieves logs from retention buffer
func (c *Coordinator) GetRetainedLogs(jobID string) (stdout, stderr string, found bool) {
	c.outputMu.Lock()
	defer c.outputMu.Unlock()

	if entry, ok := c.retainByID[jobID]; ok {
		return entry.stdout, entry.stderr, true
	}
	return "", "", false
}
```

**Step 4: Run test to verify it passes**

Run: `go test -run "TestCoordinator_LogRetention" ./internal/buildpool/...`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/buildpool/coordinator.go internal/buildpool/coordinator_test.go
git commit -m "feat(coordinator): add log retention ring buffer (50 jobs)"
```

---

### Task 4: Add Verbosity Filtering Function

**Files:**
- Modify: `internal/buildpool/coordinator.go`
- Test: `internal/buildpool/coordinator_test.go`

**Step 1: Write the failing test**

Add to `internal/buildpool/coordinator_test.go`:

```go
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
```

**Step 2: Run test to verify it fails**

Run: `go test -run TestCoordinator_VerbosityFiltering ./internal/buildpool/...`
Expected: FAIL with "coord.FilterOutput undefined"

**Step 3: Write minimal implementation**

Add to `coordinator.go`:

```go
// FilterOutput applies verbosity filtering to stdout/stderr
func (c *Coordinator) FilterOutput(stdout, stderr, verbosity string) *buildprotocol.JobResult {
	result := &buildprotocol.JobResult{
		Stderr: stderr,
	}

	switch verbosity {
	case buildprotocol.VerbosityFull:
		result.Stdout = stdout
	case buildprotocol.VerbosityNormal:
		result.Stdout = tailLines(stdout, 50)
	default: // minimal or empty
		// Only stderr, no stdout
	}

	return result
}

// tailLines returns the last n lines of s
func tailLines(s string, n int) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[len(lines)-n:], "\n") + "\n"
}
```

**Step 4: Run test to verify it passes**

Run: `go test -run TestCoordinator_VerbosityFiltering ./internal/buildpool/...`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/buildpool/coordinator.go internal/buildpool/coordinator_test.go
git commit -m "feat(coordinator): add verbosity filtering for output"
```

---

### Task 5: Wire Verbosity Through Job Completion

**Files:**
- Modify: `internal/buildpool/coordinator.go:169-181`
- Modify: `internal/buildpool/dispatcher.go`
- Test: `internal/buildpool/coordinator_test.go`

**Step 1: Write the failing test**

Add to `internal/buildpool/coordinator_test.go`:

```go
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
```

**Step 2: Run test to verify it fails**

Run: `go test -run TestCoordinator_CompleteWithVerbosity ./internal/buildpool/...`
Expected: FAIL with "coord.CompleteJob undefined"

**Step 3: Write minimal implementation**

Add to `coordinator.go`:

```go
// CompleteJob creates a JobResult with verbosity filtering and retains logs
func (c *Coordinator) CompleteJob(jobID string, exitCode int, durationMs int64, verbosity string) *buildprotocol.JobResult {
	stdout, stderr := c.GetSeparateOutput(jobID)

	// Always retain full logs before filtering
	c.outputMu.Lock()
	if old := c.retainedLogs[c.retainIndex]; old != nil {
		delete(c.retainByID, old.jobID)
	}
	entry := &completedLog{
		jobID:  jobID,
		stdout: stdout,
		stderr: stderr,
	}
	c.retainedLogs[c.retainIndex] = entry
	c.retainByID[jobID] = entry
	c.retainIndex = (c.retainIndex + 1) % 50
	c.outputMu.Unlock()

	// Apply verbosity filtering
	result := c.FilterOutput(stdout, stderr, verbosity)
	result.JobID = jobID
	result.ExitCode = exitCode
	result.DurationSecs = float64(durationMs) / 1000

	// Keep backwards-compat Output field
	result.Output = stdout + stderr

	return result
}
```

Update the `TypeComplete` handler (around line 176) to store verbosity - but first we need to track verbosity per job. Update `PendingJob` in `dispatcher.go`:

```go
// PendingJob tracks a job waiting for dispatch or completion
type PendingJob struct {
	Job       *buildprotocol.JobMessage
	ResultCh  chan *buildprotocol.JobResult
	WorkerID  string // Assigned worker (empty if queued)
	Verbosity string // Output verbosity level
}
```

**Step 4: Run test to verify it passes**

Run: `go test -run TestCoordinator_CompleteWithVerbosity ./internal/buildpool/...`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/buildpool/coordinator.go internal/buildpool/dispatcher.go internal/buildpool/coordinator_test.go
git commit -m "feat(coordinator): wire verbosity through job completion"
```

---

### Task 6: Add Verbosity Parameter to MCP Tools

**Files:**
- Modify: `internal/buildpool/mcp_server.go:77-135,211-262`
- Test: `internal/buildpool/mcp_server_test.go`

**Step 1: Write the failing test**

Add to `internal/buildpool/mcp_server_test.go`:

```go
func TestMCPServer_ToolsHaveVerbosityParam(t *testing.T) {
	server := NewMCPServer(MCPServerConfig{
		WorktreePath: "/tmp/test-worktree",
	}, nil, nil)

	tools := server.ListTools()

	toolsWithVerbosity := []string{"build", "clippy", "test", "run_command"}

	for _, tool := range tools {
		shouldHave := false
		for _, name := range toolsWithVerbosity {
			if tool.Name == name {
				shouldHave = true
				break
			}
		}

		if !shouldHave {
			continue
		}

		props, ok := tool.InputSchema["properties"].(map[string]interface{})
		if !ok {
			t.Errorf("tool %s: properties not found", tool.Name)
			continue
		}

		verbosity, ok := props["verbosity"].(map[string]interface{})
		if !ok {
			t.Errorf("tool %s: missing verbosity property", tool.Name)
			continue
		}

		enum, ok := verbosity["enum"].([]string)
		if !ok {
			t.Errorf("tool %s: verbosity missing enum", tool.Name)
			continue
		}

		expected := []string{"minimal", "normal", "full"}
		if len(enum) != len(expected) {
			t.Errorf("tool %s: verbosity enum = %v, want %v", tool.Name, enum, expected)
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -run TestMCPServer_ToolsHaveVerbosityParam ./internal/buildpool/...`
Expected: FAIL with "missing verbosity property"

**Step 3: Write minimal implementation**

Update `ListTools()` in `mcp_server.go` to add verbosity to each tool. Add a helper:

```go
var verbositySchema = map[string]interface{}{
	"type":        "string",
	"description": "Output verbosity: minimal (default), normal, full",
	"enum":        []string{"minimal", "normal", "full"},
}
```

Then add `"verbosity": verbositySchema` to each tool's properties (build, clippy, test, run_command).

**Step 4: Run test to verify it passes**

Run: `go test -run TestMCPServer_ToolsHaveVerbosityParam ./internal/buildpool/...`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/buildpool/mcp_server.go internal/buildpool/mcp_server_test.go
git commit -m "feat(mcp): add verbosity parameter to tool schemas"
```

---

### Task 7: Add get_job_logs Tool

**Files:**
- Modify: `internal/buildpool/mcp_server.go`
- Test: `internal/buildpool/mcp_server_test.go`

**Step 1: Write the failing test**

Add to `internal/buildpool/mcp_server_test.go`:

```go
func TestMCPServer_GetJobLogsTool(t *testing.T) {
	server := NewMCPServer(MCPServerConfig{
		WorktreePath: "/tmp/test-worktree",
	}, nil, nil)

	tools := server.ListTools()

	var found bool
	for _, tool := range tools {
		if tool.Name == "get_job_logs" {
			found = true
			props := tool.InputSchema["properties"].(map[string]interface{})
			if _, ok := props["job_id"]; !ok {
				t.Error("get_job_logs missing job_id property")
			}
			if _, ok := props["stream"]; !ok {
				t.Error("get_job_logs missing stream property")
			}
			required := tool.InputSchema["required"].([]string)
			if len(required) != 1 || required[0] != "job_id" {
				t.Errorf("get_job_logs required = %v, want [job_id]", required)
			}
			break
		}
	}

	if !found {
		t.Error("get_job_logs tool not found")
	}
}

func TestMCPServer_GetJobLogsExecution(t *testing.T) {
	registry := NewRegistry()
	dispatcher := NewDispatcher(registry, nil)
	coord := NewCoordinator(CoordinatorConfig{WebSocketPort: 0}, registry, dispatcher)

	server := NewMCPServer(MCPServerConfig{WorktreePath: "."}, dispatcher, registry)
	server.SetCoordinator(coord)

	// Simulate a completed job with retained logs
	coord.AccumulateOutput("test-job-logs", "stdout", "stdout content")
	coord.AccumulateOutput("test-job-logs", "stderr", "stderr content")
	coord.RetainLogs("test-job-logs")

	// Call get_job_logs
	result, err := server.CallTool("get_job_logs", map[string]interface{}{
		"job_id": "test-job-logs",
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	if !strings.Contains(result.Output, "stdout content") {
		t.Errorf("output missing stdout: %s", result.Output)
	}
	if !strings.Contains(result.Output, "stderr content") {
		t.Errorf("output missing stderr: %s", result.Output)
	}
}

func TestMCPServer_GetJobLogsNotFound(t *testing.T) {
	registry := NewRegistry()
	dispatcher := NewDispatcher(registry, nil)
	coord := NewCoordinator(CoordinatorConfig{WebSocketPort: 0}, registry, dispatcher)

	server := NewMCPServer(MCPServerConfig{WorktreePath: "."}, dispatcher, registry)
	server.SetCoordinator(coord)

	result, err := server.CallTool("get_job_logs", map[string]interface{}{
		"job_id": "nonexistent",
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	if result.ExitCode != 1 {
		t.Errorf("exit code = %d, want 1 for not found", result.ExitCode)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -run "TestMCPServer_GetJobLogs" ./internal/buildpool/...`
Expected: FAIL with "get_job_logs tool not found"

**Step 3: Write minimal implementation**

Add to `MCPServer` struct:

```go
	coordinator *Coordinator
```

Add setter:

```go
// SetCoordinator sets the coordinator for log retrieval
func (s *MCPServer) SetCoordinator(c *Coordinator) {
	s.coordinator = c
}
```

Add to `ListTools()`:

```go
		{
			Name:        "get_job_logs",
			Description: "Retrieve full logs for a completed job",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"job_id": map[string]interface{}{
						"type":        "string",
						"description": "Job ID from a previous command",
					},
					"stream": map[string]interface{}{
						"type":        "string",
						"description": "Which stream to retrieve: stdout, stderr, or both (default)",
						"enum":        []string{"stdout", "stderr", "both"},
					},
				},
				"required": []string{"job_id"},
			},
		},
```

Add handler in `CallTool()`:

```go
	case "get_job_logs":
		return s.getJobLogs(args)
```

Add implementation:

```go
func (s *MCPServer) getJobLogs(args map[string]interface{}) (*buildprotocol.JobResult, error) {
	jobID, _ := args["job_id"].(string)
	if jobID == "" {
		return nil, fmt.Errorf("get_job_logs requires 'job_id' argument")
	}

	if s.coordinator == nil {
		return nil, fmt.Errorf("no coordinator configured")
	}

	stdout, stderr, found := s.coordinator.GetRetainedLogs(jobID)
	if !found {
		return &buildprotocol.JobResult{
			JobID:    jobID,
			ExitCode: 1,
			Output:   fmt.Sprintf("logs not found for job %s (may have been evicted)", jobID),
		}, nil
	}

	stream, _ := args["stream"].(string)
	var output string
	switch stream {
	case "stdout":
		output = stdout
	case "stderr":
		output = stderr
	default: // "both" or empty
		response := map[string]interface{}{
			"job_id": jobID,
			"stdout": stdout,
			"stderr": stderr,
		}
		outputBytes, _ := json.MarshalIndent(response, "", "  ")
		output = string(outputBytes)
	}

	return &buildprotocol.JobResult{
		JobID:    jobID,
		ExitCode: 0,
		Output:   output,
	}, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test -run "TestMCPServer_GetJobLogs" ./internal/buildpool/...`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/buildpool/mcp_server.go internal/buildpool/mcp_server_test.go
git commit -m "feat(mcp): add get_job_logs tool for log retrieval"
```

---

### Task 8: Wire Verbosity Through MCP Tool Calls

**Files:**
- Modify: `internal/buildpool/mcp_server.go:211-262`
- Modify: `internal/buildpool/dispatcher.go`
- Test: `internal/buildpool/mcp_server_test.go`

**Step 1: Write the failing test**

This requires an integration test with a mock embedded worker. Add to `mcp_server_test.go`:

```go
func TestMCPServer_VerbosityPassthrough(t *testing.T) {
	registry := NewRegistry()

	// Create embedded worker that returns predictable output
	embedded := func(job *buildprotocol.JobMessage) *buildprotocol.JobResult {
		return &buildprotocol.JobResult{
			JobID:    job.JobID,
			ExitCode: 0,
			Stdout:   "stdout line 1\nstdout line 2\n",
			Stderr:   "stderr output\n",
		}
	}

	dispatcher := NewDispatcher(registry, embedded)
	coord := NewCoordinator(CoordinatorConfig{WebSocketPort: 0}, registry, dispatcher)
	server := NewMCPServer(MCPServerConfig{WorktreePath: "."}, dispatcher, registry)
	server.SetCoordinator(coord)

	// Test minimal verbosity
	result, err := server.CallTool("run_command", map[string]interface{}{
		"command":   "echo test",
		"verbosity": "minimal",
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	if result.Stdout != "" {
		t.Errorf("minimal: stdout should be empty, got %q", result.Stdout)
	}
	if result.Stderr != "stderr output\n" {
		t.Errorf("minimal: stderr = %q, want %q", result.Stderr, "stderr output\n")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -run TestMCPServer_VerbosityPassthrough ./internal/buildpool/...`
Expected: FAIL (verbosity not being passed through)

**Step 3: Write minimal implementation**

Update `CallTool` to extract and pass verbosity:

```go
func (s *MCPServer) CallTool(name string, args map[string]interface{}) (*buildprotocol.JobResult, error) {
	var command string
	var timeout int
	verbosity, _ := args["verbosity"].(string)
	if verbosity == "" {
		verbosity = buildprotocol.VerbosityMinimal
	}

	// ... existing switch ...

	// Submit to dispatcher with verbosity
	resultCh := s.dispatcher.SubmitWithVerbosity(job, verbosity)
	// ...
}
```

Update `dispatcher.go` to add `SubmitWithVerbosity`:

```go
// SubmitWithVerbosity adds a job with verbosity setting
func (d *Dispatcher) SubmitWithVerbosity(job *buildprotocol.JobMessage, verbosity string) chan *buildprotocol.JobResult {
	d.mu.Lock()
	defer d.mu.Unlock()

	resultCh := make(chan *buildprotocol.JobResult, 1)
	pending := &PendingJob{
		Job:       job,
		ResultCh:  resultCh,
		Verbosity: verbosity,
	}

	d.queue = append(d.queue, pending)
	d.pending[job.JobID] = pending

	return resultCh
}

// GetVerbosity returns the verbosity for a pending job
func (d *Dispatcher) GetVerbosity(jobID string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if pj, ok := d.pending[jobID]; ok {
		return pj.Verbosity
	}
	return buildprotocol.VerbosityMinimal
}
```

**Step 4: Run test to verify it passes**

Run: `go test -run TestMCPServer_VerbosityPassthrough ./internal/buildpool/...`
Expected: PASS

**Step 5: Run all tests**

Run: `go test ./internal/buildpool/... ./internal/buildprotocol/...`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/buildpool/mcp_server.go internal/buildpool/dispatcher.go internal/buildpool/mcp_server_test.go
git commit -m "feat(mcp): wire verbosity through tool calls"
```

---

### Task 9: Update HTTP Job Endpoint for Verbosity

**Files:**
- Modify: `internal/buildpool/coordinator.go:274-342`
- Test: `internal/buildpool/coordinator_test.go`

**Step 1: Write the failing test**

Add to `coordinator_test.go`:

```go
func TestCoordinator_HTTPJobVerbosity(t *testing.T) {
	registry := NewRegistry()

	embedded := func(job *buildprotocol.JobMessage) *buildprotocol.JobResult {
		return &buildprotocol.JobResult{
			JobID:    job.JobID,
			ExitCode: 0,
			Stdout:   "stdout",
			Stderr:   "stderr",
		}
	}

	dispatcher := NewDispatcher(registry, embedded)
	coord := NewCoordinator(CoordinatorConfig{WebSocketPort: 0}, registry, dispatcher)

	server := httptest.NewServer(http.HandlerFunc(coord.HandleJobSubmit))
	defer server.Close()

	// Test with minimal verbosity (default)
	reqBody := `{"command":"echo test"}`
	resp, err := http.Post(server.URL, "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	var result JobResponse
	json.NewDecoder(resp.Body).Decode(&result)

	// With minimal, stdout should be filtered out
	// (This test may need adjustment based on exact behavior)
}
```

**Step 2: Run test to verify current behavior**

Run: `go test -run TestCoordinator_HTTPJobVerbosity ./internal/buildpool/...`

**Step 3: Update JobRequest to include verbosity**

```go
// JobRequest represents an HTTP job submission request
type JobRequest struct {
	Command   string `json:"command"`
	Repo      string `json:"repo"`
	Commit    string `json:"commit"`
	Timeout   int    `json:"timeout,omitempty"`
	Verbosity string `json:"verbosity,omitempty"`
}
```

Update `HandleJobSubmit` to use `SubmitWithVerbosity`.

**Step 4: Run test to verify it passes**

Run: `go test -run TestCoordinator_HTTPJobVerbosity ./internal/buildpool/...`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/buildpool/coordinator.go internal/buildpool/coordinator_test.go
git commit -m "feat(coordinator): add verbosity to HTTP job endpoint"
```

---

### Task 10: Integration Test and Final Verification

**Files:**
- Test: `internal/buildpool/integration_test.go` (new)

**Step 1: Write integration test**

Create `internal/buildpool/integration_test.go`:

```go
package buildpool

import (
	"strings"
	"testing"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/buildprotocol"
)

func TestVerbosityIntegration(t *testing.T) {
	registry := NewRegistry()

	// Create mock embedded worker
	embedded := func(job *buildprotocol.JobMessage) *buildprotocol.JobResult {
		// Generate 60 lines of stdout
		var lines []string
		for i := 1; i <= 60; i++ {
			lines = append(lines, "stdout line")
		}
		return &buildprotocol.JobResult{
			JobID:    job.JobID,
			ExitCode: 0,
			Stdout:   strings.Join(lines, "\n") + "\n",
			Stderr:   "warning: something\n",
		}
	}

	dispatcher := NewDispatcher(registry, embedded)
	coord := NewCoordinator(CoordinatorConfig{WebSocketPort: 0}, registry, dispatcher)
	server := NewMCPServer(MCPServerConfig{WorktreePath: "."}, dispatcher, registry)
	server.SetCoordinator(coord)

	t.Run("minimal verbosity", func(t *testing.T) {
		result, err := server.CallTool("run_command", map[string]interface{}{
			"command":   "test",
			"verbosity": "minimal",
		})
		if err != nil {
			t.Fatalf("CallTool: %v", err)
		}
		if result.Stdout != "" {
			t.Error("minimal should have no stdout")
		}
		if result.Stderr == "" {
			t.Error("minimal should have stderr")
		}
	})

	t.Run("normal verbosity", func(t *testing.T) {
		result, err := server.CallTool("run_command", map[string]interface{}{
			"command":   "test",
			"verbosity": "normal",
		})
		if err != nil {
			t.Fatalf("CallTool: %v", err)
		}
		lines := strings.Split(strings.TrimSuffix(result.Stdout, "\n"), "\n")
		if len(lines) != 50 {
			t.Errorf("normal should have 50 lines, got %d", len(lines))
		}
	})

	t.Run("full verbosity", func(t *testing.T) {
		result, err := server.CallTool("run_command", map[string]interface{}{
			"command":   "test",
			"verbosity": "full",
		})
		if err != nil {
			t.Fatalf("CallTool: %v", err)
		}
		lines := strings.Split(strings.TrimSuffix(result.Stdout, "\n"), "\n")
		if len(lines) != 60 {
			t.Errorf("full should have 60 lines, got %d", len(lines))
		}
	})

	t.Run("get_job_logs retrieval", func(t *testing.T) {
		// First run a command
		result, _ := server.CallTool("run_command", map[string]interface{}{
			"command":   "test",
			"verbosity": "minimal",
		})

		// Then retrieve full logs
		logs, err := server.CallTool("get_job_logs", map[string]interface{}{
			"job_id": result.JobID,
		})
		if err != nil {
			t.Fatalf("get_job_logs: %v", err)
		}
		if !strings.Contains(logs.Output, "stdout line") {
			t.Error("get_job_logs should return full stdout")
		}
	})
}
```

**Step 2: Run integration test**

Run: `go test -run TestVerbosityIntegration ./internal/buildpool/...`
Expected: PASS

**Step 3: Run all tests**

Run: `go test ./...`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/buildpool/integration_test.go
git commit -m "test: add verbosity integration tests"
```

---

### Task 11: Update cmd/build-mcp for Verbosity

**Files:**
- Modify: `cmd/build-mcp/main.go`

**Step 1: Verify build-mcp still works**

Run: `go build -o /tmp/build-mcp ./cmd/build-mcp`
Expected: Build succeeds

**Step 2: Wire coordinator to MCP server if needed**

Review `cmd/build-mcp/main.go` and ensure the MCP server has access to the coordinator for `get_job_logs`.

**Step 3: Run full build**

Run: `go build ./...`
Expected: PASS

**Step 4: Commit if changes needed**

```bash
git add cmd/build-mcp/main.go
git commit -m "feat(build-mcp): wire coordinator for log retrieval"
```

---

## Summary

| Task | Description | Files |
|------|-------------|-------|
| 1 | Add verbosity constants and update JobResult | messages.go |
| 2 | Separate stdout/stderr buffers | coordinator.go |
| 3 | Add log retention ring buffer | coordinator.go |
| 4 | Add verbosity filtering function | coordinator.go |
| 5 | Wire verbosity through job completion | coordinator.go, dispatcher.go |
| 6 | Add verbosity param to MCP tools | mcp_server.go |
| 7 | Add get_job_logs tool | mcp_server.go |
| 8 | Wire verbosity through tool calls | mcp_server.go, dispatcher.go |
| 9 | Update HTTP endpoint for verbosity | coordinator.go |
| 10 | Integration tests | integration_test.go |
| 11 | Update build-mcp main | main.go |
