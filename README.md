# Claude Plan Orchestrator

An autonomous development orchestrator that manages Claude Code agents working on your project. It parses markdown plans, dispatches work to agents in isolated git worktrees, and handles the full PR lifecycle through to merge.

## Features

- **Task Management**: Parse tasks from markdown plans with YAML frontmatter
- **Dependency Resolution**: Automatic implicit (sequential) and explicit dependency tracking
- **Agent Orchestration**: Spawn and manage multiple Claude Code agents in parallel
- **Git Worktree Isolation**: Each agent works in its own worktree for conflict-free development
- **PR Lifecycle**: Automatic PR creation, semantic review routing, and auto-merge for routine changes
- **Real-time Monitoring**: TUI dashboard and Web UI with live status updates
- **Notifications**: Desktop notifications and Slack webhook integration
- **Scheduled Batches**: Cron-based batch execution for overnight runs

## Quick Start

```bash
# Install (no compilation required)
curl -fsSL https://raw.githubusercontent.com/hochfrequenz/claude-plan-orchestrator/main/scripts/install.sh | bash

# Set up a new project
claude-orch onboard
```

## Installation

### Option 1: Quick Install (Recommended)

Download a pre-built binary with a single command:

```bash
# Install latest version
curl -fsSL https://raw.githubusercontent.com/hochfrequenz/claude-plan-orchestrator/main/scripts/install.sh | bash

# Install specific version
curl -fsSL https://raw.githubusercontent.com/hochfrequenz/claude-plan-orchestrator/main/scripts/install.sh | bash -s -- v1.0.0

# Custom install directory
INSTALL_DIR=/usr/local/bin curl -fsSL https://raw.githubusercontent.com/hochfrequenz/claude-plan-orchestrator/main/scripts/install.sh | bash
```

### Option 2: Homebrew (macOS/Linux)

```bash
brew tap hochfrequenz/tap
brew install claude-orch
```

### Option 3: Build from Source

Prerequisites: Go 1.21+

```bash
# Clone the repository
git clone https://github.com/hochfrequenz/claude-plan-orchestrator.git
cd claude-plan-orchestrator

# Build the CLI
go build -o claude-orch ./cmd/claude-orch

# Install to PATH
mv claude-orch ~/.local/bin/
```

### Prerequisites

- Git
- Claude Code CLI (`claude`) - required for agent execution

### Check Dependencies

```bash
# Check what's installed/missing
./scripts/check-deps.sh

# Install missing dependencies (user-local)
./scripts/check-deps.sh --local

# Install missing dependencies (system-wide)
./scripts/check-deps.sh --install
```

## Project Onboarding

Set up claude-orch for a new project with the interactive wizard:

```bash
claude-orch onboard
```

This will:
1. Check prerequisites (git, claude CLI)
2. Create configuration file (~/.config/claude-plan-orchestrator/config.toml)
3. Set up plans directory structure (docs/plans/)
4. Create a sample plan file
5. Run initial task sync

Alternatively, run the standalone onboarding script:

```bash
curl -fsSL https://raw.githubusercontent.com/hochfrequenz/claude-plan-orchestrator/main/scripts/onboard.sh | bash
```

## Configuration

Create a configuration file at `~/.config/claude-plan-orchestrator/config.toml`:

```toml
[general]
# Path to the your project repository
project_root = "~/code/energy-erp"

# Directory for agent worktrees
worktree_dir = "~/.claude-plan-orchestrator/worktrees"

# Maximum concurrent agents
max_parallel_agents = 3

# SQLite database path
database_path = "~/.claude-plan-orchestrator/orchestrator.db"

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

Create `~/.config/claude-plan-orchestrator/schedule.toml` for automated batch runs:

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
claude-orch sync
```

Tasks are identified as `{module}/E{number}` (e.g., `technical/E05`).

### Viewing Status

```bash
# Summary view
claude-orch status

# List all tasks
claude-orch list

# Filter by module
claude-orch list --module technical

# Filter by status
claude-orch list --status in_progress
```

### Starting Tasks

```bash
# Start next 3 ready tasks (default)
claude-orch start

# Start specific number of tasks
claude-orch start --count 5

# Start specific tasks
claude-orch start technical/E05 billing/E02

# Start tasks from specific module
claude-orch start --module technical
```

### Viewing Logs

```bash
claude-orch logs technical/E05
```

### TUI Dashboard

Launch the terminal UI for real-time monitoring:

```bash
claude-orch tui
```

### Web UI

Start the web server:

```bash
# Default port 8080
claude-orch serve

# Custom port
claude-orch serve --port 3000
```

Then open http://localhost:8080 in your browser.

### PR Management

```bash
# List PRs needing review
claude-orch pr review

# Manually merge a flagged PR
claude-orch pr merge technical/E05
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
