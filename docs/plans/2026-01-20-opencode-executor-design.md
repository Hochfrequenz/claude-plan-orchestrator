# Design: OpenCode Executor Support

## Goal

Allow the orchestrator to dispatch agents using OpenCode instead of Claude Code, configurable globally via config file or CLI flag.

## Background

Currently, agents are hardcoded to use the `claude` CLI. OpenCode (`opencode`) is an alternative AI coding agent with a different CLI interface but similar capabilities.

## CLI Comparison

| Feature | Claude Code | OpenCode |
|---------|-------------|----------|
| Non-interactive | `claude --print -p "prompt"` | `opencode run "prompt"` |
| JSON output | `--output-format stream-json` | `--format json` |
| Session resume | `--resume <session-id>` | `-s <session-id>` |
| Skip permissions | `--dangerously-skip-permissions` | Auto-approved in run mode |
| MCP config | `--mcp-config <path>` | `OPENCODE_CONFIG` env var or `opencode.json` in project |

## Design

### Executor Type

```go
type ExecutorType string

const (
    ExecutorClaudeCode ExecutorType = "claude-code"
    ExecutorOpenCode   ExecutorType = "opencode"
)
```

### Configuration

**Config file** (`~/.config/claude-orchestrator/config.toml`):
```toml
[general]
executor = "claude-code"  # or "opencode"
```

**CLI flag**:
```
claude-orch tui --executor opencode
```

CLI flag overrides config file. Default is `claude-code` for backwards compatibility.

### Agent Changes

The `Agent` struct gains an `ExecutorType` field:

```go
type Agent struct {
    // ... existing fields ...
    ExecutorType ExecutorType
}
```

Command building is delegated to executor-specific methods:

```go
func (a *Agent) buildCommand(ctx context.Context) *exec.Cmd {
    switch a.ExecutorType {
    case ExecutorOpenCode:
        return a.buildOpenCodeCommand(ctx)
    default:
        return a.buildClaudeCodeCommand(ctx)
    }
}
```

### MCP Configuration

**Claude Code**: Uses `--mcp-config <path>` flag pointing to generated JSON file.

**OpenCode**: Uses `OPENCODE_CONFIG` environment variable pointing to generated config file with OpenCode's format:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "server-name": {
      "type": "local",
      "command": ["command", "arg1", "arg2"],
      "enabled": true,
      "environment": {
        "ENV_VAR": "value"
      }
    }
  }
}
```

### Output Streaming

Both executors support JSON output. Initial implementation:
- Claude Code: Full token/cost parsing (existing)
- OpenCode: Basic line streaming for TUI display (token/cost tracking added later)

```go
func (a *Agent) streamOutput(stdout, stderr io.ReadCloser) {
    switch a.ExecutorType {
    case ExecutorOpenCode:
        a.streamOpenCodeOutput(stdout, stderr)
    default:
        a.streamClaudeCodeOutput(stdout, stderr)
    }
}
```

## File Changes

| File | Changes |
|------|---------|
| `internal/config/config.go` | Add `Executor string` to `GeneralConfig`, default `"claude-code"` |
| `internal/executor/agent.go` | Add `ExecutorType`, `buildClaudeCodeCommand()`, `buildOpenCodeCommand()`, `generateOpenCodeMCPConfig()`, `streamOpenCodeOutput()` |
| `cmd/claude-orch/commands.go` | Add `--executor` flag to `tui` command |
| `tui/model.go` | Add `ExecutorType` to `ModelConfig`, pass to agent manager |

## Out of Scope

- Per-task executor selection (global only for now)
- Token/cost tracking for OpenCode (add when JSON format is understood)
- OpenCode session resume testing (implement basic, verify works)

## Verification

1. `claude-orch tui` - uses Claude Code (default)
2. `claude-orch tui --executor opencode` - uses OpenCode
3. Config file `executor = "opencode"` respected
4. CLI flag overrides config file
5. MCP servers work with both executors
6. Agent output streams to TUI for both
