# ERP Orchestrator

An autonomous development orchestrator that manages Claude Code agents working on EnergyERP. It parses markdown plans, dispatches work to agents in isolated git worktrees, and handles the full PR lifecycle through to merge.

## Features

- **Task Management**: Parse tasks from markdown plans with YAML frontmatter
- **Dependency Resolution**: Automatic implicit (sequential) and explicit dependency tracking
- **Agent Orchestration**: Spawn and manage multiple Claude Code agents in parallel
- **Git Worktree Isolation**: Each agent works in its own worktree for conflict-free development
- **PR Lifecycle**: Automatic PR creation, semantic review routing, and auto-merge for routine changes
- **Real-time Monitoring**: TUI dashboard and Web UI with live status updates
- **Notifications**: Desktop notifications and Slack webhook integration
- **Scheduled Batches**: Cron-based batch execution for overnight runs

## Installation

### Prerequisites

- Go 1.21+
- Git
- Claude Code CLI (`claude`)
- Node.js 18+ (for Web UI development)

### Build from Source

```bash
# Clone the repository
git clone https://github.com/anthropics/erp-orchestrator.git
cd erp-orchestrator

# Build the CLI
go build -o erp-orch ./cmd/erp-orch

# Optional: Install to PATH
sudo mv erp-orch /usr/local/bin/
```

### Check Dependencies

```bash
# Check what's installed/missing
./scripts/check-deps.sh

# Install missing dependencies (user-local)
./scripts/check-deps.sh --local

# Install missing dependencies (system-wide)
./scripts/check-deps.sh --install
```

## Configuration

Create a configuration file at `~/.config/erp-orchestrator/config.toml`:

```toml
[general]
# Path to the EnergyERP repository
project_root = "~/code/energy-erp"

# Directory for agent worktrees
worktree_dir = "~/.erp-orchestrator/worktrees"

# Maximum concurrent agents
max_parallel_agents = 3

# SQLite database path
database_path = "~/.erp-orchestrator/orchestrator.db"

[claude]
model = "claude-sonnet-4-20250514"
max_tokens = 16000

[notifications]
desktop = true
slack_webhook = ""  # Optional: Slack webhook URL

[web]
host = "127.0.0.1"
port = 8080
```

### Scheduled Batches (Optional)

Create `~/.config/erp-orchestrator/schedule.toml` for automated batch runs:

```toml
[[batch]]
name = "overnight"
cron = "0 22 * * *"  # 10 PM daily
max_tasks = 10
max_duration = "8h"
notify_on_complete = true

[[batch]]
name = "weekday-noon"
cron = "0 12 * * 1-5"  # Noon on weekdays
max_tasks = 5
max_duration = "4h"
```

## Usage

### Syncing Tasks

Parse tasks from markdown files in `docs/plans/`:

```bash
erp-orch sync
```

Tasks are identified as `{module}/E{number}` (e.g., `technical/E05`).

### Viewing Status

```bash
# Summary view
erp-orch status

# List all tasks
erp-orch list

# Filter by module
erp-orch list --module technical

# Filter by status
erp-orch list --status in_progress
```

### Starting Tasks

```bash
# Start next 3 ready tasks (default)
erp-orch start

# Start specific number of tasks
erp-orch start --count 5

# Start specific tasks
erp-orch start technical/E05 billing/E02

# Start tasks from specific module
erp-orch start --module technical
```

### Viewing Logs

```bash
erp-orch logs technical/E05
```

### TUI Dashboard

Launch the terminal UI for real-time monitoring:

```bash
erp-orch tui
```

### Web UI

Start the web server:

```bash
# Default port 8080
erp-orch serve

# Custom port
erp-orch serve --port 3000
```

Then open http://localhost:8080 in your browser.

### PR Management

```bash
# List PRs needing review
erp-orch pr review

# Manually merge a flagged PR
erp-orch pr merge technical/E05
```

## Task Format

Tasks are defined in markdown files under `docs/plans/`. Example:

```markdown
---
module: technical
epic: 5
title: Input Validators
priority: high
depends_on:
  - technical/E04
  - billing/E00
---

# Epic 05: Input Validators

Implement input validation for all user-facing forms.

## Acceptance Criteria

- [ ] Email validation
- [ ] Phone number validation
- [ ] Required field validation
```

### Dependencies

- **Implicit**: Within a module, E{N} automatically depends on E{N-1}
- **Explicit**: Defined in `depends_on` frontmatter field
- **Cross-module**: Can reference tasks from other modules

## Architecture

```
internal/
├── parser/      # Markdown parsing, YAML extraction
├── taskstore/   # SQLite task state management
├── scheduler/   # Dependency resolution, priority queue
├── executor/    # Worktree management, agent spawning
├── prbot/       # PR creation, semantic analysis, auto-merge
├── observer/    # Log collection, metrics, stuck detection
├── sync/        # Status writeback to markdown
├── batch/       # Scheduled batch execution
└── notify/      # Desktop and Slack notifications

web/
├── api/         # HTTP API with SSE
└── ui/          # Svelte web dashboard
```

## Semantic Review Routing

PRs are automatically merged unless changes touch sensitive areas:

| Category | Triggers |
|----------|----------|
| **Security** | Auth, encryption, credentials, permissions |
| **Architecture** | Core modules, new dependencies, public API |
| **Migrations** | Database schema changes |

Flagged PRs require manual review and merge.

## Status Legend

| Emoji | Status |
|-------|--------|
| 🔴 | Not started |
| 🟡 | In progress |
| 🟢 | Complete |

## Development

```bash
# Run tests
go test ./...

# Run specific test
go test -run TestName ./path/to/package

# Lint
golangci-lint run

# Build Web UI (for development)
cd web/ui
npm install
npm run dev
```

## License

MIT
