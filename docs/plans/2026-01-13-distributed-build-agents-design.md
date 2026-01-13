# Distributed Build Agents Design

**Date**: 2026-01-13
**Status**: Draft

## Overview

A distributed build system that offloads build/clippy/test workloads from the central orchestrator server to worker machines with unused CPU cycles. Claude Code agents remain on the central server, but their MCP tool calls for builds and tests are dispatched to remote workers.

## Goals

- Utilize beefy machines on the network for build/test parallelism
- Keep orchestration and LLM interaction on central server
- Transparent fallback to local execution when no workers available
- Reproducible build environments via Nix
- Real-time output streaming to Claude Code agents

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    Central Server                        │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────┐  │
│  │ Claude Code │→ │ MCP Server  │→ │ Build Coordinator│  │
│  │   Agents    │  │ (new)       │  │ + Embedded Worker│  │
│  └─────────────┘  └─────────────┘  └────────┬────────┘  │
│                                              │ WebSocket │
│                        ┌─────────────────────┴─────┐     │
│                        │ Git Daemon (:9418)        │     │
└────────────────────────┴───────────────────────────┴─────┘
                              │              │
                    Tailscale │              │ git://
                              ▼              ▼
         ┌──────────────────────────────────────────────┐
         │              Worker Machine                   │
         │  ┌────────────────────────────────────────┐  │
         │  │           Build Worker                  │  │
         │  │  ┌─────────┐ ┌─────────┐ ┌─────────┐   │  │
         │  │  │ Job 1   │ │ Job 2   │ │ Job 3   │   │  │
         │  │  │worktree │ │worktree │ │worktree │   │  │
         │  │  │nix shell│ │nix shell│ │nix shell│   │  │
         │  │  └─────────┘ └─────────┘ └─────────┘   │  │
         │  └────────────────────────────────────────┘  │
         │  ┌────────────────────────────────────────┐  │
         │  │  Persistent Git Clone (shared objects) │  │
         │  └────────────────────────────────────────┘  │
         └──────────────────────────────────────────────┘
```

### Components

1. **Build Coordinator** (runs on central server)
   - WebSocket server for worker connections
   - Git daemon for source distribution
   - MCP server interface for Claude Code agents
   - Job queue and dispatch logic
   - Embedded worker for local fallback

2. **Build Worker** (runs on beefy machines)
   - Connects to coordinator via WebSocket over Tailscale
   - Maintains persistent git clone from coordinator's git daemon
   - Creates isolated worktrees + Nix shells per job
   - Streams output back in real-time
   - Configurable parallelism

3. **MCP Interface** (used by Claude Code)
   - Tools: `build`, `clippy`, `test`, `run_command`
   - Synchronous from LLM perspective
   - Transparent routing to remote or local workers

## Communication Protocol

### Transport

WebSocket with JSON messages over Tailscale network.

### Message Types

**Worker → Coordinator:**

```json
{"type": "register", "worker_id": "worker-1", "max_jobs": 4}
{"type": "ready", "slots": 2}
{"type": "output", "job_id": "...", "stream": "stdout", "data": "..."}
{"type": "complete", "job_id": "...", "exit_code": 0}
{"type": "error", "job_id": "...", "message": "nix develop failed"}
{"type": "pong"}
```

**Coordinator → Worker:**

```json
{"type": "job", "job_id": "...", "repo": "git://central:9418/project",
 "commit": "abc123", "command": "cargo test --lib", "env": {...}}
{"type": "cancel", "job_id": "..."}
{"type": "ping"}
```

### Job Lifecycle

1. Claude Code calls MCP tool (e.g., `test(filter="auth")`)
2. Coordinator creates job with repo, commit, command
3. Coordinator checks for ready workers (slots > 0)
4. If worker ready → push job immediately via WebSocket
5. If no workers ready → dispatch to embedded worker
6. Worker executes job, streams output
7. Worker sends `complete` with exit code
8. Coordinator returns result to MCP call
9. Worker sends `ready` with updated slots

### Heartbeat & Reconnection

- Coordinator sends `ping` every 30s, expects `pong` within 10s
- Workers reconnect with exponential backoff on disconnect
- In-flight jobs on disconnected worker are re-queued after timeout

## MCP Server Interface

```typescript
build(args?: {
  release?: boolean,
  features?: string[],
  package?: string
}) → {exit_code: number, output: string, duration_secs: number}

clippy(args?: {
  fix?: boolean,
  features?: string[]
}) → {exit_code: number, output: string, warnings: number, errors: number}

test(args?: {
  filter?: string,
  package?: string,
  features?: string[],
  nocapture?: boolean
}) → {exit_code: number, output: string, passed: number, failed: number, ignored: number}

run_command(args: {
  command: string,
  timeout_secs?: number
}) → {exit_code: number, output: string, duration_secs: number}

worker_status() → {
  workers: [{id, connected_since, active_jobs, max_jobs}],
  queued_jobs: number,
  local_fallback_active: boolean
}
```

### Context Passing

MCP server is started per Claude Code agent session with worktree path as argument:

```bash
build-mcp-server --worktree /path/to/agent/worktree
```

The MCP server derives repo and current commit from the worktree.

## Worker Job Execution

### Execution Flow

```bash
1. Acquire job slot (decrement available slots)

2. Fetch latest from coordinator's git daemon
   $ git fetch origin

3. Create isolated worktree for this job
   $ git worktree add /tmp/build-agent/jobs/{job_id} {commit}

