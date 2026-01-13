# Build-MCP Verbosity Modes Design

## Problem

Build output currently returns everything to MCP callers, cluttering LLM context windows and wasting tokens on irrelevant output (e.g., verbose npm install logs).

## Solution

Three verbosity levels via per-tool parameter, plus a separate tool for log retrieval.

### Verbosity Levels

| Level | Returns | Use Case |
|-------|---------|----------|
| `minimal` (default) | Exit code + stderr | Normal operation, minimal tokens |
| `normal` | Exit code + stderr + last 50 lines stdout | When you want build summary |
| `full` | Exit code + all output | Debugging failures |

### Log Retrieval

New `get_job_logs` tool fetches complete logs by job ID. Coordinator retains logs for the last 50 completed jobs in a ring buffer.

## Protocol Changes

### SubmitMessage

Add verbosity field:

```go
type SubmitMessage struct {
    // ... existing fields ...
    Verbosity string `json:"verbosity,omitempty"` // "minimal", "normal", "full"
}
```

### JobResult

Separate stdout and stderr:

```go
type JobResult struct {
    JobID    string `json:"job_id"`
    Success  bool   `json:"success"`
    ExitCode int    `json:"exit_code"`
    Stderr   string `json:"stderr"`           // Always included
    Stdout   string `json:"stdout,omitempty"` // Based on verbosity
}
```

### MCP Tool Schema

Each tool (run_command, run_tests, etc.) gets an optional `verbosity` parameter:

```json
{
  "name": "verbosity",
  "description": "Output verbosity: minimal (default), normal, full",
  "type": "string",
  "enum": ["minimal", "normal", "full"]
}
```

## Coordinator Changes

### Separate Stream Accumulation

Change from single buffer to separate stdout/stderr tracking:

```go
type jobOutput struct {
    stdout strings.Builder
    stderr strings.Builder
}
outputBuffers map[string]*jobOutput  // keyed by jobID
```

### Log Retention Ring Buffer

```go
type completedLog struct {
    jobID  string
    stdout string
    stderr string
}
completedLogs [50]completedLog       // circular buffer
logIndex      int                    // next write position
logsByID      map[string]*completedLog  // for O(1) lookup
```

When a job completes, its full logs move from `outputBuffers` to `completedLogs`. If the buffer is full, the oldest entry is evicted from the map.

### Verbosity Filtering

Applied in `GetAndClearOutput()`:

- `minimal`: return stderr only
- `normal`: return stderr + last 50 lines of stdout
- `full`: return stderr + full stdout

## New get_job_logs Tool

### Definition

```go
{
    Name:        "get_job_logs",
    Description: "Retrieve full logs for a completed job",
    InputSchema: {
        "type": "object",
        "properties": {
            "job_id": {
                "type": "string",
                "description": "Job ID from a previous build/test command"
            },
            "stream": {
                "type": "string",
                "enum": ["stdout", "stderr", "both"],
                "description": "Which stream(s) to retrieve (default: both)"
            }
        },
        "required": ["job_id"]
    }
}
```

### Behavior

- Returns full logs from the retention buffer
- If job ID not found (evicted or invalid): error message
- Response includes job_id confirmation so caller can verify

### Response Format

```json
{
  "job_id": "abc123",
  "stdout": "...",
  "stderr": "..."
}
```

## Files to Modify

| File | Changes |
|------|---------|
| `internal/buildprotocol/messages.go` | Add `Verbosity` to `SubmitMessage`, split `Stdout`/`Stderr` in `JobResult` |
| `internal/buildpool/coordinator.go` | Separate stdout/stderr buffers, add retention ring buffer, verbosity-aware output retrieval |
| `internal/buildpool/mcp_server.go` | Add `verbosity` param to existing tools, add `get_job_logs` tool |
| `internal/buildpool/dispatcher.go` | Pass verbosity through to result formatting |
| `internal/buildworker/client.go` | Tag output messages with stream type (already does this) |
| `cmd/build-mcp/main.go` | Pass verbosity from HTTP endpoint if using direct submission |

## Backwards Compatibility

- `verbosity` defaults to `minimal` if omitted
- Existing callers see less output (improvement), but can opt into `full` for old behavior

## Testing

- Unit tests for verbosity filtering logic (minimal/normal/full)
- Unit tests for ring buffer eviction
- Integration test for `get_job_logs` retrieval
- Verify stdout tail truncation at 50 lines
