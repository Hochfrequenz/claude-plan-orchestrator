# ERP Orchestrator Design

Autonomous development orchestrator for EnergyERP that reads markdown plans, manages Claude Code agents in git worktrees, and handles the full lifecycle through to PR merge.

## Overview

The system acts as a "development manager" that:
- Knows what needs to be done (parses `docs/plans/`)
- Knows what can be done now (dependency resolution)
- Dispatches work to Claude Code agents in isolated worktrees
- Monitors progress and handles completion (PR, review routing, merge)
- Reports status through TUI and Web UI

```
┌─────────────────┐     ┌──────────────┐     ┌─────────────────┐
│  docs/plans/*.md │────▶│  Orchestrator │────▶│  Claude Code    │
│  (source of truth)│    │  (scheduler)   │     │  (in worktrees) │
└─────────────────┘     └──────────────┘     └─────────────────┘
         ▲                      │                      │
         │                      ▼                      ▼
         │              ┌──────────────┐     ┌─────────────────┐
         └──────────────│ Status Sync   │◀────│  PR Automator   │
                        └──────────────┘     └─────────────────┘
```

## Tech Stack

| Component | Technology |
|-----------|------------|
| Backend | Go |
| TUI | bubbletea / lipgloss |
| Web UI | Svelte |
| Storage | SQLite |
| Process management | Native Go |

## Task Model

### Source Structure

```
docs/plans/
├── technical-module/
│   ├── 00-overview.md          → Module metadata
│   ├── epic-00-scaffolding.md  → Task: technical/E00
│   ├── epic-01-supporting-entities.md → Task: technical/E01
│   └── ...
├── billing-module/
│   └── ...
└── README.md                   → Status sync target
```

### Task Fields

| Field | Source | Description |
|-------|--------|-------------|
| ID | Path | `{module}/E{number}` (e.g., `technical/E05`) |
| Title | Markdown H1 | Epic title from header |
| Description | Markdown | First paragraph or summary section |
| Status | README.md | 🔴 not_started / 🟡 in_progress / 🟢 complete |
| Dependencies | Implicit + explicit | E05 depends on E04; parsed from frontmatter |
| Priority | Frontmatter | Optional business priority override |
| Needs Review | Frontmatter | Force human review flag |

### Optional Frontmatter

Epic files may include YAML frontmatter for explicit metadata:

```yaml
---
priority: high
depends_on: [billing/E01, technical/E03]
needs_review: true
---
```

### Dependency Resolution

1. **Implicit**: Within a module, `E{N}` depends on `E{N-1}`
2. **Explicit**: Parsed from frontmatter `depends_on` field
3. **Cross-module**: Parsed from doc content ("Requires", "Depends on", "After")

## Execution Model

### Worktree Isolation

Each task runs in its own git worktree:

```
~/github/energyerp/              # main worktree (interactive work)
~/.erp-orchestrator/worktrees/
├── technical-E05-abc123/        # agent working on technical/E05
├── billing-E03-def456/          # agent working on billing/E03
└── pricing-E04-ghi789/          # agent working on pricing/E04
```

### Agent Lifecycle

1. **Spawn**: Create worktree from `main`, checkout branch `feat/{module}-E{nn}`
2. **Initialize**: Start Claude Code with task prompt
3. **Execute**: Agent implements, runs tests via MCP test runner
4. **Complete**: Agent signals done (or stuck/needs-help)
5. **PR**: Create PR, run semantic analysis for review routing
6. **Merge/Flag**: Auto-merge or flag for human review
7. **Cleanup**: Delete worktree, sync status back to markdown

### Task Prompt Template

```
You are implementing: {task_title}

Epic: {epic_markdown_content}

Module context: {overview_md if exists}

Dependencies completed: {list of completed prerequisite epics}

Instructions:
1. Implement the epic requirements
2. Run tests via MCP test runner (sync_tests, run_tests)
3. Ensure all tests pass
4. Signal completion when done
```

### Concurrency

- Configurable max parallel agents (default: 3)
- Respects dependency order
- Resource-aware limiting optional

## PR Automation

### PR Creation

After implementation completes:

1. **Commit**: `feat({module}): implement E{nn} - {title}`
2. **Push**: Push branch to origin
3. **Create PR**: Via `gh pr create`

PR body template:

```markdown
## Summary
Implements {epic_title}

## Changes
- {AI-generated summary of what changed}

## Test Results
- ✅ {N} tests passed
- 🕐 {duration}

## Epic Reference
{link to docs/plans/{module}/epic-{nn}.md}

🤖 Autonomous implementation by ERP Orchestrator
```