4. Enter Nix shell and run command, streaming output
   $ cd /tmp/build-agent/jobs/{job_id}
   $ nix develop --command sh -c "{command}" 2>&1

5. Capture exit code, send completion message

6. Cleanup worktree
   $ git worktree remove /tmp/build-agent/jobs/{job_id}

7. Release job slot (send "ready" with updated slots)
```

### Isolation Model

- Each job runs in its own git worktree (source isolation)
- Each job runs in its own `nix develop` shell (environment isolation)
- Build environment defined by repo's `flake.nix`
- Jobs share the git object store (efficient storage)

### Error Handling

| Error | Worker Action |
|-------|---------------|
| `git fetch` fails | Return error, don't create worktree |
| `nix develop` fails | Return error with nix output |
| Command timeout | SIGTERM, wait 5s, SIGKILL, return timeout error |
| Worker crash mid-job | Coordinator re-queues after heartbeat timeout |

## Coordinator Configuration

```toml
# ~/.config/claude-orchestrator/config.toml (extended)

[build_pool]
enabled = true
websocket_port = 8080
git_daemon_port = 9418

[build_pool.local_fallback]
enabled = true
max_jobs = 2
worktree_dir = "/tmp/build-coordinator/local"

[build_pool.timeouts]
job_default_secs = 300
heartbeat_interval_secs = 30
heartbeat_timeout_secs = 10
```

## Worker Configuration

### NixOS Module

```nix
{ config, pkgs, ... }:
{
  services.build-agent = {
    enable = true;
    serverUrl = "wss://central-server:8080/ws";
    maxJobs = 4;
    gitCacheDir = "/var/cache/build-agent/repos";
    worktreeDir = "/tmp/build-agent/jobs";
    timeouts = {
      jobDefault = 300;
      nixDevelop = 120;
    };
  };

  nix.settings.experimental-features = [ "nix-command" "flakes" ];
}
```

The NixOS module generates the TOML config file. For non-NixOS machines, the TOML config is written manually.

### Systemd Unit

```ini
[Unit]
Description=Build Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/bin/build-agent --config /etc/build-agent/config.toml
Restart=always
RestartSec=10
NoNewPrivileges=true
ProtectSystem=strict
ReadWritePaths=/var/cache/build-agent /tmp/build-agent

[Install]
WantedBy=multi-user.target
```

## Project Structure

Integration into existing orchestrator codebase:

```
erp-orchestrator/
├── cmd/
│   ├── claude-orch/           # Existing (add build-pool commands)
│   └── build-agent/           # New: worker binary
│       └── main.go
├── internal/
│   ├── executor/              # Existing
│   ├── mcp/                   # Existing
│   ├── taskstore/             # Existing
│   │
│   ├── buildpool/             # New: coordinator logic
│   │   ├── coordinator.go     # WebSocket server + job dispatch
│   │   ├── dispatcher.go      # Job queue, worker tracking
│   │   ├── gitdaemon.go       # Git daemon wrapper
│   │   ├── embedded.go        # Embedded worker
│   │   └── mcp_server.go      # MCP server for build tools
│   ├── buildworker/           # New: worker logic
│   │   ├── executor.go        # Worktree + nix shell execution
│   │   ├── pool.go            # Job slot management
│   │   └── stream.go          # Output streaming
│   └── buildprotocol/         # New: shared messages
│       └── messages.go
├── nix/
│   └── build-agent.nix        # NixOS module
└── tui/                       # Extend with worker status
```

### CLI Extensions

```bash
# Existing commands unchanged
claude-orch start [TASK...]
claude-orch status
claude-orch tui

# New commands
claude-orch build-pool start      # Start coordinator
claude-orch build-pool status     # Show workers, queue
claude-orch build-pool stop       # Shutdown coordinator
```

### TUI Extensions

New panel showing:
- Connected workers (id, active jobs, max jobs)
- Current job queue depth
- Recent job completions with timing

## Implementation Phases

### Phase 1: Core Protocol & Worker

1. Define `buildprotocol` message types
2. Implement `buildworker/executor.go` - worktree + nix execution
3. Implement `buildworker/pool.go` - job slot management
4. Create `cmd/build-agent` binary with WebSocket client
5. Test worker standalone

### Phase 2: Coordinator

1. Implement `buildpool/coordinator.go` - WebSocket server
2. Implement `buildpool/dispatcher.go` - job queue, worker tracking
3. Implement `buildpool/gitdaemon.go` - git daemon management
4. Implement `buildpool/embedded.go` - local fallback
5. Add `claude-orch build-pool` commands
6. Test coordinator + worker communication

### Phase 3: MCP Integration

1. Implement `buildpool/mcp_server.go` - build/clippy/test tools
2. Wire MCP server into agent spawning
3. Update agent prompts for new MCP tools
4. End-to-end testing

### Phase 4: TUI & Polish

1. Add worker status panel to TUI
2. Add job queue visibility
3. Implement job timeout handling
4. Add metrics
5. NixOS module for deployment

### Phase 5: Production Hardening

1. Reconnection with exponential backoff
2. Graceful shutdown (drain jobs)
3. Job result persistence
4. Worker authentication (if needed beyond Tailscale)

## Security Considerations

- All communication over Tailscale (encrypted, authenticated network)
- Git daemon read-only, only serves to authenticated Tailscale nodes
- Workers execute arbitrary commands (trusted, same as local execution)
- No secrets in job payloads (environment from Nix)
