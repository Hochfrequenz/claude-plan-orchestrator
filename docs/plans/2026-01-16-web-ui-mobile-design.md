# Web UI Mobile-Friendly Enhancement Design

## Overview

Extend the existing web UI to provide mobile-friendly access for status monitoring and quick actions. The goal is not full TUI parity, but rather essential controls accessible from phones and tablets.

## Target Use Cases

- Check orchestration status from mobile devices
- Start/stop/pause batch execution
- Monitor agent progress and control stuck/failed agents
- Review and merge flagged PRs
- Adjust group priority tiers

## Architecture

### API Layer Expansion

Extend `web/api/` with new REST endpoints. The existing HTTP router and SSE hub architecture remains intact.

**New Endpoints**

Batch control:
- `POST /api/batch/start` - Start batch execution
- `POST /api/batch/stop` - Stop batch execution
- `POST /api/batch/pause` - Pause batch execution
- `POST /api/batch/resume` - Resume paused batch

Agent control:
- `GET /api/agents` - List all agents (active + recent history)
- `GET /api/agents/{id}` - Agent details including recent log lines
- `GET /api/agents/{id}/logs` - Full log content (for dedicated log page)
- `POST /api/agents/{id}/stop` - Stop running agent
- `POST /api/agents/{id}/resume` - Resume failed agent

PR management:
- `GET /api/prs` - List flagged PRs needing review
- `GET /api/prs/{id}` - PR details
- `POST /api/prs/{id}/merge` - Merge a flagged PR

Group priorities:
- `GET /api/groups` - List groups with priority tiers and progress
- `PUT /api/groups/{name}/priority` - Set group priority tier

### Backend Dependencies

The web server (`web/api/Server`) currently only has access to `taskstore.Store`. To support agent and batch control, it needs additional dependencies:

```go
type Server struct {
    store        *taskstore.Store
    agentManager *executor.AgentManager  // NEW: agent lifecycle
    scheduler    *scheduler.Scheduler    // NEW: batch control
    prbot        *prbot.PRBot            // NEW: PR operations
    sseHub       *SSEHub
}
```

These are passed during server initialization in `cmd/claude-orch/commands.go` `runServe()`.

### SSE Event Expansion

Current events: `status_update`, `task_update`

New events:
- `agent_update` - Agent status changes, token usage updates, new log lines
- `batch_update` - Batch state changes (running/paused/stopped)
- `pr_update` - PR merged or status changed

Event payload format (JSON):
```json
{
  "type": "agent_update",
  "data": {
    "id": "agent-123",
    "status": "running",
    "task_id": "billing/E05",
    "tokens": {"input": 1500, "output": 800},
    "log_lines": ["Building...", "Tests passed"]
  }
}
```

## UI Design

### Navigation

Bottom tab bar with 4 tabs (thumb-friendly placement):
1. **Dashboard** - Status overview and batch controls
2. **Agents** - Agent list and controls
3. **PRs** - Flagged PRs needing review
4. **Groups** - Priority tier management

On larger screens (tablets, desktop), navigation moves to a side rail.

### Dashboard Tab

**Status Cards** (top section):
- Running agents count (pulsing indicator when active)
- Task progress: completed / total
- Queued tasks count
- Flagged PRs count (tappable, jumps to PRs tab)

**Batch Controls** (middle section):
- Large touch-friendly buttons: Start / Pause / Stop
- Current state indicator: Running, Paused, Idle
- Auto-mode toggle switch

**Quick View** (bottom section):
- Next 3 queued tasks (condensed cards)
- Agents needing attention (stuck/failed) with quick action buttons

### Agents Tab

**List View**

Agent cards showing:
- Task ID and title (truncated)
- Status badge: Running (blue), Completed (green), Failed (red), Stuck (orange)
- Elapsed time or completion time
- Context-appropriate action button:
  - Running → Stop
  - Failed → Resume
  - Stuck → Stop