### Semantic Review Routing

AI scans the diff and categorizes changes:

| Category | Triggers | Action |
|----------|----------|--------|
| `security` | Auth, encryption, credentials, permissions | Flag for review |
| `architecture` | Core module changes, new dependencies, public API | Flag for review |
| `migrations` | Database schema changes | Flag for review |
| `routine` | Everything else | Auto-merge |

Flagged PRs:
- Add label `needs-human-review`
- Send notification
- Appear in TUI/Web UI attention queue

### Merge Behavior

- **Auto-merge**: Squash merge, delete branch
- **Flagged**: Wait for human approval

## Monitoring & Observability

### Metrics Per Agent

| Metric | Description |
|--------|-------------|
| Status | `queued` → `running` → `pr_created` → `merged` / `needs_review` / `failed` |
| Duration | Wall clock time from spawn to completion |
| Token usage | Input/output tokens consumed |
| Test results | Pass/fail counts, duration |
| Logs | Full Claude Code session output |
| Errors | Captured failures, stuck detection |

### Stuck Detection

- No progress for N minutes → mark as `stuck`
- Agent explicitly signals "I need help with X"
- Test failures after M retries

### TUI Dashboard

```
┌─ ERP Orchestrator ──────────────────────────────────────────────┐
│ Active: 3/3 │ Queued: 7 │ Completed today: 12 │ Flagged: 1     │
├─────────────────────────────────────────────────────────────────┤
│ RUNNING                                                         │
│  ● technical/E05  Validators       12m  ████████░░ tests running│
│  ● billing/E03    Advance Payment  24m  ██████████ implementing │
│  ● pricing/E04    Price Calc        3m  ██░░░░░░░░ starting     │
├─────────────────────────────────────────────────────────────────┤
│ QUEUED (next 5)                                                 │
│  ○ technical/E06  Formula Engine   (waiting: E05)               │
│  ○ billing/E05    Invoice Lifecycle (waiting: E03)              │
│  ○ measurement/E03 MSCONS          (ready)                      │
│  ...                                                            │
├─────────────────────────────────────────────────────────────────┤
│ NEEDS ATTENTION                                                 │
│  ⚠ customer/E05   HTTP API         PR #142 needs review         │
└─────────────────────────────────────────────────────────────────┘
│ [r]efresh [l]ogs [s]tart batch [p]ause [q]uit                  │
```

### Web UI Features

- Real-time status dashboard
- Historical charts (throughput, token costs over time)
- PR review queue with diff preview
- Log viewer with search
- Task dependency graph visualization

## Scheduling

### Manual Triggers (CLI)

```bash
# Start the next N ready tasks
erp-orch start --count 3

# Start specific tasks
erp-orch start technical/E05 billing/E03

# Start all ready tasks in a module
erp-orch start --module technical

# Pause all running agents (gracefully)
erp-orch pause

# Resume paused agents
erp-orch resume

# Show status
erp-orch status

# View logs for a task
erp-orch logs technical/E05
```

### Scheduled Batches

Configuration in `~/.config/erp-orchestrator/schedule.toml`:

```toml
[[batch]]
name = "overnight"
cron = "0 22 * * *"          # 10 PM daily
max_tasks = 10
max_duration = "8h"
notify_on_complete = true

[[batch]]
name = "lunch-break"
cron = "0 12 * * 1-5"        # noon on weekdays
max_tasks = 3
max_duration = "1h"
```

### Priority in Task Selection

1. Explicit `priority: high` in epic frontmatter
2. Business priority field (if set)
3. Dependency depth (tasks that unblock more work go first)
4. Module grouping (preference to finish a module)

## Architecture

### Project Structure

```
erp-orchestrator/
├── cmd/
│   └── erp-orch/              # CLI entrypoint
├── internal/
│   ├── parser/                # Markdown parsing, frontmatter extraction
│   ├── taskstore/             # SQLite-backed task state
│   ├── scheduler/             # Dependency resolution, priority queue
│   ├── executor/              # Worktree management, Claude Code spawning
│   ├── prbot/                 # PR creation, semantic analysis, merge
│   ├── observer/              # Log collection, metrics, stuck detection
│   └── sync/                  # Status writeback to markdown/README
├── tui/                       # Bubbletea dashboard
├── web/
│   ├── api/                   # HTTP API
│   └── ui/                    # Svelte SPA
├── config/
│   └── schema.go              # Config structs
└── migrations/                # SQLite schema
```

### Data Flow

