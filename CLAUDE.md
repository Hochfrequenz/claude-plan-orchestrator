# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Claude Plan Orchestrator is an autonomous development orchestrator that manages Claude Code agents working on your project. It parses markdown plans from `docs/plans/`, dispatches work to agents in isolated git worktrees, and handles the full PR lifecycle through to merge.

## Tech Stack

- **Backend**: Go
- **TUI**: bubbletea / lipgloss
- **Web UI**: Svelte
- **Storage**: SQLite
- **Process management**: Native Go

## Setup

```bash
# Check dependencies (shows what's missing)
./scripts/check-deps.sh

# Install missing dependencies (user-local, no sudo)
./scripts/check-deps.sh --local

# Install missing dependencies (system-wide, requires sudo)
./scripts/check-deps.sh --install
```

## Build Commands

```bash
# Build the CLI
go build -o claude-orch ./cmd/claude-orch

# Run tests
go test ./...

# Run a single test
go test -run TestName ./path/to/package

# Run tests with verbose output
go test -v ./...

# Lint (if golangci-lint configured)
golangci-lint run
```

## Architecture

### Core Components

```
internal/
├── parser/      # Markdown parsing, YAML frontmatter extraction from docs/plans/
├── taskstore/   # SQLite-backed task state management
├── scheduler/   # Dependency resolution, priority queue for task execution
├── executor/    # Git worktree management, Claude Code agent spawning
├── prbot/       # PR creation via gh CLI, semantic diff analysis, auto-merge
├── observer/    # Log collection, metrics, stuck detection
└── sync/        # Status writeback to markdown files and README.md
```

### Data Flow

1. **Parser** reads `docs/plans/*.md` → extracts tasks with dependencies
2. **TaskStore** persists task state to SQLite
3. **Scheduler** resolves dependencies, selects next tasks by priority
4. **Executor** creates worktrees, spawns Claude Code agents with task prompts
5. **PRBot** creates PRs, runs semantic analysis, auto-merges or flags for review
6. **Observer** collects metrics, detects stuck agents
7. **Sync** updates status emojis in README.md and epic files

### Task Model

Tasks are identified as `{module}/E{number}` (e.g., `technical/E05`). Dependencies are:
- **Implicit**: Within a module, E{N} depends on E{N-1}
- **Explicit**: From frontmatter `depends_on` field
- **Cross-module**: Parsed from doc content

### Agent Lifecycle

1. Create worktree from `main` → branch `feat/{module}-E{nn}`
2. Start Claude Code with task prompt containing epic content
3. Agent implements and runs tests via MCP
4. Create PR, semantic analysis determines review routing
5. Auto-merge routine changes; flag security/architecture/migrations
6. Cleanup worktree, sync status back to markdown

### Semantic Review Routing

PRs are auto-merged unless changes touch:
- **security**: Auth, encryption, credentials, permissions
- **architecture**: Core modules, new dependencies, public API
- **migrations**: Database schema changes

## Database

SQLite with tables: `tasks`, `runs`, `logs`, `prs`, `batches`. Runs track worktree paths, token usage, and duration. PRs track review status through to merge.

## Configuration

Main config at `~/.config/claude-orchestrator/config.toml`:
- `project_root`: Path to your project repo
- `worktree_dir`: Where agent worktrees are created
- `max_parallel_agents`: Concurrency limit (default: 3)

Schedule config at `~/.config/claude-orchestrator/schedule.toml` for batch runs.

## CLI Commands

```bash
claude-orch start [--count N] [--module M] [TASK...]  # Start tasks
claude-orch stop [TASK...]                            # Stop tasks gracefully
claude-orch status                                     # Show status summary
claude-orch list [--status S] [--module M]            # List tasks
claude-orch logs TASK                                  # View task logs
claude-orch sync                                       # Re-parse markdown, sync state
claude-orch tui                                        # Launch TUI dashboard
claude-orch serve                                      # Start web UI server
claude-orch pr review                                  # List PRs needing review
claude-orch pr merge TASK                             # Manually merge flagged PR
```