Swipe-left gesture reveals action buttons as alternative to tap.

**Detail View** (inline expansion)

Tap card to expand accordion showing:
- Full task title
- Token usage: input / output / total
- Estimated cost
- Last 10 log lines (auto-updating via SSE)
- "View Full Logs" button

**Full Logs Page** (`/logs/{agent-id}`)

Dedicated page for complete log viewing:
- Monospace text, dark background
- Auto-scroll toggle
- Download as text file
- Better suited for desktop/tablet deep-dive

**History Toggle**

Switch between "Active" and "History" views. History shows last 20 completed/failed runs from database with same detail expansion pattern.

### PRs Tab

**List View**

PR cards showing:
- Task ID and PR number (#123)
- Flag reason badge: Security, Architecture, Migration
- PR title (truncated)
- Time since flagged

**Detail View** (inline expansion)

Tap to expand:
- Full PR title and description summary
- Changed files count
- Link to GitHub PR
- Action buttons: "Merge", "View on GitHub"

Merge shows confirmation dialog before executing. After merge, card animates out.

### Groups Tab

**Tier Display**

Groups organized by priority tier:

```
Tier 0 (runs first)
  [auth] ████████░░ 8/10  [↑] [↓]

Tier 1
  [billing] ██████░░░░ 6/10  [↑] [↓]
  [reporting] ████░░░░░░ 4/10  [↑] [↓]

Unassigned
  [analytics] ██░░░░░░░░ 2/10  [↑] [↓]
```

Each row shows:
- Group name
- Progress bar (completed/total)
- Up/Down tier buttons

**Interactions**:
- Tap ↑ to move to lower tier number (higher priority)
- Tap ↓ to move to higher tier number (lower priority)
- Long-press to unassign from all tiers
- Changes save immediately

## Responsive Design

CSS media queries handle layout changes:

**Mobile** (< 768px):
- Bottom tab navigation
- Single column layout
- Compact cards
- Touch targets minimum 44px

**Tablet** (768px - 1024px):
- Side navigation rail
- Two-column layout where appropriate
- More detail visible inline

**Desktop** (> 1024px):
- Full side navigation
- Multi-column dashboard
- Agent details visible alongside list

## Out of Scope

The following TUI features are excluded from mobile:

- **Sync conflict resolution** - Complex interaction better suited for desktop/TUI
- **Maintenance task modal** - Template selection and scope picking is desktop workflow
- **Full task/module browsing** - Read-heavy views better on larger screens
- **Keyboard shortcuts** - Mobile is touch-only

## Implementation Notes

### Svelte Components

New components needed:
- `BottomNav.svelte` - Tab bar navigation
- `StatusCard.svelte` - Reusable stat card
- `AgentCard.svelte` - Agent list item with expansion
- `PRCard.svelte` - PR list item with expansion
- `GroupRow.svelte` - Priority tier row
- `BatchControls.svelte` - Start/pause/stop buttons
- `LogViewer.svelte` - Streaming log display
- `ConfirmDialog.svelte` - Action confirmation modal

### API Client

Extend `src/lib/api.js` with functions for all new endpoints. Use consistent error handling and loading states.

### SSE Integration

Extend existing EventSource handling to process new event types. Update relevant stores reactively.

## File Changes Summary

**Backend** (`web/api/`):
- `server.go` - Add new dependencies to Server struct
- `handlers.go` - Add handler functions for new endpoints
- `routes.go` - Register new routes (or add to server.go if no separate file)
- `sse.go` - Add new event type broadcasting

**Frontend** (`web/ui/`):
- `src/App.svelte` - Add bottom nav, restructure for tabs
- `src/lib/api.js` - Add API functions
- `src/lib/stores.js` - Add stores for agents, PRs, groups, batch state
- `src/routes/` or `src/components/` - New components listed above
- `src/app.css` - Responsive styles, touch targets