```
┌────────────┐    parse     ┌────────────┐
│ docs/plans │─────────────▶│  TaskStore │◀──────┐
└────────────┘              └────────────┘       │
      ▲                           │              │
      │ sync status               │ next task   │ update status
      │                           ▼              │
┌────────────┐              ┌────────────┐      │
│ README.md  │◀─────────────│ Scheduler  │──────┤
└────────────┘              └────────────┘      │
                                  │             │
                                  ▼             │
                            ┌────────────┐      │
                            │  Executor  │──────┤
                            └────────────┘      │
                                  │             │
                                  ▼             │
                            ┌────────────┐      │
                            │   PRBot    │──────┘
                            └────────────┘
                                  │
                                  ▼
                            ┌────────────┐
                            │  Observer  │───▶ TUI / Web UI
                            └────────────┘
```

### Database Schema

```sql
-- Core task tracking
CREATE TABLE tasks (
    id TEXT PRIMARY KEY,           -- e.g., "technical/E05"
    module TEXT NOT NULL,
    epic_num INTEGER NOT NULL,
    title TEXT NOT NULL,
    description TEXT,
    status TEXT NOT NULL DEFAULT 'not_started',
    priority TEXT,
    depends_on TEXT,               -- JSON array of task IDs
    needs_review BOOLEAN DEFAULT FALSE,
    file_path TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Execution runs
CREATE TABLE runs (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id),
    worktree_path TEXT,
    branch TEXT,
    status TEXT NOT NULL,          -- queued, running, completed, failed, stuck
    started_at TIMESTAMP,
    finished_at TIMESTAMP,
    tokens_input INTEGER,
    tokens_output INTEGER,
    FOREIGN KEY (task_id) REFERENCES tasks(id)
);

-- Log entries
CREATE TABLE logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL REFERENCES runs(id),
    timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    level TEXT,
    message TEXT,
    FOREIGN KEY (run_id) REFERENCES runs(id)
);

-- PR tracking
CREATE TABLE prs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL REFERENCES runs(id),
    pr_number INTEGER,
    url TEXT,
    review_status TEXT,            -- pending, approved, merged, closed
    merged_at TIMESTAMP,
    FOREIGN KEY (run_id) REFERENCES runs(id)
);

-- Scheduled batches
CREATE TABLE batches (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    started_at TIMESTAMP,
    finished_at TIMESTAMP,
    tasks_completed INTEGER DEFAULT 0,
    tasks_failed INTEGER DEFAULT 0
);
```

## Configuration

### Main Config

`~/.config/erp-orchestrator/config.toml`:

```toml
[general]
project_root = "~/github/energyerp"
worktree_dir = "~/.erp-orchestrator/worktrees"
max_parallel_agents = 3

[claude]
model = "claude-sonnet-4-20250514"
max_tokens = 16000

[notifications]
desktop = true
slack_webhook = ""  # optional

[web]
port = 8080
host = "127.0.0.1"
```

## Notifications

| Event | Notification |
|-------|--------------|
| Task completed | Desktop notification with summary |
| PR needs review | Desktop + optional Slack |
| Agent stuck | Desktop + optional Slack |
| Batch completed | Summary with stats |

## Status Sync

### README.md Updates

The orchestrator updates the status emoji in README.md epic tables:

- `🔴` → `🟡` when task starts
- `🟡` → `🟢` when PR merged
- `🟡` → `🔴` on failure (with note)

### Epic File Updates

Optionally add status badge to epic file:

```markdown
<!-- erp-orchestrator: status=complete, pr=#142, merged=2026-01-12 -->
# Epic 05: Validators
```

## CLI Reference

| Command | Description |
|---------|-------------|
| `erp-orch start [--count N] [--module M] [TASK...]` | Start tasks |
| `erp-orch stop [TASK...]` | Stop running tasks gracefully |
| `erp-orch pause` | Pause all agents |
| `erp-orch resume` | Resume paused agents |
| `erp-orch status` | Show current status summary |
| `erp-orch list [--status S] [--module M]` | List tasks |
| `erp-orch logs TASK` | View logs for a task |
| `erp-orch sync` | Force re-parse markdown and sync |
| `erp-orch tui` | Launch TUI dashboard |
| `erp-orch serve` | Start web UI server |
| `erp-orch pr review` | List PRs needing review |
| `erp-orch pr merge TASK` | Manually merge a flagged PR |

## Security Considerations

- Claude Code runs with same permissions as user
- No credentials stored in SQLite (uses system keychain/env)
- Web UI binds to localhost by default
- Semantic analysis errs on side of caution (flag if uncertain)

## Future Considerations

- Multi-repository support
- Team collaboration features
- Cost tracking and budgets
- Integration with external issue trackers
- Mobile notifications app
